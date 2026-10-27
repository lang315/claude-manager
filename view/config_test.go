package view

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// open opens the row of the outline showing key, as Right does on the row
// chosen.
func open(t *testing.T, tt *ui.Tester, key string) {
	t.Helper()
	must(t, tt.Click(key))
	tt.Key(0, ui.KeyRight)
}

func TestScalarMasksSecrets(t *testing.T) {
	for _, c := range []struct {
		key  string
		v    any
		want string
	}{
		{"API_TOKEN", "secret123", "••••••"},
		{"Authorization", "Bearer abc", "••••••"},
		{"OPENAI_API_KEY", "sk-1", "••••••"},
		{"AWS_ACCESS_KEY_ID", "AKIA1", "••••••"},
		{"GITHUB_PAT", "x", "••••••"},
		{"SSH_KEY", "x", "••••••"},
		{"Cookie", "x", "••••••"},
		{"PASSPHRASE", "x", "••••••"},
		{"SENTRY_DSN", "x", "••••••"},
		{"note", "Bearer abc", "••••••"},
		{"0", "sk-abcdefgh123", "••••••"},
		{"url", "postgres://u:p@h/db", "••••••"},
		{"url", "https://example.com/mcp", "https://example.com/mcp"},
		{"EDITOR", "vim", "vim"},
		{"empty", "", `""`},
		{"count", 3.0, "3"},
		{"on", true, "true"},
		{"none", nil, "null"},
	} {
		if got := scalar(c.key, c.v); got != c.want {
			t.Errorf("scalar(%q, %v) = %q, want %q", c.key, c.v, got, c.want)
		}
	}
}

func TestConfigMasksSecrets(t *testing.T) {
	_, tt, _, _ := newApp(t)
	must(t, tt.Click("Config"))
	open(t, tt, "env")
	if !tt.HasText("API_TOKEN") || !tt.HasText("••••••") || !tt.HasText("vim") {
		t.Errorf("env not shown masked; texts %q", tt.Texts())
	}
	must(t, tt.Click("MCP servers"))
	if !tt.HasText("http · https://example.com/mcp") {
		t.Errorf("no summary of the remote server; texts %q", tt.Texts())
	}
	open(t, tt, "remote")
	open(t, tt, "headers")
	if !tt.HasText("Authorization") {
		t.Errorf("MCP header not shown; texts %q", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if strings.Contains(s, "secret123") || strings.Contains(s, "Bearer abc") {
			t.Errorf("a credential shows: %q", s)
		}
	}
}

func TestConfigDocsAndBackups(t *testing.T) {
	_, tt, d, trashed := newApp(t)
	must(t, tt.Click("Config"))
	must(t, tt.Click("Memory"))
	if !tt.HasText("All projects") || !tt.HasText("Global rules") {
		t.Errorf("CLAUDE.md not listed first and shown; texts %q", tt.Texts())
	}
	must(t, tt.Click("MEMORY.md"))
	if !tt.HasText("me-proj") || !tt.HasText("likes Go") {
		t.Errorf("memory file not rendered under its project; texts %q", tt.Texts())
	}
	must(t, tt.Click("Skills"))
	if !tt.HasText("A demo skill") || !tt.HasText("description") {
		t.Errorf("skill's front matter not shown; texts %q", tt.Texts())
	}
	must(t, tt.Click("Backups"))
	must(t, tt.Click("settings.json.bak-1"))
	must(t, tt.Click("Move to Trash…"))
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	if len(*trashed) != 1 || (*trashed)[0] != filepath.Join(d.Root, "settings.json.bak-1") {
		t.Errorf("trashed %q, want only settings.json.bak-1", *trashed)
	}
}

func TestConfigMasksByStructure(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, os.WriteFile(d.ClaudeJSON, []byte(`{"mcpServers":{"x":{"args":["--k","sk-abcdefgh123"],
		"env":{"FOO":"plain-secret","NEST":{"deep":"nested-secret"}},"auth":{"value":"abc-auth"},"tokens":["tok-arr"]}}}`), 0o600))
	a.Rescan()
	tt.Frame()
	must(t, tt.Click("Config"))
	must(t, tt.Click("MCP servers"))
	open(t, tt, "x")
	for _, k := range []string{"tokens", "env", "auth", "args"} { // bottom up, so rows opened don't move those above
		open(t, tt, k)
	}
	open(t, tt, "NEST")
	for _, k := range []string{"FOO", "deep", "value"} {
		if !tt.HasText(k) {
			t.Errorf("%s not shown; texts %q", k, tt.Texts())
		}
	}
	for _, s := range tt.Texts() {
		for _, bad := range []string{"plain-secret", "nested-secret", "abc-auth", "tok-arr", "sk-abcdefgh123"} {
			if strings.Contains(s, bad) {
				t.Errorf("a credential shows: %q", s)
			}
		}
	}
}

