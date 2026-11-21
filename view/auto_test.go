package view

import (
	"os"
	"slices"
	"strings"
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

func TestAutoSwitchSwitches(t *testing.T) {
	l := limitsApp(t)
	h := canSwitch(t, l)
	l.weekly.Store("bob", 5)
	must(t, l.book.SetAuto(account.Auto{On: true, Line: 90, Cooldown: 300, Margin: 10, Strategy: account.Best}))
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool {
		return l.tt.HasText("Auto-switched to bob@example.com: the account in use reached its limit.")
	})
	waitFor(t, l.tt, func() bool { return inUseIs(l, "bob@example.com") })
	conf, _ := os.ReadFile(h.ClaudeJSON)
	if !strings.Contains(string(conf), `"accountUuid":"bob"`) {
		t.Errorf("~/.claude.json = %s", conf)
	}
}

func inUseIs(l *lim, email string) bool {
	texts := l.tt.Texts()
	for i := range texts[:len(texts)-1] {
		if texts[i] == email {
			for _, s := range texts[i+1 : min(i+4, len(texts))] {
				if s == "In use" {
					return true
				}
			}
		}
	}
	return false
}

func TestLimitsAccountInUseHasNoRemove(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool {
		return inUseIs(l, "alice@example.com") && strings.Count(strings.Join(l.tt.Texts(), "\n"), "⋯") == 2
	})
	clickMenu(t, l.tt, "alice@example.com")
	if m := l.tt.Menu(); slices.Contains(m, "Remove…") || slices.Contains(m, "Switch to This Account") || !slices.Contains(m, "Sign In Again") {
		t.Errorf("menu %q", m)
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
	l.tt.Update(func() { l.a.limits.auto.draft.Model = "Fable" })
	must(t, l.tt.Click("Save"))
	waitFor(t, l.tt, func() bool {
		s, _ := l.book.AutoSettings()
		return s.WeeklyLine == 90 && s.Model == "Fable"
	})
}

func TestAutoSwitchHoldOut(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	clickMenu(t, l.tt, "bob@example.com")
	if !slices.Contains(l.tt.Menu(), "Auto-Switch Line and Priority…") {
		t.Errorf("menu %q", l.tt.Menu())
	}
	must(t, l.tt.ChooseMenuItem("Hold Out of Auto-Switch"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Held out of auto-switch") })
	if got, _ := l.book.Blocks(); !got[1].AutoOff || got[1].Email != "bob@example.com" {
		t.Fatalf("held out: %+v", got)
	}
	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Put Back in Auto-Switch"))
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("Held out of auto-switch") })
}
