package account

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"time"
)

var errPitboard = errors.New("Pitboard's files could not be read.")

// ReadPitboard reads the Claude Code accounts Pitboard follows
// (github.com/datlechin/pitboard), and its last reading of each, from its
// folder's state.json and usage.json, which hold no secret. It reads
// nothing else there: Pitboard's logins are its own to renew. No folder is
// no error; a row it can't read is left out.
func ReadPitboard(dir string) ([]Account, error) {
	var st struct {
		Accounts []struct {
			ID          string `json:"id"`
			Provider    string `json:"provider"`
			Email       string `json:"email"`
			Plan        string `json:"plan"`
			AccountUUID string `json:"account_uuid"`
			OrgUUID     string `json:"organization_uuid"`
		} `json:"accounts"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &st)
	}
	if err != nil {
		return nil, errPitboard
	}
	var usage map[string]struct {
		ObservedAt int64             `json:"observed_at"`
		Windows    []json.RawMessage `json:"windows"`
	}
	data, err = os.ReadFile(filepath.Join(dir, "usage.json"))
	switch {
	case err == nil:
		if json.Unmarshal(data, &usage) != nil {
			return nil, errPitboard
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, errPitboard
	}
	var out []Account
	for _, p := range st.Accounts {
		if p.Provider != "claude" || p.AccountUUID == "" || p.OrgUUID == "" {
			continue
		}
		a := Account{AccountUUID: p.AccountUUID, OrgUUID: p.OrgUUID, Email: p.Email, Plan: p.Plan, Source: "Pitboard"}
		u := usage[p.ID]
		for _, raw := range u.Windows {
			var w struct {
				Kind     string   `json:"kind"`
				Scope    *string  `json:"scope"`
				Percent  *float64 `json:"percent"`
				ResetsAt int64    `json:"resets_at"`
			}
			if json.Unmarshal(raw, &w) != nil || w.Kind == "" || !valid(w.Percent) {
				continue
			}
			l := Limit{Kind: w.Kind, Percent: math.Min(*w.Percent, 100)}
			if w.Scope != nil {
				l.Scope = *w.Scope
			}
			if w.ResetsAt > 0 {
				l.ResetsAt = time.Unix(w.ResetsAt, 0)
			}
			a.Limits = append(a.Limits, l)
		}
		if len(a.Limits) > 0 && u.ObservedAt > 0 {
			a.ReadAt = time.Unix(u.ObservedAt, 0)
		}
		out = append(out, a)
	}
	return out, nil
}
