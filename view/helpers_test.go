package view

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/internal/fixture"
	"github.com/lang315/claude-manager/store"
)

// newApp shows the app on a fresh fixture, scanned. Work runs inline, and
// its result lands on the next frame, as Window.Update would land it.
func newApp(t *testing.T) (*App, *ui.Tester, fixture.Dirs, *[]string) {
	t.Helper()
	a, tt, d, trashed := unscannedApp(t)
	a.Rescan()
	tt.Frame()
	return a, tt, d, trashed
}

// unscannedApp shows the app on a fresh fixture before its first Rescan.
func unscannedApp(t *testing.T) (*App, *ui.Tester, fixture.Dirs, *[]string) {
	t.Helper()
	d := fixture.New(t)
	var trashed []string
	a := New(&store.Store{
		Root:       d.Root,
		ClaudeJSON: d.ClaudeJSON,
		Now:        func() time.Time { return fixture.Now },
		Alive:      fixture.Alive,
		Trash: func(p string) error {
			trashed = append(trashed, p)
			return os.RemoveAll(p)
		},
	})
	a.Go = func(f func()) { f() }
	tt := ui.NewTester(a.View, 1200, 800)
	a.Update = tt.Update
	return a, tt, d, &trashed
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
