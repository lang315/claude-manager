package account

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app", "vault.json")
	v := &FileVault{Path: path}
	if _, err := v.Get("s", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty: %v", err)
	}
	must(t, v.Put("s", "a", []byte("1")))
	must(t, v.Put("s", "b", []byte("2")))
	must(t, v.Put("t", "a", []byte("3")))
	if got, _ := v.Get("s", "b"); string(got) != "2" {
		t.Errorf("get %q", got)
	}
	if got, err := v.Get("t", ""); err != nil || string(got) != "3" {
		t.Errorf("any account: %q %v", got, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	must(t, v.Delete("s", "a"))
	if _, err := v.Get("s", "a"); !errors.Is(err, ErrNotFound) {
		t.Error("not deleted")
	}
	must(t, v.Delete("s", ""))
	if _, err := v.Get("s", "b"); !errors.Is(err, ErrNotFound) {
		t.Error("service not deleted")
	}
	if got, _ := (&FileVault{Path: path}).Get("t", "a"); string(got) != "3" {
		t.Error("not kept on disk")
	}
}

// fileBook is switchBook as on Linux, WSL and Windows: Claude Code's login
// in .credentials.json alone, the app's items in a FileVault.
func fileBook(t *testing.T) (*Book, *FileVault, *Home) {
	t.Helper()
	b, _, h, _ := switchBook(t)
	v := &FileVault{Path: filepath.Join(b.Dir, "vault.json")}
	b.Vault, h.FileOnly = v, true
	b.Live = func() (Login, error) { return ReadLive(v, "", h.Root) }
	must(t, os.WriteFile(filepath.Join(h.Root, ".credentials.json"), stored(t, login("alice-at", "alice-rt", t0.Add(5*60*60*1e9))), 0o600))
	must(t, v.Put(ParkService, bob.Key(), login("bob-at", "bob-rt", t0.Add(5*60*60*1e9)).Marshal()))
	return b, v, h
}

func TestSwitchFileOnly(t *testing.T) {
	b, v, h := fileBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	data, _ := os.ReadFile(filepath.Join(h.Root, ".credentials.json"))
	if l, _ := parseLogin(data); l.AccessToken != "bob-at" || !strings.Contains(string(data), "mcpOAuth") {
		t.Fatalf(".credentials.json %s", data)
	}
	if _, err := v.Get(liveDefault, ""); !errors.Is(err, ErrNotFound) {
		t.Error("wrote a keychain-like item for Claude Code")
	}
	if l, _ := parseLogin(get2(t, v, ParkService, alice.Key())); l.AccessToken != "alice-at" {
		t.Errorf("alice saved %q", l.AccessToken)
	}

	k, err := b.AddAPIKey(key1, "")
	must(t, err)
	must(t, b.Switch(ctx, k.Key()))
	var conf map[string]json.RawMessage
	raw, _ := os.ReadFile(h.ClaudeJSON)
	must(t, json.Unmarshal(raw, &conf))
	if string(conf["primaryApiKey"]) != `"`+key1+`"` {
		t.Errorf("primaryApiKey %s", conf["primaryApiKey"])
	}
	if _, err := v.Get(managedService, ""); !errors.Is(err, ErrNotFound) {
		t.Error("the key went to a keychain-like item")
	}
	if got := b.keyInUse(); got != k.Key() {
		t.Errorf("key in use %q", got)
	}
	must(t, b.Switch(ctx, bob.Key()))
	raw, _ = os.ReadFile(h.ClaudeJSON)
	if strings.Contains(string(raw), "primaryApiKey") {
		t.Error("primaryApiKey kept after switching to OAuth")
	}
}

func TestSwitchFileOnlyRestores(t *testing.T) {
	b, _, h := fileBook(t)
	before, _ := os.ReadFile(filepath.Join(h.Root, ".credentials.json"))
	rename = func(from, to string) error {
		if filepath.Base(to) == ".claude.json" {
			return errors.New("disk full")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })
	if err := b.Switch(ctx, bob.Key()); err == nil {
		t.Fatal("switched")
	}
	if after, _ := os.ReadFile(filepath.Join(h.Root, ".credentials.json")); string(after) != string(before) {
		t.Error(".credentials.json not put back")
	}
}

func get2(t *testing.T, v Vault, service, acct string) []byte {
	t.Helper()
	b, err := v.Get(service, acct)
	must(t, err)
	return b
}

// Signed out, the switch creates .credentials.json; a stale item in the
// app's vault under Claude Code's name is never taken for its login.
func TestSwitchFileOnlySignedOutAndStale(t *testing.T) {
	b, v, h := fileBook(t)
	must(t, os.Remove(filepath.Join(h.Root, ".credentials.json")))
	must(t, v.Put(liveDefault, "me", stored(t, login("stale-at", "stale-rt", t0))))
	must(t, b.Switch(ctx, bob.Key()))
	data, err := os.ReadFile(filepath.Join(h.Root, ".credentials.json"))
	if l, _ := parseLogin(data); err != nil || l.AccessToken != "bob-at" {
		t.Fatalf(".credentials.json %s %v", data, err)
	}
	if kept, _ := b.UnclaimedLogins(); len(kept) != 0 {
		t.Errorf("took the stale item for the login in use: %+v", kept)
	}
	if got := get2(t, v, liveDefault, "me"); !strings.Contains(string(got), "stale-at") {
		t.Error("the stale item was written")
	}
}
