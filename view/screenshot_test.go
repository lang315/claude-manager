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
// (<name>-1100.png), and dark at that size (<name>-dark.png). It shows no real data, so the PNGs can go in a PR.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set SCREENSHOT_DIR to write PNGs of each screen")
	}
	a, tt, _, _ := newApp(t)
	small := ui.NewTester(a.View, 1100, 720)
	dark := ui.NewTester(a.View, 1100, 720)
	dark.SetDark(true)
	shots := []struct {
		name, click string
		then        []string
	}{
		{"overview", "Overview", nil},
		{"sessions", "Sessions", []string{"me/proj"}},
		{"transcript", "Sessions", []string{"me/proj", "Login bug fix", "Open Transcript"}},
		{"config", "Back", []string{"Config", "Settings", "env"}},
		{"config-hooks", "Hooks", nil},
		{"config-mcp", "MCP servers", []string{"remote"}},
		{"config-plugins", "Plugins", nil},
		{"config-skills", "Skills", nil},
		{"config-memory", "Memory", []string{"MEMORY.md"}},
		{"config-backups", "Backups", []string{"settings.json.bak-1"}},
		{"cleanup", "Cleanup", []string{"paste-cache"}},
		{"history", "History", []string{"Review this:"}},
	}
	// One tester at a time: they share the app, and the transcript shot
	// leaves it open until the next shot's Back.
	for _, w := range []struct {
		suffix string
		t      *ui.Tester
	}{{"", tt}, {"-1100", small}, {"-dark", dark}} {
		for _, s := range shots {
			a.Update = w.t.Update
			must(t, w.t.Click(s.click))
			for _, then := range s.then {
				must(t, w.t.Click(then))
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
