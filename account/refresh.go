package account

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// LiveExpired is the status of the account in use when Claude Code's login
// has lapsed: the app never renews it; Claude Code does, as it runs.
const LiveExpired = "Claude Code's sign-in has expired. Open Claude Code to renew it."

const (
	floor      = 3 * time.Minute // an account is read at most this often, unless forced
	renewAhead = 5 * time.Minute // an added login is renewed this long before it lapses
)

var errLiveExpired = errors.New(LiveExpired)

// Refresh asks Anthropic for the limits of the account in use and of each
// added account, and saves what it learns. An account read in the last 3
// minutes is skipped unless force; one Anthropic asked to wait is skipped
// until then, force or not. An account's failure is in its Err; the error
// is the book's own.
func (b *Book) Refresh(ctx context.Context, force bool) ([]Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return b.blocks(), err
	}
	b.saveUnsaved()
	now := b.Now()
	live, who, liveErr := b.identify(ctx, now)
	var followErr error
	if who != nil {
		followErr = b.follow(*who, live)
	}

	type job struct {
		acct  *Account
		token string // the login in use's; "" for an added account's own login
	}
	// With the login in use unknown, ~/.claude.json still says whose it
	// is: that account's saved login shares Claude Code's refresh chain, so
	// it is not read with it, nor renewed.
	inUse, held := "", false
	switch {
	case who != nil:
		inUse = who.Key()
	case b.Home != nil:
		if inUse = b.keyInUse(); inUse == "" {
			inUse = b.Home.accountKey()
		}
	}
	var jobs []job
	var backErr error
	for i := range b.added {
		a := &b.added[i]
		a.InUse = inUse != "" && a.Key() == inUse
		if a.Terminal.Dir != "" && b.Home != nil && b.Running != nil {
			backErr = errors.Join(backErr, b.takeBack(a, now))
		}
		token := ""
		switch {
		case a.APIKey: // no limits to read
			a.Err, held = "", held || a.InUse
			continue
		case a.Terminal.Dir != "" && b.drifted(*a):
			a.Err = errDrifted.Error()
			continue
		case a.Terminal.Dir != "":
			// The terminal renews this login: read with its token, never renew.
			blob, err := b.profileLogin(a.Terminal.Dir)
			l, perr := parseLogin(blob)
			if err != nil || perr != nil {
				a.Err, a.WaitUntil = errTerminalExpired.Error(), time.Time{}
				continue
			}
			token = l.AccessToken
		case a.InUse && who == nil:
			a.Err, a.WaitUntil, held = liveErr.Error(), time.Time{}, true
			continue
		case a.InUse:
			token = live.AccessToken
		}
		jobs = append(jobs, job{a, token})
	}
	b.nobody = nil
	switch {
	case held:
		b.inUse = nil
	case who == nil && b.inUse != nil:
		b.inUse.Err = liveErr.Error()
	case who == nil:
		b.nobody = &Account{InUse: true, Err: liveErr.Error()}
	case slices.ContainsFunc(b.added, func(a Account) bool { return a.InUse }):
		b.inUse = nil
	default:
		if b.inUse == nil || b.inUse.Key() != who.Key() {
			b.inUse = &Account{}
		}
		u := b.inUse
		u.AccountUUID, u.OrgUUID, u.Email, u.OrgName, u.Plan = who.AccountUUID, who.OrgUUID, who.Email, who.OrgName, live.SubscriptionType
		jobs = append(jobs, job{u, live.AccessToken})
	}

	// The account in use first: how near its line it now is sets how
	// closely the others are watched.
	near := b.nearLine(now)
	for _, active := range []bool{true, false} {
		var wg sync.WaitGroup
		for _, j := range jobs {
			a := j.acct
			if (a == b.inUse || a.InUse) != active {
				continue
			}
			if now.Before(a.WaitUntil) || now.Before(a.HeldUntil) || !force && now.Before(b.due(a, active, near)) {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				b.read(ctx, a, j.token, live.RefreshToken, now)
			}()
		}
		wg.Wait()
		near = b.nearLine(now)
	}
	return b.blocks(), errors.Join(followErr, backErr, b.save())
}

