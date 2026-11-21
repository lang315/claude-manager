package account

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// acct is an account read at t0 with a five-hour and a 7-day window; the
// 7-day one resets weeklyIn from t0.
func acct(email string, fiveH, sevenD float64, weeklyIn time.Duration) Account {
	return Account{AccountUUID: email, OrgUUID: "o", Email: email, Added: true, ReadAt: t0,
		OAuthAccount: json.RawMessage(`{}`),
		Limits: []Limit{
			{Kind: "session", Percent: fiveH, ResetsAt: t0.Add(2 * time.Hour)},
			{Kind: "weekly_all", Percent: sevenD, ResetsAt: t0.Add(weeklyIn)},
		}}
}

func inUse(a Account) Account { a.InUse = true; return a }

const day = 24 * time.Hour

func TestDecide(t *testing.T) {
	def := DefaultAuto()
	with := func(f func(*Auto)) Auto { s := def; f(&s); return s }
	held := acct("held", 0, 0, day)
	held.AutoOff = true
	stale := acct("stale", 0, 0, day)
	stale.ReadAt = t0.Add(-10 * time.Minute)
	noAcct := acct("noacct", 0, 0, day)
	noAcct.OAuthAccount = nil
	inTerm := acct("interm", 0, 0, day)
	inTerm.Terminal.Dir = "/p"
	ownLine := inUse(acct("own", 82, 10, day))
	ownLine.Line = 80
	tier := func(a Account, p int) Account { a.Priority = p; return a }
	line := func(a Account, l float64) Account { a.Line = l; return a }
	model := func(a Account, pct float64) Account {
		a.Limits = append(a.Limits, Limit{Kind: "weekly_scoped", Scope: "Fable", Percent: pct})
		return a
	}
	over := func(a Account) Account {
		a.Limits = append(a.Limits, Limit{Kind: "weekly_scoped", Scope: "Fable", Percent: 100})
		return a
	}

	for _, c := range []struct {
		name  string
		s     Auto
		accts []Account
		last  time.Time
		want  string // the email switched to, or ""
	}{
		{"best stays under its line", def, []Account{inUse(acct("a", 80, 10, day)), acct("b", 0, 0, day)}, time.Time{}, ""},
		{"best leaves at its line for the most room", def, []Account{inUse(acct("a", 90, 10, day)), acct("b", 50, 10, day), acct("c", 20, 10, day)}, time.Time{}, "c"},
		{"hysteresis: not 10 points better", def, []Account{inUse(acct("a", 91, 10, day)), acct("b", 85, 10, day)}, time.Time{}, ""},
		{"a candidate over its line is no landing", def, []Account{inUse(acct("a", 92, 10, day)), acct("b", 10, 95, day), acct("c", 60, 10, day)}, time.Time{}, "c"},
		// docs/auto-switch.md: 7d 93% under a 97 line holds, 5h 95% under an 85 line switches.
		{"weekly line holds 7d 93 under 97", with(func(s *Auto) { s.Line, s.WeeklyLine = 85, 97 }), []Account{inUse(acct("a", 10, 93, day)), acct("b", 0, 0, day)}, time.Time{}, ""},
		{"five-hour 95 under 85 switches", with(func(s *Auto) { s.Line, s.WeeklyLine = 85, 97 }), []Account{inUse(acct("a", 95, 10, day)), acct("b", 0, 0, day)}, time.Time{}, "b"},
		{"per-model windows don't count", def, []Account{inUse(over(acct("a", 10, 10, day))), acct("b", 0, 0, day)}, time.Time{}, ""},
		{"a named model's spent window switches", with(func(s *Auto) { s.Model = "opus, fable" }), []Account{inUse(over(acct("a", 10, 10, day))), acct("b", 0, 0, day)}, time.Time{}, "b"},
		{"all names every model", with(func(s *Auto) { s.Model = "all" }), []Account{inUse(over(acct("a", 10, 10, day))), acct("b", 0, 0, day)}, time.Time{}, "b"},
		{"another model's window doesn't count", with(func(s *Auto) { s.Model = "Opus" }), []Account{inUse(over(acct("a", 10, 10, day))), acct("b", 0, 0, day)}, time.Time{}, ""},
		{"a model window spent is no landing", with(func(s *Auto) { s.Model = "Fable" }), []Account{inUse(acct("a", 95, 10, day)), over(acct("b", 0, 0, day)), acct("c", 30, 10, day)}, time.Time{}, "c"},
		{"a model window takes the weekly line", with(func(s *Auto) { s.Model, s.Line, s.WeeklyLine = "Fable", 85, 97 }), []Account{inUse(model(acct("a", 10, 10, day), 93)), acct("b", 0, 0, day)}, time.Time{}, ""},
		{"own line", def, []Account{ownLine, acct("b", 0, 0, day)}, time.Time{}, "b"},
		{"all over: 2 points more is not enough", def, []Account{inUse(acct("a", 95, 10, day)), acct("b", 93, 10, day)}, time.Time{}, ""},
		{"all over: 3 points more moves", def, []Account{inUse(acct("a", 95, 10, day)), acct("b", 92, 10, day)}, time.Time{}, "b"},
		{"all over: twice the room moves", def, []Account{inUse(acct("a", 98, 10, day)), acct("b", 96, 10, day)}, time.Time{}, "b"},
		{"cooldown holds a proactive move", def, []Account{inUse(acct("a", 92, 10, day)), acct("b", 0, 0, day)}, t0.Add(-time.Minute), ""},
		{"at the limit skips the cooldown", def, []Account{inUse(acct("a", 100, 10, day)), acct("b", 0, 0, day)}, t0.Add(-time.Minute), "b"},
		{"held out, stale, or without oauthAccount: none is a landing", def, []Account{inUse(acct("a", 95, 10, day)), held, stale, noAcct}, time.Time{}, ""},
		{"one open in a terminal is no landing", def, []Account{inUse(acct("a", 95, 10, day)), inTerm}, time.Time{}, ""},
		{"weekly-first takes the sooner weekly reset", with(func(s *Auto) { s.Strategy = WeeklyFirst }), []Account{inUse(acct("a", 92, 10, day)), acct("b", 0, 0, 5*day), acct("c", 40, 40, 2*day)}, time.Time{}, "c"},
		{"best takes the most room there", def, []Account{inUse(acct("a", 92, 10, day)), acct("b", 0, 0, 5*day), acct("c", 40, 40, 2*day)}, time.Time{}, "b"},
		{"consume-first moves early to a sooner reset", with(func(s *Auto) { s.Strategy = ConsumeFirst }), []Account{inUse(acct("a", 10, 10, 5*day)), acct("b", 20, 20, 2*day)}, time.Time{}, "b"},
		{"consume-first needs the margin of room", with(func(s *Auto) { s.Strategy = ConsumeFirst }), []Account{inUse(acct("a", 10, 10, 5*day)), acct("b", 85, 20, 2*day)}, time.Time{}, ""},
		{"consume-first stays when nothing resets sooner", with(func(s *Auto) { s.Strategy = ConsumeFirst }), []Account{inUse(acct("a", 10, 10, 2*day)), acct("b", 0, 0, 5*day)}, time.Time{}, ""},
		{"priority goes up a tier", with(func(s *Auto) { s.Strategy = ByPriority }), []Account{inUse(acct("a", 10, 10, day)), tier(acct("b", 50, 50, 5*day), 2)}, time.Time{}, "b"},
		{"priority stays on equal keys", with(func(s *Auto) { s.Strategy = ByPriority }), []Account{inUse(acct("a", 10, 10, day)), acct("b", 0, 0, day)}, time.Time{}, ""},
		{"priority never lands over a line", with(func(s *Auto) { s.Strategy = ByPriority }), []Account{inUse(acct("a", 100, 10, day)), tier(acct("b", 95, 10, day), 5)}, time.Time{}, ""},
		// Incidents quoted in claude-swap's autoswitch.py.
		{"unequal lines: 7d 93 under 97 beats 5h 95 under 85", with(func(s *Auto) { s.Line, s.WeeklyLine = 85, 97 }), []Account{inUse(acct("a", 95, 0, day)), acct("b", 0, 93, day)}, time.Time{}, "b"},
		{"all over, own lines: 85 on an 80 line beats 96 on a 95 line", def, []Account{inUse(acct("a", 99, 0, day)), line(acct("b", 96, 0, day), 95), line(acct("c", 85, 0, day), 80)}, time.Time{}, "c"},
		{"consume-first at the limit: 44 beats 97.9 under a 98 line", with(func(s *Auto) { s.Strategy, s.WeeklyLine = ConsumeFirst, 98 }), []Account{inUse(acct("a", 100, 0, 5*day)), acct("b", 0, 97.9, day), acct("c", 0, 44, 4*day)}, time.Time{}, "c"},
		{"weekly-first forced move wants the margin of room", with(func(s *Auto) { s.Strategy = WeeklyFirst }), []Account{inUse(acct("a", 92, 0, day)), acct("b", 82, 0, day), acct("c", 50, 0, 5*day)}, time.Time{}, "c"},
		{"a stale reading of the account in use holds", def, []Account{inUse(stale), acct("b", 0, 0, day)}, time.Time{}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := decide(t0, c.s, c.accts, memo{last: c.last})
			if d.Email != c.want || (d.To == "") != (c.want == "") || d.Why == "" {
				t.Errorf("decide = %+v, want %q", d, c.want)
			}
		})
	}
}

