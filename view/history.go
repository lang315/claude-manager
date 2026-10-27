package view

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

// The date filter's presets, in the order the Segmented shows them.
const (
	presetToday = iota
	preset7Days
	preset30Days
	presetAll
	presetCustom
)

var datePresets = []string{"Today", "7 days", "30 days", "All", "Custom"}

// dateRange is the span the date filter keeps. Off, it keeps every prompt;
// on, those with a time from lo up to hi, hi excluded, a zero bound leaving
// its side open. It is part of the filter key, so it stays comparable.
type dateRange struct {
	on     bool
	lo, hi time.Time
}

// rangeOf works out the date filter. A preset counts whole days back from
// today's midnight, in now's location (the local one in the app), today
// included; Custom spans from's day to to's, both whole. A Custom field that
// is not a date turns the filter off, so the table doesn't empty.
func rangeOf(preset int, now time.Time, from, to string) dateRange {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch preset {
	case presetToday:
		return dateRange{on: true, lo: today}
	case preset7Days:
		return dateRange{on: true, lo: today.AddDate(0, 0, -6)}
	case preset30Days:
		return dateRange{on: true, lo: today.AddDate(0, 0, -29)}
	case presetCustom:
		lo, msg1 := parseDay(from, now.Location())
		hi, msg2 := parseDay(to, now.Location())
		if msg1 != "" || msg2 != "" {
			return dateRange{}
		}
		if !hi.IsZero() {
			hi = hi.AddDate(0, 0, 1)
		}
		return dateRange{on: true, lo: lo, hi: hi}
	}
	return dateRange{}
}

// parseDay reads a Custom field: a YYYY-MM-DD date, its midnight in loc. An
// empty field is the zero time, and one that is not a date a message.
func parseDay(s string, loc *time.Location) (time.Time, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, ""
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, "Use YYYY-MM-DD"
	}
	return t, ""
}

// keeps reports whether a prompt of time t passes the date filter. A prompt
// without a time passes only when the filter is off.
func (r dateRange) keeps(t time.Time) bool {
	if !r.on {
		return true
	}
	return !t.IsZero() && (r.lo.IsZero() || !t.Before(r.lo)) && (r.hi.IsZero() || t.Before(r.hi))
}

// matchPrompt reports whether the search keyword is in the prompt or in one
// of its pastes, ignoring case. A blank keyword matches every prompt.
func matchPrompt(p *store.Prompt, kw string) bool {
	kw = strings.TrimSpace(kw)
	if kw == "" || containsFold(p.Text, kw) {
		return true
	}
	for _, x := range p.Pastes {
		if containsFold(x.Content, kw) {
			return true
		}
	}
	return false
}

// history is the History screen: every prompt typed, found by search,
// project and date.
type history struct {
	prompts  []store.Prompt
	bad      int
	projects []string // allProjects, then each project as tilde shows it, newest prompt first
	shown    []int    // indexes in prompts of the rows the filters keep, newest first
	key      filterKey
	stale    bool // prompts changed since shown was worked out
	list     ui.ListState
	at       int // the row chosen in shown, -1 for none
	split    float32

	query, project string
	preset         int
	from, to       string // Custom's fields
	pasteOpen      map[[2]int]*bool
}

const allProjects = "All projects"

// filterKey is what the rows shown were filtered by.
type filterKey struct {
	query, project string
	dates          dateRange
}

func (h *history) init() {
	h.split, h.at, h.preset, h.project = 560, -1, presetAll, allProjects
	h.projects = []string{allProjects}
	h.list.Selected = &h.at
	h.list.Key = func(row int) any { return h.prompts[h.shown[row]].Line }
	h.pasteOpen = map[[2]int]*bool{}
}

