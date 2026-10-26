package store

import (
	"os"
	"path/filepath"
	"testing"
)

func names(ds []Doc) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}

func TestConfig(t *testing.T) {
	s, d, _ := newStore(t)
	cfg := s.Config()
	if len(cfg.Errs) > 0 {
		t.Fatalf("errors: %q", cfg.Errs)
	}
	if env, _ := cfg.Settings["env"].(map[string]any); env["EDITOR"] != "vim" {
		t.Errorf("settings env %v", cfg.Settings["env"])
	}
	if _, ok := cfg.MCP["local"]; !ok {
		t.Error("MCP lacks local, from settings.json")
	}
	if _, ok := cfg.MCP["remote"]; !ok {
		t.Error("MCP lacks remote, from .claude.json")
	}
	if cfg.Plugins["installed"] == nil || cfg.Plugins["marketplaces"] == nil {
		t.Errorf("plugins %v", cfg.Plugins)
	}
	if got := names(cfg.Skills); len(got) != 1 || got[0] != "demo" {
		t.Errorf("skills %q", got)
	}
	if got := names(cfg.Commands); len(got) != 1 || got[0] != "hello" {
		t.Errorf("commands %q", got)
	}
	want := []string{"CLAUDE.md", "-Users-me-proj/memory/MEMORY.md", "-Users-me-proj/mygo-memory/MEMORY.md"}
	if got := names(cfg.Memory); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("memory %q, want %q", got, want)
	}
	if len(cfg.Backups) != 2 {
		t.Errorf("%d backups, want 2", len(cfg.Backups))
	}

	if err := os.WriteFile(filepath.Join(d.Root, "settings.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := s.Config(); len(cfg.Errs) != 1 {
		t.Errorf("errors for broken settings.json: %q", cfg.Errs)
	}
}