func TestAutoSettingsSaved(t *testing.T) {
	b, _, _, _ := newBook(t, newAnthropic())
	s, err := b.AutoSettings()
	if err != nil || s != DefaultAuto() {
		t.Fatalf("first settings = %+v, %v", s, err)
	}
	must(t, b.SetAuto(Auto{On: true, Line: 120, WeeklyLine: 10, Cooldown: 0, Margin: 0, Strategy: "nope"}))
	again := &Book{Dir: b.Dir, Vault: b.Vault, API: b.API, Now: b.Now, Live: b.Live}
	got, _ := again.AutoSettings()
	if got != (Auto{On: true, Line: 99.9, WeeklyLine: 50, Cooldown: 0, Margin: 0, Strategy: Best}) {
		t.Fatalf("saved = %+v", got)
	}
}

func TestAccountAutoSurvivesSwitches(t *testing.T) {
	b, _, _, _ := switchBook(t)
	must(t, b.Switch(ctx, bob.Key()))
	must(t, b.SetAccountAuto(alice.Key(), true, 85, 7))
	must(t, b.Switch(ctx, alice.Key()))
	must(t, b.Switch(ctx, bob.Key()))
	got, _ := b.Blocks()
	a := blockOf(t, got, "alice@example.com")
	if !a.AutoOff || a.Line != 85 || a.Priority != 7 {
		t.Fatalf("alice = %+v", a)
	}
}