// follow keeps the saved login of the account in use in step with Claude
// Code's, adding the account when it isn't yet, as claude-swap's cswap add
// does. Elsewhere than the default folder a saved login is its own chain,
// renewed by the app, so it is left alone.
func (b *Book) follow(who Profile, live Login) error {
	if b.Home == nil {
		return nil
	}
	lb, err := b.Home.readLive(b.Vault)
	if err != nil {
		return err
	}
	l, err := parseLogin(lb.data)
	if err != nil || l.AccessToken != live.AccessToken {
		return nil
	}
	i := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == who.Key() })
	if i < 0 {
		conf, _ := os.ReadFile(b.Home.ClaudeJSON)
		acct := oauthAccountOf(conf)
		if keyOf(acct) != who.Key() {
			return nil
		}
		_, err := b.keepLocked(lb.data, who, acct)
		return err
	}
	saved, err := b.Vault.Get(ParkService, who.Key())
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return err
	default:
		if s, err := parseLogin(saved); err == nil && s.ExpiresAt >= l.ExpiresAt {
			return nil
		}
	}
	if err := b.park(who.Key(), lb.data); err != nil {
		return err
	}
	if l.RefreshTokenExpiresAt > 0 {
		b.added[i].GoodUntil = time.UnixMilli(l.RefreshTokenExpiresAt)
	}
	return nil
}

// identify reads the login in use and asks whose it is, once per token.
func (b *Book) identify(ctx context.Context, now time.Time) (Login, *Profile, error) {
	l, err := b.Live()
	if err != nil {
		return l, nil, err
	}
	if p := b.tokenOwner(l); p != nil {
		return l, p, nil // a setup-token: no account to ask about, and no expiry
	}
	if l.expiresBy(now) {
		return l, nil, errLiveExpired
	}
	p, err := b.profileOf(ctx, l.AccessToken)
	if errors.Is(err, ErrUnauthorized) {
		err = errLiveExpired
	}
	if err != nil {
		return l, nil, err
	}
	return l, &p, nil
}

func (b *Book) profileOf(ctx context.Context, token string) (Profile, error) {
	k := sha256.Sum256([]byte(token))
	b.side.Lock()
	p, ok := b.who[k]
	b.side.Unlock()
	if ok {
		return p, nil
	}
	p, err := b.API.Profile(ctx, token)
	if err != nil {
		return p, err
	}
	b.side.Lock()
	defer b.side.Unlock()
	if b.who == nil {
		b.who = map[[32]byte]Profile{}
	}
	b.who[k] = p
	return p, nil
}

// read asks for one account's limits: with token, the login in use's, or
// else with the account's own login.
func (b *Book) read(ctx context.Context, a *Account, token, held string, now time.Time) {
	var limits []Limit
	var err error
	used := token
	if a.Token {
		limits, err = b.readToken(ctx, a, now)
	} else if token != "" {
		limits, err = b.API.Usage(ctx, token)
		switch {
		case !errors.Is(err, ErrUnauthorized):
		case a.Terminal.Dir != "":
			err = errTerminalExpired
		default:
			err = errLiveExpired // Claude Code's to renew, never the app's
		}
	} else {
		limits, used, err = b.readOwn(ctx, a, held, now)
	}
	if err != nil {
		a.Err, a.WaitUntil = err.Error(), time.Time{}
		var rl *RateLimited
		if errors.As(err, &rl) {
			a.WaitUntil = now.Add(rateLimitWait(rl.Wait, a.Failures+1))
			a.Last429 = now
		}
		a.Failures++
		a.Retry = now.Add(backoff(a.Failures))
		if !errors.Is(err, ErrUnauthorized) && !errors.Is(err, ErrInvalidGrant) && !errors.Is(err, ErrForbidden) && !errors.As(err, &rl) {
			b.readPlan(ctx, a, used, now) // its own schedule, as claude-swap's: a plan shows with usage failing
		}
		return
	}
	a.Failures, a.Retry = 0, time.Time{}
	b.readPlan(ctx, a, used, now)
	a.PollEvery = nextPoll(a.PollEvery, a.Limits, limits, a == b.inUse || a.InUse, b.settings(), a.Last429, now, jitter())
	a.NextPoll = now.Add(a.PollEvery)
	if line := usageLine(a.Email, limits); line != usageLine(a.Email, a.Limits) && line != "" {
		b.logf("%s", line) // claude-swap's per-account usage line, when it changes
	}
	a.Limits, a.ReadAt, a.Err, a.WaitUntil = limits, now, "", time.Time{}
}

