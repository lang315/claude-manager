package store

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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
	if p := cfg.Plugins; len(p) != 1 || p[0].Name != "demo" || p[0].Marketplace != "market" || p[0].Version != "1.0.0" {
		t.Errorf("plugins %+v", p)
	}
	if m := cfg.Marketplaces; len(m) != 1 || m[0].Name != "market" || m[0].Source != "github" {
		t.Errorf("marketplaces %+v", m)
	}
	if s := cfg.Skills; len(s) != 1 || s[0].Name != "demo" || s[0].Desc != "A demo skill" || s[0].Body != "# Demo\n" {
		t.Errorf("skills %+v", s)
	}
	if c := cfg.Commands; len(c) != 1 || c[0].Name != "hello" || c[0].Desc != "Say hello" {
		t.Errorf("commands %+v", c)
	}
	want := []string{"CLAUDE.md", "MEMORY.md", "mygo-memory/MEMORY.md"}
	if got := names(cfg.Memory); !slices.Equal(got, want) || cfg.Memory[0].Group != "" || !strings.HasSuffix(cfg.Memory[1].Group, "me-proj") {
		t.Errorf("memory %q in %q, want %q", got, cfg.Memory[1].Group, want)
	}
	if len(cfg.Backups) != 2 {
		t.Errorf("%d backups, want 2", len(cfg.Backups))
	}

	for _, f := range []string{"installed_plugins.json", "known_marketplaces.json"} {
		if err := os.Remove(filepath.Join(d.Root, "plugins", f)); err != nil {
			t.Fatal(err)
		}
	}
	if cfg := s.Config(); len(cfg.Plugins) != 0 || len(cfg.Marketplaces) != 0 || len(cfg.Errs) != 0 {
		t.Errorf("plugins %v, errors %q without plugin files", cfg.Plugins, cfg.Errs)
	}

	if err := os.WriteFile(filepath.Join(d.Root, "settings.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := s.Config(); len(cfg.Errs) != 1 {
		t.Errorf("errors for broken settings.json: %q", cfg.Errs)
	}
}

func TestPluginSizeAndState(t *testing.T) {
	s, d, _ := newStore(t)
	inst := filepath.Join(d.Root, "plugins", "cache", "big")
	if err := os.MkdirAll(inst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, "f"), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	// Two installs of one plugin share a path; it counts once.
	installed := `{"plugins":{"small@m":[{"version":"1"}],"big@m":[` +
		`{"installPath":"` + inst + `","version":"1","lastUpdated":"2026-01-01T00:00:00Z"},` +
		`{"installPath":"` + inst + `","version":"2","lastUpdated":"2026-02-01T00:00:00Z"}]}}`
	if err := os.WriteFile(filepath.Join(d.Root, "plugins", "installed_plugins.json"), []byte(installed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.Root, "settings.json"), []byte(`{"enabledPlugins":{"small@m":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := s.Config().Plugins
	if len(p) != 2 || p[0].Name != "big" || p[0].Size != 1000 || p[0].Version != "2" || p[0].Off || p[1].Name != "small" || !p[1].Off {
		t.Errorf("plugins %+v", p)
	}
}

func TestFrontMatter(t *testing.T) {
	fields, body := frontMatter("---\nname: x\ndescription: >\n  runs on\n  two lines\nallowed-tools: \"Read\"\n---\n# Body\n")
	want := [][2]string{{"name", "x"}, {"description", "runs on two lines"}, {"allowed-tools", "Read"}}
	if !slices.Equal(fields, want) || body != "# Body\n" {
		t.Errorf("fields %q, body %q", fields, body)
	}
	if fields, _ := frontMatter("---\ndescription: \"says \\\"hi\\\"\"\n---\n"); len(fields) != 1 || fields[0][1] != `says "hi"` {
		t.Errorf("quoted value %q", fields)
	}
	if fields, body := frontMatter("# No front matter\n"); fields != nil || body != "# No front matter\n" {
		t.Errorf("fields %q, body %q", fields, body)
	}
}

func TestProjectPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "github.com", "my-org", ".hidden_dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, _ = filepath.EvalSymlinks(dir) // /var is /private/var on macOS
	name := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(dir, "-")
	if got := projectPath(name); got != dir {
		t.Errorf("projectPath(%q) = %q, want %q", name, got, dir)
	}
	if got := projectPath("-no-such-place-here"); got != "/no-such-place-here" {
		t.Errorf("a gone path gave %q", got)
	}
}

// A doc that exists but can't be read is reported, not dropped in silence.
func TestConfigReportsUnreadableDocs(t *testing.T) {
	s, d, _ := newStore(t)
	f := filepath.Join(d.Root, "skills", "demo", "SKILL.md")
	if err := os.Chmod(f, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(f, 0o644) })
	cfg := s.Config()
	if len(cfg.Skills) != 0 || len(cfg.Errs) != 1 || !strings.Contains(cfg.Errs[0], "SKILL.md") {
		t.Errorf("skills %d, errors %q; want the unreadable SKILL.md reported", len(cfg.Skills), cfg.Errs)
	}
}
