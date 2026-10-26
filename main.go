// Claude Manager shows what is in ~/.claude and moves what you pick to the
// Trash.
package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
	"github.com/lang315/claude-manager/view"
)

func main() {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	claudeJSON := filepath.Join(root, ".claude.json")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		root, claudeJSON = filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")
	}
	a := view.New(&store.Store{
		Root:       root,
		ClaudeJSON: claudeJSON,
		Trash:      mygo.Shell.TrashItem,
		Now:        time.Now,
		Alive:      store.PIDAlive,
	})
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "Claude Manager",
			Width:   1100,
			Height:  720,
			Content: ui.View(a.View),
		})
		a.Update = win.Update
		a.Rescan()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
