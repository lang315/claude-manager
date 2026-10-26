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
	ui.Row(c).Fill().Children(func() {
		ui.Sidebar(c, &a.page, func() {
			ui.SidebarItem(c, "overview", nil, "Overview")
			ui.SidebarItem(c, "sessions", nil, "Sessions")
			ui.SidebarItem(c, "config", nil, "Config")
			ui.SidebarItem(c, "cleanup", nil, "Cleanup")
		}).Width(180)
		ui.Column(c).Grow(1).Padding(12).Gap(8).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				if a.busy > 0 {
					ui.Text(c, "Scanning…").TextColor(c.Theme().TextMuted)
				}
				ui.Spacer(c)
				if ui.Button(c, "Rescan").Disabled(a.busy > 0).Clicked() {
					a.Rescan()
				}
			})
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
			a.notice = fmt.Sprintf("Moved %d items to the Trash", moved)
			if err != nil {
				a.notice = fmt.Sprintf("Moved %d items to the Trash, then stopped: %v", moved, err)
			}
			a.Rescan()
		}
	})
}
