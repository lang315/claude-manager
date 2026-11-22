package view

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lang315/claude-manager/account"
)

// TrayItem is one row of the menu bar item's menu. A zero Label is a
// separator. Do runs on the main thread, where main's menu calls it.
type TrayItem struct {
	Label    string
	Toggle   bool // a checkbox: Checked says whether it is on
	Checked  bool
	Disabled bool
	Do       func()
	Sub      []TrayItem // a submenu, as claude-swap's Add, Remove and Disable menus
}

// Tray is the menu bar item, as claude-swap's menu bar shows it: the title
// is the fullest window of the account in use; the menu lists each
// account's windows, switches on a click, and turns auto-switch on and off.
func (a *App) Tray() (title string, items []TrayItem) {
	l := &a.limits
	title = "–"
	can := a.Accounts != nil && a.Accounts.CanSwitch()
	if a.Accounts == nil {
		return title, []TrayItem{{Label: "Limits are unavailable.", Disabled: true}}
	}
	now := a.Store.Now()
	items = append(items, TrayItem{Label: health(l.auto.s.On, l.updated, now), Disabled: true})
	if l.auto.s.On && l.auto.why != "" {
		items = append(items, TrayItem{Label: l.auto.why, Disabled: true})
	}
	items = append(items, TrayItem{})
	for _, b := range l.blocks {
		if b.InUse {
			if t := trayTitle(b); t != "" {
				title = t
			}
		}
		name := b.Email
		if name == "" {
			name = "Claude Code"
		}
		if b.Alias != "" {
			name = b.Alias + " · " + name
		}
		if b.Slot > 0 {
			name = fmt.Sprintf("%d  %s", b.Slot, name)
		}
		if w := windows(b, now); w != "" {
			name += "  " + w
		}
		if b.AutoOff {
			name += "  (disabled)"
		}
		if !b.GoodUntil.IsZero() && b.GoodUntil.Sub(now) < 7*24*time.Hour {
			name += "  re-login " + strings.TrimPrefix(goodUntil(b.GoodUntil, now), "Sign-in ")
		}
		if b.Terminal.Dir != "" {
			name += "  (in a terminal)"
		}
		it := TrayItem{Label: name, Checked: b.InUse}
		if b.InUse || !can || !b.Added || b.Terminal.Dir != "" || l.switching != "" {
			it.Disabled = true
		} else {
			it.Do = func() { l.startSwitch(a, b) }
		}
		items = append(items, it)
	}
	items = append(items, TrayItem{},
		TrayItem{Label: "Auto-switch", Toggle: true, Checked: l.auto.s.On, Disabled: !l.auto.loaded || !can, Do: func() {
			s := l.auto.s
			s.On = !s.On
			l.saveAuto(a, s)
		}},
		TrayItem{Label: "Switch to the Best Account", Disabled: !can || l.switching != "", Do: func() { l.switchPick(a, "best") }},
		TrayItem{Label: "Switch to the Next Account", Disabled: !can || l.switching != "", Do: func() { l.switchPick(a, "rotate") }},
		TrayItem{Label: "Switch to the Next Available", Disabled: !can || l.switching != "", Do: func() { l.switchPick(a, "next-available") }},
		TrayItem{Label: "Refresh", Disabled: l.loading, Do: func() { l.load(a, true) }},
		TrayItem{},
	)
	items = append(items, l.trayMenus(a, can)...)
	if len(l.switches) > 0 { // claude-swap's recent switch history
		items = append(items, TrayItem{Label: "Recent switches", Disabled: true})
		for _, s := range l.switches[:min(5, len(l.switches))] {
			how := "by hand"
			if s.Auto {
				how = "auto"
			}
			items = append(items, TrayItem{Label: s.At.Local().Format("Jan 2 15:04") + "  " + s.To + " (" + how + ")", Disabled: true})
		}
		items = append(items, TrayItem{})
	}
	if a.LogPath != "" { // claude-swap's "Open full log…"
		items = append(items, TrayItem{Label: "Open Log", Do: func() {
			if err := a.OpenURL("file://" + a.LogPath); err != nil {
				a.limits.toast(a, err)
			}
		}}, TrayItem{})
	}
	for _, t := range []struct{ label, theme string }{{"Appearance: As macOS", ""}, {"Appearance: Light", "light"}, {"Appearance: Dark", "dark"}} {
		on := a.prefs.Theme == t.theme || t.theme == "" && a.prefs.Theme == "auto"
		items = append(items, TrayItem{Label: t.label, Toggle: true, Checked: on, Do: func() { a.setTheme(t.theme) }})
	}
	return title, items
}

// health is claude-swap's first row: whether auto-switch is on, and how
// old the reading is, stale past 15 minutes.
func health(on bool, read, now time.Time) string {
	s := "Auto-switch off"
	if on {
		s = "Auto-switch on"
	}
	switch {
	case read.IsZero():
		return s + " · no usage reading yet"
	case now.Sub(read) > 15*time.Minute:
		return s + " · usage stale, read " + ago(read, now)
	}
	return s + " · usage read " + ago(read, now)
}

// trayTitle is the menu bar title for the account in use: its five-hour
// and 7-day shares, as claude-swap's.
func trayTitle(b account.Account) string {
	var s []string
	for _, x := range b.Limits {
		switch x.Kind {
		case "session", "five_hour", "weekly_all", "seven_day":
			s = append(s, fmt.Sprintf("%.0f%%", x.Percent))
		}
	}
	return strings.Join(s, " · ")
}

