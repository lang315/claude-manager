package account

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	errDrifted         = errors.New("The terminal opened as this account is signed in to another one now; its login will be kept as unclaimed when claude quits there.")
)

// InTerminalExpired is the status of an account open in a terminal whose
// login has lapsed there: that terminal's claude renews it as it runs.
const InTerminalExpired = "Its sign-in in the terminal has lapsed; claude there renews it as it runs."

// launchGrace is how long after opening a terminal the app waits for its
// claude to show up before it takes the account back.
const launchGrace = 2 * time.Minute

// setup and history are what a profile takes from ~/.claude by link, as
// claude-swap's session mode: setup unless --no-share, history unless
// --no-share-history. sessions, plugins, ide and the logins stay the
// profile's own.
var (
	setup   = []string{"settings.json", "keybindings.json", "CLAUDE.md", "skills", "commands", "agents", "rules"}
	history = []string{"projects", "history.jsonl"}
)

// Share is what a profile shares with ~/.claude: its setup (settings,
// CLAUDE.md, skills…, and the user's MCP servers) and its chat history.
type Share struct{ Setup, History bool }

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
//
// share applies when the profile is seeded; opened again while claude may
// run there, the profile is left as it is.
func (b *Book) Run(key, folder string, share Share, args ...string) error {
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
		return b.Launch(folder, a.Terminal.Dir, args...)
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
	if err := b.seed(dir, parked, a.OAuthAccount, share); err != nil {
		return errors.Join(err, b.clearProfile(dir))
	}
	return b.Launch(folder, dir, args...)
}

func (b *Book) seed(dir string, parked []byte, acct json.RawMessage, share Share) error {
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
	if mine, err := os.ReadFile(h.ClaudeJSON); err == nil && share.Setup {
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
	busy := false
	if b.Running != nil {
		running, err := b.Running(dir)
		busy = running || err != nil
	}
	if share.History && !busy {
		// Its own history first goes into ~/.claude's, as claude-swap's;
		// never under a claude still writing it.
		for _, name := range history {
			if _, _, err := mergeHistory(filepath.Join(h.Root, name), filepath.Join(dir, name), filepath.Join(b.Dir, "history-conflicts")); err != nil {
				return err
			}
		}
	}
	for _, g := range []struct {
		names []string
		on    bool
	}{{setup, share.Setup}, {history, share.History}} {
		for _, name := range g.names {
			if err := link(filepath.Join(h.Root, name), filepath.Join(dir, name), g.on); err != nil {
				return err
			}
		}
	}
	return nil
}

// link makes dest a link to src, or, when !on, removes such a link. Only
// links to src are touched: the profile's own file or folder of that name,
// and src itself, never are.
func link(src, dest string, on bool) error {
	to, lerr := os.Readlink(dest)
	ours := lerr == nil && to == src
	switch {
	case !on && ours:
		return os.Remove(dest)
	case !on:
		return nil
	}
	if _, err := os.Lstat(dest); err == nil {
		return nil // a link made before, or the profile's own
	}
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	return os.Symlink(src, dest)
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
	if err == nil && b.drifted(*a) {
		// A /login in that terminal put another account there: its login
		// is not this account's to save. It is kept as unclaimed instead.
		if err := b.stashLocked(blob, "signed in to another account in a terminal opened as "+a.Email); err != nil {
			return err
		}
	} else if err == nil {
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

// drifted reports whether a's profile is signed in to another account,
// as claude-swap's session_identity_drifted: its .claude.json names
// another account, or its login's own uuids do. Unreadable is no drift.
func (b *Book) drifted(a Account) bool {
	if a.Terminal.Dir == "" {
		return false
	}
	conf, _ := os.ReadFile(filepath.Join(a.Terminal.Dir, ".claude.json"))
	if k := keyOf(oauthAccountOf(conf)); k != "" && k != a.Key() {
		return true
	}
	blob, err := b.profileLogin(a.Terminal.Dir)
	k := keyOf(oauthOf(blob))
	return err == nil && k != "" && k != a.Key()
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
		if pidAlive(r.PID) {
			return true, nil
		}
	}
	return false, nil
}

// RunCommand is the shell command LaunchInTerminal runs: claude with args.
func RunCommand(folder, configDir string, args ...string) string {
	cmd := "cd " + shellQuote(folder) + " && CLAUDE_CONFIG_DIR=" + shellQuote(configDir) + " exec claude"
	for _, a := range args {
		cmd += " " + shellQuote(a)
	}
	return cmd
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
