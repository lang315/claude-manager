package view

import (
	"cmp"
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
	cfg      store.Config
	tab      int
	settings tree
	mcp      tree
	hooks    []hookRow
	hookList ui.ListState
	skills   docList
	commands docList
	memory   docList
	md       markdown.Options
	most     int64 // the largest plugin, the plugins' scale
	backups  ui.DataTableState[store.Item, string]
	asking   bool
	pending  []store.Item
}

func (p *configPage) init() {
	p.backups.Key = func(x *store.Item) string { return x.Path }
	p.backups.NoSearchBar = true
	p.backups.Columns = []ui.DataColumn[store.Item]{
		{TableColumn: ui.TableColumn{Title: "File", MinWidth: 200}, Text: func(x *store.Item) string { return filepath.Base(x.Path) }},
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
			p.settings.set("settings", cfg.Settings)
			p.mcp.set("mcp", cfg.MCP)
			p.hooks = hookRows(cfg.Settings["hooks"])
			p.skills.set(cfg.Skills, false)
			p.commands.set(cfg.Commands, false)
			p.memory.set(cfg.Memory, true)
			p.most = 0
			for _, x := range cfg.Plugins {
				p.most = max(p.most, x.Size)
			}
			p.backups.Checked.Clear() // the files checked may be gone
			p.backups.Reset()
		}
	})
}

func (p *configPage) view(c *ui.Context, a *App, pal *palette) {
	for _, e := range p.cfg.Errs {
		ui.Text(c, e).TextColor(c.Theme().Danger)
	}
	ui.Tabs(c, &p.tab, configTabs...)
	switch p.tab {
	case 0:
		sheet(c, pal, "Settings", a.root+"/settings.json", func() {
			p.settings.view(c, pal, "Nothing configured in settings.json.")
		})
	case 1:
		events := 0
		for i, h := range p.hooks {
			if i == 0 || h.event != p.hooks[i-1].event {
				events++
			}
		}
		sheet(c, pal, "Hooks", plural(len(p.hooks), "hook")+" on "+plural(events, "event"), func() { p.hookTable(c, pal) })
	case 2:
		sheet(c, pal, "MCP servers", plural(len(p.mcp.roots), "server")+" · env and headers hidden", func() {
			p.mcp.view(c, pal, "No MCP servers in settings.json or ~/.claude.json.")
		})
	case 3:
		p.plugins(c, pal)
	case 4:
		sheet(c, pal, "Skills", plural(len(p.cfg.Skills), "skill")+" in "+a.root+"/skills", func() {
			p.skills.view(c, pal, &p.md, func(d *store.Doc) string { return d.Name },
				"No skills yet. A skill is a folder in "+a.root+"/skills with a SKILL.md in it.")
		})
	case 5:
		sheet(c, pal, "Commands", plural(len(p.cfg.Commands), "command")+" in "+a.root+"/commands", func() {
			p.commands.view(c, pal, &p.md, func(d *store.Doc) string { return "/" + d.Name },
				"No commands yet. Each .md file in "+a.root+"/commands is a slash command.")
		})
	case 6:
		sheet(c, pal, "Memory", plural(len(p.cfg.Memory), "file"), func() {
			p.memory.view(c, pal, &p.md, func(d *store.Doc) string { return d.Name },
				"No CLAUDE.md and no project memory yet.")
		})
	case 7:
		sheet(c, pal, "Settings backups", plural(len(p.cfg.Backups), "backup")+" of settings.json", func() { p.backupTable(c, a, pal) })
	}
}

// sheet is a tab's face, filling the room under the tabs: a caption, a
// rule, then what fn builds.
func sheet(c *ui.Context, pal *palette, title, note string, fn func()) {
	face(c, pal).Grow(1).MinHeight(0).Children(func() {
		head(c, pal, title, note)
		ui.Divider(c)
		fn()
	})
}

// blank says why a face is empty, and what would fill it.
func blank(c *ui.Context, pal *palette, s string) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Padding(24).Children(func() {
		ui.Text(c, s).TextColor(pal.muted).TextAlign(ui.Center)
	})
}

// tree is a JSON value laid out for an OutlineTable: a node per path.
type tree struct {
	state ui.OutlineState[string]
	at    int // the row chosen, -1 for none
	nodes map[string]treeNode
	roots []string
}

