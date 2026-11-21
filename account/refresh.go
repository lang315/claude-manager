package account

import (
	"context"
	"crypto/sha256"
	"errors"
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
	for i := range b.added {
		a := &b.added[i]
		a.InUse = inUse != "" && a.Key() == inUse
		token := ""
		switch {
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

	var wg sync.WaitGroup
	for _, j := range jobs {
		a := j.acct
		if now.Before(a.WaitUntil) || !force && !a.ReadAt.IsZero() && now.Sub(a.ReadAt) < floor {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.read(ctx, a, j.token, now)
		}()
	}
	wg.Wait()
	return b.blocks(), b.save()
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
func (b *Book) read(ctx context.Context, a *Account, token string, now time.Time) {
	var limits []Limit
	var err error
	if token != "" {
		limits, err = b.API.Usage(ctx, token)
		if errors.Is(err, ErrUnauthorized) {
			err = errLiveExpired // Claude Code's to renew, never the app's
		}
	} else {
		limits, err = b.readOwn(ctx, a, now)
	}
	if err != nil {
		a.Err = err.Error()
		var rl *RateLimited
		if errors.As(err, &rl) {
			a.WaitUntil = now.Add(rl.Wait)
		}
		return
	}
	a.Limits, a.ReadAt, a.Err, a.WaitUntil = limits, now, "", time.Time{}
}

// readOwn reads an added account's limits with its own login: renewed
// first when it is about to lapse, and once more when Anthropic refuses it.
func (b *Book) readOwn(ctx context.Context, a *Account, now time.Time) ([]Limit, error) {
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
		if l, err = b.renew(ctx, a, blob, l, now); err != nil {
			return nil, err
		}
	}
	limits, err := b.API.Usage(ctx, l.AccessToken)
	if errors.Is(err, ErrUnauthorized) {
		if l, err = b.renew(ctx, a, blob, l, now); err != nil {
			return nil, err
		}
		limits, err = b.API.Usage(ctx, l.AccessToken)
	}
	return limits, err
}

// A save that fails loses the login: the old refresh token may be spent.
func (b *Book) renew(ctx context.Context, a *Account, blob []byte, l Login, now time.Time) (Login, error) {
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
