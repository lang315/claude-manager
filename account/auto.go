package account

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"
)

// The strategies, by claude-swap's names (docs/auto-switch.md).
const (
	Best         = "best"
	WeeklyFirst  = "weekly-first"
	ConsumeFirst = "consume-first"
	ByPriority   = "priority-consume-first"
)

// Auto is how auto-switch decides, with claude-swap's defaults and ranges.
type Auto struct {
	On         bool    `json:"on"`
	Line       float64 `json:"line"`       // the five-hour line, and every line while WeeklyLine is 0
	WeeklyLine float64 `json:"weeklyLine"` // the 7-day line; 0 for none
	Cooldown   int     `json:"cooldown"`   // seconds
	Margin     float64 `json:"margin"`
	Strategy   string  `json:"strategy"`
}

// DefaultAuto is auto-switch as first set: off.
func DefaultAuto() Auto { return Auto{Line: 90, Cooldown: 300, Margin: 10, Strategy: Best} }

func (s Auto) clamped() Auto {
	s.Line = clamp(s.Line, 50, 99.9)
	if s.WeeklyLine != 0 {
		s.WeeklyLine = clamp(s.WeeklyLine, 50, 99.9)
	}
	s.Cooldown = int(clamp(float64(s.Cooldown), 0, 86400))
	s.Margin = clamp(s.Margin, 0, 50)
	if !slices.Contains([]string{Best, WeeklyFirst, ConsumeFirst, ByPriority}, s.Strategy) {
		s.Strategy = Best
	}
	return s
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

// fresh is how old a reading may be for auto-switch to act on it
// (claude-swap's STALE_OK_S).
const fresh = 5 * time.Minute

// Decision is what auto-switch does: To is the account switched to, "" to
// stay; Why says why it stays or moves.
type Decision struct {
	To, Email, Why string
}

// standing is one account's readings against its lines.
type standing struct {
	over     bool      // a window at or over its line
	lh       float64   // line headroom of the binding window, on the five-hour scale
	room     float64   // points before the nearest line
	raw      float64   // 100 - the fullest window
	resets   time.Time // the binding window's reset
	weekly   time.Time // the 7-day window's reset; zero when unknown
	priority float64
}

// stand judges a's five-hour and 7-day windows against their lines; ok is
// false when a has neither.
func stand(a Account, s Auto) (standing, bool) {
	line := s.Line
	if a.Line != 0 {
		line = a.Line
	}
	st := standing{room: math.Inf(1), raw: 100, priority: float64(a.Priority)}
	bind, bindOver, ok := math.Inf(1), false, false
	for _, l := range a.Limits {
		w := line
		switch l.Kind {
		case "session", "five_hour":
		case "weekly_all", "seven_day":
			if s.WeeklyLine != 0 {
				w = s.WeeklyLine
			}
			st.weekly = l.ResetsAt
		default:
			continue
		}
		ok = true
		over := l.Percent >= w
		lh := lineHeadroom(l.Percent, w, s.Line)
		if over && !bindOver || over == bindOver && lh < bind {
			bind, bindOver, st.resets = lh, over, l.ResetsAt
		}
		st.over = st.over || over
		st.room = math.Min(st.room, w-l.Percent)
		st.raw = math.Min(st.raw, 100-l.Percent)
	}
	st.lh = bind
	return st, ok
}

// lineHeadroom is claude-swap's lines.line_headroom: a window's distance
// from its line on the scale of the global line, so accounts on other
// lines compare.
func lineHeadroom(pct, line, scale float64) float64 {
	switch {
	case line == scale:
		return 100 - pct
	case pct <= line:
		return (100 - scale) + scale*(line-pct)/line
	}
	return (100 - scale) * (100 - pct) / (100 - line)
}

func resetKey(t time.Time) float64 {
	if t.IsZero() {
		return math.Inf(1)
	}
	return float64(t.Unix())
}

// decide is claude-swap's auto-switch tick (autoswitch.py _tick_inner and
// _rank_candidates) over the accounts as last read, without its no-return
// bar, failover and per-model windows. last is the time of the last switch.
func decide(now time.Time, s Auto, accts []Account, last time.Time) Decision {
	i := slices.IndexFunc(accts, func(a Account) bool { return a.InUse })
	if i < 0 || accts[i].Err != "" || now.Sub(accts[i].ReadAt) > fresh {
		return Decision{Why: "Waiting for a fresh reading of the account in use."}
	}
	active := accts[i]
	act, ok := stand(active, s)
	if !ok {
		return Decision{Why: "The account in use reports no limits."}
	}
	priority := s.Strategy == ByPriority
	consume := s.Strategy == ConsumeFirst
	var trigger string
	switch {
	case !act.over && !consume && !priority:
		return Decision{Why: "The account in use is under its lines."}
	case !act.over && priority:
		trigger = "priority"
	case !act.over:
		trigger = "consume"
	case act.raw <= 0:
		trigger = "at-limit"
	default:
		trigger = "proactive"
	}
	if trigger != "at-limit" && now.Sub(last) < time.Duration(s.Cooldown)*time.Second {
		return Decision{Why: "Waiting out the cooldown after the last switch."}
	}

	type cand struct {
		a  Account
		st standing
	}
	var cands []cand
	for _, a := range accts {
		if a.InUse || !a.Added || a.AutoOff || a.OAuthAccount == nil || a.Terminal.Dir != "" || a.Err != "" || now.Sub(a.ReadAt) > fresh {
			continue
		}
		if st, ok := stand(a, s); ok && st.raw > 0 {
			cands = append(cands, cand{a, st})
		}
	}

	type ranked struct {
		key []float64
		a   Account
	}
	var out []ranked
	if priority {
		key := func(st standing) []float64 { return []float64{-st.priority, resetKey(st.weekly)} }
		for _, c := range cands {
			if c.st.over || !act.over && !active.AutoOff && slices.Compare(key(c.st), key(act)) >= 0 {
				continue
			}
			out = append(out, ranked{key(c.st), c.a})
		}
	} else {
		allAbove := len(cands) > 0 && !slices.ContainsFunc(cands, func(c cand) bool { return !c.st.over })
		if trigger != "at-limit" {
			allAbove = allAbove && act.over
		}
		weekly := s.Strategy == WeeklyFirst
		for _, c := range cands {
			st := c.st
			if trigger == "proactive" || trigger == "consume" {
				switch {
				case st.over && !allAbove:
					continue
				case allAbove:
					if st.raw < math.Min(2*act.raw, act.raw+3) {
						continue
					}
				case consume:
					if trigger == "consume" && (st.weekly.IsZero() || act.weekly.IsZero() || !st.weekly.Before(act.weekly) || st.room < s.Margin) {
						continue
					}
				case st.lh-act.lh < s.Margin:
					continue
				}
			}
			mostRoom := []float64{-st.raw, resetKey(st.resets)}
			var key []float64
			switch {
			case allAbove:
				key = mostRoom
			case consume || weekly && trigger == "proactive":
				key = []float64{b2f(trigger == "proactive" && st.room < s.Margin), resetKey(st.weekly), -st.lh}
			default:
				key = []float64{-st.lh}
			}
			if trigger == "at-limit" && !allAbove {
				switch {
				case !consume:
					key = append([]float64{b2f(st.over)}, key...)
				case !st.over && st.room >= s.Margin:
					key = append([]float64{0}, key...)
				default:
					key = append([]float64{1, b2f(st.over)}, mostRoom...)
				}
			}
			out = append(out, ranked{key, c.a})
		}
	}
	slices.SortStableFunc(out, func(x, y ranked) int { return slices.Compare(x.key, y.key) })
	if len(out) == 0 {
		if !act.over {
			return Decision{Why: "No account is ahead of the one in use."}
		}
		return Decision{Why: "No account has room."}
	}
	to := out[0].a
	why := map[string]string{
		"at-limit":  "the account in use reached its limit",
		"proactive": "the account in use passed its line",
		"consume":   "its weekly window resets sooner",
		"priority":  "it is a higher priority",
	}[trigger]
	return Decision{To: to.Key(), Email: to.Email, Why: why}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// AutoSettings returns auto-switch's settings.
func (b *Book) AutoSettings() (Auto, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return b.auto, err
}

// SetAuto saves auto-switch's settings, kept in their ranges.
func (b *Book) SetAuto(s Auto) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	b.auto = s.clamped()
	return b.save()
}

// SetAccountAuto sets how auto-switch treats the added account key: held
// out of rotation, its own five-hour line (0 for the global one), and its
// priority tier.
func (b *Book) SetAccountAuto(key string, off bool, line float64, priority int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	i := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == key })
	if i < 0 {
		return errNotAdded
	}
	if line != 0 {
		line = clamp(line, 50, 99.9)
	}
	a := &b.added[i]
	a.AutoOff, a.Line, a.Priority = off, line, int(clamp(float64(priority), 0, 1000))
	return b.save()
}

// AutoSwitch switches when auto-switch is on and the accounts as last read
// call for it. A switch the user made starts the cooldown too.
func (b *Book) AutoSwitch(ctx context.Context) (Decision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Decision{}, err
	}
	if !b.auto.On || b.Home == nil {
		return Decision{}, nil
	}
	now := b.Now()
	wait := max(time.Duration(b.auto.Cooldown)*time.Second, fresh)
	if now.Sub(b.lastFailed) < wait {
		return Decision{Why: "Waiting after a failed switch."}, nil
	}
	d := decide(now, b.auto, b.blocks(), b.lastSwitch)
	if d.To == "" {
		return d, nil
	}
	if err := b.switchLocked(ctx, d.To); err != nil {
		b.lastFailed = now
		return Decision{Why: fmt.Sprintf("The switch to %s failed: %v", d.Email, err)}, err
	}
	return d, nil
}
