package view

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScalarMasksSecrets(t *testing.T) {
	for _, c := range []struct {
		key  string
		v    any
		want string
	}{
		{"API_TOKEN", "secret123", "••••••"},
		{"Authorization", "Bearer abc", "••••••"},
		{"OPENAI_API_KEY", "sk-1", "••••••"},
		{"EDITOR", "vim", `"vim"`},
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
	must(t, tt.Click("env"))
	if !tt.HasText("API_TOKEN: ••••••") || !tt.HasText(`EDITOR: "vim"`) {
		t.Errorf("env not shown masked; texts %q", tt.Texts())
	}
	must(t, tt.Click("MCP servers"))
	must(t, tt.Click("remote"))
	must(t, tt.Click("headers"))
	if !tt.HasText("Authorization: ••••••") {
		t.Errorf("MCP header not masked; texts %q", tt.Texts())
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
	must(t, tt.Click("-Users-me-proj/memory/MEMORY.md"))
	if !tt.HasText("likes Go") {
		t.Errorf("memory file not rendered; texts %q", tt.Texts())
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
