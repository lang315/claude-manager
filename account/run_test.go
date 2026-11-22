package account

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// runBook is switchBook with a fake terminal: launched records each
// launch; running is what the process scan answers.
type runBook struct {
	b        *Book
	v        *Memory
	h        *Home
	f        *anthropic
	launched [][2]string
	running  bool
	scanErr  error
}

func newRunBook(t *testing.T) *runBook {
	t.Helper()
	b, v, h, f := switchBook(t)
	r := &runBook{b: b, v: v, h: h, f: f}
	b.Launch = func(folder, dir string, _ ...string) error {
		r.launched = append(r.launched, [2]string{folder, dir})
		return nil
	}
	b.Running = func(string) (bool, error) { return r.running, r.scanErr }
	return r
}

func (r *runBook) dir() string { return r.b.profile(bob.Key()) }

func (r *runBook) bob(t *testing.T) Account {
	t.Helper()
	blocks, _ := r.b.Blocks()
	return blockOf(t, blocks, "bob@example.com")
}

func TestRunSeedsProfile(t *testing.T) {
	r := newRunBook(t)
	must(t, os.WriteFile(filepath.Join(r.h.Root, "settings.json"), []byte(`{}`), 0o600))
	must(t, os.Mkdir(filepath.Join(r.h.Root, "projects"), 0o700))
	must(t, os.WriteFile(r.h.ClaudeJSON, []byte(strings.Replace(aliceConf, `"numStartups": 3,`, `"numStartups": 3, "mcpServers": {"srv": {"command": "x"}},`, 1)), 0o600))
	dir := r.dir()
	must(t, os.MkdirAll(dir, 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("its own"), 0o600))
	must(t, os.WriteFile(filepath.Join(r.h.Root, "CLAUDE.md"), []byte("mine"), 0o600))
	must(t, r.v.Put(liveService(dir), "me", []byte("stale")))

	must(t, r.b.Run(bob.Key(), "/work", both))
	if !slices.Equal(r.launched, [][2]string{{"/work", dir}}) {
		t.Fatalf("launched %q", r.launched)
	}
	if strings.Contains(dir, "bob") {
		t.Errorf("profile folder %s carries the email", dir)
	}
	if _, err := r.v.Get(liveService(dir), "me"); !errors.Is(err, ErrNotFound) {
		t.Error("the profile's old keychain item stays, shadowing the seed")
	}
	creds, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	must(t, err)
	if l, _ := parseLogin(creds); l.RefreshToken != "bob-rt" || strings.Contains(string(creds), "mcpOAuth") {
		t.Errorf("seed %s", creds)
	}
	if fi, _ := os.Stat(filepath.Join(dir, ".credentials.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("seed mode %v", fi.Mode().Perm())
	}
	conf, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	must(t, err)
	var c struct {
		Done bool            `json:"hasCompletedOnboarding"`
		Acct json.RawMessage `json:"oauthAccount"`
		MCP  json.RawMessage `json:"mcpServers"`
	}
	must(t, json.Unmarshal(conf, &c))
	if !c.Done || keyOf(c.Acct) != bob.Key() || string(c.MCP) != `{"srv": {"command": "x"}}` {
		t.Errorf("profile .claude.json %s", conf)
	}
	for _, name := range []string{"settings.json", "projects"} {
		if to, err := os.Readlink(filepath.Join(dir, name)); err != nil || to != filepath.Join(r.h.Root, name) {
			t.Errorf("%s: %q %v", name, to, err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); string(data) != "its own" {
		t.Error("replaced the profile's own CLAUDE.md")
	}
	if _, err := os.Lstat(filepath.Join(dir, "skills")); err == nil {
		t.Error("linked skills, which ~/.claude has none of")
	}
	if a := r.bob(t); a.Terminal.Dir != dir {
		t.Errorf("bob not marked: %+v", a.Terminal)
	}
}

// The mark is saved before the seed: a seed the app doesn't know of would
// be a second renewer. A seed that fails takes it off again, else a retry
// would open the cleared profile, signed out.
func TestRunMarksBeforeSeeding(t *testing.T) {
	r := newRunBook(t)
	marked := false
	r.b.Vault = &seedSpy{r.v, func() {
		data, _ := os.ReadFile(r.b.path())
		marked = strings.Contains(string(data), `"terminal"`)
	}}
	creds := filepath.Join(r.dir(), ".claude.json")
	must(t, os.MkdirAll(creds, 0o700)) // a folder in its way: the seed can't write the profile's .claude.json
	must(t, os.WriteFile(filepath.Join(creds, "x"), nil, 0o600))
	r.b.Launch = func(string, string, ...string) error { t.Fatal("launched without a seed"); return nil }
	if err := r.b.Run(bob.Key(), "/work", both); err == nil {
		t.Fatal("ran without a seed")
	}
	if !marked {
		t.Error("the mark was not saved before seeding")
	}
	if a := r.bob(t); a.Terminal.Dir != "" {
		t.Errorf("a failed seed kept its mark: %+v", a.Terminal)
	}
	data, _ := os.ReadFile(r.b.path())
	if strings.Contains(string(data), `"terminal"`) {
		t.Error("a failed seed's mark stays in accounts.json")
	}
}

// seedSpy calls at when the seed deletes the profile's keychain item.
type seedSpy struct {
	Vault
	at func()
}

func (s *seedSpy) Delete(service, acct string) error {
	if service != ParkService {
		s.at()
	}
	return s.Vault.Delete(service, acct)
}

// A claude running in the profile holds its login: the seed would replace
// a newer one, as claude-swap refuses to.
func TestRunRefusesProfileInUse(t *testing.T) {
	r := newRunBook(t)
	r.running = true
	if err := r.b.Run(bob.Key(), "/work", both); !errors.Is(err, errProfileBusy) {
		t.Fatalf("err = %v", err)
	}
	if len(r.launched) != 0 || r.bob(t).Terminal.Dir != "" {
		t.Errorf("launched %q, marked %+v", r.launched, r.bob(t).Terminal)
	}
}

func TestRunRefuses(t *testing.T) {
	r := newRunBook(t)
	if err := r.b.Run(alice.Key(), "/work", both); !errors.Is(err, errNotAdded) {
		t.Errorf("not added: %v", err)
	}
	r.b.Refresh(ctx, true) // alice is added by following her login
	if err := r.b.Run(alice.Key(), "/work", both); !errors.Is(err, ErrRunInUse) {
		t.Errorf("in use: %v", err)
	}
	r.b.Home = nil
	if err := r.b.Run(bob.Key(), "/work", both); !errors.Is(err, ErrCantSwitch) {
		t.Errorf("no home: %v", err)
	}
	if len(r.launched) != 0 {
		t.Error("launched")
	}
}

// Opening it again goes to the profile as it is: the terminal's login is
// newer than the app's.
func TestRunAgainKeepsTheProfile(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/a", both))
	renewed := stored(t, login("bob-at2", "bob-rt2", t0.Add(8*time.Hour)))
	must(t, r.v.Put(liveService(r.dir()), "me", renewed))
	must(t, r.b.Run(bob.Key(), "/b", both))
	if got, _ := r.v.Get(liveService(r.dir()), "me"); string(got) != string(renewed) {
		t.Error("the terminal's login was replaced")
	}
	if len(r.launched) != 2 {
		t.Errorf("launched %q", r.launched)
	}
}

func TestInTerminalIsNotSwitchedExportedImportedOrRemoved(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/work", both))
	if err := r.b.Switch(ctx, bob.Key()); !errors.Is(err, errInTerminal) {
		t.Errorf("switch: %v", err)
	}
	if err := r.b.Remove(bob.Key()); !errors.Is(err, errInTerminal) {
		t.Errorf("remove: %v", err)
	}
	if _, _, err := r.b.Export(filepath.Join(t.TempDir(), "out.json"), false); !errors.Is(err, errNothingToExport) {
		t.Errorf("export: %v", err)
	}
	path := writeExport(t, claudeSwap(t,
		swapEntry(t, 1, bob, login("bob-new", "bob-rt-new", t0.Add(9*time.Hour)), bobAccount)))
	if got, err := r.b.Import(path); err != nil || got != (Imported{Skipped: 1}) {
		t.Errorf("import: %+v %v", got, err)
	}
}

func TestRefreshReadsTerminalLoginNeverRenews(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/work", both))
	r.running = true
	r.f.usage["bob-at2"] = 40
	must(t, r.v.Put(liveService(r.dir()), "me", stored(t, login("bob-at2", "bob-rt2", t0.Add(time.Hour)))))
	r.f.took()
	got, _ := r.b.Refresh(ctx, true)
	if a := blockOf(t, got, "bob@example.com"); a.Err != "" || len(a.Limits) == 0 {
		t.Fatalf("bob = %+v", a)
	}
	if !slices.Contains(r.f.took(), "usage bob-at2") {
		t.Error("not read with the terminal's token")
	}
	r.f.renewed["bob-rt"], r.f.renewed["bob-rt2"] = "x", "y"
	delete(r.f.usage, "bob-at2")
	got, _ = r.b.Refresh(ctx, true)
	if a := blockOf(t, got, "bob@example.com"); a.Err != InTerminalExpired {
		t.Errorf("bob = %+v", a)
	}
	for _, c := range r.f.took() {
		if strings.HasPrefix(c, "renew") {
			t.Errorf("renewed while in a terminal: %s", c)
		}
	}
}

func TestTakeBack(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/work", both))
	dir := r.dir()
	renewed := login("bob-at2", "bob-rt2", t0.Add(8*time.Hour))
	renewed.RefreshTokenExpiresAt = t0.Add(30 * 24 * time.Hour).UnixMilli()
	must(t, r.v.Put(liveService(dir), "me", stored(t, renewed)))

	r.b.Refresh(ctx, true) // claude not up yet: within the grace
	if r.bob(t).Terminal.Dir == "" {
		t.Fatal("taken back before claude could start")
	}
	r.b.Now = func() time.Time { return t0.Add(time.Hour) }
	r.scanErr = errors.New("unreadable")
	r.b.Refresh(ctx, true)
	if r.bob(t).Terminal.Dir == "" {
		t.Fatal("taken back on a failed scan")
	}
	r.scanErr, r.running = nil, true
	r.b.Refresh(ctx, true)
	if a := r.bob(t); a.Terminal.Dir == "" || !a.Terminal.Seen {
		t.Fatalf("while running: %+v", a.Terminal)
	}
	r.running = false
	r.b.Refresh(ctx, true)
	a := r.bob(t)
	if a.Terminal.Dir != "" {
		t.Fatal("not taken back after claude quit")
	}
	if l, _ := parseLogin(get(t, r.v, ParkService, bob.Key())); l.RefreshToken != "bob-rt2" {
		t.Errorf("saved login %q", l.RefreshToken)
	}
	if !a.GoodUntil.Equal(time.UnixMilli(renewed.RefreshTokenExpiresAt)) {
		t.Errorf("good until %v", a.GoodUntil)
	}
	if _, err := r.v.Get(liveService(dir), "me"); !errors.Is(err, ErrNotFound) {
		t.Error("the profile keeps its keychain login")
	}
	if _, err := os.Stat(filepath.Join(dir, ".credentials.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the profile keeps its seed")
	}
	must(t, r.b.Switch(ctx, bob.Key()))
}

// A seed claude never renewed is older than nothing: the app's login stays.
func TestTakeBackKeepsNewerSaved(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/work", both))
	_, err := r.b.keep(login("bob-new", "bob-rt-new", t0.Add(5*time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	if r.bob(t).Terminal.Dir == "" {
		t.Fatal("keep dropped the mark")
	}
	r.b.Now = func() time.Time { return t0.Add(time.Hour) }
	r.b.Refresh(ctx, true)
	if r.bob(t).Terminal.Dir != "" {
		t.Fatal("not taken back")
	}
	if l, _ := parseLogin(get(t, r.v, ParkService, bob.Key())); l.RefreshToken != "bob-rt-new" {
		t.Errorf("saved login %q", l.RefreshToken)
	}
}

func TestClaudeRunning(t *testing.T) {
	dir := t.TempDir()
	if ok, err := ClaudeRunning(dir); ok || err != nil {
		t.Fatalf("no sessions folder: %v %v", ok, err)
	}
	s := filepath.Join(dir, "sessions")
	must(t, os.Mkdir(s, 0o700))
	dead := exec.Command("true")
	must(t, dead.Run())
	must(t, os.WriteFile(filepath.Join(s, "1.json"), []byte(`{"pid":`+strconv.Itoa(dead.Process.Pid)+`}`), 0o600))
	if ok, err := ClaudeRunning(dir); ok || err != nil {
		t.Fatalf("dead pid: %v %v", ok, err)
	}
	must(t, os.WriteFile(filepath.Join(s, "2.json"), []byte(`{"pid":`+strconv.Itoa(os.Getpid())+`}`), 0o600))
	if ok, err := ClaudeRunning(dir); !ok || err != nil {
		t.Fatalf("live pid: %v %v", ok, err)
	}
	must(t, os.Remove(filepath.Join(s, "2.json")))
	must(t, os.WriteFile(filepath.Join(s, "3.json"), []byte(`{`), 0o600))
	if _, err := ClaudeRunning(dir); err == nil {
		t.Fatal("an unreadable record reads as no session")
	}
}

// The command reaches claude with both paths whole, whatever they hold.
func TestRunCommandQuoting(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, `it's a "dir" $HOME`)
	profile := filepath.Join(root, "Application Support", `o'k`)
	must(t, os.MkdirAll(folder, 0o700))
	bin := filepath.Join(root, "bin")
	must(t, os.Mkdir(bin, 0o700))
	must(t, os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nprintf '%s\\n%s' \"$PWD\" \"$CLAUDE_CONFIG_DIR$ANTHROPIC_API_KEY$CLAUDE_CODE_OAUTH_TOKEN\"\n"), 0o755))
	// claude is where FindClaude's fallbacks may have found it, not on PATH.
	cmd := exec.Command("/bin/sh", "-c", RunCommand(filepath.Join(bin, "claude"), folder, profile))
	// A key or token the user's shell exports would override the profile's
	// login (cswap's AUTH_OVERRIDE_ENV_VARS): claude gets none.
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin", "ANTHROPIC_API_KEY=leak", "CLAUDE_CODE_OAUTH_TOKEN=leak")
	out, err := cmd.Output()
	must(t, err)
	real, _ := filepath.EvalSymlinks(folder)
	if got := strings.Split(string(out), "\n"); len(got) != 2 || (got[0] != folder && got[0] != real) || got[1] != profile {
		t.Errorf("claude got %q", out)
	}
}

// Opened again after its claude quit: take-back waits for the new claude
// as for the first, and leaves its login be.
func TestRunAgainWaitsForTheNewClaude(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/a", both))
	r.running = true
	r.b.Refresh(ctx, true)
	r.running = false
	r.b.Now = func() time.Time { return t0.Add(time.Hour) }
	must(t, r.b.Run(bob.Key(), "/b", both))
	r.b.Refresh(ctx, true)
	if r.bob(t).Terminal.Dir == "" {
		t.Fatal("taken back under the claude just opened")
	}
	if _, err := os.Stat(filepath.Join(r.dir(), ".credentials.json")); err != nil {
		t.Error("the profile's login was removed")
	}
}

// Outside the default folder a marked account is still read, not a panic.
func TestRefreshInTerminalWithoutHome(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/a", both))
	r.b.Home = nil
	r.f.usage["bob-at"] = 10
	got, _ := r.b.Refresh(ctx, true)
	if a := blockOf(t, got, "bob@example.com"); a.Terminal.Dir == "" || a.Err != "" {
		t.Errorf("bob = %+v", a)
	}
}

// ~/.claude.json may lag: Claude Code's login naming the account, or
// sharing its refresh token, makes it the one in use.
func TestRunRefusesClaudeCodesLogin(t *testing.T) {
	for _, name := range []string{"its uuids", "its refresh token", "~/.claude.json"} {
		t.Run(name, func(t *testing.T) {
			r := newRunBook(t)
			l := stored(t, login("bob-at", "bob-rt", t0.Add(time.Hour)))
			switch name {
			case "its uuids":
				l = withUUIDs(t, stored(t, login("x-at", "x-rt", t0.Add(time.Hour))), bob)
			case "~/.claude.json":
				l = stored(t, login("x-at", "x-rt", t0.Add(time.Hour)))
				must(t, os.WriteFile(r.h.ClaudeJSON, []byte(`{"oauthAccount":`+bobAccount+`}`), 0o600))
			}
			must(t, r.v.Put(liveDefault, "me", l))
			if err := r.b.Run(bob.Key(), "/a", both); !errors.Is(err, ErrRunInUse) {
				t.Fatalf("err = %v", err)
			}
			if len(r.launched) != 0 || r.bob(t).Terminal.Dir != "" {
				t.Error("seeded Claude Code's own login")
			}
		})
	}
}

var both = Share{Setup: true, History: true}

// claude-swap's --no-share and --no-share-history: links not wanted are
// removed, the profile's own files and ~/.claude never touched.
func TestRunShare(t *testing.T) {
	r := newRunBook(t)
	for _, name := range []string{"settings.json", "history.jsonl"} {
		must(t, os.WriteFile(filepath.Join(r.h.Root, name), []byte("mine"), 0o600))
	}
	must(t, os.Mkdir(filepath.Join(r.h.Root, "projects"), 0o700))
	must(t, os.WriteFile(r.h.ClaudeJSON, []byte(`{"oauthAccount":`+aliceAccount+`,"mcpServers":{"s":{}}}`), 0o600))
	must(t, r.b.Run(bob.Key(), "/a", Share{Setup: true}))
	dir := r.dir()
	isLink := func(name string) bool { _, err := os.Readlink(filepath.Join(dir, name)); return err == nil }
	if !isLink("settings.json") || isLink("projects") || isLink("history.jsonl") {
		t.Fatal("history shared without asking")
	}
	r.b.Now = func() time.Time { return t0.Add(time.Hour) }
	r.b.Refresh(ctx, true) // taken back
	must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("its own"), 0o600))
	must(t, r.b.Run(bob.Key(), "/a", Share{History: true}))
	if isLink("settings.json") || !isLink("projects") || !isLink("history.jsonl") {
		t.Error("setup still shared, or history not")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); string(data) != "its own" {
		t.Error("removed the profile's own CLAUDE.md")
	}
	if data, _ := os.ReadFile(filepath.Join(r.h.Root, "settings.json")); string(data) != "mine" {
		t.Error("~/.claude/settings.json changed")
	}
	r2 := newRunBook(t)
	must(t, os.WriteFile(r2.h.ClaudeJSON, []byte(`{"oauthAccount":`+aliceAccount+`,"mcpServers":{"s":{}}}`), 0o600))
	must(t, r2.b.Run(bob.Key(), "/a", Share{}))
	conf2, _ := os.ReadFile(filepath.Join(r2.dir(), ".claude.json"))
	if strings.Contains(string(conf2), "mcpServers") {
		t.Errorf("MCP servers mirrored without setup shared: %s", conf2)
	}
}

// A /login to another account in the terminal: that login is neither read
// as this account's nor saved under it; it is kept as unclaimed.
func TestTakeBackDrifted(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/work", both))
	dir := r.dir()
	carolLogin := withUUIDs(t, stored(t, login("carol-at", "carol-rt", t0.Add(9*time.Hour))), carol)
	must(t, r.v.Put(liveService(dir), "me", carolLogin))
	r.running = true
	got, _ := r.b.Refresh(ctx, true)
	if a := blockOf(t, got, "bob@example.com"); a.Err != errDrifted.Error() {
		t.Errorf("read as bob's: %+v", a)
	}
	r.running = false
	r.b.Refresh(ctx, true)
	if l, _ := parseLogin(get(t, r.v, ParkService, bob.Key())); l.RefreshToken != "bob-rt" {
		t.Errorf("carol's login saved as bob's: %q", l.RefreshToken)
	}
	kept, _ := r.b.UnclaimedLogins()
	if len(kept) != 1 || !strings.Contains(kept[0].Why, "another account") {
		t.Fatalf("kept %+v", kept)
	}
	if r.bob(t).Terminal.Dir != "" {
		t.Error("not taken back")
	}

	// Its .claude.json alone says so too.
	r2 := newRunBook(t)
	must(t, r2.b.Run(bob.Key(), "/work", both))
	must(t, os.WriteFile(filepath.Join(r2.dir(), ".claude.json"), []byte(`{"oauthAccount":`+carolAccount+`}`), 0o600))
	if !r2.b.drifted(r2.bob(t)) {
		t.Error("drift by .claude.json missed")
	}
	must(t, os.WriteFile(filepath.Join(r2.dir(), ".claude.json"), []byte(`{}`), 0o600))
	if r2.b.drifted(r2.bob(t)) {
		t.Error("unreadable identity taken for drift")
	}
}

// Terminal may wait on macOS's Automation prompt: refresh and the window
// must not wait with it.
func TestRunLaunchesUnlocked(t *testing.T) {
	r := newRunBook(t)
	locked := 0
	r.b.Launch = func(string, string, ...string) error {
		if !r.b.mu.TryLock() {
			locked++
		} else {
			r.b.mu.Unlock()
		}
		return nil
	}
	must(t, r.b.Run(bob.Key(), "/a", both))
	must(t, r.b.Run(bob.Key(), "/a", both)) // opened again
	if locked != 0 {
		t.Errorf("launched %d times holding the lock", locked)
	}
}

// claude-swap's purge: every saved login, previous login, unclaimed login,
// terminal profile and its keychain item, and accounts.json, go; never
// under a claude running in a profile.
func TestPurge(t *testing.T) {
	r := newRunBook(t)
	must(t, r.b.Run(bob.Key(), "/a", both))
	must(t, r.v.Put(liveService(r.dir()), "me", []byte("profile login")))
	must(t, r.v.Put(ParkPrev, bob.Key(), []byte("prev")))
	must(t, r.v.Put(UnclaimedService, "u1", []byte("kept")))
	r.b.unclaimed = []Unclaimed{{ID: "u1"}}
	must(t, r.b.save())
	r.running = true
	if _, err := r.b.Purge(); !errors.Is(err, errProfileBusy) {
		t.Fatalf("purged under a running claude: %v", err)
	}
	r.running = false
	n, err := r.b.Purge()
	if err != nil || n != 1 {
		t.Fatalf("Purge = %d, %v", n, err)
	}
	if r.v.Len() != 1 { // Claude Code's own login stays
		t.Errorf("%d keychain items left", r.v.Len())
	}
	for _, p := range []string{r.dir(), r.b.path()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left", p)
		}
	}
	if got, _ := r.b.Blocks(); slices.ContainsFunc(got, func(a Account) bool { return a.Added }) {
		t.Error("an added account left")
	}
}

