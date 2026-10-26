package view

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestScreenshots writes a PNG of each screen, drawn from the fixture, into
// $SCREENSHOT_DIR. It shows no real data, so the PNGs can go in a PR.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set SCREENSHOT_DIR to write PNGs of each screen")
	}
	_, tt, _, _ := newApp(t)
	shots := []struct{ name, click, then string }{
		{"overview", "Overview", ""},
		{"sessions", "Sessions", "/Users/me/proj"},
		{"config", "Config", "env"},
		{"cleanup", "Cleanup", "paste-cache"},
	}
	for _, s := range shots {
		must(t, tt.Click(s.click))
		if s.then != "" {
			must(t, tt.Click(s.then))
		}
		f, err := os.Create(filepath.Join(dir, s.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
