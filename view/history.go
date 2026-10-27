package view

import (
	"strings"
	"time"

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
