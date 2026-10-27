package view

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lang315/claude-manager/internal/fixture"
	"github.com/lang315/claude-manager/store"
)

func TestRangeOf(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	var open time.Time
	for _, c := range []struct {
		name     string
		preset   int
		from, to string
		want     dateRange
	}{
		{"today", presetToday, "", "", dateRange{true, day(10, 26), open}},
		{"7 days, today included", preset7Days, "", "", dateRange{true, day(10, 20), open}},
		{"30 days", preset30Days, "", "", dateRange{true, day(9, 27), open}},
		{"all", presetAll, "", "", dateRange{}},
		{"custom, both days whole", presetCustom, "2026-08-01", "2026-08-31", dateRange{true, day(8, 1), day(9, 1)}},
		{"custom, from open", presetCustom, "", "2026-08-31", dateRange{true, open, day(9, 1)}},
		{"custom, both open", presetCustom, "", "", dateRange{true, open, open}},
		{"custom, bad date turns it off", presetCustom, "2026-13-01", "", dateRange{}},
	} {
		got := rangeOf(c.preset, now, c.from, c.to)
		if got.on != c.want.on || !got.lo.Equal(c.want.lo) || !got.hi.Equal(c.want.hi) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestParseDay(t *testing.T) {
	if d, msg := parseDay(" 2026-08-01 ", time.UTC); msg != "" || !d.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("parseDay(2026-08-01) = %v, %q", d, msg)
	}
	if d, msg := parseDay("", time.UTC); msg != "" || !d.IsZero() {
		t.Errorf("an empty field is %v, %q; want open, no message", d, msg)
	}
	for _, bad := range []string{"2026-8-1", "26/10/2026", "2026-02-30", "yesterday"} {
		if _, msg := parseDay(bad, time.UTC); msg != "Use YYYY-MM-DD" {
			t.Errorf("parseDay(%q) message %q, want Use YYYY-MM-DD", bad, msg)
		}
	}
}

func TestDateRangeKeeps(t *testing.T) {
	lo := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	hi := lo.AddDate(0, 0, 1)
	r := dateRange{true, lo, hi}
	for _, c := range []struct {
		t    time.Time
		want bool
	}{
		{lo, true}, {hi.Add(-time.Millisecond), true}, {hi, false}, {lo.Add(-time.Millisecond), false}, {time.Time{}, false},
	} {
		if got := r.keeps(c.t); got != c.want {
			t.Errorf("keeps(%v) = %v, want %v", c.t, got, c.want)
		}
	}
	if !(dateRange{}).keeps(time.Time{}) {
		t.Error("All drops a prompt without a time")
	}
}

func TestMatchPrompt(t *testing.T) {
	p := &store.Prompt{Text: "Review THIS", Pastes: []store.Paste{{ID: 1, Content: "func main() {}"}}}
	for kw, want := range map[string]bool{"": true, "   ": true, "this": true, "FUNC MAIN": true, "nope": false} {
		if got := matchPrompt(p, kw); got != want {
			t.Errorf("matchPrompt(%q) = %v, want %v", kw, got, want)
		}
	}
}

// shownLines are the history.jsonl lines of the prompts the table shows.
func shownLines(a *App) []int {
	var out []int
	for _, i := range a.history.shown {
		out = append(out, a.history.prompts[i].Line)
	}
	return out
}

func TestHistoryShowsPrompts(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	if !tt.HasText("5 prompts in 2 projects · 1 unreadable line") {
		t.Errorf("header; texts %q", tt.Texts())
	}
	for _, s := range []string{"Fix the login bug", "Review this:", "Old idea", "No time prompt", "5 prompts"} {
		if !tt.HasText(s) {
			t.Errorf("%q not shown; texts %q", s, tt.Texts())
		}
	}
	if got := shownLines(a); !slices.Equal(got, []int{2, 4, 5, 1, 6}) {
		t.Errorf("rows %v, want newest first", got)
	}
}

func TestHistorySearchesPastes(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	a.history.query = "FUNC MAIN"
	tt.Frame()
	if got := shownLines(a); !slices.Equal(got, []int{4}) {
		t.Errorf("search in a paste shows %v, want [4]", got)
	}
	a.history.query = "nothing like it"
	tt.Frame()
	if !tt.HasText("No prompt matches.") {
		t.Errorf("no empty state; texts %q", tt.Texts())
	}
}

func TestHistoryFilters(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	h := &a.history
	for _, c := range []struct {
		name     string
		project  string
		preset   int
		from, to string
		want     []int
	}{
		{"today", allProjects, presetToday, "", "", []int{2}},
		{"7 days", allProjects, preset7Days, "", "", []int{2, 4}},
		{"30 days", allProjects, preset30Days, "", "", []int{2, 4, 5}},
		{"all", allProjects, presetAll, "", "", []int{2, 4, 5, 1, 6}},
		{"custom august", allProjects, presetCustom, "2026-08-01", "2026-08-31", []int{1}},
		{"custom bad date keeps every row", allProjects, presetCustom, "2026-13-01", "", []int{2, 4, 5, 1, 6}},
		{"custom from after to", allProjects, presetCustom, "2026-09-01", "2026-08-01", nil},
		{"one project", "/Users/me/other", presetAll, "", "", []int{4, 6}},
		{"project and date", "/Users/me/other", preset7Days, "", "", []int{4}},
	} {
		h.project, h.preset, h.from, h.to = c.project, c.preset, c.from, c.to
		tt.Frame()
		if got := shownLines(a); !slices.Equal(got, c.want) {
			t.Errorf("%s: rows %v, want %v", c.name, got, c.want)
		}
	}
	h.project, h.preset, h.from = allProjects, presetCustom, "2026-13-01"
	tt.Frame()
	if !tt.HasText("Use YYYY-MM-DD") {
		t.Errorf("a bad date says nothing; texts %q", tt.Texts())
	}
}

// The Segmented drives the same filter as the field.
func TestHistoryPresetClick(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	must(t, tt.Click("Today"))
	if got := shownLines(a); !slices.Equal(got, []int{2}) {
		t.Errorf("Today shows %v, want [2]", got)
	}
}

func TestHistoryRescanDropsGoneProject(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, tt.Click("History"))
	a.history.project = "/Users/me/other"
	tt.Frame()
	must(t, os.WriteFile(filepath.Join(d.Root, "history.jsonl"),
		[]byte(`{"display":"only","timestamp":1,"project":"/Users/me/proj","sessionId":"`+fixture.Old+`"}`+"\n"), 0o644))
	a.Rescan()
	tt.Frame()
	if a.history.project != allProjects || len(a.history.shown) != 1 {
		t.Errorf("after the project left: choice %q, %d rows; want All projects, 1", a.history.project, len(a.history.shown))
	}
}

func TestHistoryEmpty(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, os.Remove(filepath.Join(d.Root, "history.jsonl")))
	a.Rescan()
	tt.Frame()
	must(t, tt.Click("History"))
	if !tt.HasText("No prompt history yet.") {
		t.Errorf("no empty state; texts %q", tt.Texts())
	}
}
