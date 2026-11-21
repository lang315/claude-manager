package view

import (
	"os"
	"strings"
	"testing"

	"github.com/lang315/claude-manager/account"
)

func trayItem(t *testing.T, l *lim, prefix string) TrayItem {
	t.Helper()
	var got TrayItem
	found := false
	l.tt.Update(func() {
		_, items := l.a.Tray()
		for _, it := range items {
			if strings.HasPrefix(it.Label, prefix) {
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
	if title != "62%" {
		t.Errorf("title %q, want alice's fullest window", title)
	}
	alice := trayItem(t, l, "alice@example.com")
	if alice.Label != "alice@example.com  5h 62% · 7d 40%" || !alice.Checked || !alice.Disabled {
		t.Errorf("alice %+v", alice)
	}
	bob := trayItem(t, l, "bob@example.com")
	if bob.Checked || bob.Disabled || bob.Do == nil {
		t.Fatalf("bob %+v", bob)
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
