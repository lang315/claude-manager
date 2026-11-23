package account

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"time"
)

// Pick is claude-swap's switch with no account, and its TUI's best pick:
// rotate (the next slot after the account in use, round), best (the most
// quota left) or next-available (the next slot with room), over the
// accounts as last read, skipping those held out, open in a terminal,
// without an oauthAccount, or in use.
func (b *Book) Pick(strategy string) (Account, error) {
	accts, err := b.Blocks()
	if err != nil {
		return Account{}, err
	}
	now := b.Now()
	cur := 0
	if i := slices.IndexFunc(accts, func(a Account) bool { return a.InUse }); i >= 0 {
		cur = accts[i].Slot
	}
	var cands []Account
	for _, a := range accts {
		if a.Added && !a.InUse && !a.AutoOff && a.Terminal.Dir == "" && a.OAuthAccount != nil {
			cands = append(cands, a)
		}
	}
	slices.SortStableFunc(cands, func(x, y Account) int {
		rx, ry := x.Slot, y.Slot
		if rx <= cur {
			rx += 1 << 20
		}
		if ry <= cur {
			ry += 1 << 20
		}
		return rx - ry
	})
	switch strategy {
	case "", "rotate":
		if len(cands) > 0 {
			return cands[0], nil
		}
	case "next-available":
		s, _ := b.AutoInEffect()
		for _, a := range cands {
			if h, ok := headroom(a, s); a.Usable(now) && ok && h > 0 {
				return a, nil
			}
		}
	case "best":
		return b.best(accts, cands, now)
	default:
		return Account{}, errors.New("--strategy is rotate, best or next-available")
	}
	return Account{}, b.noneLeft(accts)
}

// Stay is Pick's answer when no switch would help, in claude-swap's words;
// Reason is its JSON reason.
type Stay struct{ Reason, Message string }

func (s *Stay) Error() string { return s.Message }

// best is claude-swap's _select_best_switchable: an account with strictly
// more room than the one in use, counting --model's windows; else Stay.
// Ties stay, as does a comparison that can't be made.
func (b *Book) best(accts, cands []Account, now time.Time) (Account, error) {
	if len(cands) == 0 {
		return Account{}, b.noneLeft(accts)
	}
	s, _ := b.AutoInEffect()
	cur := slices.IndexFunc(accts, func(a Account) bool { return a.InUse })
	here := "the account in use"
	curRoom, curKnown := 0.0, false
	if cur >= 0 {
		here = accts[cur].Email
		curRoom, curKnown = headroom(accts[cur], s)
		curKnown = curKnown && accts[cur].Usable(now)
	}
	if !curKnown {
		return Account{}, &Stay{"usage-unavailable", "The account in use has no usage to compare: staying on " + here + ". claude-manager switch rotates."}
	}
	best, room, unknown := Account{}, -1.0, false
	for _, a := range cands {
		h, ok := headroom(a, s)
		if !ok || !a.Usable(now) {
			unknown = true
			continue
		}
		if h > room {
			best, room = a, h
		}
	}
	switch {
	case room < 0:
		return Account{}, &Stay{"usage-unavailable", "No other account has usage to compare: staying on " + here + ". claude-manager switch rotates."}
	case room > curRoom:
		return best, nil
	case unknown:
		return Account{}, &Stay{"usage-unavailable", "No account with known usage has more room; some usage is unavailable: staying on " + here + "."}
	case curRoom <= 0:
		limits := "5h/7d limits"
		if s.Model != "" {
			limits = "usage limits"
		}
		return Account{}, &Stay{"candidates-exhausted", "All accounts are at their " + limits + ": staying on " + here + "."}
	}
	return Account{}, &Stay{"already-best", "Already on the account with the most room (" + here + ")."}
}

// noneLeft says why there is no account to switch to.
func (b *Book) noneLeft(accts []Account) error {
	added := 0
	for _, a := range accts {
		if a.Added {
			added++
		}
	}
	if added <= 1 {
		return &Stay{"only-one-account", "Only one account is added: add more to switch between."}
	}
	return &Stay{"no-valid-target", "No account to switch to: the others are held out of auto-switch, open in a terminal, or can't be switched to."}
}

// headroom is the room left before a's fullest window: five-hour, 7-day,
// and the per-model windows s.Model names (claude-swap's account_headroom).
func headroom(a Account, s Auto) (float64, bool) {
	p, ok := Fullest(a)
	for _, l := range a.Limits {
		if l.Kind == "weekly_scoped" && s.counts(l.Scope) {
			p, ok = max(p, l.Percent), true
		}
	}
	return 100 - p, ok
}

// Fullest is the highest share of a's five-hour and 7-day windows.
func Fullest(a Account) (float64, bool) {
	p, ok := 0.0, false
	for _, l := range a.Limits {
		switch l.Kind {
		case "session", "five_hour", "weekly_all", "seven_day":
			p, ok = max(p, l.Percent), true
		}
	}
	return p, ok
}

// Candidate is one account in claude-swap's TUI "next best" list: its
// quota left before the fullest window, or Known false when it has no
// reading to go by.
type Candidate struct {
	Email string
	Left  float64
	Known bool
}

// Ranking lists the accounts auto-switch could move to, the most quota
// left first, those without a usable reading last.
func (b *Book) Ranking() ([]Candidate, error) {
	accts, err := b.Blocks()
	if err != nil {
		return nil, err
	}
	now := b.Now()
	var out []Candidate
	for _, a := range accts {
		if !a.Added || a.InUse || a.AutoOff || a.Terminal.Dir != "" || a.APIKey {
			continue
		}
		p, ok := Fullest(a)
		out = append(out, Candidate{Email: a.Email, Left: 100 - p, Known: ok && a.Usable(now)})
	}
	slices.SortStableFunc(out, func(x, y Candidate) int {
		switch {
		case x.Known != y.Known:
			if x.Known {
				return -1
			}
			return 1
		case x.Left > y.Left:
			return -1
		case x.Left < y.Left:
			return 1
		}
		return 0
	})
	return out, nil
}

// AddInUse adds the account Claude Code is signed in to, as claude-swap's
// add from the TUI: its login saved, renewed by Claude Code alone while in
// use (Refresh never renews the login in use's refresh token).
func (b *Book) AddInUse(ctx context.Context) (Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Account{}, err
	}
	live, who, err := b.identify(ctx, b.Now())
	if err != nil {
		return Account{}, err
	}
	blob := live.Marshal()
	var acct json.RawMessage
	if b.Home != nil {
		if lb, err := b.Home.readLive(b.Vault); err == nil && oauthOf(lb.data) != nil {
			blob = lb.data
		}
		conf, _ := os.ReadFile(b.Home.ClaudeJSON)
		if a := oauthAccountOf(conf); keyOf(a) == who.Key() {
			acct = a
		}
	}
	a, err := b.keepLocked(blob, *who, acct)
	if err != nil {
		return Account{}, err
	}
	return a, b.save()
}
