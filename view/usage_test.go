package view

import (
	"image"
	"os"
	"path/filepath"
	"slices"
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

func TestBars(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	byDay := map[string]float64{"2026-10-20": 6.74, "2026-10-21": 3.4, "2026-10-26": 0.058}
	got := bars(byDay, day(20), day(26), false)
	if want := []float64{6.74, 3.4, 0, 0, 0, 0, 0.058}; !slices.Equal(got, want) {
		t.Errorf("daily %v, want %v", got, want)
	}
	// 2026-10-19 and 10-26 are Mondays: from the 18th, the weeks start on
	// the 12th, the 19th and the 26th.
	got = bars(byDay, day(18), day(26), true)
	if len(got) != 3 || got[0] != 0 || got[1] != 6.74+3.4 || got[2] != 0.058 {
		t.Errorf("weekly %v, want [0 10.14 0.058]", got)
	}
}

func TestDollarStep(t *testing.T) {
	for most, want := range map[float64]float64{0.05: 0.02, 3.4: 1, 6.74: 2, 15.5: 5, 140: 50} {
		if got := dollarStep(most); got != want {
			t.Errorf("dollarStep(%v) = %v, want %v", most, got, want)
		}
	}
}

func TestUsageChartCaption(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	if !tt.HasText("COST BY DAY") || !tt.HasText("$15.49 over 30 days · prices of "+store.PricesAsOf) {
		t.Errorf("chart caption; texts %q", tt.Texts())
	}
	a.usage.buckets = append(a.usage.buckets, store.Usage{SessionID: fixture.Old, Project: "/Users/me/proj",
		Day: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Model: "claude-opus-5-5", Calls: 1, Cost: 1})
	a.usage.gen++
	a.usage.preset = presetAll
	tt.Frame()
	if !tt.HasText("COST BY WEEK") {
		t.Errorf("over 90 days, no weekly bars; texts %q", tt.Texts())
	}
}

// The usage scan is long and feeds no Trash target: it must not hold the
// app busy, which disables Rescan and every Move to Trash.
func TestUsageScanLeavesAppFree(t *testing.T) {
	a, _, q := heldApp(t)
	a.usage.load(a)
	if a.busy != 0 || len(*q) != 1 {
		t.Errorf("busy %d, %d queued; want 0 busy, the scan queued", a.busy, len(*q))
	}
}

// inkRun is the widest run of dark pixels on the rows around y, from x0 to x1.
func inkRun(img *image.RGBA, x0, x1, y int) int {
	best := 0
	for yy := y - 4; yy <= y+4; yy++ {
		run := 0
		for x := x0; x < x1; x++ {
			if r, g, b, _ := img.At(x, yy).RGBA(); r>>8 < 90 && g>>8 < 90 && b>>8 < 90 {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
	}
	return best
}

// Share is of the period's cost: two projects at the same cost fill half
// the column each, not all of it.
func TestUsageShareOfPeriod(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	day := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	a.usage.buckets = []store.Usage{
		{SessionID: fixture.Old, Project: "/p/alpha", Day: day, Model: "claude-opus-5-5", Calls: 1, Cost: 1},
		{SessionID: fixture.Recent, Project: "/p/beta", Day: day, Model: "claude-opus-5-5", Calls: 1, Cost: 1},
	}
	a.usage.gen++
	tt.Frame()
	head, ok := tt.Find("Share")
	row, ok2 := tt.Find("p/alpha")
	if !ok || !ok2 {
		t.Fatalf("no Share column or row; texts %q", tt.Texts())
	}
	img := tt.Image()
	k := float32(img.Bounds().Dx()) / 1200
	x0, x1 := int((head.X-4)*k), int((head.X+150)*k)
	full := inkRun(img, x0, x1, int((row.Y+row.H/2)*k))
	if want := 140 * k / 2; float32(full) < want-8*k || float32(full) > want+8*k {
		t.Errorf("alpha's bar is %d px, want about %.0f (half of 140)", full, want)
	}
}
