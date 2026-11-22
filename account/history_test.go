package account

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestHistoryEventsLog(t *testing.T) {
	b, _, _, f := switchBook(t)
	var told []string
	var log bytes.Buffer
	b.Events = func(title, body string) { told = append(told, title) }
	b.Log = &log
	f.usage["alice-at"], f.usage["bob-at"] = 95, 10
	b.Refresh(ctx, true)
	must(t, b.Switch(ctx, bob.Key()))
	must(t, b.Switch(ctx, alice.Key()))
	s := DefaultAuto()
	s.On, s.Cooldown = true, 0
	must(t, b.SetAuto(s))
	b.Now = func() time.Time { return t0.Add(4 * time.Minute) }
	b.Refresh(ctx, true)
	d, err := b.AutoSwitch(ctx)
	if err != nil || d.To != bob.Key() {
		t.Fatalf("auto: %+v %v", d, err)
	}
	hs, _ := b.Switches()
	if len(hs) != 3 || !hs[0].Auto || hs[0].To != "bob@example.com" || hs[0].From != "alice@example.com" || hs[2].Why != "by hand" || hs[2].Auto {
		t.Fatalf("history %+v", hs)
	}
	// Switches by hand are told too, as claude-swap's menu bar.
	if !slices.Equal(told, []string{"Switched account", "Switched account", "Auto-switched account"}) {
		t.Errorf("told %q", told)
	}
	if !strings.Contains(log.String(), "switch alice@example.com -> bob@example.com") || strings.Contains(log.String(), "-at") {
		t.Errorf("log %q", log.String())
	}
	// Every account spent: told once, not each tick.
	f.usage["alice-at"], f.usage["bob-at"] = 100, 100
	told = nil
	for m := 10; m < 30; m += 5 {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		b.Refresh(ctx, true)
		b.AutoSwitch(ctx)
	}
	if len(told) != 1 || told[0] != "All accounts exhausted" {
		t.Errorf("told %q", told)
	}
	// Room again, then spent again: told again.
	f.usage["alice-at"] = 10
	b.Now = func() time.Time { return t0.Add(40 * time.Minute) }
	b.Refresh(ctx, true)
	b.AutoSwitch(ctx)
	f.usage["alice-at"] = 100
	b.Now = func() time.Time { return t0.Add(50 * time.Minute) }
	b.Refresh(ctx, true)
	b.AutoSwitch(ctx)
	if n := strings.Count(strings.Join(told, "|"), "All accounts exhausted"); n != 2 || slices.Contains(told, "") {
		t.Errorf("not told again, or told empty: %q", told)
	}
	again := &Book{Dir: b.Dir, Vault: b.Vault, Now: b.Now}
	if hs, _ := again.Switches(); len(hs) != 4 {
		t.Errorf("history not saved: %d", len(hs))
	}
}

func TestQuarantineTold(t *testing.T) {
	b, _, _, f := switchBook(t)
	var told []string
	b.Events = func(title, body string) { told = append(told, title+": "+body) }
	f.usage["alice-at"] = 10
	b.Now = func() time.Time { return t0.Add(2 * time.Hour) }
	b.Refresh(ctx, true)
	b.Now = func() time.Time { return t0.Add(3 * time.Hour) }
	b.Refresh(ctx, true)
	if len(told) != 1 || !strings.Contains(told[0], "Account quarantined: bob@example.com") {
		t.Errorf("told %q", told)
	}
}

func TestProjection(t *testing.T) {
	l := Limit{Kind: "weekly_all", Percent: 70, ResetsAt: t0.Add(84 * time.Hour)} // half way, 70%
	at, lasts, ok := l.Projection(t0)
	if !ok || lasts || !at.Equal(t0.Add(36*time.Hour)) {
		t.Errorf("70%% half way: %v %v %v", at, lasts, ok)
	}
	l.Percent = 30
	if _, lasts, ok := l.Projection(t0); !ok || !lasts {
		t.Error("30% half way lasts")
	}
	l.Percent = 0
	if at, lasts, ok := l.Projection(t0); !ok || !lasts || !at.IsZero() {
		t.Error("unused lasts, no ETA")
	}
	if _, _, ok := (Limit{Kind: "session", Percent: 50, ResetsAt: t0.Add(time.Hour)}).Projection(t0); ok {
		t.Error("five-hour has none")
	}
}

func TestHistoryCapped(t *testing.T) {
	b, _, _, _ := switchBook(t)
	for i := 0; i < 60; i++ {
		b.record("a", "b", "x", false)
	}
	if len(b.history) != historyMax {
		t.Errorf("%d kept", len(b.history))
	}
}
