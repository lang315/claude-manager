package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"time"
)

// Readings is claude-swap's `cswap list --json` (schemaVersion 1): one
// Mac's usage readings, to hand to another so they don't both poll
// (`cswap import-usage`).
type readingsFile struct {
	Schema   int          `json:"schemaVersion"`
	Accounts []readingRow `json:"accounts"`
}

type readingRow struct {
	Email   string   `json:"email"`
	OrgUUID *string  `json:"organizationUuid"`
	Status  string   `json:"usageStatus"`
	Age     *float64 `json:"usageAgeSeconds"`
	Usage   *struct {
		FiveHour *readingWindow `json:"fiveHour"`
		SevenDay *readingWindow `json:"sevenDay"`
		Spend    *struct {
			readingWindow
			Used     *float64 `json:"used"`
			Limit    *float64 `json:"limit"`
			Currency string   `json:"currency"`
		} `json:"spend"`
		Scoped []struct {
			readingWindow
			Name string `json:"name"`
		} `json:"scoped"`
	} `json:"usage"`
}

type readingWindow struct {
	Pct      *float64 `json:"pct"`
	ResetsAt string   `json:"resetsAt"`
}

// ReadingsImported counts what ImportUsage did: readings adopted, kept
// (the one here was newer), and rows skipped (no usable reading, or no
// such account here).
type ReadingsImported struct{ Adopted, Kept, Skipped int }

// IsReadings reports whether data is a readings file rather than an
// accounts export.
func IsReadings(data []byte) bool {
	var f struct {
		Schema *int `json:"schemaVersion"`
	}
	return json.Unmarshal(data, &f) == nil && f.Schema != nil
}

const holdMax = time.Hour // a held reading is never trusted past this age

// ImportUsage adopts the readings in the file at path, as claude-swap's
// import-usage: each "ok" row, matched to an account by email and
// organization, replaces the reading here when it is newer. hold > 0 also
// keeps the app from reading those accounts itself for that long, never
// past the reading's earliest reset nor an hour after it was taken, and
// trusts the reading meanwhile; hold 0 lifts earlier holds; hold < 0
// leaves them. The whole file is checked before anything changes.
func (b *Book) ImportUsage(path string, hold time.Duration) (ReadingsImported, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ReadingsImported{}, err
	}
	var f readingsFile
	if json.Unmarshal(data, &f) != nil || f.Schema == 0 {
		return ReadingsImported{}, errors.New("This isn't a readings file from cswap list --json.")
	}
	if f.Schema != 1 {
		return ReadingsImported{}, fmt.Errorf("This readings file is schema %d; Claude Manager reads 1.", f.Schema)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return ReadingsImported{}, err
	}
	now := b.Now()
	type reading struct {
		a      *Account
		limits []Limit
		at     time.Time
	}
	var in []reading
	var got ReadingsImported
	accts := b.added
	if b.inUse != nil {
		accts = append(slices.Clip(accts), *b.inUse)
	}
	for i, r := range f.Accounts {
		bad := func(why string) error {
			return fmt.Errorf("Row %d of this readings file %s, so nothing was imported.", i+1, why)
		}
		if r.Email == "" || r.OrgUUID == nil {
			return ReadingsImported{}, bad("has no email or organization")
		}
		if r.Status != "ok" {
			got.Skipped++
			continue
		}
		if r.Age == nil || *r.Age < 0 || math.IsInf(*r.Age, 0) || math.IsNaN(*r.Age) {
			return ReadingsImported{}, bad("has no age for its reading")
		}
		limits, err := r.limits()
		if err != nil {
			return ReadingsImported{}, bad(err.Error())
		}
		j := slices.IndexFunc(accts, func(a Account) bool { return a.Email == r.Email && a.OrgUUID == *r.OrgUUID })
		if j < 0 {
			got.Skipped++
			continue
		}
		var a *Account
		if k := slices.IndexFunc(b.added, func(x Account) bool { return x.Key() == accts[j].Key() }); k >= 0 {
			a = &b.added[k]
		} else {
			a = b.inUse
		}
		if slices.ContainsFunc(in, func(x reading) bool { return x.a == a }) {
			return ReadingsImported{}, bad("repeats an account")
		}
		in = append(in, reading{a, limits, now.Add(-time.Duration(*r.Age * float64(time.Second)))})
	}
	for _, r := range in {
		a := r.a
		if r.at.After(a.ReadAt) {
			a.Limits, a.ReadAt, a.Err, a.WaitUntil = r.limits, r.at, "", time.Time{}
			a.Failures, a.Retry = 0, time.Time{}
			got.Adopted++
		} else {
			got.Kept++
		}
		switch {
		case hold == 0:
			a.HeldUntil = time.Time{}
		case hold > 0:
			until := now.Add(hold)
			if cap := a.ReadAt.Add(holdMax); cap.Before(until) {
				until = cap
			}
			for _, l := range a.Limits {
				if l.ResetsAt.After(now) && l.ResetsAt.Before(until) {
					until = l.ResetsAt
				}
			}
			a.HeldUntil = until
		}
	}
	return got, b.save()
}

func (r readingRow) limits() ([]Limit, error) {
	u := r.Usage
	if u == nil {
		return nil, errors.New("has no usage")
	}
	var out []Limit
	add := func(kind, scope string, w readingWindow, name string) error {
		if w.Pct == nil {
			return nil // reported without a figure
		}
		if !valid(w.Pct) {
			return fmt.Errorf("has a bad %s share", name)
		}
		var resets time.Time
		if w.ResetsAt != "" {
			if resets = when(w.ResetsAt); resets.IsZero() {
				return fmt.Errorf("has a bad %s reset", name)
			}
		}
		out = append(out, Limit{Kind: kind, Scope: scope, Percent: math.Min(*w.Pct, 100), ResetsAt: resets})
		return nil
	}
	if u.FiveHour != nil {
		if err := add("session", "", *u.FiveHour, "five-hour"); err != nil {
			return nil, err
		}
	}
	if u.SevenDay != nil {
		if err := add("weekly_all", "", *u.SevenDay, "7-day"); err != nil {
			return nil, err
		}
	}
	for _, s := range u.Scoped {
		if s.Name == "" {
			return nil, errors.New("has a model window with no name")
		}
		if err := add("weekly_scoped", s.Name, s.readingWindow, s.Name); err != nil {
			return nil, err
		}
	}
	if s := u.Spend; s != nil {
		if !valid(s.Used) || !valid(s.Limit) || s.Currency == "" {
			return nil, errors.New("has a bad spend")
		}
		if err := add("extra_usage", "", s.readingWindow, "spend"); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].Kind == "extra_usage" {
			out[n-1].Used, out[n-1].Cap, out[n-1].Currency = *s.Used, *s.Limit, s.Currency
		}
	}
	if len(out) == 0 {
		return nil, errors.New("carries no windows")
	}
	return out, nil
}
