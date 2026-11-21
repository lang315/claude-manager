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

const key1 = "sk-ant-api03-0123456789abcdefghijKLMNOPQRSTUV"

func confOf(t *testing.T, h *Home) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(h.ClaudeJSON)
	must(t, err)
	var m map[string]json.RawMessage
	must(t, json.Unmarshal(data, &m))
	return m
}

func TestAddAPIKey(t *testing.T) {
	b, v, _, _ := switchBook(t)
	for _, bad := range []string{"sk-ant-oat01-x", "abc", "sk-ant-api03 x"} {
		if _, err := b.AddAPIKey(bad, ""); !errors.Is(err, errNotAPIKey) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	a, err := b.AddAPIKey(key1, "")
	must(t, err)
	if a.Email != "api-key-1@token.local" || !a.APIKey || string(get(t, v, ParkService, a.Key())) != key1 {
		t.Fatalf("a = %+v", a)
	}
}

// To a key and back, as Claude Code's saveApiKey and removeApiKey: one way
// in at a time, the MCP sign-ins kept.
func TestSwitchAPIKey(t *testing.T) {
	b, v, h, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 10
	must(t, os.WriteFile(h.ClaudeJSON, []byte(strings.Replace(aliceConf, `"numStartups": 3,`, `"numStartups": 3, "primaryApiKey": "old",`, 1)), 0o600))
	must(t, v.Put(managedService, "me", []byte("sk-ant-api03-theirs")))
	k, err := b.AddAPIKey(key1, "ci@example.com")
	must(t, err)
	must(t, b.Switch(ctx, k.Key()))
	b.lastSwitch = time.Time{}
	must(t, b.Switch(ctx, k.Key()))
	if !b.lastSwitch.IsZero() {
		t.Error("switched again to the key in use")
	}
	if got := get(t, v, managedService, "me"); string(got) != key1 {
		t.Errorf("managed key %q", got)
	}
	live := get(t, v, liveDefault, "me")
	if oauthOf(live) != nil || !strings.Contains(string(live), "mcpOAuth") {
		t.Errorf("live %s", live)
	}
	c := confOf(t, h)
	if _, ok := c["primaryApiKey"]; ok {
		t.Error("primaryApiKey kept")
	}
	if !strings.Contains(string(c["customApiKeyResponses"]), `"`+key1[len(key1)-20:]+`"`) || !strings.Contains(string(c["oauthAccount"]), "ci@example.com") {
		t.Errorf("conf %s %s", c["customApiKeyResponses"], c["oauthAccount"])
	}
	if l, _ := parseLogin(get(t, v, ParkService, alice.Key())); l.RefreshToken != "alice-rt" {
		t.Errorf("alice not saved: %q", l.RefreshToken)
	}
	blocks, _ := b.Blocks()
	if theirs := blockOf(t, blocks, "api-key-1@token.local"); !theirs.APIKey {
		t.Error("the managed key the app didn't hold was not added first")
	}

	f.took()
	got, _ := b.Refresh(ctx, true)
	if a := blockOf(t, got, "ci@example.com"); !a.InUse || a.Err != "" {
		t.Errorf("key in use: %+v", a)
	}
	for _, c := range f.took() {
		if strings.Contains(c, key1) {
			t.Errorf("asked Anthropic with the key: %s", c)
		}
	}
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	if d, _ := b.AutoSwitch(ctx); d.To != "" || d.Why != errKeyInUse.Error() {
		t.Errorf("auto-switch on a key: %+v", d)
	}

	must(t, b.Switch(ctx, bob.Key()))
	if _, err := v.Get(managedService, "me"); !errors.Is(err, ErrNotFound) {
		t.Error("managed key left in place")
	}
	live = get(t, v, liveDefault, "me")
	if l, _ := parseLogin(live); l.AccessToken != "bob-at" || !strings.Contains(string(live), "mcpOAuth") {
		t.Errorf("live %s", live)
	}
}

// ~/.claude.json's primaryApiKey alone names the key in use too.
func TestAPIKeyInUseByPrimaryApiKey(t *testing.T) {
	b, _, h, _ := switchBook(t)
	k, err := b.AddAPIKey(key1, "")
	must(t, err)
	must(t, os.WriteFile(h.ClaudeJSON, []byte(`{"primaryApiKey":"`+key1+`"}`), 0o600))
	if got := b.keyInUse(); got != k.Key() {
		t.Errorf("in use %q", got)
	}
}

func TestSwitchAPIKeyRestores(t *testing.T) {
	b, v, h, _ := switchBook(t)
	k, err := b.AddAPIKey(key1, "")
	must(t, err)
	before, _ := os.ReadFile(h.ClaudeJSON)
	rename = func(from, to string) error {
		if filepath.Base(to) == ".claude.json" {
			return errors.New("disk full")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })
	if err := b.Switch(ctx, k.Key()); err == nil {
		t.Fatal("switched")
	}
	if _, err := v.Get(managedService, "me"); !errors.Is(err, ErrNotFound) {
		t.Error("managed key left after a failed switch")
	}
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "alice-at" {
		t.Errorf("live %q", l.AccessToken)
	}
	if after, _ := os.ReadFile(h.ClaudeJSON); string(after) != string(before) {
		t.Error("~/.claude.json changed")
	}
}

// claude-swap's includeApiKeyAccounts: a key is the last resort, at the
// limit or on a failover, never otherwise.
func TestDecideAPIKeyLastResort(t *testing.T) {
	key := Account{AccountUUID: keyUUID + "k", Email: "k", Added: true, APIKey: true, OAuthAccount: json.RawMessage(`{}`)}
	spent := acct("s", 100, 10, day)
	s := DefaultAuto()
	if d := decide(t0, s, []Account{inUse(acct("a", 100, 10, day)), spent, key}, memo{}); d.To != "" {
		t.Errorf("off: %+v", d)
	}
	s.Keys = true
	if d := decide(t0, s, []Account{inUse(acct("a", 100, 10, day)), spent, key}, memo{}); d.To != key.Key() {
		t.Errorf("at the limit: %+v", d)
	}
	if d := decide(t0, s, []Account{inUse(acct("a", 95, 10, day)), spent, key}, memo{}); d.To != "" {
		t.Errorf("proactive: %+v", d)
	}
	if d := decide(t0, s, []Account{inUse(acct("a", 100, 10, day)), acct("b", 10, 10, day), key}, memo{}); d.Email != "b" {
		t.Errorf("a subscription with room comes first: %+v", d)
	}
}

func TestExportImportAPIKey(t *testing.T) {
	b, _, _, _ := switchBook(t)
	_, err := b.AddAPIKey(key1, "ci@example.com")
	must(t, err)
	path := filepath.Join(t.TempDir(), "out.json")
	n, _, err := b.Export(path, false)
	must(t, err)
	e := entryOf(t, exportOf(t, path), "ci@example.com")
	if n != 2 || e.Kind != "api_key" || string(e.Credentials) != `"`+key1+`"` {
		t.Fatalf("exported %d: %+v", n, e)
	}
	c, v, _, _ := switchBook(t)
	got, err := c.Import(path)
	must(t, err)
	if got.Added != 1 || string(get(t, v, ParkService, keyUUID+"ci@example.com/")) != key1 {
		t.Errorf("imported %+v", got)
	}
	if got, _ := c.Import(path); got.Skipped != 2 {
		t.Errorf("again: %+v", got)
	}
}
