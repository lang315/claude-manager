package account

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// A setup-token account (claude-swap's add-token): a long-lived token from
// `claude setup-token`, inference only, with no refresh token and no
// account of its own to ask about. It is named by the email given, or a
// placeholder, and keyed by it.

var (
	errNotSetupToken = errors.New("That isn't a setup-token: claude setup-token prints one starting sk-ant-oat.")
	errTokenEmail    = errors.New("That isn't an email.")
	errNoProbe       = errors.New("Anthropic's usage endpoint doesn't answer a setup-token. Turn on Read Setup-Token Usage to read it with 1-token requests.")
)

const tokenUUID = "token:" // an account key's prefix for a setup-token account

// AddToken adds the setup-token token as an account labelled email, or
// setup-token-N@token.local when email is "". Adding one already added
// replaces its token. Nothing is asked of Anthropic.
func (b *Book) AddToken(token, email string) (Account, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "sk-ant-oat") || strings.ContainsAny(token, " \t\r\n\"") {
		return Account{}, errNotSetupToken
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Account{}, err
	}
	email = strings.TrimSpace(email)
	switch {
	case email == "":
		for n := 1; ; n++ {
			email = fmt.Sprintf("setup-token-%d@token.local", n)
			if !slices.ContainsFunc(b.added, func(a Account) bool { return a.AccountUUID == tokenUUID+email }) {
				break
			}
		}
	case !isEmail(email):
		return Account{}, errTokenEmail
	}
	if slices.ContainsFunc(b.added, func(x Account) bool { return x.Email == email && x.APIKey }) {
		return Account{}, fmt.Errorf("%s is already an API key account: give the setup-token a distinct email.", email)
	}
	acct := []byte(fmt.Sprintf(`{"emailAddress":%q,"accountUuid":"","organizationUuid":null,"organizationName":null}`, email))
	blob := []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"scopes":["user:inference"]}}`, token))
	a, err := b.keepLocked(blob, Profile{AccountUUID: tokenUUID + email, Email: email}, acct)
	if err != nil {
		return Account{}, err
	}
	i := slices.IndexFunc(b.added, func(x Account) bool { return x.Key() == a.Key() })
	b.added[i].Token, b.added[i].Err = true, ""
	delete(b.dead, a.Key())
	return b.added[i], b.save()
}

// tokenOwner is the setup-token account whose token l is: such a login has
// no refresh token, so its access token names it.
func (b *Book) tokenOwner(l Login) *Profile {
	if l.RefreshToken != "" || l.AccessToken == "" {
		return nil
	}
	for _, a := range b.added {
		if !a.Token {
			continue
		}
		if saved, err := b.Vault.Get(ParkService, a.Key()); err == nil {
			if s, err := parseLogin(saved); err == nil && s.AccessToken == l.AccessToken {
				return &Profile{AccountUUID: a.AccountUUID, Email: a.Email}
			}
		}
	}
	return nil
}

// readToken reads a setup-token account's limits with the header probe,
// when it is on. Its saved token is the one in use too: Claude Code can't
// renew a setup-token.
func (b *Book) readToken(ctx context.Context, a *Account, now time.Time) ([]Limit, error) {
	if !b.settings().Probe {
		return nil, errNoProbe
	}
	saved, err := b.Vault.Get(ParkService, a.Key())
	if err != nil {
		return nil, ErrInvalidGrant
	}
	l, err := parseLogin(saved)
	if err != nil {
		return nil, ErrInvalidGrant
	}
	return b.API.Probe(ctx, l.AccessToken, now)
}
