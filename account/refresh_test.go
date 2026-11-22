package account

import (
	"context"
	"encoding/json"
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
	if c := f.took(); !slices.Equal(c, []string{"renew bob-rt", "usage bob-2", "profile bob-2"}) { // its plan, every 6 hours
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
	if c := f.took(); !slices.Equal(c, []string{"usage bob-1", "renew bob-rt", "usage bob-2", "profile bob-2"}) {
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
	// Still since: claude-swap's plan grows 3 minutes ×1.5.
	*now = t0.Add(6 * time.Minute)
	b.Refresh(ctx, false)
	if c := f.took(); len(c) != 0 {
		t.Fatalf("read again before 4.5 minutes: %v", c)
	}
	*now = t0.Add(6*time.Minute + 30*time.Second)
	b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"usage live"}) {
		t.Fatalf("4.5 minutes on: %v", c)
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

// claude-swap's subscription: the plan from the profile every 6 hours,
// kept only when it is the account's own.
func TestPlan(t *testing.T) {
	for _, c := range []struct {
		a     Account
		label string
		lapse bool
	}{
		{Account{PlanTier: "default_claude_max_20x"}, "Max 20x", false},
		{Account{PlanTier: "default_claude_max_5x", PlanName: "Max"}, "Max 5x", false},
		{Account{PlanTier: "default_claude_ai", PlanOrg: "claude_pro"}, "Pro", false},
		{Account{PlanTier: "default_claude_ai", PlanStatus: "canceled"}, "Free", true},
		{Account{PlanTier: "default_claude_max_5x", PlanStatus: "canceled"}, "Max 5x", false},
		{Account{PlanTier: "tier_x", PlanName: "Ultra"}, "Ultra", false},
		{Account{PlanTier: "tier_x"}, "tier_x", false},
		{Account{}, "", false},
	} {
		if l := PlanLabel(c.a); l != c.label || PlanLapsed(c.a) != c.lapse {
			t.Errorf("%+v: %q %v", c.a, l, PlanLapsed(c.a))
		}
	}
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 10, 10
	f.plans = map[string]string{"bob-at": "default_claude_max_5x"}
	b.Refresh(ctx, true)
	blocks, _ := b.Blocks()
	if a := blockOf(t, blocks, "bob@example.com"); PlanLabel(a) != "Max 5x" || !a.PlanAt.Equal(t0) {
		t.Fatalf("bob %+v", a)
	}
	f.took()
	b.Now = func() time.Time { return t0.Add(5 * time.Minute) }
	b.Refresh(ctx, true)
	if slices.Contains(f.took(), "profile bob-at") {
		t.Error("plan asked again within 6 hours")
	}
	// An answer for another account is not kept.
	c, _, _, g := switchBook(t)
	g.usage["alice-at"], g.usage["bob-at"] = 10, 10
	g.who["bob-at"] = Profile{AccountUUID: "zed", Email: "bob@example.com", OrgUUID: "org-b"} // bob's organization, not bob
	g.plans = map[string]string{"bob-at": "default_claude_max_5x"}
	c.Refresh(ctx, true)
	blocks, _ = c.Blocks()
	// claude-swap's plan retry is its own: the plan's age stays true.
	if a := blockOf(t, blocks, "bob@example.com"); a.PlanTier != "" || !a.PlanAt.IsZero() || !a.PlanRetry.Equal(t0.Add(planRetry)) {
		t.Errorf("kept another account's plan, or no retry in 15 minutes: %+v", a)
	}
	// Within 6 hours nothing is asked, even with no answer cached.
	a := Account{AccountUUID: "bob", OrgUUID: "org-b", PlanTier: "old", PlanAt: t0.Add(-time.Hour)}
	b.who = nil
	b.readPlan(ctx, &a, "bob-at", t0)
	if a.PlanTier != "old" {
		t.Error("asked within 6 hours")
	}
	a.PlanAt = t0.Add(-7 * time.Hour)
	b.readPlan(ctx, &a, "bob-at", t0)
	if a.PlanTier != "default_claude_max_5x" {
		t.Errorf("not asked after 6 hours: %+v", a)
	}
}

// A renewed login the keychain won't take is kept in memory and written
// at the next refresh: the old refresh token is spent, and posting it
// again would quarantine the account (claude-swap's stashed successor).
func TestRenewedLoginOutlivesFailedSave(t *testing.T) {
	f := newAnthropic()
	b, v, _, _ := newBook(t, f)
	_, err := b.keep(login("bob-at", "bob-rt", t0.Add(time.Minute)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	f.renewed["bob-rt"], f.usage["bob-at2"] = "bob-at2", 10
	v.Fail = errors.New("keychain locked")
	got, _ := b.Refresh(ctx, true)
	if a := blockOf(t, got, "bob@example.com"); a.Err != "" {
		t.Fatalf("bob = %+v", a)
	}
	if err := b.Switch(ctx, bob.Key()); err == nil {
		t.Error("switched to a login only in memory")
	}
	v.Fail = nil
	f.took()
	b.Refresh(ctx, true)
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt+" {
		t.Errorf("saved login %+v", l)
	}
	for _, c := range f.took() {
		if c == "renew bob-rt" {
			t.Error("the spent refresh token was posted again")
		}
	}
}

// claude-swap's _failure_backoff_s for a 429: an hour-scale ask is waited
// out with 15 minutes to spare (blocks re-armed at their deadline), any
// wait is bounded at 75 minutes, and a short ask never undercuts the
// back-off.
func TestRateLimitWait(t *testing.T) {
	for _, c := range []struct {
		asked time.Duration
		n     int
		want  time.Duration
	}{
		{time.Hour, 1, 75 * time.Minute},
		{20 * time.Minute, 1, 35 * time.Minute},
		{3 * time.Hour, 1, 75 * time.Minute},
		{2 * time.Minute, 1, 2 * time.Minute},
		{10 * time.Second, 3, 2 * time.Minute}, // backoff(3) = 2 minutes
	} {
		if got := rateLimitWait(c.asked, c.n); got != c.want {
			t.Errorf("asked %v, failure %d: %v, want %v", c.asked, c.n, got, c.want)
		}
	}
}

// The plan is read even when the usage read fails: an account whose usage
// Anthropic won't give still shows its plan, and a lapsed one says so.
func TestPlanReadWhenUsageFails(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"] = 10
	f.status["bob-at"] = 503
	f.plans = map[string]string{"bob-at": "default_claude_max_5x"}
	b.Refresh(ctx, true)
	blocks, _ := b.Blocks()
	if a := blockOf(t, blocks, "bob@example.com"); PlanLabel(a) != "Max 5x" {
		t.Errorf("no plan with the usage failing: %+v", a)
	}
}

// claude-swap's plan_after_fetch: moving halves the interval, still grows
// it ×1.5 to the ceiling, a rise near the line is urgent, a recent 429
// floors it at 6 minutes, a spent account waits 10.
func TestNextPoll(t *testing.T) {
	lim := func(p float64) []Limit { return []Limit{{Kind: "session", Percent: p}} }
	s := Auto{Line: 90}
	for _, c := range []struct {
		name      string
		prevEvery time.Duration
		prev, new []Limit
		active    bool
		since429  time.Duration
		want      time.Duration
	}{
		{"first read, in use", 0, nil, lim(10), true, 0, 3 * time.Minute},
		{"first read, another", 0, nil, lim(10), false, 0, 5 * time.Minute},
		{"still, grows", 5 * time.Minute, lim(10), lim(10), false, 0, 7*time.Minute + 30*time.Second},
		{"still, capped", 8 * time.Minute, lim(10), lim(10), false, 0, 10 * time.Minute},
		{"moving, halves", 8 * time.Minute, lim(10), lim(20), false, 0, 4 * time.Minute},
		{"rising near the line, in use", 3 * time.Minute, lim(70), lim(80), true, 0, time.Minute},
		{"a recent 429", 3 * time.Minute, lim(70), lim(80), true, 10 * time.Minute, 6 * time.Minute},
		{"spent", 3 * time.Minute, lim(99), lim(100), false, 0, 10 * time.Minute},
		{"spent, in use", 3 * time.Minute, lim(100), lim(100), true, 0, 4*time.Minute + 30*time.Second},
	} {
		last := time.Time{}
		if c.since429 > 0 {
			last = t0.Add(-c.since429)
		}
		if got := nextPoll(c.prevEvery, c.prev, c.new, c.active, s, last, t0, 0.5); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if j := nextPoll(0, nil, lim(10), false, s, time.Time{}, t0, 1); j != 5*time.Minute+30*time.Second {
		t.Errorf("jitter +10%%: %v", j)
	}
}