// claude-swap refuses before launching when claude isn't installed: the
// terminal would open on "command not found", the login seeded for
// nothing.
func TestRunNeedsClaude(t *testing.T) {
	r := newRunBook(t)
	r.b.Claude = func() (string, error) { return "", errors.New("Claude Code not found.") }
	if err := r.b.Run(bob.Key(), "/a", both); err == nil || err.Error() != "Claude Code not found." {
		t.Fatalf("err = %v", err)
	}
	if len(r.launched) != 0 || r.bob(t).Terminal.Dir != "" {
		t.Error("launched or marked without claude")
	}
}

// claude-swap's _bootstrap: a profile's .claude.json is always past
// onboarding, so a profile kept from before doesn't ask again.
func TestSeedSkipsOnboarding(t *testing.T) {
	r := newRunBook(t)
	write(t, filepath.Join(r.dir(), ".claude.json"), `{"numStartups":2}`)
	must(t, r.b.Run(bob.Key(), "/a", both))
	conf, _ := os.ReadFile(filepath.Join(r.dir(), ".claude.json"))
	if !strings.Contains(string(conf), `"hasCompletedOnboarding":true`) || !strings.Contains(string(conf), `"numStartups":2`) {
		t.Errorf("profile .claude.json %s", conf)
	}
}

