package view

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lang315/claude-manager/store"
)

var configTabs = []string{"Settings", "Hooks", "MCP servers", "Plugins", "Skills", "Commands", "Memory", "Backups"}

type configPage struct {
	cfg     store.Config
	tab     int
	open    map[string]*bool // tree items expanded, by path
	doc     string           // Path of the doc shown on the Skills, Commands and Memory tabs
	md      markdown.Options
	backups ui.DataTableState[store.Item, string]
	asking  bool
	pending []store.Item
}

func (p *configPage) init() {
	p.open = map[string]*bool{}
	p.backups.Key = func(x *store.Item) string { return x.Path }
	p.backups.NoSearchBar = true
	p.backups.Columns = []ui.DataColumn[store.Item]{
		{TableColumn: ui.TableColumn{Title: "File", Width: 320}, Text: func(x *store.Item) string { return filepath.Base(x.Path) }},
		{TableColumn: ui.TableColumn{Title: "Modified", Width: 140}, Text: func(x *store.Item) string { return fmtTime(x.Modified) },
			Compare: func(x, y *store.Item) int { return x.Modified.Compare(y.Modified) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 90, Align: ui.End}, Text: func(x *store.Item) string { return humanSize(x.Size) }},
	}
}

func (p *configPage) load(a *App) {
	a.run(func() func() {
		cfg := a.Store.Config()
		return func() {
			p.cfg = cfg
			p.backups.Reset()
		}
	})
}

func (p *configPage) view(c *ui.Context, a *App, pal *palette) {
	for _, e := range p.cfg.Errs {
		ui.Text(c, e).TextColor(c.Theme().Danger)
	}
	ui.Tabs(c, &p.tab, configTabs...)
	face(c, pal).Grow(1).Padding(12).Children(func() { p.body(c, a, pal) })
}

func (p *configPage) body(c *ui.Context, a *App, pal *palette) {
	switch p.tab {
	case 0:
		p.tree(c, "settings", p.cfg.Settings)
	case 1:
		p.tree(c, "hooks", p.cfg.Settings["hooks"])
	case 2:
		p.tree(c, "mcp", p.cfg.MCP)
	case 3:
		p.tree(c, "plugins", p.cfg.Plugins)
	case 4:
		p.docs(c, p.cfg.Skills, "No skills in skills/")
	case 5:
		p.docs(c, p.cfg.Commands, "No commands in commands/")
	case 6:
		p.docs(c, p.cfg.Memory, "No CLAUDE.md or memory files")
	case 7:
		p.backupTable(c, a, pal)
	}
}

func (p *configPage) tree(c *ui.Context, root string, v any) {
	if empty(v) {
		ui.Text(c, "Nothing configured").TextColor(c.Theme().TextMuted)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Tree(c, func() { p.node(c, root, "", v, false) })
	})
}

// empty reports a nil value or an empty object or array; a nil map inside an
// any is not == nil.
func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}

// node builds a JSON value as tree items; the root ("" key) builds only its
// children. A click on an item's label opens it, as its arrow does. Scalars
// are masked when masked is set (an ancestor is secret), when their key is
// secret, or, on the MCP tab, when they sit anywhere under env or headers.
func (p *configPage) node(c *ui.Context, path, key string, v any, masked bool) {
	item := func(label string, children func()) {
		open := p.opened(path)
		if ui.TreeItem(c.Key(path), label, open, children).Clicked() {
			*open = !*open
		}
	}
	mcp := path == "mcp" || strings.HasPrefix(path, "mcp/")
	masked = masked || secretKey.MatchString(key) || (key == "env" || key == "headers") && mcp
	switch x := v.(type) {
	case map[string]any:
		build := func() {
			for _, k := range slices.Sorted(maps.Keys(x)) {
				p.node(c, path+"/"+k, k, x[k], masked)
			}
		}
		if key == "" {
			build()
			return
		}
		item(key, build)
	case []any:
		build := func() {
			for i, e := range x {
				// "--api-key", "abc123": the value is its own element, so the flag before it decides.
				val := masked || mcp && key == "args" && i > 0 && secretFlag(x[i-1])
				p.node(c, fmt.Sprintf("%s/%d", path, i), strconv.Itoa(i), e, val)
			}
		}
		if key == "" {
			build()
			return
		}
		item(fmt.Sprintf("%s [%d]", key, len(x)), build)
	default:
		val := scalar(key, x)
		if masked {
			val = mask
		}
		ui.TreeItem(c.Key(path), key+": "+val, nil, nil)
	}
}

