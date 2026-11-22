package account

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ParkService = "claude-manager-park"
	ParkPrev    = "claude-manager-park-prev"
)

// Account is an account the Limits page shows, with its last reading. It
// holds no secret: the book saves it as is.
type Account struct {
	AccountUUID string `json:"accountUuid,omitempty"`
	OrgUUID     string `json:"orgUuid,omitempty"`
	Email       string `json:"email,omitempty"`
	OrgName     string `json:"orgName,omitempty"`
	Plan        string `json:"plan,omitempty"` // the login's subscriptionType: pro, max, team…
	// The plan as Anthropic's profile last said, every 6 hours
	// (claude-swap's subscription): see PlanLabel.
	PlanTier   string    `json:"planTier,omitempty"`
	PlanOrg    string    `json:"planOrgType,omitempty"`
	PlanStatus string    `json:"planStatus,omitempty"`
	PlanName   string    `json:"planName,omitempty"`
	PlanAt     time.Time `json:"planAt,omitzero"`
	PlanRetry  time.Time `json:"planRetryAt,omitzero"` // when a failed plan read is tried again
	GoodUntil  time.Time `json:"goodUntil,omitzero"`   // when an added account's sign-in lapses
	Limits     []Limit   `json:"limits,omitempty"`     // as last read
	ReadAt     time.Time `json:"readAt,omitzero"`      // when Limits were read
	Err        string    `json:"err,omitempty"`        // why the last ask failed; "" once one works
	WaitUntil  time.Time `json:"waitUntil,omitzero"`   // Anthropic asked to wait until then
	HeldUntil  time.Time `json:"heldUntil,omitzero"`   // read by another Mac: not read here until then
	// OAuthAccount is Claude Code's oauthAccount object from ~/.claude.json.
	OAuthAccount json.RawMessage `json:"oauthAccount,omitempty"`
	AutoOff      bool            `json:"autoOff,omitempty"`      // held out of auto-switch
	Line         float64         `json:"line,omitempty"`         // its own five-hour line; 0 for the global one
	Priority     int             `json:"priority,omitempty"`     // its tier for priority-consume-first
	Terminal     Terminal        `json:"terminal,omitzero"`      // open in a terminal from its own profile
	Token        bool            `json:"token,omitempty"`        // a setup-token account: no refresh token, read by the header probe
	APIKey       bool            `json:"apiKey,omitempty"`       // a Console API key account: no limits, nothing read
	Alias        string          `json:"alias,omitempty"`        // a short name for it on the command line
	Slot         int             `json:"slot,omitempty"`         // its number from 1, as claude-swap's slots
	InUse        bool            `json:"-"`                      // Claude Code is signed in to it
	Added        bool            `json:"-"`                      // the app holds a login of its own for it
	Source       string          `json:"-"`                      // "Pitboard" when Limits are Pitboard's reading
	Failures     int             `json:"failures,omitempty"`     // reads failed in a row; saved, so every process backs off
	Retry        time.Time       `json:"retryAt,omitzero"`       // when a failed read is tried again
	NextPoll     time.Time       `json:"nextPollAt,omitzero"`    // claude-swap's poll plan: when a good read is read again
	PollEvery    time.Duration   `json:"pollInterval,omitempty"` // and the interval it went by
	Last429      time.Time       `json:"last429At,omitzero"`     // when Anthropic last asked it to wait
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
	// Pitboard reads the accounts Pitboard follows; nil reads none.
	Pitboard func() ([]Account, error)
	Home     *Home
	// Launch opens a terminal at folder running claude on configDir:
	// LaunchInTerminal in the app. Running says whether a claude runs on a
	// profile: ClaudeRunning in the app.
	Launch func(folder, configDir string, args ...string) error
	// Events, when set, is told what claude-swap's menu bar notifies:
	// an auto-switch, an account quarantined, every account spent, an
	// auto-switch that failed. Log, when set, gets a line per switch and
	// event (claude-swap's claude-swap.log).
	Events  func(title, body string)
	Log     io.Writer
	Running func(configDir string) (bool, error)

	mu         bookMu
	stamp      stamp      // accounts.json as this book last read or wrote it
	side       sync.Mutex // who, dead, told and Log: Refresh reads accounts in parallel
	loaded     bool
	cleaned    bool // cleanLeftovers ran
	added      []Account
	inUse      *Account             // the account in use, when it is not an added one
	nobody     *Account             // stands in for it when the login in use can't be read
	who        map[[32]byte]Profile // an access token's SHA-256: whose it is
	others     []Account            // Pitboard's accounts, as last read
	auto       Auto
	lastSwitch time.Time
	lastFailed time.Time
	bar        memo              // the no-return bar auto-switch set
	maps       map[string]string // folder → account key
	prefs      Prefs
	history    []Switched
	switched   int               // switches made, to tell one was
	told       map[string]string // the last body told per event kind
	override   *Auto             // auto-switch settings for this process only (Override)
	unclaimed  []Unclaimed
	unreadable time.Time           // since when the account in use can't be read
	dead       map[string][32]byte // an account's refresh token Anthropic refused: its SHA-256
	unsaved    map[string][]byte   // renewed logins the keychain refused, written at the next refresh
	note       atomic.Value        // string: why Pitboard's files could not be read; read without the lock
}

