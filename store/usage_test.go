package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lang315/claude-manager/internal/fixture"
)

func readUsage(t *testing.T, path string) (*usageFile, int64) {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	f := &usageFile{}
	n, err := f.read(fh)
	if err != nil {
		t.Fatal(err)
	}
	return f, n
}

func callsByKey(f *usageFile) map[string]call {
	m := map[string]call{}
	for _, c := range f.calls {
		m[c.key] = c
	}
	return m
}

func TestUsageFileReadsCalls(t *testing.T) {
	_, d, _ := newStore(t)
	f, _ := readUsage(t, filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"))
	if f.cwd != "/Users/me/proj" || f.bad != 0 {
		t.Errorf("cwd %q, bad %d", f.cwd, f.bad)
	}
	got := callsByKey(f)
	if len(f.calls) != 6 {
		t.Fatalf("%d calls, want 6 (o1, o2, o3, o5, its advisor, o6): %+v", len(f.calls), f.calls)
	}
	o1 := got["msg-o1/req-o1"]
	if o1.Output != 200000 || o1.Input != 100000 || o1.CacheRead != 1000000 || !o1.time.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("o1: the 3 lines keep the first time and the final usage: %+v", o1)
	}
	if o2 := got["msg-o2/req-o2"]; o2.CacheWrite5m != 100000 || o2.CacheWrite1h != 200000 {
		t.Errorf("o2: the 5m/1h split: %+v", o2)
	}
	if o3 := got["msg-o3/req-o3"]; !o3.Fast || !o3.US {
		t.Errorf("o3: fast and US: %+v", o3)
	}
	if _, ok := got["msg-o4/req-o4"]; ok {
		t.Error("a <synthetic> message counts as a call")
	}
	if a := got["msg-o5/req-o5:advisor:1"]; a.model != "claude-opus-5-5" || a.Input != 100000 || a.Output != 50000 {
		t.Errorf("the advisor call: %+v", a)
	}
	if o6 := got["msg-o6/req-o6"]; o6.CacheWrite5m != 400000 || o6.CacheWrite1h != 0 || o6.WebSearches != 100 {
		t.Errorf("o6: writes without the split are 5m: %+v", o6)
	}
}

func TestUsageFileCountsBadLines(t *testing.T) {
	_, d, _ := newStore(t)
	f, _ := readUsage(t, filepath.Join(d.Root, fixture.Other, fixture.Recent+".jsonl"))
	if f.bad != 1 {
		t.Errorf("bad = %d, want 1", f.bad)
	}
	if r3 := callsByKey(f)["msg-r3/req-r3"]; r3.Output != 1000 || !r3.time.Equal(time.Date(2026, 10, 25, 23, 59, 59, 900_000_000, time.UTC)) {
		t.Errorf("r3: %+v", r3)
	}
}

// Claude Code may still be writing the last line: it is left for the next
// scan, and the bytes read stop before it.
func TestUsageFileLeavesHalfLine(t *testing.T) {
	whole := fixture.UsageLine("/p", "2026-10-26T08:00:00.000Z", "a", "claude-haiku-4-5", `{"input_tokens":1}`) + "\n"
	half := fixture.UsageLine("/p", "2026-10-26T08:00:01.000Z", "b", "claude-haiku-4-5", `{"input_tokens":1}`)
	f := &usageFile{}
	n, err := f.read(strings.NewReader(whole + half))
	if err != nil || n != int64(len(whole)) || len(f.calls) != 1 {
		t.Errorf("read %d bytes, %d calls, err %v; want %d bytes, 1 call", n, len(f.calls), err, len(whole))
	}
}

func TestUsageFileLongLine(t *testing.T) {
	big := strings.Repeat("x", 2<<20)
	line := `{"type":"assistant","cwd":"/p","timestamp":"2026-10-26T08:00:00.000Z","requestId":"r","message":{"id":"m","model":"claude-haiku-4-5","content":[{"type":"text","text":"` + big + `"}],"usage":{"input_tokens":7}}}` + "\n"
	f := &usageFile{}
	if _, err := f.read(strings.NewReader(line)); err != nil || len(f.calls) != 1 || f.calls[0].Input != 7 {
		t.Fatalf("a 2 MB line: %d calls, err %v", len(f.calls), err)
	}
}

