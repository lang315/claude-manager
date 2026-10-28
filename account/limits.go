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
		if json.Unmarshal(raw, &r) != nil || r.Kind == "" || !valid(r.Percent) {
			continue
		}
		l := Limit{Kind: r.Kind, Percent: math.Min(*r.Percent, 100), ResetsAt: when(r.ResetsAt)}
		if s := r.Scope; s != nil {
			if s.Model != nil && s.Model.DisplayName != "" {
				l.Scope = s.Model.DisplayName
			} else if s.Surface != nil {
				l.Scope = s.Surface.DisplayName
			}
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		for _, w := range []struct {
			kind string
			w    *window
		}{{"five_hour", u.FiveHour}, {"seven_day", u.SevenDay}} {
			if w.w != nil && valid(w.w.Utilization) {
				out = append(out, Limit{Kind: w.kind, Percent: math.Min(*w.w.Utilization, 100), ResetsAt: when(w.w.ResetsAt)})
			}
		}
	}
	if len(out) == 0 {
		return nil, errNoLimits
	}
	return out, nil
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
