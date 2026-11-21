// Package account reads how much of each Claude Code account's limits is
// used, from Anthropic: with the login Claude Code holds for the account in
// use, and with logins of its own for the accounts the user adds.
package account

import (
	"encoding/json"
	"errors"
	"math"
	"time"
)

// Limit is one of an account's limits, as Anthropic's usage answer gives it.
type Limit struct {
	Kind     string    `json:"kind"`              // session, weekly_all, weekly_scoped, or another
	Scope    string    `json:"scope,omitempty"`   // the model or surface a scoped limit is for
	Percent  float64   `json:"percent"`           // used, 0 to 100
	ResetsAt time.Time `json:"resetsAt,omitzero"` // zero where the answer gave none
}

// Label names the limit as the Limits page shows it.
func (l Limit) Label() string {
	name := l.Kind
	switch l.Kind {
	case "session", "five_hour":
		name = "Session (5h)"
	case "weekly_all", "seven_day", "weekly_scoped":
		name = "Weekly"
	}
	if l.Scope != "" {
		name += " · " + l.Scope
	}
	return name
}

// Pace is claude-swap's weekly pace (pace.py): expected is the share of a
// weekly window used on schedule, read at readAt, from how far into its
// 7-day cycle it is; ahead is 15 points or more past it. ok is false for
// a window that isn't weekly, has no reset, or reset less than a day ago,
// when nearly any use reads as ahead.
func (l Limit) Pace(readAt time.Time) (expected float64, ahead, ok bool) {
	switch l.Kind {
	case "weekly_all", "seven_day", "weekly_scoped":
	default:
		return 0, false, false
	}
	if l.ResetsAt.IsZero() || readAt.IsZero() {
		return 0, false, false
	}
	const week = 7 * 24 * time.Hour
	left := l.ResetsAt.Sub(readAt) % week
	if left < 0 {
		left += week
	}
	elapsed := week - left
	if left == 0 {
		elapsed = 0
	}
	if elapsed < 24*time.Hour {
		return 0, false, false
	}
	expected = min(100, 100*float64(elapsed)/float64(week))
	return expected, l.Percent-expected >= 15, true
}

var errNoLimits = errors.New("Anthropic's answer holds no limits.")

type scoped struct {
	DisplayName string `json:"display_name"`
}

type row struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt string   `json:"resets_at"`
	Scope    *struct {
		Model   *scoped `json:"model"`
		Surface *scoped `json:"surface"`
	} `json:"scope"`
}

type window struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// ParseUsage reads the answer of /api/oauth/usage: its limits rows or,
// where none can be read, its five_hour and seven_day windows. A row it
// cannot read is left out.
func ParseUsage(body []byte) ([]Limit, error) {
	var u struct {
		FiveHour *window           `json:"five_hour"`
		SevenDay *window           `json:"seven_day"`
		Limits   []json.RawMessage `json:"limits"`
	}
	if json.Unmarshal(body, &u) != nil {
		return nil, errNoLimits
	}
	var out []Limit
	for _, raw := range u.Limits {
		var r row
		if json.Unmarshal(raw, &r) != nil {
			continue
		}
		scope := ""
		if s := r.Scope; s != nil {
			if s.Model != nil && s.Model.DisplayName != "" {
				scope = s.Model.DisplayName
			} else if s.Surface != nil {
				scope = s.Surface.DisplayName
			}
		}
		if l, ok := limitOf(r.Kind, scope, r.Percent, when(r.ResetsAt)); ok {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		for _, w := range []struct {
			kind string
			w    *window
		}{{"five_hour", u.FiveHour}, {"seven_day", u.SevenDay}} {
			if w.w == nil {
				continue
			}
			if l, ok := limitOf(w.kind, "", w.w.Utilization, when(w.w.ResetsAt)); ok {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 {
		return nil, errNoLimits
	}
	return out, nil
}

// limitOf makes a limit of a row: none without a kind or a share it can
// read. A share over 100 shows as 100.
func limitOf(kind, scope string, pct *float64, resets time.Time) (Limit, bool) {
	if kind == "" || !valid(pct) {
		return Limit{}, false
	}
	return Limit{Kind: kind, Scope: scope, Percent: math.Min(*pct, 100), ResetsAt: resets}, true
}

func valid(p *float64) bool { return p != nil && *p >= 0 && !math.IsInf(*p, 0) }

// when reads an RFC 3339 time, with or without fractions; zero if it can't.
func when(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