// backoff is how long a read that failed n times in a row waits:
// claude-swap's 30 seconds, doubling, at most 10 minutes.
func backoff(n int) time.Duration {
	if n > 5 {
		return 10 * time.Minute
	}
	return min(30*time.Second<<(n-1), 10*time.Minute)
}

// rateLimitWait is how long a read Anthropic answered 429 waits, as
// claude-swap's _failure_backoff_s: an ask longer than the back-off's cap
// gets 15 minutes more (an hour-scale block retried at its deadline is
// re-armed for another hour), no wait passes 75 minutes, and none falls
// under the back-off of the nth failure.
func rateLimitWait(asked time.Duration, n int) time.Duration {
	const margin, bound = 15 * time.Minute, 75 * time.Minute
	w := asked
	if asked > 10*time.Minute {
		w = asked + margin
	}
	return min(max(w, backoff(n)), bound)
}

// nearLine reports whether auto-switch is on and the account in use, as
// last read, is within 15 points of a line or can't be read: then every
// account is watched closely (claude-swap's ESCALATION_MARGIN_PCT).
func (b *Book) nearLine(now time.Time) bool {
	if !b.settings().On {
		return false
	}
	a := b.inUse
	if i := slices.IndexFunc(b.added, func(a Account) bool { return a.InUse }); i >= 0 {
		a = &b.added[i]
	}
	if a == nil {
		return false
	}
	st, ok := stand(*a, b.settings())
	return !ok || !usable(*a, now) || st.room <= 15
}

// due is when a is next read: a failing read at its back-off, a good one
// at claude-swap's poll plan (nextPoll, saved for every process); the
// others every 3 minutes at most while the one in use nears a line; a
// spent one a minute after its window resets, if sooner. claude-swap
// reads a spent account in use every 10 minutes too, trusting its reading
// until then; here the one in use keeps its plan, so its reading stays
// fresh for auto-switch.
func (b *Book) due(a *Account, active, near bool) time.Time {
	switch {
	case a.Err != "":
		// A failing read waits out its backoff, not the cadence of a good one.
		return a.Retry
	case a.ReadAt.IsZero():
		return time.Time{}
	}
	due := a.NextPoll // claude-swap's poll plan (nextPoll)
	if due.IsZero() {
		due = a.ReadAt.Add(5 * time.Minute)
	}
	if near && !active && a.ReadAt.Add(floor).Before(due) {
		due = a.ReadAt.Add(floor) // the others are watched closely while the one in use nears its line
	}
	if st, ok := stand(*a, Auto{Line: 100}); ok && st.raw <= 0 && !active {
		due = a.ReadAt.Add(10 * time.Minute)
		for _, l := range a.Limits {
			if l.Percent >= 100 && l.ResetsAt.After(a.ReadAt) && l.ResetsAt.Add(time.Minute).Before(due) {
				due = l.ResetsAt.Add(time.Minute)
			}
		}
	}
	return due
}

// readOwn reads an added account's limits with its own login: renewed
// first when it is about to lapse, and once more when Anthropic refuses it.
func (b *Book) readOwn(ctx context.Context, a *Account, held string, now time.Time) ([]Limit, string, error) {
	blob, err := b.savedLogin(a.Key())
	if errors.Is(err, ErrNotFound) {
		return nil, "", ErrInvalidGrant
	}
	if err != nil {
		return nil, "", err
	}
	l, err := parseLogin(blob)
	if err != nil {
		return nil, "", ErrInvalidGrant
	}
	// claude-swap's quarantine: a refused refresh token is not posted again
	// until the saved login changes (Sign In Again, Import, a terminal's).
	b.side.Lock()
	dead := b.dead[a.Key()] == sha256.Sum256([]byte(l.RefreshToken))
	b.side.Unlock()
	if dead {
		return nil, "", ErrInvalidGrant
	}
	if l.expiresBy(now.Add(renewAhead)) {
		if l, err = b.renew(ctx, a, blob, l, held, now); err != nil {
			return nil, "", err
		}
	}
	limits, err := b.API.Usage(ctx, l.AccessToken)
	if errors.Is(err, ErrUnauthorized) {
		if l, err = b.renew(ctx, a, blob, l, held, now); err != nil {
			return nil, "", err
		}
		limits, err = b.API.Usage(ctx, l.AccessToken)
	}
	return limits, l.AccessToken, err
}

