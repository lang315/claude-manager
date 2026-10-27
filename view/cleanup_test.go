package view

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
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

// A session that started after the last scan is no orphan when the days
// change, and is judged by its own age.
func TestCleanupDaysSeesSessionsStartedSinceTheScan(t *testing.T) {
	a, tt, d, _ := newApp(t)
	id := "77777777-7777-4777-8777-777777777777"
	old := fixture.Now.AddDate(0, 0, -2)
	for rel, c := range map[string]string{
		fixture.Proj + "/" + id + ".jsonl": `{"type":"user","message":{"content":"hi"}}` + "\n",
		"file-history/" + id + "/v1":       "edit",
	} {
		p := filepath.Join(d.Root, rel)
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(c), 0o644))
	}
	for _, rel := range []string{fixture.Proj + "/" + id + ".jsonl", "file-history/" + id} {
		must(t, os.Chtimes(filepath.Join(d.Root, rel), old, old))
	}
	must(t, tt.Click("Cleanup"))
	a.cleanup.days = 1
	a.cleanup.load(a)
	tt.Frame()
	found := false
	for _, g := range a.cleanup.groups {
		for _, it := range g.Items {
			if filepath.Base(it.Path) == id && g.Name != "sessions older than 1 days" {
				t.Errorf("session started after the scan offered in %q: %s", g.Name, it.Path)
			}
			if filepath.Base(it.Path) == id+".jsonl" {
				found = true
			}
		}
	}
	if !found {
		t.Error("a session 2 days old, started after the scan, is not offered for days = 1")
	}
}

func TestMoveToTrashDisabledWhileBusy(t *testing.T) {
	a, tt, _, trashed := newApp(t)
	must(t, tt.Click("Cleanup"))
	must(t, tt.Click("paste-cache"))
	a.busy = 1
	tt.Frame()
	tt.Click("Move to Trash…") // disabled: a click does nothing
	if tt.HasText("Move 1 item to the Trash?") {
		t.Errorf("confirmation opens while busy; texts %q", tt.Texts())
	}
	a.busy = 0
	tt.Frame()
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("Move 1 item to the Trash?") || len(*trashed) != 0 {
		t.Errorf("no confirmation once idle; texts %q", tt.Texts())
	}
}

// groupNotes are matched to groups by position.
func TestGroupNotesMatchGroups(t *testing.T) {
	a, _, _, _ := newApp(t)
	if len(groupNotes) != len(a.cleanup.groups) {
		t.Errorf("%d group notes for %d groups", len(groupNotes), len(a.cleanup.groups))
	}
}
