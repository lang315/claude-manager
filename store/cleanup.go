package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Item is a file or directory the Cleanup screen can trash.
type Item struct {
	Path     string
	Size     int64
	Modified time.Time
}

// Group is a kind of leftover, with the items of it found.
type Group struct {
	Name  string
	Items []Item
	Size  int64
}

func item(path string) Item {
	it := Item{Path: path}
	if info, err := os.Lstat(path); err == nil {
		it.Modified = info.ModTime()
	}
	it.Size, _ = dirSize(path)
	return it
}

// SessionPaths returns what belongs to a session and exists: its .jsonl,
// its side dir, its file history and its environment.
func (s *Store) SessionPaths(x Session) []string {
	var out []string
	for _, p := range []string{
		x.File,
		filepath.Join(x.Dir, x.ID),
		filepath.Join(s.Root, "file-history", x.ID),
		filepath.Join(s.Root, "session-env", x.ID),
	} {
		if _, err := os.Lstat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Cleanup returns the leftovers the user can trash, in a fixed order of
// groups. Live sessions and anything written in the last 10 minutes are
// left out. With no projects, nothing counts as an orphan.
func (s *Store) Cleanup(projects []Project, days int) []Group {
	liveIDs, since := s.live()
	live := map[string]bool{}
	for id := range liveIDs {
		live[id] = true
	}
	now := s.Now()
	cutoff := now.AddDate(0, 0, -days)
	ids := map[string]bool{}
	for _, p := range projects {
		// The list may be older than the disk: a session whose .jsonl exists is never an orphan.
		for _, f := range listFiles(p.Dir, ".jsonl") {
			if id := strings.TrimSuffix(filepath.Base(f), ".jsonl"); uuidRe.MatchString(id) {
				ids[id] = true
			}
		}
		for _, x := range p.Sessions {
			ids[x.ID] = true
			// A session written to in the last 10 minutes runs, even without a sessions/<pid>.json.
			if x.Live {
				live[x.ID] = true
			}
		}
	}
	skip := func(it Item) bool { return isLive(live, it.Path) || now.Sub(it.Modified) < recent }
	// A snapshot is written once at session start and not touched again, and its name is no
	// session id: keep every one made since the earliest live session started (all, if unknown).
	skipSnapshot := func(it Item) bool {
		return skip(it) || (len(liveIDs) > 0 && !it.Modified.Before(since))
	}
	orphan := func(it Item) bool { return len(ids) > 0 && !ids[filepath.Base(it.Path)] }

	groups := []Group{
		s.entries("session-env", "session-env", nil, skip),
		s.entries("paste-cache", "paste-cache", nil, skip),
		s.entries("shell-snapshots", "shell-snapshots", nil, skipSnapshot),
		s.entries("debug", "debug", nil, skip),
		s.entries("file-history without a session", "file-history", orphan, skip),
		s.entries(fmt.Sprintf("transcripts older than %d days", days), "transcripts",
			func(it Item) bool { return it.Modified.Before(cutoff) }, skip),
	}
	orphans := Group{Name: "session data without a session"}
	old := Group{Name: fmt.Sprintf("sessions older than %d days", days)}
	for _, p := range projects {
		des, _ := os.ReadDir(p.Dir)
		for _, d := range des {
			if d.IsDir() && uuidRe.MatchString(d.Name()) && !ids[d.Name()] {
				orphans.add(item(filepath.Join(p.Dir, d.Name())), skip)
			}
		}
		for _, x := range p.Sessions {
			if x.Live || !x.Modified.Before(cutoff) {
				continue
			}
			for _, path := range s.SessionPaths(x) {
				old.add(item(path), skip)
			}
		}
	}
	return append(groups, orphans, old)
}

// entries is a group of the entries of a directory of the root that want
// keeps (all when nil). The directory itself is never an item: Claude Code
// expects it to exist.
func (s *Store) entries(name, dir string, want, skip func(Item) bool) Group {
	g := Group{Name: name}
	des, _ := os.ReadDir(filepath.Join(s.Root, dir))
	for _, d := range des {
		it := item(filepath.Join(s.Root, dir, d.Name()))
		if want == nil || want(it) {
			g.add(it, skip)
		}
	}
	return g
}

func (g *Group) add(it Item, skip func(Item) bool) {
	if skip(it) {
		return
	}
	g.Items = append(g.Items, it)
	g.Size += it.Size
}

// isLive reports whether the base name of path, with or without its
// extension (debug/<id>.txt), is a live session id.
func isLive(live map[string]bool, path string) bool {
	base := filepath.Base(path)
	return live[base] || live[strings.TrimSuffix(base, filepath.Ext(base))]
}

// TrashPaths moves paths to the Trash in order and stops at the first that
// fails, naming it. It refuses the root itself and anything outside it. It
// skips, without counting, a path already handled in this call (groups
// overlap), one that is gone, one of a session that is live now, and one changed
// in the last 10 minutes or belonging to a session whose .jsonl was.
func (s *Store) TrashPaths(paths []string) (moved int, err error) {
	live := s.LiveIDs()
	now := s.Now()
	touched := map[string]bool{} // ids of sessions written to in the last 10 minutes
	dirs, _ := os.ReadDir(filepath.Join(s.Root, "projects"))
	for _, d := range dirs {
		es, _ := os.ReadDir(filepath.Join(s.Root, "projects", d.Name()))
		for _, e := range es {
			if info, err := e.Info(); err == nil && !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") && now.Sub(info.ModTime()) < recent {
				touched[strings.TrimSuffix(e.Name(), ".jsonl")] = true
			}
		}
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if !s.inside(p) {
			return moved, fmt.Errorf("%s is not inside %s", p, s.Root)
		}
		p = filepath.Clean(p)
		if seen[p] || isLive(live, p) {
			continue
		}
		seen[p] = true
		info, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if (err == nil && now.Sub(info.ModTime()) < recent) || isLive(touched, p) {
			continue
		}
		if err := s.Trash(p); err != nil {
			return moved, fmt.Errorf("%s: %w", p, err)
		}
		moved++
	}
	return moved, nil
}

func (s *Store) inside(p string) bool {
	rel, err := filepath.Rel(s.Root, filepath.Clean(p))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
