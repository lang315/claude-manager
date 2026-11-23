//go:build darwin

package account

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realKeychain is a keychain file of the test's own, made and unlocked
// with macOS's security, never the user's login keychain; removed after.
func realKeychain(t *testing.T) Keychain {
	t.Helper()
	if _, err := os.Stat("/usr/bin/security"); err != nil {
		t.Skip("no /usr/bin/security")
	}
	path := filepath.Join(t.TempDir(), "e2e.keychain-db")
	for _, args := range [][]string{
		{"create-keychain", "-p", "e2e", path},
		{"unlock-keychain", "-p", "e2e", path},
		{"set-keychain-settings", path}, // no auto-lock
	} {
		if out, err := exec.Command("/usr/bin/security", args...).CombinedOutput(); err != nil {
			t.Fatalf("security %s: %v %s", args[0], err, out)
		}
	}
	t.Cleanup(func() { exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	t.Setenv("CLAUDE_MANAGER_KEYCHAIN_GUARD", path)
	return Keychain{File: path}
}

// End to end on macOS's own keychain tool: Claude Code's item, the app's
// park items, an 8 KB login (past security -i's line), a switch A -> B -> A
// under Claude Code's locks, and ~/.claude.json, read back with security.
func TestE2ESwitchOnARealKeychain(t *testing.T) {
	k := realKeychain(t)
	f := newAnthropic()
	f.who["alice-at"], f.who["bob-at"] = alice, bob
	f.usage["alice-at"], f.usage["bob-at"] = 40, 10
	b, _, _, _ := newBook(t, f)
	b.Vault = k
	home := t.TempDir()
	h := &Home{Root: filepath.Join(home, ".claude"), ClaudeJSON: filepath.Join(home, ".claude.json"), User: "me"}
	must(t, os.Mkdir(h.Root, 0o700))
	must(t, os.WriteFile(h.ClaudeJSON, []byte(aliceConf), 0o600))
	b.Home = h
	b.Live = func() (Login, error) { return ReadLive(k, "", h.Root) }

	// Claude Code's login as it keeps it: about 8 KB, MCP sign-ins and all.
	mcp := map[string]any{}
	for i := 0; i < 40; i++ {
		mcp[strings.Repeat("s", 8)+string(rune('a'+i%26))+string(rune('a'+i/26))] = map[string]string{"accessToken": strings.Repeat("m", 150)}
	}
	blob, _ := json.Marshal(map[string]any{"mcpOAuth": mcp})
	live, err := withOAuth(blob, oauthOf(login("alice-at", "alice-rt", t0.Add(time.Hour)).Marshal()))
	must(t, err)
	if len(live) < 6000 {
		t.Fatalf("login only %d bytes", len(live))
	}
	must(t, k.Put(liveDefault, "me", live))
	if got, err := k.Get(liveDefault, "me"); err != nil || string(got) != string(live) {
		t.Fatalf("an 8 KB login read back wrong: %v (%d bytes)", err, len(got))
	}
	_, err = b.keep(login("bob-at", "bob-rt", t0.Add(time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)

	must(t, b.Switch(ctx, bob.Key()))
	now, _ := k.Get(liveDefault, "me")
	if l, _ := parseLogin(now); l.AccessToken != "bob-at" || !strings.Contains(string(now), "mcpOAuth") {
		t.Fatalf("after A -> B the keychain holds %q", l.AccessToken)
	}
	if conf, _ := os.ReadFile(h.ClaudeJSON); !strings.Contains(string(conf), `"accountUuid":"bob"`) || !strings.Contains(string(conf), `"numStartups": 3`) {
		t.Errorf("~/.claude.json %s", conf)
	}
	saved, err := k.Get(ParkService, alice.Key())
	if l, _ := parseLogin(saved); err != nil || l.RefreshToken != "alice-rt" || strings.Contains(string(saved), "mcpOAuth") {
		t.Errorf("alice's saved login: %v", err)
	}

	must(t, b.Switch(ctx, alice.Key()))
	now, _ = k.Get(liveDefault, "me")
	if l, _ := parseLogin(now); l.AccessToken != "alice-at" || !strings.Contains(string(now), "mcpOAuth") {
		t.Fatalf("after B -> A the keychain holds %q", l.AccessToken)
	}
	if conf, _ := os.ReadFile(h.ClaudeJSON); !strings.Contains(string(conf), `"accountUuid": "alice"`) && !strings.Contains(string(conf), `"accountUuid":"alice"`) {
		t.Errorf("~/.claude.json %s", conf)
	}
	if l, _ := parseLogin(must2(k.Get(ParkService, bob.Key()))); l.RefreshToken != "bob-rt" {
		t.Error("bob's saved login lost")
	}
	// Refresh reads the account in use and follows it.
	got, err := b.Refresh(ctx, true)
	must(t, err)
	if a := blockOf(t, got, "alice@example.com"); !a.InUse || a.Limits[0].Percent != 40 {
		t.Errorf("alice %+v", a)
	}
	// Remove deletes the park items from the keychain.
	must(t, b.Remove(bob.Key()))
	if _, err := k.Get(ParkService, bob.Key()); err != ErrNotFound {
		t.Errorf("bob's item: %v", err)
	}
}

func must2(b []byte, err error) []byte { return b }

// The guard: with it set, a command naming another keychain is refused
// before it runs.
func TestKeychainGuard(t *testing.T) {
	t.Setenv("CLAUDE_MANAGER_KEYCHAIN_GUARD", "/tmp/only-this.keychain-db")
	if _, err := (Keychain{}).Get("claude-manager-guard-test", "x"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("err = %v", err)
	}
	if err := (Keychain{}).Put("claude-manager-guard-test", "x", []byte("s")); err == nil {
		t.Error("a write past the guard")
	}
}