func TestAutoSwitch(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 95, 10
	if _, err := b.Refresh(ctx, true); err != nil {
		t.Fatal(err)
	}
	if d, _ := b.AutoSwitch(ctx); d.To != "" {
		t.Fatalf("switched while off: %+v", d)
	}
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	d, err := b.AutoSwitch(ctx)
	if err != nil || d.Email != "bob@example.com" {
		t.Fatalf("AutoSwitch = %+v, %v", d, err)
	}
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "bob-at" {
		t.Fatalf("login in use %q", l.AccessToken)
	}
	f.usage["alice-at"], f.usage["bob-at"] = 10, 95
	b.Refresh(ctx, true)
	if d, _ := b.AutoSwitch(ctx); d.To != "" {
		t.Fatalf("switched in the cooldown: %+v", d)
	}
}

func TestAutoSwitchFailureWaits(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 100, 10
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	v.Fail = errors.New("refused")
	if _, err := b.AutoSwitch(ctx); err == nil {
		t.Fatal("the switch worked with the keychain refusing")
	}
	v.Fail = nil
	if d, _ := b.AutoSwitch(ctx); d.To != "" || d.Why != "Waiting after a failed switch." {
		t.Fatalf("retried at once: %+v", d)
	}
}

// claude-swap's no-return bar, failover and trust in a failing account's
// last good reading.
func TestDecideMemo(t *testing.T) {
	def := DefaultAuto()
	a, b, c := acct("a", 92, 10, day), acct("b", 70, 10, day), acct("c", 79, 10, day)
	failed := func(x Account, err string, age time.Duration) Account { x.Err, x.ReadAt = err, t0.Add(-age); return x }
	throttled := failed(acct("t", 0, 0, day), (&RateLimited{}).Error(), 90*time.Minute)
	throttled.WaitUntil = t0.Add(time.Minute)
	stale429 := failed(acct("s", 0, 0, day), ErrInvalidGrant.Error(), time.Minute)
	stale429.WaitUntil = t0.Add(-time.Minute)
	bar := memo{barTo: inUse(a).Key(), barFrom: b.Key(), leftLH: 29}
	for _, x := range []struct {
		name  string
		accts []Account
		m     memo
		want  string
	}{
		{"the account left is barred until it recovers", []Account{inUse(a), b, c}, bar, "c"},
		{"recovered by 3 points, it lands again", []Account{inUse(a), acct("b", 69, 10, day), c}, memo{barTo: bar.barTo, barFrom: bar.barFrom, leftLH: 28}, "b"},
		{"recovered when its reset came nearer", []Account{inUse(a), b, c}, memo{barTo: bar.barTo, barFrom: bar.barFrom, leftLH: 40, leftResets: t0.Add(3 * time.Hour)}, "b"},
		{"left on a failover: under its lines is recovered", []Account{inUse(a), b, c}, memo{barTo: bar.barTo, barFrom: bar.barFrom, leftLH: 40, leftWhy: "failover"}, "b"},
		{"a manual switch lifts the bar", []Account{inUse(a), b, c}, memo{barTo: "other", barFrom: bar.barFrom, leftLH: 40}, "b"},
		{"at the limit the bar doesn't apply", []Account{inUse(acct("a", 100, 10, day)), b, c}, bar, "b"},
		{"failover moves off an unreadable account", []Account{inUse(failed(acct("a", 10, 10, day), "x", 3*time.Hour)), b, c}, memo{failover: true}, "b"},
		{"failover skips the cooldown", []Account{inUse(failed(acct("a", 10, 10, day), "x", 3*time.Hour)), b}, memo{failover: true, last: t0}, "b"},
		{"failover by priority needs no higher tier", []Account{inUse(failed(acct("a", 10, 10, day), "x", 3*time.Hour)), b}, memo{failover: true}, "b|priority"},
		{"failover lands nowhere spent", []Account{inUse(failed(acct("a", 10, 10, day), "x", 3*time.Hour)), acct("s", 100, 10, day)}, memo{failover: true}, ""},
		{"a failing in-use account is trusted an hour", []Account{inUse(failed(acct("a", 92, 10, day), ErrUnreachable.Error(), 50*time.Minute)), acct("f", 0, 0, day)}, memo{}, "f"},
		{"but not past it", []Account{inUse(failed(acct("a", 92, 10, day), ErrUnreachable.Error(), 70*time.Minute)), acct("f", 0, 0, day)}, memo{}, ""},
		{"a refused certificate is out of reach too", []Account{inUse(failed(acct("a", 92, 10, day), ErrTLS.Error(), 50*time.Minute)), acct("f", 0, 0, day)}, memo{}, "f"},
		{"a failing candidate is trusted an hour", []Account{inUse(a), failed(acct("f", 0, 0, day), "Anthropic answered 503.", 50*time.Minute)}, memo{}, "f"},
		{"a refused login is never trusted", []Account{inUse(a), failed(acct("f", 0, 0, day), ErrUnauthorized.Error(), time.Minute)}, memo{}, ""},
		{"nor another answer", []Account{inUse(a), failed(acct("f", 0, 0, day), "Anthropic answered 400.", time.Minute)}, memo{}, ""},
		{"two when Anthropic asked to wait", []Account{inUse(a), throttled}, memo{}, "t"},
		{"a spent login stays untrusted after a rate limit", []Account{inUse(a), stale429}, memo{}, ""},
		{"a spent login is never trusted", []Account{inUse(a), failed(acct("f", 0, 0, day), ErrInvalidGrant.Error(), time.Minute)}, memo{}, ""},
	} {
		s, want := def, x.want
		if w, ok := strings.CutSuffix(want, "|priority"); ok {
			s.Strategy, want = ByPriority, w
		}
		d := decide(t0, s, x.accts, x.m)
		if d.To != want && !(want != "" && d.Email == want) {
			t.Errorf("%s: %+v, want %q", x.name, d, want)
		}
	}
}

