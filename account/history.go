package account

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"
)

// What claude-swap's log and menu bar keep of switches and events: the
// recent switches, shown in the menu bar, and a hook for its notifications
// (auto-switched, quarantined, all exhausted, failed).

// Switched is one switch: when, from and to whom, and why.
type Switched struct {
	At   time.Time `json:"at"`
	From string    `json:"from,omitempty"`
	To   string    `json:"to"`
	Why  string    `json:"why"`
	Auto bool      `json:"auto,omitempty"`
}

const historyMax = 50

// Switches lists the recent switches, the latest first.
func (b *Book) Switches() ([]Switched, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	out := slices.Clone(b.history)
	slices.Reverse(out)
	return out, err
}

// emailOf is the account key's email, "" for none.
func (b *Book) emailOf(key string) string {
	for _, a := range b.blocks() {
		if a.Key() == key {
			return a.Email
		}
	}
	return ""
}

// inUseEmail is the account in use's email: as last read, else as
// ~/.claude.json names it; "" for none.
func (b *Book) inUseEmail() string {
	for _, a := range b.blocks() {
		if a.InUse {
			return a.Email
		}
	}
	if b.Home == nil {
		return ""
	}
	conf, _ := os.ReadFile(b.Home.ClaudeJSON)
	var o struct {
		Email string `json:"emailAddress"`
	}
	_ = json.Unmarshal(oauthAccountOf(conf), &o)
	return o.Email
}

// record keeps a switch and tells the log and Events.
func (b *Book) record(from, to, why string, auto bool) {
	b.history = append(b.history, Switched{At: b.Now(), From: from, To: to, Why: why, Auto: auto})
	if len(b.history) > historyMax {
		b.history = slices.Delete(b.history, 0, len(b.history)-historyMax)
	}
	b.logf("switch %s -> %s (%s)", from, to, why)
	if auto {
		b.event("auto-switched", "Auto-switched account", fmt.Sprintf("%s: %s.", to, why))
	} else { // claude-swap's menu bar tells a switch by hand too
		b.event("switched", "Switched account", "Claude Code is on "+to+".")
	}
}

// event tells Events of something worth a notification, once until it
// changes: kind names it, and the same kind and body aren't told twice
// in a row.
func (b *Book) event(kind, title, body string) {
	b.side.Lock()
	if b.told[kind] == body {
		b.side.Unlock()
		return
	}
	if b.told == nil {
		b.told = map[string]string{}
	}
	b.told[kind] = body
	b.side.Unlock()
	if b.Events != nil {
		b.Events(title, body)
	}
}

// logf writes a line to Log when set: never a token.
func (b *Book) logf(format string, args ...any) {
	if b.Log != nil {
		b.side.Lock()
		defer b.side.Unlock()
		fmt.Fprintf(b.Log, "%s %s\n", b.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
	}
}

// Duplicates are the added accounts that hold one login (claude-swap's
// _duplicate_account_warnings): the same refresh token, or the same key,
// under two accounts; one renewing it spends the other's. Emails, paired.
func (b *Book) Duplicates() [][2]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.load() != nil {
		return nil
	}
	seen := map[string]string{}
	var out [][2]string
	for _, a := range b.added {
		saved, err := b.Vault.Get(ParkService, a.Key())
		if err != nil {
			continue
		}
		id := string(saved) // an API key
		if l, err := parseLogin(saved); err == nil {
			id = l.RefreshToken
			if id == "" {
				id = l.AccessToken // a setup-token
			}
		}
		if other, ok := seen[id]; ok && id != "" {
			out = append(out, [2]string{other, a.Email})
			continue
		}
		seen[id] = a.Email
	}
	return out
}
