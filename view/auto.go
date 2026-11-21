package view

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
)

var (
	strategies     = []string{account.Best, account.WeeklyFirst, account.ConsumeFirst, account.ByPriority}
	strategyLabels = []string{"Most left", "Weekly reset first", "Spend soonest reset", "By priority"}
)

// autoUI is auto-switch on the Limits page.
type autoUI struct {
	s               account.Auto
	loaded          bool
	why             string
	log             []string            // claude-swap's TUI event log: the last decisions, the latest first
	ranked          []account.Candidate // its "next best" list
	showLog, goLive bool
	confirmLive     bool
	pending         account.Auto
	ticked          time.Time // the last tick that ran

	editing  bool
	draft    account.Auto
	weekly   bool
	cooldown float64 // minutes
	interval float64 // seconds
	ticks    float64
	strategy string

	tuning   bool
	tuned    account.Account
	ownLine  bool
	line     float64
	priority float64
}

// AutoTick runs auto-switch, whatever page is shown and whether or not the
// app is in front, once its interval has passed since the last tick. main
// calls it every 15 seconds, the shortest interval.
func (a *App) AutoTick() {
	if a.Accounts == nil {
		return
	}
	a.Go(func() {
		s, err := a.Accounts.AutoSettings()
		p, _ := a.Accounts.Prefs()
		a.Update(func() {
			a.prefs = p
			u := &a.limits.auto
			u.s, u.loaded = s, err == nil
			if now := a.Store.Now(); s.On && now.Sub(u.ticked) >= s.Every()-time.Second {
				u.ticked = now
				a.limits.load(a, false)
			}
		})
	})
}

func (l *limits) autoRow(c *ui.Context, a *App, pal *palette) {
	u := &l.auto
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		on := u.s.On
		ui.Checkbox(c, &on, "Auto-switch").Disabled(!u.loaded || !a.Accounts.CanSwitch())
		if on != u.s.On {
			s := u.s
			s.On = on
			l.saveAuto(a, s)
		}
		switch {
		case !a.Accounts.CanSwitch():
			ui.Text(c, account.ErrCantSwitch.Error()).FontSize(12).TextColor(pal.muted)
		case u.s.On && len(u.s.Unseen(l.blocks)) > 0:
			ui.Text(c, strings.Join(u.s.Unseen(l.blocks), ", ")+" matches no account's windows: only the 5h and 7d limits count for it (typo?).").FontSize(12).TextColor(pal.muted).SingleLine()
		case u.s.On && u.why != "":
			ui.Text(c, u.why).FontSize(12).TextColor(pal.muted).SingleLine()
			if next := nextBest(u.ranked); next != "" {
				ui.Text(c, "Next: "+next).FontSize(12).TextColor(pal.muted).SingleLine()
			}
		case u.s.On:
			ui.Text(c, "Checks every minute while the app is open.").FontSize(12).TextColor(pal.muted)
		}
		ui.Spacer(c)
		if len(u.log) > 0 && ui.Button(c, "Log…").Clicked() {
			u.showLog = true
		}
		if ui.Button(c, "Auto-Switch Settings…").Disabled(!u.loaded).Clicked() {
			u.draft, u.weekly, u.cooldown = u.s, u.s.WeeklyLine != 0, float64(u.s.Cooldown)/60
			u.interval, u.ticks = u.s.Every().Seconds(), float64(u.s.Ticks)
			if u.ticks == 0 {
				u.ticks = 3
			}
			if !u.weekly {
				u.draft.WeeklyLine = u.s.Line
			}
			u.strategy = strategyLabels[max(0, slices.Index(strategies, u.s.Strategy))]
			u.editing = true
		}
	})
}

func (l *limits) saveAuto(a *App, s account.Auto) {
	if l.auto.s.DryRun && !s.DryRun && s.On && !l.auto.goLive {
		// Out of a dry run: claude-swap's TUI asks before going live.
		l.auto.pending, l.auto.confirmLive = s, true
		return
	}
	l.auto.goLive = false
	l.auto.s = s
	a.Go(func() {
		err := a.Accounts.SetAuto(s)
		saved, _ := a.Accounts.AutoSettings()
		a.Update(func() {
			l.auto.s = saved
			l.toast(a, err)
			if saved.On {
				l.load(a, false)
			}
		})
	})
}

// autoBadges are an added account's own auto-switch settings, for its
// block's footer.
func autoBadges(b account.Account) string {
	var s []string
	if b.AutoOff {
		s = append(s, "Held out of auto-switch")
	}
	if b.Line != 0 {
		s = append(s, fmt.Sprintf("Own line %g%%", b.Line))
	}
	if b.Priority != 0 {
		s = append(s, fmt.Sprintf("Priority %d", b.Priority))
	}
	return strings.Join(s, " · ")
}

func (l *limits) autoMenu(m *ui.Menu, a *App, b account.Account) {
	label := "Hold Out of Auto-Switch"
	if b.AutoOff {
		label = "Put Back in Auto-Switch"
	}
	if m.Item(label).Chosen() {
		l.setAccountAuto(a, b.Key(), !b.AutoOff, b.Line, b.Priority)
	}
	if m.Item("Auto-Switch Line and Priority…").Chosen() {
		u := &l.auto
		u.tuned, u.ownLine, u.line, u.priority = b, b.Line != 0, b.Line, float64(b.Priority)
		if !u.ownLine {
			u.line = u.s.Line
		}
		u.tuning = true
	}
}

