package account

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
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
	// Model names the per-model weekly windows that count too, comma
	// separated, matched case-insensitively; "all" for every one, "" for
	// none (claude-swap's --model).
	Model string `json:"model,omitempty"`
	// Interval is the seconds between ticks (autoswitch.intervalSeconds,
	// 15 to 3600); Ticks how many ticks the account in use stays unreadable
	// before failover (autoswitch.unhealthyTicks, 1 to 100). 0 is the
	// default: 60 and 3.
	Interval int `json:"interval,omitempty"`
	Ticks    int `json:"ticks,omitempty"`
	// Probe reads setup-token accounts' limits with 1-token requests
	// (usage.headerProbe): each is a real request on the account.
	Probe bool `json:"headerProbe,omitempty"`
	// Keys lets auto-switch fall back on an API key account when no account
	// with a subscription has room, at the limit or on a failover
	// (autoswitch.includeApiKeyAccounts).
	Keys bool `json:"includeApiKeyAccounts,omitempty"`
	// DryRun decides and says, switching nothing (claude-swap's TUI dry run).
	DryRun bool `json:"dryRun,omitempty"`
}

// Every is the time between auto-switch's ticks.
func (s Auto) Every() time.Duration {
	if s.Interval == 0 {
		return time.Minute
	}
	return time.Duration(s.Interval) * time.Second
}

// failoverAfter is how long the account in use stays unreadable before
// failover: Ticks ticks.
func (s Auto) failoverAfter() time.Duration {
	n := s.Ticks
	if n == 0 {
		n = 3
	}
	return time.Duration(n) * s.Every()
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
	if s.Interval != 0 {
		s.Interval = int(clamp(float64(s.Interval), 15, 3600))
	}
	if s.Ticks != 0 {
		s.Ticks = int(clamp(float64(s.Ticks), 1, 100))
	}
	if !slices.Contains([]string{Best, WeeklyFirst, ConsumeFirst, ByPriority}, s.Strategy) {
		s.Strategy = Best
	}
	return s
}

// counts reports whether model's weekly window is one of s.Model's.
func (s Auto) counts(model string) bool {
	for _, m := range strings.Split(s.Model, ",") {
		m = strings.TrimSpace(m)
		if m != "" && (strings.EqualFold(m, "all") || strings.EqualFold(m, model)) {
			return true
		}
	}
	return false
}

// Unseen is the names in s.Model no account's readings have a window for,
// as claude-swap warns of a likely typo; nil until every added account
// that can be read has been.
func (s Auto) Unseen(accts []Account) []string {
	seen := map[string]bool{}
	for _, a := range accts {
		if a.Added && len(a.Limits) == 0 && !a.APIKey && !a.Token { // a key, or a token unprobed, is never read
			return nil
		}
		for _, l := range a.Limits {
			if l.Kind == "weekly_scoped" {
				seen[strings.ToLower(l.Scope)] = true
			}
		}
	}
	var out []string
	for _, m := range strings.Split(s.Model, ",") {
		m = strings.TrimSpace(m)
		if m != "" && !strings.EqualFold(m, "all") && !seen[strings.ToLower(m)] {
			seen[strings.ToLower(m)] = true // named twice, said once
			out = append(out, m)
		}
	}
	return out
}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Min(math.Max(v, lo), hi)
}

// fresh is how old a reading may be for auto-switch to act on it
// (claude-swap's STALE_OK_S).
const fresh = 5 * time.Minute

// Decision is what auto-switch does: To is the account switched to, "" to
// stay; Why says why it stays or moves.
type Decision struct {
	To, Email, Why string
	Blocked        bool // it should move, and no account has room

	trigger    string    // at-limit, proactive, consume, priority or failover
	from       string    // the account left
	leftLH     float64   // its line headroom then
	leftResets time.Time // its binding window's reset then
}

// memo is what auto-switch remembers between ticks, in memory as
// claude-swap keeps it in its state file: the last switch, for the
// cooldown; failover, when the account in use stayed unreadable; and the
// no-return bar on the account it last left.
type memo struct {
	last       time.Time
	failover   bool
	barTo      string // the account auto-switch landed on
	barFrom    string // the account it left
	leftLH     float64
	leftResets time.Time
	leftWhy    string // the trigger it left on
}

const (
	trustFor       = time.Hour       // a reading that failed since is trusted this long (TRUST_MAX_AGE_S)
	trustThrottled = 2 * time.Hour   // when Anthropic asked to wait (RATE_LIMIT_TRUST_MAX_AGE_S)
	recoverySlack  = 5 * time.Minute // RECOVERY_HYSTERESIS_S
	spentHeadroom  = 3.0             // SPENT_HEADROOM_PCT
)

// Usable reports whether a's limits may be decided on at now.
func (a Account) Usable(now time.Time) bool { return usable(a, now) }

