package account

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"
)

var ctx = context.Background()

// aliceInUse is a fake on which alice is signed in to Claude Code with
// token "live", at 62% of her session limit.
func aliceInUse(t *testing.T) (*Book, *anthropic, *time.Time, *Login) {
	f := newAnthropic()
	f.who["live"], f.usage["live"] = alice, 62
	b, _, now, live := newBook(t, f)
	*live = login("live", "live-rt", t0.Add(time.Hour))
	return b, f, now, live
}

func TestRefreshAccountInUse(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	got, err := b.Refresh(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].InUse || got[0].Added || got[0].Email != "alice@example.com" || got[0].Plan != "max" {
		t.Fatalf("got %+v", got)
	}
	if a := got[0]; len(a.Limits) != 1 || a.Limits[0].Percent != 62 || !a.ReadAt.Equal(t0) || a.Err != "" {
		t.Fatalf("reading %+v", a)
	}
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live"}) {
		t.Fatalf("calls %v", c)
	}
}

func TestRefreshAsksWhoseOncePerToken(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	b.Refresh(ctx, true)
	b.Refresh(ctx, true)
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live", "usage live"}) {
		t.Fatalf("calls %v", c)
	}
}

func TestRefreshSendsNothingForALapsedLogin(t *testing.T) {
	b, f, _, live := aliceInUse(t)
	live.ExpiresAt = t0.UnixMilli()
	got, _ := b.Refresh(ctx, true)
	if len(got) != 1 || !got[0].InUse || got[0].Err != LiveExpired || got[0].Email != "" {
		t.Fatalf("got %+v", got)
	}
	if c := f.took(); len(c) != 0 {
		t.Fatalf("calls %v", c)
	}
}

func TestRefreshNeverRenewsTheLoginInUse(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	delete(f.usage, "live") // refused: 401
	f.renewed["live-rt"] = "fresh"
	got, _ := b.Refresh(ctx, true)
	if got[0].Err != LiveExpired {
		t.Fatalf("got %+v", got)
	}
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live"}) {
		t.Fatalf("calls %v: the login in use is Claude Code's to renew", c)
	}
}

