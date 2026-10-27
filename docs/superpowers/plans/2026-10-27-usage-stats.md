# Usage Statistics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Usage screen that shows the tokens Claude Code used, and their estimated API cost, by day, project, session and model.

**Architecture:** `store/usage.go` scans every transcript under `projects/` and turns it into calls. It keeps the deduplicated calls of each file in a `UsageScan`, which lives in memory, so a Rescan reads only files that are new, grew or changed. After each scan, the calls of a session's files are merged by key and summed into buckets of (session, local day, model), each priced from `store/prices.go`. `view/usage.go` sums the buckets for the period and project chosen, and shows a cost-by-day chart and Projects / Sessions / Models tables.

**Tech Stack:**
- Go 1.27 and the standard library: `encoding/json`, `bufio`, `syscall` for the inode;
- MyGo native UI (`github.com/egoist/mygo/ui`): `Table`, `ListState`, `Tabs`, `Segmented`, `Painter`;
- `ui.Tester` for view tests.

**Spec:** `docs/superpowers/specs/2026-10-27-usage-stats-design.md`

## Global Constraints

- **Read-only:** the app never writes anything in `~/.claude`, and Usage writes nothing anywhere. No cache on disk.
- **Tests never read the real `~/.claude`:** they use `fixture.New` or a `t.TempDir()` root.
- **Prices:** USD per million tokens, from <https://platform.claude.com/docs/en/about-claude/pricing>, checked 2026-10-27. `store.PricesAsOf = "2026-10-27"`.
- **Dedupe:**
  - The key is `message.id + "/" + requestId`, within one parent session, across its main transcript and all its subagent files.
  - The copy with the largest `output_tokens` wins, and the later one on a tie.
  - A call's time is the earliest of its copies.
  - A call without the key counts on every line.
- **Skipped calls:** a `<synthetic>` model is skipped, and negative counts are 0.
- **Days:** a local day is the midnight of `Store.Now().Location()`. That is `time.Local` in the app, and the fixture clock's zone in tests.
- **Exact UI strings:**
  - the page `Usage`;
  - the presets `Today`, `7 days`, `30 days`, `All`;
  - `est. API cost`, `tokens`, `% from cache`, `calls unpriced`;
  - the progress texts `Reading transcripts…` and `Reading N of M transcripts`;
  - the chart captions `COST BY DAY` and `COST BY WEEK`;
  - the tabs `Projects`, `Sessions`, `Models`;
  - `Open Transcript`;
  - the chip `Project: <shortPath>  ✕`;
  - `No usage yet.`, `No usage in this period.`;
  - the toast `N transcripts could not be read`.
- **Look:** follow DESIGN.md (Braun Instrument).
  - Bars and scales are in ink; selection is graphite.
  - There is no orange on Usage, because orange marks only what can be cleaned up.
  - Figures use `FontFeatures("tnum")`.
