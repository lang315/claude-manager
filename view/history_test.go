package view

import (
	"testing"
	"time"

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
