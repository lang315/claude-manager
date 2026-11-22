package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lang315/claude-manager/account"
)

// add is cswap add: the account Claude Code is signed in to, added (the
// app follows it), with a slot and an alias when asked.
func (c *cmd) add(ctx context.Context, slot int, alias string, yes bool) error {
	accts, err := c.b.Refresh(ctx, true)
	i := slices.IndexFunc(accts, func(a account.Account) bool { return a.InUse })
	if i < 0 || !accts[i].Added {
		return errors.Join(err, errors.New("Claude Code is signed in to no account the app could add: run claude and /login"))
	}
	a := accts[i]
	if slot > 0 && slot != a.Slot {
		if i := slices.IndexFunc(accts, func(x account.Account) bool { return x.Slot == slot }); i >= 0 && !yes &&
			!c.ask(fmt.Sprintf("Slot %d holds %s: move it to slot %d?", slot, accts[i].Email, a.Slot), false) {
			return fmt.Errorf("%s stays in slot %d", a.Email, a.Slot)
		}
		if err := c.b.Move(a.Slot, slot); err != nil {
			return err
		}
		a.Slot = slot
	}
	if alias != "" {
		if err := c.b.SetAlias(a.Key(), alias); err != nil {
			return err
		}
	}
	fmt.Fprintf(c.out, "Added %s in slot %d.\n", a.Email, a.Slot)
	return nil
}

// tune is cswap disable, enable, threshold and priority: one account's
// part in auto-switch, or, with no account, every account's.
func (c *cmd) tune(verb string, rest []string, unset bool) error {
	accts, _ := c.b.Blocks()
	added := slices.DeleteFunc(slices.Clone(accts), func(a account.Account) bool { return !a.Added })
	if len(rest) == 0 && (verb == "threshold" || verb == "priority") {
		if verb == "threshold" {
			s, _ := c.b.AutoSettings()
			fmt.Fprintf(c.out, "global  %g\n", s.Line)
		}
		for _, a := range added {
			switch {
			case verb == "threshold" && a.Line != 0:
				fmt.Fprintf(c.out, "%-3s %s  %g\n", slotOf(a), a.Email, a.Line)
			case verb == "priority":
				fmt.Fprintf(c.out, "%-3s %s  %d\n", slotOf(a), a.Email, a.Priority)
			}
		}
		return nil
	}
	a, err := find(added, one(rest))
	if err != nil {
		return err
	}
	off, line, tier := a.AutoOff, a.Line, a.Priority
	value := one(rest[min(1, len(rest)):])
	switch verb {
	case "disable":
		off = true
	case "enable":
		off = false
	case "threshold":
		if unset {
			line = 0
		} else if line, err = strconv.ParseFloat(value, 64); err != nil || !(line >= 50 && line <= 99.9) {
			return errors.New("threshold takes a number from 50 to 99.9, or --unset")
		}
	case "priority":
		if unset {
			tier = 0
		} else if tier, err = strconv.Atoi(value); err != nil || tier < 0 || tier > 1000 {
			return errors.New("priority takes a whole number from 0 to 1000, or --unset")
		}
	}
	if err := c.b.SetAccountAuto(a.Key(), off, line, tier); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s: %s.\n", a.Email, map[string]string{
		"disable": "held out of auto-switch", "enable": "back in auto-switch",
		"threshold": "threshold " + map[bool]string{true: "back to the global one", false: value + "%"}[unset],
		"priority":  "priority " + map[bool]string{true: "0", false: value}[unset],
	}[verb])
	return nil
}

// pick is cswap switch with no account: Book.Pick on fresh readings.
func (c *cmd) pick(ctx context.Context, strategy string) (account.Account, error) {
	if _, err := c.read(ctx); err != nil {
		return account.Account{}, err
	}
	return c.b.Pick(strategy)
}