// The account in use runs plain claude, its shell's environment as it is.
func TestRunCommandDefault(t *testing.T) {
	if got := RunCommand("/bin/it's claude", "/f", "", "--resume"); got != `cd '/f' && exec '/bin/it'\''s claude' '--resume'` {
		t.Errorf("RunCommand = %s", got)
	}
}

// claude-swap's MCP mirror (issue #139): with setup shared, the profile's
// mcpServers mirror ~/.claude.json's, its own first kept aside once; with
// it off, a mirrored copy goes, and a profile never mirrored is left be.
func TestSeedMirrorsMCPServers(t *testing.T) {
	r := newRunBook(t)
	conf := filepath.Join(r.dir(), ".claude.json")
	must(t, os.WriteFile(r.h.ClaudeJSON, []byte(strings.Replace(aliceConf, `"numStartups": 3,`, `"numStartups": 3, "mcpServers": {"srv": {"command": "x"}},`, 1)), 0o600))
	write(t, conf, `{"mcpServers":{"own":{"command":"y"}}}`)
	// Never mirrored, sharing off: its own servers stay.
	must(t, r.b.Run(bob.Key(), "/a", Share{}))
	if c, _ := os.ReadFile(conf); !strings.Contains(string(c), `"own"`) {
		t.Fatalf("an unmirrored profile lost its servers: %s", c)
	}
	r.b.Now = func() time.Time { return t0.Add(time.Hour) }
	r.b.Refresh(ctx, true) // taken back
	must(t, r.b.Run(bob.Key(), "/a", both))
	if c, _ := os.ReadFile(conf); !strings.Contains(string(c), `"srv"`) || strings.Contains(string(c), `"own"`) {
		t.Errorf("mirror: %s", c)
	}
	if s, _ := os.ReadFile(filepath.Join(r.dir(), mcpDisplaced)); !strings.Contains(string(s), `"own"`) {
		t.Errorf("its own servers weren't kept aside: %q", s)
	}
	r.b.Now = func() time.Time { return t0.Add(2 * time.Hour) }
	r.b.Refresh(ctx, true)
	must(t, r.b.Run(bob.Key(), "/a", Share{}))
	if c, _ := os.ReadFile(conf); strings.Contains(string(c), `"srv"`) {
		t.Errorf("the mirrored servers stayed with sharing off: %s", c)
	}
}

