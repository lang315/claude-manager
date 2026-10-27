package view

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

// usage is the Usage screen: the tokens Claude Code used, and what they would
// cost on the API, by day, project, session and model.
type usage struct {
	scan        store.UsageScan // touched only by the scanning goroutine
	buckets     []store.Usage
	bad         int
	unreadable  int  // the last scan's unreadable transcripts, so its toast shows once
	loaded      bool // a scan has landed
	scanning    bool
	again       bool // a Rescan came during a scan
	done, total int  // the scan's progress
	gen         int  // counts the scans landed

	preset  int    // in datePresets, Custom left out
	project string // the project filtered to, "" for all
	tab     int
	key     usageKey
	sum     usageSum

	projAt, sessAt     int // the rows chosen
	projList, sessList ui.ListState
	modelList          ui.ListState
}

var usageTabs = []string{"Projects", "Sessions", "Models"}

// usageKey is what sum was worked out for.
type usageKey struct {
	dates   dateRange
	project string
	gen     int
}

// usageRow sums the buckets of one project, session or model.
type usageRow struct {
	name     string    // the project's path, the session's id or the model
	project  string    // a session's project
	last     time.Time // a session's last day
	sessions int       // a project's sessions
	store.Usage
}

// usageSum is the screen's figures for one period and project.
type usageSum struct {
	total                      store.Usage
	bars                       []stack // the chart's cost an hour today, else a day or a week, from first
	hourly                     bool
	byFamily                   stack     // the cost of each family, for the legend
	first, last                time.Time // the days the chart spans
	weekly                     bool
	projects, sessions, models []usageRow
	allProjects                float64 // the period's cost in every project: the Share bars' whole
}

func (u *usage) init() {
	u.preset, u.key.gen, u.projAt, u.sessAt = preset30Days, -1, -1, -1
	u.projList.Selected = &u.projAt
	u.sessList.Selected = &u.sessAt
}

// load scans the transcripts off the main thread; a load during a scan runs
// once it lands, so one scan at a time uses u.scan. It is not a.run: the
// scan takes seconds and feeds no Trash target, so it doesn't hold the app
// busy.
func (u *usage) load(a *App) {
	if u.scanning {
		u.again = true
		return
	}
	u.scanning = true
	a.Go(func() {
		progress := func(done, total int) {
			if done%100 == 0 || done == total {
				a.Update(func() { u.done, u.total = done, total })
			}
		}
		buckets, bad, unreadable, err := a.Store.Usage(&u.scan, progress)
		a.Update(func() {
			u.scanning, u.loaded = false, true
			// A failed scan keeps the last figures; the toast says why.
			if err == nil {
				u.buckets, u.bad = buckets, bad
				u.gen++
			}
			switch {
			case a.notice != "": // a pending notice, like a Trash's, stays
			case err != nil:
				a.notice = err.Error()
			case unreadable > 0 && unreadable != u.unreadable:
				a.notice = plural(unreadable, "transcript") + " could not be read"
			}
			if err == nil {
				u.unreadable = unreadable
			}
			if u.again {
				u.again = false
				u.load(a)
			}
		})
	})
}

// refresh works out the figures again when the period, the project or the
// buckets changed: one pass over the buckets, not one a frame.
func (u *usage) refresh(now time.Time) {
	k := usageKey{rangeOf(u.preset, now, "", ""), u.project, u.gen}
	if k == u.key {
		return
	}
	// A scan that lands keeps the chosen session; a new period or project
	// drops it.
	keep := ""
	if k.dates == u.key.dates && k.project == u.key.project && u.sessAt >= 0 && u.sessAt < len(u.sum.sessions) {
		keep = u.sum.sessions[u.sessAt].name
	}
	u.key = k
	var s usageSum
	byDay := map[string]stack{} // cost by local day, "2006-01-02"
	var byHour [24]stack
	projects, sessions, models := map[string]*usageRow{}, map[string]*usageRow{}, map[string]*usageRow{}
	inProject := map[string]map[string]bool{} // project: its sessions
	for _, b := range u.buckets {
		if !k.dates.keeps(b.Day) {
			continue
		}
		rowOf(projects, b.Project).Add(b) // every project, so another can be chosen
		s.allProjects += b.Cost
		if inProject[b.Project] == nil {
			inProject[b.Project] = map[string]bool{}
		}
		inProject[b.Project][b.SessionID] = true
		if k.project != "" && b.Project != k.project {
			continue
		}
		s.total.Add(b)
		f := modelFamily(b.Model)
		s.byFamily[f] += b.Cost
		if !b.Day.IsZero() {
			d := byDay[b.Day.Format(time.DateOnly)]
			d[f] += b.Cost
			byDay[b.Day.Format(time.DateOnly)] = d
			for h, c := range b.Hours {
				byHour[h][f] += c
			}
			if s.first.IsZero() || b.Day.Before(s.first) {
				s.first = b.Day
			}
		}
		r := rowOf(sessions, b.SessionID)
		r.project = b.Project
		if b.Day.After(r.last) {
			r.last = b.Day
		}
		r.Add(b)
		rowOf(models, b.Model).Add(b)
	}
	for name, r := range projects {
		r.sessions = len(inProject[name])
	}
	s.projects, s.sessions, s.models = sortRows(projects), sortRows(sessions), sortRows(models)
	s.last = midnight(now)
	if k.dates.on {
		s.first = k.dates.lo
	}
	s.weekly = !k.dates.on && !s.first.IsZero() && s.last.Sub(s.first) > 90*24*time.Hour
	s.bars = bars(byDay, s.first, s.last, s.weekly)
	if u.preset == presetToday {
		s.hourly, s.bars = true, byHour[:]
	}
	u.sum = s
	u.sessAt = slices.IndexFunc(s.sessions, func(r usageRow) bool { return keep != "" && r.name == keep })
}

