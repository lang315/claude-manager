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
	})
	a.Accounts = accounts(home, configDir, root)
	a.OpenURL, a.CopyText = mygo.Shell.OpenExternal, mygo.Clipboard.WriteText
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "Claude Manager",
			Width:   1100,
			Height:  720,
			Content: ui.View(a.View),
		})
		a.Update = win.Update
		a.Rescan()
		go a.Watch(context.Background(), root, claudeJSON)
		mygo.App.OnDidBecomeActive(func() { a.SetActive(true) })
		mygo.App.OnDidResignActive(func() { a.SetActive(false) })
		a.SetActive(mygo.App.IsActive()) // launched in the background: hold reloads until it is in front
		go func() {
			for range time.Tick(5 * time.Minute) {
				a.LimitsTick()
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// accounts is the Limits page's book, in the app's folder, with logins in
// the login keychain. It switches Claude Code's account only in Claude
// Code's default folder.
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
		Dir:           filepath.Join(home, "Library", "Application Support", "Claude Manager"),
		Vault:         vault,
		API:           account.NewAPI(version),
		Live:          func() (account.Login, error) { return account.ReadLive(vault, configDir, root) },
		Now:           time.Now,
		Claude:        account.FindClaude,
		SignInTimeout: 10 * time.Minute,
		Pitboard:      func() ([]account.Account, error) { return account.ReadPitboard(filepath.Join(home, ".pitboard")) },
		Home:          h,
	}
}