func (l *limits) setAccountAuto(a *App, key string, off bool, line float64, priority int) {
	a.Go(func() {
		err := a.Accounts.SetAccountAuto(key, off, line, priority)
		blocks, _ := a.Accounts.Blocks()
		a.Update(func() {
			l.toast(a, err)
			l.blocks = blocks
		})
	})
}

func (l *limits) autoDialogs(c *ui.Context, a *App) {
	u := &l.auto
	if ui.AlertDialog(c, &u.confirmLive, "Go live?", "Auto-switch will switch accounts from now on, not just say what it would do.", "Cancel", "Go Live") == 1 {
		u.goLive = true
		l.saveAuto(a, u.pending)
	}
	ui.Modal(c, &u.showLog, func() {
		ui.Column(c).Gap(8).Padding(20).Width(560).Children(func() {
			ui.Text(c, "Auto-Switch Log").FontSize(17).Bold()
			ui.Scroll(c).MaxHeight(360).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					for _, line := range u.log {
						ui.Text(c, line).FontSize(12).Wrap()
					}
				})
			})
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if ui.PrimaryButton(c, "Done").Clicked() {
					u.showLog = false
				}
			})
		})
	})
	field := func(label string, fn func()) {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).Width(170)
			fn()
		})
	}
	ui.Modal(c, &u.editing, func() {
		ui.Column(c).Gap(12).Padding(20).Width(460).Children(func() {
			ui.Text(c, "Auto-Switch Settings").FontSize(17).Bold()
			ui.Text(c, "Switches before the account in use reaches a line, to the added account the strategy picks.").Wrap()
			field("Five-hour line (%)", func() { ui.NumberInput(c, &u.draft.Line, 50, 99.9, 1) })
			ui.Checkbox(c, &u.weekly, "A separate line for the weekly window")
			if u.weekly {
				field("Weekly line (%)", func() { ui.NumberInput(c, &u.draft.WeeklyLine, 50, 99.9, 1) })
			}
			field("Cooldown (minutes)", func() { ui.NumberInput(c, &u.cooldown, 0, 1440, 1) })
			field("Margin (points)", func() { ui.NumberInput(c, &u.draft.Margin, 0, 50, 1) })
			field("Check every (seconds)", func() { ui.NumberInput(c, &u.interval, 15, 3600, 15) })
			field("Unreadable ticks before moving on", func() { ui.NumberInput(c, &u.ticks, 1, 100, 1) })
			ui.Checkbox(c, &u.draft.Keys, "Fall back on an API key account when no account has room")
			ui.Checkbox(c, &u.draft.DryRun, "Dry run: say what it would do, switch nothing")
			field("Strategy", func() { ui.Select(c, &u.strategy, strategyLabels) })
			field("Model windows", func() { ui.TextInput(c, &u.draft.Model).Placeholder("None; e.g. Fable, or all").Grow(1) })
			ui.Text(c, "Their weekly windows count too, on the weekly line: the names on the Weekly rows, comma separated.").FontSize(12).Wrap()
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					u.editing = false
				}
				if ui.PrimaryButton(c, "Save").Clicked() {
					s := u.draft
					if !u.weekly {
						s.WeeklyLine = 0
					}
					s.Cooldown = int(u.cooldown * 60)
					s.Interval, s.Ticks = int(u.interval), int(u.ticks)
					s.Strategy = strategies[max(0, slices.Index(strategyLabels, u.strategy))]
					u.editing = false
					l.saveAuto(a, s)
				}
			})
		})
	})
	ui.Modal(c, &u.tuning, func() {
		ui.Column(c).Gap(12).Padding(20).Width(440).Children(func() {
			ui.Text(c, u.tuned.Email).FontSize(17).Bold()
			ui.Checkbox(c, &u.ownLine, "Its own five-hour line")
			if u.ownLine {
				field("Five-hour line (%)", func() { ui.NumberInput(c, &u.line, 50, 99.9, 1) })
			}
			field("Priority (0–1000)", func() { ui.NumberInput(c, &u.priority, 0, 1000, 1) })
			ui.Text(c, "Only the By priority strategy uses it: higher goes first.").FontSize(12).Wrap()
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					u.tuning = false
				}
				if ui.PrimaryButton(c, "Save").Clicked() {
					line := 0.0
					if u.ownLine {
						line = u.line
					}
					u.tuning = false
					l.setAccountAuto(a, u.tuned.Key(), u.tuned.AutoOff, line, int(u.priority))
				}
			})
		})
	})
}

// nextBest is claude-swap's TUI "next best": the first three accounts by
// quota left, those without a reading as "unknown".
func nextBest(r []account.Candidate) string {
	var s []string
	for _, c := range r[:min(3, len(r))] {
		if c.Known {
			s = append(s, fmt.Sprintf("%s %.0f%% left", c.Email, c.Left))
		} else {
			s = append(s, c.Email+" unknown")
		}
	}
	return strings.Join(s, " · ")
}

// logWhy keeps a decision in the log when it differs from the last one.
func (u *autoUI) logWhy(now time.Time, why string) {
	if why == "" || len(u.log) > 0 && strings.HasSuffix(u.log[0], "  "+why) {
		return
	}
	u.log = append([]string{now.Format("Jan 2 15:04:05") + "  " + why}, u.log...)
	if len(u.log) > 50 {
		u.log = u.log[:50]
	}
}
