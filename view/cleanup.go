package view

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

type cleanup struct {
	days    float64
	groups  []store.Group
	on      []bool // groups checked, by index: the order of groups is fixed
	asking  bool
	pending []string
	size    int64
	gen     int // which load the groups arriving belong to

	reclaim int64            // what every group holds, each path once
	byTop   map[string]int64 // reclaim by top-level entry of the root
}

func (p *cleanup) set(root string, groups []store.Group) {
	p.groups = groups
	if len(p.on) != len(groups) {
		p.on = make([]bool, len(groups))
	}
	p.reclaim, p.byTop = 0, map[string]int64{}
	for _, it := range unique(groups, nil) {
		p.reclaim += it.Size
		if rel, err := filepath.Rel(root, it.Path); err == nil {
			p.byTop[strings.SplitN(rel, string(filepath.Separator), 2)[0]] += it.Size
		}
	}
}

// unique returns the items of the groups on (all when on is nil), each path
// once: groups overlap.
func unique(groups []store.Group, on []bool) []store.Item {
	var out []store.Item
	seen := map[string]bool{}
	for i, g := range groups {
		if on != nil && !on[i] {
			continue
		}
		for _, it := range g.Items {
			if !seen[it.Path] {
				seen[it.Path] = true
				out = append(out, it)
			}
		}
	}
	return out
}

// load finds the groups again from the sessions on disk now.
func (p *cleanup) load(a *App) {
	days := int(p.days)
	p.gen++
	gen := p.gen
	a.run(func() func() {
		projects, _ := a.Store.Projects() // fresh: a session may have started since the last scan
		groups := a.Store.Cleanup(projects, days)
		return func() {
			if gen == p.gen {
				p.set(a.Store.Root, groups)
			}
		}
	})
}

// groupNotes say what each group holds, in the order Cleanup returns them.
var groupNotes = []string{
	"Environment saved for each session",
	"Text pasted into prompts",
	"Shell state saved as a session starts; a running session keeps its own",
	"Debug logs",
	"Edit history whose session is gone",
	"Transcript files past the age above",
	"Subagent and tool-result folders whose session is gone",
	"Whole sessions with their subagents, file history and environment",
}

func (p *cleanup) view(c *ui.Context, a *App, pal *palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Older than")
		if ui.NumberInput(c, &p.days, 1, 3650, 1).Width(140).Label("Days").Changed() {
			p.load(a)
		}
		ui.Text(c, "days")
	})
	face(c, pal).Grow(1).Children(func() {
		ui.Scroll(c).Grow(1).Children(func() { p.list(c, pal, true) })
		p.footer(c, a, pal)
	})
}

// list draws the groups as rows, each with its size on one scale shared by
// all of them; notes adds what each group holds.
func (p *cleanup) list(c *ui.Context, pal *palette, notes bool) {
	var most int64
	for _, g := range p.groups {
		most = max(most, g.Size)
	}
	for i, g := range p.groups {
		if i > 0 {
			ui.Divider(c).MarginX(16)
		}
		gap := float32(14)
		if !notes {
			gap = 8 // the Overview's face is narrow
		}
		ui.Row(c.Key(i)).Gap(gap).PaddingX(16).PaddingY(9).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
				ui.Checkbox(c, &p.on[i], g.Name)
				if notes && i < len(groupNotes) {
					ui.Text(c, groupNotes[i]).FontSize(12).TextColor(pal.muted).Padding(0, 0, 0, 24).SingleLine()
				}
			})
			if notes {
				scale(c, pal, g.Size, g.Size, most).Width(160)
			}
			ui.Text(c, plural(len(g.Items), "item")).FontSize(12).TextColor(pal.muted).Width(84).TextAlign(ui.End).SingleLine()
			ui.Text(c, humanSize(g.Size)).Width(72).TextAlign(ui.End).FontFeatures("tnum").SingleLine()
		})
	}
}

// footer holds the selected total and the key that moves it to the Trash.
func (p *cleanup) footer(c *ui.Context, a *App, pal *palette) {
	var total int64
	count := 0
	for i, g := range p.groups {
		if p.on[i] {
			total += g.Size
			count += len(g.Items)
		}
	}
	ui.Divider(c)
	ui.Row(c).Gap(12).PaddingX(16).PaddingY(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Selected").TextColor(pal.muted)
		ui.Textf(c, "%s · %s", plural(count, "item"), humanSize(total)).Bold().FontFeatures("tnum")
		ui.Spacer(c)
		if orangeKey(c, pal, "Move to Trash…", count == 0 || a.busy > 0).Clicked() {
			p.pending, p.size = nil, 0
			for _, it := range unique(p.groups, p.on) {
				p.pending = append(p.pending, it.Path)
				p.size += it.Size
			}
			p.asking = true
		}
	})
}

// confirm asks before moving the checked groups; App.View draws it once, for
// the Overview and the Cleanup screen alike.
func (p *cleanup) confirm(c *ui.Context, a *App) {
	msg := humanSize(p.size) + " moves to the Trash. Live sessions and anything changed in the last 10 minutes are left alone. You can put it back from the Trash."
	if ui.AlertDialog(c, &p.asking, fmt.Sprintf("Move %s to the Trash?", plural(len(p.pending), "item")), msg, "Cancel", "Move to Trash") == 1 {
		pending := p.pending
		clear(p.on)
		a.trash(func() []string { return pending })
	}
}
