package account

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A ~/.claude.json as Claude Code writes it, signed in to alice.
const aliceConf = `{
  "numStartups": 3,
  "oauthAccount": {
    "accountUuid": "alice",
    "emailAddress": "alice@example.com",
    "organizationUuid": "org-a",
    "displayName": "Alice"
  },
  "projects": {"/x": {"allowedTools": []}}
}
`

const bobAccount = `{"accountUuid":"bob","emailAddress":"bob@example.com","organizationUuid":"org-b","displayName":"Bob"}`

// switchBook is a book whose Claude Code, in a temp home, is signed in to
// alice through the keychain (with an MCP login beside hers), and which has
// bob added.
func switchBook(t *testing.T) (*Book, *Memory, *Home, *anthropic) {
	t.Helper()
	f := newAnthropic()
	f.who["alice-at"], f.who["bob-at"] = alice, bob
	b, v, _, _ := newBook(t, f)
	home := t.TempDir()
	h := &Home{Root: filepath.Join(home, ".claude"), ClaudeJSON: filepath.Join(home, ".claude.json"), User: "me"}
	must(t, os.Mkdir(h.Root, 0o700))
	must(t, os.WriteFile(h.ClaudeJSON, []byte(aliceConf), 0o600))
	b.Home = h
	b.Live = func() (Login, error) { return ReadLive(v, "", h.Root) }
	must(t, v.Put(liveDefault, "me", stored(t, login("alice-at", "alice-rt", t0.Add(time.Hour)))))
	_, err := b.keep(login("bob-at", "bob-rt", t0.Add(time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	return b, v, h, f
}

// stored is l as Claude Code stores it, beside an MCP login.
func stored(t *testing.T, l Login) []byte {
	t.Helper()
	b, err := withOAuth([]byte(`{"mcpOAuth":{"srv":{"accessToken":"mcp"}}}`), oauthOf(l.Marshal()))
	must(t, err)
	return b
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, v *Memory, service, acct string) []byte {
	t.Helper()
	b, err := v.Get(service, acct)
	must(t, err)
	return b
}

func TestSwitch(t *testing.T) {
	b, v, h, _ := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))

	live := get(t, v, liveDefault, "me")
	if l, _ := parseLogin(live); l.AccessToken != "bob-at" {
		t.Errorf("Claude Code's login is %q's, want bob's", l.AccessToken)
	}
	if !strings.Contains(string(live), `"mcpOAuth":{"srv":{"accessToken":"mcp"}}`) {
		t.Errorf("the MCP login was lost: %s", live)
	}
	if l, _ := parseLogin(get(t, v, ParkService, alice.Key())); l.RefreshToken != "alice-rt" {
		t.Errorf("alice's login was not saved: %+v", l)
	}
	conf, _ := os.ReadFile(h.ClaudeJSON)
	want := strings.Replace(aliceConf, aliceConf[strings.Index(aliceConf, `{
    "accountUuid"`):strings.Index(aliceConf, `,
  "projects"`)], bobAccount, 1)
	if string(conf) != want {
		t.Errorf("~/.claude.json =\n%s\nwant\n%s", conf, want)
	}
	for _, d := range []string{filepath.Join(h.Root, ".oauth_refresh.lock"), h.Root + ".lock", h.ClaudeJSON + ".lock"} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("lock %s left behind", d)
		}
	}
	// alice joined the added accounts, with her oauthAccount, and switching
	// back puts both where they were.
	got, _ := b.Blocks()
	a := blockOf(t, got, "alice@example.com")
	if !a.Added || keyOf(a.OAuthAccount) != alice.Key() {
		t.Fatalf("alice = %+v", a)
	}
	must(t, b.Switch(ctx, alice.Key()))
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "alice-at" {
		t.Errorf("switch back: login %q", l.AccessToken)
	}
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.AccessToken != "bob-at" {
		t.Errorf("switch back: bob's saved login %q", l.AccessToken)
	}
	conf, _ = os.ReadFile(h.ClaudeJSON)
	if keyOf(oauthAccountOf(conf)) != alice.Key() || !strings.Contains(string(conf), `"displayName": "Alice"`) {
		t.Errorf("switch back: ~/.claude.json =\n%s", conf)
	}
}

func TestSwitchToAccountInUse(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	before := get(t, v, liveDefault, "me")
	must(t, b.Switch(ctx, bob.Key()))
	if string(get(t, v, liveDefault, "me")) != string(before) {
		t.Error("a switch to the account in use wrote its login")
	}
}

