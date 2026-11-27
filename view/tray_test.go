package view

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lang315/claude-manager/account"
	"github.com/lang315/claude-manager/internal/fixture"
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
	if title != "62%" { // compact: the fuller of her 5h and 7d windows, beside the icon
		t.Errorf("title %q, want the fuller of alice's 5h and 7d windows", title)
	}
	l.tt.Update(func() {
		was := l.a.prefs
		l.a.prefs = account.Prefs{TrayTitle: "off", NoTrayName: true}
		title, _ = l.a.Tray()
		l.a.prefs = was
	})
	l.tt.Frame()
	if title != "" { // the icon alone, no "–" beside it
		t.Errorf("title with nothing to show %q", title)
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
	l.tt.Update(th.Sub[0].Do) // 80%
	l.tt.Frame()
	if saved, _ := l.book.AutoSettings(); saved.DryRun || saved.Line == 80 {
		t.Errorf("a change in the session saved it: %+v", saved)
	}
	if s, _ := l.book.AutoInEffect(); s.Line != 80 || !s.DryRun {
		t.Errorf("in effect %+v", s)
	}
}

// claude-swap's format_title: the name (alias, else the email's local part cut
// to 12 with a "*"), "rejected (!)", the title percentage, then each model's
// weekly limit when title_scoped is on.
func TestTrayTitleModes(t *testing.T) {
	b := account.Account{Email: "alice@example.com", Limits: []account.Limit{{Kind: "session", Percent: 62}, {Kind: "weekly_all", Percent: 40},
		{Kind: "weekly_scoped", Scope: "Fable", Percent: 91}}}
	for mode, want := range map[string]string{"": "62%", "both": "alice · 62% · 40%", "5h": "alice · 62%", "7d": "alice · 40%", "off": "alice"} {
		if got := trayTitle(b, account.Prefs{TrayTitle: mode}); got != want {
			t.Errorf("%q: %q, want %q", mode, got, want)
		}
	}
	for _, c := range []struct {
		name string
		p    account.Prefs
		b    account.Account
		want string
	}{
		{"name off", account.Prefs{NoTrayName: true, TrayTitle: "both"}, b, "62% · 40%"},
		{"scoped", account.Prefs{TrayScoped: true, TrayTitle: "both"}, b, "alice · 62% · 40% · Fable 91%"},
		{"scoped, no name, no pct", account.Prefs{TrayScoped: true, NoTrayName: true, TrayTitle: "off"}, b, "Fable 91%"},
		{"alias", account.Prefs{TrayTitle: "both"}, account.Account{Email: "alice@example.com", Alias: "work", Limits: b.Limits[:1]}, "work · 62%"},
		{"long local part", account.Prefs{TrayTitle: "both"}, account.Account{Email: "averyveryverylongname@example.com", Limits: b.Limits[:1]}, "averyveryve* · 62%"},
		{"12 chars kept", account.Prefs{TrayTitle: "both"}, account.Account{Email: "abcdefghijkl@x", Limits: b.Limits[:1]}, "abcdefghijkl · 62%"},
		{"rejected", account.Prefs{TrayTitle: "off"}, account.Account{Email: "a@x", Limits: []account.Limit{{Kind: "session", Percent: 100, Refused: true}}}, "a · rejected (!)"},
		{"a refused model is not a rejected account", account.Prefs{TrayTitle: "off", TrayScoped: true}, account.Account{Email: "a@x", Limits: []account.Limit{{Kind: "weekly_scoped", Scope: "Fable", Percent: 100, Refused: true}}}, "a · Fable 100%"},
		{"none at all: the icon alone", account.Prefs{TrayTitle: "off", NoTrayName: true}, b, ""},
		{"compact: the fuller window", account.Prefs{TrayScoped: true}, account.Account{Email: "a@x", Limits: []account.Limit{{Kind: "session", Percent: 49}, {Kind: "weekly_all", Percent: 71.4}}}, "71%"},
		{"compact, refused", account.Prefs{}, account.Account{Email: "a@x", Limits: []account.Limit{{Kind: "session", Percent: 100, Refused: true}}}, "!"},
		{"compact, nothing read", account.Prefs{}, account.Account{Email: "a@x"}, ""},
		{"no email, no alias", account.Prefs{TrayTitle: "both"}, account.Account{Limits: b.Limits[:1]}, "62%"},
	} {
		if got := trayTitle(c.b, c.p); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
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

// claude-swap's menu bar choices: thresholds 80/90/95/98, the last 10
// switches.
func TestTrayChoicesAsClaudeSwap(t *testing.T) {
	l := limitsApp(t)
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	var got []string
	for _, it := range trayItem(t, l, "Auto-Switch Threshold").Sub {
		got = append(got, it.Label)
	}
	if strings.Join(got, " ") != "80% 90% 95% 98%" {
		t.Errorf("thresholds %v", got)
	}
	var sw []account.Switched
	for i := range 12 {
		sw = append(sw, account.Switched{At: time.Date(2026, 11, 1, i, 0, 0, 0, time.Local), To: fmt.Sprintf("s%d@x", i)})
	}
	l.tt.Update(func() { l.a.limits.switches = sw })
	l.tt.Frame()
	var items []TrayItem
	l.tt.Update(func() { _, items = l.a.Tray() })
	l.tt.Frame()
	n := 0
	for _, it := range items {
		if strings.Contains(it.Label, "(by hand)") {
			n++
		}
	}
	if n != 10 {
		t.Errorf("%d recent switches shown, want 10", n)
	}
}

// claude-swap's settings menu: "Show account name in menu bar" (on) and
// "Show model limits in title" (off) flip menubar.showAccountName and
// menubar.titleScoped.
func TestTrayTitleSettings(t *testing.T) {
	l := limitsApp(t)
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	find := func(label string) TrayItem {
		for _, it := range trayItem(t, l, "Menu Bar").Sub {
			if it.Label == label {
				return it
			}
		}
		t.Fatalf("no %q", label)
		return TrayItem{}
	}
	if c := find("Title: Compact"); !c.Checked {
		t.Error("compact is not the default title")
	}
	if n, s := find("Show Account Name"), find("Show Model Limits in Title"); !n.Disabled || !s.Disabled {
		t.Errorf("in compact mode the name and model toggles are live: %v, %v", n.Disabled, s.Disabled)
	}
	l.tt.Update(func() { find("Title: 5h · 7d").Do() })
	l.tt.Frame()
	if n, s := find("Show Account Name"), find("Show Model Limits in Title"); !n.Checked || s.Checked || n.Disabled {
		t.Errorf("defaults: name %v, scoped %v, disabled %v", n.Checked, s.Checked, n.Disabled)
	}
	l.tt.Update(func() { find("Show Account Name").Do(); find("Show Model Limits in Title").Do() })
	l.tt.Frame()
	if n, s := find("Show Account Name"), find("Show Model Limits in Title"); n.Checked || !s.Checked {
		t.Errorf("flipped: name %v, scoped %v", n.Checked, s.Checked)
	}
	if p, _ := l.a.Accounts.Prefs(); !p.NoTrayName || !p.TrayScoped {
		t.Errorf("saved %+v", p)
	}
	l.tt.Update(func() { find("Show Account Name").Do() })
	l.tt.Frame()
	if p, _ := l.a.Accounts.Prefs(); p.NoTrayName {
		t.Errorf("name not back on: %+v", p)
	}
}

// claude-swap's on_open_log: `open -R` reveals the log in Finder; with no
// log yet it is the folder that is revealed.
func TestTrayOpenLogReveals(t *testing.T) {
	l := limitsApp(t)
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	dir := t.TempDir()
	l.a.LogPath = filepath.Join(dir, "claude-manager.log")
	var got []string
	l.a.Reveal = func(p string) error { got = append(got, p); return nil }
	trayItem(t, l, "Open Log").Do()
	if len(got) != 1 || got[0] != dir { // no log yet: its folder
		t.Errorf("no log: revealed %v, want [%s]", got, dir)
	}
	must(t, os.WriteFile(l.a.LogPath, []byte("x"), 0o644))
	got = nil
	trayItem(t, l, "Open Log").Do()
	if len(got) != 1 || got[0] != l.a.LogPath {
		t.Errorf("log there: revealed %v, want [%s]", got, l.a.LogPath)
	}
	if len(*l.opened) != 0 {
		t.Errorf("opened %v: the log is revealed, not opened", *l.opened)
	}
}

// claude-swap's health row dates the account in use's own reading, stale
// past max(3 refresh intervals, 15 minutes).
func TestTrayHealthAge(t *testing.T) {
	now := fixture.Now
	for _, c := range []struct {
		name  string
		read  time.Time
		every time.Duration
		want  string
	}{
		{"never read", time.Time{}, 5 * time.Minute, "Auto-switch off · no usage reading yet"},
		{"fresh", now.Add(-2 * time.Minute), 30 * time.Second, "Auto-switch off · usage read " + ago(now.Add(-2*time.Minute), now)},
		{"15 minutes is not stale", now.Add(-15 * time.Minute), 30 * time.Second, "Auto-switch off · usage read " + ago(now.Add(-15*time.Minute), now)},
		{"stale past the 15 minute floor", now.Add(-16 * time.Minute), 30 * time.Second, "Auto-switch off · usage stale, read " + ago(now.Add(-16*time.Minute), now)},
		{"3 long intervals outlast the floor", now.Add(-20 * time.Minute), 10 * time.Minute, "Auto-switch off · usage read " + ago(now.Add(-20*time.Minute), now)},
		{"stale past 3 long intervals", now.Add(-31 * time.Minute), 10 * time.Minute, "Auto-switch off · usage stale, read " + ago(now.Add(-31*time.Minute), now)},
	} {
		if got := health(false, c.read, now, c.every); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if got := health(true, now, now, time.Minute); !strings.HasPrefix(got, "Auto-switch on · ") {
		t.Errorf("on: %q", got)
	}
}

// The row's age is the in-use block's ReadAt, not when the menu last redrew.
func TestTrayHealthUsesTheAccountInUse(t *testing.T) {
	l := limitsApp(t)
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	now := l.a.Store.Now()
	for _, c := range []struct {
		read time.Time
		want string
	}{{time.Time{}, "no usage reading yet"}, {now.Add(-2 * time.Hour), "usage stale, read "}, {now.Add(-time.Minute), "usage read "}} {
		in := account.Account{AccountUUID: "a", Email: "a@x", Added: true, InUse: true, ReadAt: c.read, Limits: []account.Limit{{Kind: "session", Percent: 30}}}
		other := account.Account{AccountUUID: "b", Email: "b@x", Added: true, ReadAt: now}
		l.tt.Update(func() { l.a.limits.blocks, l.a.limits.updated = []account.Account{other, in}, now })
		l.tt.Frame()
		trayItem(t, l, "Auto-switch off · "+c.want)
	}
}

// The icon draws the account in use's two windows; the tooltip spells them out.
func TestTrayIconAndTip(t *testing.T) {
	l := limitsApp(t)
	var icon []byte
	var tip string
	l.tt.Update(func() { icon, tip = l.a.TrayIcon(), l.a.TrayTip() })
	l.tt.Frame()
	if !bytes.Equal(icon, TrayIcon(0, 0)) || tip != "Claude Manager" {
		t.Errorf("before a read: tip %q, icon of shares %v", tip, !bytes.Equal(icon, TrayIcon(0, 0)))
	}
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.a.limits.loaded })
	l.tt.Update(func() { icon, tip = l.a.TrayIcon(), l.a.TrayTip() })
	l.tt.Frame()
	if !bytes.Equal(icon, TrayIcon(62, 100)) {
		t.Error("the icon does not draw 5h 62% and 7d 100%")
	}
	if tip != "alice · 5h 62% · 7d 100%" {
		t.Errorf("tip %q", tip)
	}
}

// A window not read is left out of the tooltip, not shown as 0%.
func TestTrayTipLeavesOutAWindowNotRead(t *testing.T) {
	b := account.Account{Email: "alice@example.com", Limits: []account.Limit{{Kind: "session", Percent: 62}}}
	if got := trayTip(b); got != "alice · 5h 62%" {
		t.Errorf("tip %q", got)
	}
	b.Limits = []account.Limit{{Kind: "weekly_all", Percent: 40}}
	if got := trayTip(b); got != "alice · 7d 40%" {
		t.Errorf("tip %q", got)
	}
}

// fakeAwake counts held activities.
func fakeAwake(l *lim) *int {
	held := 0
	l.a.KeepAwake = func(reason string) func() {
		if reason != "Keep Mac Awake is on in Claude Manager" {
			panic(reason)
		}
		held++
		return func() { held-- }
	}
	return &held
}

func TestTrayKeepAwake(t *testing.T) {
	l := limitsApp(t)
	held := fakeAwake(l)
	it := trayItem(t, l, "Keep Mac Awake")
	if !it.Toggle || it.Checked || *held != 0 {
		t.Fatalf("starts off: %+v held %d", it, *held)
	}
	l.tt.Update(it.Do)
	if !trayItem(t, l, "Keep Mac Awake").Checked || *held != 1 {
		t.Fatalf("on: held %d", *held)
	}
	l.tt.Update(func() { l.a.setAwake(true) }) // on while on
	if *held != 1 {
		t.Fatalf("on twice holds %d", *held)
	}
	l.tt.Update(trayItem(t, l, "Keep Mac Awake").Do)
	l.tt.Update(func() { l.a.setAwake(false) }) // off while off
	if trayItem(t, l, "Keep Mac Awake").Checked || *held != 0 {
		t.Fatalf("off: held %d", *held)
	}
}

func TestWindowKeepAwake(t *testing.T) {
	l := limitsApp(t)
	held := fakeAwake(l)
	l.tt.Frame()
	must(t, l.tt.Click("Keep Mac awake"))
	l.tt.Frame()
	if *held != 1 || !trayItem(t, l, "Keep Mac Awake").Checked {
		t.Fatalf("window on: held %d", *held)
	}
	must(t, l.tt.Click("Keep Mac awake"))
	l.tt.Frame()
	if *held != 0 {
		t.Fatalf("window off: held %d", *held)
	}
}

func TestKeepAwakeHiddenWithoutPower(t *testing.T) {
	l := limitsApp(t) // KeepAwake nil
	l.tt.Frame()
	if l.tt.HasText("Keep Mac awake") {
		t.Error("window checkbox shown without KeepAwake")
	}
	l.tt.Update(func() {
		_, items := l.a.Tray()
		for _, it := range items {
			if it.Label == "Keep Mac Awake" {
				t.Error("tray row shown without KeepAwake")
			}
		}
	})
}

func TestTrayKeepAwakeWithoutAccounts(t *testing.T) {
	l := limitsApp(t)
	fakeAwake(l)
	l.a.Accounts = nil
	trayItem(t, l, "Keep Mac Awake")
}