func rowOf(m map[string]*usageRow, name string) *usageRow {
	r := m[name]
	if r == nil {
		r = &usageRow{name: name}
		m[name] = r
	}
	return r
}

// sortRows orders rows by cost, then tokens, highest first.
func sortRows(m map[string]*usageRow) []usageRow {
	out := make([]usageRow, 0, len(m))
	for _, r := range m {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b usageRow) int {
		return cmp.Or(cmp.Compare(b.Cost, a.Cost), cmp.Compare(b.Tokens(), a.Tokens()), cmp.Compare(a.name, b.name))
	})
	return out
}

// subtitle is the header's line: the period's figures, or the scan's
// progress before the first lands.
func (u *usage) subtitle(now time.Time) string {
	if !u.loaded {
		if u.total == 0 {
			return "Reading transcripts…"
		}
		return fmt.Sprintf("Reading %s of %s transcripts", commas(int64(u.done)), commas(int64(u.total)))
	}
	u.refresh(now)
	t := u.sum.total
	s := money(t.Cost) + " est. API cost · " + tokenCount(t.Tokens()) + " tokens"
	if in := t.Input + t.CacheRead + t.CacheWrite5m + t.CacheWrite1h; in > 0 {
		s += fmt.Sprintf(" · %d%% from cache", t.CacheRead*100/in)
	}
	if t.Unpriced > 0 {
		s += " · " + plural(t.Unpriced, "call") + " unpriced"
	}
	if u.bad > 0 {
		s += " · " + plural(u.bad, "unreadable line")
	}
	return s
}

func (u *usage) view(c *ui.Context, a *App, pal *palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Segmented(c, &u.preset, datePresets[:presetCustom]...)
		ui.Spacer(c)
		if u.project != "" && ui.Button(c, "Project: "+shortPath(u.project)+"  ✕").Tooltip(u.project).Clicked() {
			u.project = ""
		}
	})
	u.refresh(a.Store.Now())
	switch {
	case !u.loaded:
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
			ui.Spinner(c).Label("Reading transcripts")
			ui.Text(c, "Reading transcripts…").TextColor(pal.muted)
		})
		return
	case len(u.buckets) == 0:
		blank(c, pal, "No usage yet.")
		return
	case u.sum.total.Calls == 0:
		blank(c, pal, "No usage in this period.")
		return
	}
	u.chart(c, pal)
	ui.Tabs(c, &u.tab, usageTabs...)
	face(c, pal).Grow(1).MinHeight(0).Children(func() {
		switch u.tab {
		case 0:
			u.projectTable(c, pal)
		case 1:
			u.sessionTable(c, a, pal)
		case 2:
			u.modelTable(c)
		}
	})
}

// Model families, in the chart's stack order from the baseline up.
const (
	famOpus = iota
	famSonnet
	famHaiku
	famFable
	famOther
	families
)

var familyNames = [families]string{"Opus", "Sonnet", "Haiku", "Fable", "Other"}

// stack is a bar's cost by model family.
type stack [families]float64

// modelFamily is the family a model's name says, famOther when it says none.
func modelFamily(model string) int {
	for f, name := range familyNames[:famOther] {
		if strings.Contains(model, strings.ToLower(name)) {
			return f
		}
	}
	return famOther
}

// hue is a family's colour in the chart; Other is grey.
func (p *palette) hue(f int) ui.Color {
	if f == famOther {
		return p.muted
	}
	return p.hues[f]
}

