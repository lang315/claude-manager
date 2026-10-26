// Package view draws Claude Manager's screens in MyGo's native UI.
package view

import (
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
}

func New(s *store.Store) *App {
	return &App{Store: s, Go: func(f func()) { go f() }, page: "overview"}
}

// Rescan reloads every screen from disk.
func (a *App) Rescan() {
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
			// Each screen adds its case here.
			}
		})
	})
}
