package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lang315/claude-manager/account"
	"github.com/lang315/claude-manager/store"
)

func TestAddAndTune(t *testing.T) {
	a := newApp(t)
	if code, out, errs := a.run(t, "", "add", "--slot", "5", "--alias", "me"); code != ok || !strings.Contains(out, "Added alice@example.com in slot 5") {
		t.Fatalf("add: %d %s %s", code, out, errs)
	}
	accts, _ := a.b.Blocks()
	i := slices.IndexFunc(accts, func(x account.Account) bool { return x.Email == "alice@example.com" })
	if accts[i].Slot != 5 || accts[i].Alias != "me" {
		t.Errorf("alice %+v", accts[i])
	}
	for _, c := range [][]string{{"disable", "bob@example.com"}, {"threshold", "bob@example.com", "85"}, {"priority", "bob@example.com", "7"}} {
		if code, _, errs := a.run(t, "", c...); code != ok {
			t.Fatalf("%v: %s", c, errs)
		}
	}
	accts, _ = a.b.Blocks()
	i = slices.IndexFunc(accts, func(x account.Account) bool { return x.Email == "bob@example.com" })
	if b := accts[i]; !b.AutoOff || b.Line != 85 || b.Priority != 7 {
		t.Errorf("bob %+v", b)
	}
	if _, out, _ := a.run(t, "", "threshold"); !strings.Contains(out, "global  90") || !strings.Contains(out, "bob@example.com  85") {
		t.Errorf("threshold list %q", out)
	}
	if _, out, _ := a.run(t, "", "priority"); !strings.Contains(out, "bob@example.com  7") {
		t.Errorf("priority list %q", out)
	}
	for _, c := range [][]string{{"enable", "bob@example.com"}, {"threshold", "bob@example.com", "--unset"}, {"priority", "bob@example.com", "--unset"}} {
		a.run(t, "", c...)
	}
	accts, _ = a.b.Blocks()
	if b := accts[i]; b.AutoOff || b.Line != 0 || b.Priority != 0 {
		t.Errorf("bob after %+v", b)
	}
	for _, bad := range [][]string{{"threshold", "bob@example.com", "40"}, {"priority", "bob@example.com", "2000"}} {
		if code, _, _ := a.run(t, "", bad...); code != failed {
			t.Errorf("%v taken", bad)
		}
	}
}

func TestSwitchWithoutAccount(t *testing.T) {
	a := newApp(t)
	must(t, os.WriteFile(filepath.Join(a.b.Dir, "accounts.json"), []byte(`{"accounts":[`+
		`{"accountUuid":"bob","orgUuid":"org","email":"bob@example.com","slot":1,"oauthAccount":{"accountUuid":"bob","organizationUuid":"org"}},`+
		`{"accountUuid":"carol","orgUuid":"org","email":"carol@example.com","slot":2,"oauthAccount":{"accountUuid":"carol","organizationUuid":"org"}}]}`), 0o600))
	must(t, a.v.Put(account.ParkService, "carol/org", login("carol")))
	a.usage["bob"], a.usage["carol"] = 100, 20
	c := &cmd{b: a.b, out: new(strings.Builder), errs: new(strings.Builder)}
	for strategy, want := range map[string]string{"": "bob@example.com", "rotate": "bob@example.com", "next-available": "carol@example.com", "best": "carol@example.com"} {
		got, err := c.pick(context.Background(), strategy)
		if err != nil || got.Email != want {
			t.Errorf("%q: %s %v", strategy, got.Email, err)
		}
	}
	must(t, a.b.SetAccountAuto("bob/org", true, 0, 0)) // held out: bare switch skips it, as cswap's
	if got, _ := c.pick(context.Background(), ""); got.Email != "carol@example.com" {
		t.Errorf("held out picked: %s", got.Email)
	}
	must(t, a.b.SetAccountAuto("bob/org", false, 0, 0))
	if _, err := c.pick(context.Background(), "worst"); err == nil {
		t.Error("worst taken")
	}
	if code, out, _ := a.run(t, "", "switch"); code != ok || !strings.Contains(out, "bob@example.com") {
		t.Errorf("rotate: %d %s", code, out)
	}
	// From bob (slot 1) rotation goes on to carol.
	if code, out, _ := a.run(t, "", "switch"); code != ok || !strings.Contains(out, "carol@example.com") {
		t.Errorf("rotate on: %d %s", code, out)
	}
}

