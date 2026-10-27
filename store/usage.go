package store

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// call is one API call a transcript records.
type call struct {
	key   string // message.id/requestId, "" when the line lacks either
	time  time.Time
	model string
	callTokens
}

// usageFile is what a scan read of one transcript.
type usageFile struct {
	calls []call
	index map[string]int // a key's index in calls
	cwd   string         // the first cwd its calls carry
	bad   int            // usage lines that are not JSON
}

// usageFields are the counts of a usage record, at its top or in one of its
// iterations.
type usageFields struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	CacheSplit *struct {
		Write5m int64 `json:"ephemeral_5m_input_tokens"`
		Write1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServerTools struct {
		WebSearches int64 `json:"web_search_requests"`
	} `json:"server_tool_use"`
	Speed string `json:"speed"`
	Geo   string `json:"inference_geo"`
}

// usageLine is what a scan reads of a transcript line.
type usageLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			usageFields
			Iterations []struct {
				Type  string `json:"type"`
				Model string `json:"model"`
				usageFields
			} `json:"iterations"`
		} `json:"usage"`
	} `json:"message"`
}

// tokens are the counts of a usage record, negatives as 0. Without the
// 5m/1h split, every cache write counts as a 5-minute one.
func (u usageFields) tokens() callTokens {
	t := callTokens{
		Input: max(u.Input, 0), Output: max(u.Output, 0), CacheRead: max(u.CacheRead, 0),
		WebSearches: max(u.ServerTools.WebSearches, 0),
		Fast:        u.Speed == "fast", US: u.Geo == "us",
	}
	if s := u.CacheSplit; s != nil && s.Write5m+s.Write1h > 0 {
		t.CacheWrite5m, t.CacheWrite1h = max(s.Write5m, 0), max(s.Write1h, 0)
	} else {
		t.CacheWrite5m = max(u.CacheWrite, 0)
	}
	return t
}

var usageMark, assistantMark = []byte(`"usage"`), []byte(`"assistant"`)

// read reads the calls of r, which starts at a line boundary, and returns
// the bytes of the whole lines it read. A last line without "\n" is left for
// the next scan: Claude Code may still be writing it.
func (f *usageFile) read(r io.Reader) (int64, error) {
	br := bufio.NewReaderSize(r, 1<<20) // not a Scanner: a line can be megabytes
	var n int64
	for {
		line, err := br.ReadBytes('\n')
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n += int64(len(line))
		// About one line in seven holds usage: the others are not decoded.
		if bytes.Contains(line, usageMark) && bytes.Contains(line, assistantMark) {
			f.line(line)
		}
	}
}

func (f *usageFile) line(b []byte) {
	var x usageLine
	// As parseRecord: a field of an unexpected type doesn't fail the line.
	if err := json.Unmarshal(b, &x); err != nil && !errors.As(err, new(*json.UnmarshalTypeError)) {
		f.bad++
		return
	}
	m := x.Message
	if x.Type != "assistant" || m.Usage == nil || m.Model == "" || m.Model == "<synthetic>" {
		return
	}
	if f.cwd == "" {
		f.cwd = x.Cwd
	}
	t, _ := time.Parse(time.RFC3339Nano, x.Timestamp)
	key := ""
	if m.ID != "" && x.RequestID != "" {
		key = m.ID + "/" + x.RequestID
	}
	f.add(call{key, t, m.Model, m.Usage.tokens()})
	// An advisor's tokens are not in the top-level counts: each is a call of
	// its own, under the advisor's model.
	for i, it := range m.Usage.Iterations {
		if it.Type != "advisor_message" {
			continue
		}
		k := ""
		if key != "" {
			k = key + ":advisor:" + strconv.Itoa(i)
		}
		tk := it.tokens()
		tk.Fast = tk.Fast || it.Speed == "" && m.Usage.Speed == "fast"
		f.add(call{k, t, cmp.Or(it.Model, m.Model), tk})
	}
}

