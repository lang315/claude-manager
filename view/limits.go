package view

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
)

// limits is the Limits page: how much of each account's limits is used,
// read from Anthropic.
type limits struct {
	blocks  []account.Account
	loading bool
	loaded  bool      // a refresh has landed
	again   bool      // a load was asked during one
	force   bool      // and it was forced
	updated time.Time // when the last refresh landed

	// The Add Account dialog, while a sign-in runs.
	adding bool
	signIn *account.SignIn
	gen    int  // counts sign-ins, so an old one's late events are ignored
	ended  bool // this sign-in reported its end
	link   string
	asks   bool // claude waits for a code
	code   string
	failed string // why the sign-in failed

	asking   bool // the Remove confirmation is open
	removing account.Account

	switching string

	auto autoUI
}

// load shows the book's saved readings, then refreshes them from
// Anthropic. force is the Refresh button's: it skips the 3-minute floor.
func (l *limits) load(a *App, force bool) {
	if a.Accounts == nil {
		return
	}
	if l.loading {
		l.again, l.force = true, l.force || force
		return
	}
	l.loading = true
	autoOn := l.auto.s.On && l.switching == ""
	a.Go(func() {
		saved, err := a.Accounts.Blocks()
		a.Update(func() {
			if !l.loaded {
				l.blocks = saved
			}
			l.toast(a, err)
		})
		blocks, err := a.Accounts.Refresh(context.Background(), force)
		var d account.Decision
		var autoErr error
		if autoOn {
			d, autoErr = a.Accounts.AutoSwitch(context.Background())
		}
		a.Update(func() {
			l.loading, l.loaded = false, true
			l.blocks, l.updated = blocks, a.Store.Now()
			l.toast(a, err)
			if autoOn {
				l.auto.why = d.Why
				if d.To != "" && autoErr == nil {
					l.auto.why = "Switched to " + d.Email + ": " + d.Why + "."
					a.notice = "Auto-switched to " + d.Email + ": " + d.Why + "."
					l.again, l.force = true, true
				}
			}
			if l.again {
				f := l.force
				l.again, l.force = false, false
				l.load(a, f)
			}
		})
	})
}

// toast shows err, unless a notice is waiting already.
func (l *limits) toast(a *App, err error) {
	if err != nil && a.notice == "" {
		a.notice = err.Error()
	}
}

// LimitsTick refreshes the Limits page while it is shown and the app is in
// front. main calls it every 5 minutes.
func (a *App) LimitsTick() {
	a.Update(func() {
		if a.page == "limits" && a.live.active {
			a.limits.load(a, false)
		}
	})
}

func (l *limits) subtitle(now time.Time) string {
	switch {
	case l.loading:
		return "Updating…"
	case l.updated.IsZero():
		return "Not updated yet"
	}
	return "Updated " + ago(l.updated, now)
}

func (l *limits) view(c *ui.Context, a *App, pal *palette) {
	switch {
	case a.Accounts == nil:
		ui.Text(c, "Limits are unavailable.").TextColor(pal.muted)
	case !l.loaded && len(l.blocks) == 0:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Label("Reading limits")
			ui.Text(c, "Reading limits…").TextColor(pal.muted)
		})
	case len(l.blocks) == 0:
		ui.Text(c, "No accounts yet. Add one with Add Account…").TextColor(pal.muted)
	default:
		now := a.Store.Now()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(14).Children(func() {
				l.autoRow(c, a, pal)
				for _, b := range l.blocks {
					l.block(c, a, pal, b, now)
				}
				if n := a.Accounts.Note(); n != "" {
					ui.Text(c, n).FontSize(12).TextColor(pal.muted)
				}
			})
		})
	}
	l.dialogs(c, a, pal)
	l.autoDialogs(c, a)
}

