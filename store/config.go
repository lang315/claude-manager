package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Doc is a Markdown file: its YAML front matter, if any, as fields, and the
// rest as its body.
type Doc struct {
	Item
	Name   string      // the skill, the command, or the memory file in its project
	Group  string      // a memory file's project path, "" for ~/.claude/CLAUDE.md and the rest
	Desc   string      // its description field, or its first line of text
	Fields [][2]string // the front matter's top-level keys and values, in order
	Body   string
}

// Plugin is an installed plugin, with the space its installs take.
type Plugin struct {
	Name, Marketplace, Version string
	Off                        bool // settings.json's enabledPlugins sets it to false
	Updated                    time.Time
	Size                       int64
}

// Marketplace is a plugin marketplace Claude Code knows.
type Marketplace struct {
	Name, Source string // Source is its repo, URL or path
	Updated      time.Time
}

// Config is what Claude Code is configured with. The app only reads it.
type Config struct {
	Settings     map[string]any // settings.json
	MCP          map[string]any // mcpServers of settings.json and .claude.json
	Plugins      []Plugin       // plugins/installed_plugins.json, largest first
	Marketplaces []Marketplace  // plugins/known_marketplaces.json, by name
	Skills       []Doc          // skills/*/SKILL.md
	Commands     []Doc          // commands/**/*.md
	Memory       []Doc          // CLAUDE.md, then projects/*/*memory/*.md by project, MEMORY.md first
	Backups      []Item         // settings.json.bak*
	Errs         []string       // files that exist but could not be read
}

func (s *Store) Config() Config {
	var cfg Config
	load := func(path string, v any) bool {
		b, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				cfg.Errs = append(cfg.Errs, err.Error())
			}
			return false
		}
		if err := json.Unmarshal(b, v); err != nil {
			cfg.Errs = append(cfg.Errs, fmt.Sprintf("%s: %v", path, err))
			return false
		}
		return true
	}
	load(filepath.Join(s.Root, "settings.json"), &cfg.Settings)
	// .claude.json holds much else (projects, caches); only its servers are decoded.
	var claudeJSON struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	load(s.ClaudeJSON, &claudeJSON)
	cfg.MCP = map[string]any{}
	if servers, ok := cfg.Settings["mcpServers"].(map[string]any); ok {
		maps.Copy(cfg.MCP, servers)
	}
	maps.Copy(cfg.MCP, claudeJSON.MCPServers)

	var installed struct {
		Plugins map[string][]struct{ InstallPath, Version, LastUpdated string }
	}
	load(filepath.Join(s.Root, "plugins", "installed_plugins.json"), &installed)
	enabled, _ := cfg.Settings["enabledPlugins"].(map[string]any)
	for id, installs := range installed.Plugins {
		p := Plugin{Name: id, Off: enabled[id] == false}
		if i := strings.LastIndexByte(id, '@'); i > 0 {
			p.Name, p.Marketplace = id[:i], id[i+1:]
		}
		seen := map[string]bool{}
		for _, in := range installs {
			if t := parseTime(in.LastUpdated); !t.Before(p.Updated) {
				p.Updated, p.Version = t, in.Version
			}
			if in.InstallPath != "" && !seen[in.InstallPath] {
				seen[in.InstallPath] = true
				size, _ := dirSize(in.InstallPath)
				p.Size += size
			}
		}
		cfg.Plugins = append(cfg.Plugins, p)
	}
	slices.SortFunc(cfg.Plugins, func(a, b Plugin) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Name, b.Name)) })

	var markets map[string]struct {
		Source      any
		LastUpdated string
	}
	load(filepath.Join(s.Root, "plugins", "known_marketplaces.json"), &markets)
	for _, name := range slices.Sorted(maps.Keys(markets)) {
		m := markets[name]
		cfg.Marketplaces = append(cfg.Marketplaces, Marketplace{Name: name, Source: source(m.Source), Updated: parseTime(m.LastUpdated)})
	}

	cfg.Skills = docs(&cfg.Errs, filepath.Join(s.Root, "skills", "*", "SKILL.md"), func(p string) (string, string) { return filepath.Base(filepath.Dir(p)), "" })
	cfg.Commands = commands(&cfg.Errs, filepath.Join(s.Root, "commands"))
	cfg.Memory = append(docs(&cfg.Errs, filepath.Join(s.Root, "CLAUDE.md"), func(p string) (string, string) { return filepath.Base(p), "" }),
		docs(&cfg.Errs, filepath.Join(s.Root, "projects", "*", "*memory", "*.md"), func(p string) (string, string) {
			dir := filepath.Dir(p)
			name := filepath.Base(p)
			if filepath.Base(dir) != "memory" {
				name = filepath.Base(dir) + "/" + name
			}
			return name, projectPath(filepath.Base(filepath.Dir(dir)))
		})...)
	index := func(d Doc) int { // MEMORY.md, the index, leads its project
		if d.Name == "MEMORY.md" {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(cfg.Memory, func(a, b Doc) int {
		return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(index(a), index(b)), cmp.Compare(a.Name, b.Name))
	})
	baks, _ := filepath.Glob(filepath.Join(s.Root, "settings.json.bak*"))
	for _, p := range baks {
		cfg.Backups = append(cfg.Backups, item(p))
	}
	return cfg
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// source returns where a marketplace comes from: its repo, URL or path.
func source(v any) string {
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"repo", "url", "path"} {
			if s, ok := m[k].(string); ok {
				return s
			}
		}
		v = m["source"]
	}
	s, _ := v.(string)
	return s
}

