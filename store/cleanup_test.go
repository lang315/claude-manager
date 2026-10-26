package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lang315/claude-manager/internal/fixture"
)

func rel(t *testing.T, root string, items []Item) []string {
	t.Helper()
	var out []string
	for _, it := range items {
		r, err := filepath.Rel(root, it.Path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func session(t *testing.T, projects []Project, id string) Session {
	t.Helper()
	for _, p := range projects {
		for _, x := range p.Sessions {
			if x.ID == id {
				return x
			}
		}
	}
	t.Fatalf("no session %s", id)
	return Session{}
}

func TestSessionPaths(t *testing.T) {
	s, d, _ := newStore(t)
	projects, _ := s.Projects()
	got := s.SessionPaths(session(t, projects, fixture.Old))
	want := []string{
		filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"),
		filepath.Join(d.Root, fixture.Proj, fixture.Old),
		filepath.Join(d.Root, "file-history", fixture.Old),
		filepath.Join(d.Root, "session-env", fixture.Old),
	}
	if !slices.Equal(got, want) {
		t.Errorf("paths %q, want %q", got, want)
	}
	if got := s.SessionPaths(session(t, projects, fixture.Recent)); len(got) != 1 {
		t.Errorf("paths of a session without side data %q, want only its .jsonl", got)
	}
}

func TestCleanupGroups(t *testing.T) {
	s, d, _ := newStore(t)
	projects, _ := s.Projects()
	groups := s.Cleanup(projects, 30)
	want := map[string][]string{
		"session-env":                    {"session-env/" + fixture.Old, "session-env/" + fixture.OrphanEnv},
		"paste-cache":                    {"paste-cache/p1.txt"},
		"shell-snapshots":                {}, // 4242.json has no startedAt: every snapshot may belong to the live session
		"debug":                          {"debug/d.txt"},
		"file-history without a session": {"file-history/.DS_Store", "file-history/" + fixture.OrphanHistory},
		"transcripts older than 30 days": {"transcripts/old.jsonl"},
		"session data without a session": {fixture.Proj + "/" + fixture.OrphanDir},
		"sessions older than 30 days": {
			fixture.Proj + "/" + fixture.Old + ".jsonl",
			fixture.Proj + "/" + fixture.Old,
			"file-history/" + fixture.Old,
			"session-env/" + fixture.Old,
		},
	}
	if len(groups) != len(want) {
		t.Fatalf("%d groups, want %d", len(groups), len(want))
	}
	for _, g := range groups {
		w, ok := want[g.Name]
		if !ok {
			t.Errorf("unexpected group %q", g.Name)
			continue
		}
		if got := rel(t, d.Root, g.Items); !slices.Equal(got, w) {
			t.Errorf("%s: %q, want %q", g.Name, got, w)
		}
		var size int64
		for _, it := range g.Items {
			size += it.Size
		}
		if g.Size != size {
			t.Errorf("%s: size %d, items add up to %d", g.Name, g.Size, size)
		}
		for _, it := range g.Items {
			base := filepath.Base(it.Path)
			if base == fixture.Live || strings.Contains(it.Path, "memory") || strings.HasSuffix(base, ".wakatime") {
				t.Errorf("%s offers %s, which must never be a candidate", g.Name, it.Path)
			}
		}
	}
}

// With no sessions listed (projects/ unreadable, or not loaded yet), nothing
// may count as an orphan: every file-history entry would.
func TestCleanupWithoutSessions(t *testing.T) {
	s, _, _ := newStore(t)
	for _, g := range s.Cleanup(nil, 30) {
		if (g.Name == "file-history without a session" || g.Name == "session data without a session") && len(g.Items) > 0 {
			t.Errorf("%s offers %d items without a session list", g.Name, len(g.Items))
		}
	}
}

func TestTrashPaths(t *testing.T) {
	s, d, trashed := newStore(t)
	in := filepath.Join(d.Root, "paste-cache", "p1.txt")
	moved, err := s.TrashPaths([]string{in})
	if err != nil || moved != 1 || !slices.Equal(*trashed, []string{in}) {
		t.Errorf("moved %d, err %v, trashed %q", moved, err, *trashed)
	}

	for _, bad := range []string{d.Root, filepath.Dir(d.Root), filepath.Join(d.Root, "..", "x"), d.ClaudeJSON} {
		if _, err := s.TrashPaths([]string{bad}); err == nil {
			t.Errorf("TrashPaths(%q) did not refuse", bad)
		}
	}

	s.Trash = func(p string) error {
		if strings.Contains(p, "debug") {
			return errors.New("no trash here")
		}
		return nil
	}
	moved, err = s.TrashPaths([]string{
		filepath.Join(d.Root, "shell-snapshots", "s.sh"),
		filepath.Join(d.Root, "debug", "d.txt"),
		filepath.Join(d.Root, "transcripts", "old.jsonl"),
	})
	if moved != 1 || err == nil || !strings.Contains(err.Error(), "d.txt") {
		t.Errorf("moved %d, err %v: want 1 moved and an error naming d.txt", moved, err)
	}
}

// put writes root-relative files with the given modification time, then sets
// the time on their parent dirs under root too, since writing moves it.
func put(t *testing.T, root string, mod time.Time, rels ...string) {
	t.Helper()
	for _, r := range rels {
		p := filepath.Join(root, r)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		for q := p; q != root; q = filepath.Dir(q) {
			if err := os.Chtimes(q, mod, mod); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func group(gs []Group, name string) Group {
	for _, g := range gs {
		if g.Name == name {
			return g
		}
	}
	return Group{}
}

// A snapshot is written once at session start and never touched again, so
// only its age against the live session's start tells whose it is.
func TestCleanupSnapshotsOfLiveSessions(t *testing.T) {
	s, d, _ := newStore(t)
	start := fixture.Now.Add(-time.Hour)
	put(t, d.Root, start, "sessions/4242.json")
	if err := os.WriteFile(filepath.Join(d.Root, "sessions/4242.json"),
		[]byte(fmt.Sprintf(`{"pid":4242,"sessionId":%q,"startedAt":%d}`, fixture.Live, start.UnixMilli())), 0o644); err != nil {
		t.Fatal(err)
	}
	put(t, d.Root, start.Add(-time.Hour), "shell-snapshots/before.sh")
	put(t, d.Root, start.Add(time.Minute), "shell-snapshots/during.sh")
	got := rel(t, d.Root, group(s.Cleanup(nil, 30), "shell-snapshots").Items)
	if want := []string{"shell-snapshots/before.sh", "shell-snapshots/s.sh"}; !slices.Equal(got, want) {
		t.Errorf("snapshots %q, want %q", got, want)
	}

	// Without a live session every old snapshot is a leftover.
	s.Alive = func(int) bool { return false }
	got = rel(t, d.Root, group(s.Cleanup(nil, 30), "shell-snapshots").Items)
	if want := []string{"shell-snapshots/before.sh", "shell-snapshots/during.sh", "shell-snapshots/s.sh"}; !slices.Equal(got, want) {
		t.Errorf("snapshots without live sessions %q, want %q", got, want)
	}
}

// Recent is live by its .jsonl alone (no sessions/<pid>.json); its leftovers
// have old mtimes, and debug/<id>.txt is named with an extension.
func TestCleanupSkipsRecentSessionData(t *testing.T) {
	s, d, _ := newStore(t)
	old := fixture.Now.AddDate(0, 0, -60)
	put(t, d.Root, old,
		"session-env/"+fixture.Recent+"/env",
		"file-history/"+fixture.Recent+"/v1",
		"debug/"+fixture.Recent+".txt",
		"debug/"+fixture.Live+".txt")
	projects, _ := s.Projects()
	for _, g := range s.Cleanup(projects, 30) {
		for _, it := range g.Items {
			if b := filepath.Base(it.Path); strings.Contains(b, fixture.Recent) || strings.Contains(b, fixture.Live) {
				t.Errorf("%s offers %s of a live session", g.Name, it.Path)
			}
		}
	}
}

func TestTrashPathsOverlapMissingAndLive(t *testing.T) {
	s, d, trashed := newStore(t)
	env := filepath.Join(d.Root, "session-env", fixture.Old)
	hist := filepath.Join(d.Root, "file-history", fixture.Old)
	moved, err := s.TrashPaths([]string{env, hist, env, filepath.Join(d.Root, "debug", "gone.txt")})
	if err != nil || moved != 2 || !slices.Equal(*trashed, []string{env, hist}) {
		t.Errorf("moved %d, err %v, trashed %q: want the 2 existing paths once each", moved, err, *trashed)
	}
	// Already gone since the first call.
	if moved, err := s.TrashPaths([]string{env}); err != nil || moved != 0 {
		t.Errorf("moved %d, err %v for a path that is gone", moved, err)
	}

	*trashed = nil
	live := filepath.Join(d.Root, "session-env", fixture.Live)
	liveDebug := filepath.Join(d.Root, "debug", fixture.Live+".txt")
	put(t, d.Root, fixture.Now.AddDate(0, 0, -60), "debug/"+fixture.Live+".txt")
	if moved, err := s.TrashPaths([]string{live, liveDebug}); err != nil || moved != 0 || len(*trashed) != 0 {
		t.Errorf("moved %d, err %v, trashed %q: live session must not be trashed", moved, err, *trashed)
	}
}