// chart draws the period's cost by model family: a bar an hour today, a day,
// or a week when All spans more than 90 days.
func (u *usage) chart(c *ui.Context, pal *palette) {
	s := &u.sum
	title := "Cost by day"
	switch {
	case s.hourly:
		title = "Cost by hour"
	case s.weekly:
		title = "Cost by week"
	}
	face(c, pal).Children(func() {
		head(c, pal, title, money(s.total.Cost)+" "+periodText[u.preset]+" · prices of "+store.PricesAsOf)
		ui.Column(c).Padding(0, 16, 12, 16).Gap(8).Children(func() {
			ui.Row(c).Gap(14).Children(func() {
				for f, cost := range s.byFamily {
					if cost <= 0 {
						continue
					}
					ui.Row(c.Key(f)).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 8).Radius(2).Background(pal.hue(f))
						ui.Text(c, familyNames[f]).FontSize(12).TextColor(pal.muted).SingleLine()
					})
				}
			})
			ui.Box(c).Height(120).Label(title).Draw(func(pt *ui.Painter, r ui.Rect) {
				drawBars(pt, r, pal, s.bars, s.hourly)
			})
		})
	})
}

// bars returns the cost of each day from first to last, both local
// midnights, or of each week from the Monday on or before first when weekly.
func bars(byDay map[string]stack, first, last time.Time, weekly bool) []stack {
	if first.IsZero() {
		return nil
	}
	if weekly {
		for first.Weekday() != time.Monday {
			first = first.AddDate(0, 0, -1)
		}
	}
	var out []stack
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if !weekly || len(out) == 0 || d.Weekday() == time.Monday {
			out = append(out, stack{})
		}
		for f, v := range byDay[d.Format(time.DateOnly)] {
			out[len(out)-1][f] += v
		}
	}
	return out
}

// dollarStep is a round amount, 1, 2 or 5 times a power of ten, that puts at
// most four steps under most.
func dollarStep(most float64) float64 {
	for unit := 0.01; ; unit *= 10 {
		for _, m := range []float64{1, 2, 5} {
			if s := math.Round(m*unit*100) / 100; most/s <= 4 {
				return s
			}
		}
	}
}

// dollarTick is a tick's label: whole dollars bare, cents when the step has
// them.
func dollarTick(v float64) string {
	if v == math.Trunc(v) {
		return "$" + commas(int64(v))
	}
	return fmt.Sprintf("$%.2f", v)
}

// drawBars draws each bar's cost as a stack of family hues over an ink
// baseline, with ticks at round dollar amounts at the left, and the hours
// under an hourly chart.
func drawBars(pt *ui.Painter, r ui.Rect, pal *palette, vals []stack, hourly bool) {
	const axisW = 52
	plot := ui.Rect{X: r.X + axisW, Y: r.Y + 6, W: r.W - axisW, H: r.H - 10}
	if hourly {
		plot.H -= 16 // the hours' labels
	}
	pt.Fill(ui.Rect{X: plot.X, Y: plot.Y + plot.H, W: plot.W, H: 1}, pal.ink, 0)
	bw := plot.W / float32(max(len(vals), 1))
	if hourly {
		for h := 0; h < 24; h += 6 {
			label := ui.Span{Text: fmt.Sprintf("%02d", h), Size: 11, Color: pal.muted}
			w, _ := pt.MeasureText(0, label)
			pt.RichText(plot.X+(float32(h)+0.5)*bw-w/2, plot.Y+plot.H+4, 0, label)
		}
	}
	most := 0.0
	for _, st := range vals {
		total := 0.0
		for _, v := range st {
			total += v
		}
		most = max(most, total)
	}
	if most <= 0 {
		return
	}
	step := dollarStep(most)
	top := math.Ceil(most/step) * step
	y := func(v float64) float32 { return plot.Y + plot.H - plot.H*float32(v/top) }
	for i := 0; float64(i)*step <= top+step/2; i++ {
		v := float64(i) * step
		yy := y(v)
		pt.Line(plot.X-5, yy, plot.X, yy, 1, pal.ink)
		label := ui.Span{Text: dollarTick(v), Size: 11, Color: pal.muted}
		w, h := pt.MeasureText(0, label)
		pt.RichText(plot.X-9-w, yy-h/2, 0, label)
	}
	gap := min(bw*0.25, 3)
	base := plot.Y + plot.H
	for i, st := range vals {
		x, w := plot.X+float32(i)*bw+gap/2, max(bw-gap, 1)
		y0 := base
		for f, v := range st {
			if v <= 0 {
				continue
			}
			h := max(plot.H*float32(v/top), 1)
			// A 2px gap of the face parts a segment from the one under it.
			g := float32(0)
			if y0 < base {
				g = min(2, h/3)
			}
			pt.Fill(ui.Rect{X: x, Y: y0 - h, W: w, H: h - g}, pal.hue(f), 0)
			y0 -= h
		}
	}
}

// num is a figure in a table cell.
func num(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontFeatures("tnum").SingleLine()
}

