package view

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

type overview struct {
	rows []store.Entry // largest first
	err  string
}

func (o *overview) load(a *App) {
	a.run(func() func() {
		rows, err := a.Store.Scan()
		return func() { o.rows, o.err = rows, errText(err) }
	})
}

func (o *overview) total() (n int64) {
	for _, e := range o.rows {
		n += e.Size
	}
	return n
}

func (o *overview) view(c *ui.Context, a *App, pal *palette) {
	if o.err != "" {
		ui.Text(c, o.err).TextColor(c.Theme().Danger)
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(16).Children(func() {
			o.dial(c, a, pal)
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				face(c, pal).Grow(1).Basis(0).MinWidth(0).Children(func() { recent(c, a, pal) })
				face(c, pal).Grow(1).Basis(0).MinWidth(0).Children(func() {
					head(c, pal, "Can be cleaned", "older than "+plural(int(a.cleanup.days), "day"))
					a.cleanup.list(c, pal, false)
					a.cleanup.footer(c, a, pal)
				})
			})
			face(c, pal).Children(func() { o.folders(c, a, pal) })
		})
	})
}

// head is a face's caption row: the legend, and a note at the right.
func head(c *ui.Context, pal *palette, title, note string) {
	ui.Row(c).PaddingX(16).PaddingY(12).Gap(8).AlignItems(ui.Center).Children(func() {
		legend(c, pal, strings.ToUpper(title))
		ui.Spacer(c)
		ui.Text(c, note).FontSize(12).TextColor(pal.muted).SingleLine()
	})
}

// dial draws ~/.claude on one calibrated scale: a segment per top-level
// entry, its cleanable part orange, ticks at round sizes.
func (o *overview) dial(c *ui.Context, a *App, pal *palette) {
	total := o.total()
	face(c, pal).Padding(18, 20).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			legend(c, pal, "SPACE USED")
			ui.Spacer(c)
			ui.Box(c).Size(10, 10).Radius(2).Background(pal.ink)
			ui.Text(c, "kept").FontSize(12).TextColor(pal.muted)
			ui.Box(c).Size(10, 10).Radius(2).Background(pal.orange).MarginX(4)
			ui.Textf(c, "%s can be cleaned", humanSize(a.cleanup.reclaim)).FontSize(12).TextColor(pal.muted).SingleLine()
		})
		ui.Box(c).Height(86).Label("Space used by each folder").Draw(func(pt *ui.Painter, r ui.Rect) {
			drawDial(pt, r, pal, o.rows, a.cleanup.byTop, total)
		})
	})
}

func drawDial(pt *ui.Painter, r ui.Rect, pal *palette, rows []store.Entry, cleanable map[string]int64, total int64) {
	const rowH, segY, segH, baseY, numY = 15, 34, 16, 62, 70
	pt.Fill(ui.Rect{X: r.X, Y: r.Y + baseY, W: r.W, H: 1}, pal.ink, 0)
	if total <= 0 {
		return
	}
	at := func(n int64) float32 { return r.X + r.W*float32(n)/float32(total) }
	// Two rows of markers above the scale; a label goes in the first row
	// where it fits, else the second. Only a segment with an orange part is
	// always named: it is what the user reads the dial for.
	var taken [2][][2]float32
	place := func(x, w float32) int {
		for row := range taken {
			free := true
			for _, t := range taken[row] {
				if x < t[1]+10 && x+w > t[0]-10 {
					free = false
				}
			}
			if free {
				taken[row] = append(taken[row], [2]float32{x, x + w})
				return row
			}
		}
		return -1
	}
	var from int64
	for _, e := range rows {
		x0, x1 := at(from), at(from+e.Size)
		from += e.Size
		if x1-x0 < 0.5 {
			continue
		}
		pt.Fill(ui.Rect{X: x0, Y: r.Y + segY, W: max(x1-x0-1, 0.5), H: segH}, pal.ink, 0)
		o := cleanable[e.Name]
		if o > 0 {
			w := min(max((x1-x0)*float32(o)/float32(e.Size), 1.5), x1-x0)
			pt.Fill(ui.Rect{X: x1 - w, Y: r.Y + segY, W: max(w-1, 0.5), H: segH}, pal.orange, 0)
		}
		mark := x0 // a marker points at the start of its segment
		label := ui.Span{Text: e.Name + "  " + humanSize(e.Size), Size: 11, Color: pal.ink}
		lw, _ := pt.MeasureText(0, label)
		if o == 0 && lw+8 >= x1-x0 {
			continue // too narrow to name, and nothing to clean in it
		}
		lx := min(mark, r.X+r.W-lw-4)
		row := place(lx, lw+4)
		if row < 0 {
			continue
		}
		ly := r.Y + float32(row)*rowH
		pt.Line(mark+0.5, ly+rowH-1, mark+0.5, r.Y+segY, 1, pal.ink)
		pt.RichText(lx+4, ly, 0, label)
	}
	step := tickStep(total)
	for n, i := int64(0), 0; n <= total; n, i = n+step/2, i+1 {
		x := at(n)
		h := float32(4)
		if i%2 == 0 {
			h = 8
			s := ui.Span{Text: tickLabel(n, step), Size: 11, Color: pal.muted}
			w, _ := pt.MeasureText(0, s)
			if x+w/2 < r.X+r.W-40 { // keep clear of the total at the end
				pt.RichText(max(x-w/2, r.X), r.Y+numY, 0, s)
			}
		}
		pt.Line(x, r.Y+baseY-h, x, r.Y+baseY, 1, pal.ink)
	}
	end := ui.Span{Text: humanSize(total), Size: 11, Weight: 600, Color: pal.ink}
	w, _ := pt.MeasureText(0, end)
	pt.Line(r.X+r.W-0.5, r.Y+baseY-8, r.X+r.W-0.5, r.Y+baseY, 1, pal.ink)
	pt.RichText(r.X+r.W-w, r.Y+numY, 0, end)
}

