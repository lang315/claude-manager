package view

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

type overview struct {
	rows  []store.Entry
	max   int64
	err   string
	table ui.DataTableState[store.Entry, string]
}

func (o *overview) init() {
	bySize := func(x, y *store.Entry) int { return cmp.Compare(x.Size, y.Size) }
	o.table.Key = func(e *store.Entry) string { return e.Name }
	o.table.NoSearchBar = true
	o.table.Columns = []ui.DataColumn[store.Entry]{
		{TableColumn: ui.TableColumn{Title: "Name", Width: 240}, Text: func(e *store.Entry) string { return e.Name },
			Compare: func(x, y *store.Entry) int { return cmp.Compare(x.Name, y.Name) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 90}, Text: func(e *store.Entry) string { return humanSize(e.Size) }, Compare: bySize},
		{TableColumn: ui.TableColumn{Title: "Share", Width: 200}, Text: func(e *store.Entry) string { return humanSize(e.Size) }, Compare: bySize,
			Cell: func(c *ui.Context, e *store.Entry) {
				ui.Meter(c, float64(e.Size), 0, float64(max(o.max, 1)), nil).Label(e.Name + " share").Grow(1)
			}},
		{TableColumn: ui.TableColumn{Title: "Files", Width: 80}, Text: func(e *store.Entry) string { return strconv.Itoa(e.Files) },
			Compare: func(x, y *store.Entry) int { return cmp.Compare(x.Files, y.Files) }},
		{TableColumn: ui.TableColumn{Title: "Cleanable", Width: 90}, Text: func(e *store.Entry) string {
			if e.Cleanable {
				return "yes"
			}
			return ""
		}},
	}
}

func (o *overview) load(a *App) {
	a.run(func() func() {
		rows, err := a.Store.Scan()
		return func() {
			o.rows, o.err, o.max = rows, errText(err), 0
			for _, e := range rows {
				o.max = max(o.max, e.Size)
			}
			o.table.Reset()
		}
	})
}

func (o *overview) view(c *ui.Context, a *App) {
	if o.err != "" {
		ui.Text(c, o.err).TextColor(c.Theme().Danger)
	}
	target := ""
	if e := o.table.Current(); e != nil {
		target = screenOf(e)
	}
	if ui.Button(c, "Open").Disabled(target == "").Clicked() {
		a.page = target
	}
	ui.DataTable(c, &o.table, o.rows).Grow(1)
}

// screenOf returns the screen that shows an entry, "" for none.
func screenOf(e *store.Entry) string {
	switch {
	case e.Name == "projects":
		return "sessions"
	case e.Name == "settings.json", e.Name == "CLAUDE.md", e.Name == "skills", e.Name == "plugins",
		e.Name == "commands", strings.HasPrefix(e.Name, "settings.json.bak"):
		return "config"
	case e.Cleanable:
		return "cleanup"
	}
	return ""
}