// costText is a row's cost: "—" when none of its calls has a price, and a
// "+" after the amount when only some have.
func costText(u store.Usage) string {
	switch {
	case u.Cost == 0 && u.Unpriced > 0:
		return "—"
	case u.Unpriced > 0:
		return money(u.Cost) + "+"
	}
	return money(u.Cost)
}

// costCell is a row's cost in a table cell, saying how many calls it leaves
// out.
func costCell(c *ui.Context, u store.Usage) {
	e := num(c, costText(u))
	if u.Unpriced > 0 {
		e.Tooltip(plural(u.Unpriced, "call") + " unpriced")
	}
}

func cents(f float64) int64 { return int64(f * 100) }

func (u *usage) projectTable(c *ui.Context, pal *palette) {
	rows := u.sum.projects
	cols := []ui.TableColumn{{Title: "Project", MinWidth: 160}, {Title: "Sessions", Width: 80, Align: ui.End},
		{Title: "Tokens", Width: 90, Align: ui.End}, {Title: "Cost", Width: 100, Align: ui.End}, {Title: "Share", Width: 160}}
	ui.Table(c, &u.projList, cols, len(rows), func(i, col int) {
		r := &rows[i]
		switch col {
		case 0:
			ui.Text(c, shortPath(r.name)).SingleLine().Tooltip(r.name)
		case 1:
			num(c, strconv.Itoa(r.sessions))
		case 2:
			num(c, tokenCount(r.Tokens()))
		case 3:
			costCell(c, r.Usage)
		case 4:
			scale(c, pal, cents(r.Cost), 0, cents(u.sum.allProjects)).Grow(1)
		}
	}).Grow(1).MinHeight(0)
	// A click on a project filters the screen to it.
	if u.projAt >= 0 && u.projAt < len(rows) {
		u.project = rows[u.projAt].name
	}
	u.projAt = -1
}

func (u *usage) sessionTable(c *ui.Context, a *App, pal *palette) {
	rows := u.sum.sessions
	cols := []ui.TableColumn{{Title: "Session", MinWidth: 160}, {Title: "Project", Width: 130}, {Title: "Last day", Width: 100},
		{Title: "Tokens", Width: 90, Align: ui.End}, {Title: "Cost", Width: 100, Align: ui.End}}
	ui.Table(c, &u.sessList, cols, len(rows), func(i, col int) {
		r := &rows[i]
		switch col {
		case 0:
			name := r.name
			if x := a.sessions.find(r.name); x != nil {
				name = sessionName(x)
			}
			ui.Text(c, name).SingleLine()
		case 1:
			ui.Text(c, shortPath(r.project)).TextColor(pal.muted).SingleLine().Tooltip(r.project)
		case 2:
			day := "—"
			if !r.last.IsZero() {
				day = r.last.Format(time.DateOnly)
			}
			num(c, day)
		case 3:
			num(c, tokenCount(r.Tokens()))
		case 4:
			costCell(c, r.Usage)
		}
	}).Grow(1).MinHeight(0)
	ui.Divider(c)
	ui.Row(c).PaddingX(16).PaddingY(10).Children(func() {
		var x *store.Session
		if u.sessAt >= 0 && u.sessAt < len(rows) {
			x = a.sessions.find(rows[u.sessAt].name)
		}
		if ui.Button(c, "Open Transcript").Disabled(x == nil).Clicked() {
			a.page = "sessions"
			a.sessions.openTranscript(a, *x)
		}
	})
}

func (u *usage) modelTable(c *ui.Context) {
	rows := u.sum.models
	cols := []ui.TableColumn{{Title: "Model", MinWidth: 180}, {Title: "Calls", Width: 70, Align: ui.End},
		{Title: "Input", Width: 80, Align: ui.End}, {Title: "Output", Width: 80, Align: ui.End},
		{Title: "Cache write", Width: 90, Align: ui.End}, {Title: "Cache read", Width: 90, Align: ui.End},
		{Title: "Cost", Width: 100, Align: ui.End}}
	ui.Table(c, &u.modelList, cols, len(rows), func(i, col int) {
		r := &rows[i]
		switch col {
		case 0:
			name := r.name
			if r.Fast > 0 {
				name += " · fast"
			}
			ui.Text(c, name).SingleLine()
		case 1:
			num(c, commas(int64(r.Calls)))
		case 2:
			num(c, tokenCount(r.Input))
		case 3:
			num(c, tokenCount(r.Output))
		case 4:
			num(c, tokenCount(r.CacheWrite5m+r.CacheWrite1h))
		case 5:
			num(c, tokenCount(r.CacheRead))
		case 6:
			costCell(c, r.Usage)
		}
	}).Grow(1).MinHeight(0)
}
