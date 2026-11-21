package account

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Terminal marks an added account open in a terminal: Claude Code runs it
// from its own profile folder, and renews its login there.
type Terminal struct {
	Dir   string    `json:"dir"`   // the profile: its CLAUDE_CONFIG_DIR
	Since time.Time `json:"since"` // when the app opened it
	Seen  bool      `json:"seen"`  // a claude process has been seen on it
}

var (
	errInTerminal = errors.New("This account is open in a terminal. Quit Claude Code there first.")
	errRunInUse   = errors.New("This account is the one in use: run claude in any terminal.")

	errTerminalExpired = errors.New(InTerminalExpired)
)

// InTerminalExpired is the status of an account open in a terminal whose
// login has lapsed there: that terminal's claude renews it as it runs.
const InTerminalExpired = "Its sign-in in the terminal has lapsed; claude there renews it as it runs."

// launchGrace is how long after opening a terminal the app waits for its
// claude to show up before it takes the account back.
const launchGrace = 2 * time.Minute

// shared are what a profile takes from ~/.claude by link, as claude-swap's
// session mode with history shared. sessions, plugins, ide and the logins
// stay the profile's own.
var shared = []string{"settings.json", "keybindings.json", "CLAUDE.md", "skills", "commands", "agents", "rules", "projects", "history.jsonl"}

// ProfilesDir is where the profiles live, under dir, the app's folder.
func ProfilesDir(dir string) string { return filepath.Join(dir, "profiles") }

func (b *Book) profile(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(ProfilesDir(b.Dir), hex.EncodeToString(sum[:6]))
}

// Run opens Claude Code in a terminal at folder, as the added account key,
// from that account's own profile: the rest of the Mac stays on the
// account in use. From then on that terminal renews the account's login;
// the app takes it back once claude has quit there.
func (b *Book) Run(key, folder string) error {
	h := b.Home
	if h == nil {
		return ErrCantSwitch
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	i := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == key })
	if i < 0 {
		return errNotAdded
	}
	a := &b.added[i]
	if key == h.accountKey() {
		return errRunInUse
	}
	if a.Terminal.Dir != "" {
		// The profile holds the login now: open it again, with take-back
		// waiting for this claude as for the first.
		a.Terminal.Since, a.Terminal.Seen = b.Now(), false
		if err := b.save(); err != nil {
			return err
		}
		return b.Launch(folder, a.Terminal.Dir)
	}
	if a.OAuthAccount == nil {
		return errNoAccount
	}
	parked, err := b.Vault.Get(ParkService, key)
	if errors.Is(err, ErrNotFound) {
		return ErrInvalidGrant
	}
	if err != nil {
		return err
	}
	pl, ok := complete(parked)
	if !ok {
		return ErrInvalidGrant
	}
	// Claude Code's own login, whatever ~/.claude.json says: a seed of it
	// would be a second renewer on Claude Code's chain.
	if lb, err := h.readLive(b.Vault); err == nil {
		if l, err := parseLogin(lb.data); keyOf(oauthOf(lb.data)) == key || err == nil && l.RefreshToken == pl.RefreshToken {
			return errRunInUse
		}
	}
	dir := b.profile(key)
	// Marked before the seed: a seed without its mark would be a second
	// renewer the app doesn't know of.
	a.Terminal = Terminal{Dir: dir, Since: b.Now()}
	if err := b.save(); err != nil {
		a.Terminal = Terminal{}
		return err
	}
	if err := b.seed(dir, parked, a.OAuthAccount); err != nil {
		return errors.Join(err, b.clearProfile(dir))
	}
	return b.Launch(folder, dir)
}

func (b *Book) seed(dir string, parked []byte, acct json.RawMessage) error {
	h := b.Home
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Claude Code reads its keychain item before the file: an old one would
	// shadow the seed.
	if err := b.Vault.Delete(liveService(dir), h.User); err != nil {
		return err
	}
	creds, err := withOAuth(nil, oauthOf(parked))
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(dir, ".credentials.json"), creds, 0o600); err != nil {
		return err
	}
	conf := []byte(`{"hasCompletedOnboarding":true}`)
	if old, err := os.ReadFile(filepath.Join(dir, ".claude.json")); err == nil {
		conf = old
	}
	conf, err = spliceKey(conf, "oauthAccount", acct)
	if err != nil {
		return err
	}
	if mine, err := os.ReadFile(h.ClaudeJSON); err == nil {
		var m map[string]json.RawMessage
		if json.Unmarshal(mine, &m) == nil && len(m["mcpServers"]) > 0 {
			if conf, err = spliceKey(conf, "mcpServers", m["mcpServers"]); err != nil {
				return err
			}
		}
	}
	if err := writeAtomic(filepath.Join(dir, ".claude.json"), conf, 0o600); err != nil {
		return err
	}
	for _, name := range shared {
		src, dest := filepath.Join(h.Root, name), filepath.Join(dir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if _, err := os.Lstat(dest); err == nil {
			continue // a link made before, or the profile's own: never replaced
		}
		if err := os.Symlink(src, dest); err != nil {
			return err
		}
	}
	return nil
}