func TestSwitchSaveFailsWritesNothing(t *testing.T) {
	b, v, h, _ := switchBook(t)
	before := get(t, v, liveDefault, "me")
	v.Fail = errors.New("refused")
	if err := b.Switch(ctx, bob.Key()); err == nil {
		t.Fatal("the switch went on without saving alice's login")
	}
	v.Fail = nil
	if string(get(t, v, liveDefault, "me")) != string(before) {
		t.Error("Claude Code's login changed")
	}
	if conf, _ := os.ReadFile(h.ClaudeJSON); string(conf) != aliceConf {
		t.Error("~/.claude.json changed")
	}
}

// The first switch adds the account in use: accounts.json must list it
// before Claude Code's login is overwritten.
func TestSwitchListsOutgoingFirst(t *testing.T) {
	b, v, h, _ := switchBook(t)
	before := get(t, v, liveDefault, "me")
	must(t, os.Mkdir(filepath.Join(b.Dir, "accounts.json.tmp"), 0o700)) // accounts.json can't be written
	if err := b.Switch(ctx, bob.Key()); err == nil {
		t.Fatal("switched without listing alice")
	}
	if string(get(t, v, liveDefault, "me")) != string(before) {
		t.Error("Claude Code's login changed")
	}
	if conf, _ := os.ReadFile(h.ClaudeJSON); string(conf) != aliceConf {
		t.Error("~/.claude.json changed")
	}
}

// Claude Code renewed its login between the first read and the locks.
func TestSwitchLiveMoved(t *testing.T) {
	renewed := stored(t, login("alice-2", "alice-rt2", t0.Add(2*time.Hour)))
	// Claude Code renews while the switch asks whose alice's login is.
	b, v, _, f := switchBook(t)
	f.onProfile = func() { v.Put(liveDefault, "me", renewed) }
	if err := b.Switch(ctx, bob.Key()); !errors.Is(err, errLiveMoved) {
		t.Fatalf("err = %v", err)
	}
	if string(get(t, v, liveDefault, "me")) != string(renewed) {
		t.Error("Claude Code's renewed login was overwritten")
	}
}

func TestSwitchRestoresOnFailedWrite(t *testing.T) {
	b, v, h, _ := switchBook(t)
	before := get(t, v, liveDefault, "me")
	rename = func(from, to string) error {
		if to == h.ClaudeJSON {
			return errors.New("disk full")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })
	err := b.Switch(ctx, bob.Key())
	if err == nil || !strings.Contains(err.Error(), "still uses its earlier account") {
		t.Fatalf("err = %v", err)
	}
	if string(get(t, v, liveDefault, "me")) != string(before) {
		t.Error("Claude Code's login was not put back")
	}
}

func TestSwitchLapsedLiveLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	// alice is added; her login in use has lapsed, with the same refresh
	// token as her saved one: hers.
	must(t, b.Switch(ctx, bob.Key()))
	must(t, b.Switch(ctx, alice.Key()))
	must(t, v.Put(liveDefault, "me", stored(t, login("alice-old", "alice-rt", t0.Add(-time.Hour)))))
	must(t, b.Switch(ctx, bob.Key()))
	if l, _ := parseLogin(get(t, v, ParkService, alice.Key())); l.AccessToken != "alice-old" {
		t.Errorf("alice's saved login = %q, want the one in use", l.AccessToken)
	}
	// A lapsed login nobody has: whose it is is unknown, so nothing moves.
	must(t, v.Put(liveDefault, "me", stored(t, login("x", "x-rt", t0.Add(-time.Hour)))))
	if err := b.Switch(ctx, alice.Key()); !errors.Is(err, errLiveWhose) {
		t.Fatalf("err = %v", err)
	}
}

func TestSwitchNotSignedIn(t *testing.T) {
	b, v, h, _ := switchBook(t)
	must(t, v.Delete(liveDefault, ""))
	must(t, b.Switch(ctx, bob.Key()))
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "bob-at" {
		t.Errorf("login %q", l.AccessToken)
	}
	if _, err := os.Stat(filepath.Join(h.Root, ".credentials.json")); !os.IsNotExist(err) {
		t.Error(".credentials.json was created")
	}
}

