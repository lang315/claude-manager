package account

import (
	"strings"
	"testing"
	"time"
)

func TestRankingAndAddInUse(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 30
	b.Refresh(ctx, true)
	_, err := b.keep(login("carol-at", "carol-rt", t0.Add(time.Hour)).Marshal(), carol, nil)
	must(t, err)
	b.Now = func() time.Time { return t0.Add(time.Minute) }
	f.usage["carol-at"], f.who["carol-at"] = 80, carol
	b.Refresh(ctx, true)
	r, _ := b.Ranking()
	if len(r) != 2 || r[1].Email != "carol@example.com" || r[1].Left != 20 {
		t.Errorf("by quota left: %+v", r)
	}
	must(t, b.Remove(carol.Key()))
	_, err = b.keep(login("carol-at", "carol-rt", t0.Add(time.Hour)).Marshal(), carol, nil)
	must(t, err)
	r, _ = b.Ranking()
	if len(r) != 2 || r[0].Email != "bob@example.com" || r[0].Left != 70 || !r[0].Known || r[1].Known {
		t.Errorf("ranking %+v", r)
	}
	// Outside Claude Code's default folder nothing follows the login in use: add it by hand.
	c, cv, _, g := switchBook(t)
	c.Home = nil
	g.usage["alice-at"] = 10
	c.Live = func() (Login, error) { return ReadLive(cv, "", t.TempDir()) }
	a, err := c.AddInUse(ctx)
	if err != nil || a.Email != "alice@example.com" {
		t.Fatalf("AddInUse %+v %v", a, err)
	}
	if l, _ := parseLogin(get(t, cv, ParkService, alice.Key())); l.AccessToken != "alice-at" {
		t.Errorf("saved %q", l.AccessToken)
	}
	_ = v
}

// A dry run says what it would do and switches nothing.
func TestDryRun(t *testing.T) {
	b, v, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 95, 10
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On, s.DryRun = true, true
	must(t, b.SetAuto(s))
	d, err := b.AutoSwitch(ctx)
	if err != nil || d.To != "" || !strings.Contains(d.Why, "Would switch to bob@example.com") {
		t.Fatalf("%+v %v", d, err)
	}
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "alice-at" {
		t.Error("a dry run switched")
	}
	if d, _ := b.Once(ctx, false); d.To != bob.Key() {
		t.Errorf("--once ignores the dry run setting: %+v", d)
	}
}