func (b *Book) renew(ctx context.Context, a *Account, blob []byte, l Login, held string, now time.Time) (Login, error) {
	if held != "" && l.RefreshToken == held { // Claude Code's to renew: two renewers break the chain
		return l, errLiveExpired
	}
	fresh, err := b.API.Renew(ctx, l, now)
	if errors.Is(err, ErrInvalidGrant) {
		b.side.Lock()
		if b.dead == nil {
			b.dead = map[string][32]byte{}
		}
		b.dead[a.Key()] = sha256.Sum256([]byte(l.RefreshToken))
		b.side.Unlock()
		b.logf("%s quarantined: its refresh token was refused", a.Email)
		b.event("quarantined:"+a.Key(), "Account quarantined", a.Email+"'s sign-in was refused: sign in again to it.")
	}
	if err != nil {
		return l, err
	}
	saved := withTokens(blob, fresh)
	if err := b.Vault.Put(ParkService, a.Key(), saved); err != nil {
		// The old refresh token is spent: the new login is kept here and
		// written at the next refresh (claude-swap's stashed successor).
		b.side.Lock()
		if b.unsaved == nil {
			b.unsaved = map[string][]byte{}
		}
		b.unsaved[a.Key()] = saved
		werr := b.writeUnsaved()
		b.side.Unlock()
		if werr != nil {
			b.logf("%s's renewed sign-in couldn't be kept on disk either: %v", a.Email, werr)
		}
		b.logf("%s's renewed sign-in couldn't be saved to the keychain yet: %v", a.Email, err)
	}
	if fresh.RefreshTokenExpiresAt > 0 {
		a.GoodUntil = time.UnixMilli(fresh.RefreshTokenExpiresAt)
	}
	return fresh, nil
}

// RetryAt is when a failed read is tried again; zero when none failed.
func (a Account) RetryAt() time.Time { return a.Retry }

// isDead reports whether saved, key's saved login, holds the refresh token
// Anthropic refused (the quarantine).
func (b *Book) isDead(key string, saved []byte) bool {
	l, err := parseLogin(saved)
	b.side.Lock()
	defer b.side.Unlock()
	h, ok := b.dead[key]
	return err == nil && ok && h == sha256.Sum256([]byte(l.RefreshToken))
}

// savedLogin is key's saved login: the renewed one the keychain refused,
// else the keychain's.
func (b *Book) savedLogin(key string) ([]byte, error) {
	b.side.Lock()
	u, ok := b.unsaved[key]
	b.side.Unlock()
	if ok {
		return u, nil
	}
	return b.Vault.Get(ParkService, key)
}

// dropUnsaved forgets key's unsaved login: a newer one was written, or the
// account went.
func (b *Book) dropUnsaved(key string) {
	b.side.Lock()
	var err error
	if _, ok := b.unsaved[key]; ok {
		delete(b.unsaved, key)
		err = b.writeUnsaved()
	}
	b.side.Unlock()
	if err != nil {
		b.logf("the unsaved sign-ins couldn't be kept on disk: %v", err)
	}
}

// saveUnsaved writes the renewed logins the keychain refused before.
func (b *Book) saveUnsaved() {
	n := len(b.unsaved)
	for k, blob := range b.unsaved {
		if b.Vault.Put(ParkService, k, blob) == nil {
			delete(b.unsaved, k)
		}
	}
	if len(b.unsaved) != n {
		if err := b.writeUnsaved(); err != nil {
			b.logf("the unsaved sign-ins couldn't be kept on disk: %v", err)
		}
	}
}

// unsavedPath holds the unsaved logins across restarts: the old refresh
// token is spent, so losing them loses the sign-in. Like claude-swap's
// stash it is a private file, not the keychain that refused them.
func (b *Book) unsavedPath() string { return filepath.Join(b.Dir, "unsaved.json") }