func TestAutoLoopAndFlags(t *testing.T) {
	a := newApp(t)
	maxTicks = 2
	var waited []time.Duration
	after = func(d time.Duration) <-chan time.Time {
		waited = append(waited, d)
		c := make(chan time.Time, 1)
		c <- time.Time{}
		return c
	}
	t.Cleanup(func() { maxTicks, after = 0, time.After })
	code, out, errs := a.run(t, "", "auto", "--threshold", "40", "--strategy", "best", "--interval", "30")
	if code != ok || !strings.Contains(out, "Switched to bob@example.com") {
		t.Fatalf("auto: %d %q %s", code, out, errs)
	}
	if len(waited) == 0 || waited[0] != 30*time.Second {
		t.Errorf("waited %v, want --interval's 30s", waited)
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("ticks: %q", out)
	}
	again := &account.Book{Dir: a.b.Dir, Vault: a.v, Now: a.b.Now}
	if s, _ := again.AutoSettings(); s.Line != 90 || s.Interval != 0 {
		t.Errorf("the run's flags were saved: %+v", s)
	}
	if code, _, _ := a.run(t, "", "auto", "--once", "--strategy", "worst"); code != failed {
		t.Error("worst strategy taken")
	}
	for _, f := range []string{"--threshold", "--weekly-threshold", "--interval"} {
		if code, out, _ := a.run(t, "", "auto", "--once", f, "nan"); code != failed || strings.Contains(out, "Switched") {
			t.Errorf("%s nan: %d %s", f, code, out)
		}
	}
}

func TestTokenStatusRunArgsStdio(t *testing.T) {
	a := newApp(t)
	if _, out, _ := a.run(t, "", "list", "--token-status"); !strings.Contains(out, "alice@example.com: Claude Code's, in use") || !strings.Contains(out, "bob@example.com: saved by the app; access token until") {
		t.Errorf("token status %q", out)
	}
	code, out, _ := a.run(t, "", "export", "-")
	if code != ok || !strings.Contains(out, `"version": 1`) {
		t.Fatalf("export -: %d %s", code, out)
	}
	var got []string
	a.b.Launch = func(f, p string, args ...string) error { got = args; return nil }
	a.b.Running = func(string) (bool, error) { return false, nil }
	if code, _, errs := a.run(t, "", "run", "bob@example.com", t.TempDir(), "--", "--resume", "x y"); code != ok || !slices.Equal(got, []string{"--resume", "x y"}) {
		t.Errorf("run args: %d %s %q", code, errs, got)
	}
	if !strings.HasSuffix(account.RunCommand("/f", "/p", "--resume", "x y"), ` claude '--resume' 'x y'`) {
		t.Error("RunCommand args")
	}
	b := newApp(t)
	must(t, b.b.Remove("bob/org"))
	if code, res, errs := b.run(t, out, "import", "-"); code != ok || !strings.Contains(res, "Added 1") {
		t.Errorf("import -: %d %s %s", code, res, errs)
	}
}