// Claude Code's lapsed login holds auto-switch as long as it lasts: the
// app never renews it, and Claude Code does on the next message.
func TestAutoSwitchHoldsOnLapsedLogin(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 10
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	f.renewed["bob-rt"] = "bob-at"
	delete(f.usage, "alice-at")
	for m := 120; m < 300; m += 10 {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		b.Refresh(ctx, true)
		if d, _ := b.AutoSwitch(ctx); d.To != "" || !strings.Contains(d.Why, "holds") {
			t.Fatalf("minute %d: %+v", m, d)
		}
	}
}

// Any other unreadable account in use is left after 3 minutes.
func TestAutoSwitchFailover(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 10
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	must(t, v.Put(liveDefault, "me", stored(t, login("alice-at", "alice-rt", t0.Add(5*time.Hour)))))
	f.status["alice-at"] = 500
	f.renewed["bob-rt"] = "bob-at"
	for m := 120; m <= 123; m++ {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		b.Refresh(ctx, true)
		for range 3 { // a Refresh by hand is no tick: three at once don't fail over
			d, err := b.AutoSwitch(ctx)
			if m < 123 && d.To != "" {
				t.Fatalf("minute %d: left early: %+v", m, d)
			}
			if m == 123 {
				if err != nil || d.Email != "bob@example.com" {
					t.Fatalf("no failover: %+v %v", d, err)
				}
				break
			}
		}
	}
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "bob-at" {
		t.Errorf("login in use %q", l.AccessToken)
	}
}

