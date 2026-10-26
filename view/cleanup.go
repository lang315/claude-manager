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

func (p *cleanup) view(c *ui.Context, a *App) {
	t := c.Theme()
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, "Older than")
		if ui.NumberInput(c, &p.days, 1, 3650, 1).Width(170).Label("Days").Changed() {
			p.load(a)
		}
		ui.Text(c, "days")
	})
	var total int64
	count := 0
	for i, g := range p.groups {
		ui.Row(c.Key(i)).Gap(8).Children(func() {
			ui.Checkbox(c, &p.on[i], g.Name)
			ui.Spacer(c)
			ui.Textf(c, "%d items · %s", len(g.Items), humanSize(g.Size)).TextColor(t.TextMuted)
		})
		if p.on[i] {
			total += g.Size
			count += len(g.Items)
		}
	}
	ui.Textf(c, "Checked: %d items · %s", count, humanSize(total)).Bold()
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
	msg := humanSize(p.size) + " moves to the Trash. Live sessions and anything changed in the last 10 minutes are left alone. You can put it back from the Trash."
	if ui.AlertDialog(c, &p.asking, fmt.Sprintf("Move %d items to the Trash?", len(p.pending)), msg, "Cancel", "Move to Trash") == 1 {
		pending := p.pending
		clear(p.on)
		a.trash(func() []string { return pending })
	}
}
