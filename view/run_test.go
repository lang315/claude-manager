package view

import (
	"testing"

	"github.com/lang315/claude-manager/account"
)

func TestLimitsOpenInTerminal(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	var launched []string
	l.book.Launch = func(folder, dir string) error { launched = append(launched, folder); return nil }
	l.book.Running = func(string) (bool, error) { return true, nil }
	l.a.ChooseFolder = func(string) (string, error) { return "/work", nil }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Open in Terminal…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Opened Claude Code in Terminal as bob@example.com.") })
	if len(launched) != 1 || launched[0] != "/work" {
		t.Fatalf("launched %q", launched)
	}
	waitFor(t, l.tt, func() bool { return l.tt.HasText("In a terminal") })
	clickMenu(t, l.tt, "bob@example.com")
	for _, item := range []string{"Switch to This Account", "Remove…", "Sign In Again"} {
		if err := l.tt.ChooseMenuItem(item); err == nil {
			t.Errorf("%s offered while in a terminal", item)
		}
	}
}

func TestLimitsNoTerminalForAccountInUse(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.book.Launch = func(string, string) error { t.Fatal("launched"); return nil }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	if account.ProfilesDir(l.book.Dir) == "" {
		t.Fatal("no profiles folder")
	}
	clickMenu(t, l.tt, "alice@example.com")
	if err := l.tt.ChooseMenuItem("Open in Terminal…"); err == nil {
		t.Error("offered for the account in use")
	}
}
