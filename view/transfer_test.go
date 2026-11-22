package view

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	l.tt.Frame()
	must(t, l.tt.Click("Choose File…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Exported 2 accounts. " + exportWarning + " " + twoMacs) })
	if said != exportWarning+" "+twoMacs {
		t.Errorf("save panel said %q", said)
	}
	if fi, err := os.Stat(path); err != nil || runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("export file: %v %v", fi, err)
	}

	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Remove…"))
	l.tt.Frame()
	must(t, l.tt.Click("Remove"))
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("bob@example.com") })
	l.a.OpenFile = func(string) (string, error) { return path, nil }
	must(t, l.tt.Click("Import…"))
	l.tt.Frame()
	must(t, l.tt.Click("Choose File…"))
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
	must(t, l.tt.Click("Choose File…"))
	l.tt.Frame()
	for _, s := range l.tt.Texts() {
		if len(s) > 8 && s[:8] == "Exported" {
			t.Errorf("toast %q after a cancel", s)
		}
	}
}

func TestLimitsExportFull(t *testing.T) {
	l := limitsApp(t)
	l.live.RefreshToken = "alice-rt"
	h := canSwitch(t, l)
	must(t, os.WriteFile(h.ClaudeJSON, []byte(`{"numStartups":7,"oauthAccount":{"accountUuid":"alice","organizationUuid":"org"}}`), 0o600))
	blob := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"alice","refreshToken":"alice-rt","expiresAt":%d,"accountUuid":"alice","organizationUuid":"org"},"mcpOAuth":{}}`, l.live.ExpiresAt)
	must(t, l.vault.Put("Claude Code-credentials", "me", []byte(blob)))
	path := filepath.Join(t.TempDir(), "out.json")
	l.a.SaveFile = func(string, string) (string, error) { return path, nil }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	for _, full := range []bool{false, true} {
		must(t, l.tt.Click("Export…"))
		l.tt.Frame()
		if full {
			must(t, l.tt.Click("Full backup for this Mac"))
			l.tt.Frame()
		}
		must(t, os.RemoveAll(path))
		must(t, l.tt.Click("Choose File…"))
		waitFor(t, l.tt, func() bool { _, err := os.Stat(path); return err == nil })
		data, _ := os.ReadFile(path)
		if got := strings.Contains(string(data), "numStartups"); got != full {
			t.Errorf("full %v: whole ~/.claude.json in the file: %v", full, got)
		}
	}
}

// A readings file from another Mac, held for 10 minutes.
func TestLimitsImportReadings(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	path := filepath.Join(t.TempDir(), "list.json")
	must(t, os.WriteFile(path, []byte(`{"schemaVersion":1,"accounts":[{"email":"bob@example.com","organizationUuid":"org","usageStatus":"ok","usageAgeSeconds":0,"usage":{"fiveHour":{"pct":41}}}]}`), 0o600))
	*l.now = l.now.Add(time.Minute)
	l.a.OpenFile = func(string) (string, error) { return path, nil }
	must(t, l.tt.Click("Import…"))
	l.tt.Frame()
	must(t, l.tt.Click("Don't read those accounts here for"))
	l.tt.Frame()
	must(t, l.tt.Click("Choose File…"))
	waitFor(t, l.tt, func() bool {
		return l.tt.HasText("Readings: 1 taken, 0 kept (newer here), 0 skipped.") && l.tt.HasText("41%")
	})
	blocks, _ := l.book.Blocks()
	for _, b := range blocks {
		if b.Email == "bob@example.com" && !b.HeldUntil.Equal(l.now.Add(10*time.Minute)) {
			t.Errorf("held until %v", b.HeldUntil)
		}
	}
}
