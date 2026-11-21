// Claude Manager shows what is in ~/.claude and moves what you pick to the
// Trash.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/updates"
	"github.com/lang315/claude-manager/account"
	"github.com/lang315/claude-manager/cli"
	"github.com/lang315/claude-manager/internal/logfile"
	"github.com/lang315/claude-manager/store"
	"github.com/lang315/claude-manager/view"
)

// version is mygo.json's, for the User-Agent of requests to Anthropic.
const version = "0.1.0"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	root, claudeJSON := configDir, filepath.Join(configDir, ".claude.json")
	if root == "" {
		root, claudeJSON = filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")
	}
	gui := cli.GUI(os.Args[1:])
	if !gui && len(os.Args) > 1 && slices.Contains(cli.Commands, os.Args[1]) {
		cli.Version = version
		cli.Upgrade = upgrade
		st := &store.Store{Root: root, ClaudeJSON: claudeJSON, Alive: store.PIDAlive, Profiles: account.ProfilesDir(appDir(home))}
		cli.Instances = st.Instances
		os.Exit(cli.Run(context.Background(), os.Args[1:], accounts(home, configDir, root), os.Stdin, os.Stdout, os.Stderr))
	}
	a := view.New(&store.Store{
		Root:       root,
		ClaudeJSON: claudeJSON,
		Trash:      mygo.Shell.TrashItem,
		Now:        time.Now,
		Alive:      store.PIDAlive,
		Profiles:   account.ProfilesDir(appDir(home)),
	})
	a.Accounts = accounts(home, configDir, root)
	a.Accounts.Events = func(title, body string) {
		// claude-swap's menu bar notifications, through MyGo's.
		if mygo.NotificationsSupported() {
			mygo.NewNotification(mygo.NotificationOptions{ID: title, Title: title, Body: body, Group: "auto-switch"}).Show()
		}
	}
	if gui && len(os.Args) > 1 && os.Args[1] == "watch" {
		a.Show("limits") // cswap watch: the dashboard on its live page
	}
	a.OpenURL, a.CopyText = mygo.Shell.OpenExternal, mygo.Clipboard.WriteText
	jsonFiles := []mygo.FileFilter{{Name: "JSON", Extensions: []string{"json"}}}
	a.SaveFile = func(name, message string) (string, error) {
		return mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "Export Accounts", DefaultPath: filepath.Join(home, "Desktop", name),
			Message: message, Filters: jsonFiles, CreateDirectories: true})
	}
	a.OpenFile = func(message string) (string, error) {
		// claude-swap's exports go by any name.
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Import Accounts", Message: message, Filters: append(jsonFiles, mygo.FileFilter{Name: "All Files", Extensions: []string{"*"}})})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}
	a.ChooseFolder = func(message string) (string, error) {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Open in Terminal", Message: message, Directory: true, CreateDirectories: true, DefaultPath: home})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "Claude Manager",
			Width:   1100,
			Height:  720,
			Content: ui.View(a.View),
		})
		// The window hides on close, so the menu bar item and auto-switch
		// keep running; Quit still quits.
		quitting := false
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Hide()
			}
		})
		show := func() { win.Show(); win.Focus() }
		mygo.App.OnActivate(func(visible bool) {
			if !visible {
				show()
			}
		})
		tray, err := mygo.NewTray(mygo.TrayOptions{ID: "claude-manager", Title: "–", ToolTip: "Claude Manager"})
		if err != nil {
			log.Print(err)
		}
		a.Update = func(f func()) {
			win.Update(func() {
				f()
				if tray != nil {
					setTray(tray, a, show)
				}
			})
		}
		a.Rescan()
		go a.Watch(context.Background(), root, claudeJSON)
		mygo.App.OnDidBecomeActive(func() { a.SetActive(true) })
		mygo.App.OnDidResignActive(func() { a.SetActive(false) })
		a.SetActive(mygo.App.IsActive()) // launched in the background: hold reloads until it is in front
		a.TrayTick()
		go checkUpdates(a)
		go func() {
			for range time.Tick(5 * time.Minute) {
				a.TrayTick()
			}
		}()
		a.AutoTick()
		go func() {
			for range time.Tick(15 * time.Second) {
				a.AutoTick()
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// accounts is the Limits page's book, in the app's folder, with logins in
// the login keychain.
// newer is a newer version found by the daily check (updates.check).
var newer atomic.Pointer[updates.Update]

// checkUpdates looks for a newer version now and every day after, unless
// the user turned updates.check off or this build doesn't update itself.
func checkUpdates(a *view.App) {
	if !updates.Enabled() {
		return
	}
	for {
		if p, err := a.Accounts.Prefs(); err == nil && !p.NoUpdates {
			if up, err := updates.Check(context.Background()); err == nil && up != nil && newer.Swap(up) == nil {
				a.Update(func() {})
			}
		}
		time.Sleep(24 * time.Hour)
	}
}

// upgrade is claude-manager upgrade: install a newer version, if any.
func upgrade(ctx context.Context) (string, error) {
	if !updates.Enabled() {
		return "", errors.New("This build doesn't update itself: it was built without an update feed, or runs from a development build.")
	}
	up, err := updates.Check(ctx)
	if err != nil {
		return "", err
	}
	if up == nil {
		return "Claude Manager " + version + " is the newest.", nil
	}
	if err := up.Install(ctx, nil); err != nil {
		return "", err
	}
	return "Installed Claude Manager " + up.Version + ": it runs from the next start.", nil
}

// trayWas is what setTray last put on the item: a menu set again while it
// is open would close it.
var trayWas string

// setTray puts the view's menu bar item on tray, with the app's own rows
// after it: open the window, start at login, quit. It changes nothing when
// they are as last set.
func setTray(tray *mygo.Tray, a *view.App, show func()) {
	title, items := a.Tray()
	login := mygo.App.OpenAtLogin()
	was := fmt.Sprint(title, login, newer.Load() != nil)
	for _, it := range items {
		was += fmt.Sprint("|", it.Label, it.Checked, it.Disabled, it.Do != nil)
	}
	if was == trayWas {
		return
	}
	trayWas = was
	var menu []*mygo.MenuItem
	for _, it := range items {
		if it.Label == "" {
			menu = append(menu, mygo.Separator())
			continue
		}
		m := &mygo.MenuItem{Label: it.Label, Disabled: it.Disabled || it.Do == nil}
		if it.Toggle || it.Checked {
			m.Type, m.Checked = mygo.MenuItemCheckbox, it.Checked
		}
		if do := it.Do; do != nil {
			m.Click = func(*mygo.MenuItem, *mygo.Window) { a.Update(do) }
		}
		menu = append(menu, m)
	}
	if up := newer.Load(); up != nil {
		menu = append(menu, mygo.Separator(), &mygo.MenuItem{Label: "Update to " + up.Version + "…", Click: func(*mygo.MenuItem, *mygo.Window) {
			go func() {
				if err := up.Install(context.Background(), nil); err != nil {
					a.Update(func() { log.Print(err) })
					return
				}
				mygo.App.Relaunch()
			}()
		}})
	}
	menu = append(menu, mygo.Separator(),
		&mygo.MenuItem{Label: "Open Claude Manager", Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
		&mygo.MenuItem{Label: "Start at Login", Type: mygo.MenuItemCheckbox, Checked: login, Click: func(*mygo.MenuItem, *mygo.Window) {
			if err := mygo.App.SetOpenAtLogin(!login); err != nil {
				log.Print(err)
			}
			a.Update(func() {})
		}},
		&mygo.MenuItem{Label: "Quit Claude Manager", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	)
	tray.SetTitle(title)
	tray.SetMenu(mygo.NewMenu(menu))
}

// appDir is the app's folder: accounts.json, sign-in folders and profiles.
func appDir(home string) string {
	if runtime.GOOS != "darwin" {
		if d, err := os.UserConfigDir(); err == nil {
			return filepath.Join(d, "Claude Manager") // ~/.config on Linux and WSL, %AppData% on Windows
		}
	}
	return filepath.Join(home, "Library", "Application Support", "Claude Manager")
}

func accounts(home, configDir, root string) *account.Book {
	// Without a macOS keychain Claude Code keeps its login in
	// .credentials.json, and the app keeps its own in a 0600 file.
	fileOnly := runtime.GOOS != "darwin"
	var vault account.Vault = account.Keychain{}
	if k := os.Getenv("CLAUDE_MANAGER_KEYCHAIN"); k != "" {
		// A keychain file of its own, never the user's: for trying the app
		// out, and its end-to-end checks.
		vault = account.Keychain{File: k}
	} else if fileOnly {
		vault = &account.FileVault{Path: filepath.Join(appDir(home), "vault.json")}
	}
	var h *account.Home
	if configDir == "" {
		user := os.Getenv("USER")
		if user == "" {
			user = "claude-code-user" // claude-swap's last fallback, after the OS name
			if u, err := osuser.Current(); err == nil {
				user = u.Username
			}
		}
		h = &account.Home{Root: root, ClaudeJSON: filepath.Join(home, ".claude.json"), User: user, FileOnly: fileOnly}
	}
	return &account.Book{
		Dir:           appDir(home),
		Vault:         vault,
		API:           account.NewAPI(version),
		Live:          func() (account.Login, error) { return account.ReadLive(vault, configDir, root) },
		Now:           time.Now,
		Claude:        account.FindClaude,
		SignInTimeout: 10 * time.Minute,
		Pitboard:      func() ([]account.Account, error) { return account.ReadPitboard(filepath.Join(home, ".pitboard")) },
		Home:          h,
		Launch:        account.LaunchInTerminal,
		Log:           &logfile.File{Path: filepath.Join(appDir(home), "claude-manager.log"), Max: 1 << 20, Keep: 3},
		Running:       account.ClaudeRunning,
	}
}
