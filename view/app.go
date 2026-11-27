// Package view draws Claude Manager's screens in MyGo's native UI.
package view

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
	"github.com/lang315/claude-manager/store"
)

// App is the window's state. The view reads and writes it on the main thread.
type App struct {
	Store *store.Store
	// icon is TrayIcon's last PNG, for the shares in iconOf.
	icon   []byte
	iconOf [2]float64
	// Go runs slow work (the disk) off the main thread; tests run it inline.
	Go func(func())
	// Update runs a change on the main thread and redraws: Window.Update in
	// the app, Tester.Update in tests.
	Update func(func())
	// Accounts holds the Limits page's accounts; nil shows the page empty.
	Accounts *account.Book
	// OpenURL and CopyText reach the system: the browser and the clipboard.
	OpenURL  func(string) error
	CopyText func(string)
	// SaveFile and OpenFile ask for a file, showing message: a save panel
	// suggesting name, and an open panel. They return "" when canceled, and
	// block until then, so they run off the main thread.
	SaveFile func(name, message string) (string, error)
	OpenFile func(message string) (string, error)
	// ChooseFolder asks for a folder, as OpenFile does.
	ChooseFolder func(message string) (string, error)
	// Reveal shows a file or folder in the file manager: cswap's `open -R`.
	Reveal func(path string) error
	// KeepAwake keeps the Mac from idle sleep until release is called:
	// mygo.Power.KeepAwake in the app. nil hides Keep Mac Awake.
	KeepAwake func(reason string) (release func())
	// OpenPath opens a file with its default app, where the user edits it.
	OpenPath func(path string) error
	// LogPath is the app's log, which the menu bar opens; "" for none.
	LogPath string
	// Raise brings the window to the front, for the menu bar's Add and
	// Remove; nil in tests.
	Raise func()

	root   string // Store.Root as shown, with ~ for the home directory
	page   string
	busy   int    // loads the user started and moves in flight; not Usage's scan nor quiet reloads, which feed no Trash target
	notice string // shown as a toast on the next frame
	prefs  account.Prefs
	awake  func()  // KeepAwake's release; non-nil while on
	quiet  screens // the screen a quiet reload is starting for; 0 for a load the user started
	live   live    // paces the reloads the watch asks for
	shown  string  // the page live last heard of

	overview overview
	sessions sessions
	history  history
	usage    usage
	heads    store.SessionScan // session heads, kept across Rescans
	config   configPage
	cleanup  cleanup
	limits   limits
}

func New(s *store.Store) *App {
	a := &App{Store: s, Go: func(f func()) { go f() }, page: "overview", root: tilde(s.Root)}
	a.sessions.init()
	a.history.init()
	a.usage.init()
	a.config.init()
	a.cleanup.days = 30
	a.live = live{active: true, visible: pageScreens(a.page), start: a.reload}
	a.shown = a.page
	return a
}

// Rescan reloads every screen from disk.
func (a *App) Rescan() {
	a.overview.load(a)
	a.sessions.load(a)
	a.history.load(a)
	a.usage.load(a)
	a.config.load(a)
	// Each screen adds its load here.
}

