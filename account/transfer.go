package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
)

// exportFile is claude-swap's export (transfer.py, FORMAT_VERSION 1): the
// app reads claude-swap's exports and writes its format. It is not
// encrypted.
type exportFile struct {
	Version      int           `json:"version"`
	ExportedAt   string        `json:"exportedAt"`
	ExportedFrom string        `json:"exportedFrom"`
	Encrypted    bool          `json:"encrypted"`
	Accounts     []exportEntry `json:"accounts"`
}

type exportEntry struct {
	Number      int             `json:"number"` // claude-swap's slot; its import requires one from 1
	Email       string          `json:"email"`
	UUID        string          `json:"uuid"`
	OrgUUID     string          `json:"organizationUuid"`
	OrgName     string          `json:"organizationName"`
	Added       string          `json:"added"`
	Kind        string          `json:"kind,omitempty"`
	Credentials json.RawMessage `json:"credentials"` // {"claudeAiOauth": …}; a string for an API key
	Config      json.RawMessage `json:"config"`      // {"oauthAccount": …}
}

var (
	errNothingToExport = errors.New("No added account has a saved login to export.")
	errNotExport       = errors.New("This isn't an accounts file exported by Claude Manager or claude-swap.")
)

// Export writes the added accounts and their logins to path, readable by
// the user only. It returns how many it wrote, and how many it left out:
// those without an oauthAccount or a complete saved login, which Import
// would refuse. The account in use goes with Claude Code's login when that
// is newer than its saved one.
//
// full is claude-swap's --full, a backup for this Mac: the account in use
// goes with Claude Code's whole login (its MCP sign-ins too) and the whole
// of ~/.claude.json. Import still takes only the login and oauthAccount.
func (b *Book) Export(path string, full bool) (n, left int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return 0, 0, err
	}
	var live []byte
	inUse := ""
	if b.Home != nil {
		inUse = b.Home.accountKey()
		if lb, err := b.Home.readLive(b.Vault); err == nil && keyOf(oauthOf(lb.data)) == inUse {
			live = lb.data
		}
	}
	now := b.Now().UTC().Format("2006-01-02T15:04:05Z")
	f := exportFile{Version: 1, ExportedAt: now, ExportedFrom: "macos"}
	for _, a := range b.added {
		blob, err := b.Vault.Get(ParkService, a.Key())
		if err != nil && !errors.Is(err, ErrNotFound) {
			return 0, 0, err
		}
		whole := full && a.Key() == inUse && live != nil
		if a.Key() == inUse && live != nil && (whole || newer(live, blob)) {
			blob = live
		}
		if _, ok := complete(blob); !ok || keyOf(a.OAuthAccount) == "" || a.Terminal.Dir != "" {
			left++
			continue
		}
		creds, err := withOAuth(nil, oauthOf(blob))
		if err != nil {
			return 0, 0, err
		}
		conf, err := json.Marshal(map[string]json.RawMessage{"oauthAccount": a.OAuthAccount})
		if err != nil {
			return 0, 0, err
		}
		if whole {
			mine, err := os.ReadFile(b.Home.ClaudeJSON)
			if err != nil {
				return 0, 0, err
			}
			var m map[string]json.RawMessage
			if json.Unmarshal(mine, &m) != nil || json.Unmarshal(blob, &m) != nil {
				return 0, 0, errors.New("~/.claude.json or Claude Code's login is not a JSON object.")
			}
			creds, conf = bytes.TrimSpace(blob), bytes.TrimSpace(mine)
		}
		f.Accounts = append(f.Accounts, exportEntry{Number: len(f.Accounts) + 1, Email: a.Email, UUID: a.AccountUUID,
			OrgUUID: a.OrgUUID, OrgName: a.OrgName, Added: now, Credentials: creds, Config: conf})
	}
	if len(f.Accounts) == 0 {
		return 0, left, errNothingToExport
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return 0, 0, err
	}
	// writeAtomic keeps a replaced file's mode, and would follow a link's.
	switch fi, err := os.Lstat(path); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return 0, 0, err
	case !fi.Mode().IsRegular():
		return 0, 0, errors.New("Choose a new file name to export to.")
	default:
		if err := os.Chmod(path, 0o600); err != nil {
			return 0, 0, err
		}
	}
	if err := writeAtomic(path, append(data, '\n'), 0o600); err != nil {
		return 0, 0, err
	}
	return len(f.Accounts), left, nil
}

// complete reads blob's login when it has all a switch needs.
func complete(blob []byte) (Login, bool) {
	o := oauthOf(blob)
	l, err := parseLogin(blob)
	return l, o != nil && err == nil && l.RefreshToken != "" && l.ExpiresAt != 0
}

