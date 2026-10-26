package view

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestCleanupTrashesCheckedGroups(t *testing.T) {
	_, tt, d, trashed := newApp(t)
	must(t, tt.Click("Cleanup"))
	must(t, tt.Click("paste-cache"))
	must(t, tt.Click("debug"))
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("Move 2 items to the Trash?") {
		t.Fatalf("no confirmation; texts %q", tt.Texts())
	}
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	want := []string{filepath.Join(d.Root, "paste-cache", "p1.txt"), filepath.Join(d.Root, "debug", "d.txt")}
	if !slices.Equal(*trashed, want) {
		t.Errorf("trashed %q, want %q", *trashed, want)
	}
}

// The view must never touch the disk on the main thread: until the work
// handed to App.Go runs, nothing is trashed.
func TestCleanupTrashesOffTheMainThread(t *testing.T) {
	a, tt, _, trashed := newApp(t)
	var queued []func()
	a.Go = func(f func()) { queued = append(queued, f) }
	must(t, tt.Click("Cleanup"))
	must(t, tt.Click("paste-cache"))
	must(t, tt.Click("Move to Trash…"))
	must(t, tt.Click("Move to Trash"))
	if len(*trashed) != 0 {
		t.Fatalf("trashed on the main thread: %q", *trashed)
	}
	if len(queued) != 1 {
		t.Fatalf("%d jobs queued, want 1", len(queued))
	}
	queued[0]()
	if len(*trashed) != 1 {
		t.Errorf("trashed %q after the job ran, want 1 path", *trashed)
	}
}

func TestCleanupDays(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Cleanup"))
	if !tt.HasText("transcripts older than 30 days") {
		t.Fatalf("texts %q", tt.Texts())
	}
	a.cleanup.days = 1
	a.cleanup.load(a)
	tt.Frame()
	if !tt.HasText("transcripts older than 1 days") {
		t.Errorf("days not applied; texts %q", tt.Texts())
	}
	for _, g := range a.cleanup.groups {
		if g.Name == "transcripts older than 1 days" && len(g.Items) != 2 {
			t.Errorf("%d transcripts older than 1 day, want 2", len(g.Items))
		}
	}
}

// Groups overlap: the confirmation counts each path once, and the trash gets
// each path once.
func TestCleanupCountsOverlappingPathsOnce(t *testing.T) {
	a, tt, _, trashed := newApp(t)
	must(t, tt.Click("Cleanup"))
	must(t, tt.Click("session-env"))
	must(t, tt.Click("sessions older than 30 days"))
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("Move 5 items to the Trash?") {
		t.Fatalf("overlap double-counted; texts %q", tt.Texts())
	}
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	if len(*trashed) != 5 {
		t.Errorf("trashed %d paths, want 5: %q", len(*trashed), *trashed)
	}
	seen := map[string]bool{}
	for _, p := range *trashed {
		if seen[p] {
			t.Errorf("%q trashed twice", p)
		}
		seen[p] = true
	}
	if slices.Contains(a.cleanup.on, true) {
		t.Errorf("groups stay checked after the trash: %v", a.cleanup.on)
	}
}

// Loads can finish out of order; the last one started wins.
func TestCleanupStaleLoadIgnored(t *testing.T) {
	a, tt, _, _ := newApp(t)
	var queued []func()
	a.Go = func(f func()) { queued = append(queued, f) }
	must(t, tt.Click("Cleanup"))
	a.cleanup.days = 5
	a.cleanup.load(a)
	a.cleanup.days = 6
	a.cleanup.load(a)
	for _, f := range slices.Backward(queued) {
		f()
	}
	tt.Frame()
	if !tt.HasText("transcripts older than 6 days") || tt.HasText("transcripts older than 5 days") {
		t.Errorf("stale load applied; texts %q", tt.Texts())
	}
}