// tickStep is a round size that puts four to eight major ticks on total.
func tickStep(total int64) int64 {
	for unit := int64(1); ; unit *= 1024 {
		for _, m := range []int64{1, 2, 5, 10, 20, 50, 100, 200, 500} {
			if s := m * unit; total/s <= 8 {
				return s
			}
		}
	}
}

func tickLabel(n, step int64) string {
	if n == 0 {
		return "0"
	}
	unit, suffix := int64(1), "B"
	for _, u := range []string{"KB", "MB", "GB", "TB"} {
		if step < unit*1024 {
			break
		}
		unit, suffix = unit*1024, u
	}
	return strconv.FormatInt(n/unit, 10) + " " + suffix
}

// recent lists the sessions running now, then the latest others; a click
// opens one's transcript.
func recent(c *ui.Context, a *App, pal *palette) {
	var all []store.Session
	live := 0
	for _, p := range a.sessions.projects {
		for _, x := range p.Sessions {
			all = append(all, x)
			if x.Live {
				live++
			}
		}
	}
	slices.SortFunc(all, func(x, y store.Session) int {
		if x.Live != y.Live {
			if x.Live {
				return -1
			}
			return 1
		}
		return y.Modified.Compare(x.Modified)
	})
	note := "none running"
	if live > 0 {
		note = plural(live, "session") + " running"
	}
	head(c, pal, "Sessions", note)
	const shown = 6
	ui.Column(c).Children(func() {
		for i, x := range all[:min(len(all), shown)] {
			if i > 0 {
				ui.Box(c).Height(1).Background(pal.rule).MarginX(16)
			}
			row := ui.Row(c.Key(x.ID)).Gap(12).PaddingX(16).PaddingY(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(pal.panel)
			}
			row.Children(func() {
				if x.Live {
					lamp(c, pal).Tooltip("Running now; it can't be moved to the Trash")
				} else {
					unlit(c, pal)
				}
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, strings.TrimSuffix(sessionLabel(&x), liveSuffix)).SingleLine()
					when := fmtTime(x.Modified)
					if x.Live {
						when = "running"
					}
					ui.Text(c, shortPath(x.Cwd)+" · "+when).FontSize(12).TextColor(pal.muted).SingleLine()
				})
				ui.Text(c, humanSize(x.Size)).FontFeatures("tnum").SingleLine()
			})
			if row.Clicked() {
				a.page = "sessions"
				a.sessions.openTranscript(a, x)
			}
		}
	})
	if len(all) > shown {
		ui.Box(c).Height(1).Background(pal.rule)
		ui.Row(c).PaddingX(16).PaddingY(10).Children(func() {
			if ui.Button(c, fmt.Sprintf("Show all %d sessions", len(all))).Clicked() {
				a.page = "sessions"
			}
		})
	}
}

// folders lists every top-level entry on one scale; a click on one with a
// screen of its own opens it.
func (o *overview) folders(c *ui.Context, a *App, pal *palette) {
	head(c, pal, "Folders", plural(len(o.rows), "item"))
	var most int64
	for _, e := range o.rows {
		most = max(most, e.Size)
	}
	for i, e := range o.rows {
		ui.Box(c).Height(1).Background(pal.rule).MarginX(16)
		target := screenOf(&e)
		row := ui.Row(c.Key(i)).Gap(14).PaddingX(16).PaddingY(8).AlignItems(ui.Center)
		if target != "" {
			row.Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(pal.panel)
			}
		}
		row.Children(func() {
			ui.Text(c, e.Name).Width(200).SingleLine()
			scale(c, pal, e.Size, a.cleanup.byTop[e.Name], most).Grow(1)
			ui.Text(c, humanSize(e.Size)).Width(80).TextAlign(ui.End).FontFeatures("tnum").SingleLine()
			ui.Text(c, plural(e.Files, "file")).FontSize(12).TextColor(pal.muted).Width(90).TextAlign(ui.End).SingleLine()
			open := ""
			if target != "" {
				open = "Open in " + titles[target]
			}
			ui.Text(c, open).FontSize(12).TextColor(pal.muted).Width(110).TextAlign(ui.End).SingleLine()
		})
		if target != "" && row.Clicked() {
			a.page = target
		}
	}
}

var titles = map[string]string{"sessions": "Sessions", "config": "Config", "cleanup": "Cleanup"}

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
