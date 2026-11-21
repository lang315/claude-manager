package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
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

func TestSwitchListsOutgoingFirst(t *testing.T) {
	b, v, h, _ := switchBook(t)
	before := get(t, v, liveDefault, "me")
	rename = func(from, to string) error {
		if filepath.Base(to) == "accounts.json" {
			return errors.New("disk full")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })
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

func TestSwitchAsksAnthropicBeforeLocks(t *testing.T) {
	b, _, h, f := switchBook(t)
	locks := []string{filepath.Join(h.Root, ".oauth_refresh.lock"), h.Root + ".lock", h.ClaudeJSON + ".lock"}
	asked := false
	f.onProfile = func() {
		asked = true
		for _, d := range locks {
			if _, err := os.Stat(d); err == nil {
				t.Errorf("Anthropic asked while %s is held", d)
			}
		}
	}
	must(t, b.Switch(ctx, bob.Key()))
	if !asked {
		t.Fatal("the switch never asked whose the login in use is")
	}
}

func TestSwitchLiveRenewedMidSwitch(t *testing.T) {
	renewed := stored(t, login("alice-2", "alice-rt2", t0.Add(2*time.Hour)))
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
	must(t, b.Switch(ctx, bob.Key()))
	must(t, b.Switch(ctx, alice.Key()))
	must(t, v.Put(liveDefault, "me", stored(t, login("alice-old", "alice-rt", t0.Add(-time.Hour)))))
	must(t, b.Switch(ctx, bob.Key()))
	if l, _ := parseLogin(get(t, v, ParkService, alice.Key())); l.AccessToken != "alice-old" {
		t.Errorf("alice's saved login = %q, want the one in use", l.AccessToken)
	}
	must(t, v.Put(liveDefault, "me", stored(t, login("x", "x-rt", t0.Add(-time.Hour)))))
	must(t, b.Switch(ctx, alice.Key()))
	kept, _ := b.UnclaimedLogins()
	if len(kept) != 1 {
		t.Fatalf("unclaimed %+v", kept)
	}
	if l, _ := parseLogin(get(t, v, UnclaimedService, kept[0].ID)); l.AccessToken != "x" {
		t.Errorf("kept %q", l.AccessToken)
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
	old := time.Now().Add(-configStale - time.Second)
	must(t, os.Chtimes(held, old, old))
	must(t, b.Switch(ctx, bob.Key()))
	if _, err := os.Stat(held + ".takeover"); !os.IsNotExist(err) {
		t.Error("takeover dir left behind")
	}
}

func TestLockStaleUnremovable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x.lock")
	must(t, os.MkdirAll(filepath.Join(dir, "inside"), 0o755))
	old := time.Now().Add(-time.Hour)
	must(t, os.Chtimes(dir, old, old))
	done := make(chan error, 1)
	go func() { _, err := lock(ctx, dir, configStale); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("took a lock it could not remove")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lock spun on a stale lock it could not remove")
	}
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
	_, err := b.keep(login("carol-at", "carol-rt", t0.Add(time.Hour)).Marshal(), carol, nil)
	must(t, err)
	if !errors.Is(b.Switch(ctx, carol.Key()), errNoAccount) {
		t.Fatal("switched with no oauthAccount for carol")
	}
}

func TestRefreshLeavesAccountInUse(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	must(t, b.Switch(ctx, alice.Key()))
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

// withUUIDs is blob with whose login it is inside claudeAiOauth, as Claude
// Code writes it.
func withUUIDs(t *testing.T, blob []byte, p Profile) []byte {
	t.Helper()
	o := map[string]any{}
	must(t, json.Unmarshal(oauthOf(blob), &o))
	o["accountUuid"], o["organizationUuid"] = p.AccountUUID, p.OrgUUID
	raw, _ := json.Marshal(o)
	out, err := withOAuth(blob, raw)
	must(t, err)
	return out
}

// bobLapsed switches to bob, then has Claude Code renew his login and leave
// it to lapse: its refresh token is no longer the one the app saved.
func bobLapsed(t *testing.T) (*Book, *Memory, *Home, *anthropic) {
	t.Helper()
	b, v, h, f := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	must(t, v.Put(liveDefault, "me", withUUIDs(t, stored(t, login("bob-at2", "bob-rt2", t0.Add(3*time.Hour))), bob)))
	b.Now = func() time.Time { return t0.Add(4 * time.Hour) }
	return b, v, h, f
}

func TestSwitchLapsedRenewedLogin(t *testing.T) {
	b, v, _, _ := bobLapsed(t)
	must(t, b.Switch(ctx, alice.Key()))
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt2" {
		t.Errorf("bob's saved login = %q, want Claude Code's latest", l.RefreshToken)
	}
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "alice-at" {
		t.Errorf("login in use %q", l.AccessToken)
	}
}

func TestSwitchLapsedLoginDisagreeing(t *testing.T) {
	b, v, h, _ := bobLapsed(t)
	must(t, os.WriteFile(h.ClaudeJSON, []byte(aliceConf), 0o600)) // ~/.claude.json names alice, the login says bob
	before := get(t, v, liveDefault, "me")
	must(t, b.Switch(ctx, alice.Key()))
	kept, _ := b.UnclaimedLogins()
	if len(kept) != 1 || !bytes.Equal(oauthOf(get(t, v, UnclaimedService, kept[0].ID)), oauthOf(before)) {
		t.Fatalf("the login in use was not kept: %+v", kept)
	}
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt" {
		t.Errorf("bob's saved login = %q", l.RefreshToken)
	}
}