- **Every commit:**
  - It ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`, with no session URLs.
  - Before it, `gofmt -l .` prints nothing, `go vet ./...` exits 0, and `CGO_ENABLED=1 go test -race ./...` exits 0.

## Review Focus

Each of these failure modes gets its own test:

1. **A Rescan while a scan runs.** The second scan waits for the first and then runs once; two scans never touch the `UsageScan` at once. Test: `TestUsageRescanDuringScan`, Task 4.
2. **A local day in a zone other than UTC.** At +07:00, a call at 23:59:59.900Z falls on the next local day. Test: `TestUsageLocalDay`, Task 3.
3. **A transcript that vanishes mid-scan.** The scan drops it; it does not count as unreadable, so no toast. Test: `TestUsageFileVanishes`, Task 3.
4. **Subagents of a session whose main transcript is gone.** They still count, under that session id, with the subagent's cwd as project. Test: `TestUsageOrphanSubagent`, Task 3.
5. **A usage line of megabytes.** It is read whole: no `bufio.Scanner` limit. Test: `TestUsageFileLongLine`, Task 2.

## Fixture figures (used across tasks)

`fixture.Now` is 2026-10-26 12:00 UTC.

| Call | Session, cwd | Day | Model | Tokens | Cost |
|---|---|---|---|---|---|
| o1 (3 lines: partial, final, final) | Old, `/Users/me/proj` | 10-20 | claude-opus-5-5 | in 100,000; out 200,000; read 1,000,000 | $4.60 |
| o2 (5m/1h split) | Old | 10-20 | claude-opus-5-5 | in 10,000; 5m 100,000; 1h 200,000 | $2.14 |
| o3 (fast, US) | Old | 10-10 | claude-opus-5-5 | in 100,000; out 100,000 | $5.28 |
| o4 | Old | — | `<synthetic>` | skipped | — |
| o5 | Old | 10-21 | claude-sonnet-5-5 | in 1,000,000 | $2.00 |
| o5 advisor | Old | 10-21 | claude-opus-5-5 | in 100,000; out 50,000 | $1.40 |
| o6 (no split, web) | Old | 09-01 | claude-sonnet-5-5 | 5m 400,000; 100 searches | $2.00 |
| r1 (prompt > 100K) | Recent, `/Users/me/other` | 10-26 | claude-haiku-5-5 | in 50,000; out 10,000; read 100,000 | $0.055 |
| r2 | Recent | 10-26 | claude-future-9 | in 1,000; out 1,000 | unpriced |
| r3 (partial 23:59:59.9, final 00:00:00.1) | Recent | 10-25 | claude-sonnet-5 | out 1,000 | $0.01 |
| s1 (subagent: partial in agent-r1, final in agent-r1, partial copy in workflows/agent-r2) | Recent | 10-26 | claude-haiku-4-5-20251001 | in 1,000; out 400 | $0.003 |
| broken line in Recent | — | — | — | bad 1 | — |

Totals by period:

| Period | Cost | Shown |
|---|---|---|
| Today | 0.058 | `$0.06` |
| 7 days | 10.208 | `$10.21` |
| 30 days | 15.488 | `$15.49` |
| All | 17.488 | `$17.49` |

Over 30 days:
- **tokens:** 3,124,400, shown as `3.1M`;
- **from cache:** 1,100,000 × 100 / 2,762,000 = 39, shown as `39%`;
- **me/other:** $0.068, shown as `$0.07`.

---

## File Structure

| File | Responsibility |
|---|---|
| `store/prices.go` (new) | Price table, `PricesAsOf`, `callTokens`, `price` |
| `store/prices_test.go` (new) | Price rules |
| `store/usage.go` (new) | Reading a transcript (`usageFile`), `merge`, `UsageScan`, `Usage`, `(*Store).Usage`, buckets |
| `store/usage_test.go` (new) | File reading, dedupe, attribution, incremental scan |
| `internal/fixture/fixture.go` | `UsageLine`, usage lines in Old and Recent, Recent's subagents |
| `view/format.go`, `view/format_test.go` | `commas`, `money`, `tokenCount` |
| `view/usage.go` (new) | The Usage screen |
| `view/usage_test.go` (new) | Screen tests |
| `view/history.go` | `rangeOf` uses the new `midnight` |
| `view/app.go`, `view/icons.go`, `view/app_test.go` | Wiring: sidebar, page, header, Rescan |
| `view/screenshot_test.go`, `docs/screenshots/*`, `docs/roadmap.md` | Screenshots, roadmap tick |

---

### Task 1: The price table

**Files:**
- Create: `store/prices.go`
- Create: `store/prices_test.go`

**Interfaces:**
- Produces:
  - `type callTokens struct { Input, Output, CacheRead, CacheWrite5m, CacheWrite1h, WebSearches int64; Fast, US bool }`
  - `func price(model string, c callTokens) (float64, bool)`
  - `const PricesAsOf = "2026-10-27"`

- [ ] **Step 1: Write the failing test**

Create `store/prices_test.go`:

```go
package store

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPrice(t *testing.T) {
	M := int64(1_000_000)
	for _, c := range []struct {
		name  string
		model string
		tok   callTokens
		want  float64
	}{
		{"each rate", "claude-opus-5-5", callTokens{Input: M, Output: M, CacheRead: M, CacheWrite5m: M, CacheWrite1h: M}, 4 + 20 + 0.20 + 5 + 8},
		{"date suffix", "claude-haiku-4-5-20251001", callTokens{Input: M, Output: M}, 1 + 5},
		{"haiku 5.5 short prompt", "claude-haiku-5-5", callTokens{Input: 100_000, Output: M}, 0.01 + 0.50},
		{"haiku 5.5 long prompt counts cache", "claude-haiku-5-5", callTokens{Input: 50_000, CacheRead: 60_000, Output: M}, 0.025 + 0.003 + 2.50},
		{"fast doubles", "claude-opus-5-5", callTokens{Input: M, Fast: true}, 8},
		{"fast only on fast models", "claude-sonnet-5-5", callTokens{Input: M, Fast: true}, 2},
		{"us adds a tenth", "claude-sonnet-5-5", callTokens{Input: M, US: true}, 2.2},
		{"us and fast stack", "claude-opus-5-5", callTokens{Input: M, Fast: true, US: true}, 8.8},
		{"us not on 4.5", "claude-sonnet-4-5", callTokens{Input: M, US: true}, 3},
		{"fable 5.1 cache read", "claude-fable-5-1", callTokens{CacheRead: M}, 0.25},
		{"web search", "claude-sonnet-5", callTokens{WebSearches: 1000}, 10},
	} {
		got, ok := price(c.model, c.tok)
		if !ok || !near(got, c.want) {
			t.Errorf("%s: price = %v, %v; want %v", c.name, got, ok, c.want)
		}
	}
	if _, ok := price("claude-future-9", callTokens{Input: 1}); ok {
		t.Error("an unknown model has a price")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./store -run TestPrice`
Expected: a build failure, `undefined: callTokens`.

- [ ] **Step 3: Write `store/prices.go`**

```go
package store

import "regexp"

// PricesAsOf is the day the price table was read from
// https://platform.claude.com/docs/en/about-claude/pricing.
const PricesAsOf = "2026-10-27"

// rates are USD per million tokens.
type rates struct{ input, write5m, write1h, read, output float64 }

var prices = map[string]rates{
	"claude-fable-5-1":  {10, 12.50, 20, 0.25, 50},
	"claude-fable-5":    {10, 12.50, 20, 1, 50},
	"claude-opus-5-5":   {4, 5, 8, 0.20, 20},
	"claude-opus-5":     {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-8":   {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-7":   {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-6":   {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-5":   {5, 6.25, 10, 0.50, 25},
	"claude-sonnet-5-5": {2, 2.50, 4, 0.10, 10},
	"claude-sonnet-5":   {2, 2.50, 4, 0.20, 10},
	"claude-sonnet-4-6": {3, 3.75, 6, 0.30, 15},
	"claude-sonnet-4-5": {3, 3.75, 6, 0.30, 15},
	"claude-haiku-5-5":  {0.10, 0.125, 0.20, 0.01, 0.50},
	"claude-haiku-4-5":  {1, 1.25, 2, 0.10, 5},
}

// haiku55Long are Claude Haiku 5.5's rates for a prompt over 100,000 tokens.
var haiku55Long = rates{0.50, 0.625, 1, 0.05, 2.50}

// fastModels run fast mode at twice every rate.
var fastModels = map[string]bool{"claude-opus-5-5": true, "claude-opus-5": true, "claude-opus-4-8": true}

// noGeo are the models before Claude 4.6: US-only inference costs them
// nothing more.
var noGeo = map[string]bool{"claude-opus-4-5": true, "claude-sonnet-4-5": true, "claude-haiku-4-5": true}

const webSearch = 10.0 / 1000 // USD a search

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// callTokens are the counts a call is priced on.
type callTokens struct {
	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h, WebSearches int64
	Fast bool // speed "fast"
	US   bool // inference_geo "us"
}

// price is a call's cost at API list prices, in USD, and false for a model
// the table doesn't have.
func price(model string, c callTokens) (float64, bool) {
	m := dateSuffix.ReplaceAllString(model, "")
	r, ok := prices[m]
	if !ok {
		return 0, false
	}
	// Haiku 5.5 prices each request by its prompt: input, cache reads and writes.
	if m == "claude-haiku-5-5" && c.Input+c.CacheRead+c.CacheWrite5m+c.CacheWrite1h > 100_000 {
		r = haiku55Long
	}
	k := 1.0
	if c.Fast && fastModels[m] {
		k *= 2
	}
	if c.US && !noGeo[m] {
		k *= 1.1
	}
	t := float64(c.Input)*r.input + float64(c.Output)*r.output + float64(c.CacheRead)*r.read +
		float64(c.CacheWrite5m)*r.write5m + float64(c.CacheWrite1h)*r.write1h
	return t/1e6*k + float64(c.WebSearches)*webSearch, true
}
```

- [ ] **Step 4: Run it and confirm it passes**

Run: `go test ./store -run TestPrice -v`
Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add store/prices.go store/prices_test.go
git commit -m "feat(store): Claude API price table

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Reading the calls of one transcript

**Files:**
- Create: `store/usage.go`
- Create: `store/usage_test.go`
- Modify: `internal/fixture/fixture.go`:
  - add `UsageLine` and the usage lines;
  - append them to Old's and Recent's transcripts;
  - add Recent's two subagent files.

**Interfaces:**
- Consumes: `callTokens` and `price` from Task 1.
- Produces:
  - `type call struct { key string; time time.Time; model string; callTokens }`
  - `type usageFile struct { calls []call; index map[string]int; cwd string; bad int }`
  - `func (f *usageFile) read(r io.Reader) (int64, error)`, which returns the bytes of the whole lines read
  - `func merge(a, b call) call`
  - `fixture.UsageLine(cwd, ts, id, model, usage string) string`, whose keys are `msg-<id>/req-<id>`

- [ ] **Step 1: Add the fixture lines**

In `internal/fixture/fixture.go`, add this after `lateSession`:

```go
// UsageLine is an assistant line with a usage record, as Claude Code writes
// one, keyed msg-<id>/req-<id>. Its content is empty, so a transcript shows
// nothing more for it.
func UsageLine(cwd, ts, id, model, usage string) string {
	return `{"type":"assistant","cwd":"` + cwd + `","timestamp":"` + ts + `","requestId":"req-` + id +
		`","message":{"id":"msg-` + id + `","role":"assistant","model":"` + model + `","content":[],"usage":` + usage + `}}`
}

// oldUsage are Old's calls: one written as three lines (a streaming partial,
// then the final usage twice), a 5m/1h cache split, fast mode with US-only
// inference, a synthetic message, an advisor call, and cache writes without
// the split with web searches.
var oldUsage = strings.Join([]string{
	UsageLine("/Users/me/proj", "2026-10-20T10:00:00.000Z", "o1", "claude-opus-5-5", `{"input_tokens":100000,"output_tokens":50000,"cache_read_input_tokens":1000000,"cache_creation_input_tokens":0}`),
	UsageLine("/Users/me/proj", "2026-10-20T10:00:01.000Z", "o1", "claude-opus-5-5", `{"input_tokens":100000,"output_tokens":200000,"cache_read_input_tokens":1000000,"cache_creation_input_tokens":0}`),
	UsageLine("/Users/me/proj", "2026-10-20T10:00:02.000Z", "o1", "claude-opus-5-5", `{"input_tokens":100000,"output_tokens":200000,"cache_read_input_tokens":1000000,"cache_creation_input_tokens":0}`),
	UsageLine("/Users/me/proj", "2026-10-20T11:00:00.000Z", "o2", "claude-opus-5-5", `{"input_tokens":10000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":300000,"cache_creation":{"ephemeral_5m_input_tokens":100000,"ephemeral_1h_input_tokens":200000}}`),
	UsageLine("/Users/me/proj", "2026-10-10T09:00:00.000Z", "o3", "claude-opus-5-5", `{"input_tokens":100000,"output_tokens":100000,"speed":"fast","inference_geo":"us"}`),
	UsageLine("/Users/me/proj", "2026-10-20T12:00:00.000Z", "o4", "<synthetic>", `{"input_tokens":0,"output_tokens":0}`),
	UsageLine("/Users/me/proj", "2026-10-21T09:00:00.000Z", "o5", "claude-sonnet-5-5", `{"input_tokens":1000000,"output_tokens":0,"iterations":[{"type":"message","input_tokens":1000000,"output_tokens":0},{"type":"advisor_message","model":"claude-opus-5-5","input_tokens":100000,"output_tokens":50000}]}`),
	UsageLine("/Users/me/proj", "2026-09-01T09:00:00.000Z", "o6", "claude-sonnet-5-5", `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":400000,"server_tool_use":{"web_search_requests":100}}`),
}, "\n") + "\n"

// recentUsage are Recent's calls: a Haiku 5.5 prompt over 100K tokens, a
// model with no price, a call whose partial is a tenth of a second before
// midnight and its final copy just after, and a cut-off line.
var recentUsage = strings.Join([]string{
	UsageLine("/Users/me/other", "2026-10-26T09:00:00.000Z", "r1", "claude-haiku-5-5", `{"input_tokens":50000,"output_tokens":10000,"cache_read_input_tokens":100000}`),
	UsageLine("/Users/me/other", "2026-10-26T09:30:00.000Z", "r2", "claude-future-9", `{"input_tokens":1000,"output_tokens":1000}`),
	UsageLine("/Users/me/other", "2026-10-25T23:59:59.900Z", "r3", "claude-sonnet-5", `{"input_tokens":0,"output_tokens":5}`),
	UsageLine("/Users/me/other", "2026-10-26T00:00:00.100Z", "r3", "claude-sonnet-5", `{"input_tokens":0,"output_tokens":1000}`),
	`{"type":"assistant","message":{"usage":{"input_tokens":`,
}, "\n") + "\n"

// subPartial is a subagent's call as streaming starts; subFinal is the same
// call done. A resumed subagent's file can hold the partial copy alone.
var (
	subPartial = UsageLine("/Users/me/other", "2026-10-26T08:00:00.000Z", "s1", "claude-haiku-4-5-20251001", `{"input_tokens":1000,"output_tokens":4}`)
	subFinal   = UsageLine("/Users/me/other", "2026-10-26T08:00:01.000Z", "s1", "claude-haiku-4-5-20251001", `{"input_tokens":1000,"output_tokens":400}`)
)
```

In `New`'s `files` map:
- change the Old entry to `oldSession + oldUsage`;
- change the Recent entry to its current line, then `+ "\n" + recentUsage`;
- add the two subagent files:

```go
		Proj + "/" + Old + ".jsonl":                    oldSession + oldUsage,
```

```go
		Other + "/" + Recent + ".jsonl":                `{"type":"user","cwd":"/Users/me/other","timestamp":"2026-10-26T11:58:00.000Z","message":{"role":"user","content":"Recent thing"}}` + "\n" + recentUsage,
		Other + "/" + Recent + "/subagents/agent-r1.jsonl":           subPartial + "\n" + subFinal + "\n",
		Other + "/" + Recent + "/subagents/workflows/agent-r2.jsonl": subPartial + "\n",
```

Leave Old's `subagents/agent-a1.jsonl` as `"{}\n"`. `TestSessions` checks that Old has 1 subagent and checks its exact size.

- [ ] **Step 2: Write the failing tests**

Create `store/usage_test.go`:

```go
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
```

- [ ] **Step 3: Run them and confirm they fail**

Run: `go test ./store -run 'TestUsageFile|TestMerge'`
Expected: a build failure, `undefined: usageFile`.

- [ ] **Step 4: Write `store/usage.go`**

```go
package store

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"strconv"
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
	if json.Unmarshal(b, &x) != nil {
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
	n := 0
	for _, it := range m.Usage.Iterations {
		if it.Type != "advisor_message" {
			continue
		}
		k := ""
		if key != "" {
			k = key + ":advisor:" + strconv.Itoa(n)
		}
		n++
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
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./store -run 'TestUsageFile|TestMerge' -v`
Expected: PASS.

- [ ] **Step 6: Run the whole suite, since the fixture changed**

Run: `go vet ./... && CGO_ENABLED=1 go test -race ./...`
Expected: exit 0.

If a test that counts sizes, messages, sessions or Cleanup groups fails because Old and Recent grew, read the failure first. Update the expectation only if the new value is the honest one, and say so in the report. Do not change `agent-a1.jsonl`.

- [ ] **Step 7: Commit**

```bash
gofmt -l .
git add store/usage.go store/usage_test.go internal/fixture/fixture.go
git commit -m "feat(store): read the calls of a transcript, deduped

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Scanning every transcript, incrementally, into buckets

**Files:**
- Modify: `store/usage.go`:
  - add the imports `errors`, `io/fs`, `maps`, `os`, `path/filepath`, `slices`, `strings` and `syscall`;
  - append the new code.
- Test: `store/usage_test.go`

**Interfaces:**
- Consumes: `usageFile`, `call`, `merge` (Task 2); `price` (Task 1); `uuidRe` (`store/sessions.go`).
- Produces:
  - `type Usage struct { SessionID, Project string; Day time.Time; Model string; Calls, Fast int; Input, Output, CacheRead, CacheWrite5m, CacheWrite1h, WebSearches int64; Cost float64; Unpriced int }`
  - `func (u *Usage) Add(b Usage)`
  - `func (u Usage) Tokens() int64`
  - `type UsageScan struct` (zero value ready; unexported fields `files` and `open`)
  - `func (s *Store) Usage(scan *UsageScan, progress func(done, total int)) (buckets []Usage, bad, unreadable int, err error)`
  - The buckets are sorted by Day, then SessionID, then Model.

- [ ] **Step 1: Write the failing tests**

Append to `store/usage_test.go`, adding `"errors"`, `"io/fs"` and `"slices"` to its imports:

```go
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
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./store -run 'TestUsage'`
Expected: a build failure, `undefined: UsageScan`.

- [ ] **Step 3: Append the scan to `store/usage.go`**

```go
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
		b.Add(Usage{Calls: 1, Input: c.Input, Output: c.Output, CacheRead: c.CacheRead,
			CacheWrite5m: c.CacheWrite5m, CacheWrite1h: c.CacheWrite1h, WebSearches: c.WebSearches})
		if c.Fast {
			b.Fast++
		}
		if cost, ok := price(c.model, c.callTokens); ok {
			b.Cost += cost
		} else {
			b.Unpriced++
		}
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
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./store -run 'TestUsage|TestMerge|TestPrice' -v`
Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add store/usage.go store/usage_test.go
git commit -m "feat(store): scan usage incrementally into session-day-model buckets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: The Usage screen: header, period, project filter, tabs

**Files:**
- Modify: `view/format.go`, `view/format_test.go` (add `commas`, `money`, `tokenCount`)
- Create: `view/usage.go`
- Create: `view/usage_test.go`
- Modify: `view/history.go` (`rangeOf` calls `midnight`)
- Modify: `view/app.go`:
  - add the field `usage usage` after `history history`;
  - in `New`: `a.usage.init()`;
  - in `Rescan`: `a.usage.load(a)`;
  - add the sidebar item after History, `case "usage"` in `View`, and `case "usage"` in `header`.
- Modify: `view/icons.go` (add `iconUsage`)
- Modify: `view/app_test.go` (add `"Usage"` to the sidebar labels)

**Interfaces:**
- Consumes:
  - `store.Usage`, `Add`, `Tokens`, `store.UsageScan`, `(*Store).Usage`, `store.PricesAsOf` (Task 3);
  - `rangeOf`, `dateRange`, `datePresets` and the presets (`view/history.go`);
  - `sessions.find`, `sessionName`, `shortPath`, `scale`, `face`, `head`, `blank`, `plural`.
- Produces:
  - `type usage struct` with fields `buckets`, `loaded`, `gen`, `preset`, `project`, `tab`, `sum usageSum`;
  - `func (u *usage) chart(c *ui.Context, pal *palette)`, a stub that Task 5 replaces;
  - `func midnight(t time.Time) time.Time`;
  - `usageSum` with fields `total store.Usage`, `byDay map[string]float64`, `first`, `last time.Time`, `weekly bool`, and `projects`, `sessions`, `models []usageRow`.

- [ ] **Step 1: Write the failing tests**

Append to `view/format_test.go`:

```go
func TestUsageFormats(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 15086: "15,086", 1234567: "1,234,567"} {
		if got := commas(in); got != want {
			t.Errorf("commas(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{0: "$0.00", 0.004: "<$0.01", 0.058: "$0.06", 15.488: "$15.49", 1234.5: "$1,234.50"} {
		if got := money(in); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int64]string{950: "950", 12345: "12.3K", 3124400: "3.1M", 820_400_000: "820.4M", 2_100_000_000: "2.1B"} {
		if got := tokenCount(in); got != want {
			t.Errorf("tokenCount(%d) = %q, want %q", in, got, want)
		}
	}
}
```

Create `view/usage_test.go`:

```go
package view

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/internal/fixture"
	"github.com/lang315/claude-manager/store"
)

func TestUsageHeader(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	if want := "$15.49 est. API cost · 3.1M tokens · 39% from cache · 1 call unpriced · 1 unreadable line"; !tt.HasText(want) {
		t.Errorf("header for 30 days; texts %q", tt.Texts())
	}
	for preset, want := range map[int]string{presetToday: "$0.06 est. API cost", preset7Days: "$10.21 est. API cost", presetAll: "$17.49 est. API cost"} {
		a.usage.preset = preset
		tt.Frame()
		if !tt.HasText(want) {
			t.Errorf("preset %d: want %q; texts %q", preset, want, tt.Texts())
		}
	}
}

func TestUsageProjectFilter(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	must(t, tt.Click("me/other"))
	tt.Frame()
	if a.usage.project != "/Users/me/other" || !tt.HasText("$0.07 est. API cost") {
		t.Fatalf("filter %q; texts %q", a.usage.project, tt.Texts())
	}
	must(t, tt.Click("Project: me/other  ✕"))
	tt.Frame()
	if a.usage.project != "" || !tt.HasText("$15.49 est. API cost") {
		t.Errorf("the ✕ leaves the filter %q", a.usage.project)
	}
}

func TestUsageOpensTranscript(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	a.usage.tab = 1 // Sessions: the tab's label is also the sidebar's
	tt.Frame()
	must(t, tt.Click("Login bug fix"))
	must(t, tt.Click("Open Transcript"))
	if a.page != "sessions" || a.sessions.open == nil || a.sessions.open.ID != fixture.Old {
		t.Fatalf("page %q, open %v; want Old's transcript", a.page, a.sessions.open)
	}
}

func TestUsageModels(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	a.usage.tab = 2
	tt.Frame()
	for _, s := range []string{"claude-opus-5-5 · fast", "claude-future-9", "—"} {
		if !tt.HasText(s) {
			t.Errorf("%q not shown; texts %q", s, tt.Texts())
		}
	}
}

func TestUsageEmpty(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, tt.Click("Usage"))
	a.usage.preset, a.usage.project = presetToday, "/Users/me/proj"
	tt.Frame()
	if !tt.HasText("No usage in this period.") {
		t.Errorf("no empty period; texts %q", tt.Texts())
	}
	must(t, os.RemoveAll(filepath.Join(d.Root, "projects")))
	a.Rescan()
	tt.Frame()
	if !tt.HasText("No usage yet.") {
		t.Errorf("no empty state; texts %q", tt.Texts())
	}
}

// heldApp shows the app with its work queued, not run.
func heldApp(t *testing.T) (*App, *ui.Tester, *[]func()) {
	d := fixture.New(t)
	a := New(&store.Store{Root: d.Root, ClaudeJSON: d.ClaudeJSON, Now: func() time.Time { return fixture.Now }, Alive: fixture.Alive})
	var queued []func()
	a.Go = func(f func()) { queued = append(queued, f) }
	tt := ui.NewTester(a.View, 1200, 800)
	a.Update = tt.Update
	return a, tt, &queued
}

func runQueued(q *[]func()) {
	for len(*q) > 0 {
		f := (*q)[0]
		*q = (*q)[1:]
		f()
	}
}

func TestUsageShowsProgress(t *testing.T) {
	a, tt, q := heldApp(t)
	a.Rescan()
	must(t, tt.Click("Usage"))
	if !tt.HasText("Reading transcripts…") {
		t.Errorf("no progress; texts %q", tt.Texts())
	}
	runQueued(q)
	tt.Frame()
	if !tt.HasText("$15.49 est. API cost") {
		t.Errorf("figures after the scan; texts %q", tt.Texts())
	}
}

// A Rescan during a scan waits: one scan at a time, then one more.
func TestUsageRescanDuringScan(t *testing.T) {
	a, tt, q := heldApp(t)
	a.Rescan()
	a.Rescan()
	// A scan's result lands on the next frame, which starts the scan waiting.
	runQueued(q)
	tt.Frame()
	runQueued(q)
	tt.Frame()
	if a.usage.gen != 2 || a.usage.scanning {
		t.Errorf("gen %d, scanning %v; want 2 scans, done", a.usage.gen, a.usage.scanning)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestUsage'`
Expected: a build failure, `undefined: commas` / `a.usage undefined`.

- [ ] **Step 3: Add the formats to `view/format.go`**

Add the import `"math"` if it is missing, then:

```go
// commas writes n with thousands separators: 15,086.
func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// money is a cost in dollars and cents: $1,234.56, or <$0.01 for a cost
// under a cent.
func money(f float64) string {
	if f > 0 && f < 0.01 {
		return "<$0.01"
	}
	c := int64(math.Round(f * 100))
	return fmt.Sprintf("$%s.%02d", commas(c/100), c%100)
}

// tokenCount is a count of tokens, short: 950, 12.3K, 820.4M, 2.1B.
func tokenCount(n int64) string {
	switch {
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1e9)
}
```

Check that `view/format.go` already imports `fmt` and `strconv`, and add whichever is missing.

- [ ] **Step 4: Share `midnight` with History**

In `view/history.go`, in `rangeOf`, replace the line
`today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())`
with `today := midnight(now)`. Then add after `rangeOf`:

```go
// midnight is the start of t's day, in t's location.
func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
```

- [ ] **Step 5: Write `view/usage.go`**

```go
package view

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

// usage is the Usage screen: the tokens Claude Code used, and what they would
// cost on the API, by day, project, session and model.
type usage struct {
	scan        store.UsageScan // touched only by the scanning goroutine
	buckets     []store.Usage
	bad         int
	loaded      bool // a scan has landed
	scanning    bool
	again       bool // a Rescan came during a scan
	done, total int  // the scan's progress
	gen         int  // counts the scans landed

	preset  int    // in datePresets, Custom left out
	project string // the project filtered to, "" for all
	tab     int
	key     usageKey
	sum     usageSum

	projAt, sessAt     int // the rows chosen
	projList, sessList ui.ListState
	modelList          ui.ListState
}

var usageTabs = []string{"Projects", "Sessions", "Models"}

// usageKey is what sum was worked out for.
type usageKey struct {
	dates   dateRange
	project string
	gen     int
}

// usageRow sums the buckets of one project, session or model.
type usageRow struct {
	name     string    // the project's path, the session's id or the model
	project  string    // a session's project
	last     time.Time // a session's last day
	sessions int       // a project's sessions
	store.Usage
}

// usageSum is the screen's figures for one period and project.
type usageSum struct {
	total                      store.Usage
	byDay                      map[string]float64 // cost by local day, "2006-01-02"
	first, last                time.Time          // the days the chart spans
	weekly                     bool
	projects, sessions, models []usageRow
}

func (u *usage) init() {
	u.preset, u.key.gen, u.projAt, u.sessAt = preset30Days, -1, -1, -1
	u.projList.Selected = &u.projAt
	u.sessList.Selected = &u.sessAt
}

// load scans the transcripts off the main thread; a load during a scan runs
// once it lands, so one scan at a time uses u.scan.
func (u *usage) load(a *App) {
	if u.scanning {
		u.again = true
		return
	}
	u.scanning = true
	a.run(func() func() {
		progress := func(done, total int) {
			if done%100 == 0 || done == total {
				a.Update(func() { u.done, u.total = done, total })
			}
		}
		buckets, bad, unreadable, err := a.Store.Usage(&u.scan, progress)
		return func() {
			u.scanning, u.loaded = false, true
			u.buckets, u.bad = buckets, bad
			u.gen++
			switch {
			case err != nil:
				a.notice = err.Error()
			case unreadable > 0:
				a.notice = plural(unreadable, "transcript") + " could not be read"
			}
			if u.again {
				u.again = false
				u.load(a)
			}
		}
	})
}

// refresh works out the figures again when the period, the project or the
// buckets changed: one pass over the buckets, not one a frame.
func (u *usage) refresh(now time.Time) {
	k := usageKey{rangeOf(u.preset, now, "", ""), u.project, u.gen}
	if k == u.key {
		return
	}
	u.key, u.sessAt = k, -1
	s := usageSum{byDay: map[string]float64{}}
	projects, sessions, models := map[string]*usageRow{}, map[string]*usageRow{}, map[string]*usageRow{}
	inProject := map[string]map[string]bool{} // project: its sessions
	for _, b := range u.buckets {
		if !k.dates.keeps(b.Day) {
			continue
		}
		rowOf(projects, b.Project).Add(b) // every project, so another can be chosen
		if inProject[b.Project] == nil {
			inProject[b.Project] = map[string]bool{}
		}
		inProject[b.Project][b.SessionID] = true
		if k.project != "" && b.Project != k.project {
			continue
		}
		s.total.Add(b)
		if !b.Day.IsZero() {
			s.byDay[b.Day.Format(time.DateOnly)] += b.Cost
			if s.first.IsZero() || b.Day.Before(s.first) {
				s.first = b.Day
			}
		}
		r := rowOf(sessions, b.SessionID)
		r.project = b.Project
		if b.Day.After(r.last) {
			r.last = b.Day
		}
		r.Add(b)
		rowOf(models, b.Model).Add(b)
	}
	for name, r := range projects {
		r.sessions = len(inProject[name])
	}
	s.projects, s.sessions, s.models = sortRows(projects), sortRows(sessions), sortRows(models)
	s.last = midnight(now)
	if k.dates.on {
		s.first = k.dates.lo
	}
	s.weekly = !k.dates.on && !s.first.IsZero() && s.last.Sub(s.first) > 90*24*time.Hour
	u.sum = s
}

func rowOf(m map[string]*usageRow, name string) *usageRow {
	r := m[name]
	if r == nil {
		r = &usageRow{name: name}
		m[name] = r
	}
	return r
}

// sortRows orders rows by cost, then tokens, highest first.
func sortRows(m map[string]*usageRow) []usageRow {
	out := make([]usageRow, 0, len(m))
	for _, r := range m {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b usageRow) int {
		return cmp.Or(cmp.Compare(b.Cost, a.Cost), cmp.Compare(b.Tokens(), a.Tokens()), cmp.Compare(a.name, b.name))
	})
	return out
}

// subtitle is the header's line: the period's figures, or the scan's
// progress before the first lands.
func (u *usage) subtitle(now time.Time) string {
	if !u.loaded {
		if u.total == 0 {
			return "Reading transcripts…"
		}
		return fmt.Sprintf("Reading %s of %s transcripts", commas(int64(u.done)), commas(int64(u.total)))
	}
	u.refresh(now)
	t := u.sum.total
	s := money(t.Cost) + " est. API cost · " + tokenCount(t.Tokens()) + " tokens"
	if in := t.Input + t.CacheRead + t.CacheWrite5m + t.CacheWrite1h; in > 0 {
		s += fmt.Sprintf(" · %d%% from cache", t.CacheRead*100/in)
	}
	if t.Unpriced > 0 {
		s += " · " + plural(t.Unpriced, "call") + " unpriced"
	}
	if u.bad > 0 {
		s += " · " + plural(u.bad, "unreadable line")
	}
	return s
}

func (u *usage) view(c *ui.Context, a *App, pal *palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Segmented(c, &u.preset, datePresets[:presetCustom]...)
		ui.Spacer(c)
		if u.project != "" && ui.Button(c, "Project: "+shortPath(u.project)+"  ✕").Tooltip(u.project).Clicked() {
			u.project = ""
		}
	})
	u.refresh(a.Store.Now())
	switch {
	case !u.loaded:
		blank(c, pal, "Reading transcripts…")
		return
	case len(u.buckets) == 0:
		blank(c, pal, "No usage yet.")
		return
	case u.sum.total.Calls == 0:
		blank(c, pal, "No usage in this period.")
		return
	}
	u.chart(c, pal)
	ui.Tabs(c, &u.tab, usageTabs...)
	face(c, pal).Grow(1).MinHeight(0).Children(func() {
		switch u.tab {
		case 0:
			u.projectTable(c, pal)
		case 1:
			u.sessionTable(c, a, pal)
		case 2:
			u.modelTable(c)
		}
	})
}

// chart draws the cost by day. Task 5 fills it in.
func (u *usage) chart(c *ui.Context, pal *palette) {}

// num is a figure in a table cell.
func num(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontFeatures("tnum").SingleLine()
}

// costText is a row's cost, "—" when none of its calls has a price.
func costText(u store.Usage) string {
	if u.Cost == 0 && u.Unpriced > 0 {
		return "—"
	}
	return money(u.Cost)
}

func cents(f float64) int64 { return int64(f * 100) }

func (u *usage) projectTable(c *ui.Context, pal *palette) {
	rows := u.sum.projects
	cols := []ui.TableColumn{{Title: "Project", MinWidth: 160}, {Title: "Sessions", Width: 80, Align: ui.End},
		{Title: "Tokens", Width: 90, Align: ui.End}, {Title: "Cost", Width: 100, Align: ui.End}, {Title: "Share", Width: 160}}
	ui.Table(c, &u.projList, cols, len(rows), func(i, col int) {
		r := &rows[i]
		switch col {
		case 0:
			ui.Text(c, shortPath(r.name)).SingleLine().Tooltip(r.name)
		case 1:
			num(c, strconv.Itoa(r.sessions))
		case 2:
			num(c, tokenCount(r.Tokens()))
		case 3:
			num(c, costText(r.Usage))
		case 4:
			scale(c, pal, cents(r.Cost), 0, cents(rows[0].Cost))
		}
	}).Grow(1).MinHeight(0)
	// A click on a project filters the screen to it.
	if u.projAt >= 0 && u.projAt < len(rows) {
		u.project = rows[u.projAt].name
	}
	u.projAt = -1
}

func (u *usage) sessionTable(c *ui.Context, a *App, pal *palette) {
	rows := u.sum.sessions
	cols := []ui.TableColumn{{Title: "Session", MinWidth: 160}, {Title: "Project", Width: 130}, {Title: "Last day", Width: 100},
		{Title: "Tokens", Width: 90, Align: ui.End}, {Title: "Cost", Width: 100, Align: ui.End}}
	ui.Table(c, &u.sessList, cols, len(rows), func(i, col int) {
		r := &rows[i]
		switch col {
		case 0:
			name := r.name
			if x := a.sessions.find(r.name); x != nil {
				name = sessionName(x)
			}
			ui.Text(c, name).SingleLine()
		case 1:
			ui.Text(c, shortPath(r.project)).TextColor(pal.muted).SingleLine().Tooltip(r.project)
		case 2:
			day := "—"
			if !r.last.IsZero() {
				day = r.last.Format(time.DateOnly)
			}
			num(c, day)
		case 3:
			num(c, tokenCount(r.Tokens()))
		case 4:
			num(c, costText(r.Usage))
		}
	}).Grow(1).MinHeight(0)
	ui.Divider(c)
	ui.Row(c).PaddingX(16).PaddingY(10).Children(func() {
		var x *store.Session
		if u.sessAt >= 0 && u.sessAt < len(rows) {
			x = a.sessions.find(rows[u.sessAt].name)
		}
		if ui.Button(c, "Open Transcript").Disabled(x == nil).Clicked() {
			a.page = "sessions"
			a.sessions.openTranscript(a, *x)
		}
	})
}

func (u *usage) modelTable(c *ui.Context) {
	rows := u.sum.models
	cols := []ui.TableColumn{{Title: "Model", MinWidth: 180}, {Title: "Calls", Width: 70, Align: ui.End},
		{Title: "Input", Width: 80, Align: ui.End}, {Title: "Output", Width: 80, Align: ui.End},
		{Title: "Cache write", Width: 90, Align: ui.End}, {Title: "Cache read", Width: 90, Align: ui.End},
		{Title: "Cost", Width: 100, Align: ui.End}}
	ui.Table(c, &u.modelList, cols, len(rows), func(i, col int) {
		r := &rows[i]
		switch col {
		case 0:
			name := r.name
			if r.Fast > 0 {
				name += " · fast"
			}
			ui.Text(c, name).SingleLine()
		case 1:
			num(c, commas(int64(r.Calls)))
		case 2:
			num(c, tokenCount(r.Input))
		case 3:
			num(c, tokenCount(r.Output))
		case 4:
			num(c, tokenCount(r.CacheWrite5m+r.CacheWrite1h))
		case 5:
			num(c, tokenCount(r.CacheRead))
		case 6:
			num(c, costText(r.Usage))
		}
	}).Grow(1).MinHeight(0)
}
```

- [ ] **Step 6: Wire the screen into the app**

In `view/icons.go`:
- change the comment's list to `pie-chart, message-square, history, chart-column, sliders-horizontal, trash-2.`;
- after `iconHistory`, add:

```go
	iconUsage    = icon(`<path d="M3 3v16a2 2 0 0 0 2 2h16"/><path d="M18 17V9"/><path d="M13 17V5"/><path d="M8 17v-3"/>`)
```

In `view/app.go`:
- in `App`, after `history  history`, add `usage    usage`;
- in `New`, after `a.history.init()`, add `a.usage.init()`;
- in `Rescan`, after `a.history.load(a)`, add `a.usage.load(a)`;
- in `View`'s sidebar, after the History item, add:

```go
			ui.SidebarItem(c, "usage", iconUsage, "Usage")
```

- in `View`'s page switch, after the `"history"` case, add:

```go
			case "usage":
				a.usage.view(c, a, pal)
```

- in `header`'s switch, after the `"history"` case, add:

```go
	case "usage":
		title, sub = "Usage", a.usage.subtitle(a.Store.Now())
```

In `view/app_test.go`, change the label list to
`[]string{"Overview", "Sessions", "History", "Usage", "Config", "Cleanup"}`.

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `go test ./view -run 'TestUsage|TestSidebarChoosesPage|TestRangeOf' -v`
Expected: PASS.

If `Click("me/other")` doesn't choose a row, check `tt.Texts()`. The Projects tab shows `shortPath` names, and a click on a cell's text chooses its row, as `TestHistoryDetailAndCopy` shows. Fix the cause; do not drop the test.

- [ ] **Step 8: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/format.go view/format_test.go view/usage.go view/usage_test.go view/history.go view/app.go view/icons.go view/app_test.go
git commit -m "feat(view): Usage screen with period, project filter and tables

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The cost chart

**Files:**
- Modify: `view/usage.go` (replace the `chart` stub; add `bars`, `dollarStep`, `dollarTick`, `drawBars`)
- Test: `view/usage_test.go`

**Interfaces:**
- Consumes: `usageSum` (Task 4); `face`, `head`, `money` and `store.PricesAsOf`.
- Produces:
  - `func bars(byDay map[string]float64, first, last time.Time, weekly bool) []float64`
  - `func dollarStep(most float64) float64`

- [ ] **Step 1: Write the failing tests**

Append to `view/usage_test.go`, adding `"slices"` to its imports:

```go
func TestBars(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	byDay := map[string]float64{"2026-10-20": 6.74, "2026-10-21": 3.4, "2026-10-26": 0.058}
	got := bars(byDay, day(20), day(26), false)
	if want := []float64{6.74, 3.4, 0, 0, 0, 0, 0.058}; !slices.Equal(got, want) {
		t.Errorf("daily %v, want %v", got, want)
	}
	// 2026-10-19 and 10-26 are Mondays: from the 18th, the weeks start on
	// the 12th, the 19th and the 26th.
	got = bars(byDay, day(18), day(26), true)
	if len(got) != 3 || got[0] != 0 || got[1] != 6.74+3.4 || got[2] != 0.058 {
		t.Errorf("weekly %v, want [0 10.14 0.058]", got)
	}
}

func TestDollarStep(t *testing.T) {
	for most, want := range map[float64]float64{0.05: 0.02, 3.4: 1, 6.74: 2, 15.5: 5, 140: 50} {
		if got := dollarStep(most); got != want {
			t.Errorf("dollarStep(%v) = %v, want %v", most, got, want)
		}
	}
}

func TestUsageChartCaption(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Usage"))
	if !tt.HasText("COST BY DAY") || !tt.HasText("$15.49 over 30 days · prices of "+store.PricesAsOf) {
		t.Errorf("chart caption; texts %q", tt.Texts())
	}
	a.usage.buckets = append(a.usage.buckets, store.Usage{SessionID: fixture.Old, Project: "/Users/me/proj",
		Day: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Model: "claude-opus-5-5", Calls: 1, Cost: 1})
	a.usage.gen++
	a.usage.preset = presetAll
	tt.Frame()
	if !tt.HasText("COST BY WEEK") {
		t.Errorf("over 90 days, no weekly bars; texts %q", tt.Texts())
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestBars|TestDollarStep|TestUsageChartCaption'`
Expected: a build failure, `undefined: bars`.

- [ ] **Step 3: Replace the stub with the chart**

Add `"math"` to `view/usage.go`'s imports, then replace
`// chart draws the cost by day. Task 5 fills it in.` and the empty `chart` with:

```go
var periodText = []string{"today", "over 7 days", "over 30 days", "in all"}

// chart draws the period's cost, a bar a day, or a week when All spans more
// than 90 days.
func (u *usage) chart(c *ui.Context, pal *palette) {
	s := &u.sum
	vals := bars(s.byDay, s.first, s.last, s.weekly)
	title := "Cost by day"
	if s.weekly {
		title = "Cost by week"
	}
	face(c, pal).Children(func() {
		head(c, pal, title, money(s.total.Cost)+" "+periodText[u.preset]+" · prices of "+store.PricesAsOf)
		ui.Column(c).Padding(2, 16, 12, 16).Children(func() {
			ui.Box(c).Height(110).Label(title).Draw(func(pt *ui.Painter, r ui.Rect) {
				drawBars(pt, r, pal, vals)
			})
		})
	})
}

// bars returns the cost of each day from first to last, both local
// midnights, or of each week from the Monday on or before first when weekly.
func bars(byDay map[string]float64, first, last time.Time, weekly bool) []float64 {
	if first.IsZero() {
		return nil
	}
	if weekly {
		for first.Weekday() != time.Monday {
			first = first.AddDate(0, 0, -1)
		}
	}
	var out []float64
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if !weekly || len(out) == 0 || d.Weekday() == time.Monday {
			out = append(out, 0)
		}
		out[len(out)-1] += byDay[d.Format(time.DateOnly)]
	}
	return out
}

// dollarStep is a round amount, 1, 2 or 5 times a power of ten, that puts at
// most four steps under most.
func dollarStep(most float64) float64 {
	for unit := 0.01; ; unit *= 10 {
		for _, m := range []float64{1, 2, 5} {
			if s := math.Round(m*unit*100) / 100; most/s <= 4 {
				return s
			}
		}
	}
}

// dollarTick is a tick's label: whole dollars bare, cents when the step has
// them.
func dollarTick(v float64) string {
	if v == math.Trunc(v) {
		return "$" + commas(int64(v))
	}
	return fmt.Sprintf("$%.2f", v)
}

// drawBars draws costs as ink bars over a baseline, with ticks at round
// dollar amounts at the left.
func drawBars(pt *ui.Painter, r ui.Rect, pal *palette, vals []float64) {
	const axisW = 52
	plot := ui.Rect{X: r.X + axisW, Y: r.Y + 6, W: r.W - axisW, H: r.H - 10}
	pt.Fill(ui.Rect{X: plot.X, Y: plot.Y + plot.H, W: plot.W, H: 1}, pal.ink, 0)
	most := 0.0
	for _, v := range vals {
		most = max(most, v)
	}
	if most <= 0 {
		return
	}
	step := dollarStep(most)
	top := math.Ceil(most/step) * step
	y := func(v float64) float32 { return plot.Y + plot.H - plot.H*float32(v/top) }
	for i := 0; float64(i)*step <= top+step/2; i++ {
		v := float64(i) * step
		yy := y(v)
		pt.Line(plot.X-5, yy, plot.X, yy, 1, pal.ink)
		label := ui.Span{Text: dollarTick(v), Size: 11, Color: pal.muted}
		w, h := pt.MeasureText(0, label)
		pt.RichText(plot.X-9-w, yy-h/2, 0, label)
	}
	bw := plot.W / float32(len(vals))
	gap := min(bw*0.25, 3)
	for i, v := range vals {
		if v <= 0 {
			continue
		}
		h := max(plot.H*float32(v/top), 1)
		pt.Fill(ui.Rect{X: plot.X + float32(i)*bw + gap/2, Y: plot.Y + plot.H - h, W: max(bw-gap, 1), H: h}, pal.ink, 0)
	}
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./view -run 'TestBars|TestDollarStep|TestUsage' -v`
Expected: PASS.

`TestBars` compares floats with `slices.Equal`. The sums in it are exact as written, because each day has a single value. If the weekly case trips on rounding, compare with `math.Abs(a-b) < 1e-9`.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/usage.go view/usage_test.go
git commit -m "feat(view): Usage's cost-by-day chart

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Screenshots and the roadmap

**Files:**
- Modify: `view/screenshot_test.go` (the `shots` list)
- Create: `docs/screenshots/usage-light.png`, `docs/screenshots/usage-dark.png`
- Modify: the `docs/screenshots/*` that the bigger fixture changes (overview, sessions, cleanup)
- Modify: `docs/roadmap.md`

- [ ] **Step 1: Add the shot**

In `view/screenshot_test.go`'s `shots`, after the `history` entry, add:

```go
		{"usage", "Usage", nil},
```

- [ ] **Step 2: Write the PNGs from the fixture**

```bash
D=$(mktemp -d) && SCREENSHOT_DIR=$D go test ./view -run TestScreenshots -count=1 && \
for f in docs/screenshots/*-light.png; do n=$(basename $f -light.png); cp $D/$n-1100.png $f; done && \
for f in docs/screenshots/*-dark.png; do n=$(basename $f -dark.png); cp $D/$n-dark.png $f; done && \
cp $D/usage-1100.png docs/screenshots/usage-light.png && cp $D/usage-dark.png docs/screenshots/usage-dark.png && \
git status --short docs/screenshots
```

Expected: exit 0. `git status` lists the new usage PNGs and the PNGs whose image changed with the fixture.

Open `usage-light.png` and `usage-dark.png` and check:
- the header reads `$15.49 est. API cost · 3.1M tokens · 39% from cache · …`;
- the bars are ink, with dollar ticks;
- the Projects tab shows me/proj and me/other with their share bars;
- there is no orange.

Open each other changed PNG and check that only figures moved.

- [ ] **Step 3: Tick the roadmap**

In `docs/roadmap.md`, change
`- [ ] Token and cost statistics per session, project and day (from \`message.usage\`).`
to `- [x] …` with the rest of the line unchanged.

- [ ] **Step 4: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/screenshot_test.go docs/screenshots docs/roadmap.md
git commit -m "docs: Usage screenshots, tick the roadmap item

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