// treeNode is a key and its value as shown: a scalar, masked when secret,
// or the count of a container's children, whose paths kids holds.
type treeNode struct {
	key, value string
	kids       []string
}

func (t *tree) set(root string, v any) {
	t.nodes = map[string]treeNode{}
	t.roots = t.add(root, "", v, false).kids
	if t.state.List.Selected == nil {
		t.at = -1
		t.state.List.Selected = &t.at
	}
}

// add adds the node of v at path, and those under it. Scalars are masked
// when masked is set (an ancestor is secret), when their key is secret, or,
// on the MCP tab, when they sit anywhere under env or headers.
func (t *tree) add(path, key string, v any, masked bool) treeNode {
	mcp := path == "mcp" || strings.HasPrefix(path, "mcp/") || strings.Contains(path, "/mcpServers/")
	masked = masked || secretKey.MatchString(key) || (key == "env" || key == "headers") && mcp
	n := treeNode{key: key}
	switch x := v.(type) {
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(x)) {
			n.kids = append(n.kids, path+"/"+k)
			t.add(path+"/"+k, k, x[k], masked)
		}
		n.value = plural(len(x), "item")
		if mcp && strings.Count(path, "/") == 1 {
			n.value = mcpSummary(x)
		}
	case []any:
		for i, e := range x {
			// "--api-key", "abc123": the value is its own element, so the flag before it decides.
			val := masked || mcp && key == "args" && i > 0 && secretFlag(x[i-1])
			p := fmt.Sprintf("%s/%d", path, i)
			n.kids = append(n.kids, p)
			t.add(p, strconv.Itoa(i), e, val)
		}
		n.value = plural(len(x), "item")
	default:
		n.value = scalar(key, x)
		if masked {
			n.value = mask
		}
	}
	t.nodes[path] = n
	return n
}

// view shows the tree in two columns, as a property list editor does: a
// click on an arrow, a double click or Enter on a row opens it.
func (t *tree) view(c *ui.Context, pal *palette, none string) {
	if len(t.roots) == 0 {
		blank(c, pal, none)
		return
	}
	chosen := ""
	if t.at >= 0 && t.at < t.state.Rows() {
		chosen = t.state.Item(t.at)
	}
	cols := []ui.TableColumn{{Title: "Key", Width: 260, MinWidth: 120}, {Title: "Value", MinWidth: 120}}
	table := ui.OutlineTable(c, &t.state, cols, t.roots, func(p string) []string { return t.nodes[p].kids }, func(p string, col int) {
		n := t.nodes[p]
		if col == 0 {
			ui.Text(c, n.key).SingleLine()
			return
		}
		v := ui.Text(c, n.value).SingleLine()
		switch {
		case n.kids != nil && p != chosen:
			v.TextColor(pal.muted)
		case n.kids == nil && len(n.value) > 60:
			v.Tooltip(clip(n.value))
		}
	}).Grow(1).MinHeight(0)
	if table.Submitted() && t.at >= 0 && t.at < t.state.Rows() {
		if p := t.state.Item(t.at); t.nodes[p].kids != nil {
			if t.state.Open.Has(p) {
				t.state.Open.Remove(p)
			} else {
				t.state.Open.Add(p)
			}
		}
	}
}

// mcpSummary sums up an MCP server in a line: how Claude Code reaches it,
// and its URL or the command that starts it, masked as its fields are.
func mcpSummary(m map[string]any) string {
	str := func(k string) string { s, _ := m[k].(string); return s }
	if u := str("url"); u != "" {
		return cmp.Or(str("type"), "http") + " · " + scalar("url", u)
	}
	var line []string
	if s := str("command"); s != "" {
		line = append(line, scalar("command", s))
	}
	args, _ := m["args"].([]any)
	for i, a := range args {
		s := scalar(strconv.Itoa(i), a)
		if i > 0 && secretFlag(args[i-1]) {
			s = mask
		}
		line = append(line, s)
	}
	return cmp.Or(str("type"), "stdio") + " · " + strings.Join(line, " ")
}

const mask = "••••••"

// secretKey matches keys whose values are credentials, which the tree hides.
var secretKey = regexp.MustCompile(`(?i)token|secret|password|passwd|passphrase|api[-_]?key|access[-_]?key|private[-_]?key|_key$|^key$|auth|credential|cookie|bearer|_pat$|^pat$|dsn|webhook|headers`)