func (a *App) View(c *ui.Context) {
	pal := applyTheme(c, a.prefs.Theme)
	if a.page != a.shown { // a page opened: what it missed while hidden loads now
		a.shown = a.page
		a.live.show(pageScreens(a.page))
		if a.page == "limits" {
			a.limits.load(a, false)
		}
	}
	if a.notice != "" {
		c.Toast(a.notice)
		a.notice = ""
	}
	live := a.sessions.live
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Children(func() {
			ui.Sidebar(c, &a.page, func() {
				ui.SidebarItem(c, "overview", iconOverview, "Overview")
				ui.SidebarItem(c, "sessions", iconSessions, "Sessions").Children(func() {
					if live > 0 {
						lamp(c, pal).Tooltip(plural(live, "session") + " running")
					}
				})
				ui.SidebarItem(c, "history", iconHistory, "History")
				ui.SidebarItem(c, "usage", iconUsage, "Usage")
				ui.SidebarItem(c, "limits", iconLimits, "Limits")
				ui.SidebarItem(c, "config", iconConfig, "Config")
				ui.SidebarItem(c, "cleanup", iconCleanup, "Cleanup")
			}).Grow(1)
			if a.KeepAwake != nil {
				on := a.awake != nil
				ui.Checkbox(c, &on, "Keep Mac awake").Margin(12).Tooltip("The display may still turn off. Closing the lid on battery still sleeps.")
				if on != (a.awake != nil) {
					a.setAwake(on)
					a.Update(func() {}) // main redraws the menu bar only in Update
				}
			}
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
			case "usage":
				a.usage.view(c, a, pal)
			case "limits":
				a.limits.view(c, a, pal)
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
	case "usage":
		title, sub = "Usage", a.usage.subtitle(a.Store.Now())
	case "limits":
		title, sub = "Limits", a.limits.subtitle(a.Store.Now())
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
		if a.busy > 0 || a.page == "limits" && a.limits.loading {
			ui.Spinner(c).Label("Scanning")
			ui.Text(c, "Scanning…").TextColor(pal.muted)
		}
		if a.page == "limits" {
			if ui.Button(c, "Add Account…").Disabled(a.Accounts == nil || a.limits.switching != "").Clicked() {
				a.limits.startAdd(a)
			}
			if ui.Button(c, "Add Token…").Disabled(a.Accounts == nil || a.limits.switching != "").Clicked() {
				a.limits.startToken()
			}
			if ui.Button(c, "Refresh").Disabled(a.Accounts == nil || a.limits.loading || a.limits.switching != "").Clicked() {
				a.limits.load(a, true)
			}
			if ui.Button(c, "Export…").Disabled(a.Accounts == nil || a.limits.switching != "").Clicked() {
				a.limits.startExport(a)
			}
			if ui.Button(c, "Import…").Disabled(a.Accounts == nil || a.limits.switching != "").Clicked() {
				a.limits.startImport(a)
			}
		} else if ui.Button(c, "Rescan").Disabled(a.busy > 0).Clicked() {
			a.Rescan()
		}
		if a.page == "overview" && orangeKey(c, pal, "Clean up…", a.cleanup.reclaim == 0).Clicked() {
			a.page = "cleanup"
		}
	})
}

// run does work off the main thread, then applies the change it returns on
// the main thread. A load the user started counts in busy; a quiet reload
// doesn't, and tells live when it lands.
func (a *App) run(work func() (apply func())) {
	q := a.quiet
	if q == 0 {
		a.busy++
	}
	a.Go(func() {
		apply := work()
		a.Update(func() {
			if q == 0 {
				a.busy--
			}
			apply()
			if q != 0 {
				a.live.landed(q)
			}
		})
	})
}

// reload starts the quiet loads of s: they leave busy alone, so Rescan and
// Trash stay enabled. TrashPaths checks each path again as it moves it.
func (a *App) reload(s screens) {
	for _, x := range []struct {
		s    screens
		load func(*App)
	}{
		{overviewScreen, a.overview.load}, {sessionsScreen, a.sessions.load}, {historyScreen, a.history.load},
		{usageScreen, a.usage.load}, {configScreen, a.config.load},
	} {
		if s&x.s != 0 {
			a.quiet = x.s
			x.load(a)
			a.quiet = 0
		}
	}
}

// SetActive tells the app whether it is in front. In the background, the
// reloads the watch asks for wait until it is back.
func (a *App) SetActive(on bool) {
	a.Update(func() {
		if on {
			a.live.activate()
			if a.page == "limits" {
				a.limits.load(a, false)
			}
		} else {
			a.live.resign()
		}
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

// keepOnly drops the keys of s that keep doesn't hold: rows gone since the
// last load. A reload keeps the user's other choices.
func keepOnly[K comparable](s *ui.Selection[K], keep map[K]bool) {
	for _, k := range slices.Collect(s.All()) {
		if !keep[k] {
			s.Remove(k)
		}
	}
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

// Show opens page: "limits" and the others of the sidebar.
func (a *App) Show(page string) { a.page = page }

// tell shows err, if any, as the app's toast.
func (a *App) tell(err error) {
	if err != nil {
		a.notice = err.Error()
	}
}