func TestConfigEmptySettings(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, os.WriteFile(filepath.Join(d.Root, "settings.json"), []byte(`{}`), 0o600))
	a.Rescan()
	tt.Frame()
	must(t, tt.Click("Config"))
	if !tt.HasText("Nothing configured in settings.json.") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestScalarMasksCredentialShapes(t *testing.T) {
	for _, v := range []string{
		"https://mcp.example.com/sse?api_key=abc123def456",
		"https://server.smithery.ai/x/mcp?token=abc123def456",
		"--api-key=abc123def456",
		"--token=abc123def456",
		"--passphrase=abc123def456",
		"--cookie=abc123def456",
		"https://mcp.example.com/sse?credential=abc123def456",
		"ghs_abcdefabcdefabcdef",
		"gho_abcdefabcdefabcdef",
		"AKIAABCDEFGHIJKLMNOP",
		"Authorization: Basic dXNlcjpwYXNz",
	} {
		if got := scalar("url", v); got != mask {
			t.Errorf("shown: %s", got)
		}
	}
}

// An args array holds a flag and its value as two elements.
func TestConfigMasksValueAfterSecretFlag(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, os.WriteFile(d.ClaudeJSON, []byte(`{"mcpServers":{"x":{"args":["--api-key","abc123","--verbose","plain"]}}}`), 0o600))
	a.Rescan()
	tt.Frame()
	must(t, tt.Click("Config"))
	must(t, tt.Click("MCP servers"))
	if !tt.HasText("stdio · --api-key •••••• --verbose plain") {
		t.Errorf("summary does not mask the value after --api-key; texts %q", tt.Texts())
	}
	open(t, tt, "x")
	open(t, tt, "args")
	if !tt.HasText("plain") {
		t.Errorf("args not shown; texts %q", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if strings.Contains(s, "abc123") {
			t.Errorf("a credential shows: %q", s)
		}
	}
}

// A long list scrolls inside its face: the tabs and the title stay put.
func TestConfigLongListStaysInItsFace(t *testing.T) {
	a, tt, d, _ := newApp(t)
	for i := range 80 {
		f := filepath.Join(d.Root, "projects", fmt.Sprintf("-gone-project-%d", i%8), "memory", fmt.Sprintf("note-%02d.md", i))
		must(t, os.MkdirAll(filepath.Dir(f), 0o755))
		must(t, os.WriteFile(f, []byte("---\ndescription: a note\n---\nbody\n"), 0o644))
	}
	a.Rescan()
	tt.Frame()
	must(t, tt.Click("Config"))
	tab, _ := tt.Find("Memory")
	title, _ := tt.Find("Read-only. Credentials are masked.")
	must(t, tt.Click("Memory"))
	tab2, _ := tt.Find("Memory")
	title2, ok := tt.Find("Read-only. Credentials are masked.")
	first, _ := tt.Find("All projects")
	if !ok || tab2.Y != tab.Y || title2.Y != title.Y || first.Y <= tab2.Y {
		t.Errorf("the list moved the page: tab %v to %v, title %v to %v, first header at %v", tab, tab2, title, title2, first)
	}
}

