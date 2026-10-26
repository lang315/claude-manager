package store

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Entry is a top-level file or directory of the root.
type Entry struct {
	Name      string
	Size      int64
	Files     int
	Cleanable bool // the Cleanup screen or the Config screen's Backups tab can trash it, or part of it
}

var cleanable = map[string]bool{
	"projects": true, "file-history": true, "session-env": true, "paste-cache": true,
	"shell-snapshots": true, "debug": true, "transcripts": true,
}

// Scan returns the root's entries, largest first.
func (s *Store) Scan() ([]Entry, error) {
	des, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, d := range des {
		e := Entry{Name: d.Name(), Cleanable: cleanable[d.Name()] || strings.HasPrefix(d.Name(), "settings.json.bak")}
		e.Size, e.Files = dirSize(filepath.Join(s.Root, d.Name()))
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return cmp.Compare(b.Size, a.Size) })
	return out, nil
}

// dirSize adds up the files under path, or path itself when it is a file,
// without following symlinks. What it cannot read it leaves out.
func dirSize(path string) (size int64, files int) {
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
			files++
		}
		return nil
	})
	return size, files
}