// Claude Code keeps its login in .credentials.json when the keychain
// refuses it: the switch writes there, and makes no keychain item.
func TestSwitchCredentialsFile(t *testing.T) {
	b, v, h, _ := switchBook(t)
	file := filepath.Join(h.Root, ".credentials.json")
	must(t, os.WriteFile(file, get(t, v, liveDefault, "me"), 0o600))
	must(t, v.Delete(liveDefault, ""))
	must(t, b.Switch(ctx, bob.Key()))
	data, _ := os.ReadFile(file)
	if l, _ := parseLogin(data); l.AccessToken != "bob-at" {
		t.Errorf(".credentials.json holds %q", l.AccessToken)
	}
	if _, err := v.Get(liveDefault, ""); !errors.Is(err, ErrNotFound) {
		t.Error("a keychain item was made")
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

func TestSwitchWaitsForLock(t *testing.T) {
	b, v, h, _ := switchBook(t)
	lockWait = 300 * time.Millisecond
	t.Cleanup(func() { lockWait = 9 * time.Second })
	held := h.ClaudeJSON + ".lock"
	must(t, os.Mkdir(held, 0o755))
	before := get(t, v, liveDefault, "me")
	if err := b.Switch(ctx, bob.Key()); !errors.Is(err, errLockBusy) {
		t.Fatalf("err = %v", err)
	}
	if string(get(t, v, liveDefault, "me")) != string(before) {
		t.Error("the login changed while Claude Code held its lock")
	}
	for _, d := range []string{filepath.Join(h.Root, ".oauth_refresh.lock"), h.Root + ".lock"} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("lock %s left behind", d)
		}
	}
	// A lock older than its 10 s is a dead holder's: taken over.
	old := time.Now().Add(-time.Minute)
	must(t, os.Chtimes(held, old, old))
	must(t, b.Switch(ctx, bob.Key()))
}

func TestSwitchNeedsHome(t *testing.T) {
	b, _, _, _ := switchBook(t)
	b.Home = nil
	if b.CanSwitch() || !errors.Is(b.Switch(ctx, bob.Key()), ErrCantSwitch) {
		t.Fatal("switched without Claude Code's default folder")
	}
	b, _, _, _ = switchBook(t)
	if !errors.Is(b.Switch(ctx, carol.Key()), errNotAdded) {
		t.Fatal("switched to an account not added")
	}
	// carol, added before the app kept oauthAccount, needs a sign-in again.
	_, err := b.keep(login("carol-at", "carol-rt", t0.Add(time.Hour)).Marshal(), carol, nil)
	must(t, err)
	if !errors.Is(b.Switch(ctx, carol.Key()), errNoAccount) {
		t.Fatal("switched with no oauthAccount for carol")
	}
}

// While Claude Code's login can't be read, ~/.claude.json still names its
// account: that account's saved login shares Claude Code's refresh chain,
// so it is not renewed.
func TestRefreshLeavesAccountInUse(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	must(t, b.Switch(ctx, alice.Key())) // alice in use, both added
	must(t, v.Put(liveDefault, "me", stored(t, login("alice-old", "alice-rt", t0.Add(-time.Hour)))))
	got, _ := b.Refresh(ctx, true)
	a := blockOf(t, got, "alice@example.com")
	if !a.InUse || a.Err != LiveExpired {
		t.Fatalf("alice = %+v", a)
	}
	if len(got) != 2 {
		t.Fatalf("blocks = %+v", got)
	}
}

func TestSpliceKey(t *testing.T) {
	v := json.RawMessage(`{"a":1}`)
	for _, c := range []struct{ doc, want string }{
		{`{"x": 1, "k": {"old": true}, "y": [1]}`, `{"x": 1, "k": {"a":1}, "y": [1]}`},
		{`{"x": 1}`, `{"x": 1,"k":{"a":1}}`},
		{"{ }\n", `{ "k":{"a":1}}` + "\n"},
		{`{"k": null}`, `{"k": {"a":1}}`},
	} {
		got, err := spliceKey([]byte(c.doc), "k", v)
		if err != nil || string(got) != c.want {
			t.Errorf("spliceKey(%s) = %s, %v; want %s", c.doc, got, err, c.want)
		}
	}
	for _, bad := range []string{`[1]`, `{"x":`, ``} {
		if _, err := spliceKey([]byte(bad), "k", v); err == nil {
			t.Errorf("spliceKey(%q) worked", bad)
		}
	}
}

func TestWithTokensKeepsFields(t *testing.T) {
	blob := []byte(`{"claudeAiOauth":{"accessToken":"a","refreshToken":"r","expiresAt":1,"scopes":["s"],"extra":"keep"}}`)
	l, _ := parseLogin(blob)
	l.AccessToken, l.RefreshToken = "a2", "r2"
	out := withTokens(blob, l)
	got, _ := parseLogin(out)
	if got.AccessToken != "a2" || got.RefreshToken != "r2" || !strings.Contains(string(out), `"extra":"keep"`) {
		t.Fatalf("withTokens = %s", out)
	}
}
