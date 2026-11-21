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
		{"usage", "Usage", nil},
		{"usage-today", "Usage", []string{"Today"}},
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

// TestLimitsScreenshot writes the Limits page, on the fake Anthropic of
// limitsApp, light and dark at main.go's window size, into $SCREENSHOT_DIR.
func TestLimitsScreenshot(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set SCREENSHOT_DIR to write PNGs of the Limits page")
	}
	for _, dark := range []bool{false, true} {
		l := limitsApp(t)
		seedBob(t, l, "bob")
		tt := ui.NewTester(l.a.View, 1100, 720)
		tt.SetDark(dark)
		l.a.Update, l.tt = tt.Update, tt
		l.open(t)
		waitFor(t, tt, func() bool { return tt.HasText("bob@example.com") })
		name := "limits.png"
		if dark {
			name = "limits-dark.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	l := limitsApp(t)
	h := canSwitch(t, l)
	tt := ui.NewTester(l.a.View, 1100, 720)
	l.a.Update, l.tt = tt.Update, tt
	l.open(t)
	waitFor(t, tt, func() bool { return tt.HasText("bob@example.com") })
	l.a.Go = func(f func()) { go f() }
	held := h.ClaudeJSON + ".lock"
	must(t, os.Mkdir(held, 0o755))
	defer os.Remove(held)
	must(t, tt.Click("⋯"))
	must(t, tt.ChooseMenuItem("Switch to This Account"))
	waitFor(t, tt, func() bool { return tt.HasText("Switching…") })
	f, err := os.Create(filepath.Join(dir, "limits-switching.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	f.Close()
}
