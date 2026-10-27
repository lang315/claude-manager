package view

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/internal/fixture"
	"github.com/lang315/claude-manager/store"
)

func TestUsageHeader(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	if want := "$15.49 est. API cost · 3.1M tokens · 39% from cache · 1 call unpriced · 1 unreadable line"; !tt.HasText(want) {
		t.Errorf("header for 30 days; texts %q", tt.Texts())
	}
	for preset, want := range map[int]string{presetToday: "$0.06 est. API cost", preset7Days: "$10.21 est. API cost", presetAll: "$17.49 est. API cost"} {
		a.usage.preset = preset
		tt.Frame()
		if !tt.HasText(want) {
			t.Errorf("preset %d: want %q; texts %q", preset, want, tt.Texts())
		}
	}
}

func TestUsageProjectFilter(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	must(t, tt.Click("me/other"))
	tt.Frame()
	if a.usage.project != "/Users/me/other" || !tt.HasText("$0.07 est. API cost") {
		t.Fatalf("filter %q; texts %q", a.usage.project, tt.Texts())
	}
	must(t, tt.Click("Project: me/other  ✕"))
	tt.Frame()
	if a.usage.project != "" || !tt.HasText("$15.49 est. API cost") {
		t.Errorf("the ✕ leaves the filter %q", a.usage.project)
	}
}

func TestUsageOpensTranscript(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	a.usage.tab = 1 // Sessions: the tab's label is also the sidebar's
	tt.Frame()
	must(t, tt.Click("Login bug fix"))
	must(t, tt.Click("Open Transcript"))
	if a.page != "sessions" || a.sessions.open == nil || a.sessions.open.ID != fixture.Old {
		t.Fatalf("page %q, open %v; want Old's transcript", a.page, a.sessions.open)
	}
}

func TestUsageModels(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	a.usage.tab = 2
	tt.Frame()
	for _, s := range []string{"claude-opus-5-5 · fast", "claude-future-9", "—"} {
		if !tt.HasText(s) {
			t.Errorf("%q not shown; texts %q", s, tt.Texts())
		}
	}
}

func TestUsageEmpty(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, tt.Click("Usage"))
	a.usage.preset, a.usage.project = presetToday, "/Users/me/proj"
	tt.Frame()
	if !tt.HasText("No usage in this period.") {
		t.Errorf("no empty period; texts %q", tt.Texts())
	}
	must(t, os.RemoveAll(filepath.Join(d.Root, "projects")))
	a.Rescan()
	tt.Frame()
	if !tt.HasText("No usage yet.") {
		t.Errorf("no empty state; texts %q", tt.Texts())
	}
}

// heldApp shows the app with its work queued, not run.
func heldApp(t *testing.T) (*App, *ui.Tester, *[]func()) {
	d := fixture.New(t)
	a := New(&store.Store{Root: d.Root, ClaudeJSON: d.ClaudeJSON, Now: func() time.Time { return fixture.Now }, Alive: fixture.Alive})
	var queued []func()
	a.Go = func(f func()) { queued = append(queued, f) }
	tt := ui.NewTester(a.View, 1200, 800)
	a.Update = tt.Update
	return a, tt, &queued
}

func runQueued(q *[]func()) {
	for len(*q) > 0 {
		f := (*q)[0]
		*q = (*q)[1:]
		f()
	}
}

func TestUsageShowsProgress(t *testing.T) {
	a, tt, q := heldApp(t)
	a.Rescan()
	must(t, tt.Click("Usage"))
	if !tt.HasText("Reading transcripts…") {
		t.Errorf("no progress; texts %q", tt.Texts())
	}
	runQueued(q)
	tt.Frame()
	if !tt.HasText("$15.49 est. API cost") {
		t.Errorf("figures after the scan; texts %q", tt.Texts())
	}
}

// A Rescan during a scan waits: one scan at a time, then one more.
func TestUsageRescanDuringScan(t *testing.T) {
	a, tt, q := heldApp(t)
	a.Rescan()
	a.Rescan()
	// A scan's result lands on the next frame, which starts the scan waiting.
	runQueued(q)
	tt.Frame()
	if a.usage.gen != 1 || !a.usage.scanning {
		t.Errorf("after the first scan: gen %d, scanning %v; want 1 scan landed, the second running", a.usage.gen, a.usage.scanning)
	}
	runQueued(q)
	tt.Frame()
	if a.usage.gen != 2 || a.usage.scanning {
		t.Errorf("gen %d, scanning %v; want 2 scans, done", a.usage.gen, a.usage.scanning)
	}
}