func TestMerge(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	partial := call{key: "k", time: t0, callTokens: callTokens{Output: 4}}
	final := call{key: "k", time: t0.Add(time.Second), callTokens: callTokens{Output: 400}}
	for _, c := range []struct {
		name string
		a, b call
	}{{"partial first", partial, final}, {"final first", final, partial}} {
		got := merge(c.a, c.b)
		if got.Output != 400 || !got.time.Equal(t0) {
			t.Errorf("%s: %+v, want output 400 at %v", c.name, got, t0)
		}
	}
	tie := merge(call{key: "k", model: "a", callTokens: callTokens{Output: 1}}, call{key: "k", model: "b", callTokens: callTokens{Output: 1}})
	if tie.model != "b" {
		t.Errorf("a tie keeps %q, want the later copy", tie.model)
	}
}
func costOf(us []Usage) (total float64) {
	for _, u := range us {
		total += u.Cost
	}
	return total
}

func TestUsage(t *testing.T) {
	s, _, _ := newStore(t)
	var scan UsageScan
	got, bad, unreadable, err := s.Usage(&scan, nil)
	if err != nil || bad != 1 || unreadable != 0 {
		t.Fatalf("bad %d, unreadable %d, err %v; want 1, 0, nil", bad, unreadable, err)
	}
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	proj, other := "/Users/me/proj", "/Users/me/other"
	want := []Usage{
		{SessionID: fixture.Old, Project: proj, Day: day(9, 1), Model: "claude-sonnet-5-5", Calls: 1, CacheWrite5m: 400000, WebSearches: 100, Cost: 2},
		{SessionID: fixture.Old, Project: proj, Day: day(10, 10), Model: "claude-opus-5-5", Calls: 1, Fast: 1, Input: 100000, Output: 100000, Cost: 5.28},
		{SessionID: fixture.Old, Project: proj, Day: day(10, 20), Model: "claude-opus-5-5", Calls: 2, Input: 110000, Output: 200000, CacheRead: 1000000, CacheWrite5m: 100000, CacheWrite1h: 200000, Cost: 6.74},
		{SessionID: fixture.Old, Project: proj, Day: day(10, 21), Model: "claude-opus-5-5", Calls: 1, Input: 100000, Output: 50000, Cost: 1.4},
		{SessionID: fixture.Old, Project: proj, Day: day(10, 21), Model: "claude-sonnet-5-5", Calls: 1, Input: 1000000, Cost: 2},
		{SessionID: fixture.Recent, Project: other, Day: day(10, 25), Model: "claude-sonnet-5", Calls: 1, Output: 1000, Cost: 0.01},
		{SessionID: fixture.Recent, Project: other, Day: day(10, 26), Model: "claude-future-9", Calls: 1, Input: 1000, Output: 1000, Unpriced: 1},
		{SessionID: fixture.Recent, Project: other, Day: day(10, 26), Model: "claude-haiku-4-5-20251001", Calls: 1, Input: 1000, Output: 400, Cost: 0.003},
		{SessionID: fixture.Recent, Project: other, Day: day(10, 26), Model: "claude-haiku-5-5", Calls: 1, Input: 50000, Output: 10000, CacheRead: 100000, Cost: 0.055},
	}
	if len(got) != len(want) {
		t.Fatalf("%d buckets, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		gc, wc := g.Cost, w.Cost
		g.Cost, w.Cost = 0, 0
		if g != w || !near(gc, wc) {
			t.Errorf("bucket %d:\n got %+v cost %v\nwant %+v cost %v", i, g, gc, w, wc)
		}
	}
}

// At +07:00, r3's first line (23:59:59.900Z on the 25th) is on the 26th.
func TestUsageLocalDay(t *testing.T) {
	s, _, _ := newStore(t)
	zone := time.FixedZone("ICT", 7*3600)
	s.Now = func() time.Time { return fixture.Now.In(zone) }
	got, _, _, _ := s.Usage(&UsageScan{}, nil)
	for _, u := range got {
		if u.Model == "claude-sonnet-5" && !u.Day.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, zone)) {
			t.Errorf("r3's day is %v, want the 26th at +07:00", u.Day)
		}
	}
}

