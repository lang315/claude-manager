package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ParkService names the keychain items that hold added accounts' logins.
const ParkService = "claude-manager-park"

// Account is an account the Limits page shows, with its last reading. It
// holds no secret: the book saves it as is.
type Account struct {
	AccountUUID string    `json:"accountUuid,omitempty"`
	OrgUUID     string    `json:"orgUuid,omitempty"`
	Email       string    `json:"email,omitempty"`
	OrgName     string    `json:"orgName,omitempty"`
	Plan        string    `json:"plan,omitempty"`     // the login's subscriptionType: pro, max, team…
	GoodUntil   time.Time `json:"goodUntil,omitzero"` // when an added account's sign-in lapses
	Limits      []Limit   `json:"limits,omitempty"`   // as last read
	ReadAt      time.Time `json:"readAt,omitzero"`    // when Limits were read
	Err         string    `json:"err,omitempty"`      // why the last ask failed; "" once one works
	WaitUntil   time.Time `json:"waitUntil,omitzero"` // Anthropic asked to wait until then
	InUse       bool      `json:"-"`                  // Claude Code is signed in to it
	Added       bool      `json:"-"`                  // the app holds a login of its own for it
}

// Key names the account by Anthropic's account and organisation.
func (a Account) Key() string { return a.AccountUUID + "/" + a.OrgUUID }

// Book is the accounts the Limits page shows: the one Claude Code is
// signed in to, and those added. One mutex runs its work one at a time, so
// no login is renewed twice at once.
type Book struct {
	Dir   string // the app's folder: accounts.json and sign-in folders
	Vault Vault
	API   *API
	Live  func() (Login, error) // reads the login Claude Code uses
	Now   func() time.Time
	// Claude finds the claude binary; SignInTimeout bounds a sign-in, 10
	// minutes when zero.
	Claude        func() (string, error)
	SignInTimeout time.Duration

	mu     sync.Mutex
	loaded bool
	added  []Account
	inUse  *Account             // the account in use, when it is not an added one
	nobody *Account             // stands in for it when the login in use can't be read
	who    map[[32]byte]Profile // an access token's SHA-256: whose it is
}

type saved struct {
	Accounts []Account `json:"accounts"`
	InUse    *Account  `json:"inUse,omitempty"`
}

func (b *Book) path() string { return filepath.Join(b.Dir, "accounts.json") }

// load reads accounts.json once.
func (b *Book) load() error {
	if b.loaded {
		return nil
	}
	b.loaded = true
	data, err := os.ReadFile(b.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var s saved
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil {
		return fmt.Errorf("accounts.json could not be read: %w", err)
	}
	b.added, b.inUse = s.Accounts, s.InUse
	return nil
}

// save writes accounts.json atomically, private to the user.
func (b *Book) save() error {
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(b.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved{Accounts: b.added, InUse: b.inUse}, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path())
}

// Blocks returns the accounts as last read: the one in use first, then the
// added ones by email. The first call reads accounts.json.
func (b *Book) Blocks() ([]Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return b.blocks(), err
}

func (b *Book) blocks() []Account {
	var out []Account
	switch {
	case b.inUse != nil:
		a := *b.inUse
		a.InUse = true
		out = append(out, a)
	case b.nobody != nil:
		out = append(out, *b.nobody)
	}
	for _, a := range b.added {
		a.Added = true
		out = append(out, a)
	}
	for i := range out {
		out[i].Limits = slices.Clone(out[i].Limits)
	}
	slices.SortStableFunc(out, func(x, y Account) int {
		if x.InUse != y.InUse {
			if x.InUse {
				return -1
			}
			return 1
		}
		return strings.Compare(x.Email, y.Email)
	})
	return out
}

// keep saves l as the login of the account p names, replacing any it had,
// and lists the account. A sign-in again keeps its last numbers.
func (b *Book) keep(l Login, p Profile) (Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Account{}, err
	}
	if err := b.Vault.Put(ParkService, p.Key(), l.Marshal()); err != nil {
		return Account{}, err
	}
	a := Account{AccountUUID: p.AccountUUID, OrgUUID: p.OrgUUID, Email: p.Email, OrgName: p.OrgName, Plan: l.SubscriptionType}
	if l.RefreshTokenExpiresAt > 0 {
		a.GoodUntil = time.UnixMilli(l.RefreshTokenExpiresAt)
	}
	if i := slices.IndexFunc(b.added, func(x Account) bool { return x.Key() == a.Key() }); i >= 0 {
		a.Limits, a.ReadAt = b.added[i].Limits, b.added[i].ReadAt
		b.added[i] = a
	} else {
		b.added = append(b.added, a)
	}
	a.Added = true
	return a, b.save()
}

// Remove deletes an added account's login from the keychain and drops the
// account. The login is not revoked: it stays valid at Anthropic until it
// expires.
func (b *Book) Remove(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	if err := b.Vault.Delete(ParkService, key); err != nil {
		return err
	}
	b.added = slices.DeleteFunc(b.added, func(a Account) bool { return a.Key() == key })
	return b.save()
}
