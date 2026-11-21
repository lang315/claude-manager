// Claude Manager shows what is in ~/.claude and moves what you pick to the
// Trash.
package main

import (
	"context"
	"log"
	"os"
	osuser "os/user"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
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
	a := view.New(&store.Store{
		Root:       root,
		ClaudeJSON: claudeJSON,
		Trash:      mygo.Shell.TrashItem,
		Now:        time.Now,
		Alive:      store.PIDAlive,
		Profiles:   account.ProfilesDir(appDir(home)),
	})
	a.Accounts = accounts(home, configDir, root)
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
		go func() {
			for range time.Tick(5 * time.Minute) {
				a.TrayTick()
			}
		}()
		a.AutoTick()
		go func() {
			for range time.Tick(time.Minute) {
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
// setTray puts the view's menu bar item on tray, with the app's own rows
// after it: open the window, start at login, quit.
func setTray(tray *mygo.Tray, a *view.App, show func()) {
	title, items := a.Tray()
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
	login := mygo.App.OpenAtLogin()
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
	return filepath.Join(home, "Library", "Application Support", "Claude Manager")
}

func accounts(home, configDir, root string) *account.Book {
	vault := account.Keychain{}
	var h *account.Home
	if configDir == "" {
		user := os.Getenv("USER")
		if user == "" {
			user = "claude-code-user" // claude-swap's last fallback, after the OS name
			if u, err := osuser.Current(); err == nil {
				user = u.Username
			}
		}
		h = &account.Home{Root: root, ClaudeJSON: filepath.Join(home, ".claude.json"), User: user}
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
		Running:       account.ClaudeRunning,
	}
}