func TestSwitchLapsedLoginKeepsNewerSaved(t *testing.T) {
	b, v, _, _ := bobLapsed(t)
	_, err := b.keep(login("bob-new", "bob-rt-new", t0.Add(5*time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	must(t, b.Switch(ctx, alice.Key()))
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt-new" {
		t.Errorf("bob's newer saved login was replaced by %q", l.RefreshToken)
	}
}

// ~/.claude.json names another account, so the app can't tell bob is in
// use; his saved login is still Claude Code's, and must not be renewed.
func TestRefreshNeverRenewsClaudeCodesToken(t *testing.T) {
	b, _, h, f := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	must(t, os.WriteFile(h.ClaudeJSON, []byte(aliceConf), 0o600))
	b.Now = func() time.Time { return t0.Add(2 * time.Hour) }
	f.renewed["bob-rt"], f.renewed["alice-rt"] = "bob-at3", "alice-at3"
	f.took()
	got, _ := b.Refresh(ctx, true)
	if slices.Contains(f.took(), "renew bob-rt") {
		t.Fatal("the app renewed the refresh token Claude Code holds")
	}
	if a := blockOf(t, got, "bob@example.com"); a.Err != LiveExpired {
		t.Errorf("bob = %+v", a)
	}
}

func TestRefreshAddsAccountInUse(t *testing.T) {
	b, v, _, _ := switchBook(t)
	got, err := b.Refresh(ctx, true)
	must(t, err)
	a := blockOf(t, got, "alice@example.com")
	if !a.InUse || !a.Added || keyOf(a.OAuthAccount) != alice.Key() {
		t.Fatalf("alice = %+v", a)
	}
	if l, _ := parseLogin(get(t, v, ParkService, alice.Key())); l.AccessToken != "alice-at" {
		t.Errorf("alice's saved login %q", l.AccessToken)
	}
	if strings.Contains(string(get(t, v, ParkService, alice.Key())), "mcpOAuth") {
		t.Error("Claude Code's MCP logins were saved with hers")
	}
}

func TestRefreshAddsNothingWhenClaudeJSONDisagrees(t *testing.T) {
	b, v, h, _ := switchBook(t)
	must(t, os.WriteFile(h.ClaudeJSON, []byte(strings.ReplaceAll(aliceConf, `"alice"`, `"carol"`)), 0o600))
	b.Refresh(ctx, true)
	if _, err := v.Get(ParkService, alice.Key()); !errors.Is(err, ErrNotFound) {
		t.Fatal("alice added though ~/.claude.json names another account")
	}
}

// Claude Code renews bob's login; then the user signs in as alice outside
// the app. bob's saved login must be the renewed one, or it is spent.
func TestRefreshFollowsClaudeCodesRenewal(t *testing.T) {
	b, v, _, f := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	f.who["bob-at2"], f.usage["bob-at2"] = bob, 5
	must(t, v.Put(liveDefault, "me", stored(t, login("bob-at2", "bob-rt2", t0.Add(8*time.Hour)))))
	b.Refresh(ctx, true)
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt2" {
		t.Fatalf("bob's saved login = %q", l.RefreshToken)
	}
	if l, _ := parseLogin(get(t, v, ParkPrev, bob.Key())); l.RefreshToken != "bob-rt" {
		t.Errorf("previous login = %q", l.RefreshToken)
	}
	f.who["alice-at3"], f.usage["alice-at3"] = alice, 5
	f.renewed["bob-rt2"] = "bob-at3"
	f.usage["bob-at3"] = 7
	must(t, v.Put(liveDefault, "me", stored(t, login("alice-at3", "alice-rt3", t0.Add(9*time.Hour)))))
	b.Now = func() time.Time { return t0.Add(8 * time.Hour) }
	got, _ := b.Refresh(ctx, true)
	if a := blockOf(t, got, "bob@example.com"); a.Err != "" || a.InUse {
		t.Fatalf("bob = %+v", a)
	}
}

func TestRefreshKeepsNewerSavedLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	_, err := b.keep(login("bob-new", "bob-rt-new", t0.Add(5*time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	b.Refresh(ctx, true)
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt-new" {
		t.Fatalf("bob's newer saved login was replaced by %q", l.RefreshToken)
	}
}

// Claude Code changes its login while the app asks whose it is: what it
// reads after is not the login it asked about, and is not saved.
func TestRefreshSavesOnlyTheLoginIdentified(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.onProfile = func() { v.Put(liveDefault, "me", stored(t, login("x-at", "x-rt", t0.Add(9*time.Hour)))) }
	b.Refresh(ctx, true)
	if _, err := v.Get(ParkService, alice.Key()); !errors.Is(err, ErrNotFound) {
		t.Fatal("saved a login other than the one identified")
	}
}

func TestRefreshFollowsOnlyInDefaultFolder(t *testing.T) {
	b, v, _, _ := switchBook(t)
	b.Home = nil
	b.Refresh(ctx, true)
	if _, err := v.Get(ParkService, alice.Key()); !errors.Is(err, ErrNotFound) {
		t.Fatal("added the account in use outside Claude Code's default folder")
	}
}

// A forced switch onto the account in use keeps the login it replaces as
// the account's previous one: it may be the newer.
func TestSwitchForceKeepsReplacedLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	_, err := b.keep(login("alice-old-at", "alice-old-rt", t0.Add(time.Hour)).Marshal(), alice, json.RawMessage(aliceConf))
	must(t, err)
	must(t, b.SwitchForce(ctx, alice.Key()))
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "alice-old-at" {
		t.Fatalf("Claude Code's login is %q", l.AccessToken)
	}
	if l, _ := parseLogin(get(t, v, ParkPrev, alice.Key())); l.RefreshToken != "alice-rt" {
		t.Fatalf("the replaced login was not kept: %+v", l)
	}
}
