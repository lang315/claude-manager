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
	"sync"
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

// Project is a directory of projects/, its sessions newest first. Folders
// whose sessions share a cwd are one project: Dir is its newest session's.
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

// SessionScan keeps what earlier scans read from the head of each session,
// so a Rescan rereads only the sessions whose .jsonl changed. Its zero value
// is ready, and the Sessions and Cleanup loads may use it at once.
type SessionScan struct {
	mu    sync.Mutex
	heads map[string]sessionHead // by .jsonl path
	reads int                    // heads read from disk: tests count them
}

// sessionHead is what readHead found in a .jsonl of this size and mtime.
type sessionHead struct {
	size  int64
	mtime time.Time
	Session
}

// head fills x's head metadata from the cache, or from the file when it
// changed since it was read.
func (sc *SessionScan) head(file string, info fs.FileInfo, x *Session) {
	sc.mu.Lock()
	h, ok := sc.heads[file]
	sc.mu.Unlock()
	if !ok || h.size != info.Size() || !h.mtime.Equal(info.ModTime()) {
		h = sessionHead{size: info.Size(), mtime: info.ModTime()}
		err := readHead(file, &h.Session)
		sc.mu.Lock()
		sc.reads++
		if err == nil { // an error is not kept: the next scan tries again
			if sc.heads == nil {
				sc.heads = map[string]sessionHead{}
			}
			sc.heads[file] = h
		}
		sc.mu.Unlock()
		if err != nil {
			h.Err = err.Error()
		}
	}
	x.Cwd, x.Branch, x.Version = h.Cwd, h.Branch, h.Version
	x.Title, x.FirstPrompt, x.Started, x.Err = h.Title, h.FirstPrompt, h.Started, h.Err
}

// keep drops the heads of the files not in seen: sessions gone.
func (sc *SessionScan) keep(seen map[string]bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for f := range sc.heads {
		if !seen[f] {
			delete(sc.heads, f)
		}
	}
}

var aiTitle = []byte(`"ai-title"`)
var userType = []byte(`"type":"user"`)

// LiveIDs returns the ids of the sessions whose process runs, from
// sessions/<pid>.json in Root and in each profile.
func (s *Store) LiveIDs() map[string]bool {
	ids, _ := s.live()
	return ids
}

// live is LiveIDs plus the earliest start of those sessions. since is the
// zero time when any live session has no startedAt: its start is unknown.
func (s *Store) live() (ids map[string]bool, since time.Time) {
	ids = map[string]bool{}
	unknown := false
	files := listFiles(filepath.Join(s.Root, "sessions"), ".json")
	if s.Profiles != "" {
		ps, _ := os.ReadDir(s.Profiles)
		for _, p := range ps {
			files = append(files, listFiles(filepath.Join(s.Profiles, p.Name(), "sessions"), ".json")...)
		}
	}
	for _, f := range files {
		var r struct {
			PID       int             `json:"pid"`
			SessionID string          `json:"sessionId"`
			StartedAt int64           `json:"startedAt"` // epoch milliseconds
			ProcStart json.RawMessage `json:"procStart"`
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// A startedAt that is no integer leaves StartedAt 0 (start unknown); the session still counts as live.
		var typeErr *json.UnmarshalTypeError
		if err := json.Unmarshal(b, &r); err != nil && !errors.As(err, &typeErr) || r.SessionID == "" {
			continue
		}
		if !s.Alive(r.PID) || !SameProcess(r.PID, Stamp(r.ProcStart)) { // a crashed claude's pid, reused
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
// project first. Folders that share a cwd are one project, keyed by the
// folder of its newest session. scan, when set, keeps the sessions' heads
// across calls.
func (s *Store) Projects(scan *SessionScan) ([]Project, error) {
	if scan == nil {
		scan = &SessionScan{}
	}
	live := s.LiveIDs()
	dirs, err := os.ReadDir(filepath.Join(s.Root, "projects"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Project
	byPath := map[string]int{} // a path's index in out
	seen := map[string]bool{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		p := Project{Dir: filepath.Join(s.Root, "projects", d.Name()), Path: d.Name()}
		for _, f := range listFiles(p.Dir, ".jsonl") {
			if !uuidRe.MatchString(strings.TrimSuffix(filepath.Base(f), ".jsonl")) {
				continue
			}
			seen[f] = true
			x := s.session(f, live, scan)
			if p.Path == d.Name() && x.Cwd != "" {
				p.Path = x.Cwd
			}
			p.Size += x.Size
			p.Sessions = append(p.Sessions, x)
		}
		if len(p.Sessions) == 0 {
			continue
		}
		// Claude Code can encode one cwd into two folder names.
		if i, ok := byPath[p.Path]; ok {
			out[i].Size += p.Size
			out[i].Sessions = append(out[i].Sessions, p.Sessions...)
			continue
		}
		byPath[p.Path] = len(out)
		out = append(out, p)
	}
	scan.keep(seen)
	for i := range out {
		p := &out[i]
		slices.SortFunc(p.Sessions, func(a, b Session) int { return b.Modified.Compare(a.Modified) })
		p.Dir = p.Sessions[0].Dir
	}
	slices.SortFunc(out, func(a, b Project) int { return cmp.Compare(b.Size, a.Size) })
	return out, nil
}

// dirs are the folders a project's sessions are in: one, or more when
// folders that share a cwd were merged.
func (p Project) dirs() []string {
	out := []string{p.Dir}
	for _, x := range p.Sessions {
		if !slices.Contains(out, x.Dir) {
			out = append(out, x.Dir)
		}
	}
	return out
}

func (s *Store) session(file string, live map[string]bool, scan *SessionScan) Session {
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
	scan.head(file, info, &x)
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

// readHead fills a session's metadata. It parses every line of the first 64 KB
// (headLimit): cwd, branch, version, start, and the first title or prompt found.
// Past the head it reads on, parsing only a line that holds an ai-title (while
// there is no title) or a user record (while there is no first prompt), and stops
// once both are found, at the end of the file, or after 8 MB (titleLimit).
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
			x.FirstPrompt = FirstLine(slashCommand(m.Text), 200)
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

// commandTag finds the slash command Claude Code wraps a typed command in:
// <command-name>/model</command-name> … <command-args>opus</command-args>.
var commandTag = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>(?:.*?<command-args>(.*?)</command-args>)?`)

// slashCommand turns a prompt that is a slash command into the command typed.
func slashCommand(s string) string {
	if m := commandTag.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1] + " " + m[2])
	}
	return s
}