// clearProfile removes a profile's logins, so a claude started there
// later, as from shell history, can't renew a login the app holds.
func (b *Book) clearProfile(dir string) error {
	err := os.Remove(filepath.Join(dir, ".credentials.json"))
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return errors.Join(err, b.Vault.Delete(liveService(dir), b.user()))
}

// user is the keychain account Claude Code files its logins under; "" for
// any, outside the default folder.
func (b *Book) user() string {
	if b.Home == nil {
		return ""
	}
	return b.Home.User
}

// profileLogin reads the login Claude Code keeps for a profile.
func (b *Book) profileLogin(dir string) ([]byte, error) {
	blob, err := b.Vault.Get(liveService(dir), b.user())
	if errors.Is(err, ErrNotFound) {
		blob, err = os.ReadFile(filepath.Join(dir, ".credentials.json"))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotSignedIn
		}
	}
	return blob, err
}

// takeBack ends a's terminal once no claude runs on its profile: a login
// the profile renewed is saved first, and read back, then the profile's
// logins are removed. A failed process scan keeps the mark.
func (b *Book) takeBack(a *Account, now time.Time) error {
	t := &a.Terminal
	running, err := b.Running(t.Dir)
	if err != nil {
		return nil
	}
	if running {
		t.Seen = true
		return nil
	}
	if !t.Seen && now.Sub(t.Since) < launchGrace {
		return nil
	}
	blob, err := b.profileLogin(t.Dir)
	if err != nil && !errors.Is(err, ErrNotSignedIn) {
		return err
	}
	if err == nil {
		parked, _ := b.Vault.Get(ParkService, a.Key())
		if _, ok := complete(blob); ok && newer(blob, parked) {
			if err := b.park(a.Key(), blob); err != nil {
				return err
			}
			saved, err := b.Vault.Get(ParkService, a.Key())
			if err != nil || !bytes.Equal(oauthOf(saved), oauthOf(blob)) {
				return errors.Join(err, errors.New("The login renewed in the terminal reads back different; it stays there."))
			}
			if l, err := parseLogin(blob); err == nil && l.RefreshTokenExpiresAt > 0 {
				a.GoodUntil = time.UnixMilli(l.RefreshTokenExpiresAt)
			}
		}
	}
	if err := b.clearProfile(t.Dir); err != nil {
		return err
	}
	a.Terminal = Terminal{}
	return nil
}

// ClaudeRunning reports whether a claude runs on configDir, from the
// sessions/<pid>.json records Claude Code keeps there, as claude-swap's
// process_detection: a folder or record it can't read is an error, so the
// caller keeps its guard; a missing folder is no session. A reused pid
// counts as running.
func ClaudeRunning(configDir string) (bool, error) {
	es, err := os.ReadDir(filepath.Join(configDir, "sessions"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range es {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(configDir, "sessions", e.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var r struct {
			PID int `json:"pid"`
		}
		if err != nil || json.Unmarshal(data, &r) != nil {
			return false, errors.Join(err, errors.New("a session record can't be read"))
		}
		if r.PID > 0 && (syscall.Kill(r.PID, 0) == nil || errors.Is(syscall.Kill(r.PID, 0), syscall.EPERM)) {
			return true, nil
		}
	}
	return false, nil
}

// LaunchInTerminal opens Terminal at folder running claude with configDir
// as its CLAUDE_CONFIG_DIR. The command goes to AppleScript as an
// argument, so only the shell's quoting applies.
func LaunchInTerminal(folder, configDir string) error {
	return exec.CommandContext(context.Background(), "/usr/bin/osascript",
		"-e", "on run argv", "-e", `tell application "Terminal"`, "-e", "activate",
		"-e", "do script (item 1 of argv)", "-e", "end tell", "-e", "end run",
		RunCommand(folder, configDir)).Run()
}

// RunCommand is the shell command LaunchInTerminal runs.
func RunCommand(folder, configDir string) string {
	return "cd " + shellQuote(folder) + " && CLAUDE_CONFIG_DIR=" + shellQuote(configDir) + " exec claude"
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