// newer reports whether login blob x lapses after login blob y.
func newer(x, y []byte) bool {
	lx, err := parseLogin(x)
	if err != nil {
		return false
	}
	ly, err := parseLogin(y)
	return err != nil || lx.ExpiresAt > ly.ExpiresAt
}

// Imported counts what Import did: accounts added, accounts whose saved
// login a newer one replaced, and entries left alone (the account in use,
// a login no newer than the saved one, an API key).
type Imported struct{ Added, Updated, Skipped int }

// Import adds the accounts in the export file at path. The whole file is
// checked before anything is written; nothing asks Anthropic, so a login
// is not renewed here.
func (b *Book) Import(path string) (Imported, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Imported{}, err
	}
	var f exportFile
	if json.Unmarshal(data, &f) != nil || f.Version == 0 {
		return Imported{}, errNotExport
	}
	switch {
	case f.Version != 1:
		return Imported{}, fmt.Errorf("This accounts file is version %d; Claude Manager reads version 1.", f.Version)
	case f.Encrypted:
		return Imported{}, errors.New("This accounts file is encrypted. Decrypt it first.")
	case len(f.Accounts) == 0:
		return Imported{}, errors.New("This accounts file holds no accounts.")
	}

	type entry struct {
		p    Profile
		blob []byte
		acct json.RawMessage
	}
	var in []entry
	var got Imported
	for i, e := range f.Accounts {
		bad := func(why string) error {
			return fmt.Errorf("Account %d in this file %s, so nothing was imported.", i+1, why)
		}
		if e.Kind == "api_key" || bytes.HasPrefix(bytes.TrimSpace(e.Credentials), []byte(`"`)) {
			got.Skipped++ // the app holds subscription logins only
			continue
		}
		if e.Email == "" {
			return Imported{}, bad("has no email")
		}
		o := oauthOf(e.Credentials)
		blob, err := withOAuth(nil, o)
		if _, ok := complete(blob); o == nil || err != nil || !ok {
			return Imported{}, bad("has no complete login")
		}
		acct := oauthAccountOf(e.Config)
		k := keyOf(acct)
		if k == "" {
			return Imported{}, bad("has no account details")
		}
		var ids struct {
			AccountUUID string `json:"accountUuid"`
			OrgUUID     string `json:"organizationUuid"`
			OrgName     string `json:"organizationName"`
		}
		_ = json.Unmarshal(acct, &ids)
		// A login filed under the wrong account would be lost to the right one.
		if e.UUID != "" && e.UUID != ids.AccountUUID || e.OrgUUID != "" && e.OrgUUID != ids.OrgUUID {
			return Imported{}, bad("names two accounts")
		}
		if lk := keyOf(o); lk != "" && lk != k {
			return Imported{}, bad("holds another account's login")
		}
		if slices.ContainsFunc(in, func(x entry) bool { return x.p.Key() == k }) {
			return Imported{}, bad("repeats an account")
		}
		name := e.OrgName
		if name == "" {
			name = ids.OrgName
		}
		in = append(in, entry{Profile{AccountUUID: ids.AccountUUID, Email: e.Email, OrgUUID: ids.OrgUUID, OrgName: name}, blob, acct})
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Imported{}, err
	}
	inUse, liveKey := "", ""
	if b.Home != nil {
		inUse = b.Home.accountKey()
		if lb, err := b.Home.readLive(b.Vault); err == nil {
			liveKey = keyOf(oauthOf(lb.data))
		}
	}
	for _, e := range in {
		k := e.p.Key()
		if k == inUse || k == liveKey { // follow keeps Claude Code's login; a file's copy would be a second renewer
			got.Skipped++
			continue
		}
		if slices.ContainsFunc(b.added, func(a Account) bool { return a.Key() == k && a.Terminal.Dir != "" }) {
			got.Skipped++ // the terminal renews it
			continue
		}
		saved, err := b.Vault.Get(ParkService, k)
		switch {
		case errors.Is(err, ErrNotFound):
		case err != nil:
			return got, errors.Join(err, b.save())
		case !newer(e.blob, saved):
			got.Skipped++
			continue
		}
		added := !slices.ContainsFunc(b.added, func(a Account) bool { return a.Key() == k })
		if _, err := b.keepLocked(e.blob, e.p, e.acct); err != nil {
			return got, errors.Join(err, b.save())
		}
		if added {
			got.Added++
		} else {
			got.Updated++
		}
	}
	return got, b.save()
}