// claude-swap's _default_login_signed_out: only a login that certainly
// holds no usable token reads as signed out; anything else keeps the fast path.
func TestLiveSignedOut(t *testing.T) {
	past, future := t0.Add(-time.Hour).UnixMilli(), t0.Add(time.Hour).UnixMilli()
	oauth := func(s string) string { return `{"claudeAiOauth":{` + s + `}}` }
	for _, c := range []struct {
		name, blob string
		want       bool
	}{
		{"empty", ``, true},
		{"blank", " \n", true},
		{"api key", `sk-ant-api03-xyz`, false},
		{"not json", `{`, false},
		{"no oauth object", `{"mcpOAuth":{}}`, false},
		{"refresh token", oauth(`"accessToken":"","refreshToken":"rt"`), false},
		{"both tokens empty", oauth(`"accessToken":"","refreshToken":""`), true},
		{"no tokens", oauth(``), true},
		{"access token lapsed", oauth(`"accessToken":"at","expiresAt":` + strconv.FormatInt(past, 10)), true},
		{"access token valid", oauth(`"accessToken":"at","expiresAt":` + strconv.FormatInt(future, 10)), false},
		{"access token no expiry", oauth(`"accessToken":"at"`), false},
		{"access token null expiry", oauth(`"accessToken":"at","expiresAt":null`), false},
		{"access token odd expiry", oauth(`"accessToken":"at","expiresAt":true`), false},
	} {
		r := newRunBook(t)
		must(t, r.v.Put(liveDefault, "me", []byte(c.blob)))
		if got := r.b.liveSignedOut(); got != c.want {
			t.Errorf("%s: signed out = %v, want %v", c.name, got, c.want)
		}
	}
	r := newRunBook(t)
	must(t, r.v.Delete(liveDefault, "me"))
	if r.b.liveSignedOut() {
		t.Error("no login at all is not a signed-out login")
	}
	r = newRunBook(t)
	must(t, r.v.Put(liveDefault, "me", []byte(``)))
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", "/elsewhere")
	if r.b.liveSignedOut() {
		t.Error("another credential store than the one read")
	}
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", "")
	r.b.Vault = lockedVault{r.v}
	if r.b.liveSignedOut() {
		t.Error("an unreadable keychain")
	}
}

