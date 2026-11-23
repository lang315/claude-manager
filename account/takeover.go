package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var (
	errNoPark       = errors.New("Pitboard holds no login for this account.")
	errParkMoved    = errors.New("Pitboard renewed this login while it was being taken over. Try again.")
	errTakenAlready = errors.New("This account is added here already.")
	// shortcut: on Linux and Windows Pitboard parks logins in files of its own; read them if asked.
	errTakeOverMac = errors.New("Taking an account over from Pitboard works on macOS only.")
)

// pitboardFingerprint is Pitboard's fingerprint of a refresh token, as its
// store::fingerprint: the hex of the first 8 bytes of its SHA-256.
func pitboardFingerprint(refreshToken string) string {
	sum := sha256.Sum256([]byte(refreshToken))
	return hex.EncodeToString(sum[:8])
}

// pitboardPark is where Pitboard parks key's login, as its state.json says.
type pitboardPark struct {
	label, service, fingerprint string
	who                         Profile
	oauthAccount                json.RawMessage
}

func readPitboardPark(dir, key string) (pitboardPark, error) {
	var st pitboardFile
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		err = json.Unmarshal(data, &st)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return pitboardPark{}, errNoPark
	case err != nil:
		return pitboardPark{}, errPitboard
	}
	for _, p := range st.Accounts {
		if p.Provider != "claude" || p.AccountUUID+"/"+p.OrgUUID != key || p.Parked == nil || p.Parked.Service == "" ||
			keyOf(p.OAuthAccount) != key {
			continue
		}
		label := p.Label
		if label == "" {
			label = p.Email
		}
		return pitboardPark{label: label, service: p.Parked.Service, fingerprint: p.Parked.Fingerprint,
			who: Profile{AccountUUID: p.AccountUUID, OrgUUID: p.OrgUUID, Email: p.Email}, oauthAccount: p.OAuthAccount}, nil
	}
	return pitboardPark{}, errNoPark
}

// TakeOver moves the login Pitboard parks for key here as it is, then has
// Pitboard forget the account: a refresh token is spent once, so one app
// alone may renew it. Nothing is renewed on the way. Were Pitboard to renew
// it meanwhile, or refuse to forget it, the copy taken is dropped.
func (b *Book) TakeOver(ctx context.Context, key string) (Account, error) {
	switch {
	case b.Home == nil:
		return Account{}, ErrCantSwitch
	case b.Home.FileOnly:
		return Account{}, errTakeOverMac
	case b.PitboardDir == "" || b.PitboardForget == nil:
		return Account{}, errNoPark
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Account{}, err
	}
	if slices.ContainsFunc(b.added, func(a Account) bool { return a.Key() == key }) {
		return Account{}, errTakenAlready
	}
	p, err := readPitboardPark(b.PitboardDir, key)
	if err != nil {
		return Account{}, err
	}
	// Pitboard keeps it under Claude Code's keychain account, through
	// /usr/bin/security as the app does: no keychain prompt.
	blob, err := b.Vault.Get(p.service, b.Home.User)
	switch {
	case errors.Is(err, ErrNotFound):
		return Account{}, errNoPark
	case err != nil:
		return Account{}, err
	}
	l, err := parseLogin(blob)
	if err != nil {
		return Account{}, err
	}
	if pitboardFingerprint(l.RefreshToken) != p.fingerprint {
		return Account{}, errParkMoved
	}
	a, err := b.keepLocked(blob, p.who, p.oauthAccount)
	if err != nil {
		return Account{}, err
	}
	undo := func(cause error) (Account, error) { return Account{}, errors.Join(cause, b.drop(key)) }
	if err := b.save(); err != nil {
		return undo(err)
	}
	// shortcut: a renewal Pitboard starts between this check and its forget still spends the copy taken; Pitboard's own lock would close it.
	if q, err := readPitboardPark(b.PitboardDir, key); err != nil || q.fingerprint != p.fingerprint {
		return undo(errParkMoved)
	}
	if err := b.PitboardForget(ctx, p.label); err != nil {
		if _, rerr := readPitboardPark(b.PitboardDir, key); errors.Is(rerr, errNoPark) {
			return a, nil // it failed after Pitboard let go: this is the only copy now
		}
		return undo(fmt.Errorf("Pitboard didn't forget %s, so its login stays with Pitboard: %w", p.who.Email, err))
	}
	return a, nil
}

// ForgetInPitboard has Pitboard forget the account labelled label: its
// parked login goes, the login in use stays.
func ForgetInPitboard(ctx context.Context, label string) error {
	home, _ := os.UserHomeDir()
	bin, err := findTool("pitboard", "Pitboard's command line was not found.",
		"/Applications/Pitboard.app/Contents/Helpers/pitboard", "/opt/homebrew/bin/pitboard", "/usr/local/bin/pitboard",
		filepath.Join(home, ".cargo", "bin", "pitboard"))
	if err != nil {
		return err
	}
	return forgetIn(ctx, bin, label)
}

func forgetIn(ctx context.Context, bin, label string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "forget", "--yes", "--", label).CombinedOutput()
	if err == nil {
		return nil
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New("Pitboard: " + msg)
	}
	return err
}
