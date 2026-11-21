package view

import (
	"slices"
	"testing"

	"github.com/lang315/claude-manager/account"
)

func TestAutoSwitchToggle(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	must(t, l.tt.Click("Auto-switch"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("No account has room.") })
	s, _ := l.book.AutoSettings()
	if !s.On {
		t.Fatal("auto-switch not saved on")
	}
	if l.tt.HasText("Switched to") {
		t.Error("switched with no room anywhere")
	}
}

func TestAutoSwitchOffOutsideDefaultFolder(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.book.Home = nil
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText(account.ErrCantSwitch.Error()) })
}

func TestAutoSwitchSettings(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	must(t, l.tt.Click("Auto-Switch Settings…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("A separate line for the weekly window") })
	must(t, l.tt.Click("A separate line for the weekly window"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Weekly line (%)") })
	must(t, l.tt.Click("Save"))
	waitFor(t, l.tt, func() bool {
		s, _ := l.book.AutoSettings()
		return s.WeeklyLine == 90
	})
}

func TestAutoSwitchHoldOut(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	must(t, l.tt.Click("⋯"))
	if !slices.Contains(l.tt.Menu(), "Auto-Switch Line and Priority…") {
		t.Errorf("menu %q", l.tt.Menu())
	}
	must(t, l.tt.ChooseMenuItem("Hold Out of Auto-Switch"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Held out of auto-switch") })
	must(t, l.tt.Click("⋯"))
	must(t, l.tt.ChooseMenuItem("Put Back in Auto-Switch"))
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("Held out of auto-switch") })
}