// writeUnsaved mirrors b.unsaved to its file, removed once empty; the
// caller holds b.side or b.mu alone, so it must not log.
func (b *Book) writeUnsaved() error {
	var err error
	if len(b.unsaved) == 0 {
		if err = os.Remove(b.unsavedPath()); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	} else if err = os.MkdirAll(b.Dir, 0o700); err == nil {
		var data []byte
		if data, err = json.Marshal(b.unsaved); err == nil {
			err = writeAtomic(b.unsavedPath(), data, 0o600)
		}
	}
	return err
}

// readUnsaved takes the unsaved logins from their file, which every
// process writes under the book's lock: what it says is the latest. An
// unreadable file leaves what this process holds.
func (b *Book) readUnsaved() {
	data, err := os.ReadFile(b.unsavedPath())
	if errors.Is(err, fs.ErrNotExist) {
		b.unsaved = nil
		return
	}
	var u map[string][]byte
	if err == nil && json.Unmarshal(data, &u) == nil {
		b.unsaved = u
	}
}

var errUnsaved = errors.New("This account's renewed sign-in isn't in the keychain yet: try again in a minute.")

// jitter spreads polls, as claude-swap's JITTER_FRAC; tests fix it.
var jitter = rand.Float64

// nextPoll is claude-swap's plan_after_fetch: the interval to the next
// read after one that worked, from how the fullest window moved since
// prev. Moving (a point or more) halves it, still grows it ×1.5 to the
// account's ceiling (5 minutes in use, 10 for the others); a window rising
// within 15 points of its line on the account in use polls every minute;
// a 429 in the last hour floors it at 6 minutes, growing ×1.5 to 30; a
// spent account waits 10. r in [0, 1) spreads it ±10%.
func nextPoll(prevEvery time.Duration, prev, cur []Limit, active bool, s Auto, last429, now time.Time, r float64) time.Duration {
	const minEvery = 3 * time.Minute
	def, ceil := 5*time.Minute, 10*time.Minute
	if active {
		def, ceil = minEvery, 5*time.Minute
	}
	base := prevEvery
	if base == 0 {
		base = def
	}
	pp, pok := headroom(Account{Limits: prev}, s)
	cp, cok := headroom(Account{Limits: cur}, s)
	every := def
	switch {
	case !pok || !cok:
	case math.Abs(cp-pp) >= 1:
		every = max(minEvery, base/2)
	default:
		every = min(ceil, max(minEvery, base*3/2))
	}
	recent := !last429.IsZero() && now.Sub(last429) < time.Hour
	if active && !recent {
		for _, l := range cur {
			line := s.Line
			if l.Kind != "session" && l.Kind != "five_hour" && s.WeeklyLine != 0 {
				line = s.WeeklyLine
			}
			for _, p := range prev {
				if p.Kind == l.Kind && p.Scope == l.Scope && l.Percent-p.Percent >= 1 && l.Percent >= line-15 {
					every = time.Minute
				}
			}
		}
	}
	if recent {
		every = min(30*time.Minute, max(every, max(base*3/2, 6*time.Minute)))
	}
	if cok && cp <= 0 && !active { // the one in use stays fresh enough for auto-switch
		every = max(every, 10*time.Minute)
	}
	return time.Duration(float64(every) * (1 + 0.1*(2*r-1)))
}

// usageLine is claude-swap's log line for an account's limits: "usage
// email: 5h 62% (resets 15:00) · 7d 40%"; "" with neither window.
func usageLine(email string, limits []Limit) string {
	var parts []string
	for _, l := range limits {
		var name string
		switch l.Kind {
		case "session", "five_hour":
			name = "5h"
		case "weekly_all", "seven_day":
			name = "7d"
		default:
			continue
		}
		p := fmt.Sprintf("%s %.0f%%", name, l.Percent)
		if !l.ResetsAt.IsZero() {
			p += " (resets " + l.ResetsAt.Local().Format("Jan 2 15:04") + ")"
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return ""
	}
	return "usage " + email + ": " + strings.Join(parts, " · ")
}