func (p *configPage) opened(path string) *bool {
	b := p.open[path]
	if b == nil {
		b = new(bool)
		p.open[path] = b
	}
	return b
}

const mask = "••••••"

// secretKey matches keys whose values are credentials, which the tree hides.
var secretKey = regexp.MustCompile(`(?i)token|secret|password|passwd|passphrase|api[-_]?key|access[-_]?key|private[-_]?key|_key$|^key$|auth|credential|cookie|bearer|_pat$|^pat$|dsn|webhook|headers`)

// secretValue matches credentials inside a string value, whatever its key.
// shortcut: known shapes only (bearer/basic, sk-, gh?_, github_pat_, xox?-, AKIA, user:pass@, a query
// parameter or --flag=value named like a secret); a token without a known shape still shows, as does a
// bare value after a separate flag element outside an MCP "args" array (inside one it is masked, see
// node). Match more shapes if one leaks.
var secretValue = regexp.MustCompile(`(?i)bearer\s+\S+|basic\s+\S+|\bsk-[\w-]{8,}|\bgh[pousr]_\w+|github_pat_\w+|xox[abposr]-\S+|\bAKIA[0-9A-Z]{16}|://[^/\s:@]+:[^@\s]+@|[?&][^=&\s]*(key|token|secret|auth|sig|pass)[^=&\s]*=[^&\s]+|--?[\w-]*(key|token|secret|password|auth)[\w-]*=\S+`)

// secretFlag reports whether v is a command-line flag named like a secret.
func secretFlag(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "-") && secretKey.MatchString(strings.TrimLeft(s, "-"))
}

func scalar(key string, v any) string {
	if s, ok := v.(string); ok {
		if secretKey.MatchString(key) || secretValue.MatchString(s) {
			return mask
		}
		return strconv.Quote(s)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (p *configPage) docs(c *ui.Context, ds []store.Doc, none string) {
	if len(ds) == 0 {
		ui.Text(c, none).TextColor(c.Theme().TextMuted)
		return
	}
	ui.Row(c).Grow(1).Gap(12).Children(func() {
		ui.Sidebar(c, &p.doc, func() {
			for _, d := range ds {
				ui.SidebarItem(c.Key(d.Path), d.Path, nil, d.Name)
			}
		}).Width(280)
		ui.Scroll(c).Grow(1).Children(func() {
			shown := false
			for _, d := range ds {
				if d.Path == p.doc {
					markdown.View(c, d.Text, &p.md)
					shown = true
				}
			}
			if !shown {
				ui.Text(c, "Choose a file to read it.").TextColor(c.Theme().TextMuted)
			}
		})
	})
}

func (p *configPage) backupTable(c *ui.Context, a *App, pal *palette) {
	targets := picked(&p.backups, p.cfg.Backups)
	ui.Row(c).Gap(8).Children(func() {
		if orangeKey(c, pal, "Move to Trash…", len(targets) == 0 || a.busy > 0).Clicked() {
			p.pending = nil
			for _, x := range targets {
				p.pending = append(p.pending, *x)
			}
			p.asking = true
		}
	})
	ui.DataTable(c, &p.backups, p.cfg.Backups).Grow(1)
	title := fmt.Sprintf("Move %s to the Trash?", plural(len(p.pending), "settings backup"))
	if ui.AlertDialog(c, &p.asking, title, "settings.json itself is not touched. You can put them back from the Trash.", "Cancel", "Move to Trash") == 1 {
		pending := p.pending
		a.trash(func() []string {
			var out []string
			for _, x := range pending {
				out = append(out, x.Path)
			}
			return out
		})
	}
}