// autoLoop is cswap auto in the foreground: a tick every interval until
// ctx ends or, in tests, ticks run out.
func (c *cmd) autoLoop(ctx context.Context, dry, asJSON bool) error {
	s, err := c.b.AutoInEffect()
	if err != nil {
		return err
	}
	for n := 0; maxTicks == 0 || n < maxTicks; n++ {
		code, err := c.auto(ctx, dry, asJSON)
		if err != nil {
			fmt.Fprintln(c.errs, err)
		}
		accts, _ := c.b.Blocks()
		select {
		case <-ctx.Done():
			return nil
		case <-after(nextDelay(s.Every(), code == blocked, c.held && code == nothing, accts, c.b.Now(), jitter())):
		}
	}
	return nil
}

// nextDelay is how long auto waits, as claude-swap's _next_delay: the
// interval, +-10% so machines don't hit the API together; when every
// account is spent, until the earliest recovery and a minute (at least the
// interval, at most 10 minutes), 5 minutes when none is known. An idle hold
// crawls at 5 minutes (at least the interval); a normal wait is shortened to
// the account in use's next poll, as _respect_poll_plan.
func nextDelay(interval time.Duration, blocked, held bool, accts []account.Account, now time.Time, rnd float64) time.Duration {
	if held { // _next_delay: NO_ACTION in an idle hold crawls
		return max(interval, 5*time.Minute)
	}
	if !blocked {
		delay := time.Duration(float64(interval) * (0.9 + 0.2*rnd))
		// _respect_poll_plan: only ever shorter, never under the planner's 60 s URGENT_INTERVAL_S.
		if i := slices.IndexFunc(accts, func(a account.Account) bool { return a.InUse }); i >= 0 && !accts[i].NextPoll.IsZero() {
			return min(delay, max(accts[i].NextPoll.Sub(now), time.Minute))
		}
		return delay
	}
	var earliest time.Time
	for _, a := range accts {
		var usable time.Time // when it is usable again: the latest reset of its spent windows
		for _, l := range a.Limits {
			if l.Percent >= 100 && l.ResetsAt.After(usable) {
				usable = l.ResetsAt
			}
		}
		spent := slices.ContainsFunc(a.Limits, func(l account.Limit) bool { return l.Percent >= 100 })
		switch {
		case !spent:
		case !usable.After(now): // its recovery can't be proven: don't oversleep
			return max(interval, 5*time.Minute)
		case earliest.IsZero() || usable.Before(earliest):
			earliest = usable
		}
	}
	if earliest.IsZero() {
		return max(interval, 5*time.Minute)
	}
	return min(max(earliest.Add(time.Minute).Sub(now), interval), 10*time.Minute)
}

// jitter is the random part of the wait; tests fix it.
var jitter = rand.Float64

// maxTicks and after let tests run autoLoop a few ticks without waiting.
var (
	maxTicks = 0
	after    = time.After
)

// autoFlags applies cswap auto's flags over the saved settings for this
// run.
func (c *cmd) autoFlags(threshold, weekly float64, model, strategy string, interval, cooldown float64, keys bool) error {
	if math.IsNaN(threshold) || math.IsNaN(weekly) || math.IsNaN(interval) || math.IsNaN(cooldown) { // flag takes "nan"
		return errors.New("--threshold, --weekly-threshold, --interval and --cooldown take numbers.")
	}
	s, err := c.b.AutoSettings()
	if err != nil {
		return err
	}
	if threshold != 0 {
		s.Line = threshold
	}
	if weekly != 0 {
		s.WeeklyLine = weekly
	}
	if model != "" {
		s.Model = model
	}
	if strategy != "" {
		if !slices.Contains([]string{account.Best, account.WeeklyFirst, account.ConsumeFirst, account.ByPriority}, strategy) {
			return fmt.Errorf("--strategy is %s, %s, %s or %s", account.Best, account.WeeklyFirst, account.ConsumeFirst, account.ByPriority)
		}
		s.Strategy = strategy
	}
	if interval != 0 {
		s.Interval = int(interval)
	}
	if cooldown >= 0 {
		s.Cooldown = int(cooldown)
	}
	if keys {
		s.Keys = true
	}
	return c.b.Override(s)
}

