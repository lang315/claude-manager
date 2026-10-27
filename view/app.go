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

	page   string
	busy   int    // loads and moves in flight
	notice string // shown as a toast on the next frame

	overview overview
	sessions sessions
	config   configPage
	cleanup  cleanup
}

func New(s *store.Store) *App {
	a := &App{Store: s, Go: func(f func()) { go f() }, page: "overview"}
	a.overview.init()
	a.sessions.init()
	a.config.init()
	a.cleanup.days = 30
	return a
}

// Rescan reloads every screen from disk.
func (a *App) Rescan() {
	a.overview.load(a)
	a.sessions.load(a)
	a.config.load(a)
	// Each screen adds its load here.
}

func (a *App) View(c *ui.Context) {
	if a.notice != "" {
		c.Toast(a.notice)
		a.notice = ""
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Sidebar(c, &a.page, func() {
			ui.SidebarItem(c, "overview", iconOverview, "Overview")
			ui.SidebarItem(c, "sessions", iconSessions, "Sessions")
			ui.SidebarItem(c, "config", iconConfig, "Config")
			ui.SidebarItem(c, "cleanup", iconCleanup, "Cleanup")
		}).Width(180)
		ui.Column(c).Grow(1).PaddingX(20).PaddingY(16).Gap(12).Children(func() {
			a.header(c)
			switch a.page {
			case "overview":
				a.overview.view(c, a)
			case "sessions":
				a.sessions.view(c, a)
			case "config":
				a.config.view(c, a)
			case "cleanup":
				a.cleanup.view(c, a)
				// Each screen adds its case here.
			}
		})
	})
}

// header names the screen, sums up what it shows, and holds Rescan.
func (a *App) header(c *ui.Context) {
	t := c.Theme()
	var title, sub string
	switch a.page {
	case "overview":
		var size int64
		for _, e := range a.overview.rows {
			size += e.Size
		}
		title = "Overview"
		sub = fmt.Sprintf("%s in %s", humanSize(size), tilde(a.Store.Root))
	case "sessions":
		n, size := 0, int64(0)
		for _, p := range a.sessions.projects {
			n += len(p.Sessions)
			size += p.Size
		}
		title = "Sessions"
		sub = fmt.Sprintf("%s in %s · %s", plural(n, "session"), plural(len(a.sessions.projects), "project"), humanSize(size))
	case "config":
		title, sub = "Config", "Read-only. Credentials are masked."
	case "cleanup":
		title, sub = "Cleanup", "Everything moves to the Trash, so you can put it back."
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(20).Bold()
			ui.Text(c, sub).TextColor(t.TextMuted).SingleLine()
		})
		if a.busy > 0 {
			ui.Spinner(c).Label("Scanning")
			ui.Text(c, "Scanning…").TextColor(t.TextMuted)
		}
		if ui.Button(c, "Rescan").Disabled(a.busy > 0).Clicked() {
			a.Rescan()
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
// the chosen ones when none is checked.
func picked[T any, K comparable](s *ui.DataTableState[T, K], rows []T) []*T {
	set := &s.Checked
	if set.Len() == 0 {
		set = &s.Chosen
	}
	var out []*T
	for i := range rows {
		if set.Has(s.Key(&rows[i])) {
			out = append(out, &rows[i])
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
