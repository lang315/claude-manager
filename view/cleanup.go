package view

import (
	"fmt"

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
}

func (p *cleanup) set(groups []store.Group) {
	p.groups = groups
	if len(p.on) != len(groups) {
		p.on = make([]bool, len(groups))
	}
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
				p.set(groups)
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

func (p *cleanup) view(c *ui.Context, a *App) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Older than")
		if ui.NumberInput(c, &p.days, 1, 3650, 1).Width(140).Label("Days").Changed() {
			p.load(a)
		}
		ui.Text(c, "days")
	})
	var total int64
	count := 0
	ui.Column(c).Border(1, t.Border).Radius(t.Radius).Children(func() {
		for i, g := range p.groups {
			if i > 0 {
				ui.Divider(c)
			}
			ui.Row(c.Key(i)).Gap(12).PaddingX(12).PaddingY(8).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Checkbox(c, &p.on[i], g.Name)
					if i < len(groupNotes) {
						ui.Text(c, groupNotes[i]).FontSize(12).TextColor(t.TextMuted).PaddingX(t.Space(6))
					}
				})
				ui.Text(c, plural(len(g.Items), "item")).TextColor(t.TextMuted)
				ui.Text(c, humanSize(g.Size)).Width(80).TextAlign(ui.End)
			})
			if p.on[i] {
				total += g.Size
				count += len(g.Items)
			}
		}
	})
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Textf(c, "Selected: %s · %s", plural(count, "item"), humanSize(total)).Bold()
		ui.Spacer(c)
		if ui.PrimaryButton(c, "Move to Trash…").Disabled(count == 0 || a.busy > 0).Clicked() {
			p.pending, p.size = nil, 0
			seen := map[string]bool{} // groups overlap: count each path once
			for i, g := range p.groups {
				if p.on[i] {
					for _, it := range g.Items {
						if !seen[it.Path] {
							seen[it.Path] = true
							p.pending = append(p.pending, it.Path)
							p.size += it.Size
						}
					}
				}
			}
			p.asking = true
		}
	})
	msg := humanSize(p.size) + " moves to the Trash. Live sessions and anything changed in the last 10 minutes are left alone. You can put it back from the Trash."
	if ui.AlertDialog(c, &p.asking, fmt.Sprintf("Move %s to the Trash?", plural(len(p.pending), "item")), msg, "Cancel", "Move to Trash") == 1 {
		pending := p.pending
		clear(p.on)
		a.trash(func() []string { return pending })
	}
}
