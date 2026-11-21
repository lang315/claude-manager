package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
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
	Alias       string          `json:"alias,omitempty"`
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
//
// keys, when given, are the only accounts written (cswap export --account).
func (b *Book) Export(path string, full bool, keys ...string) (n, left int, err error) {
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
		if len(keys) > 0 && !slices.Contains(keys, a.Key()) {
			continue
		}
		blob, err := b.Vault.Get(ParkService, a.Key())
		if err != nil && !errors.Is(err, ErrNotFound) {
			return 0, 0, err
		}
		if a.APIKey { // claude-swap's kind api_key: the key as a string
			key := strings.TrimSpace(string(blob))
			if !IsAPIKey(key) {
				left++
				continue
			}
			creds, _ := json.Marshal(key)
			conf, _ := json.Marshal(map[string]json.RawMessage{"oauthAccount": a.OAuthAccount})
			f.Accounts = append(f.Accounts, exportEntry{Number: max(a.Slot, 1), Email: a.Email, Added: now, Kind: "api_key", Credentials: creds, Config: conf, Alias: a.Alias})
			continue
		}
		if a.Token { // claude-swap's: OAuth with the access token alone, accountUuid ""
			if l, err := parseLogin(blob); err != nil || !isSetupToken(l.AccessToken) {
				left++
				continue
			}
			conf, _ := json.Marshal(map[string]json.RawMessage{"oauthAccount": a.OAuthAccount})
			f.Accounts = append(f.Accounts, exportEntry{Number: max(a.Slot, 1), Email: a.Email, Added: now, Credentials: bytes.TrimSpace(blob), Config: conf, Alias: a.Alias})
			continue
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
		f.Accounts = append(f.Accounts, exportEntry{Number: max(a.Slot, 1), Email: a.Email, UUID: a.AccountUUID,
			OrgUUID: a.OrgUUID, OrgName: a.OrgName, Added: now, Credentials: creds, Config: conf, Alias: a.Alias})
	}
	if len(f.Accounts) == 0 {
		return 0, left, errNothingToExport
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return 0, 0, err
	}
	// writeAtomic would write through a link: an export makes a file of its own.
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
func (b *Book) Import(path string) (Imported, error) { return b.ImportWith(path, false) }

// ImportWith is Import; force (cswap import --force) also replaces a saved
// login no older than the file's, and the account in use's saved one,
// which switch --force then puts in use.
func (b *Book) ImportWith(path string, force bool) (Imported, error) {
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
		p     Profile
		blob  []byte
		acct  json.RawMessage
		alias string
		slot  int
	}
	var in []entry
	var keys []entry   // API key accounts: the key in blob
	var tokens []entry // setup-token accounts: the token in blob
	var got Imported
	for i, e := range f.Accounts {
		bad := func(why string) error {
			return fmt.Errorf("Account %d in this file %s, so nothing was imported.", i+1, why)
		}
		if e.Kind == "api_key" || bytes.HasPrefix(bytes.TrimSpace(e.Credentials), []byte(`"`)) {
			var key string
			if json.Unmarshal(e.Credentials, &key) != nil || !IsAPIKey(strings.TrimSpace(key)) {
				return Imported{}, bad("has an API key that isn't one")
			}
			if e.Email == "" {
				return Imported{}, bad("has no email")
			}
			if !isEmail(e.Email) {
				return Imported{}, bad("has an email that isn't one")
			}
			if slices.ContainsFunc(keys, func(x entry) bool { return x.p.Email == e.Email }) {
				return Imported{}, bad("repeats an account")
			}
			keys = append(keys, entry{p: Profile{Email: e.Email}, blob: []byte(strings.TrimSpace(key)), alias: e.Alias, slot: e.Number})
			continue
		}
		if e.Email == "" {
			return Imported{}, bad("has no email")
		}
		if l, err := parseLogin(e.Credentials); err == nil && l.RefreshToken == "" && isSetupToken(l.AccessToken) {
			if !isEmail(e.Email) {
				return Imported{}, bad("has an email that isn't one")
			}
			if slices.ContainsFunc(tokens, func(x entry) bool { return x.p.Email == e.Email }) {
				return Imported{}, bad("repeats an account")
			}
			tokens = append(tokens, entry{p: Profile{Email: e.Email}, blob: []byte(l.AccessToken), alias: e.Alias, slot: e.Number})
			continue
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
		in = append(in, entry{Profile{AccountUUID: ids.AccountUUID, Email: e.Email, OrgUUID: ids.OrgUUID, OrgName: name}, blob, acct, e.Alias, e.Number})
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Imported{}, err
	}
	for _, e := range keys { // as addKeyLocked refuses it, before anything is written
		if slices.ContainsFunc(b.added, func(a Account) bool { return a.Email == e.p.Email && !a.APIKey }) ||
			slices.ContainsFunc(in, func(x entry) bool { return x.p.Email == e.p.Email }) {
			return Imported{}, fmt.Errorf("%s is an account signed in to with OAuth and an API key in this file, so nothing was imported.", e.p.Email)
		}
	}
	for _, e := range tokens { // as addTokenLocked refuses it, before anything is written
		if slices.ContainsFunc(b.added, func(a Account) bool { return a.Email == e.p.Email && a.APIKey }) ||
			slices.ContainsFunc(keys, func(x entry) bool { return x.p.Email == e.p.Email }) {
			return Imported{}, fmt.Errorf("%s is an API key account and a setup-token in this file, so nothing was imported.", e.p.Email)
		}
	}
	inUse, liveKey := "", ""
	if b.Home != nil {
		inUse = b.Home.accountKey()
		if lb, err := b.Home.readLive(b.Vault); err == nil {
			liveKey = keyOf(oauthOf(lb.data))
		}
	}
	keyInUse := b.keyInUse()
	for _, e := range keys {
		k := keyUUID + e.p.Email + "/"
		if !force && k == keyInUse { // keyOwner goes by the saved key: a new one would orphan Claude Code's
			got.Skipped++
			continue
		}
		if saved, err := b.Vault.Get(ParkService, k); err == nil && string(saved) == string(e.blob) && !force {
			got.Skipped++
			continue
		}
		added := !slices.ContainsFunc(b.added, func(a Account) bool { return a.Key() == k })
		if _, err := b.addKeyLocked(string(e.blob), e.p.Email); err != nil {
			return got, errors.Join(err, b.save())
		}
		if added {
			got.Added++
		} else {
			got.Updated++
		}
	}
	for _, e := range in {
		k := e.p.Key()
		if !force && (k == inUse || k == liveKey) { // follow keeps Claude Code's login; a file's copy would be a second renewer
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
		case !force && !newer(e.blob, saved) && !b.isDead(k, saved): // a quarantined login gives way (claude-swap #136)
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
	for _, e := range tokens {
		k := tokenUUID + e.p.Email + "/"
		if saved, err := b.Vault.Get(ParkService, k); err == nil && strings.Contains(string(saved), string(e.blob)) && !force {
			got.Skipped++
			continue
		}
		added := !slices.ContainsFunc(b.added, func(a Account) bool { return a.Key() == k })
		if _, err := b.addTokenLocked(string(e.blob), e.p.Email); err != nil {
			return got, errors.Join(err, b.save())
		}
		if added {
			got.Added++
		} else {
			got.Updated++
		}
	}
	for _, e := range tokens {
		b.takeAlias(tokenUUID+e.p.Email+"/", e.alias)
		b.takeSlot(tokenUUID+e.p.Email+"/", e.slot)
	}
	for _, e := range append(keys, in...) {
		k := e.p.Key()
		if e.p.AccountUUID == "" {
			k = keyUUID + e.p.Email + "/"
		}
		b.takeAlias(k, e.alias)
		b.takeSlot(k, e.slot)
	}
	return got, b.save()
}

// takeAlias gives the added account key an imported alias when it has
// none and no other account has it; a bad or taken one is dropped, as
// claude-swap's import does.
func (b *Book) takeAlias(key, name string) {
	alias, err := normalizeAlias(name)
	if name == "" || err != nil || slices.ContainsFunc(b.added, func(a Account) bool { return a.Alias == alias }) {
		return
	}
	if i := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == key }); i >= 0 && b.added[i].Alias == "" {
		b.added[i].Alias = alias
	}
}

// takeSlot gives a newly imported account its exported slot when that is
// free, as claude-swap's import does; otherwise it keeps the next free one.
func (b *Book) takeSlot(key string, n int) {
	i := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == key })
	if n < 1 || i < 0 || b.added[i].Slot != 0 || b.atSlot(n) >= 0 {
		return
	}
	b.added[i].Slot = n
}