// usable reports whether a's limits may be decided on at now: read in the
// last 5 minutes, or, while reads fail for a passing reason (Anthropic out
// of reach, a proxy or captive portal refusing its certificate, asking to wait, or failing itself), the last good ones for an
// hour, two when it asked to wait. A refused, spent or lapsed login never
// is: a switch doesn't test it.
func usable(a Account, now time.Time) bool {
	if len(a.Limits) == 0 || a.ReadAt.IsZero() {
		return false
	}
	age := now.Sub(a.ReadAt)
	switch {
	case a.Err == "" && now.Before(a.HeldUntil): // another Mac's reading, held
		return true
	case a.Err == "":
		return age <= fresh
	case a.Err == (&RateLimited{}).Error() && !a.WaitUntil.IsZero():
		return age <= trustThrottled
	case a.Err == ErrUnreachable.Error() || a.Err == ErrTLS.Error() || strings.HasPrefix(a.Err, "Anthropic answered 5"):
		return age <= trustFor
	}
	return false
}

// released reports whether the account barred since auto-switch left it,
// standing at st, may be landed on again from act: it recovered (its
// headroom grew by 3 points, its binding reset came 5 minutes nearer, or,
// left on a failover, it is under its lines) and leads by the all-over
// margin. claude-swap's _left_account_recovered and _all_above_margin_met.
func (m memo) released(st, act standing) bool {
	recovered := st.lh >= m.leftLH+spentHeadroom ||
		!m.leftResets.IsZero() && !st.resets.IsZero() && st.resets.Before(m.leftResets.Add(-recoverySlack))
	if m.leftWhy == "failover" {
		recovered = !st.over
	}
	return recovered && st.lh >= math.Min(2*act.lh, act.lh+spentHeadroom)
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
		case "weekly_scoped":
			if !s.counts(l.Scope) {
				continue
			}
			if s.WeeklyLine != 0 {
				w = s.WeeklyLine
			}
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
// _rank_candidates) over the accounts as last read. m.failover moves off an
// account in use that stayed unreadable, ranked as at its limit.
func decide(now time.Time, s Auto, accts []Account, m memo) Decision {
	i := slices.IndexFunc(accts, func(a Account) bool { return a.InUse })
	priority := s.Strategy == ByPriority
	consume := s.Strategy == ConsumeFirst
	var active Account
	var act standing
	var trigger string
	if m.failover && i >= 0 {
		active, trigger = accts[i], "failover"
		act, _ = stand(active, s)
	} else {
		if i < 0 || !usable(accts[i], now) {
			return Decision{Why: "Waiting for a fresh reading of the account in use."}
		}
		active = accts[i]
		var ok bool
		if act, ok = stand(active, s); !ok {
			return Decision{Why: "The account in use reports no limits."}
		}
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
	}
	escape := trigger == "at-limit" || trigger == "failover"
	if !escape && now.Sub(m.last) < time.Duration(s.Cooldown)*time.Second {
		return Decision{Why: "Waiting out the cooldown after the last switch."}
	}

	type cand struct {
		a  Account
		st standing
	}
	var cands []cand
	for _, a := range accts {
		if a.InUse || !a.Added || a.AutoOff || a.OAuthAccount == nil || a.Terminal.Dir != "" || !usable(a, now) {
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
			if c.st.over || trigger != "failover" && !act.over && !active.AutoOff && slices.Compare(key(c.st), key(act)) >= 0 {
				continue
			}
			out = append(out, ranked{key(c.st), c.a})
		}
	} else {
		allAbove := len(cands) > 0 && !slices.ContainsFunc(cands, func(c cand) bool { return !c.st.over })
		if !escape {
			allAbove = allAbove && act.over
		}
		weekly := s.Strategy == WeeklyFirst
		for _, c := range cands {
			st := c.st
			if trigger == "proactive" || trigger == "consume" {
				// The no-return bar holds only while auto-switch's own landing is in use.
				if m.barTo == active.Key() && c.a.Key() == m.barFrom && !m.released(st, act) {
					continue
				}
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
			if escape && !allAbove {
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
	if len(out) == 0 && escape && s.Keys {
		// claude-swap's last resort: a key has no limits, and costs per token.
		for _, a := range accts {
			if a.APIKey && !a.InUse && !a.AutoOff {
				return Decision{To: a.Key(), Email: a.Email, Why: "no account with a subscription has room; it is an API key",
					trigger: trigger, from: active.Key(), leftLH: act.lh, leftResets: act.resets}
			}
		}
	}
	if len(out) == 0 {
		if !act.over && !escape {
			return Decision{Why: "No account is ahead of the one in use."}
		}
		return Decision{Why: "No account has room.", Blocked: true}
	}
	to := out[0].a
	why := map[string]string{
		"at-limit":  "the account in use reached its limit",
		"proactive": "the account in use passed its line",
		"consume":   "its weekly window resets sooner",
		"priority":  "it is a higher priority",
		"failover":  "the account in use could not be read",
	}[trigger]
	return Decision{To: to.Key(), Email: to.Email, Why: why, trigger: trigger, from: active.Key(), leftLH: act.lh, leftResets: act.resets}
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

// freshenAhead is claude-swap's FRESHEN_BUFFER_MS: a target lapsing this
// soon is renewed before Claude Code is put on it.
const freshenAhead = 10 * time.Minute

// freshen renews the saved login of key, auto-switch's target, when it
// lapses within freshenAhead (claude-swap's _freshen_target): a refused
// one is quarantined by renew, and not switched to. A key or a
// setup-token has nothing to renew.
func (b *Book) freshen(ctx context.Context, key string, now time.Time) error {
	i := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == key })
	if i < 0 || b.added[i].APIKey || b.added[i].Token {
		return nil
	}
	blob, err := b.Vault.Get(ParkService, key)
	if err != nil {
		return err
	}
	l, err := parseLogin(blob)
	if err != nil || !l.expiresBy(now.Add(freshenAhead)) {
		return err
	}
	_, err = b.renew(ctx, &b.added[i], blob, l, "", now)
	return err
}

// AutoInEffect is the auto-switch settings this process goes by: an
// Override's, else the saved ones.
func (b *Book) AutoInEffect() (Auto, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return b.settings(), err
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

// Once is AutoSwitch for one tick from the command line (claude-swap's
// auto --once): it runs whether or not auto-switch is on, and with dry it
// only says what it would do.
func (b *Book) Once(ctx context.Context, dry bool) (Decision, error) {
	return b.autoSwitch(ctx, true, dry)
}

// AutoSwitch switches when auto-switch is on and the accounts as last read
// call for it. A switch the user made starts the cooldown too.
func (b *Book) AutoSwitch(ctx context.Context) (Decision, error) {
	return b.autoSwitch(ctx, false, false)
}

func (b *Book) autoSwitch(ctx context.Context, once, dry bool) (Decision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Decision{}, err
	}
	if !b.settings().On && !once || b.Home == nil {
		return Decision{}, nil
	}
	now := b.Now()
	wait := max(time.Duration(b.settings().Cooldown)*time.Second, fresh)
	if now.Sub(b.lastFailed) < wait {
		return Decision{Why: "Waiting after a failed switch."}, nil
	}
	accts := b.blocks()
	m := b.bar
	m.last = b.lastSwitch
	i := slices.IndexFunc(accts, func(a Account) bool { return a.InUse })
	switch {
	case i < 0 || accts[i].AccountUUID == "":
		// Nobody is signed in to Claude Code: nothing to move off.
		b.unreadable = time.Time{}
		return Decision{Why: "Waiting for a fresh reading of the account in use."}, nil
	case usable(accts[i], now):
		b.unreadable = time.Time{}
	case accts[i].APIKey:
		b.unreadable = time.Time{}
		return Decision{Why: errKeyInUse.Error()}, nil
	case accts[i].Err == ErrNotSignedIn.Error():
		b.unreadable = time.Time{}
		return Decision{Why: "Not signed in to Claude Code."}, nil
	case accts[i].Err == LiveExpired:
		// claude-swap's idle hold, without its 30-minute cap: claude-swap
		// renews the login in use itself, so a lapse there is rare; the app
		// never does, so on an idle Mac it is the rule, and Claude Code
		// renews it on the next message.
		b.unreadable = time.Time{}
		return Decision{Why: "Claude Code's sign-in has lapsed; it renews on your next message, so auto-switch holds."}, nil
	default:
		if b.unreadable.IsZero() {
			b.unreadable = now
		}
		if wait := b.settings().failoverAfter() - now.Sub(b.unreadable); wait > 0 {
			return Decision{Why: fmt.Sprintf("The account in use can't be read; moving on in %s if it stays so.", wait.Round(time.Minute))}, nil
		}
		m.failover = true
	}
	d := decide(now, b.settings(), accts, m)
	if b.settings().DryRun && !once && d.To != "" {
		d.Why = "Would switch to " + d.Email + ": " + d.Why + " (dry run)."
		d.To, d.Email = "", ""
		return d, nil
	}
	if d.Blocked && !dry {
		b.event("all-exhausted", "All accounts exhausted", "No account has room; auto-switch waits for a reset.")
	} else if !dry {
		delete(b.told, "all-exhausted") // told again when it happens again
	}
	if d.To == "" || dry {
		return d, nil
	}
	if err := b.freshen(ctx, d.To, now); err != nil {
		b.logf("auto-switch to %s held: its sign-in could not be renewed: %v", d.Email, err)
		return Decision{Why: fmt.Sprintf("%s's sign-in could not be renewed, so no switch: %v", d.Email, err)}, nil
	}
	from := b.inUseEmail()
	if err := b.switchLocked(ctx, d.To, false); err != nil {
		b.lastFailed = now
		b.logf("auto-switch to %s failed: %v", d.Email, err)
		b.event("auto-failed", "Auto-switch failed", fmt.Sprintf("The switch to %s failed: %v", d.Email, err))
		return Decision{Why: fmt.Sprintf("The switch to %s failed: %v", d.Email, err)}, err
	}
	b.record(from, d.Email, d.Why, true)
	b.save()
	b.unreadable = time.Time{}
	b.bar = memo{barTo: d.To, barFrom: d.from, leftLH: d.leftLH, leftResets: d.leftResets, leftWhy: d.trigger}
	return d, nil
}

// settings are the auto-switch settings in force: an Override's for this
// process, else the saved ones.
func (b *Book) settings() Auto {
	if b.override != nil {
		return *b.override
	}
	return b.auto
}
