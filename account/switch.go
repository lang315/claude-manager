package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

var (
	// ErrCantSwitch says Claude Code runs from another folder than its
	// default one, where the app doesn't switch.
	ErrCantSwitch = errors.New("Switching works with Claude Code's default folder only.")
	errNotAdded   = errors.New("Add this account here to switch to it.")
	// errNoAccount: an account added before the app kept oauthAccount.
	errNoAccount = errors.New("Sign in again to this account to switch to it.")
	errLiveMoved  = errors.New("Claude Code renewed its sign-in during the switch. Try again.")
	errLiveWhose  = errors.New("Claude Code's sign-in has expired, so the app can't tell whose it is to keep it. Open Claude Code to renew it, then switch.")
	errRestore    = errors.New("The switch failed, and Claude Code's sign-in could not be put back. Run claude and /login.")
)

// CanSwitch says whether the app can switch Claude Code's account.
func (b *Book) CanSwitch() bool { return b.Home != nil }

// Switch puts Claude Code on the added account key names: it saves the
// login in use as its account's, adding that account when it is not
// added, then writes key's login where Claude Code reads it and its
// oauthAccount in ~/.claude.json. Nothing is written until the login in
// use is saved; a failed write puts the earlier login back. Claude Code's
// own locks are held while it reads and writes, so a running Claude Code
// can't renew the old login over the new one.
func (b *Book) Switch(ctx context.Context, key string) error {
	h := b.Home
	if h == nil {
		return ErrCantSwitch
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	ti := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == key })
	if ti < 0 {
		return errNotAdded
	}
	if b.added[ti].OAuthAccount == nil {
		return errNoAccount
	}
	parked, err := b.Vault.Get(ParkService, key)
	if errors.Is(err, ErrNotFound) {
		return ErrInvalidGrant
	}
	if err != nil {
		return err
	}
	oauth := oauthOf(parked)
	if oauth == nil {
		return ErrInvalidGrant
	}

	// Whose the login in use is may need Anthropic: asked before the locks.
	was, err := h.readLive(b.Vault)
	if err != nil {
		return err
	}
	who, err := b.whose(ctx, was.data)
	if err != nil {
		return err
	}
	if who != nil && who.Key() == key {
		return nil
	}

	free, err := h.lockAll(ctx)
	if err != nil {
		return err
	}
	defer free()
	again, err := h.readLive(b.Vault)
	if err != nil {
		return err
	}
	if !bytes.Equal(again.data, was.data) || !bytes.Equal(again.fileData, was.fileData) {
		return errLiveMoved
	}
	conf, err := os.ReadFile(h.ClaudeJSON)
	if err != nil {
		return fmt.Errorf("~/.claude.json could not be read: %w", err)
	}
	newConf, err := spliceKey(conf, "oauthAccount", b.added[ti].OAuthAccount)
	if err != nil {
		return err
	}
	newLive, err := withOAuth(was.data, oauth)
	if err != nil {
		return err
	}

	// The login in use is saved first: once overwritten, it is gone.
	if who != nil {
		mine := oauthAccountOf(conf)
		if keyOf(mine) != who.Key() {
			mine = nil
		}
		_, err := b.keepLocked(was.data, *who, mine)
		if err == nil {
			err = b.save()
		}
		if err == nil {
			saved, gerr := b.Vault.Get(ParkService, who.Key())
			if gerr != nil || !bytes.Equal(oauthOf(saved), oauthOf(was.data)) {
				err = errors.Join(gerr, errors.New("it reads back different"))
			}
		}
		if err != nil {
			return fmt.Errorf("The login in use could not be saved, so nothing was switched: %w", err)
		}
	}
	err = h.writeLive(b.Vault, was, newLive)
	if err == nil {
		err = writeAtomic(h.ClaudeJSON, newConf, 0o600) // atomic: a failure leaves it as it was
	}
	if err != nil {
		if h.restoreLive(b.Vault, was) != nil {
			return errRestore
		}
		return fmt.Errorf("The switch failed; Claude Code still uses its earlier account: %w", err)
	}
	b.inUse = nil
	return b.save()
}

// whose says whose login blob is: Anthropic says while its access token
// is good; after, an added account whose saved login has the same refresh
// token is its owner. nil means no login to keep.
func (b *Book) whose(ctx context.Context, blob []byte) (*Profile, error) {
	l, err := parseLogin(blob)
	if err != nil {
		return nil, nil
	}
	if !l.expiresBy(b.Now()) {
		p, err := b.profileOf(ctx, l.AccessToken)
		if err == nil {
			return &p, nil
		}
		if !errors.Is(err, ErrUnauthorized) {
			return nil, err
		}
	}
	for _, a := range b.added {
		saved, err := b.Vault.Get(ParkService, a.Key())
		if err != nil {
			continue
		}
		if s, err := parseLogin(saved); err == nil && s.RefreshToken == l.RefreshToken {
			return &Profile{AccountUUID: a.AccountUUID, Email: a.Email, OrgUUID: a.OrgUUID, OrgName: a.OrgName}, nil
		}
	}
	return nil, errLiveWhose
}

// keyOf is the account key an oauthAccount object names, or "".
func keyOf(oauthAccount json.RawMessage) string {
	var o struct {
		AccountUUID string `json:"accountUuid"`
		OrgUUID     string `json:"organizationUuid"`
	}
	if json.Unmarshal(oauthAccount, &o) != nil || o.AccountUUID == "" {
		return ""
	}
	return o.AccountUUID + "/" + o.OrgUUID
}