// secretValue matches credentials inside a string value, whatever its key.
// shortcut: known shapes only (bearer/basic, sk-, gh?_, github_pat_, xox?-, AKIA, user:pass@, a query
// parameter or --flag=value named like a secret, a "Name: value" header named like one); a token without a known shape still shows, as does a
// bare value after a separate flag element outside an MCP "args" array (inside one it is masked, see
// tree.add). Match more shapes if one leaks.
var secretValue = regexp.MustCompile(`(?i)bearer\s+\S+|basic\s+\S+|\bsk-[\w-]{8,}|\bgh[pousr]_\w+|github_pat_\w+|xox[abposr]-\S+|\bAKIA[0-9A-Z]{16}|://[^/\s:@]+:[^@\s]+@|[?&][^=&\s]*(key|token|secret|auth|sig|pass)[^=&\s]*=[^&\s]+|--?[\w-]*(key|token|secret|password|auth)[\w-]*=\S+|(^|[=\s])[\w-]*(key|token|secret|pass|auth|cookie|sig|credential)[\w-]*\s*:\s*\S`)

// secretFlag reports whether v is a command-line flag named like a secret.
func secretFlag(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "-") && secretKey.MatchString(strings.TrimLeft(s, "-"))
}

// scalar shows a JSON scalar: a string as it is ("" when empty), the rest
// as JSON, and a credential as the mask.
func scalar(key string, v any) string {
	if s, ok := v.(string); ok {
		switch {
		case secretKey.MatchString(key) || secretValue.MatchString(s):
			return mask
		case s == "":
			return `""`
		}
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// hookRow is one hook: the event that runs it, the tools it matches, and
// what it runs.
type hookRow struct{ event, matcher, command, timeout string }

// hookEvents is the order a session meets the events in.
var hookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse",
	"PostToolUseFailure", "Notification", "SubagentStart", "SubagentStop", "Stop", "StopFailure", "TeammateIdle",
	"PreCompact", "PostCompact", "SessionEnd"}

// hookRows lists the hooks of settings.json's "hooks", event by event, in
// the order a session meets them; events it does not know come last.
func hookRows(v any) []hookRow {
	events, _ := v.(map[string]any)
	names := slices.SortedFunc(maps.Keys(events), func(a, b string) int {
		at := func(e string) int {
			if i := slices.Index(hookEvents, e); i >= 0 {
				return i
			}
			return len(hookEvents)
		}
		return cmp.Or(cmp.Compare(at(a), at(b)), cmp.Compare(a, b))
	})
	var out []hookRow
	for _, e := range names {
		groups, _ := events[e].([]any)
		for _, g := range groups {
			g, _ := g.(map[string]any)
			matcher, _ := g["matcher"].(string)
			hooks, _ := g["hooks"].([]any)
			for _, h := range hooks {
				h, _ := h.(map[string]any)
				kind, _ := h["type"].(string)
				run, _ := h["command"].(string)
				if run == "" {
					run, _ = h["prompt"].(string)
				}
				run = scalar("command", strings.Join(strings.Fields(run), " "))
				if kind != "" && kind != "command" {
					run = kind + ": " + run
				}
				r := hookRow{event: e, matcher: matcher, command: run}
				if t, ok := h["timeout"].(float64); ok {
					r.timeout = strconv.FormatFloat(t, 'f', -1, 64) + " s"
				}
				out = append(out, r)
			}
		}
	}
	return out
}

// hookTable lists the hooks, the event named on its first row only, so
// they read as one block per event.
func (p *configPage) hookTable(c *ui.Context, pal *palette) {
	if len(p.hooks) == 0 {
		blank(c, pal, `No hooks. A hook runs a command at a point in a session, set under "hooks" in settings.json.`)
		return
	}
	cols := []ui.TableColumn{{Title: "Event", Width: 170}, {Title: "Matcher", Width: 160}, {Title: "Command", MinWidth: 160}, {Title: "Timeout", Width: 80, Align: ui.End}}
	ui.Table(c, &p.hookList, cols, len(p.hooks), func(i, col int) {
		h := p.hooks[i]
		switch col {
		case 0:
			if i == 0 || p.hooks[i-1].event != h.event {
				ui.Text(c, h.event).SingleLine()
			}
		case 1:
			ui.Text(c, h.matcher).TextColor(pal.muted).SingleLine().Tooltip(h.matcher)
		case 2:
			ui.Text(c, h.command).Font("monospace").FontSize(12).SingleLine().Tooltip(clip(h.command))
		case 3:
			ui.Text(c, h.timeout).TextColor(pal.muted).FontFeatures("tnum").SingleLine()
		}
	}).Grow(1).MinHeight(0)
}

// plugins lists the installed plugins on one scale, largest first, and the
// marketplaces they come from.
func (p *configPage) plugins(c *ui.Context, pal *palette) {
	var total int64
	for _, x := range p.cfg.Plugins {
		total += x.Size
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(16).Children(func() {
			face(c, pal).Children(func() {
				head(c, pal, "Installed plugins", plural(len(p.cfg.Plugins), "plugin")+" · "+humanSize(total))
				if len(p.cfg.Plugins) == 0 {
					ui.Divider(c)
					blank(c, pal, "No plugins installed. Claude Code's /plugin command installs them.")
				}
				for _, x := range p.cfg.Plugins {
					ui.Divider(c).MarginX(16)
					ui.Row(c.Key(x.Name + "@" + x.Marketplace)).Gap(14).PaddingX(16).PaddingY(8).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Width(260).Gap(1).Children(func() {
							name := ui.Text(c, x.Name).SingleLine()
							if x.Off {
								name.TextColor(pal.muted)
							}
							ui.Text(c, strings.Join(slices.DeleteFunc([]string{x.Marketplace, x.Version}, func(s string) bool { return s == "" }), " · ")).
								FontSize(12).TextColor(pal.muted).SingleLine()
						})
						scale(c, pal, x.Size, 0, p.most).Grow(1)
						ui.Text(c, humanSize(x.Size)).Width(72).TextAlign(ui.End).FontFeatures("tnum").SingleLine()
						off := ""
						if x.Off {
							off = "Off"
						}
						ui.Text(c, off).FontSize(12).TextColor(pal.muted).Width(28).TextAlign(ui.End).SingleLine().
							Tooltip("Turned off in settings.json")
					})
				}
			})
			face(c, pal).Children(func() {
				head(c, pal, "Marketplaces", plural(len(p.cfg.Marketplaces), "marketplace"))
				for _, m := range p.cfg.Marketplaces {
					ui.Divider(c).MarginX(16)
					ui.Row(c.Key(m.Name)).Gap(14).PaddingX(16).PaddingY(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, m.Name).Width(260).SingleLine()
						ui.Text(c, scalar("url", m.Source)).TextColor(pal.muted).Grow(1).Basis(0).MinWidth(0).SingleLine()
						updated := ""
						if !m.Updated.IsZero() {
							updated = fmtTime(m.Updated)
						}
						ui.Text(c, updated).FontSize(12).TextColor(pal.muted).Width(130).TextAlign(ui.End).SingleLine()
					})
				}
			})
		})
	})
}

