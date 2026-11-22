package view

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lang315/claude-manager/account"
)

func trayItem(t *testing.T, l *lim, prefix string) TrayItem {
	t.Helper()
	var got TrayItem
	found := false
	l.tt.Update(func() {
		_, items := l.a.Tray()
		for _, it := range items {
			if strings.HasPrefix(it.Label, prefix) || strings.Contains(it.Label, "  "+prefix) { // after a slot number
				got, found = it, true
			}
		}
	})
	l.tt.Frame()
	if !found {
		t.Fatalf("no tray item %q", prefix)
	}
	return got
}

func TestTray(t *testing.T) {
	l := limitsApp(t)
	h := canSwitch(t, l)
	l.weekly.Store("alice", 40)
	l.a.AutoTick()
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded && len(l.a.limits.blocks) == 2 })
	var title string
	l.tt.Update(func() { title, _ = l.a.Tray() })
	l.tt.Frame()
	if title != "62% · 40%" { // claude-swap's: the 5h and 7d windows
		t.Errorf("title %q, want alice's 5h and 7d windows", title)
	}
	alice := trayItem(t, l, "alice@example.com")
	if !strings.Contains(alice.Label, "alice@example.com  5h 62% (") || !strings.Contains(alice.Label, "· 7d 40%") || !alice.Checked || !alice.Disabled {
		t.Errorf("alice %+v", alice)
	}
	bob := trayItem(t, l, "bob@example.com")
	if bob.Checked || bob.Disabled || bob.Do == nil {
		t.Fatalf("bob %+v", bob)
	}
	if h := trayItem(t, l, "Auto-switch off · usage read"); !h.Disabled { // claude-swap's health row
		t.Errorf("health %+v", h)
	}
	for _, p := range []string{"Switch to the Next Account", "Switch to the Next Available"} {
		if it := trayItem(t, l, p); it.Do == nil {
			t.Errorf("%s %+v", p, it)
		}
	}
	if auto := trayItem(t, l, "Auto-switch"); !auto.Toggle || auto.Checked || auto.Disabled {
		t.Errorf("auto-switch %+v", auto)
	}
	l.tt.Update(bob.Do)
	l.tt.Frame()
	waitFor(t, l.tt, func() bool {
		conf, _ := os.ReadFile(h.ClaudeJSON)
		return strings.Contains(string(conf), `"accountUuid":"bob"`)
	})
	l.tt.Update(trayItem(t, l, "Auto-switch").Do)
	l.tt.Frame()
	waitFor(t, l.tt, func() bool {
		s, _ := l.book.AutoSettings()
		return s.On
	})
}

func TestTrayNoSwitchOutsideDefaultFolder(t *testing.T) {
	l := limitsApp(t)
	seedBob(t, l, "bob")
	l.a.AutoTick()
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded && len(l.a.limits.blocks) == 2 && l.a.limits.auto.loaded })
	if bob := trayItem(t, l, "bob@example.com"); !bob.Disabled || bob.Do != nil {
		t.Errorf("bob %+v", bob)
	}
	if auto := trayItem(t, l, "Auto-switch"); !auto.Disabled {
		t.Errorf("auto-switch %+v", auto)
	}
}

func TestTrayRows(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.a.AutoTick()
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	in := account.Account{AccountUUID: "a", Email: "a@x", Added: true, InUse: true, Limits: []account.Limit{
		{Kind: "session", Percent: 30}, {Kind: "weekly_scoped", Scope: "Fable", Percent: 100}}}
	pit := account.Account{AccountUUID: "p", Email: "pit@x"}
	term := account.Account{AccountUUID: "t", Email: "term@x", Added: true}
	term.Terminal.Dir = "/p"
	free := account.Account{AccountUUID: "f", Email: "free@x", Added: true}
	l.tt.Update(func() { l.a.limits.blocks = []account.Account{in, pit, term, free} })
	l.tt.Frame()
	var title string
	l.tt.Update(func() { title, _ = l.a.Tray() })
	l.tt.Frame()
	if title != "30%" {
		t.Errorf("title %q: a model window is not the account's", title)
	}
	for _, c := range []struct {
		prefix string
		off    bool
	}{{"pit@x", true}, {"term@x", true}, {"free@x", false}} {
		if it := trayItem(t, l, c.prefix); it.Disabled != c.off {
			t.Errorf("%s %+v", c.prefix, it)
		}
	}
	if it := trayItem(t, l, "term@x"); !strings.HasSuffix(it.Label, "(in a terminal)") {
		t.Errorf("term %q", it.Label)
	}
	l.tt.Update(func() { l.a.limits.switching = "x" })
	l.tt.Frame()
	if it := trayItem(t, l, "free@x"); !it.Disabled {
		t.Error("a second switch offered while one runs")
	}
}

