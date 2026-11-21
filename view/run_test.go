package view

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lang315/claude-manager/account"
)

func TestLimitsOpenInTerminal(t *testing.T) {
	l := limitsApp(t)
	h := canSwitch(t, l)
	must(t, os.Mkdir(filepath.Join(h.Root, "projects"), 0o700))
	must(t, os.WriteFile(filepath.Join(h.Root, "settings.json"), []byte("{}"), 0o600))
	var launched, dirs []string
	l.book.Launch = func(folder, dir string) error {
		launched, dirs = append(launched, folder), append(dirs, dir)
		return nil
	}
	l.book.Running = func(string) (bool, error) { return true, nil }
	l.a.ChooseFolder = func(string) (string, error) { return "/work", nil }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Open in Terminal…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("No folder chosen") })
	if err := l.tt.Click("Open"); err == nil && len(launched) > 0 {
		t.Fatal("opened without a folder")
	}
	must(t, l.tt.Click("Choose Folder…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("/work") })
	must(t, l.tt.Click("Share chat history with the other accounts"))
	l.tt.Frame()
	must(t, l.tt.Click("Open"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Opened Claude Code in Terminal as bob@example.com.") })
	if len(launched) != 1 || launched[0] != "/work" {
		t.Fatalf("launched %q", launched)
	}
	if _, err := os.Readlink(filepath.Join(dirs[0], "settings.json")); err != nil {
		t.Error("setup not shared")
	}
	if _, err := os.Lstat(filepath.Join(dirs[0], "projects")); err == nil {
		t.Error("history shared though unticked")
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

// run.shareHistory false: the dialog starts with history unshared.
func TestOpenInTerminalSharePref(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	must(t, l.book.SetPrefs(account.Prefs{NoShareHistory: true}))
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Open in Terminal…"))
	l.tt.Frame()
	if s := l.a.limits.run.share; !s.Setup || s.History {
		t.Errorf("share %+v", s)
	}
}
