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
	b.Launch = func(folder, dir string) error { r.launched = append(r.launched, [2]string{folder, dir}); return nil }
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
// be a second renewer.
func TestRunMarksBeforeSeeding(t *testing.T) {
	r := newRunBook(t)
	must(t, os.MkdirAll(filepath.Dir(ProfilesDir(r.b.Dir)), 0o700))
	must(t, os.WriteFile(ProfilesDir(r.b.Dir), nil, 0o600)) // the seed can't make its folder
	r.b.Launch = func(string, string) error { t.Fatal("launched without a seed"); return nil }
	if err := r.b.Run(bob.Key(), "/work", both); err == nil {
		t.Fatal("ran without a profile")
	}
	data, _ := os.ReadFile(r.b.path())
	if !strings.Contains(string(data), `"terminal"`) {
		t.Error("the mark was not saved before seeding")
	}
}

func TestRunRefuses(t *testing.T) {
	r := newRunBook(t)
	if err := r.b.Run(alice.Key(), "/work", both); !errors.Is(err, errNotAdded) {
		t.Errorf("not added: %v", err)
	}
	r.b.Refresh(ctx, true) // alice is added by following her login
	if err := r.b.Run(alice.Key(), "/work", both); !errors.Is(err, errRunInUse) {
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
	if _, _, err := r.b.Export(filepath.Join(t.TempDir(), "out.json")); !errors.Is(err, errNothingToExport) {
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
	must(t, os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nprintf '%s\\n%s' \"$PWD\" \"$CLAUDE_CONFIG_DIR\"\n"), 0o755))
	cmd := exec.Command("/bin/sh", "-c", RunCommand(folder, profile))
	cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
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
			if err := r.b.Run(bob.Key(), "/a", both); !errors.Is(err, errRunInUse) {
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