// fullest is the highest share used of b's five-hour and 7-day windows.
func fullest(b account.Account) (float64, bool) {
	p, ok := 0.0, false
	for _, x := range b.Limits {
		switch x.Kind {
		case "session", "five_hour", "weekly_all", "seven_day":
			p, ok = max(p, x.Percent), true
		}
	}
	return p, ok
}

// windows is b's windows as claude-swap's rows: "5h 62% (2h 13m) · 7d 40%
// (ahead) (3d 4h) · Fable 100% (!) · extra 25%".
func windows(b account.Account, now time.Time) string {
	var s []string
	for _, x := range b.Limits {
		var seg string
		switch x.Kind {
		case "session", "five_hour":
			seg = fmt.Sprintf("5h %.0f%%", x.Percent)
		case "weekly_all", "seven_day":
			seg = fmt.Sprintf("7d %.0f%%", x.Percent)
		case "weekly_scoped":
			seg = fmt.Sprintf("%s %.0f%%", x.Scope, x.Percent)
		case "extra_usage":
			s = append(s, fmt.Sprintf("extra %.0f%%", x.Percent))
			continue
		default:
			continue
		}
		if x.Percent >= 100 || x.Refused {
			seg += " (!)"
		} else if _, ahead, ok := x.Pace(b.ReadAt); ok && ahead {
			seg += " (ahead)"
		}
		if !x.ResetsAt.IsZero() && x.ResetsAt.After(now) {
			seg += " (" + countdown(x.ResetsAt.Sub(now)) + ")"
		}
		s = append(s, seg)
	}
	return strings.Join(s, " · ")
}

// countdown is d as claude-swap's: 3d 4h, 2h 13m, 12m.
func countdown(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", max(1, int(d.Minutes())))
}

// TrayTick refreshes the limits for the menu bar item, whatever page is
// shown and whether or not the window is open. main calls it every 5
// minutes; the 3-minute floor keeps it from asking more often.
func (a *App) TrayTick() {
	a.Update(func() { a.limits.load(a, false) })
}

// setTheme saves ui.theme and redraws in it. Only the theme is saved:
// a.prefs may be older than the book's.
func (a *App) setTheme(theme string) {
	a.prefs.Theme = theme
	a.Go(func() {
		err := a.Accounts.Set("ui.theme", theme)
		a.Update(func() { a.limits.toast(a, err) })
	})
}

// switchBest is claude-swap's TUI best pick: the account with the most
// quota left.
func (l *limits) switchBest(a *App) { l.switchPick(a, "best") }

// switchPick switches to the account Pick names for strategy: best, rotate
// or next-available, as claude-swap's menu bar.
func (l *limits) switchPick(a *App, strategy string) {
	a.Go(func() { // Pick waits for a refresh under way: never on the main thread
		b, err := a.Accounts.Pick(strategy)
		a.Update(func() {
			var stay *account.Stay
			if errors.As(err, &stay) { // nothing to gain: a notice, not a failure
				a.notice = stay.Message
				return
			}
			if err != nil {
				l.toast(a, err)
				return
			}
			l.startSwitch(a, b)
		})
	})
}

// trayMenus are claude-swap's menu bar submenus: Add account, Remove
// account, Disable / enable account, and the auto-switch threshold. Add
// and Remove open the window, where they are asked and confirmed.
func (l *limits) trayMenus(a *App, can bool) []TrayItem {
	open := func(f func()) func() {
		return func() {
			a.Show("limits")
			if a.Raise != nil {
				a.Raise()
			}
			f()
		}
	}
	add := TrayItem{Label: "Add Account", Sub: []TrayItem{
		{Label: "Sign In to Another…", Do: open(func() { l.startAdd(a) })},
		{Label: "From a Setup-Token or API Key…", Do: open(l.startToken)},
	}}
	remove := TrayItem{Label: "Remove Account"}
	toggle := TrayItem{Label: "Disable / Enable Account"}
	for _, b := range l.blocks {
		if !b.Added {
			continue
		}
		remove.Sub = append(remove.Sub, TrayItem{Label: b.Email + "…", Disabled: b.InUse && can || b.Terminal.Dir != "",
			Do: open(func() { l.removing, l.asking = b, true })})
		toggle.Sub = append(toggle.Sub, TrayItem{Label: b.Email, Toggle: true, Checked: !b.AutoOff,
			Do: func() { l.setAccountAuto(a, b.Key(), !b.AutoOff, b.Line, b.Priority) }})
	}
	if len(remove.Sub) == 0 {
		remove.Sub = []TrayItem{{Label: "No added accounts", Disabled: true}}
		toggle.Sub = remove.Sub
	}
	line := TrayItem{Label: "Auto-Switch Threshold"}
	for _, p := range []float64{70, 80, 85, 90, 95} {
		line.Sub = append(line.Sub, TrayItem{Label: fmt.Sprintf("%.0f%%", p), Toggle: true, Checked: l.auto.s.Line == p,
			Disabled: !l.auto.loaded, Do: func() {
				s := l.auto.s
				s.Line = p
				l.saveAuto(a, s)
			}})
	}
	return []TrayItem{add, remove, toggle, line, {}}
}
