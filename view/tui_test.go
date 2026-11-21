package view

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
)

func TestAddThisAccount(t *testing.T) {
	l := limitsApp(t)
	l.open(t)
	must(t, l.tt.Click("Add This Account"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Added alice@example.com.") })
	blocks, _ := l.book.Blocks()
	if len(blocks) != 1 || !blocks[0].Added {
		t.Errorf("blocks %+v", blocks)
	}
}

func TestDryRunGoLive(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	s := account.DefaultAuto()
	s.On, s.DryRun = true, true
	must(t, l.book.SetAuto(s))
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.a.limits.auto.loaded })
	off := s
	off.DryRun = false
	l.tt.Update(func() { l.a.limits.saveAuto(l.a, off) })
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Go live?") })
	if got, _ := l.book.AutoSettings(); !got.DryRun {
		t.Fatal("went live without asking")
	}
	must(t, l.tt.Click("Go Live"))
	waitFor(t, l.tt, func() bool { got, _ := l.book.AutoSettings(); return !got.DryRun })
}

func TestNextBestLogAndShortcuts(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.weekly.Store("bob", 20)
	l.weekly.Store("alice", 50)
	s := account.DefaultAuto()
	s.On = true
	must(t, l.book.SetAuto(s))
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Next: bob@example.com 38% left") })
	must(t, l.tt.Click("Log…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Auto-Switch Log") })
	must(t, l.tt.Click("Done"))
	l.tt.Frame()
	// Command-L turns auto-switch off.
	l.tt.TypeKey(ui.Super, ui.KeyL, "")
	waitFor(t, l.tt, func() bool { got, _ := l.book.AutoSettings(); return !got.On })
	if nextBest([]account.Candidate{{Email: "x", Known: false}}) != "x unknown" {
		t.Error("unknown")
	}
}

func TestSwitchBest(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.weekly.Store("bob", 20)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	l.tt.Update(trayItem(t, l, "Switch to the Best Account").Do)
	waitFor(t, l.tt, func() bool {
		return l.tt.HasText("Switched to bob@example.com. Running sessions follow once macOS's keychain cache lapses, or when started again.")
	})
}

func TestLineTick(t *testing.T) {
	b := account.Account{Added: true, Line: 85}
	s := account.Auto{On: true, Line: 90, WeeklyLine: 97}
	for kind, want := range map[string]float64{"session": 85, "weekly_all": 97, "weekly_scoped": 0} {
		if got := lineFor(account.Limit{Kind: kind}, b, s); got != want {
			t.Errorf("%s: %g", kind, got)
		}
	}
	s.On = false
	if lineFor(account.Limit{Kind: "session"}, b, s) != 0 {
		t.Error("tick with auto-switch off")
	}
	if !strings.HasSuffix((account.Limit{Kind: "session", Refused: true}).Label(), "refused") {
		t.Error("refused label")
	}
}

func TestTokenSlotAsks(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	must(t, l.tt.Click("Add Token…"))
	l.tt.Frame()
	l.tt.Update(func() { l.a.limits.token.token, l.a.limits.token.slot = "sk-ant-oat01-s", 1 })
	l.tt.Frame()
	must(t, l.tt.Click("Add"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Slot 1 is taken") })
	must(t, l.tt.Click("Move It"))
	waitFor(t, l.tt, func() bool {
		blocks, _ := l.book.Blocks()
		for _, b := range blocks {
			if b.Token && b.Slot == 1 {
				return true
			}
		}
		return false
	})
}
