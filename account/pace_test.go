package account

import (
	"testing"
	"time"
)

// Cases from claude-swap's tests/test_pace.py rules: the cycle start is the
// reset rolled back by whole weeks; nothing in the first day.
func TestPace(t *testing.T) {
	read := t0
	weekly := func(pct float64, resetIn time.Duration) Limit {
		return Limit{Kind: "weekly_all", Percent: pct, ResetsAt: read.Add(resetIn)}
	}
	for _, c := range []struct {
		name     string
		l        Limit
		expected float64
		ahead    bool
		ok       bool
	}{
		{"half way, on pace", weekly(50, 84*time.Hour), 50, false, true},
		{"half way, 15 ahead", weekly(65, 84*time.Hour), 50, true, true},
		{"half way, 14.9 ahead", weekly(64.9, 84*time.Hour), 50, false, true},
		{"first day: never", weekly(90, 7*24*time.Hour-23*time.Hour), 0, false, false},
		{"a reset weeks old rolls forward", weekly(65, 84*time.Hour-14*24*time.Hour), 50, true, true},
		{"a reset weeks ahead rolls back", weekly(65, 84*time.Hour+7*24*time.Hour), 50, true, true},
		{"per-model too", Limit{Kind: "weekly_scoped", Scope: "Fable", Percent: 80, ResetsAt: read.Add(84 * time.Hour)}, 50, true, true},
		{"five-hour: never", Limit{Kind: "session", Percent: 99, ResetsAt: read.Add(time.Hour)}, 0, false, false},
		{"no reset", Limit{Kind: "weekly_all", Percent: 99}, 0, false, false},
	} {
		e, ahead, ok := c.l.Pace(read)
		if ok != c.ok || ahead != c.ahead || ok && (e < c.expected-0.01 || e > c.expected+0.01) {
			t.Errorf("%s: %.2f %v %v, want %.2f %v %v", c.name, e, ahead, ok, c.expected, c.ahead, c.ok)
		}
	}
}
