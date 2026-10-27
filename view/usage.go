package view

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
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
	byDay                      map[string]float64 // cost by local day, "2006-01-02"
	first, last                time.Time          // the days the chart spans
	weekly                     bool
	projects, sessions, models []usageRow
}

func (u *usage) init() {
	u.preset, u.key.gen, u.projAt, u.sessAt = preset30Days, -1, -1, -1
	u.projList.Selected = &u.projAt
	u.sessList.Selected = &u.sessAt
}

// load scans the transcripts off the main thread; a load during a scan runs
// once it lands, so one scan at a time uses u.scan.
func (u *usage) load(a *App) {
	if u.scanning {
		u.again = true
		return
	}
	u.scanning = true
	a.run(func() func() {
		progress := func(done, total int) {
			if done%100 == 0 || done == total {
				a.Update(func() { u.done, u.total = done, total })
			}
		}
		buckets, bad, unreadable, err := a.Store.Usage(&u.scan, progress)
		return func() {
			u.scanning, u.loaded = false, true
			u.buckets, u.bad = buckets, bad
			u.gen++
			switch {
			case err != nil:
				a.notice = err.Error()
			case unreadable > 0:
				a.notice = plural(unreadable, "transcript") + " could not be read"
			}
			if u.again {
				u.again = false
				u.load(a)
			}
		}
	})
}

// refresh works out the figures again when the period, the project or the
// buckets changed: one pass over the buckets, not one a frame.
func (u *usage) refresh(now time.Time) {
	k := usageKey{rangeOf(u.preset, now, "", ""), u.project, u.gen}
	if k == u.key {
		return
	}
	u.key, u.sessAt = k, -1
	s := usageSum{byDay: map[string]float64{}}
	projects, sessions, models := map[string]*usageRow{}, map[string]*usageRow{}, map[string]*usageRow{}
	inProject := map[string]map[string]bool{} // project: its sessions
	for _, b := range u.buckets {
		if !k.dates.keeps(b.Day) {
			continue
		}
		rowOf(projects, b.Project).Add(b) // every project, so another can be chosen
		if inProject[b.Project] == nil {
			inProject[b.Project] = map[string]bool{}
		}
		inProject[b.Project][b.SessionID] = true
		if k.project != "" && b.Project != k.project {
			continue
		}
		s.total.Add(b)
		if !b.Day.IsZero() {
			s.byDay[b.Day.Format(time.DateOnly)] += b.Cost
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
	u.sum = s
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
		blank(c, pal, "Reading transcripts…")
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

var periodText = []string{"today", "over 7 days", "over 30 days", "in all"}

// chart draws the period's cost, a bar a day, or a week when All spans more
// than 90 days.
func (u *usage) chart(c *ui.Context, pal *palette) {
	s := &u.sum
	vals := bars(s.byDay, s.first, s.last, s.weekly)
	title := "Cost by day"
	if s.weekly {
		title = "Cost by week"
	}
	face(c, pal).Children(func() {
		head(c, pal, title, money(s.total.Cost)+" "+periodText[u.preset]+" · prices of "+store.PricesAsOf)
		ui.Column(c).Padding(2, 16, 12, 16).Children(func() {
			ui.Box(c).Height(110).Label(title).Draw(func(pt *ui.Painter, r ui.Rect) {
				drawBars(pt, r, pal, vals)
			})
		})
	})
}

// bars returns the cost of each day from first to last, both local
// midnights, or of each week from the Monday on or before first when weekly.
func bars(byDay map[string]float64, first, last time.Time, weekly bool) []float64 {
	if first.IsZero() {
		return nil
	}
	if weekly {
		for first.Weekday() != time.Monday {
			first = first.AddDate(0, 0, -1)
		}
	}
	var out []float64
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if !weekly || len(out) == 0 || d.Weekday() == time.Monday {
			out = append(out, 0)
		}
		out[len(out)-1] += byDay[d.Format(time.DateOnly)]
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

// drawBars draws costs as ink bars over a baseline, with ticks at round
// dollar amounts at the left.
func drawBars(pt *ui.Painter, r ui.Rect, pal *palette, vals []float64) {
	const axisW = 52
	plot := ui.Rect{X: r.X + axisW, Y: r.Y + 6, W: r.W - axisW, H: r.H - 10}
	pt.Fill(ui.Rect{X: plot.X, Y: plot.Y + plot.H, W: plot.W, H: 1}, pal.ink, 0)
	most := 0.0
	for _, v := range vals {
		most = max(most, v)
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
	bw := plot.W / float32(len(vals))
	gap := min(bw*0.25, 3)
	for i, v := range vals {
		if v <= 0 {
			continue
		}
		h := max(plot.H*float32(v/top), 1)
		pt.Fill(ui.Rect{X: plot.X + float32(i)*bw + gap/2, Y: plot.Y + plot.H - h, W: max(bw-gap, 1), H: h}, pal.ink, 0)
	}
}

// num is a figure in a table cell.
func num(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontFeatures("tnum").SingleLine()
}

// costText is a row's cost, "—" when none of its calls has a price.
func costText(u store.Usage) string {
	if u.Cost == 0 && u.Unpriced > 0 {
		return "—"
	}
	return money(u.Cost)
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
			num(c, costText(r.Usage))
		case 4:
			scale(c, pal, cents(r.Cost), 0, cents(rows[0].Cost)).Grow(1)
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
			num(c, costText(r.Usage))
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
			num(c, costText(r.Usage))
		}
	}).Grow(1).MinHeight(0)
}
