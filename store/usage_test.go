package store

import (
	"os"
	"path/filepath"
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
	if a := got["msg-o5/req-o5:advisor:0"]; a.model != "claude-opus-5-5" || a.Input != 100000 || a.Output != 50000 {
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
