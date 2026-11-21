package account

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"slices"
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
	if who != nil {
		inUse = who.Key()
	} else if b.Home != nil {
		inUse = b.Home.accountKey()
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
		case a.Terminal.Dir != "":
			// The terminal renews this login: read with its token, never renew.
			blob, err := b.profileLogin(a.Terminal.Dir)
			l, perr := parseLogin(blob)
			if err != nil || perr != nil {
				a.Err = errTerminalExpired.Error()
				continue
			}
			token = l.AccessToken
		case a.InUse && who == nil:
			a.Err, held = liveErr.Error(), true
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
			if now.Before(a.WaitUntil) || !force && !a.ReadAt.IsZero() && now.Before(b.due(a, active, near)) {
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
	if p, ok := b.who[k]; ok {
		return p, nil
	}
	p, err := b.API.Profile(ctx, token)
	if err != nil {
		return p, err
	}
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
	if token != "" {
		limits, err = b.API.Usage(ctx, token)
		switch {
		case !errors.Is(err, ErrUnauthorized):
		case a.Terminal.Dir != "":
			err = errTerminalExpired
		default:
			err = errLiveExpired // Claude Code's to renew, never the app's
		}
	} else {
		limits, err = b.readOwn(ctx, a, held, now)
	}
	if err != nil {
		a.Err = err.Error()
		var rl *RateLimited
		if errors.As(err, &rl) {
			a.WaitUntil = now.Add(rl.Wait)
		}
		return
	}
	before, _ := stand(*a, Auto{Line: 100})
	a.Limits, a.ReadAt, a.Err, a.WaitUntil = limits, now, "", time.Time{}
	after, _ := stand(*a, Auto{Line: 100})
	a.climbing = before.raw-after.raw >= 1
}

// nearLine reports whether auto-switch is on and the account in use, as
// last read, is within 15 points of a line or can't be read: then every
// account is watched closely (claude-swap's ESCALATION_MARGIN_PCT).
func (b *Book) nearLine(now time.Time) bool {
	if !b.auto.On {
		return false
	}
	a := b.inUse
	if i := slices.IndexFunc(b.added, func(a Account) bool { return a.InUse }); i >= 0 {
		a = &b.added[i]
	}
	if a == nil {
		return false
	}
	st, ok := stand(*a, b.auto)
	return !ok || !usable(*a, now) || st.room <= 15
}

// due is when a is next read, claude-swap's poll_policy without its
// jitter and movement scaling: the account in use every 3 minutes, every
// minute while it climbs near a line; the others every 5, every 3 near a
// line; a spent one every 10, though a minute after its window resets.
func (b *Book) due(a *Account, active, near bool) time.Time {
	every := 5 * time.Minute
	switch {
	case active && near && a.climbing:
		every = time.Minute
	case active || near:
		every = floor
	}
	due := a.ReadAt.Add(every)
	if st, ok := stand(*a, Auto{Line: 100}); ok && st.raw <= 0 {
		due = a.ReadAt.Add(10 * time.Minute)
		for _, l := range a.Limits {
			if l.Percent >= 100 && !l.ResetsAt.IsZero() && l.ResetsAt.Add(time.Minute).Before(due) {
				due = l.ResetsAt.Add(time.Minute)
			}
		}
	}
	return due
}

// readOwn reads an added account's limits with its own login: renewed
// first when it is about to lapse, and once more when Anthropic refuses it.
func (b *Book) readOwn(ctx context.Context, a *Account, held string, now time.Time) ([]Limit, error) {
	blob, err := b.Vault.Get(ParkService, a.Key())
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidGrant
	}
	if err != nil {
		return nil, err
	}
	l, err := parseLogin(blob)
	if err != nil {
		return nil, ErrInvalidGrant
	}
	if l.expiresBy(now.Add(renewAhead)) {
		if l, err = b.renew(ctx, a, blob, l, held, now); err != nil {
			return nil, err
		}
	}
	limits, err := b.API.Usage(ctx, l.AccessToken)
	if errors.Is(err, ErrUnauthorized) {
		if l, err = b.renew(ctx, a, blob, l, held, now); err != nil {
			return nil, err
		}
		limits, err = b.API.Usage(ctx, l.AccessToken)
	}
	return limits, err
}

// A save that fails loses the login: the old refresh token may be spent.
func (b *Book) renew(ctx context.Context, a *Account, blob []byte, l Login, held string, now time.Time) (Login, error) {
	if held != "" && l.RefreshToken == held { // Claude Code's to renew: two renewers break the chain
		return l, errLiveExpired
	}
	fresh, err := b.API.Renew(ctx, l, now)
	if err != nil {
		return l, err
	}
	if err := b.Vault.Put(ParkService, a.Key(), withTokens(blob, fresh)); err != nil {
		return l, ErrInvalidGrant
	}
	if fresh.RefreshTokenExpiresAt > 0 {
		a.GoodUntil = time.UnixMilli(fresh.RefreshTokenExpiresAt)
	}
	return fresh, nil
}