type saved struct {
	Accounts  []Account         `json:"accounts"`
	InUse     *Account          `json:"inUse,omitempty"`
	Auto      *Auto             `json:"auto,omitempty"`
	Maps      map[string]string `json:"maps,omitempty"` // a folder to the account claude-manager run opens there
	Prefs     *Prefs            `json:"prefs,omitempty"`
	Unclaimed []Unclaimed       `json:"unclaimed,omitempty"`   // logins in use kept as nobody's
	History   []Switched        `json:"history,omitempty"`     // the recent switches
	AutoState *autoState        `json:"autoState,omitempty"`   // auto-switch's memory, for every process
	Dead      map[string]string `json:"quarantined,omitempty"` // account key → SHA-256 of its refused refresh token
}

func (b *Book) path() string { return filepath.Join(b.Dir, "accounts.json") }

// bookMu is the book's lock: within the process, and, from load to Unlock,
// across the processes on one accounts.json (the app and the command line),
// so neither saves over what the other saved meanwhile.
type bookMu struct {
	sync.Mutex
	release func()
}

func (m *bookMu) Unlock() {
	if m.release != nil {
		m.release()
		m.release = nil
	}
	m.Mutex.Unlock()
}

// bookWait is how long a call waits for another process's: a refresh
// holds the book across its reads.
var bookWait = 90 * time.Second

var errBookBusy = errors.New("Another Claude Manager is busy with the accounts. Try again in a minute.")

// stamp tells accounts.json apart from the copy a book holds: every save
// is a new file (writeAtomic), so its identity, time and size.
type stamp struct{ fi fs.FileInfo }

func stampOf(path string) stamp {
	fi, _ := os.Stat(path)
	return stamp{fi}
}

func (s stamp) same(t stamp) bool {
	if s.fi == nil || t.fi == nil {
		return s.fi == nil && t.fi == nil
	}
	return os.SameFile(s.fi, t.fi) && s.fi.ModTime().Equal(t.fi.ModTime()) && s.fi.Size() == t.fi.Size()
}