// add puts c among the file's calls, merged with an earlier copy of its key.
func (f *usageFile) add(c call) {
	if c.key == "" {
		f.calls = append(f.calls, c)
		return
	}
	if f.index == nil {
		f.index = map[string]int{}
	}
	if i, ok := f.index[c.key]; ok {
		f.calls[i] = merge(f.calls[i], c)
		return
	}
	f.index[c.key] = len(f.calls)
	f.calls = append(f.calls, c)
}

// merge returns the copy of a call to keep from a and a later copy b. A
// streaming partial comes first with a few output tokens, so the copy with
// the larger output has the final usage; b wins a tie. The time is the
// earlier one: when the call started.
func merge(a, b call) call {
	keep := a
	if b.Output >= a.Output {
		keep = b
	}
	keep.time = a.time
	if a.time.IsZero() || !b.time.IsZero() && b.time.Before(a.time) {
		keep.time = b.time
	}
	return keep
}

// Usage is one bucket: a session's calls to one model on one local day.
type Usage struct {
	SessionID string    // the parent session; its subagents' calls fold into it
	Project   string    // the session's cwd
	Day       time.Time // local midnight; zero for calls without a time
	Model     string    // as recorded, date suffix and all
	Calls     int
	Fast      int // calls in fast mode

	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h int64

	WebSearches int64
	Cost        float64 // USD at API list prices, estimated; unpriced calls add nothing
	Unpriced    int     // calls whose model has no price
}

// Add sums b's counts into u.
func (u *Usage) Add(b Usage) {
	u.Calls += b.Calls
	u.Fast += b.Fast
	u.Input += b.Input
	u.Output += b.Output
	u.CacheRead += b.CacheRead
	u.CacheWrite5m += b.CacheWrite5m
	u.CacheWrite1h += b.CacheWrite1h
	u.WebSearches += b.WebSearches
	u.Cost += b.Cost
	u.Unpriced += b.Unpriced
}

// Tokens are all of u's tokens: input, output, cache reads and writes.
func (u Usage) Tokens() int64 {
	return u.Input + u.Output + u.CacheRead + u.CacheWrite5m + u.CacheWrite1h
}

// UsageScan keeps what earlier scans read, so a Rescan reads only what is new
// or changed. Its zero value is ready; use it from one goroutine at a time.
type UsageScan struct {
	files map[string]*scannedFile
	open  func(name string) (*os.File, error) // os.Open; tests count the opens
}

// scannedFile is a transcript as the last scan left it.
type scannedFile struct {
	usageFile
	session string // the parent session's id
	main    bool   // the session's own transcript, not a subagent's
	size    int64
	mtime   time.Time
	ino     uint64
	off     int64 // the end of the last whole line read
}

// Usage scans the transcripts under projects/ and returns the buckets of all
// their calls, with the usage lines that weren't JSON and the transcripts
// that couldn't be read. progress, when set, hears how many files are done
// of how many, on the scanning goroutine.
func (s *Store) Usage(scan *UsageScan, progress func(done, total int)) (buckets []Usage, bad, unreadable int, err error) {
	root := filepath.Join(s.Root, "projects")
	var paths []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // an unreadable folder: its transcripts aren't counted
		}
		if !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			if _, _, ok := sessionOf(root, p); ok {
				paths = append(paths, p)
			}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, 0, nil
	}
	if err != nil {
		return nil, 0, 0, err
	}
	if scan.files == nil {
		scan.files = map[string]*scannedFile{}
	}
	open := scan.open
	if open == nil {
		open = os.Open
	}
	seen := make(map[string]bool, len(paths))
	for i, p := range paths {
		switch err := scan.refresh(root, p, open); {
		case err == nil:
			seen[p] = true
		case !errors.Is(err, fs.ErrNotExist): // gone since the listing: not an error
			unreadable++
		}
		if progress != nil {
			progress(i+1, len(paths))
		}
	}
	for p := range scan.files {
		if !seen[p] {
			delete(scan.files, p)
		}
	}
	for _, f := range scan.files {
		bad += f.bad
	}
	return scan.buckets(s.Now().Location()), bad, unreadable, nil
}

