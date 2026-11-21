package view

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLimitsExportImport(t *testing.T) {
	l := limitsApp(t)
	l.live.RefreshToken = "alice-rt"
	canSwitch(t, l)
	path := filepath.Join(t.TempDir(), "out.json")
	var said string
	l.a.SaveFile = func(name, message string) (string, error) { said = message; return path, nil }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	must(t, l.tt.Click("Export…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Exported 2 accounts. " + exportWarning) })
	if said != exportWarning {
		t.Errorf("save panel said %q", said)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("export file: %v %v", fi, err)
	}

	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Remove…"))
	l.tt.Frame()
	must(t, l.tt.Click("Remove"))
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("bob@example.com") })
	l.a.OpenFile = func(string) (string, error) { return path, nil }
	must(t, l.tt.Click("Import…"))
	waitFor(t, l.tt, func() bool {
		return l.tt.HasText("Imported: added 1 account, left 1 as they were. " + twoMacs)
	})
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
}

func TestLimitsExportCanceled(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.a.SaveFile = func(string, string) (string, error) { return "", nil }
	l.open(t)
	must(t, l.tt.Click("Export…"))
	l.tt.Frame()
	for _, s := range l.tt.Texts() {
		if len(s) > 8 && s[:8] == "Exported" {
			t.Errorf("toast %q after a cancel", s)
		}
	}
}