// load takes the book across processes until the caller unlocks it, then
// reads accounts.json: once, and again whenever another process wrote it
// since. Before the first read it removes what an unfinished sign-in left.
// One that can't be read is tried again on the next call, never taken as
// empty: a save would wipe it.
func (b *Book) load() error {
	if b.mu.release == nil {
		if err := os.MkdirAll(b.Dir, 0o700); err != nil {
			return err
		}
		release, err := lockFor(context.Background(), b.path()+".lock", configStale, bookWait)
		if errors.Is(err, errLockBusy) {
			return errBookBusy
		}
		if err != nil {
			return err
		}
		b.mu.release = release
	}
	if b.loaded && stampOf(b.path()).same(b.stamp) {
		return nil
	}
	b.auto, b.prefs = DefaultAuto(), Prefs{}
	if !b.cleaned {
		b.cleaned = true
		b.cleanLeftovers()
		b.readUnsaved()
	}
	st := stampOf(b.path())
	data, err := os.ReadFile(b.path())
	if errors.Is(err, fs.ErrNotExist) {
		b.loaded, b.stamp = true, stamp{}
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
	b.ensureSlots()
	b.maps, b.unclaimed, b.history = s.Maps, s.Unclaimed, s.History
	b.setAutoState(s.AutoState)
	b.dead = nil
	for k, h := range s.Dead {
		var sum [32]byte
		if raw, err := hex.DecodeString(h); err == nil && len(raw) == len(sum) {
			copy(sum[:], raw)
			if b.dead == nil {
				b.dead = map[string][32]byte{}
			}
			b.dead[k] = sum
		}
	}
	if s.Prefs != nil {
		b.prefs = *s.Prefs
	}
	if s.Auto != nil {
		b.auto = s.Auto.clamped()
	}
	b.loaded, b.stamp = true, st
	return nil
}

// save writes accounts.json atomically, private to the user.
func (b *Book) save() error {
	b.ensureSlots()
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(b.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved{Accounts: b.added, InUse: b.inUse, Auto: &b.auto, Maps: b.maps, Prefs: &b.prefs, Unclaimed: b.unclaimed, History: b.history, AutoState: b.savedAutoState(), Dead: b.savedDead()}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(b.path(), data, 0o600); err != nil {
		return err
	}
	b.stamp = stampOf(b.path())
	return nil
}

// Blocks returns the accounts as last read: the one in use first, then the
// added ones by email. The first call reads accounts.json.
func (b *Book) Blocks() ([]Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return b.blocks(), err
}

// readPitboard reads Pitboard's accounts again, a cheap read of two files.
func (b *Book) readPitboard() {
	if b.Pitboard == nil {
		return
	}
	others, err := b.Pitboard()
	note := ""
	if err != nil {
		note = err.Error()
	}
	b.others = others
	b.note.Store(note)
}

// Note says why Pitboard's files could not be read at the last load, or
// "".
func (b *Book) Note() string {
	n, _ := b.note.Load().(string)
	return n
}

// blocks reads Pitboard's accounts again, then merges them into the
// accounts as last read.
func (b *Book) blocks() []Account {
	b.ensureSlots()
	b.readPitboard()
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
	// Pitboard's reading of an account shows where it is newer than ours;
	// an account only Pitboard follows gets a block of its own.
	for _, p := range b.others {
		i := slices.IndexFunc(out, func(a Account) bool { return a.Key() == p.Key() })
		if i < 0 {
			out = append(out, p)
			continue
		}
		if len(p.Limits) > 0 && p.ReadAt.After(out[i].ReadAt) {
			out[i].Limits, out[i].ReadAt, out[i].Source = p.Limits, p.ReadAt, p.Source
		}
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

func (b *Book) keep(blob []byte, p Profile, oauthAccount json.RawMessage) (Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Account{}, err
	}
	a, err := b.keepLocked(blob, p, oauthAccount)
	if err != nil {
		return Account{}, err
	}
	return a, b.save()
}

func (b *Book) keepLocked(blob []byte, p Profile, oauthAccount json.RawMessage) (Account, error) {
	l, err := parseLogin(blob)
	if err != nil {
		return Account{}, err
	}
	if err := b.park(p.Key(), blob); err != nil {
		return Account{}, err
	}
	a := Account{AccountUUID: p.AccountUUID, OrgUUID: p.OrgUUID, Email: p.Email, OrgName: p.OrgName, Plan: l.SubscriptionType, OAuthAccount: oauthAccount}
	if l.RefreshTokenExpiresAt > 0 {
		a.GoodUntil = time.UnixMilli(l.RefreshTokenExpiresAt)
	}
	var was *Account
	if i := slices.IndexFunc(b.added, func(x Account) bool { return x.Key() == a.Key() }); i >= 0 {
		was = &b.added[i]
	} else if b.inUse != nil && b.inUse.Key() == a.Key() {
		was = b.inUse
	}
	if was != nil {
		a.Limits, a.ReadAt = was.Limits, was.ReadAt
		a.AutoOff, a.Line, a.Priority, a.Terminal, a.Token, a.Alias, a.Slot = was.AutoOff, was.Line, was.Priority, was.Terminal, was.Token, was.Alias, was.Slot
		if a.OAuthAccount == nil {
			a.OAuthAccount = was.OAuthAccount
		}
	}
	if i := slices.IndexFunc(b.added, func(x Account) bool { return x.Key() == a.Key() }); i >= 0 {
		b.added[i] = a
	} else {
		b.added = append(b.added, a)
	}
	a.Added = true
	return a, nil
}

func (b *Book) park(key string, blob []byte) error {
	b.dropUnsaved(key) // what is written now is the newer login
	o := oauthOf(blob)
	if o == nil {
		return errNoToken
	}
	data, err := withOAuth(nil, o)
	if err != nil {
		return err
	}
	if old, err := b.Vault.Get(ParkService, key); err == nil && !bytes.Equal(old, data) {
		if err := b.Vault.Put(ParkPrev, key, old); err != nil {
			return err
		}
	}
	return b.Vault.Put(ParkService, key, data)
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
	if slices.ContainsFunc(b.added, func(a Account) bool { return a.Key() == key && a.Terminal.Dir != "" }) {
		return errInTerminal
	}
	if err := b.Vault.Delete(ParkService, key); err != nil {
		return err
	}
	if err := b.Vault.Delete(ParkPrev, key); err != nil {
		return err
	}
	b.dropUnsaved(key) // else the next refresh writes it back
	b.added = slices.DeleteFunc(b.added, func(a Account) bool { return a.Key() == key })
	maps.DeleteFunc(b.maps, func(_, k string) bool { return k == key })
	return b.save()
}

func (b *Book) savedAutoState() *autoState {
	if s := b.autoState(); s != (autoState{}) {
		return &s
	}
	return nil
}

func (b *Book) savedDead() map[string]string {
	var out map[string]string
	for k, sum := range b.dead {
		if out == nil {
			out = map[string]string{}
		}
		out[k] = hex.EncodeToString(sum[:])
	}
	return out
}

// Purge removes what the app keeps, as claude-swap's purge: every saved
// and previous login, the unclaimed logins, the terminal profiles and
// their keychain items, and accounts.json. Claude Code's own login stays.
// It is refused while a claude runs in a profile. It returns how many
// added accounts went.
func (b *Book) Purge() (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return 0, err
	}
	profiles := ProfilesDir(b.Dir)
	dirs, _ := os.ReadDir(profiles)
	if b.Running != nil {
		for _, d := range dirs {
			if running, err := b.Running(filepath.Join(profiles, d.Name())); running || err != nil {
				return 0, errProfileBusy
			}
		}
	}
	var errs []error
	for _, a := range b.added {
		errs = append(errs, b.Vault.Delete(ParkService, a.Key()), b.Vault.Delete(ParkPrev, a.Key()))
	}
	for _, u := range b.unclaimed {
		errs = append(errs, b.Vault.Delete(UnclaimedService, u.ID))
	}
	for _, d := range dirs {
		errs = append(errs, b.Vault.Delete(liveService(filepath.Join(profiles, d.Name())), b.user()))
	}
	if err := errors.Join(errs...); err != nil {
		return 0, err // accounts.json stays, naming what is left
	}
	n := len(b.added)
	err := os.RemoveAll(profiles)
	if rerr := os.Remove(b.unsavedPath()); !errors.Is(rerr, fs.ErrNotExist) {
		err = errors.Join(err, rerr)
	}
	if rerr := os.Remove(b.path()); !errors.Is(rerr, fs.ErrNotExist) {
		err = errors.Join(err, rerr)
	}
	b.added, b.inUse, b.maps, b.unclaimed, b.history, b.dead, b.prefs, b.unsaved = nil, nil, nil, nil, nil, nil, Prefs{}, nil
	b.auto = DefaultAuto()
	b.setAutoState(nil)
	b.loaded = false
	return n, err
}