// Signed out of Claude Code, auto-switch never signs it in: neither with
// nobody in use, nor with ~/.claude.json still naming the account followed.
func TestAutoSwitchNeverSignsIn(t *testing.T) {
	for _, followed := range []bool{false, true} {
		b, v, _, f := switchBook(t)
		f.usage["alice-at"], f.usage["bob-at"] = 50, 10
		if followed {
			b.Refresh(ctx, true) // alice is added and in use
		}
		neverSignsIn(t, b, v)
	}
}

func neverSignsIn(t *testing.T, b *Book, v *Memory) {
	t.Helper()
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	must(t, v.Delete(liveDefault, "me"))
	for m := 0; m < 10; m++ {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		b.Refresh(ctx, true)
		if d, _ := b.AutoSwitch(ctx); d.To != "" {
			t.Fatalf("minute %d: %+v", m, d)
		}
	}
	if _, err := v.Get(liveDefault, "me"); err == nil {
		t.Error("signed Claude Code in")
	}
}

// A spent account in use stays fresh: auto-switch never takes it for
// unreadable and fails over.
func TestAutoSwitchSpentInUseStaysFresh(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 100, 100
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	for m := 1; m <= 12; m++ {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		b.Refresh(ctx, false)
		if d, _ := b.AutoSwitch(ctx); d.To != "" || strings.Contains(d.Why, "can't be read") {
			t.Fatalf("minute %d: %+v", m, d)
		}
	}
}

