package account

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// takeoverState is a Pitboard state.json parking carol's login, labelled
// "side", whose refresh token has fingerprint fp.
func takeoverState(fp string) string {
	return `{"schema":8,"accounts":[
	{"id":"p1","provider":"claude","label":"side","email":"carol@example.com","plan":"Max 5x","account_uuid":"carol","organization_uuid":"org-c",
	 "parked":{"service":"pitboard-park-p1-1","refresh_fingerprint":"` + fp + `","access_expires_at":1,"refresh_expires_at":2,"parked_at":3},
	 "oauth_account":{"accountUuid":"carol","emailAddress":"carol@example.com","organizationUuid":"org-c"}},
	{"id":"p2","provider":"claude","label":"plain","email":"dan@example.com","account_uuid":"dan","organization_uuid":"org-d"}]}`
}

// pitboardHolding gives b a Pitboard that parks carol's login (refresh
// token "c-rt") in v, and a forget that drops the account as Pitboard
// does; forgot gets each label forgotten.
func pitboardHolding(t *testing.T, b *Book, v *Memory) (dir string, forgot *[]string) {
	t.Helper()
	dir = writePitboard(t, takeoverState(pitboardFingerprint("c-rt")), "")
	must(t, v.Put("pitboard-park-p1-1", "me", login("c-at", "c-rt", t0.Add(time.Hour)).Marshal()))
	b.Home = &Home{User: "me"}
	b.PitboardDir = dir
	forgot = &[]string{}
	b.PitboardForget = func(_ context.Context, label string) error {
		*forgot = append(*forgot, label)
		must(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"schema":8,"accounts":[]}`), 0o600))
		return v.Delete("pitboard-park-p1-1", "me")
	}
	return dir, forgot
}

// Taking an account over moves Pitboard's login here as it is, renewing
// nothing, and has Pitboard forget the account so one app renews it.
func TestTakeOverFromPitboard(t *testing.T) {
	f := newAnthropic()
	b, v, _, _ := newBook(t, f)
	_, forgot := pitboardHolding(t, b, v)

	a, err := b.TakeOver(context.Background(), "carol/org-c")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Added || a.Email != "carol@example.com" {
		t.Errorf("took over %+v", a)
	}
	if !strings.Contains(string(a.OAuthAccount), `"emailAddress":"carol@example.com"`) {
		t.Errorf("oauthAccount %s: a switch to it would leave ~/.claude.json naming another", a.OAuthAccount)
	}
	if len(*forgot) != 1 || (*forgot)[0] != "side" {
		t.Errorf("Pitboard asked to forget %q, want [side]", *forgot)
	}
	saved, err := v.Get(ParkService, "carol/org-c")
	if err != nil {
		t.Fatal(err)
	}
	if l, err := parseLogin(saved); err != nil || l.RefreshToken != "c-rt" {
		t.Errorf("saved login %+v, %v", l, err)
	}
	if calls := f.took(); len(calls) > 0 {
		t.Errorf("asked Anthropic %q: a renewal would spend the chain Pitboard still held", calls)
	}
	got, err := b.Blocks()
	if err != nil {
		t.Fatal(err)
	}
	if c := blockOf(t, got, "carol@example.com"); !c.Added {
		t.Errorf("carol after the take-over: %+v", c)
	}
	raw, _ := os.ReadFile(filepath.Join(b.Dir, "accounts.json"))
	if !strings.Contains(string(raw), `"carol"`) {
		t.Errorf("accounts.json = %s", raw)
	}
}

// When Pitboard won't forget the account, both would renew one chain: the
// copy taken is dropped and Pitboard keeps its login.
func TestTakeOverUndoneWhenPitboardKeepsIt(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	pitboardHolding(t, b, v)
	b.PitboardForget = func(context.Context, string) error { return errors.New("carol@example.com is in use") }

	_, err := b.TakeOver(context.Background(), "carol/org-c")
	if err == nil || !strings.Contains(err.Error(), "carol@example.com is in use") {
		t.Fatalf("err %v", err)
	}
	if _, err := v.Get(ParkService, "carol/org-c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the copy taken stayed: %v", err)
	}
	if _, err := v.Get("pitboard-park-p1-1", "me"); err != nil {
		t.Errorf("Pitboard's login: %v", err)
	}
	got, _ := b.Blocks()
	for _, a := range got {
		if a.Added {
			t.Errorf("added after an undone take-over: %+v", a)
		}
	}
}

// A park whose refresh token is not the one Pitboard recorded was renewed
// while it was read: taking it would hold a spent chain.
func TestTakeOverRefusesARenewedPark(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	_, forgot := pitboardHolding(t, b, v)
	must(t, v.Put("pitboard-park-p1-1", "me", login("c-at2", "c-rt2", t0.Add(time.Hour)).Marshal()))

	if _, err := b.TakeOver(context.Background(), "carol/org-c"); !errors.Is(err, errParkMoved) {
		t.Fatalf("err %v, want %v", err, errParkMoved)
	}
	if len(*forgot) > 0 {
		t.Errorf("Pitboard asked to forget %q", *forgot)
	}
	if _, err := v.Get(ParkService, "carol/org-c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("saved: %v", err)
	}
}

// renewsAfterRead is a vault on which Pitboard renews carol's park right
// after it is read: state.json then records another refresh token.
type renewsAfterRead struct {
	*Memory
	dir string
}

func (r renewsAfterRead) Get(service, acct string) ([]byte, error) {
	got, err := r.Memory.Get(service, acct)
	if service == "pitboard-park-p1-1" {
		_ = os.WriteFile(filepath.Join(r.dir, "state.json"), []byte(takeoverState(pitboardFingerprint("c-rt+"))), 0o600)
	}
	return got, err
}

func TestTakeOverRefusesAParkRenewedBeforeTheForget(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	dir, forgot := pitboardHolding(t, b, v)
	b.Vault = renewsAfterRead{v, dir}

	if _, err := b.TakeOver(context.Background(), "carol/org-c"); !errors.Is(err, errParkMoved) {
		t.Fatalf("err %v, want %v", err, errParkMoved)
	}
	if len(*forgot) > 0 {
		t.Errorf("Pitboard asked to forget %q", *forgot)
	}
	if _, err := v.Get(ParkService, "carol/org-c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the spent copy stayed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(b.Dir, "accounts.json"))
	if strings.Contains(string(raw), `"carol"`) {
		t.Errorf("accounts.json = %s", raw)
	}
}

// Only an account whose login Pitboard parks can be taken over.
func TestTakeOverNeedsAParkedLogin(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	_, forgot := pitboardHolding(t, b, v)
	if _, err := b.TakeOver(context.Background(), "dan/org-d"); !errors.Is(err, errNoPark) {
		t.Errorf("dan: %v, want %v", err, errNoPark)
	}
	b.Home = nil
	if _, err := b.TakeOver(context.Background(), "carol/org-c"); !errors.Is(err, ErrCantSwitch) {
		t.Errorf("without Claude Code's default folder: %v", err)
	}
	if len(*forgot) > 0 {
		t.Errorf("Pitboard asked to forget %q", *forgot)
	}
}

// ReadPitboard says which accounts' logins Pitboard holds.
func TestReadPitboardMarksParkedLogins(t *testing.T) {
	got, err := ReadPitboard(writePitboard(t, takeoverState("f"), ""))
	if err != nil {
		t.Fatal(err)
	}
	if c := blockOf(t, got, "carol@example.com"); !c.PitboardLogin {
		t.Error("carol's parked login not marked")
	}
	if d := blockOf(t, got, "dan@example.com"); d.PitboardLogin {
		t.Error("dan marked without a park")
	}
	var probe map[string]any
	data, _ := json.Marshal(Account{PitboardLogin: true})
	_ = json.Unmarshal(data, &probe)
	if len(probe) > 0 {
		t.Errorf("the mark is saved to accounts.json: %s", data)
	}
}

func TestPitboardFingerprint(t *testing.T) {
	// hex of SHA-256's first 8 bytes, as Pitboard's store::fingerprint
	if got := pitboardFingerprint("abc"); got != "ba7816bf8f01cfea" {
		t.Errorf("fingerprint %q", got)
	}
}

// forgetIn runs Pitboard's command as `pitboard forget --yes -- label`, and
// a refusal comes back with what Pitboard said.
func TestForgetInPitboardRunsItsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a sh script stands in for pitboard")
	}
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "pitboard")
	must(t, os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+args+"'\n[ \"$4\" = side ] || { echo \"no account named $4\" >&2; exit 2; }\n"), 0o755))
	if err := forgetIn(context.Background(), bin, "side"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(args); string(got) != "forget\n--yes\n--\nside\n" {
		t.Errorf("args %q", got)
	}
	if err := forgetIn(context.Background(), bin, "-x"); err == nil || err.Error() != "Pitboard: no account named -x" {
		t.Errorf("refusal: %v", err)
	}
}
