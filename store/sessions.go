package store

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Session is one conversation: projects/<dir>/<ID>.jsonl and its side dir
// projects/<dir>/<ID>/.
type Session struct {
	ID, Dir, File        string // Dir is projects/<dir>, File the .jsonl
	Cwd, Branch, Version string
	Title, FirstPrompt   string
	Started, Modified    time.Time
	Size                 int64 // the .jsonl and its side dir
	Subagents            int
	Live                 bool // running, or written in the last 10 minutes
	Err                  string
}

// Project is a directory of projects/, its sessions newest first.
type Project struct {
	Dir      string // absolute projects/<encoded> dir
	Path     string // the real path, from a session's cwd; the encoded name without one
	Sessions []Session
	Size     int64
}

// uuidRe matches session ids: only entries named so are session data.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// headLimit is how much of a session is read for its metadata; past it only
// lines holding an ai-title or a user record are parsed, and nothing is read past titleLimit.
const headLimit = 64 << 10

// titleLimit caps the whole read: a session whose ai-title lies past it
// reads untitled, so that one without a title is not read to its end.
const titleLimit = 8 << 20

var aiTitle = []byte(`"ai-title"`)
var userType = []byte(`"type":"user"`)

// LiveIDs returns the ids of the sessions whose process runs, from
// sessions/<pid>.json.
func (s *Store) LiveIDs() map[string]bool {
	ids, _ := s.live()
	return ids
}

// live is LiveIDs plus the earliest start of those sessions. since is the
// zero time when any live session has no startedAt: its start is unknown.
func (s *Store) live() (ids map[string]bool, since time.Time) {
	ids = map[string]bool{}
	unknown := false
	for _, f := range listFiles(filepath.Join(s.Root, "sessions"), ".json") {
		var r struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
			StartedAt int64  `json:"startedAt"` // epoch milliseconds
		}
		b, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(b, &r) != nil || r.SessionID == "" {
			continue
		}
		if !s.Alive(r.PID) {
			continue
		}
		ids[r.SessionID] = true
		if r.StartedAt <= 0 {
			unknown = true
		} else if t := time.UnixMilli(r.StartedAt); since.IsZero() || t.Before(since) {
			since = t
		}
	}
	if unknown {
		since = time.Time{}
	}
	return ids, since
}

// Projects lists projects/ with the metadata of every session, largest
// project first.
func (s *Store) Projects() ([]Project, error) {
	live := s.LiveIDs()
	dirs, err := os.ReadDir(filepath.Join(s.Root, "projects"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		p := Project{Dir: filepath.Join(s.Root, "projects", d.Name()), Path: d.Name()}
		for _, f := range listFiles(p.Dir, ".jsonl") {
			if !uuidRe.MatchString(strings.TrimSuffix(filepath.Base(f), ".jsonl")) {
				continue
			}
			x := s.session(f, live)
			if p.Path == d.Name() && x.Cwd != "" {
				p.Path = x.Cwd
			}
			p.Size += x.Size
			p.Sessions = append(p.Sessions, x)
		}
		if len(p.Sessions) == 0 {
			continue
		}
		slices.SortFunc(p.Sessions, func(a, b Session) int { return b.Modified.Compare(a.Modified) })
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Project) int { return cmp.Compare(b.Size, a.Size) })
	return out, nil
}

func (s *Store) session(file string, live map[string]bool) Session {
	id := strings.TrimSuffix(filepath.Base(file), ".jsonl")
	x := Session{ID: id, File: file, Dir: filepath.Dir(file)}
	info, err := os.Stat(file)
	if err != nil {
		x.Err = err.Error()
		return x
	}
	x.Modified = info.ModTime()
	side := filepath.Join(x.Dir, id)
	sideSize, _ := dirSize(side)
	x.Size = info.Size() + sideSize
	x.Subagents = len(listFiles(filepath.Join(side, "subagents"), ".jsonl"))
	if err := readHead(file, &x); err != nil {
		x.Err = err.Error()
	}
	x.Live = live[id] || s.Now().Sub(x.Modified) < recent
	return x
}

// listFiles returns the paths of the non-directory entries of dir ending in
// suffix. Unlike filepath.Glob it is safe for roots holding [, * or ?.
func listFiles(dir, suffix string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// readHead fills a session's metadata from its first 64 KB, then reads on
// only until it finds the ai-title, parsing only the lines that hold one.
func readHead(file string, x *Session) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for read := 0; ; {
		line, err := r.ReadBytes('\n')
		head := read < headLimit
		read += len(line)
		if head || (x.Title == "" && bytes.Contains(line, aiTitle)) || (x.FirstPrompt == "" && bytes.Contains(line, userType)) {
			if rec, ok := parseRecord(line); ok {
				x.note(rec, head)
			}
		}
		if err == io.EOF || (!head && (x.Title != "" && x.FirstPrompt != "" || read >= titleLimit)) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (x *Session) note(r record, head bool) {
	if r.Type == "ai-title" && x.Title == "" {
		x.Title = r.AITitle
	}
	if head {
		x.Cwd = cmp.Or(x.Cwd, r.Cwd)
		x.Branch = cmp.Or(x.Branch, r.GitBranch)
		x.Version = cmp.Or(x.Version, r.Version)
		if x.Started.IsZero() {
			x.Started = r.time()
		}
	}
	if x.FirstPrompt != "" {
		return
	}
	for _, m := range r.messages() {
		if m.Role == "user" && m.Kind == "text" && strings.TrimSpace(m.Text) != "" {
			x.FirstPrompt = FirstLine(m.Text, 200)
			return
		}
	}
}

// FirstLine returns the first line of s, trimmed, cut to n runes.
func FirstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}
