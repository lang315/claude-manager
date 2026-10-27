package view

import (
	"fmt"
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

// foldPrompt is what the search reads of a prompt: its text and pastes in
// lower case, made once at load rather than on every keystroke.
func foldPrompt(p *store.Prompt) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(p.Text))
	for _, x := range p.Pastes {
		b.WriteByte(0) // a keyword never spans two pastes
		b.WriteString(strings.ToLower(x.Content))
	}
	return b.String()
}

// history is the History screen: every prompt typed, found by search,
// project and date.
type history struct {
	prompts  []store.Prompt
	folded   []string // foldPrompt of each prompt
	bad      int
	err      string   // why history.jsonl could not be read
	projects []string // allProjects, then each project as tilde shows it, newest prompt first
	shown    []int    // indexes in prompts of the rows the filters keep, newest first
	key      filterKey
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
		folded := make([]string, len(prompts))
		for i := range prompts {
			folded[i] = foldPrompt(&prompts[i])
		}
		return func() {
			line := 0
			if p := h.chosen(); p != nil {
				line = p.Line // Claude Code only appends: a line keeps its prompt
			}
			h.prompts, h.folded, h.bad, h.err = prompts, folded, bad, errText(err)
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
			k := h.key
			k.project = h.project
			h.apply(k, line)
		}
	})
}

// filter works out the rows shown again when a filter changed: one pass over
// the prompts, not one a frame. The row chosen stays chosen while it passes.
func (h *history) filter(k filterKey) {
	if k == h.key {
		return
	}
	line := 0
	if p := h.chosen(); p != nil {
		line = p.Line
	}
	h.apply(k, line)
}

// apply shows the prompts k lets through, and chooses the one on line again
// when it is among them.
func (h *history) apply(k filterKey, line int) {
	h.key, h.at = k, -1
	h.shown = h.shown[:0]
	kw := strings.ToLower(strings.TrimSpace(k.query))
	for i := range h.prompts {
		p := &h.prompts[i]
		if (k.project == allProjects || tilde(p.Project) == k.project) && k.dates.keeps(p.Time) && strings.Contains(h.folded[i], kw) {
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
	case h.err != "":
		blank(c, pal, "history.jsonl could not be read.")
		return
	case len(h.prompts) == 0:
		blank(c, pal, "No prompt history yet.")
		return
	case len(h.shown) == 0:
		blank(c, pal, "No prompt matches.")
		return
	}
	cols := []ui.TableColumn{{Title: "Time", Width: 150}, {Title: "Project", Width: 140}, {Title: "Prompt", MinWidth: 160}}
	ui.Table(c, &h.list, cols, len(h.shown), func(row, col int) {
		p := &h.prompts[h.shown[row]]
		switch col {
		case 0:
			ui.Text(c, fmtTime(p.Time)).FontFeatures("tnum").SingleLine()
		case 1:
			ui.Text(c, shortPath(p.Project)).SingleLine().Tooltip(p.Project)
		case 2:
			ui.Text(c, store.FirstLine(p.Text, 200)).SingleLine()
		}
	}).Grow(1).MinHeight(0)
	ui.Text(c, plural(len(h.shown), "prompt")).FontSize(12).TextColor(pal.muted).FontFeatures("tnum")
}

// detail shows the prompt chosen whole, with its pastes, and the keys to
// copy it or open its session.
func (h *history) detail(c *ui.Context, a *App, pal *palette) {
	p := h.chosen()
	note := ""
	if p != nil {
		note = fmtTime(p.Time)
	}
	sheet(c, pal, "Prompt", note, func() {
		if p == nil {
			blank(c, pal, "Choose a prompt to see it whole.")
			return
		}
		ui.Scroll(c.Key(p.Line)).Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).Padding(16, 20).Gap(12).Children(func() {
				ui.Text(c, tilde(p.Project)).FontSize(12).TextColor(pal.muted).SingleLine().Tooltip(p.Project)
				ui.Text(c, clip(p.Text)).Selectable()
				for _, x := range p.Pastes {
					label := fmt.Sprintf("Pasted text #%d (%s)", x.ID, plural(lineCount(x.Content), "line"))
					ui.Collapsible(c, label, h.pasteOpenFor(p.Line, x.ID), func() {
						ui.Text(c, clip(x.Content)).Font("monospace").FontSize(12).Selectable()
					})
				}
				if p.Lost > 0 {
					ui.Text(c, plural(p.Lost, "paste")+" no longer stored").FontSize(12).TextColor(pal.muted)
				}
			})
		})
		ui.Divider(c)
		ui.Row(c).PaddingX(16).PaddingY(12).Gap(8).Children(func() {
			if ui.Button(c, "Copy").Clicked() {
				c.WriteClipboard(p.Expand())
				a.notice = "Copied"
			}
			x := a.sessions.find(p.SessionID)
			open := ui.Button(c, "Open session").Disabled(x == nil)
			if hint := sessionHint(x != nil, !a.sessions.loaded); hint != "" {
				open.Tooltip(hint)
			}
			if open.Clicked() {
				a.page = "sessions"
				a.sessions.openTranscript(a, *x)
			}
		})
	})
}

// pasteOpenFor is whether a paste of the prompt on line is expanded.
func (h *history) pasteOpenFor(line, id int) *bool {
	k := [2]int{line, id}
	if h.pasteOpen[k] == nil {
		h.pasteOpen[k] = new(bool)
	}
	return h.pasteOpen[k]
}

// lineCount counts a text's lines, a last line without "\n" included.
func lineCount(s string) int {
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}

// sessionHint is why Open session is off, "" when it is on. A session not
// found before the first scan lands may be one it hasn't reached yet.
func sessionHint(found, loading bool) string {
	switch {
	case found:
		return ""
	case loading:
		return "Sessions are still loading"
	}
	return "Session no longer on disk"
}
