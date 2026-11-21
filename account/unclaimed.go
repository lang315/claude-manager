package account

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"time"
)

// claude-swap's unclaimed stash: a login in use that names no added
// account, such as one that lapsed after its account was renewed
// elsewhere, is kept in a keychain item of its own before a switch
// replaces it, instead of the switch being refused or the login lost.

// UnclaimedService is the keychain item an unclaimed login is kept in,
// under its id.
const UnclaimedService = "claude-manager-unclaimed"

// Unclaimed is one kept login: its id, when it was kept and why.
type Unclaimed struct {
	ID  string    `json:"id"`
	At  time.Time `json:"at"`
	Why string    `json:"why"`
}

var errNoUnclaimed = errors.New("No unclaimed login has that id: claude-manager unclaimed lists them.")

// stashLocked keeps blob's login as an unclaimed one, read back before it
// counts as kept.
func (b *Book) stashLocked(blob []byte, why string) error {
	o := oauthOf(blob)
	if o == nil {
		return errNoToken
	}
	sum := sha256.Sum256(o)
	id := hex.EncodeToString(sum[:6])
	data, err := withOAuth(nil, o)
	if err != nil {
		return err
	}
	if err := b.Vault.Put(UnclaimedService, id, data); err != nil {
		return err
	}
	if got, err := b.Vault.Get(UnclaimedService, id); err != nil || !bytes.Equal(got, data) {
		return errors.Join(err, errors.New("it reads back different"))
	}
	if !slices.ContainsFunc(b.unclaimed, func(u Unclaimed) bool { return u.ID == id }) {
		b.unclaimed = append(b.unclaimed, Unclaimed{ID: id, At: b.Now(), Why: why})
	}
	return b.save()
}

// UnclaimedLogins lists the kept logins.
func (b *Book) UnclaimedLogins() ([]Unclaimed, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return slices.Clone(b.unclaimed), err
}

// PurgeUnclaimed deletes the kept login id. Sign in again with /login and
// Add Account to have its account back.
func (b *Book) PurgeUnclaimed(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	i := slices.IndexFunc(b.unclaimed, func(u Unclaimed) bool { return u.ID == id })
	if i < 0 {
		return errNoUnclaimed
	}
	if err := b.Vault.Delete(UnclaimedService, id); err != nil {
		return err
	}
	b.unclaimed = slices.Delete(b.unclaimed, i, i+1)
	return b.save()
}
