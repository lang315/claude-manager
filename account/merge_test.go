package account

import (
	"os"
	"path/filepath"
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
// ~/.claude first; with a claude still running there it is left alone.
func TestRunMergesProfileHistory(t *testing.T) {
	r := newRunBook(t)
	must(t, os.Mkdir(filepath.Join(r.h.Root, "projects"), 0o700))
	write(t, filepath.Join(r.dir(), "projects", "p", "old.jsonl"), "kept\n")
	r.running = true
	must(t, r.b.Run(bob.Key(), "/a", both))
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
