package account

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(p), 0o700))
	must(t, os.WriteFile(p, []byte(s), 0o600))
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestMergeHistory(t *testing.T) {
	root, prof, conf := t.TempDir(), t.TempDir(), t.TempDir()
	src, dest := filepath.Join(root, "projects"), filepath.Join(prof, "projects")
	write(t, filepath.Join(src, "p", "same.jsonl"), "a\nb\n")
	write(t, filepath.Join(dest, "p", "same.jsonl"), "a\n") // the shared copy continues it
	write(t, filepath.Join(src, "p", "clash.jsonl"), "x\n")
	write(t, filepath.Join(dest, "p", "clash.jsonl"), "y\n") // diverged
	write(t, filepath.Join(dest, "q", "new.jsonl"), "n\n")   // only the profile's
	moved, parked, err := mergeHistory(src, dest, conf)
	must(t, err)
	if moved != 1 || parked != 1 {
		t.Errorf("moved %d parked %d", moved, parked)
	}
	if read(t, filepath.Join(src, "q", "new.jsonl")) != "n\n" || read(t, filepath.Join(src, "p", "clash.jsonl")) != "x\n" || read(t, filepath.Join(src, "p", "same.jsonl")) != "a\nb\n" {
		t.Error("shared history wrong")
	}
	var parkedCopy string
	filepath.WalkDir(conf, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			parkedCopy = read(t, p)
		}
		return nil
	})
	if parkedCopy != "y\n" {
		t.Errorf("parked %q", parkedCopy)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("the profile's folder is left")
	}

	hs, hd := filepath.Join(root, "history.jsonl"), filepath.Join(prof, "history.jsonl")
	write(t, hs, "1\n2")
	write(t, hd, "2\n3\n3\n")
	_, _, err = mergeHistory(hs, hd, conf)
	must(t, err)
	if got := read(t, hs); got != "1\n2\n3\n" {
		t.Errorf("history.jsonl %q", got)
	}
	if _, err := os.Stat(hd); !os.IsNotExist(err) {
		t.Error("the profile's history.jsonl is left")
	}

	must(t, os.Symlink(src, dest))
	if m, p, err := mergeHistory(src, dest, conf); m+p != 0 || err != nil {
		t.Error("merged through the link")
	}
}

// Opened with history shared, a profile that kept its own merges it into
// ~/.claude first; with a claude still running there it is left alone, as
// the whole profile is.
func TestRunMergesProfileHistory(t *testing.T) {
	r := newRunBook(t)
	must(t, os.Mkdir(filepath.Join(r.h.Root, "projects"), 0o700))
	write(t, filepath.Join(r.dir(), "projects", "p", "old.jsonl"), "kept\n")
	r.running = true
	if err := r.b.Run(bob.Key(), "/a", both); !errors.Is(err, errProfileBusy) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.h.Root, "projects", "p", "old.jsonl")); err == nil {
		t.Fatal("merged under a running claude")
	}
	r.running = false
	r.b.Now = func() time.Time { return t0.Add(time.Hour) }
	r.b.Refresh(ctx, true) // taken back
	must(t, r.b.Run(bob.Key(), "/a", both))
	if read(t, filepath.Join(r.h.Root, "projects", "p", "old.jsonl")) != "kept\n" {
		t.Error("not merged")
	}
	if to, err := os.Readlink(filepath.Join(r.dir(), "projects")); err != nil || !strings.HasSuffix(to, "projects") {
		t.Errorf("not linked: %q %v", to, err)
	}
}

// claude-swap's _profile_copy_wins: of two diverged copies of a
// transcript, the later one stays shared and the other is parked, under
// shared/ or profile/ for where it came from; never under a live session
// writing the shared copy. A parked copy never lands on another.
func TestMergeHistoryLaterCopyWins(t *testing.T) {
	root, prof, conf := t.TempDir(), t.TempDir(), t.TempDir()
	src, dest := filepath.Join(root, "projects"), filepath.Join(prof, "projects")
	old := `{"timestamp":"2026-11-01T10:00:00.000Z"}` + "\n"
	newer := `{"timestamp":"2026-11-02T10:00:00.000Z"}` + "\n" + `{"type":"last-prompt"}` + "\n"
	write(t, filepath.Join(src, "p", "s1.jsonl"), old)
	write(t, filepath.Join(dest, "p", "s1.jsonl"), newer)
	write(t, filepath.Join(src, "p", "s2.jsonl"), old)
	write(t, filepath.Join(dest, "p", "s2.jsonl"), newer)
	live := func(rel string) bool { return strings.Contains(rel, "s2") }
	_, parked, err := mergeHistoryLive(src, dest, conf, live)
	must(t, err)
	if parked != 2 || read(t, filepath.Join(src, "p", "s1.jsonl")) != newer || read(t, filepath.Join(src, "p", "s2.jsonl")) != old {
		t.Fatalf("parked %d; s1 %q, s2 %q", parked, read(t, filepath.Join(src, "p", "s1.jsonl")), read(t, filepath.Join(src, "p", "s2.jsonl")))
	}
	sides := map[string]string{}
	filepath.WalkDir(conf, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(conf, p)
			sides[strings.SplitN(rel, string(filepath.Separator), 3)[1]+"/"+filepath.Base(p)] = read(t, p)
		}
		return nil
	})
	if sides["shared/s1.jsonl"] != old || sides["profile/s2.jsonl"] != newer {
		t.Errorf("parked %v", sides)
	}
	// Again in the same second: the earlier parked copy stays.
	write(t, filepath.Join(src, "p", "s3.jsonl"), "x\n")
	write(t, filepath.Join(dest, "p", "s3.jsonl"), "y\n")
	_, _, err = mergeHistoryLive(src, dest, conf, live)
	must(t, err)
	write(t, filepath.Join(dest, "p", "s3.jsonl"), "z\n")
	_, _, err = mergeHistoryLive(src, dest, conf, live)
	must(t, err)
	n := 0
	filepath.WalkDir(conf, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(filepath.Base(p), "s3.jsonl") {
			n++
		}
		return nil
	})
	if n != 2 {
		t.Errorf("%d parked copies of s3, want 2", n)
	}
}

func TestLiveTranscripts(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sessions", "1.json"), `{"pid":`+strconv.Itoa(os.Getpid())+`,"sessionId":"abc"}`)
	write(t, filepath.Join(root, "sessions", "2.json"), `{"pid":999999999,"sessionId":"gone"}`)
	live := liveTranscripts(root)
	if !live("p/abc.jsonl") || !live("p/abc/sub.jsonl") || live("p/gone.jsonl") || live("p/other.jsonl") {
		t.Error("live sessions misread")
	}
	write(t, filepath.Join(root, "sessions", "3.json"), `{`)
	if !liveTranscripts(root)("p/other.jsonl") {
		t.Error("an unreadable record must count as live")
	}
}

// A crashed claude's record whose pid the system gave to another process
// is no running claude: take-back and Open in Terminal don't wait on it.
func TestClaudeRunningSkipsReusedPid(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-24 * time.Hour).UTC().Format("Mon Jan _2 15:04:05 2006")
	write(t, filepath.Join(dir, "sessions", "1.json"), `{"pid":`+strconv.Itoa(os.Getpid())+`,"sessionId":"s","procStart":"`+old+`"}`)
	if running, err := ClaudeRunning(dir); err != nil || running {
		t.Errorf("a reused pid: running %v, %v", running, err)
	}
	if liveTranscripts(dir)("p/s.jsonl") {
		t.Error("a reused pid's session reads as live")
	}
}