// A move auto-switch makes bars a return to the account it left.
func TestAutoSwitchSetsTheBar(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 95, 10
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	if d, err := b.AutoSwitch(ctx); err != nil || d.To != bob.Key() {
		t.Fatalf("%+v %v", d, err)
	}
	if m := b.bar; m.barTo != bob.Key() || m.barFrom != alice.Key() || m.leftLH != 5 || m.leftWhy != "proactive" {
		t.Errorf("bar %+v", m)
	}
}

// Recovered, the account left still lands only ahead by the all-over
// margin: with no hysteresis margin, that is what holds it.
func TestDecideBarMargin(t *testing.T) {
	s := DefaultAuto()
	s.Margin = 0
	a, b := inUse(acct("a", 91, 10, day)), acct("b", 89, 10, day)
	m := memo{barTo: a.Key(), barFrom: b.Key(), leftLH: 5}
	if d := decide(t0, s, []Account{a, b}, m); d.To != "" {
		t.Errorf("%+v", d)
	}
	m.leftLH, b = 5, acct("b", 85, 10, day)
	if d := decide(t0, s, []Account{a, b}, m); d.To != b.Key() {
		t.Errorf("%+v", d)
	}
}

func TestUnseenModels(t *testing.T) {
	a := acct("a", 0, 0, day)
	a.Limits = append(a.Limits, Limit{Kind: "weekly_scoped", Scope: "Fable"})
	s := Auto{Model: "fable, Opus, all, opus"}
	if got := s.Unseen([]Account{a, acct("b", 0, 0, day)}); !slices.Equal(got, []string{"Opus"}) {
		t.Errorf("unseen %q", got)
	}
	unread := acct("c", 0, 0, day)
	unread.Limits = nil
	if got := s.Unseen([]Account{a, unread}); got != nil {
		t.Errorf("before every account was read: %q", got)
	}
	key := Account{Email: "k@x", APIKey: true, Added: true} // never read: it has no limits
	tok := Account{Email: "t@x", Token: true, Added: true}  // a setup-token, the probe off
	if got := s.Unseen([]Account{a, key, tok}); !slices.Equal(got, []string{"Opus"}) {
		t.Errorf("with an API key and a setup-token: %q", got)
	}
}