func TestHookRows(t *testing.T) {
	hooks := map[string]any{
		"Stop":         []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "say\n  done", "timeout": 30.0}}}},
		"Custom":       []any{map[string]any{"hooks": []any{map[string]any{"type": "prompt", "prompt": "check"}}}},
		"SessionStart": []any{map[string]any{"matcher": "startup", "hooks": []any{map[string]any{"command": "curl -H 'Authorization: Bearer abc' x"}}}},
	}
	got := hookRows(hooks)
	want := []hookRow{
		{event: "SessionStart", matcher: "startup", command: mask},
		{event: "Stop", command: "say done", timeout: "30 s"},
		{event: "Custom", command: "prompt: check"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("hookRows = %+v, want %+v", got, want)
	}
}

// settings.json's own mcpServers show on the Settings tab too, masked as on
// the MCP servers tab.
func TestSettingsMasksItsMCPServers(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, os.WriteFile(filepath.Join(d.Root, "settings.json"), []byte(`{"mcpServers":{"s":{"args":["--api-key","plain-arg"],"env":{"FOO":"plain-env"}}}}`), 0o600))
	a.Rescan()
	tt.Frame()
	must(t, tt.Click("Config"))
	open(t, tt, "mcpServers")
	open(t, tt, "s")
	open(t, tt, "env")
	open(t, tt, "args")
	if !tt.HasText("FOO") {
		t.Errorf("env not opened; texts %q", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if strings.Contains(s, "plain-env") || strings.Contains(s, "plain-arg") {
			t.Errorf("a credential shows: %q", s)
		}
	}
}

// A header passed as its own args element, "--header", "X-API-Key: …", is
// masked by the header's name; an ordinary header still shows.
func TestScalarMasksSecretHeaders(t *testing.T) {
	for _, v := range []string{"X-API-Key: abc123", "x-auth-token:abc123", "Cookie: sid=abc123",
		"--header=X-API-Key: abc123", "-H X-API-Key: abc123", "--header X-API-Key: abc123", " X-API-Key: abc123",
		"X-Signature: abc123", "X-Client-Credential: abc123", "X-Passcode: abc123"} {
		if got := scalar("1", v); got != mask {
			t.Errorf("scalar(%q) = %q, want the mask", v, got)
		}
	}
	if got := scalar("1", "Content-Type: application/json"); got != "Content-Type: application/json" {
		t.Errorf("an ordinary header is masked: %q", got)
	}
	got := mcpSummary(map[string]any{"command": "npx", "args": []any{"mcp-remote", "--header", "X-API-Key: abc123"}})
	if strings.Contains(got, "abc123") {
		t.Errorf("summary shows the key: %q", got)
	}
}

// After a trash, the checks of the files gone don't linger: a click on a
// backup left picks it.
func TestBackupsChecksClearOnReload(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Config"))
	must(t, tt.Click("Backups"))
	a.config.backups.Checked.Add(filepath.Join(a.Store.Root, "settings.json.bak-1"))
	tt.Frame()
	must(t, tt.Click("Move to Trash…"))
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	must(t, tt.Click("settings.json.bak"))
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("Move 1 settings backup to the Trash?") {
		t.Errorf("a stale check keeps the key off; texts %q", tt.Texts())
	}
}

// Names are matched by their segments: author, hotkey and design are not
// credentials, while api-key, privatekey and access_token are.
func TestSecretNames(t *testing.T) {
	for _, v := range []string{"author: Jane", "hotkey: ctrl+k", "--design=x", "https://x.io/?bypass=1", "https://x.io/?page=2"} {
		if got := scalar("1", v); got != v {
			t.Errorf("scalar(%q) = %q, want it shown", v, got)
		}
	}
	for _, v := range []string{"x-api-key: abc", "https://x.io/?apiKey=abc", "https://x.io/?privatekey=abc", "--auth-token=abc",
		"Authorization: Bearer x", "https://x.io/?sig=abc", "https://x.io/?access_token=abc", "--password=x", "Cookie: a=b",
		// A token in a URL inside another's query, and names in the plural.
		"https://a.com/login?next=https://b.com/cb?token=abc123", "https://a.com/?redirect_uri=https://b/?api_key=abc",
		"--api-keys=sk_live_abc", "--tokens=abc", "Cookies: a=b", "https://x.io/?passwords=x", "--authentication=abc"} {
		if got := scalar("1", v); got != mask {
			t.Errorf("scalar(%q) = %q, want the mask", v, got)
		}
	}
}