// docs reads the files pattern matches, adding to errs those it can't read.
func docs(errs *[]string, pattern string, name func(path string) (name, group string)) []Doc {
	files, _ := filepath.Glob(pattern)
	var out []Doc
	for _, f := range files {
		d, err := readDoc(f)
		if err != nil {
			*errs = append(*errs, err.Error())
			continue
		}
		d.Name, d.Group = name(f)
		out = append(out, d)
	}
	return out
}

// commands lists the .md files under dir, named by their path in it
// without .md, as Claude Code names slash commands, adding to errs those it
// can't read.
func commands(errs *[]string, dir string) []Doc {
	var out []Doc
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		doc, err := readDoc(p)
		if err != nil {
			*errs = append(*errs, err.Error())
			return nil
		}
		r, _ := filepath.Rel(dir, p)
		doc.Name = strings.TrimSuffix(r, ".md")
		out = append(out, doc)
		return nil
	})
	return out
}

func readDoc(path string) (Doc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, err
	}
	d := Doc{Item: item(path)}
	d.Fields, d.Body = frontMatter(string(b))
	for _, f := range d.Fields {
		if f[0] == "description" {
			d.Desc = f[1]
		}
	}
	if d.Desc == "" {
		for _, l := range strings.Split(d.Body, "\n") {
			if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#>- ")); l != "" {
				d.Desc = l
				break
			}
		}
	}
	d.Desc = FirstLine(mdLink.ReplaceAllString(d.Desc, "$1"), 200)
	return d, nil
}

// frontMatter splits the YAML block a Markdown file may open with, between
// "---" lines, into its top-level fields and the body after it. A value
// that runs on over indented lines is joined into one line.
func frontMatter(text string) (fields [][2]string, body string) {
	rest, ok := strings.CutPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "---\n")
	if !ok {
		return nil, text
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return nil, text
	}
	if _, after, ok := strings.Cut(body, "\n"); ok {
		body = after
	} else {
		body = ""
	}
	for _, line := range strings.Split(head, "\n") {
		k, v, kv := strings.Cut(line, ":")
		switch {
		case kv && k != "" && k[0] != ' ' && k[0] != '\t' && !strings.ContainsAny(k, " \t"):
			v = strings.TrimSpace(v)
			if v == ">" || v == "|" || v == ">-" || v == "|-" {
				v = ""
			}
			if u, err := strconv.Unquote(v); err == nil && strings.HasPrefix(v, `"`) {
				v = u
			}
			fields = append(fields, [2]string{k, strings.Trim(v, `'`)})
		case len(fields) > 0 && strings.TrimSpace(line) != "":
			f := &fields[len(fields)-1]
			f[1] = strings.TrimSpace(f[1] + " " + strings.TrimSpace(line))
		}
	}
	return fields, body
}

// projectPath finds the path a projects/ dir name stands for. Claude Code
// names the dir after it with every character but a letter or a digit made
// a "-", so it walks down from "/", taking at each level the entry whose
// name, made so, starts what is left, the longest one. Where the path is
// gone, the rest of the name is its last part, dashes kept.
func projectPath(name string) string {
	dir, rest := "/", name
	for rest != "" {
		if rest[0] != '-' {
			return name // not an encoded path
		}
		rest = rest[1:] // the "/" before the next name
		next, enc := "", ""
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			n := notAlnum.ReplaceAllString(e.Name(), "-")
			if len(n) > len(enc) && strings.HasPrefix(rest, n) && (len(rest) == len(n) || rest[len(n)] == '-') {
				if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() {
					next, enc = e.Name(), n
				}
			}
		}
		if next == "" {
			return filepath.Join(dir, rest)
		}
		dir, rest = filepath.Join(dir, next), rest[len(enc):]
	}
	return dir
}

// mdLink matches a Markdown link, its text in the first group.
var mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

var notAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)