// block is one account: its name, then a meter per limit, or why there are
// no figures.
func (l *limits) block(c *ui.Context, a *App, pal *palette, b account.Account, now time.Time) {
	face(c, pal).Padding(14, 16).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			name := b.Email
			if name == "" {
				name = "Claude Code"
			}
			ui.Text(c, name).Bold().SingleLine()
			if s := planText(b); s != "" {
				ui.Text(c, s).TextColor(pal.muted).SingleLine()
			}
			if b.InUse {
				ui.Text(c, "In use").FontSize(11).Bold().TextColor(pal.muted)
			}
			if b.Terminal.Dir != "" {
				ui.Text(c, "In a terminal").FontSize(11).Bold().TextColor(pal.muted)
			}
			ui.Spacer(c)
			l.menu(c, a, b)
		})
		switch {
		case len(b.Limits) == 0 && b.Err != "":
			ui.Text(c, b.Err).TextColor(pal.muted).Wrap()
			if b.Added && b.Err == account.ErrInvalidGrant.Error() && ui.Button(c, "Sign In Again").Clicked() {
				l.startAdd(a)
			}
		case len(b.Limits) == 0:
			ui.Text(c, "No figures yet.").TextColor(pal.muted)
		}
		for _, x := range b.Limits {
			limitRow(c, pal, x, now)
		}
		if len(b.Limits) > 0 && b.Err != "" {
			ui.Text(c, b.Err+" Showing numbers from "+clock(b.ReadAt, now)+".").TextColor(pal.muted).Wrap()
		}
		if b.Source != "" && !b.ReadAt.IsZero() {
			ui.Text(c, "Read "+ago(b.ReadAt, now)+" by "+b.Source).FontSize(12).TextColor(pal.muted)
		}
		if b.Added && !b.GoodUntil.IsZero() {
			ui.Text(c, goodUntil(b.GoodUntil, now)).FontSize(12).TextColor(pal.muted)
		}
		if s := autoBadges(b); b.Added && s != "" {
			ui.Text(c, s).FontSize(12).TextColor(pal.muted)
		}
		if pitboardOnly := !b.Added && !b.InUse; pitboardOnly && a.Accounts.CanSwitch() {
			ui.Text(c, "Add this account here to switch to it.").FontSize(12).TextColor(pal.muted)
		}
		if l.switching == b.Key() {
			ui.Text(c, "Switching…").FontSize(12).TextColor(pal.muted)
		}
	})
}

// limitRow is one limit: its name, a meter, the share used and the reset.
func limitRow(c *ui.Context, pal *palette, x account.Limit, now time.Time) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, x.Label()).Width(150).SingleLine()
		meter(c, pal, x.Percent).Grow(1).Label(x.Label())
		num(c, fmt.Sprintf("%.0f%%", x.Percent)).Width(44)
		ui.Text(c, resets(x.ResetsAt, now)).TextColor(pal.muted).Width(150).SingleLine()
		reached := "" // its slot stays, so every row's meter is as long
		if x.Percent >= 100 {
			reached = "Limit reached"
		}
		ui.Text(c, reached).Bold().Width(100).SingleLine()
	})
}

// meter is a limit's bar: graphite, then MyGo's warning colour from 80%
// and danger colour from 95%. Green stays the running lamp's.
func meter(c *ui.Context, pal *palette, pct float64) ui.Element {
	t := c.Theme()
	m := *t
	m.Success = pal.sel
	c.SetTheme(&m)
	e := ui.Meter(c, pct, 0, 100, &ui.MeterLevels{Warning: 80, Critical: 95})
	c.SetTheme(t)
	return e
}

// planText names the plan, and the organisation of a team's.
func planText(b account.Account) string {
	if b.Plan == "" {
		return ""
	}
	plan := strings.ToUpper(b.Plan[:1]) + b.Plan[1:]
	if (b.Plan == "team" || b.Plan == "enterprise") && b.OrgName != "" {
		plan += " · " + b.OrgName
	}
	return plan
}

// resets says when a limit resets.
func resets(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	return "resets " + clock(at, now)
}

// clock is t as the page prints it: the time within a day of now, else the
// date and time.
func clock(t, now time.Time) string {
	t = t.In(now.Location())
	if d := t.Sub(now); d > -24*time.Hour && d < 24*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("Jan 2, 15:04")
}

// ago says how long before now t was: "just now", "4 min ago", or the time.
func ago(t, now time.Time) string {
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	}
	return "at " + t.In(now.Location()).Format("15:04")
}

// goodUntil says how long an added account's saved sign-in lasts.
func goodUntil(t, now time.Time) string {
	const day = 24 * time.Hour
	switch d := t.Sub(now); {
	case d <= 0:
		return "Sign-in expired"
	case d < day:
		return "Sign-in expires today"
	case d < 3*day:
		return fmt.Sprintf("Sign-in expires in %d days", int(d/day)+1)
	}
	return "Sign-in good until " + t.In(now.Location()).Format("Jan 2")
}

// startAdd opens the Add Account dialog and starts a sign-in.
func (l *limits) startAdd(a *App) {
	l.stopSignIn()
	l.adding, l.link, l.asks, l.code, l.failed, l.ended = true, "", false, "", "", false
	gen := l.gen
	// Off the main thread: finding claude runs a login shell, and the book
	// may be busy with a refresh.
	a.Go(func() {
		s, err := a.Accounts.SignIn(context.Background(), func(ev account.Event) {
			a.Update(func() {
				if gen == l.gen {
					l.event(a, ev)
				}
			})
		})
		a.Update(func() {
			switch {
			case gen != l.gen: // canceled while it started
				if s != nil {
					s.Cancel()
				}
			case err != nil:
				l.failed = err.Error()
			case !l.ended:
				l.signIn = s
			}
		})
	})
}