// tokenStatus is cswap list --token-status: where each account's login is
// and when it lapses.
func (c *cmd) tokenStatus(accts []account.Account) {
	for _, a := range accts {
		src := "saved by the app"
		switch {
		case a.InUse:
			src = "Claude Code's, in use"
		case a.Terminal.Dir != "":
			src = "in a terminal's profile"
		case a.APIKey:
			src = "an API key: no login"
		case a.Token:
			src = "a setup-token: no renewal"
		}
		line := fmt.Sprintf("%-3s %s: %s", slotOf(a), a.Email, src)
		if a.Added {
			if acc, ref, err := c.b.LoginTimes(a.Key()); err == nil {
				if !acc.IsZero() {
					line += "; access token until " + acc.Local().Format("2006-01-02 15:04")
				}
				if !ref.IsZero() {
					line += "; renewable until " + ref.Local().Format("2006-01-02")
				}
			}
		}
		if a.Err != "" {
			line += "; " + a.Err
		}
		fmt.Fprintln(c.out, line)
	}
}

// stdio stands "-" for stdin or stdout with a temporary file.
func stdioFile(in io.Reader) (string, func(), error) {
	f, err := os.CreateTemp("", "claude-manager-*.json")
	if err != nil {
		return "", nil, err
	}
	done := func() { os.Remove(f.Name()) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		done()
		return "", nil, err
	}
	if in != nil {
		if _, err := io.Copy(f, in); err != nil {
			f.Close()
			done()
			return "", nil, err
		}
	}
	return f.Name(), done, f.Close()
}

// The login start of the app, as cswap menubar's LaunchAgent: a plist in
// ~/Library/LaunchAgents on macOS, an autostart entry on Linux, a Run key
// on Windows. Tests point these at temporary folders and a fake runner.
var (
	label     = "com.lang315.claudemanager"
	agentsDir = func() string { h, _ := os.UserHomeDir(); return filepath.Join(h, "Library", "LaunchAgents") }
	autostart = func() string { d, _ := os.UserConfigDir(); return filepath.Join(d, "autostart") }
	system    = func(name string, args ...string) error { return exec.Command(name, args...).Run() }
	goos      = runtime.GOOS
	exe       = os.Executable
)

func (c *cmd) menubar(status, install, uninstall, on, off bool) error {
	bin, err := exe()
	if err != nil {
		return err
	}
	if !(on || off || install || uninstall || status) {
		return errors.New("menubar takes --install-service, --uninstall-service, --service-status, --auto-switch or --no-auto-switch; with none, or --foreground, it opens the app")
	}
	if on || off { // with --install-service too, as cswap's
		if err := c.b.Set("autoswitch.enabled", strconv.FormatBool(on)); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Auto-switch %s.\n", map[bool]string{true: "on", false: "off"}[on])
	}
	switch {
	case install:
		if err := installService(bin, c.b.Dir); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Claude Manager starts at login, in the menu bar.")
	case uninstall:
		if err := uninstallService(); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Claude Manager no longer starts at login.")
	case status:
		s, _ := c.b.AutoSettings()
		fmt.Fprintf(c.out, "starts at login: %v\nauto-switch: %v\n", serviceInstalled(), s.On)
		if goos == "darwin" && serviceInstalled() { // cswap's status: where, and whether it runs
			fmt.Fprintf(c.out, "plist: %s\n", plistPath())
			out := launchctlPrint()
			if m := regexp.MustCompile(`(?m)^\s*pid = (\d+)`).FindStringSubmatch(out); m != nil {
				fmt.Fprintf(c.out, "running: pid %s\n", m[1])
			} else {
				fmt.Fprintln(c.out, "running: no")
			}
		}
		if serviceInstalled() && serviceDisabled() {
			fmt.Fprintln(c.out, "macOS won't start it at login: turn Claude Manager back on in System Settings, General, Login Items & Extensions.")
		}
	}
	return nil
}