// claude-swap's same-account fast path is taken only while the default
// login holds a usable token; signed out, the stored login launches in
// the account's profile, and with none stored the old refusal stays.
func TestRunInUseSignedOut(t *testing.T) {
	r := newRunBook(t)
	r.b.Refresh(ctx, true) // alice is added by following her login
	must(t, r.v.Put(liveDefault, "me", []byte(`{"claudeAiOauth":{"accessToken":"","refreshToken":""}}`)))
	if !r.b.InUseSignedOut(alice.Key()) || r.b.InUseSignedOut(bob.Key()) {
		t.Error("InUseSignedOut: alice yes, bob no")
	}
	must(t, r.b.Run(alice.Key(), "/work", both))
	if want := [][2]string{{"/work", r.b.profile(alice.Key())}}; !slices.Equal(r.launched, want) {
		t.Errorf("launched %q, want %q", r.launched, want)
	}
	if _, err := os.Stat(filepath.Join(r.b.profile(alice.Key()), ".credentials.json")); err != nil {
		t.Errorf("the profile holds no seeded login: %v", err)
	}

	r = newRunBook(t)
	r.b.Refresh(ctx, true)
	must(t, r.v.Put(liveDefault, "me", []byte(``)))
	must(t, r.v.Delete(ParkService, alice.Key()))
	if err := r.b.Run(alice.Key(), "/work", both); !errors.Is(err, ErrRunInUse) || len(r.launched) != 0 {
		t.Errorf("no saved login: %v, launched %q", err, r.launched)
	}
}

