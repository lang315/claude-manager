// Package view draws Claude Manager's screens in MyGo's native UI.
package view

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

// App is the window's state. The view reads and writes it on the main thread.
type App struct {
	Store *store.Store
	// Go runs slow work (the disk) off the main thread; tests run it inline.
	Go func(func())
	// Update runs a change on the main thread and redraws: Window.Update in
	// the app, Tester.Update in tests.
	Update func(func())

	root   string // Store.Root as shown, with ~ for the home directory
	page   string
	busy   int    // loads and moves in flight
	notice string // shown as a toast on the next frame

	overview overview
	sessions sessions
	history  history
	config   configPage
	cleanup  cleanup
}

func New(s *store.Store) *App {
	a := &App{Store: s, Go: func(f func()) { go f() }, page: "overview", root: tilde(s.Root)}
	a.sessions.init()
	a.history.init()
	a.config.init()
	a.cleanup.days = 30
	return a
}

// Rescan reloads every screen from disk.
func (a *App) Rescan() {
	a.overview.load(a)
	a.sessions.load(a)
	a.history.load(a)
	a.config.load(a)
	// Each screen adds its load here.
}

func (a *App) View(c *ui.Context) {
	pal := applyTheme(c)
	if a.notice != "" {
		c.Toast(a.notice)
		a.notice = ""
	}
	live := a.sessions.live
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Sidebar(c, &a.page, func() {
			ui.SidebarItem(c, "overview", iconOverview, "Overview")
			ui.SidebarItem(c, "sessions", iconSessions, "Sessions").Children(func() {
				if live > 0 {
					lamp(c, pal).Tooltip(plural(live, "session") + " running")
				}
			})
			ui.SidebarItem(c, "history", iconHistory, "History")
			ui.SidebarItem(c, "config", iconConfig, "Config")
			ui.SidebarItem(c, "cleanup", iconCleanup, "Cleanup")
		}).Width(200).Background(pal.ground)
		ui.Divider(c)
		ui.Column(c).Grow(1).MinWidth(0).PaddingX(24).PaddingY(18).Gap(16).Children(func() {
			a.header(c, pal, live)
			switch a.page {
			case "overview":
				a.overview.view(c, a, pal)
			case "sessions":
				a.sessions.view(c, a, pal)
			case "history":
				a.history.view(c, a, pal)
			case "config":
				a.config.view(c, a, pal)
			case "cleanup":
				a.cleanup.view(c, a, pal)
				// Each screen adds its case here.
			}
		})
	})
	a.cleanup.confirm(c, a)
}

// header names the screen, sums up what it shows, and holds Rescan; the
// Overview's adds the key to clean up.
func (a *App) header(c *ui.Context, pal *palette, live int) {
	var title, sub string
	switch a.page {
	case "overview":
		running := "no session running"
		if live > 0 {
			running = plural(live, "session") + " running"
		}
		title = a.root
		sub = humanSize(a.overview.total) + " · " + running
	case "sessions":
		s := &a.sessions
		title = "Sessions"
		sub = fmt.Sprintf("%s in %s · %s", plural(s.count, "session"), plural(len(s.projects), "project"), humanSize(s.size))
	case "history":
		h := &a.history
		title = "History"
		sub = fmt.Sprintf("%s in %s", plural(len(h.prompts), "prompt"), plural(len(h.projects)-1, "project"))
		if h.bad > 0 {
			sub += " · " + plural(h.bad, "unreadable line")
		}
	case "config":
		title, sub = "Config", "Read-only. Credentials are masked."
	case "cleanup":
		title, sub = "Cleanup", "Everything moves to the Trash, so you can put it back."
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(24).Bold().SingleLine()
			ui.Text(c, sub).TextColor(pal.muted).SingleLine()
		})
		if a.busy > 0 {
			ui.Spinner(c).Label("Scanning")
			ui.Text(c, "Scanning…").TextColor(pal.muted)
		}
		if ui.Button(c, "Rescan").Disabled(a.busy > 0).Clicked() {
			a.Rescan()
		}
		if a.page == "overview" && orangeKey(c, pal, "Clean up…", a.cleanup.reclaim == 0).Clicked() {
			a.page = "cleanup"
		}
	})
}

// run does work off the main thread, then applies the change it returns on
// the main thread.
func (a *App) run(work func() (apply func())) {
	a.busy++
	a.Go(func() {
		apply := work()
		a.Update(func() {
			a.busy--
			apply()
		})
	})
}

// picked returns the rows a table's actions apply to: the checked rows, or
// the chosen ones when none is checked, of those shown. A row the search
// hides is left out: the user can't see it.
func picked[T any, K comparable](s *ui.DataTableState[T, K]) []*T {
	set := &s.Checked
	if set.Len() == 0 {
		set = &s.Chosen
	}
	var out []*T
	for i := range s.Len() {
		if x := s.At(i); set.Has(s.Key(x)) {
			out = append(out, x)
		}
	}
	return out
}

// trash moves the paths that paths returns to the Trash, off the main
// thread, then reports and rescans.
func (a *App) trash(paths func() []string) {
	a.run(func() func() {
		moved, err := a.Store.TrashPaths(paths())
		return func() {
			a.notice = fmt.Sprintf("Moved %s to the Trash", plural(moved, "item"))
			if err != nil {
				a.notice = fmt.Sprintf("Moved %s to the Trash, then stopped: %v", plural(moved, "item"), err)
			}
			a.Rescan()
		}
	})
}