// docList is the Skills, Commands or Memory tab: its files listed, under a
// header per project on Memory, and the file chosen read beside them.
type docList struct {
	list ui.ListState
	at   int    // the row chosen
	path string // the file chosen, kept across rescans
	rows []docRow
}

// docRow is a file's row, or, with no doc, the header of a project's files.
type docRow struct {
	doc   *store.Doc
	group string // the header's project path
	count int    // the header's files
}

func (d *docList) set(ds []store.Doc, grouped bool) {
	d.rows = d.rows[:0]
	h := -1 // the header of the project being listed
	for i := range ds {
		if grouped && (i == 0 || ds[i].Group != ds[i-1].Group) {
			h = len(d.rows)
			d.rows = append(d.rows, docRow{group: ds[i].Group})
		}
		if h >= 0 {
			d.rows[h].count++
		}
		d.rows = append(d.rows, docRow{doc: &ds[i]})
	}
}

// groupName names a project's header; ~/.claude's own CLAUDE.md, which
// every project reads, heads the list as "All projects".
func groupName(g string) string {
	if g == "" {
		return "All projects"
	}
	return shortPath(g)
}

func (d *docList) view(c *ui.Context, pal *palette, md *markdown.Options, title func(*store.Doc) string, none string) {
	if len(d.rows) == 0 {
		blank(c, pal, none)
		return
	}
	file := func(r docRow) bool { return r.doc != nil }
	// The choice stays on its file as rows come and go; the first file
	// when it is gone, so the reader is never empty.
	if d.at = slices.IndexFunc(d.rows, func(r docRow) bool { return file(r) && r.doc.Path == d.path }); d.at < 0 {
		d.at = slices.IndexFunc(d.rows, file)
	}
	d.list.Selected = &d.at
	d.list.Header = func(i int) bool { return !file(d.rows[i]) }
	d.list.Key = func(i int) any {
		if r := d.rows[i]; file(r) {
			return r.doc.Path
		}
		return "group " + d.rows[i].group
	}
	d.list.Label = func(i int) string {
		if r := d.rows[i]; file(r) {
			return title(r.doc)
		}
		return groupName(d.rows[i].group)
	}
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.List(c, &d.list, len(d.rows), func(i int) { d.row(c, pal, i, title) }).Width(300).Padding(6, 8)
		d.path = d.rows[d.at].doc.Path
		ui.Box(c).Width(1).Background(pal.rule)
		d.read(c, pal, md, title(d.rows[d.at].doc), d.rows[d.at].doc)
	})
}