func TestMenubarService(t *testing.T) {
	a := newApp(t)
	dir := t.TempDir()
	var calls []string
	agentsDir = func() string { return filepath.Join(dir, "LaunchAgents") }
	autostart = func() string { return filepath.Join(dir, "autostart") }
	system = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	exe = func() (string, error) { return "/Applications/Claude Manager.app/Contents/MacOS/claude-manager", nil }
	t.Cleanup(func() {
		agentsDir = func() string { h, _ := os.UserHomeDir(); return filepath.Join(h, "Library", "LaunchAgents") }
		autostart = func() string { d, _ := os.UserConfigDir(); return filepath.Join(d, "autostart") }
		system = func(name string, args ...string) error { return nil }
		exe = os.Executable
		goos = "darwin"
	})
	goos = "darwin"
	t.Setenv("HTTPS_PROXY", "http://proxy:8080")
	// cswap's --install-service --auto-switch does both.
	if code, _, errs := a.run(t, "", "menubar", "--install-service", "--auto-switch"); code != ok {
		t.Fatal(errs)
	}
	if s, _ := a.b.AutoSettings(); !s.On {
		t.Error("--install-service --auto-switch didn't turn auto-switch on")
	}
	plist, _ := os.ReadFile(filepath.Join(dir, "LaunchAgents", label+".plist"))
	for _, want := range []string{"<string>/Applications/Claude Manager.app/Contents/MacOS/claude-manager</string>", "<key>RunAtLoad</key><true/>",
		"<key>SuccessfulExit</key><false/>", "<key>StandardErrorPath</key>", "<key>HTTPS_PROXY</key><string>http://proxy:8080</string>"} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("no %s in plist %s", want, plist)
		}
	}
	if !slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, "launchctl bootstrap gui/") }) {
		t.Errorf("calls %q", calls)
	}
	launchctlPrint = func() string { return "state = running\n\tpid = 4242\n" }
	if _, out, _ := a.run(t, "", "menubar", "--service-status"); !strings.Contains(out, "starts at login: true") || !strings.Contains(out, "auto-switch: true") ||
		!strings.Contains(out, "running: pid 4242") || !strings.Contains(out, label+".plist") {
		t.Errorf("status %q", out)
	}
	a.run(t, "", "menubar", "--auto-switch")
	if s, _ := a.b.AutoSettings(); !s.On {
		t.Error("auto-switch not on")
	}
	a.run(t, "", "menubar", "--uninstall-service")
	if _, err := os.Stat(filepath.Join(dir, "LaunchAgents", label+".plist")); err == nil {
		t.Error("plist left")
	}
	goos = "linux"
	a.run(t, "", "menubar", "--install-service")
	if d, _ := os.ReadFile(filepath.Join(dir, "autostart", "claude-manager.desktop")); !strings.Contains(string(d), `Exec="/Applications/Claude Manager.app/Contents/MacOS/claude-manager"`) {
		t.Errorf("desktop %s", d)
	}
	goos, calls = "windows", nil
	a.run(t, "", "menubar", "--install-service")
	if len(calls) != 1 || !strings.HasPrefix(calls[0], `reg add HKCU\Software\Microsoft\Windows\CurrentVersion\Run`) {
		t.Errorf("windows %q", calls)
	}
	if code, _, _ := a.run(t, "", "menubar", "--bogus"); code != failed {
		t.Error("bad flag")
	}
}

func TestGUI(t *testing.T) {
	for args, want := range map[string]bool{"": true, "tui": true, "watch": true, "menubar": true, "menubar --foreground": true,
		"menubar --service-status": false, "list": false} {
		if got := GUI(strings.Fields(args)); got != want {
			t.Errorf("%q: %v", args, got)
		}
	}
}

