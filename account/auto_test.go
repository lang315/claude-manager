package account

import (
	"encoding/json"
	"errors"
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
	ownLine := inUse(acct("own", 82, 10, day))
	ownLine.Line = 80
	tier := func(a Account, p int) Account { a.Priority = p; return a }
	line := func(a Account, l float64) Account { a.Line = l; return a }
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
		{"own line", def, []Account{ownLine, acct("b", 0, 0, day)}, time.Time{}, "b"},
		{"all over: 2 points more is not enough", def, []Account{inUse(acct("a", 95, 10, day)), acct("b", 93, 10, day)}, time.Time{}, ""},
		{"all over: 3 points more moves", def, []Account{inUse(acct("a", 95, 10, day)), acct("b", 92, 10, day)}, time.Time{}, "b"},
		{"all over: twice the room moves", def, []Account{inUse(acct("a", 98, 10, day)), acct("b", 96, 10, day)}, time.Time{}, "b"},
		{"cooldown holds a proactive move", def, []Account{inUse(acct("a", 92, 10, day)), acct("b", 0, 0, day)}, t0.Add(-time.Minute), ""},
		{"at the limit skips the cooldown", def, []Account{inUse(acct("a", 100, 10, day)), acct("b", 0, 0, day)}, t0.Add(-time.Minute), "b"},
		{"held out, stale, or without oauthAccount: none is a landing", def, []Account{inUse(acct("a", 95, 10, day)), held, stale, noAcct}, time.Time{}, ""},
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
			d := decide(t0, c.s, c.accts, c.last)
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