// claude-swap's _history_on_the_fast_path: the profile's own history goes
// into ~/.claude's when sharing is on; off, the launch says which history
// it uses; a profile with none of its own, or none, is left alone.
func TestRunDefaultHistory(t *testing.T) {
	own := func(r *runBook) (string, string) {
		dir := r.b.profile(bob.Key())
		must(t, os.MkdirAll(filepath.Join(dir, "projects", "p"), 0o700))
		jsonl := filepath.Join(dir, "projects", "p", "s.jsonl")
		must(t, os.WriteFile(jsonl, []byte("x\n"), 0o600))
		must(t, os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte("h1\n"), 0o600))
		return dir, jsonl
	}
	shared := func(r *runBook) string { return filepath.Join(r.h.Root, "projects", "p", "s.jsonl") }

	r := newRunBook(t)
	_, jsonl := own(r)
	note, err := r.b.RunDefault(bob.Key(), "/work", both)
	must(t, err)
	if got, _ := os.ReadFile(shared(r)); string(got) != "x\n" {
		t.Errorf("shared transcript %q", got)
	}
	if _, err := os.Stat(jsonl); err == nil {
		t.Error("the profile's transcript stayed")
	}
	if got, _ := os.ReadFile(filepath.Join(r.h.Root, "history.jsonl")); string(got) != "h1\n" {
		t.Errorf("shared history.jsonl %q", got)
	}
	if note != "" || !slices.Equal(r.launched, [][2]string{{"/work", ""}}) {
		t.Errorf("note %q, launched %q", note, r.launched)
	}

	r = newRunBook(t)
	_, jsonl = own(r)
	note, err = r.b.RunDefault(bob.Key(), "/work", Share{Setup: true})
	must(t, err)
	if _, err := os.Stat(shared(r)); err == nil {
		t.Error("history merged with sharing off")
	}
	if _, err := os.Stat(jsonl); err != nil {
		t.Error("the profile's transcript went with sharing off")
	}
	if note != "This launch uses the default login's chat history (~/.claude), not the account's own." {
		t.Errorf("note %q", note)
	}

	r = newRunBook(t) // no profile
	if note, err = r.b.RunDefault(bob.Key(), "/work", both); err != nil || note != "" {
		t.Errorf("no profile: %q, %v", note, err)
	}
	if note, err = r.b.RunDefault(bob.Key(), "/work", Share{}); err != nil || note != "" {
		t.Errorf("no profile, not sharing: %q, %v", note, err)
	}
	if note, err = r.b.RunDefault("", "/work", Share{}); err != nil || note != "" {
		t.Errorf("no account: %q, %v", note, err)
	}

	r = newRunBook(t) // a profile whose history is already linked
	dir := r.b.profile(bob.Key())
	must(t, os.MkdirAll(dir, 0o700))
	must(t, os.MkdirAll(filepath.Join(r.h.Root, "projects"), 0o700))
	must(t, os.Symlink(filepath.Join(r.h.Root, "projects"), filepath.Join(dir, "projects")))
	if note, err = r.b.RunDefault(bob.Key(), "/work", Share{}); err != nil || note != "" {
		t.Errorf("linked: %q, %v", note, err)
	}

	r = newRunBook(t) // a claude running in the profile: its history is left where it is
	_, jsonl = own(r)
	r.running = true
	if _, err = r.b.RunDefault(bob.Key(), "/work", both); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsonl); err != nil {
		t.Error("history moved under a running claude")
	}
}