// cswap export --account, import --force, switch --force.
func TestForceAndOneAccount(t *testing.T) {
	a := newApp(t)
	a.run(t, "", "list") // alice is followed
	path := filepath.Join(t.TempDir(), "one.json")
	if code, out, errs := a.run(t, "", "export", path, "--account", "bob@example.com"); code != ok || !strings.Contains(out, "Exported 1 accounts") {
		t.Fatalf("export --account: %d %s %s", code, out, errs)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "alice") {
		t.Error("exported alice too")
	}

	// A login for alice, the account in use, from another Mac.
	b := newApp(t)
	b.run(t, "", "list")
	other := newApp(t)
	other.run(t, "", "list")
	must(t, other.v.Put(account.ParkService, "alice/org", login("alice2")))
	file := filepath.Join(t.TempDir(), "alice.json")
	other.run(t, "", "export", file, "--account", "alice@example.com")
	if _, out, _ := b.run(t, "", "import", file); !strings.Contains(out, "left 1") {
		t.Errorf("import without --force: %s", out)
	}
	if _, out, _ := b.run(t, "", "import", file, "--force"); !strings.Contains(out, "updated 1") {
		t.Errorf("import --force: %s", out)
	}
	if code, _, _ := b.run(t, "", "switch", "alice@example.com"); code != ok {
		t.Fatal("plain switch to the account in use")
	}
	if live, _ := b.v.Get("Claude Code-credentials", "me"); strings.Contains(string(live), "alice2") {
		t.Error("a plain switch to the account in use put the import in use")
	}
	if code, _, errs := b.run(t, "", "switch", "alice@example.com", "--force"); code != ok {
		t.Fatal(errs)
	}
	if live, _ := b.v.Get("Claude Code-credentials", "me"); !strings.Contains(string(live), "alice2-at") {
		t.Errorf("switch --force: live %s", live)
	}
	if saved, _ := b.v.Get(account.ParkService, "alice/org"); !strings.Contains(string(saved), "alice2-at") {
		t.Errorf("the import was overwritten by the old login: %s", saved)
	}
}

func TestObserve(t *testing.T) {
	a := newApp(t)
	a.run(t, "", "switch", "bob@example.com")
	if _, out, _ := a.run(t, "", "history"); !strings.Contains(out, "alice@example.com -> bob@example.com  by hand\n") {
		t.Errorf("history %q", out)
	}
	Instances = func() []store.Instance {
		return []store.Instance{{Kind: "VS Code", Folder: "/w"}, {Kind: "CLI", Folder: "/x", Profile: "/p"}}
	}
	t.Cleanup(func() { Instances = nil })
	if _, out, _ := a.run(t, "", "status"); !strings.Contains(out, "Running instances:") || !strings.Contains(out, "VS Code  /w") || !strings.Contains(out, "/x (a terminal's profile)") {
		t.Errorf("status %q", out)
	}
	// A failing account keeps its last reading, display-grade.
	b := newApp(t)
	b.run(t, "", "list")
	delete(b.usage, "bob")
	b.b.Now = func() time.Time { return t0.Add(2 * time.Hour) }
	_, out, _ := b.run(t, "", "list", "--json")
	if !strings.Contains(out, `"lastGoodUsage":{"fiveHour":{"pct":10`) || !strings.Contains(out, `"lastGoodAgeSeconds":7200`) || !strings.Contains(out, `"usageRetryAt"`) {
		t.Errorf("lastGood %s", out)
	}
}

func TestServiceDisabled(t *testing.T) {
	goos = "darwin"
	output = func(string, ...string) (string, error) {
		return "disabled services = {\n\t\"" + label + "\" => disabled\n\t\"other\" => enabled\n}", nil
	}
	t.Cleanup(func() { output = func(string, ...string) (string, error) { return "", nil } })
	if !serviceDisabled() {
		t.Error("disabled missed")
	}
	output = func(string, ...string) (string, error) { return "\"" + label + "\" => enabled", nil }
	if serviceDisabled() {
		t.Error("enabled taken for disabled")
	}
}

// switch --strategy best stays, said and not failed, when nothing has
// more room than the account in use (cswap's already-best).
func TestSwitchBestStays(t *testing.T) {
	a := newApp(t)
	a.usage["alice"], a.usage["bob"] = 10, 60
	if code, out, errs := a.run(t, "", "switch", "--strategy", "best"); code != ok || !strings.Contains(out, "Already on the account with the most room") {
		t.Errorf("best: %d %q %s", code, out, errs)
	}
	if _, out, _ := a.run(t, "", "switch", "--strategy", "best", "--json"); !strings.Contains(out, `"reason":"already-best"`) || !strings.Contains(out, `"switched":false`) {
		t.Errorf("best --json: %s", out)
	}
	a = newApp(t)
	a.usage["alice"], a.usage["bob"] = 90, 60
	if code, out, _ := a.run(t, "", "switch", "--strategy", "best"); code != ok || !strings.Contains(out, "bob@example.com") {
		t.Errorf("best onto more room: %d %s", code, out)
	}
}