func TestUsageScanReadsOnlyWhatChanged(t *testing.T) {
	s, d, _ := newStore(t)
	opens := 0
	scan := UsageScan{open: func(p string) (*os.File, error) { opens++; return os.Open(p) }}
	first, _, _, _ := s.Usage(&scan, nil)
	opens = 0
	again, _, _, _ := s.Usage(&scan, nil)
	if opens != 0 || !near(costOf(again), costOf(first)) {
		t.Errorf("an unchanged tree: %d opens, cost %v; want 0 opens, cost %v", opens, costOf(again), costOf(first))
	}

	recent := filepath.Join(d.Root, fixture.Other, fixture.Recent+".jsonl")
	appendTo := func(s string) {
		f, err := os.OpenFile(recent, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	dollar := fixture.UsageLine("/Users/me/other", "2026-10-26T10:00:00.000Z", "r4", "claude-haiku-4-5", `{"input_tokens":1000000}`) // $1
	appendTo(dollar + "\n")
	opens = 0
	grown, _, _, _ := s.Usage(&scan, nil)
	if opens != 1 || !near(costOf(grown), costOf(first)+1) {
		t.Errorf("an appended call: %d opens, cost %v; want 1 open, cost %v", opens, costOf(grown), costOf(first)+1)
	}

	appendTo(strings.Replace(dollar, "r4", "r5", 2)) // no "\n" yet
	half, _, _, _ := s.Usage(&scan, nil)
	appendTo("\n")
	whole, _, _, _ := s.Usage(&scan, nil)
	if !near(costOf(half), costOf(grown)) || !near(costOf(whole), costOf(grown)+1) {
		t.Errorf("a half line counts %v then %v; want %v then %v", costOf(half), costOf(whole), costOf(grown), costOf(grown)+1)
	}

	old := filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl")
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(old, []byte(fixture.UsageLine("/Users/me/proj", "2026-10-20T10:00:00.000Z", "o1", "claude-opus-5-5", `{"input_tokens":100000,"output_tokens":200000,"cache_read_input_tokens":1000000}`)+"\n"), 0o644))
	rewritten, _, _, _ := s.Usage(&scan, nil)
	var oldCost float64
	for _, u := range rewritten {
		if u.SessionID == fixture.Old {
			oldCost += u.Cost
		}
	}
	if !near(oldCost, 4.6) {
		t.Errorf("a rewritten Old costs %v, want 4.6 (read again whole)", oldCost)
	}

	must(os.Remove(old))
	gone, _, _, _ := s.Usage(&scan, nil)
	if slices.ContainsFunc(gone, func(u Usage) bool { return u.SessionID == fixture.Old }) {
		t.Error("a deleted transcript's calls stay")
	}
}

// A transcript that goes between the listing and the read is gone, not
// unreadable.
func TestUsageFileVanishes(t *testing.T) {
	s, _, _ := newStore(t)
	scan := UsageScan{open: func(p string) (*os.File, error) {
		if strings.HasSuffix(p, fixture.Recent+".jsonl") {
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		}
		return os.Open(p)
	}}
	_, _, unreadable, err := s.Usage(&scan, nil)
	if err != nil || unreadable != 0 {
		t.Errorf("unreadable %d, err %v; want 0, nil", unreadable, err)
	}
	scan.open = func(string) (*os.File, error) { return nil, errors.New("denied") }
	scan.files = nil
	if _, _, unreadable, _ := s.Usage(&scan, nil); unreadable == 0 {
		t.Error("files that can't be opened aren't counted")
	}
}

// Subagents whose session's own transcript is gone still count, under the
// session's id, with their cwd as the project.
func TestUsageOrphanSubagent(t *testing.T) {
	s, d, _ := newStore(t)
	p := filepath.Join(d.Root, fixture.Proj, fixture.OrphanDir, "subagents", "agent-x.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	line := fixture.UsageLine("/Users/me/side", "2026-10-26T08:00:00.000Z", "x1", "claude-haiku-4-5", `{"input_tokens":1000000}`)
	if err := os.WriteFile(p, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, _, _ := s.Usage(&UsageScan{}, nil)
	if !slices.ContainsFunc(got, func(u Usage) bool {
		return u.SessionID == fixture.OrphanDir && u.Project == "/Users/me/side" && near(u.Cost, 1)
	}) {
		t.Errorf("the orphan subagent's call is missing: %+v", got)
	}
}

func TestUsageNoProjects(t *testing.T) {
	got, bad, unreadable, err := (&Store{Root: t.TempDir(), Now: time.Now}).Usage(&UsageScan{}, nil)
	if err != nil || len(got) != 0 || bad != 0 || unreadable != 0 {
		t.Errorf("no projects/: %v %d %d %v", got, bad, unreadable, err)
	}
}

// A field of an unexpected type doesn't fail the line, as parseRecord has it:
// the call counts with the fields that decoded.
func TestUsageFileOddFieldType(t *testing.T) {
	line := fixture.UsageLine("/p", "2026-10-26T08:00:00.000Z", "a", "claude-haiku-4-5", `{"input_tokens":7,"output_tokens":"many"}`) + "\n"
	f := &usageFile{}
	if _, err := f.read(strings.NewReader(line)); err != nil || f.bad != 0 || len(f.calls) != 1 || f.calls[0].Input != 7 {
		t.Errorf("bad %d, calls %+v, err %v; want the call with input 7", f.bad, f.calls, err)
	}
}