func TestTrayAppearance(t *testing.T) {
	l := limitsApp(t)
	l.a.AutoTick()
	l.tt.Frame()
	dark := trayItem(t, l, "Appearance: Dark")
	if dark.Checked || !trayItem(t, l, "Appearance: As macOS").Checked {
		t.Fatal("as macOS is the default")
	}
	l.tt.Update(dark.Do)
	l.tt.Frame()
	waitFor(t, l.tt, func() bool { p, _ := l.book.Prefs(); return p.Theme == "dark" })
	if !trayItem(t, l, "Appearance: Dark").Checked {
		t.Error("dark not checked")
	}
	l.tt.Frame()
	if r, _, _, _ := l.tt.Image().At(4, 700).RGBA(); r>>8 > 0x40 {
		t.Errorf("the window is not drawn dark: red %#x", r>>8)
	}
}

func TestTrayRecentSwitches(t *testing.T) {
	l := limitsApp(t)
	h := canSwitch(t, l)
	_ = h
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	must(t, l.book.Switch(context.Background(), "bob/org"))
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return len(l.a.limits.switches) == 1 })
	if it := trayItem(t, l, "Recent switches"); !it.Disabled {
		t.Error("header")
	}
	trayItem(t, l, l.a.limits.switches[0].At.Local().Format("Jan 2 15:04")+"  bob@example.com (by hand)")
}

func TestPollSummary(t *testing.T) {
	if got := pollSummary([]account.Account{{InUse: true, PollEvery: 4*time.Minute + 30*time.Second}}); got != "Reads it every 4m30s" {
		t.Errorf("summary %q", got)
	}
}

// claude-swap's auto view adjusts for the session only: Override, not saved.
func TestAutoForThisSession(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.a.AutoTick()
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.a.limits.auto.loaded })
	must(t, l.tt.Click("Auto-Switch Settings…"))
	l.tt.Frame()
	must(t, l.tt.Click("Dry run: say what it would do, switch nothing"))
	l.tt.Frame()
	must(t, l.tt.Click("Use for This Session"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Auto-switch (this session)") })
	if saved, _ := l.book.AutoSettings(); saved.DryRun {
		t.Error("a session's setting was saved")
	}
	if s, _ := l.book.AutoInEffect(); !s.DryRun {
		t.Error("not in effect")
	}
	// A threshold from the menu bar stays this session's too.
	th := trayItem(t, l, "Auto-Switch Threshold")
	l.tt.Update(th.Sub[0].Do) // 70%
	l.tt.Frame()
	if saved, _ := l.book.AutoSettings(); saved.DryRun || saved.Line == 70 {
		t.Errorf("a change in the session saved it: %+v", saved)
	}
	if s, _ := l.book.AutoInEffect(); s.Line != 70 || !s.DryRun {
		t.Errorf("in effect %+v", s)
	}
}

// claude-swap's title percentage: both windows, one, or none.
func TestTrayTitleModes(t *testing.T) {
	b := account.Account{Limits: []account.Limit{{Kind: "session", Percent: 62}, {Kind: "weekly_all", Percent: 40}}}
	for mode, want := range map[string]string{"": "62% · 40%", "5h": "62%", "7d": "40%", "off": "CM"} {
		if got := trayTitle(b, mode); got != want {
			t.Errorf("%q: %q, want %q", mode, got, want)
		}
	}
}

// An account that cannot be read says so on its row, as claude-swap's
// note: "re-login needed" for a refused sign-in, else the error itself.
func TestTrayRowsSayWhyUnread(t *testing.T) {
	l := limitsApp(t)
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	dead := account.Account{AccountUUID: "d", Email: "dead@x", Added: true, Err: account.ErrInvalidGrant.Error(),
		Limits: []account.Limit{{Kind: "session", Percent: 10}}}
	down := account.Account{AccountUUID: "u", Email: "down@x", Added: true, Err: "Anthropic did not answer: a very long reason that runs on and on."}
	l.tt.Update(func() { l.a.limits.blocks = []account.Account{dead, down} })
	l.tt.Frame()
	if it := trayItem(t, l, "dead@x"); !strings.Contains(it.Label, "re-login needed") {
		t.Errorf("dead %q", it.Label)
	}
	it := trayItem(t, l, "down@x")
	if !strings.Contains(it.Label, "Anthropic did not answer") || !strings.HasSuffix(it.Label, "…") {
		t.Errorf("down %q", it.Label)
	}
}
