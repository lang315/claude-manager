package view

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestScreenshots writes a PNG of each screen, drawn from the fixture, into
// $SCREENSHOT_DIR, at the test canvas size and at main.go's window size
// (<name>-1100.png). It shows no real data, so the PNGs can go in a PR.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set SCREENSHOT_DIR to write PNGs of each screen")
	}
	a, tt, _, _ := newApp(t)
	small := ui.NewTester(a.View, 1100, 720)
	shots := []struct{ name, click, then string }{
		{"overview", "Overview", ""},
		{"sessions", "Sessions", "/Users/me/proj"},
		{"config", "Config", "env"},
		{"cleanup", "Cleanup", "paste-cache"},
	}
	for _, s := range shots {
		for _, w := range []struct {
			suffix string
			t      *ui.Tester
		}{{"", tt}, {"-1100", small}} {
			a.Update = w.t.Update
			must(t, w.t.Click(s.click))
			if s.then != "" {
				must(t, w.t.Click(s.then))
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s%s.png", s.name, w.suffix)))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, w.t.Image()); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
