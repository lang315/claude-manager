package view

import (
	"fmt"
	"strings"

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
	status := "Not updated yet"
	if !l.updated.IsZero() {
		status = "Updated " + ago(l.updated, a.Store.Now())
	}
	if l.auto.s.On && l.auto.why != "" {
		status = l.auto.why
	}
	items = append(items, TrayItem{Label: status, Disabled: true}, TrayItem{})
	for _, b := range l.blocks {
		if b.InUse {
			if p, ok := fullest(b); ok {
				title = fmt.Sprintf("%.0f%%", p)
			}
		}
		name := b.Email
		if name == "" {
			name = "Claude Code"
		}
		if w := windows(b); w != "" {
			name += "  " + w
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
		TrayItem{Label: "Switch to the Best Account", Disabled: !can || l.switching != "", Do: func() { l.switchBest(a) }},
		TrayItem{Label: "Refresh", Disabled: l.loading, Do: func() { l.load(a, true) }},
		TrayItem{},
	)
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
	for _, t := range []struct{ label, theme string }{{"Appearance: As macOS", ""}, {"Appearance: Light", "light"}, {"Appearance: Dark", "dark"}} {
		on := a.prefs.Theme == t.theme || t.theme == "" && a.prefs.Theme == "auto"
		items = append(items, TrayItem{Label: t.label, Toggle: true, Checked: on, Do: func() { a.setTheme(t.theme) }})
	}
	return title, items
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

// windows is b's five-hour and 7-day shares and its extra usage, "5h 62% ·
// 7d 100% · extra 25%".
func windows(b account.Account) string {
	var s []string
	for _, x := range b.Limits {
		switch x.Kind {
		case "session", "five_hour":
			s = append(s, fmt.Sprintf("5h %.0f%%", x.Percent))
		case "weekly_all", "seven_day":
			s = append(s, fmt.Sprintf("7d %.0f%%", x.Percent))
		case "extra_usage":
			s = append(s, fmt.Sprintf("extra %.0f%%", x.Percent))
		}
	}
	return strings.Join(s, " · ")
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
func (l *limits) switchBest(a *App) {
	a.Go(func() { // Pick waits for a refresh under way: never on the main thread
		b, err := a.Accounts.Pick("best")
		a.Update(func() {
			if err != nil {
				l.toast(a, err)
				return
			}
			l.startSwitch(a, b)
		})
	})
}