func (h *history) load(a *App) {
	a.run(func() func() {
		prompts, bad, err := a.Store.History()
		return func() {
			h.prompts, h.bad, h.stale, h.at = prompts, bad, true, -1
			if err != nil {
				a.notice = err.Error()
			}
			h.projects = []string{allProjects}
			seen := map[string]bool{}
			for _, p := range prompts {
				if t := tilde(p.Project); !seen[t] {
					seen[t] = true
					h.projects = append(h.projects, t)
				}
			}
			if !seen[h.project] {
				h.project = allProjects
			}
		}
	})
}

// filter works out the rows shown again when the filters or the prompts
// changed: one pass over the prompts, not one a frame. The row chosen stays
// chosen while the filters keep it.
func (h *history) filter(k filterKey) {
	if k == h.key && !h.stale {
		return
	}
	line := 0
	if !h.stale {
		if p := h.chosen(); p != nil {
			line = p.Line
		}
	}
	h.key, h.stale, h.at = k, false, -1
	h.shown = h.shown[:0]
	for i := range h.prompts {
		p := &h.prompts[i]
		if (k.project == allProjects || tilde(p.Project) == k.project) && k.dates.keeps(p.Time) && matchPrompt(p, k.query) {
			if p.Line == line {
				h.at = len(h.shown)
			}
			h.shown = append(h.shown, i)
		}
	}
	h.list.Version++
}

// chosen is the prompt of the row chosen, nil for none.
func (h *history) chosen() *store.Prompt {
	if h.at < 0 || h.at >= len(h.shown) {
		return nil
	}
	return &h.prompts[h.shown[h.at]]
}

func (h *history) view(c *ui.Context, a *App, pal *palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.SearchField(c, &h.query).Label("Search prompts").Width(200).MinWidth(100)
		ui.Select(c, &h.project, h.projects).Width(240).MinWidth(120)
		ui.Spacer(c)
		ui.Segmented(c, &h.preset, datePresets...)
	})
	if h.preset == presetCustom {
		loc := a.Store.Now().Location()
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Field(c, "From", func() { ui.TextInput(c, &h.from).Placeholder("YYYY-MM-DD").Width(130) }).Error(dayError(h.from, loc))
			ui.Field(c, "To", func() { ui.TextInput(c, &h.to).Placeholder("YYYY-MM-DD").Width(130) }).Error(dayError(h.to, loc))
		})
	}
	h.filter(filterKey{h.query, h.project, rangeOf(h.preset, a.Store.Now(), h.from, h.to)})
	ui.Split(c, &h.split, func() {
		ui.Column(c).Fill().Gap(6).Children(func() { h.table(c, pal) })
	}, func() {
		ui.Column(c).Fill().Padding(0, 0, 0, 12).Children(func() { h.detail(c, a, pal) })
	}).Grow(1)
}

// dayError is the message under a Custom field, "" when it is fine.
func dayError(s string, loc *time.Location) string {
	_, msg := parseDay(s, loc)
	return msg
}

func (h *history) table(c *ui.Context, pal *palette) {
	switch {
	case len(h.prompts) == 0:
		blank(c, pal, "No prompt history yet.")
		return
	case len(h.shown) == 0:
		blank(c, pal, "No prompt matches.")
		return
	}
	cols := []ui.TableColumn{{Title: "Time", Width: 130}, {Title: "Project", Width: 140}, {Title: "Prompt", MinWidth: 160}}
	ui.Table(c, &h.list, cols, len(h.shown), func(row, col int) {
		p := &h.prompts[h.shown[row]]
		switch col {
		case 0:
			ui.Text(c, fmtTime(p.Time)).TextColor(pal.muted).FontFeatures("tnum").SingleLine()
		case 1:
			ui.Text(c, shortPath(p.Project)).SingleLine().Tooltip(p.Project)
		case 2:
			ui.Text(c, store.FirstLine(p.Text, 200)).SingleLine()
		}
	}).Grow(1).MinHeight(0)
	ui.Text(c, plural(len(h.shown), "prompt")).FontSize(12).TextColor(pal.muted).FontFeatures("tnum")
}

// detail shows the prompt chosen whole. Task 4 fills it in.
func (h *history) detail(c *ui.Context, a *App, pal *palette) {}