func (d *docList) row(c *ui.Context, pal *palette, i int, title func(*store.Doc) string) {
	r := d.rows[i]
	if r.doc == nil {
		top := float32(14)
		if i == 0 {
			top = 6
		}
		ui.Row(c).Background(pal.face).Padding(top, 8, 4, 8).Gap(8).AlignItems(ui.Center).Children(func() {
			name := ui.Text(c, groupName(r.group)).FontSize(12).Bold().TextColor(pal.muted).Grow(1).Basis(0).MinWidth(0).SingleLine()
			if r.group != "" {
				name.Tooltip(tilde(r.group))
			}
			ui.Text(c, strconv.Itoa(r.count)).FontSize(12).TextColor(pal.muted).FontFeatures("tnum")
		})
		return
	}
	sub := pal.muted
	if i == d.at {
		sub = pal.selText.Alpha(0.75)
	}
	ui.Column(c).PaddingX(8).PaddingY(6).Gap(1).Children(func() {
		ui.Text(c, title(r.doc)).SingleLine()
		if r.doc.Desc != "" {
			ui.Text(c, r.doc.Desc).FontSize(12).TextColor(sub).SingleLine()
		}
	})
}

// read shows a file: its name and where it is, its front matter as fields,
// and its body.
func (d *docList) read(c *ui.Context, pal *palette, md *markdown.Options, title string, doc *store.Doc) {
	ui.Scroll(c.Key(doc.Path)).Grow(1).Basis(0).MinWidth(0).Children(func() {
		ui.Column(c).Padding(16, 24).Gap(14).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(15).Bold()
				ui.Row(c).Children(func() {
					ui.Text(c, tilde(doc.Path)).FontSize(12).TextColor(pal.muted).Shrink(1).MinWidth(0).SingleLine().Tooltip(doc.Path)
					ui.Text(c, " · "+humanSize(doc.Size)+" · "+fmtTime(doc.Modified)).FontSize(12).TextColor(pal.muted).FontFeatures("tnum").Shrink(0).SingleLine()
				})
			})
			if len(doc.Fields) > 0 {
				ui.Column(c).Gap(6).Children(func() {
					for _, f := range doc.Fields {
						ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
							ui.Text(c, f[0]).FontSize(12).TextColor(pal.muted).Width(110).SingleLine()
							ui.Text(c, f[1]).Grow(1).Basis(0).MinWidth(0)
						})
					}
				})
				ui.Divider(c)
			}
			markdown.View(c, doc.Body, md)
		})
	})
}

func (p *configPage) backupTable(c *ui.Context, a *App, pal *palette) {
	if len(p.cfg.Backups) == 0 {
		blank(c, pal, "No backups of settings.json.")
		return
	}
	targets := picked(&p.backups)
	var size int64
	for _, x := range targets {
		size += x.Size
	}
	ui.DataTable(c, &p.backups, p.cfg.Backups).Grow(1).MinHeight(0)
	ui.Divider(c)
	ui.Row(c).Gap(12).PaddingX(16).PaddingY(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Selected").TextColor(pal.muted)
		ui.Textf(c, "%s · %s", plural(len(targets), "file"), humanSize(size)).Bold().FontFeatures("tnum")
		ui.Spacer(c)
		if orangeKey(c, pal, "Move to Trash…", len(targets) == 0 || a.busy > 0).Clicked() {
			p.pending = nil
			for _, x := range targets {
				p.pending = append(p.pending, *x)
			}
			p.asking = true
		}
	})
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