// A refused refresh token is posted once, not on every tick; a new saved
// login is tried again.
func TestRefreshQuarantinesARefusedLogin(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"] = 10
	renews := 0
	for m := 0; m < 30; m++ {
		b.Now = func() time.Time { return t0.Add(2*time.Hour + time.Duration(m)*time.Minute) }
		b.Refresh(ctx, false)
		for _, c := range f.took() {
			if c == "renew bob-rt" {
				renews++
			}
		}
	}
	if renews != 1 {
		t.Fatalf("renewed a refused login %d times in 30 minutes", renews)
	}
	_, err := b.keep(login("bob-at2", "bob-rt2", t0.Add(3*time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	f.usage["bob-at2"] = 5
	got, _ := b.Refresh(ctx, true)
	if a := blockOf(t, got, "bob@example.com"); a.Err != "" {
		t.Errorf("a new login is still held out: %+v", a)
	}
}

// A failing read backs off: 30 seconds, doubling, at most 10 minutes.
func TestRefreshBacksOff(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"] = 10
	f.status["bob-at"] = 500
	var at []int
	for s := 0; s <= 40*60; s += 15 {
		b.Now = func() time.Time { return t0.Add(time.Duration(s) * time.Second) }
		b.Refresh(ctx, false)
		if slices.Contains(f.took(), "usage bob-at") {
			at = append(at, s)
		}
	}
	want := []int{0, 30, 90, 210, 450, 930, 1530, 2130}
	if !slices.Equal(at, want) {
		t.Errorf("read at %v s, want %v", at, want)
	}
}

// A login that can't be read for another reason (a locked keychain) is
// still nobody to move off.
func TestAutoSwitchNeverSignsInOnUnreadableLogin(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.usage["bob-at"] = 10
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	b.Live = func() (Login, error) { return Login{}, errors.New("keychain locked") }
	for m := 0; m < 10; m++ {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		b.Refresh(ctx, true)
		if d, _ := b.AutoSwitch(ctx); d.To != "" {
			t.Fatalf("minute %d: %+v", m, d)
		}
	}
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "alice-at" {
		t.Errorf("login in use %q", l.AccessToken)
	}
}

// A rate limit, then a refused login: the refusal is what counts.
func TestRefreshClearsWaitOnOtherFailure(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"] = 10
	f.status["bob-at"] = 429
	b.Refresh(ctx, true)
	delete(f.status, "bob-at")
	b.Now = func() time.Time { return t0.Add(2 * time.Hour) }
	got, _ := b.Refresh(ctx, false)
	a := blockOf(t, got, "bob@example.com")
	if a.Err != ErrInvalidGrant.Error() || !a.WaitUntil.IsZero() {
		t.Fatalf("bob = %+v", a)
	}
}

func TestBackoffCaps(t *testing.T) {
	for _, n := range []int{6, 40, 100} {
		if got := backoff(n); got != 10*time.Minute {
			t.Errorf("backoff(%d) = %v", n, got)
		}
	}
}

// A spent account's reset already past is no reason to read it each tick.
func TestRefreshSpentWithPastReset(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 10, 100
	f.resets["bob-at"] = "2026-10-26T11:00:00Z"
	b.Refresh(ctx, true)
	reads := 0
	for m := 1; m <= 9; m++ {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		f.took()
		b.Refresh(ctx, false)
		if slices.Contains(f.took(), "usage bob-at") {
			reads++
		}
	}
	if reads != 0 {
		t.Errorf("read %d times in 9 minutes", reads)
	}
}

// autoswitch.intervalSeconds and unhealthyTicks: failover waits Ticks
// ticks of Interval.
func TestAutoSwitchTicksSetting(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 10
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On, s.Interval, s.Ticks = true, 30, 5
	must(t, b.SetAuto(s))
	must(t, v.Put(liveDefault, "me", stored(t, login("alice-at", "alice-rt", t0.Add(5*time.Hour)))))
	f.status["alice-at"] = 500
	f.renewed["bob-rt"] = "bob-at"
	for sec := 0; sec <= 150; sec += 30 {
		b.Now = func() time.Time { return t0.Add(2*time.Hour + time.Duration(sec)*time.Second) }
		b.Refresh(ctx, true)
		d, _ := b.AutoSwitch(ctx)
		if sec < 150 && d.To != "" || sec == 150 && d.To != bob.Key() {
			t.Fatalf("at %ds: %+v", sec, d)
		}
	}
}

func TestAutoSettingsClamped(t *testing.T) {
	b, _, _, _ := switchBook(t)
	s := DefaultAuto()
	s.Interval, s.Ticks = 5, 500
	must(t, b.SetAuto(s))
	got, _ := b.AutoSettings()
	if got.Interval != 15 || got.Ticks != 100 || got.Every() != 15*time.Second {
		t.Errorf("got %+v", got)
	}
	if (Auto{}).Every() != time.Minute || (Auto{}).failoverAfter() != 3*time.Minute {
		t.Error("defaults")
	}
}
