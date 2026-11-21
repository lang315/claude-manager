package account

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// claudeStatus is `claude auth status` for configDir, as Claude Code
// itself reads what the app wrote there. Any keychain item Claude Code
// makes for that folder (a hashed name, fake tokens) is removed after.
func claudeStatus(t *testing.T, configDir string) map[string]any {
	t.Helper()
	cmd := exec.Command("claude", "auth", "status")
	cmd.Dir = configDir
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir, "ANTHROPIC_API_KEY=", "CLAUDE_CODE_OAUTH_TOKEN=")
	out, err := cmd.Output()
	sum := sha256.Sum256([]byte(configDir))
	for _, svc := range []string{"Claude Code-credentials-" + hex.EncodeToString(sum[:4]), "Claude Code-" + hex.EncodeToString(sum[:4])} {
		exec.Command("/usr/bin/security", "delete-generic-password", "-s", svc).Run()
	}
	var m map[string]any
	if json.Unmarshal(out, &m) != nil {
		t.Fatalf("claude auth status: %v %s", err, out)
	}
	return m
}

// What the app writes is what Claude Code reads: a switch (OAuth, a
// setup-token, an API key) and an Open in Terminal seed, each read back
// by the real claude. Opt in with CLAUDE_MANAGER_CLAUDE_E2E=1: it runs
// Claude Code, with fake tokens only, never on the user's own folder.
func TestClaudeCodeReadsWhatTheAppWrites(t *testing.T) {
	if os.Getenv("CLAUDE_MANAGER_CLAUDE_E2E") == "" {
		t.Skip("set CLAUDE_MANAGER_CLAUDE_E2E=1 to run Claude Code itself")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("no claude")
	}
	exp := time.Now().Add(time.Hour)
	// Claude Code holds its locks a little after it answers: the app waits
	// them out, as it would beside a running claude.
	was := lockWait
	lockWait = 70 * time.Second
	t.Cleanup(func() { lockWait = was })
	b, v, _, _ := switchBook(t)
	root := t.TempDir()
	h := &Home{Root: root, ClaudeJSON: filepath.Join(root, ".claude.json"), User: "me", FileOnly: true}
	must(t, os.WriteFile(h.ClaudeJSON, []byte(`{"hasCompletedOnboarding":true}`), 0o600))
	b.Home, b.Vault = h, &FileVault{Path: filepath.Join(t.TempDir(), "vault.json")}
	b.Now = time.Now
	b.Live = func() (Login, error) { return ReadLive(b.Vault, "", root) }

	bobLogin := login("sk-ant-oat01-fakebob", "sk-ant-ort01-fakebob", exp)
	_, err := b.keep(bobLogin.Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	must(t, b.Switch(ctx, bob.Key()))
	if m := claudeStatus(t, root); m["loggedIn"] != true || m["authMethod"] != "claude.ai" || m["email"] != "bob@example.com" {
		t.Errorf("after a switch to bob, Claude Code says %v", m)
	}

	tok, err := b.AddToken("sk-ant-oat01-fakesetup", "ci@example.com")
	must(t, err)
	must(t, b.Switch(ctx, tok.Key()))
	if m := claudeStatus(t, root); m["loggedIn"] != true || m["email"] != "ci@example.com" {
		t.Errorf("after a switch to a setup-token, Claude Code says %v", m)
	}

	key, err := b.AddAPIKey("sk-ant-api03-fakefakefakefakefakefakefake", "key@example.com")
	must(t, err)
	must(t, b.Switch(ctx, key.Key()))
	if m := claudeStatus(t, root); m["loggedIn"] != true || m["authMethod"] != "api_key" {
		t.Errorf("after a switch to an API key, Claude Code says %v", m)
	}

	// Open in Terminal's seed: the profile Claude Code would run from.
	r := newRunBook(t)
	_, err = r.b.keep(login("sk-ant-oat01-fakeseed", "sk-ant-ort01-fakeseed", exp).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	r.b.Now = time.Now
	must(t, r.b.Run(bob.Key(), t.TempDir(), Share{}))
	if m := claudeStatus(t, r.dir()); m["loggedIn"] != true || m["email"] != "bob@example.com" {
		t.Errorf("an Open in Terminal profile, Claude Code says %v", m)
	}
	_ = v
}