// serviceEnv are the variables a login-started app would lack that the
// shell installing it has: proxies and certificates for Anthropic, and
// claude's folder (cswap's launch_agent forwards them).
var serviceEnv = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "CLAUDE_CONFIG_DIR", "PATH"}

func plistPath() string { return filepath.Join(agentsDir(), label+".plist") }

func desktopPath() string { return filepath.Join(autostart(), "claude-manager.desktop") }

func installService(bin, dir string) error {
	switch goos {
	case "darwin":
		env := ""
		for _, k := range serviceEnv {
			if v := os.Getenv(k); v != "" {
				env += "\t\t<key>" + k + "</key><string>" + xmlEscape(v) + "</string>\n"
			}
		}
		if env != "" {
			env = "\t<key>EnvironmentVariables</key>\n\t<dict>\n" + env + "\t</dict>\n"
		}
		out, errs := xmlEscape(filepath.Join(dir, "menubar.log")), xmlEscape(filepath.Join(dir, "menubar.err")) // cswap: <label>.log and <label>.err
		// KeepAlive: started again after a crash, not after a quit (cswap's).
		plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + label + `</string>
	<key>ProgramArguments</key><array><string>` + xmlEscape(bin) + `</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>` + out + `</string>
	<key>StandardErrorPath</key><string>` + errs + `</string>
` + env + `</dict>
</plist>
`
		if err := os.MkdirAll(agentsDir(), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(plistPath(), []byte(plist), 0o644); err != nil {
			return err
		}
		_ = system("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
		return system("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plistPath())
	case "linux":
		if err := os.MkdirAll(autostart(), 0o755); err != nil {
			return err
		}
		return os.WriteFile(desktopPath(), []byte("[Desktop Entry]\nType=Application\nName=Claude Manager\nExec="+shellArg(bin)+"\nX-GNOME-Autostart-enabled=true\n"), 0o644)
	case "windows":
		return system("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ClaudeManager", "/t", "REG_SZ", "/d", `"`+bin+`"`, "/f")
	}
	return fmt.Errorf("No login start on %s.", goos)
}

func uninstallService() error {
	switch goos {
	case "darwin":
		_ = system("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
		return rmIfThere(plistPath())
	case "linux":
		return rmIfThere(desktopPath())
	case "windows":
		return system("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ClaudeManager", "/f")
	}
	return fmt.Errorf("No login start on %s.", goos)
}

func serviceInstalled() bool {
	switch goos {
	case "darwin":
		_, err := os.Stat(plistPath())
		return err == nil
	case "linux":
		_, err := os.Stat(desktopPath())
		return err == nil
	case "windows":
		return system("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ClaudeManager") == nil
	}
	return false
}

func rmIfThere(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func shellArg(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// GUI reports whether args open the app rather than run a command: tui,
// watch, and menubar alone or with --foreground, as cswap's.
func GUI(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "tui", "watch":
		return true
	case "menubar":
		return len(args) == 1 || slices.Contains(args[1:], "--foreground")
	}
	return false
}

// serviceDisabled reports whether macOS was told not to start the
// LaunchAgent (System Settings, Login Items), as cswap's launch_agent
// checks with launchctl print-disabled.
func serviceDisabled() bool {
	if goos != "darwin" {
		return false
	}
	out, err := output("launchctl", "print-disabled", fmt.Sprintf("gui/%d", os.Getuid()))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"`+label+`"`) && (strings.HasSuffix(line, "disabled") || strings.HasSuffix(line, "true")) {
			return true
		}
	}
	return false
}

var output = func(name string, args ...string) (string, error) {
	b, err := exec.Command(name, args...).Output()
	return string(b), err
}

// launchctlPrint is launchctl's account of the menu bar service.
var launchctlPrint = func() string {
	out, _ := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Output()
	return string(out)
}