// sessionOf returns the session a transcript under root belongs to:
// <dir>/<id>.jsonl is session id's own, and a file anywhere under
// <dir>/<id>/subagents/ is one of its subagents'.
func sessionOf(root, path string) (id string, main, ok bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case len(parts) == 2:
		id = strings.TrimSuffix(parts[1], ".jsonl")
		return id, true, uuidRe.MatchString(id)
	case len(parts) >= 4 && parts[2] == "subagents":
		return parts[1], false, uuidRe.MatchString(parts[1])
	}
	return "", false, false
}

// refresh brings path's calls up to date. Unchanged, it isn't read; grown on
// the same inode, it is read on from where the last scan stopped; anything
// else, it is read whole.
func (sc *UsageScan) refresh(root, path string, open func(string) (*os.File, error)) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	var ino uint64
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		ino = st.Ino
	}
	f := sc.files[path]
	if f != nil && f.size == info.Size() && f.mtime.Equal(info.ModTime()) {
		return nil
	}
	if f == nil || f.ino != ino || info.Size() <= f.size {
		id, main, _ := sessionOf(root, path)
		f = &scannedFile{session: id, main: main, ino: ino}
	}
	fh, err := open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	if _, err := fh.Seek(f.off, io.SeekStart); err != nil {
		return err
	}
	n, err := f.read(fh)
	if err != nil {
		return err
	}
	f.off += n
	f.size, f.mtime = info.Size(), info.ModTime()
	sc.files[path] = f
	return nil
}

// buckets merges each session's calls across its files, by key as merge
// does, and sums them by local day and model, each call priced on its own.
func (sc *UsageScan) buckets(loc *time.Location) []Usage {
	type session struct {
		cwd, subCwd string
		keyed       map[string]call
		loose       []call
	}
	sessions := map[string]*session{}
	for _, p := range slices.Sorted(maps.Keys(sc.files)) {
		f := sc.files[p]
		x := sessions[f.session]
		if x == nil {
			x = &session{keyed: map[string]call{}}
			sessions[f.session] = x
		}
		if f.main {
			x.cwd = cmp.Or(x.cwd, f.cwd)
		} else {
			x.subCwd = cmp.Or(x.subCwd, f.cwd)
		}
		for _, c := range f.calls {
			if c.key == "" {
				x.loose = append(x.loose, c)
				continue
			}
			if old, ok := x.keyed[c.key]; ok {
				c = merge(old, c)
			}
			x.keyed[c.key] = c
		}
	}
	type bucketKey struct{ session, day, model string }
	byKey := map[bucketKey]*Usage{}
	add := func(id, project string, c call) {
		var day time.Time
		if !c.time.IsZero() {
			t := c.time.In(loc)
			day = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
		k := bucketKey{id, day.Format(time.DateOnly), c.model}
		b := byKey[k]
		if b == nil {
			b = &Usage{SessionID: id, Project: project, Day: day, Model: c.model}
			byKey[k] = b
		}
		x := Usage{Calls: 1, Input: c.Input, Output: c.Output, CacheRead: c.CacheRead,
			CacheWrite5m: c.CacheWrite5m, CacheWrite1h: c.CacheWrite1h, WebSearches: c.WebSearches}
		if c.Fast {
			x.Fast = 1
		}
		if cost, ok := price(c.model, c.callTokens); ok {
			x.Cost = cost
		} else {
			x.Unpriced = 1
		}
		b.Add(x)
	}
	for id, x := range sessions {
		project := cmp.Or(x.cwd, x.subCwd)
		for _, c := range x.keyed {
			add(id, project, c)
		}
		for _, c := range x.loose {
			add(id, project, c)
		}
	}
	out := make([]Usage, 0, len(byKey))
	for _, b := range byKey {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b Usage) int {
		return cmp.Or(a.Day.Compare(b.Day), cmp.Compare(a.SessionID, b.SessionID), cmp.Compare(a.Model, b.Model))
	})
	return out
}
