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
	a.Go(func() {
		saved, err := a.Accounts.Blocks()
		a.Update(func() {
			if !l.loaded {
				l.blocks = saved
			}
			l.toast(a, err)
		})
		blocks, err := a.Accounts.Refresh(context.Background(), force)
		a.Update(func() {
			l.loading, l.loaded = false, true
			l.blocks, l.updated = blocks, a.Store.Now()
			l.toast(a, err)
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
				for _, b := range l.blocks {
					l.block(c, a, pal, b, now)
				}
			})
		})
	}
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
			ui.Spacer(c)
		})
		switch {
		case len(b.Limits) == 0 && b.Err != "":
			ui.Text(c, b.Err).TextColor(pal.muted).Wrap()
		case len(b.Limits) == 0:
			ui.Text(c, "No figures yet.").TextColor(pal.muted)
		}
		for _, x := range b.Limits {
			limitRow(c, pal, x, now)
		}
		if len(b.Limits) > 0 && b.Err != "" {
			ui.Text(c, b.Err+" Showing numbers from "+clock(b.ReadAt, now)+".").TextColor(pal.muted).Wrap()
		}
		if b.Added && !b.GoodUntil.IsZero() {
			ui.Text(c, goodUntil(b.GoodUntil, now)).FontSize(12).TextColor(pal.muted)
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
		if x.Percent >= 100 {
			ui.Text(c, "Limit reached").Bold().SingleLine()
		}
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

// startAdd opens the Add Account dialog. Task 9 builds it.
func (l *limits) startAdd(a *App) {}