// event takes what the sign-in reports.
func (l *limits) event(a *App, ev account.Event) {
	switch {
	case ev.Link != "":
		l.link = ev.Link
	case ev.AsksCode:
		l.asks = true
	case ev.Done:
		l.signIn, l.ended = nil, true
		if ev.Err != nil {
			l.failed = ev.Err.Error()
			return
		}
		l.adding = false
		l.load(a, true)
	}
}

// stopSignIn cancels the sign-in under way, if any. What it still reports
// is ignored.
func (l *limits) stopSignIn() {
	l.gen++
	if l.signIn != nil {
		l.signIn.Cancel()
		l.signIn = nil
	}
}

// dialogs are the Add Account dialog, the Remove confirmation and the Replace confirmation.
func (l *limits) dialogs(c *ui.Context, a *App, pal *palette) {
	was := l.adding
	ui.Modal(c, &l.adding, func() {
		ui.Column(c).Gap(12).Padding(20).Width(440).Children(func() {
			ui.Text(c, "Add Account").FontSize(17).Bold()
			if l.failed != "" {
				ui.Text(c, l.failed).Wrap()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						l.adding = false
					}
					if ui.PrimaryButton(c, "Try Again").Clicked() {
						l.startAdd(a)
					}
				})
				return
			}
			ui.Text(c, "Sign in, in the browser that opens, as the account to add.").Wrap()
			if l.link == "" {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label("Starting the sign-in")
					ui.Text(c, "Starting Claude Code's sign-in…").TextColor(pal.muted)
				})
			} else {
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "Copy Link").Clicked() {
						a.CopyText(l.link)
					}
					if ui.Button(c, "Open Browser").Clicked() {
						if err := a.OpenURL(l.link); err != nil {
							l.failed = err.Error()
						}
					}
				})
			}
			if l.asks {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &l.code).Placeholder("Code").Grow(1)
					submit := ui.Button(c, "Submit Code").Disabled(strings.TrimSpace(l.code) == "").Clicked()
					if submit && l.signIn != nil {
						if err := l.signIn.Code(l.code); err != nil {
							l.failed = err.Error()
						}
					}
				})
			}
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					l.adding = false
				}
			})
		})
	})
	if was && !l.adding { // Cancel, Escape, or a click outside
		l.stopSignIn()
	}
	title := "Remove " + l.removing.Email + "?"
	msg := "Its saved sign-in is deleted from this Mac. It is not revoked: it stays valid at Anthropic until it expires."
	if ui.AlertDialog(c, &l.asking, title, msg, "Cancel", "Remove") == 1 {
		key := l.removing.Key()
		a.Go(func() {
			err := a.Accounts.Remove(key)
			blocks, _ := a.Accounts.Blocks()
			a.Update(func() {
				l.toast(a, err)
				l.blocks = blocks
			})
		})
	}
}

func (l *limits) menu(c *ui.Context, a *App, b account.Account) {
	if !b.Added {
		return
	}
	inTerminal := b.Terminal.Dir != ""
	switchable := a.Accounts.CanSwitch() && !b.InUse && !inTerminal
	ui.MenuButton(c, "⋯", func(m *ui.Menu) {
		if switchable && m.Item("Switch to This Account").Disabled(l.switching != "").Chosen() {
			l.startSwitch(a, b)
		}
		if a.Accounts.CanSwitch() && !b.InUse && m.Item("Open in Terminal…").Chosen() {
			l.startRun(a, b)
		}
		l.autoMenu(m, a, b)
		if !inTerminal && m.Item("Sign In Again").Chosen() {
			l.startAdd(a)
		}
		if !(b.InUse && a.Accounts.CanSwitch()) && !inTerminal && m.Item("Remove…").Destructive(true).Chosen() {
			l.removing, l.asking = b, true
		}
	}).Tooltip("Actions for " + b.Email)
}

// startRun asks for a folder, then opens Claude Code there in Terminal as b.
func (l *limits) startRun(a *App, b account.Account) {
	a.Go(func() {
		folder, err := a.ChooseFolder("Open Claude Code in this folder as " + b.Email + ". Until it quits there, its sign-in renews in that terminal.")
		if err != nil || folder == "" {
			a.Update(func() { l.toast(a, err) })
			return
		}
		err = a.Accounts.Run(b.Key(), folder)
		a.Update(func() {
			if err == nil {
				a.notice = "Opened Claude Code in Terminal as " + b.Email + "."
			}
			l.toast(a, err)
			l.load(a, true)
		})
	})
}

func (l *limits) startSwitch(a *App, b account.Account) {
	l.switching = b.Key()
	a.Go(func() {
		err := a.Accounts.Switch(context.Background(), b.Key())
		a.Update(func() {
			l.switching = ""
			if err != nil {
				l.toast(a, err)
			} else {
				a.notice = "Switched to " + b.Email + ". Running sessions follow once macOS's keychain cache lapses, or when started again."
			}
			l.load(a, true)
		})
	})
}
