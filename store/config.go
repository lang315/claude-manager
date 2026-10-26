package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// Doc is a Markdown file shown as is.
type Doc struct{ Name, Path, Text string }

// Config is what Claude Code is configured with. The app only reads it.
type Config struct {
	Settings map[string]any // settings.json
	MCP      map[string]any // mcpServers of settings.json and .claude.json
	Plugins  map[string]any // plugins/installed_plugins.json and known_marketplaces.json
	Skills   []Doc          // skills/*/SKILL.md
	Commands []Doc          // commands/**/*.md
	Memory   []Doc          // CLAUDE.md and projects/*/*memory/*.md
	Backups  []Item         // settings.json.bak*
	Errs     []string       // files that exist but could not be read
}

func (s *Store) Config() Config {
	var cfg Config
	read := func(path string) map[string]any {
		b, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				cfg.Errs = append(cfg.Errs, err.Error())
			}
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			cfg.Errs = append(cfg.Errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		return m
	}
	cfg.Settings = read(filepath.Join(s.Root, "settings.json"))
	cfg.MCP = map[string]any{}
	for _, m := range []map[string]any{cfg.Settings, read(s.ClaudeJSON)} {
		if servers, ok := m["mcpServers"].(map[string]any); ok {
			maps.Copy(cfg.MCP, servers)
		}
	}
	cfg.Plugins = map[string]any{
		"installed":    read(filepath.Join(s.Root, "plugins", "installed_plugins.json")),
		"marketplaces": read(filepath.Join(s.Root, "plugins", "known_marketplaces.json")),
	}
	cfg.Skills = docs(filepath.Join(s.Root, "skills", "*", "SKILL.md"), func(p string) string { return filepath.Base(filepath.Dir(p)) })
	cfg.Commands = commands(filepath.Join(s.Root, "commands"))
	projects := filepath.Join(s.Root, "projects")
	cfg.Memory = append(docs(filepath.Join(s.Root, "CLAUDE.md"), filepath.Base),
		docs(filepath.Join(projects, "*", "*memory", "*.md"), func(p string) string {
			r, _ := filepath.Rel(projects, p)
			return r
		})...)
	baks, _ := filepath.Glob(filepath.Join(s.Root, "settings.json.bak*"))
	for _, p := range baks {
		cfg.Backups = append(cfg.Backups, item(p))
	}
	return cfg
}

func docs(pattern string, name func(path string) string) []Doc {
	files, _ := filepath.Glob(pattern)
	var out []Doc
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			out = append(out, Doc{Name: name(f), Path: f, Text: string(b)})
		}
	}
	return out
}

// commands lists the .md files under dir, named by their path in it
// without .md, as Claude Code names slash commands.
func commands(dir string) []Doc {
	var out []Doc
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil {
			r, _ := filepath.Rel(dir, p)
			out = append(out, Doc{Name: strings.TrimSuffix(r, ".md"), Path: p, Text: string(b)})
		}
		return nil
	})
	return out
}