func TestRefreshNotSignedIn(t *testing.T) {
	b, _, _, _ := newBook(t, newAnthropic())
	got, err := b.Refresh(ctx, false)
	if err != nil || len(got) != 1 || !got[0].InUse || got[0].Err != ErrNotSignedIn.Error() {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestRefreshKeepsTheInUseNumbersWhenItsLoginLapses(t *testing.T) {
	b, _, now, live := aliceInUse(t)
	b.Refresh(ctx, false)
	*now = t0.Add(10 * time.Minute)
	live.ExpiresAt = now.UnixMilli()
	got, _ := b.Refresh(ctx, false)
	a := blockOf(t, got, "alice@example.com")
	if a.Err != LiveExpired || len(a.Limits) != 1 || a.Limits[0].Percent != 62 {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshRenewsAnAddedLoginFirst(t *testing.T) {
	f := newAnthropic()
	f.usage["bob-2"], f.renewed["bob-rt"] = 30, "bob-2"
	b, v, _, _ := newBook(t, f)
	if _, err := b.keep(login("bob-1", "bob-rt", t0.Add(renewAhead-time.Second)).Marshal(), bob, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"renew bob-rt", "usage bob-2"}) {
		t.Fatalf("calls %v", c)
	}
	blob, _ := v.Get(ParkService, "bob/org-b")
	l, _ := parseLogin(blob)
	if l.AccessToken != "bob-2" || l.RefreshToken != "bob-rt+" || l.ExpiresAt != t0.Add(8*time.Hour).UnixMilli() {
		t.Fatalf("saved %+v", l)
	}
	a := blockOf(t, got, "bob@example.com")
	if len(a.Limits) != 1 || a.Limits[0].Percent != 30 || !a.GoodUntil.Equal(t0.Add(30*24*time.Hour)) || !a.Added {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshLosesALoginItCannotSave(t *testing.T) {
	f := newAnthropic()
	f.usage["bob-2"], f.renewed["bob-rt"] = 30, "bob-2"
	b, v, _, _ := newBook(t, f)
	if _, err := b.keep(login("bob-1", "bob-rt", t0.Add(time.Minute)).Marshal(), bob, nil); err != nil {
		t.Fatal(err)
	}
	v.Fail = errors.New("the keychain is locked")
	got, _ := b.Refresh(ctx, false)
	if a := blockOf(t, got, "bob@example.com"); a.Err != ErrInvalidGrant.Error() || len(a.Limits) != 0 {
		t.Fatalf("got %+v", a)
	}
	if c := f.took(); !slices.Equal(c, []string{"renew bob-rt"}) {
		t.Fatalf("calls %v: an unsaved login must not be used", c)
	}
}

func TestRefreshSignInExpired(t *testing.T) {
	b, _, _, _ := newBook(t, newAnthropic())
	if _, err := b.keep(login("bob-1", "bob-rt", t0).Marshal(), bob, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if a := blockOf(t, got, "bob@example.com"); a.Err != "Sign-in expired." {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshRenewsOnceWhenRefused(t *testing.T) {
	f := newAnthropic()
	f.usage["bob-2"], f.renewed["bob-rt"] = 30, "bob-2" // bob-1 is refused
	b, _, _, _ := newBook(t, f)
	if _, err := b.keep(login("bob-1", "bob-rt", t0.Add(time.Hour)).Marshal(), bob, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"usage bob-1", "renew bob-rt", "usage bob-2"}) {
		t.Fatalf("calls %v", c)
	}
	if a := blockOf(t, got, "bob@example.com"); a.Err != "" || a.Limits[0].Percent != 30 {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshShowsAnAddedAccountInUseOnce(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	f.usage["alice-own"] = 1
	if _, err := b.keep(login("alice-own", "alice-rt", t0.Add(time.Hour)).Marshal(), alice, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if len(got) != 1 || !got[0].InUse || !got[0].Added || got[0].Limits[0].Percent != 62 {
		t.Fatalf("got %+v", got)
	}
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live"}) {
		t.Fatalf("calls %v: the account in use is read with the login in use", c)
	}
}

func TestRefreshFloor(t *testing.T) {
	b, f, now, _ := aliceInUse(t)
	b.Refresh(ctx, false)
	f.took()
	*now = t0.Add(2 * time.Minute)
	b.Refresh(ctx, false)
	if c := f.took(); len(c) != 0 {
		t.Fatalf("read again within 3 minutes: %v", c)
	}
	b.Refresh(ctx, true)
	if c := f.took(); !slices.Equal(c, []string{"usage live"}) {
		t.Fatalf("a forced refresh: %v", c)
	}
	*now = t0.Add(5 * time.Minute)
	b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"usage live"}) {
		t.Fatalf("3 minutes on: %v", c)
	}
}

func TestRefreshWaitsWhenRateLimited(t *testing.T) {
	b, f, now, _ := aliceInUse(t)
	f.status["live"] = http.StatusTooManyRequests // Retry-After: 600
	got, _ := b.Refresh(ctx, false)
	if got[0].Err != "Rate limited by Anthropic." {
		t.Fatalf("got %+v", got)
	}
	f.took()
	*now = t0.Add(5 * time.Minute)
	b.Refresh(ctx, true)
	if c := f.took(); len(c) != 0 {
		t.Fatalf("asked within the wait, forced: %v", c)
	}
	delete(f.status, "live")
	*now = t0.Add(11 * time.Minute)
	got, _ = b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"usage live"}) || got[0].Err != "" {
		t.Fatalf("after the wait: %v, %+v", c, got)
	}
}

func TestRefreshKeepsNumbersOnError(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	b.Refresh(ctx, false)
	f.status["live"] = http.StatusInternalServerError
	got, _ := b.Refresh(ctx, true)
	if a := got[0]; a.Err != "Anthropic answered 500." || len(a.Limits) != 1 || a.Limits[0].Percent != 62 || !a.ReadAt.Equal(t0) {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshOrder(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	for _, p := range []Profile{carol, bob} {
		f.usage[p.AccountUUID] = 1
		if _, err := b.keep(login(p.AccountUUID, "rt", t0.Add(time.Hour)).Marshal(), p, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := b.Refresh(ctx, false)
	var emails []string
	for _, a := range got {
		emails = append(emails, a.Email)
	}
	if !slices.Equal(emails, []string{"alice@example.com", "bob@example.com", "carol@example.com"}) {
		t.Fatalf("order %v", emails)
	}
}

// claude-swap's poll_policy, in the large: the others every 5 minutes,
// every 3 near a line; a spent one every 10, but a minute after its reset;
// the one in use every minute while it climbs near a line.
func TestRefreshPolling(t *testing.T) {
	read := func(b *Book, f *anthropic, at time.Duration) []string {
		b.Now = func() time.Time { return t0.Add(at) }
		f.took()
		b.Refresh(ctx, false)
		return f.took()
	}
	has := func(c []string, s string) bool { return slices.Contains(c, s) }

	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 10, 10
	b.Refresh(ctx, true)
	if c := read(b, f, 4*time.Minute); !has(c, "usage alice-at") || has(c, "usage bob-at") {
		t.Errorf("4 minutes: %v", c)
	}
	if c := read(b, f, 5*time.Minute); !has(c, "usage bob-at") {
		t.Errorf("5 minutes: %v", c)
	}

	b, _, _, f = switchBook(t)
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	f.usage["alice-at"], f.usage["bob-at"] = 70, 10
	b.Refresh(ctx, true)
	f.usage["alice-at"] = 80 // climbing, 10 points under the line
	if c := read(b, f, 3*time.Minute); !has(c, "usage bob-at") {
		t.Errorf("near the line, 3 minutes: %v", c)
	}
	if c := read(b, f, 4*time.Minute); !has(c, "usage alice-at") || has(c, "usage bob-at") {
		t.Errorf("climbing, a minute on: %v", c)
	}

	b, _, _, f = switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 10, 100
	f.resets["bob-at"] = "2026-10-26T12:30:00Z"
	b.Refresh(ctx, true)
	if c := read(b, f, 9*time.Minute); has(c, "usage bob-at") {
		t.Errorf("spent, 9 minutes: %v", c)
	}
	if c := read(b, f, 10*time.Minute); !has(c, "usage bob-at") {
		t.Errorf("spent, 10 minutes: %v", c)
	}
	f.resets["bob-at"] = "2026-10-26T12:15:00Z"
	b.Refresh(ctx, true)
	if c := read(b, f, 15*time.Minute+30*time.Second); has(c, "usage bob-at") {
		t.Errorf("before its reset and a minute: %v", c)
	}
	if c := read(b, f, 16*time.Minute); !has(c, "usage bob-at") {
		t.Errorf("a minute after its reset: %v", c)
	}
}
