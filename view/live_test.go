package view

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/lang315/claude-manager/internal/fixture"
)

func TestScreensFor(t *testing.T) {
	for path, want := range map[string]screens{
		".":                       allScreens,
		"projects":                sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj": sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj/55555555-5555-4555-8555-555555555555/subagents": sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj/memory":                                         sessionsScreen | usageScreen | overviewScreen | configScreen,
		"sessions":                                                               sessionsScreen,
		"plugins/marketplaces/x":                                                 configScreen | overviewScreen,
		"skills/demo":                                                            configScreen | overviewScreen,
		"commands":                                                               configScreen | overviewScreen,
		"agents":                                                                 configScreen | overviewScreen,
		"file-history/abc":                                                       sessionsScreen | overviewScreen,
		"debug":                                                                  sessionsScreen | overviewScreen,
	} {
		if got := screensFor(path); got != want {
			t.Errorf("screensFor(%q) = %05b, want %05b", path, got, want)
		}
	}
}

// newLive is a live whose starts are recorded, not run.
func newLive() (*live, *[]screens) {
	var started []screens
	return &live{active: true, start: func(s screens) { started = append(started, s) }}, &started
}

func TestLiveHintStarts(t *testing.T) {
	l, started := newLive()
	l.hint(sessionsScreen | usageScreen)
	if len(*started) != 1 || (*started)[0] != sessionsScreen|usageScreen || l.inflight != sessionsScreen|usageScreen {
		t.Errorf("started %v, inflight %05b", *started, l.inflight)
	}
}

// A hint for a screen that is loading waits for it to land, then runs once.
func TestLiveHintWhileLoading(t *testing.T) {
	l, started := newLive()
	l.hint(sessionsScreen)
	l.hint(sessionsScreen | historyScreen)
	l.hint(allScreens)
	if len(*started) != 3 || (*started)[1] != historyScreen || (*started)[2] != overviewScreen|usageScreen|configScreen {
		t.Fatalf("started %v: a loading screen started twice", *started)
	}
	l.landed(sessionsScreen)
	if len(*started) != 4 || (*started)[3] != sessionsScreen {
		t.Errorf("after it landed, started %v; want sessions once more", *started)
	}
	l.landed(sessionsScreen)
	if len(*started) != 4 {
		t.Errorf("a second landing started %v", *started)
	}
}

// In the background, hints pile up and run once when the app is back.
func TestLiveHoldsInBackground(t *testing.T) {
	l, started := newLive()
	l.resign()
	for range 10 {
		l.hint(sessionsScreen)
		l.hint(configScreen)
	}
	if len(*started) != 0 {
		t.Fatalf("started %v in the background", *started)
	}
	l.activate()
	if len(*started) != 1 || (*started)[0] != sessionsScreen|configScreen {
		t.Errorf("on activation, started %v; want sessions and config once", *started)
	}
}

// A quiet reload leaves busy alone and tells live when it lands.
func TestQuietReloadLeavesBusy(t *testing.T) {
	a, tt, q := heldApp(t)
	a.live.hint(sessionsScreen | historyScreen | configScreen | overviewScreen)
	if a.busy != 0 || a.live.inflight == 0 {
		t.Fatalf("busy %d, inflight %05b; want 0 and the screens loading", a.busy, a.live.inflight)
	}
	runQueued(q)
	tt.Frame()
	if a.busy != 0 || a.live.inflight != 0 {
		t.Errorf("after landing: busy %d, inflight %05b; want 0, 0", a.busy, a.live.inflight)
	}
}

// A Rescan during a quiet reload is still loud, and busy ends at 0.
func TestQuietAndLoudReloadsCount(t *testing.T) {
	a, tt, q := heldApp(t)
	a.live.hint(sessionsScreen)
	a.Rescan()
	if a.busy == 0 {
		t.Fatal("a Rescan during a quiet reload isn't counted")
	}
	runQueued(q)
	tt.Frame()
	runQueued(q)
	tt.Frame()
	if a.busy != 0 || a.live.inflight != 0 {
		t.Errorf("busy %d, inflight %05b; want 0, 0", a.busy, a.live.inflight)
	}
}

// A quiet Usage reload folded into a scan already running still lands.
func TestQuietUsageLandsDuringScan(t *testing.T) {
	a, tt, q := heldApp(t)
	a.Rescan() // a loud scan runs
	a.live.hint(usageScreen)
	for range 3 {
		runQueued(q)
		tt.Frame()
	}
	if a.live.inflight&usageScreen != 0 {
		t.Error("the quiet Usage reload never landed")
	}
}

// In the background, SetActive(false) holds hints until SetActive(true).
func TestSetActive(t *testing.T) {
	a, tt, q := heldApp(t)
	a.SetActive(false)
	tt.Frame()
	a.live.hint(historyScreen)
	if len(*q) != 0 {
		t.Fatal("a hint in the background started a load")
	}
	a.SetActive(true)
	tt.Frame()
	if len(*q) == 0 {
		t.Error("activation didn't start the held reload")
	}
}

// fakeHints hands out batches, then ends with err (io.EOF when nil).
type fakeHints struct {
	batches [][]mygo.FileEvent
	err     error
}

func (f *fakeHints) Next(context.Context) ([]mygo.FileEvent, error) {
	if len(f.batches) == 0 {
		return nil, io.EOF
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, nil
}

func (f *fakeHints) Err() error { return f.err }

func TestFollowHintsScreens(t *testing.T) {
	a, tt, q := heldApp(t)
	a.follow(context.Background(), &fakeHints{batches: [][]mygo.FileEvent{
		{{Op: mygo.FileHint, Path: "projects/-Users-me-proj"}},
	}}, screensFor)
	tt.Frame()
	if a.live.inflight != sessionsScreen|usageScreen|overviewScreen || len(*q) == 0 {
		t.Errorf("inflight %05b, %d queued", a.live.inflight, len(*q))
	}
}

func TestFollowEndsWithToast(t *testing.T) {
	a, _, _ := heldApp(t)
	a.Update = func(f func()) { f() }
	a.follow(context.Background(), &fakeHints{err: errors.New("mygo: file watch entry limit exceeded")}, screensFor)
	if a.notice != "Live refresh is off: mygo: file watch entry limit exceeded" {
		t.Errorf("notice %q", a.notice)
	}
	a.notice = ""
	a.follow(context.Background(), &fakeHints{}, screensFor) // closed, no error
	if a.notice != "" {
		t.Errorf("a clean end toasts %q", a.notice)
	}
}

// The real watch: a new session written under projects/ shows in Sessions.
func TestWatchFindsNewSession(t *testing.T) {
	a, tt, d, _ := newApp(t)
	before := a.sessions.count
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Watch(ctx, d.Root, d.ClaudeJSON)
	file := filepath.Join(d.Root, fixture.Other, "77777777-7777-4777-8777-777777777777.jsonl")
	must(t, os.WriteFile(file, []byte(`{"type":"user","cwd":"/Users/me/other","message":{"role":"user","content":"New"}}`+"\n"), 0o644))
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		tt.Frame()
		if a.sessions.count == before+1 {
			return
		}
	}
	t.Errorf("sessions %d, want %d within 3 s", a.sessions.count, before+1)
}
