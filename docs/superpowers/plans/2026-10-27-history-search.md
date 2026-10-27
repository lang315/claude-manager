# History Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A History screen that searches every prompt in `history.jsonl`, filtered by project and date, to copy a prompt back or open its session.

**Architecture:** `store.History()` reads `history.jsonl` into `[]Prompt`, newest first. `view/history.go` keeps the indexes of the prompts that the search, project and date filters keep. It works them out again only when the filter key changes, and draws them in a `ui.Table`. The detail face beside the table copies `Prompt.Expand()` and opens the session through the Sessions screen's transcript.

**Tech Stack:**
- Go 1.27;
- MyGo native UI (`github.com/egoist/mygo/ui`, through `replace => ../mygo`): `Table`, `ListState`, `SearchField`, `Select`, `Segmented`, `Field`, `TextInput`, `Collapsible`, `Split`;
- `ui.Tester` for view tests.

**Spec:** `docs/superpowers/specs/2026-10-27-history-search-design.md`

## Global Constraints

- **Never write to `history.jsonl`.** The app writes nothing in `~/.claude`. It only moves items to the Trash, and History moves nothing.
- **Tests never read the real `~/.claude`.** Use `fixture.New`, or a `t.TempDir()` root.
- **Look:** follow DESIGN.md (Braun Instrument).
  - No orange on History; orange marks only what can be cleaned up.
  - Selection is graphite (the theme's accent, already set).
  - Figures use `FontFeatures("tnum")`.
- **Code style:** match the surrounding code. Doc comments say what and why, in full sentences. No new dependencies.
- **Exact UI strings:**
  - `History`;
  - `Search prompts`, `All projects`;
  - `Today`, `7 days`, `30 days`, `All`, `Custom`;
  - `From`, `To`, `YYYY-MM-DD`, `Use YYYY-MM-DD`;
  - `No prompt history yet.`, `No prompt matches.`, `Choose a prompt to see it whole.`;
  - `PROMPT` (the legend, from `head`), `Copy`, `Copied`, `Open session`, `Session no longer on disk`;
  - `N paste(s) no longer stored`, built with `plural(n, "paste") + " no longer stored"`.
- **Every commit** ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. No session URLs.
- **Gate before each commit:**
  - `go vet ./...` exits 0;
  - `CGO_ENABLED=1 go test -race ./...` exits 0;
  - `gofmt -l .` prints nothing.

## Review Focus

1. **One huge line.** An inline paste can be megabytes on a single line. `History` must read it whole, with no `bufio.Scanner` token limit. The test is in Task 1.
2. **No trailing newline.** If the last line has no `\n` (Claude Code is mid-append), it is still read. The test is in Task 1.
3. **The chosen row gets filtered out.** The detail must let it go and say `Choose a prompt to see it whole.`. It must not show a stale row or panic on an out-of-range index. The test is in Task 4.
4. **Custom From later than To.** The table shows `No prompt matches.`. It must not panic or ignore the range. The test is in Task 3.
5. **A Rescan drops the project chosen.** The Select goes back to `All projects` instead of showing an empty table under a stale choice. The test is in Task 3.

---

## File Structure

| File | Responsibility |
|---|---|
| `store/history.go` (new) | `Prompt`, `Paste`, `(*Store).History`, `(Prompt).Expand` |
| `store/history_test.go` (new) | Store tests for History and Expand |
| `internal/fixture/fixture.go` | Adds `history.jsonl` and the `NoSession` id |
| `view/history.go` (new) | The History screen: filter logic, controls, table, detail |
| `view/history_test.go` (new) | Pure filter tests and Tester tests |
| `view/app.go` | `history` field, init, Rescan, sidebar item, page case, header |
| `view/icons.go` | `iconHistory` |
| `view/overview.go` | `screenOf`: `history.jsonl` opens History |
| `view/sessions.go` | `(*sessions).find(id)` |
| `view/app_test.go` | Sidebar lists History |
| `view/screenshot_test.go` | The `history` shot |
| `docs/screenshots/history-{light,dark}.png` (new), `overview-{light,dark}.png` | Regenerated |
| `docs/roadmap.md` | Ticks the v2 item |

---

### Task 1: Store reads `history.jsonl`

**Files:**
- Create: `store/history.go`
- Create: `store/history_test.go`
- Modify: `internal/fixture/fixture.go` (constants block; the `files` map in `New`)

**Interfaces:**
- Consumes: `Store.Root`, `fixture.New`, `newStore(t)` from `store/store_test.go` (returns `*Store, fixture.Dirs, *[]string`).
- Produces:
  - `type Prompt struct { Line int; Text string; Time time.Time; Project, SessionID string; Pastes []Paste; Lost int }`
  - `type Paste struct { ID int; Content string }`
  - `func (s *Store) History() (prompts []Prompt, bad int, err error)`
  - `func (p Prompt) Expand() string`
  - `fixture.NoSession = "77777777-7777-4777-8777-777777777777"`
  - the fixture's `history.jsonl`. Its prompts, newest first, by `Line`:

    | Line | Text | Notes |
    |---|---|---|
    | 2 | `Fix the login bug` | session `Old`, today |
    | 4 | `Review this:\n[Pasted text #1 +2 lines]` | session `Recent`, 3 days ago, project `/Users/me/other` |
    | 5 | `Use this [Pasted text #2 +40 lines] and [Image #1]` | session `NoSession`, 20 days ago, 1 lost paste |
    | 1 | `Old idea` | session `Old`, 60 days ago |
    | 6 | `No time prompt` | no timestamp, `/Users/me/other` |

    Line 3 is broken JSON.

- [ ] **Step 1: Add the fixture file**

In `internal/fixture/fixture.go`, add to the `const` block after `OrphanHistory`:

```go
	NoSession     = "77777777-7777-4777-8777-777777777777" // a prompt's session that is not on disk
```

Add this var after `lateSession`:

```go
// history is history.jsonl: prompts in two projects, newest on line 2, a
// broken line 3, an inline paste and one stored only by hash, an image
// placeholder and a prompt without a time.
var history = strings.Join([]string{
	`{"display":"Old idea","pastedContents":{},"timestamp":1787824800000,"project":"/Users/me/proj","sessionId":"` + Old + `"}`,
	`{"display":"Fix the login bug","pastedContents":{},"timestamp":1793008800000,"project":"/Users/me/proj","sessionId":"` + Old + `"}`,
	`{not json`,
	`{"display":"Review this:\n[Pasted text #1 +2 lines]","pastedContents":{"1":{"id":1,"type":"text","content":"func main() {\n}"},"3":{"id":3,"type":"text","content":"unused paste"}},"timestamp":1792746000000,"project":"/Users/me/other","sessionId":"` + Recent + `"}`,
	`{"display":"Use this [Pasted text #2 +40 lines] and [Image #1]","pastedContents":{"2":{"id":2,"type":"text","contentHash":"abc123"}},"timestamp":1791277200000,"project":"/Users/me/proj","sessionId":"` + NoSession + `"}`,
	`{"display":"No time prompt","pastedContents":{},"project":"/Users/me/other","sessionId":"` + Recent + `"}`,
}, "\n") + "\n"
```

The timestamps are, in UTC:
- `1787824800000` = 2026-08-27 10:00;
- `1793008800000` = 2026-10-26 10:00, the day of `fixture.Now`;
- `1792746000000` = 2026-10-23 09:00;
- `1791277200000` = 2026-10-06 09:00.

In `New`, add to the `files` map:

```go
		"history.jsonl":                                history,
```

- [ ] **Step 2: Write the failing store tests**

Create `store/history_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestHistory(t *testing.T) {
	s, _, _ := newStore(t)
	ps, bad, err := s.History()
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 {
		t.Errorf("bad = %d, want 1", bad)
	}
	var lines []int
	for _, p := range ps {
		lines = append(lines, p.Line)
	}
	if want := []int{2, 4, 5, 1, 6}; !slices.Equal(lines, want) {
		t.Fatalf("lines %v, want %v: newest first, no time last", lines, want)
	}
	p := ps[0]
	if p.Text != "Fix the login bug" || p.Project != "/Users/me/proj" || p.SessionID != fixture.Old || p.Time.UnixMilli() != 1793008800000 {
		t.Errorf("first prompt %+v", p)
	}
	review := ps[1]
	if want := []Paste{{1, "func main() {\n}"}, {3, "unused paste"}}; !slices.Equal(review.Pastes, want) || review.Lost != 0 {
		t.Errorf("pastes %+v lost %d, want %+v and 0", review.Pastes, review.Lost, want)
	}
	if ps[2].Lost != 1 || len(ps[2].Pastes) != 0 {
		t.Errorf("hash-only paste: pastes %+v lost %d, want none and 1", ps[2].Pastes, ps[2].Lost)
	}
	if !ps[4].Time.IsZero() {
		t.Errorf("no timestamp gives %v, want the zero time", ps[4].Time)
	}
}

func TestHistoryMissing(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	ps, bad, err := s.History()
	if err != nil || bad != 0 || len(ps) != 0 {
		t.Errorf("missing file: %d prompts, bad %d, err %v; want none", len(ps), bad, err)
	}
}

// A paste can be megabytes on one line, and Claude Code may be writing the
// last line: neither stops the read.
func TestHistoryLongLastLine(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", 2<<20)
	line := `{"display":"[Pasted text #1 +1 lines]","pastedContents":{"1":{"id":1,"type":"text","content":"` + big + `"}},"timestamp":1,"project":"/p","sessionId":"s"}`
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(line), 0o644); err != nil { // no trailing newline
		t.Fatal(err)
	}
	ps, bad, err := (&Store{Root: root}).History()
	if err != nil || bad != 0 || len(ps) != 1 || len(ps[0].Pastes) != 1 || len(ps[0].Pastes[0].Content) != len(big) {
		t.Fatalf("read %d prompts, bad %d, err %v", len(ps), bad, err)
	}
}

func TestExpand(t *testing.T) {
	p := Prompt{
		Text:   "a [Pasted text #1 +2 lines] b [Pasted text #2] c [Pasted text #3 +9 lines] [Image #1]",
		Pastes: []Paste{{1, "ONE"}, {2, "TWO"}, {4, "unused"}},
	}
	want := "a ONE b TWO c [Pasted text #3 +9 lines] [Image #1]"
	if got := p.Expand(); got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
}
```

- [ ] **Step 3: Run the tests and confirm they fail**

Run: `go test ./store -run 'TestHistory|TestExpand'`
Expected: a build failure, `undefined: Paste` / `s.History undefined`.

- [ ] **Step 4: Write `store/history.go`**

```go
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
	"strconv"
	"time"
)

// Prompt is one line of history.jsonl: a prompt typed into Claude Code.
type Prompt struct {
	Line      int       // its line in history.jsonl, from 1: the prompt's key
	Text      string    // the prompt as typed, paste placeholders included
	Time      time.Time // zero when the line has no timestamp
	Project   string    // the working directory
	SessionID string
	Pastes    []Paste // the pastes kept in the line, by ID
	Lost      int     // pastes kept only by hash, which can't be read
}

// Paste is text pasted into a prompt, which shows in it as a placeholder.
type Paste struct {
	ID      int
	Content string
}

// History reads history.jsonl, newest first; prompts without a time come
// last. Lines that are not JSON are skipped and counted in bad. A missing
// file is no prompts and no error. It only reads: Claude Code appends to the
// file and recalls prompts from it.
func (s *Store) History() (prompts []Prompt, bad int, err error) {
	f, err := os.Open(filepath.Join(s.Root, "history.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f) // not a Scanner: a pasted text can make a line megabytes long
	for n := 1; ; n++ {
		line, rerr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if p, ok := parsePrompt(line); ok {
				p.Line = n
				prompts = append(prompts, p)
			} else {
				bad++
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, 0, rerr
		}
	}
	slices.SortStableFunc(prompts, func(a, b Prompt) int {
		if a.Time.IsZero() != b.Time.IsZero() {
			return cmp.Compare(btoi(a.Time.IsZero()), btoi(b.Time.IsZero()))
		}
		return b.Time.Compare(a.Time)
	})
	return prompts, bad, nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parsePrompt reads one line of history.jsonl.
func parsePrompt(line []byte) (Prompt, bool) {
	var r struct {
		Display        string `json:"display"`
		Timestamp      int64  `json:"timestamp"`
		Project        string `json:"project"`
		SessionID      string `json:"sessionId"`
		PastedContents map[string]struct {
			ID      int     `json:"id"`
			Type    string  `json:"type"`
			Content *string `json:"content"`
		} `json:"pastedContents"`
	}
	if err := json.Unmarshal(line, &r); err != nil {
		return Prompt{}, false
	}
	p := Prompt{Text: r.Display, Project: r.Project, SessionID: r.SessionID}
	if r.Timestamp > 0 {
		p.Time = time.UnixMilli(r.Timestamp)
	}
	for _, x := range r.PastedContents {
		switch {
		case x.Type != "" && x.Type != "text":
		case x.Content == nil:
			p.Lost++
		default:
			p.Pastes = append(p.Pastes, Paste{x.ID, *x.Content})
		}
	}
	slices.SortFunc(p.Pastes, func(a, b Paste) int { return cmp.Compare(a.ID, b.ID) })
	return p, true
}

// pastePlaceholder is how a pasted text shows in a prompt: [Pasted text #1
// +20 lines], or [Pasted text #1] for a short one.
var pastePlaceholder = regexp.MustCompile(`\[Pasted text #(\d+)(?: \+\d+ lines)?\]`)

// Expand returns the prompt with each paste placeholder replaced by the
// text pasted. Placeholders of lost pastes, and of images, stay.
func (p Prompt) Expand() string {
	return pastePlaceholder.ReplaceAllStringFunc(p.Text, func(m string) string {
		id, _ := strconv.Atoi(pastePlaceholder.FindStringSubmatch(m)[1])
		for _, x := range p.Pastes {
			if x.ID == id {
				return x.Content
			}
		}
		return m
	})
}
```

- [ ] **Step 5: Run the store tests**

Run: `go test ./store -run 'TestHistory|TestExpand' -v`
Expected: PASS for `TestHistory`, `TestHistoryMissing`, `TestHistoryLongLastLine` and `TestExpand`.

- [ ] **Step 6: Run the whole suite, since the fixture changed**

Run: `go vet ./... && CGO_ENABLED=1 go test -race ./...`
Expected: exit 0.

If an Overview or Scan test counts the fixture's top-level entries or its total size, it now sees `history.jsonl` too. Update that expectation to include it, and say so in the report.

- [ ] **Step 7: Commit**

```bash
gofmt -l .
git add store/history.go store/history_test.go internal/fixture/fixture.go
git commit -m "feat(store): read history.jsonl into prompts

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The filter logic, pure

**Files:**
- Create: `view/history.go`
- Create: `view/history_test.go`

**Interfaces:**
- Consumes: `store.Prompt`, `store.Paste` (Task 1); `containsFold` (`view/format.go`).
- Produces:
  - the constants `presetToday`, `preset7Days`, `preset30Days`, `presetAll` and `presetCustom` (0–4), and `var datePresets = []string{"Today", "7 days", "30 days", "All", "Custom"}`
  - `type dateRange struct { on bool; lo, hi time.Time }`
  - `func rangeOf(preset int, now time.Time, from, to string) dateRange`
  - `func parseDay(s string, loc *time.Location) (time.Time, string)`, whose message is `Use YYYY-MM-DD`
  - `func (r dateRange) keeps(t time.Time) bool`
  - `func matchPrompt(p *store.Prompt, kw string) bool`

- [ ] **Step 1: Write the failing tests**

Create `view/history_test.go`:

```go
package view

import (
	"testing"
	"time"

	"github.com/lang315/claude-manager/store"
)

func TestRangeOf(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	var open time.Time
	for _, c := range []struct {
		name     string
		preset   int
		from, to string
		want     dateRange
	}{
		{"today", presetToday, "", "", dateRange{true, day(10, 26), open}},
		{"7 days, today included", preset7Days, "", "", dateRange{true, day(10, 20), open}},
		{"30 days", preset30Days, "", "", dateRange{true, day(9, 27), open}},
		{"all", presetAll, "", "", dateRange{}},
		{"custom, both days whole", presetCustom, "2026-08-01", "2026-08-31", dateRange{true, day(8, 1), day(9, 1)}},
		{"custom, from open", presetCustom, "", "2026-08-31", dateRange{true, open, day(9, 1)}},
		{"custom, both open", presetCustom, "", "", dateRange{true, open, open}},
		{"custom, bad date turns it off", presetCustom, "2026-13-01", "", dateRange{}},
	} {
		got := rangeOf(c.preset, now, c.from, c.to)
		if got.on != c.want.on || !got.lo.Equal(c.want.lo) || !got.hi.Equal(c.want.hi) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestParseDay(t *testing.T) {
	if d, msg := parseDay(" 2026-08-01 ", time.UTC); msg != "" || !d.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("parseDay(2026-08-01) = %v, %q", d, msg)
	}
	if d, msg := parseDay("", time.UTC); msg != "" || !d.IsZero() {
		t.Errorf("an empty field is %v, %q; want open, no message", d, msg)
	}
	for _, bad := range []string{"2026-8-1", "26/10/2026", "2026-02-30", "yesterday"} {
		if _, msg := parseDay(bad, time.UTC); msg != "Use YYYY-MM-DD" {
			t.Errorf("parseDay(%q) message %q, want Use YYYY-MM-DD", bad, msg)
		}
	}
}

func TestDateRangeKeeps(t *testing.T) {
	lo := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	hi := lo.AddDate(0, 0, 1)
	r := dateRange{true, lo, hi}
	for _, c := range []struct {
		t    time.Time
		want bool
	}{
		{lo, true}, {hi.Add(-time.Millisecond), true}, {hi, false}, {lo.Add(-time.Millisecond), false}, {time.Time{}, false},
	} {
		if got := r.keeps(c.t); got != c.want {
			t.Errorf("keeps(%v) = %v, want %v", c.t, got, c.want)
		}
	}
	if !(dateRange{}).keeps(time.Time{}) {
		t.Error("All drops a prompt without a time")
	}
}

func TestMatchPrompt(t *testing.T) {
	p := &store.Prompt{Text: "Review THIS", Pastes: []store.Paste{{ID: 1, Content: "func main() {}"}}}
	for kw, want := range map[string]bool{"": true, "   ": true, "this": true, "FUNC MAIN": true, "nope": false} {
		if got := matchPrompt(p, kw); got != want {
			t.Errorf("matchPrompt(%q) = %v, want %v", kw, got, want)
		}
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestRangeOf|TestParseDay|TestDateRangeKeeps|TestMatchPrompt'`
Expected: a build failure, `undefined: rangeOf`.

- [ ] **Step 3: Write `view/history.go` with the filter logic**

```go
package view

import (
	"strings"
	"time"

	"github.com/lang315/claude-manager/store"
)

// The date filter's presets, in the order the Segmented shows them.
const (
	presetToday = iota
	preset7Days
	preset30Days
	presetAll
	presetCustom
)

var datePresets = []string{"Today", "7 days", "30 days", "All", "Custom"}

// dateRange is the span the date filter keeps. Off, it keeps every prompt;
// on, those with a time from lo up to hi, hi excluded, a zero bound leaving
// its side open. It is part of the filter key, so it stays comparable.
type dateRange struct {
	on     bool
	lo, hi time.Time
}

// rangeOf works out the date filter. A preset counts whole days back from
// today's midnight, in now's location (the local one in the app), today
// included; Custom spans from's day to to's, both whole. A Custom field that
// is not a date turns the filter off, so the table doesn't empty.
func rangeOf(preset int, now time.Time, from, to string) dateRange {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch preset {
	case presetToday:
		return dateRange{on: true, lo: today}
	case preset7Days:
		return dateRange{on: true, lo: today.AddDate(0, 0, -6)}
	case preset30Days:
		return dateRange{on: true, lo: today.AddDate(0, 0, -29)}
	case presetCustom:
		lo, msg1 := parseDay(from, now.Location())
		hi, msg2 := parseDay(to, now.Location())
		if msg1 != "" || msg2 != "" {
			return dateRange{}
		}
		if !hi.IsZero() {
			hi = hi.AddDate(0, 0, 1)
		}
		return dateRange{on: true, lo: lo, hi: hi}
	}
	return dateRange{}
}

// parseDay reads a Custom field: a YYYY-MM-DD date, its midnight in loc. An
// empty field is the zero time, and one that is not a date a message.
func parseDay(s string, loc *time.Location) (time.Time, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, ""
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, "Use YYYY-MM-DD"
	}
	return t, ""
}

// keeps reports whether a prompt of time t passes the date filter. A prompt
// without a time passes only when the filter is off.
func (r dateRange) keeps(t time.Time) bool {
	if !r.on {
		return true
	}
	return !t.IsZero() && (r.lo.IsZero() || !t.Before(r.lo)) && (r.hi.IsZero() || t.Before(r.hi))
}

// matchPrompt reports whether the search keyword is in the prompt or in one
// of its pastes, ignoring case. A blank keyword matches every prompt.
func matchPrompt(p *store.Prompt, kw string) bool {
	kw = strings.TrimSpace(kw)
	if kw == "" || containsFold(p.Text, kw) {
		return true
	}
	for _, x := range p.Pastes {
		if containsFold(x.Content, kw) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./view -run 'TestRangeOf|TestParseDay|TestDateRangeKeeps|TestMatchPrompt' -v`
Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/history.go view/history_test.go
git commit -m "feat(view): History's search and date filter logic

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: The History screen: sidebar, header, controls, table

**Files:**
- Modify: `view/history.go` (add the `history` type, `init`, `load`, `filter` and `view`)
- Modify: `view/app.go`:
  - the `App` struct: add `history history` after `sessions sessions`;
  - `New`: add `a.history.init()`;
  - `Rescan`: add `a.history.load(a)`;
  - `View`: the sidebar item, and `case "history"`;
  - `header`: `case "history"`.
- Modify: `view/icons.go`
- Modify: `view/overview.go` (`screenOf`)
- Modify: `view/app_test.go` (`TestSidebarChoosesPage`'s label list)
- Test: `view/history_test.go`

**Interfaces:**
- Consumes:
  - from Task 2: `rangeOf`, `parseDay`, `matchPrompt` and the presets;
  - from Task 1: `store.Prompt`, `(*store.Store).History`;
  - existing helpers: `a.run`, `tilde`, `shortPath`, `fmtTime`, `plural`, `blank`, `store.FirstLine`.
- Produces:
  - `type history struct`, with fields that later tasks use: `prompts []store.Prompt`, `shown []int`, `at int` (the row chosen in `shown`, -1 for none), `query`, `project`, `preset`, `from`, `to`;
  - `const allProjects = "All projects"`;
  - `func (h *history) init()`, `func (h *history) load(a *App)`, `func (h *history) view(c *ui.Context, a *App, pal *palette)`;
  - `func (h *history) chosen() *store.Prompt` (nil when none), which Task 4 uses;
  - `func (h *history) detail(c *ui.Context, a *App, pal *palette)`, a stub that Task 4 replaces.

- [ ] **Step 1: Write the failing Tester tests**

Append to `view/history_test.go`, adding `"os"`, `"path/filepath"` and `"github.com/lang315/claude-manager/internal/fixture"` to its imports:

```go
// shownLines are the history.jsonl lines of the prompts the table shows.
func shownLines(a *App) []int {
	var out []int
	for _, i := range a.history.shown {
		out = append(out, a.history.prompts[i].Line)
	}
	return out
}

func TestHistoryShowsPrompts(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	if !tt.HasText("5 prompts in 2 projects · 1 unreadable line") {
		t.Errorf("header; texts %q", tt.Texts())
	}
	for _, s := range []string{"Fix the login bug", "Review this:", "Old idea", "No time prompt", "5 prompts"} {
		if !tt.HasText(s) {
			t.Errorf("%q not shown; texts %q", s, tt.Texts())
		}
	}
	if got := shownLines(a); !slices.Equal(got, []int{2, 4, 5, 1, 6}) {
		t.Errorf("rows %v, want newest first", got)
	}
}

func TestHistorySearchesPastes(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	a.history.query = "FUNC MAIN"
	tt.Frame()
	if got := shownLines(a); !slices.Equal(got, []int{4}) {
		t.Errorf("search in a paste shows %v, want [4]", got)
	}
	a.history.query = "nothing like it"
	tt.Frame()
	if !tt.HasText("No prompt matches.") {
		t.Errorf("no empty state; texts %q", tt.Texts())
	}
}

func TestHistoryFilters(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	h := &a.history
	for _, c := range []struct {
		name     string
		project  string
		preset   int
		from, to string
		want     []int
	}{
		{"today", allProjects, presetToday, "", "", []int{2}},
		{"7 days", allProjects, preset7Days, "", "", []int{2, 4}},
		{"30 days", allProjects, preset30Days, "", "", []int{2, 4, 5}},
		{"all", allProjects, presetAll, "", "", []int{2, 4, 5, 1, 6}},
		{"custom august", allProjects, presetCustom, "2026-08-01", "2026-08-31", []int{1}},
		{"custom bad date keeps every row", allProjects, presetCustom, "2026-13-01", "", []int{2, 4, 5, 1, 6}},
		{"custom from after to", allProjects, presetCustom, "2026-09-01", "2026-08-01", nil},
		{"one project", "/Users/me/other", presetAll, "", "", []int{4, 6}},
		{"project and date", "/Users/me/other", preset7Days, "", "", []int{4}},
	} {
		h.project, h.preset, h.from, h.to = c.project, c.preset, c.from, c.to
		tt.Frame()
		if got := shownLines(a); !slices.Equal(got, c.want) {
			t.Errorf("%s: rows %v, want %v", c.name, got, c.want)
		}
	}
	h.project, h.preset, h.from = allProjects, presetCustom, "2026-13-01"
	tt.Frame()
	if !tt.HasText("Use YYYY-MM-DD") {
		t.Errorf("a bad date says nothing; texts %q", tt.Texts())
	}
}

// The Segmented and the Select drive the same filters as the fields.
func TestHistoryPresetClick(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	must(t, tt.Click("Today"))
	if got := shownLines(a); !slices.Equal(got, []int{2}) {
		t.Errorf("Today shows %v, want [2]", got)
	}
}

func TestHistoryRescanDropsGoneProject(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, tt.Click("History"))
	a.history.project = "/Users/me/other"
	tt.Frame()
	must(t, os.WriteFile(filepath.Join(d.Root, "history.jsonl"),
		[]byte(`{"display":"only","timestamp":1,"project":"/Users/me/proj","sessionId":"`+fixture.Old+`"}`+"\n"), 0o644))
	a.Rescan()
	tt.Frame()
	if a.history.project != allProjects || len(a.history.shown) != 1 {
		t.Errorf("after the project left: choice %q, %d rows; want All projects, 1", a.history.project, len(a.history.shown))
	}
}

func TestHistoryEmpty(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, os.Remove(filepath.Join(d.Root, "history.jsonl")))
	a.Rescan()
	tt.Frame()
	must(t, tt.Click("History"))
	if !tt.HasText("No prompt history yet.") {
		t.Errorf("no empty state; texts %q", tt.Texts())
	}
}
```

Also add `"slices"` to the imports.

In `view/app_test.go`, change the label list in `TestSidebarChoosesPage` to:

```go
	for _, label := range []string{"Overview", "Sessions", "History", "Config", "Cleanup"} {
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestHistory|TestSidebarChoosesPage'`
Expected: a build failure, `a.history undefined`.

- [ ] **Step 3: Add the icon**

In `view/icons.go`, change the comment's list to `pie-chart, message-square, history, sliders-horizontal, trash-2.`, and add to the `var` block after `iconSessions`:

```go
	iconHistory  = icon(`<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>`)
```

- [ ] **Step 4: Write the screen in `view/history.go`**

Add `"github.com/egoist/mygo/ui"` to its imports, then append:

```go
// history is the History screen: every prompt typed, found by search,
// project and date.
type history struct {
	prompts  []store.Prompt
	bad      int
	projects []string // allProjects, then each project as tilde shows it, newest prompt first
	shown    []int    // indexes in prompts of the rows the filters keep, newest first
	key      filterKey
	stale    bool // prompts changed since shown was worked out
	list     ui.ListState
	at       int // the row chosen in shown, -1 for none
	split    float32

	query, project string
	preset         int
	from, to       string // Custom's fields
	pasteOpen      map[[2]int]*bool
}

const allProjects = "All projects"

// filterKey is what the rows shown were filtered by.
type filterKey struct {
	query, project string
	dates          dateRange
}

func (h *history) init() {
	h.split, h.at, h.preset, h.project = 560, -1, presetAll, allProjects
	h.projects = []string{allProjects}
	h.list.Selected = &h.at
	h.list.Key = func(row int) any { return h.prompts[h.shown[row]].Line }
	h.pasteOpen = map[[2]int]*bool{}
}

func (h *history) load(a *App) {
	a.run(func() func() {
		prompts, bad, err := a.Store.History()
		return func() {
			h.prompts, h.bad, h.stale, h.at = prompts, bad, true, -1
			if err != nil {
				a.notice = err.Error()
			}
			h.projects = []string{allProjects}
			seen := map[string]bool{}
			for _, p := range prompts {
				if t := tilde(p.Project); !seen[t] {
					seen[t] = true
					h.projects = append(h.projects, t)
				}
			}
			if !seen[h.project] {
				h.project = allProjects
			}
		}
	})
}

// filter works out the rows shown again when the filters or the prompts
// changed: one pass over the prompts, not one a frame. The row chosen stays
// chosen while the filters keep it.
func (h *history) filter(k filterKey) {
	if k == h.key && !h.stale {
		return
	}
	line := 0
	if !h.stale {
		if p := h.chosen(); p != nil {
			line = p.Line
		}
	}
	h.key, h.stale, h.at = k, false, -1
	h.shown = h.shown[:0]
	for i := range h.prompts {
		p := &h.prompts[i]
		if (k.project == allProjects || tilde(p.Project) == k.project) && k.dates.keeps(p.Time) && matchPrompt(p, k.query) {
			if p.Line == line {
				h.at = len(h.shown)
			}
			h.shown = append(h.shown, i)
		}
	}
	h.list.Version++
}

// chosen is the prompt of the row chosen, nil for none.
func (h *history) chosen() *store.Prompt {
	if h.at < 0 || h.at >= len(h.shown) {
		return nil
	}
	return &h.prompts[h.shown[h.at]]
}

func (h *history) view(c *ui.Context, a *App, pal *palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.SearchField(c, &h.query).Label("Search prompts").Width(200).MinWidth(100)
		ui.Select(c, &h.project, h.projects).Width(240).MinWidth(120)
		ui.Spacer(c)
		ui.Segmented(c, &h.preset, datePresets...)
	})
	if h.preset == presetCustom {
		loc := a.Store.Now().Location()
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Field(c, "From", func() { ui.TextInput(c, &h.from).Placeholder("YYYY-MM-DD").Width(130) }).Error(dayError(h.from, loc))
			ui.Field(c, "To", func() { ui.TextInput(c, &h.to).Placeholder("YYYY-MM-DD").Width(130) }).Error(dayError(h.to, loc))
		})
	}
	h.filter(filterKey{h.query, h.project, rangeOf(h.preset, a.Store.Now(), h.from, h.to)})
	ui.Split(c, &h.split, func() {
		ui.Column(c).Fill().Gap(6).Children(func() { h.table(c, pal) })
	}, func() {
		ui.Column(c).Fill().Padding(0, 0, 0, 12).Children(func() { h.detail(c, a, pal) })
	}).Grow(1)
}

// dayError is the message under a Custom field, "" when it is fine.
func dayError(s string, loc *time.Location) string {
	_, msg := parseDay(s, loc)
	return msg
}

func (h *history) table(c *ui.Context, pal *palette) {
	switch {
	case len(h.prompts) == 0:
		blank(c, pal, "No prompt history yet.")
		return
	case len(h.shown) == 0:
		blank(c, pal, "No prompt matches.")
		return
	}
	cols := []ui.TableColumn{{Title: "Time", Width: 130}, {Title: "Project", Width: 140}, {Title: "Prompt", MinWidth: 160}}
	ui.Table(c, &h.list, cols, len(h.shown), func(row, col int) {
		p := &h.prompts[h.shown[row]]
		switch col {
		case 0:
			ui.Text(c, fmtTime(p.Time)).TextColor(pal.muted).FontFeatures("tnum").SingleLine()
		case 1:
			ui.Text(c, shortPath(p.Project)).SingleLine().Tooltip(p.Project)
		case 2:
			ui.Text(c, store.FirstLine(p.Text, 200)).SingleLine()
		}
	}).Grow(1).MinHeight(0)
	ui.Text(c, plural(len(h.shown), "prompt")).FontSize(12).TextColor(pal.muted).FontFeatures("tnum")
}

// detail shows the prompt chosen whole. Task 4 fills it in.
func (h *history) detail(c *ui.Context, a *App, pal *palette) {}
```

- [ ] **Step 5: Wire the screen into `view/app.go`**

Make these changes:

1. In `App`, after `sessions sessions`, add `history  history`.
2. In `New`, after `a.sessions.init()`, add `a.history.init()`.
3. In `Rescan`, after `a.sessions.load(a)`, add `a.history.load(a)`.
4. In `View`'s sidebar, between the Sessions item and the Config item, add:

```go
			ui.SidebarItem(c, "history", iconHistory, "History")
```

5. In `View`'s page switch, after `case "sessions":`, add:

```go
			case "history":
				a.history.view(c, a, pal)
```

6. In `header`'s switch, after `case "sessions":`, add:

```go
	case "history":
		h := &a.history
		title = "History"
		sub = fmt.Sprintf("%s in %s", plural(len(h.prompts), "prompt"), plural(len(h.projects)-1, "project"))
		if h.bad > 0 {
			sub += " · " + plural(h.bad, "unreadable line")
		}
```

- [ ] **Step 6: Let the Overview open History**

In `view/overview.go`'s `screenOf`, add a case before `case e.Cleanable:`:

```go
	case e.Name == "history.jsonl":
		return "history"
```

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `go test ./view -run 'TestHistory|TestSidebarChoosesPage' -v`
Expected: PASS.

If `TestHistoryPresetClick` fails because `Click("Today")` hits another "Today" text, check `tt.Texts()`. Click the Segmented's label by its exact text. Do not drop the test.

- [ ] **Step 8: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/history.go view/history_test.go view/app.go view/icons.go view/overview.go view/app_test.go
git commit -m "feat(view): History screen with search and project and date filters

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: The detail face: whole prompt, Copy, Open session

**Files:**
- Modify: `view/history.go` (replace the `detail` stub)
- Modify: `view/sessions.go` (add `find`)
- Test: `view/history_test.go`

**Interfaces:**
- Consumes:
  - from Task 3: `history.chosen()`, `h.pasteOpen`;
  - from Task 1: `store.Prompt.Expand`;
  - existing: `face`, `head`, `blank`, `clip`, `tilde`, `fmtTime`, `plural`, `a.notice`, `(*sessions).openTranscript(a, x)`, `c.WriteClipboard`.
- Produces: `func (s *sessions) find(id string) *store.Session`, nil when the session is gone.

- [ ] **Step 1: Write the failing tests**

Append to `view/history_test.go`:

```go
func TestHistoryDetailAndCopy(t *testing.T) {
	_, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	if !tt.HasText("Choose a prompt to see it whole.") {
		t.Errorf("no prompt chosen, no hint; texts %q", tt.Texts())
	}
	must(t, tt.Click("Review this:"))
	if !tt.HasText("Pasted text #1 (2 lines)") || !tt.HasText("Pasted text #3 (1 line)") {
		t.Errorf("pastes not listed; texts %q", tt.Texts())
	}
	must(t, tt.Click("Copy"))
	if got, want := tt.Clipboard(), "Review this:\nfunc main() {\n}"; got != want {
		t.Errorf("clipboard %q, want %q", got, want)
	}
	tt.Frame() // the toast shows on the frame after the click
	if !tt.HasText("Copied") {
		t.Errorf("no toast; texts %q", tt.Texts())
	}
}

func TestHistoryLostPaste(t *testing.T) {
	_, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	must(t, tt.Click("Use this [Pasted text #2 +40 lines] and [Image #1]"))
	if !tt.HasText("1 paste no longer stored") {
		t.Errorf("lost paste not said; texts %q", tt.Texts())
	}
}

func TestHistoryOpensSession(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	must(t, tt.Click("Fix the login bug"))
	must(t, tt.Click("Open session"))
	if a.page != "sessions" || a.sessions.open == nil || a.sessions.open.ID != fixture.Old {
		t.Fatalf("page %q, open %v; want the transcript of %s", a.page, a.sessions.open, fixture.Old)
	}
}

func TestHistoryGoneSession(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	must(t, tt.Click("Use this [Pasted text #2 +40 lines] and [Image #1]"))
	tt.Click("Open session") // disabled: a click does nothing
	if a.page != "history" || a.sessions.open != nil {
		t.Errorf("a gone session opened: page %q", a.page)
	}
}

// A filter that hides the row chosen lets it go: the detail doesn't show a
// prompt the table no longer has.
func TestHistoryFilterDropsChoice(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("History"))
	must(t, tt.Click("Old idea"))
	if a.history.chosen() == nil || a.history.chosen().Line != 1 {
		t.Fatalf("Old idea not chosen")
	}
	a.history.preset = presetToday
	tt.Frame()
	if a.history.chosen() != nil || !tt.HasText("Choose a prompt to see it whole.") {
		t.Errorf("the hidden row stays chosen: %+v", a.history.chosen())
	}
	a.history.preset = presetAll
	tt.Frame()
	must(t, tt.Click("Fix the login bug"))
	a.history.query = "login"
	tt.Frame()
	if p := a.history.chosen(); p == nil || p.Line != 2 {
		t.Errorf("a row the filter keeps lost its choice: %+v", p)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestHistoryDetail|TestHistoryLostPaste|TestHistoryOpensSession|TestHistoryGoneSession|TestHistoryFilterDropsChoice'`
Expected: FAIL, because the stub draws nothing: `no prompt chosen, no hint`, and `Click("Copy")` finds no button.

- [ ] **Step 3: Add `find` to `view/sessions.go`**

Put it after `targets`:

```go
// find returns the session id names among those scanned, nil when it is not
// on disk any more.
func (s *sessions) find(id string) *store.Session {
	for i := range s.projects {
		for j := range s.projects[i].Sessions {
			if x := &s.projects[i].Sessions[j]; x.ID == id {
				return x
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Replace the `detail` stub in `view/history.go`**

Make sure `"fmt"` and `"strings"` are imported, then replace the stub with:

```go
// detail shows the prompt chosen whole, with its pastes, and the keys to
// copy it or open its session.
func (h *history) detail(c *ui.Context, a *App, pal *palette) {
	p := h.chosen()
	face(c, pal).Grow(1).MinHeight(0).Children(func() {
		note := ""
		if p != nil {
			note = fmtTime(p.Time)
		}
		head(c, pal, "Prompt", note)
		ui.Divider(c)
		if p == nil {
			blank(c, pal, "Choose a prompt to see it whole.")
			return
		}
		ui.Scroll(c.Key(p.Line)).Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).Padding(16, 20).Gap(12).Children(func() {
				ui.Text(c, tilde(p.Project)).FontSize(12).TextColor(pal.muted).SingleLine().Tooltip(p.Project)
				ui.Text(c, clip(p.Text)).Selectable()
				for _, x := range p.Pastes {
					label := fmt.Sprintf("Pasted text #%d (%s)", x.ID, plural(lineCount(x.Content), "line"))
					ui.Collapsible(c, label, h.pasteOpenFor(p.Line, x.ID), func() {
						ui.Text(c, clip(x.Content)).Font("monospace").FontSize(12).Selectable()
					})
				}
				if p.Lost > 0 {
					ui.Text(c, plural(p.Lost, "paste")+" no longer stored").FontSize(12).TextColor(pal.muted)
				}
			})
		})
		ui.Divider(c)
		ui.Row(c).PaddingX(16).PaddingY(12).Gap(8).Children(func() {
			if ui.Button(c, "Copy").Clicked() {
				c.WriteClipboard(p.Expand())
				a.notice = "Copied"
			}
			x := a.sessions.find(p.SessionID)
			open := ui.Button(c, "Open session").Disabled(x == nil)
			if x == nil {
				open.Tooltip("Session no longer on disk")
			}
			if open.Clicked() {
				a.page = "sessions"
				a.sessions.openTranscript(a, *x)
			}
		})
	})
}

// pasteOpenFor is whether a paste of the prompt on line is expanded.
func (h *history) pasteOpenFor(line, id int) *bool {
	k := [2]int{line, id}
	if h.pasteOpen[k] == nil {
		h.pasteOpen[k] = new(bool)
	}
	return h.pasteOpen[k]
}

// lineCount counts a text's lines, a last line without "\n" included.
func lineCount(s string) int {
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./view -run 'TestHistory' -v`
Expected: PASS for every History test, the Task 3 ones included.

- [ ] **Step 6: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/history.go view/sessions.go view/history_test.go
git commit -m "feat(view): History's detail face, Copy and Open session

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Screenshots and the roadmap

**Files:**
- Modify: `view/screenshot_test.go` (the `shots` list)
- Create: `docs/screenshots/history-light.png`, `docs/screenshots/history-dark.png`
- Modify: `docs/screenshots/overview-light.png` and `overview-dark.png` (the fixture now has `history.jsonl`)
- Modify: `docs/roadmap.md`

- [ ] **Step 1: Add the shot**

In `view/screenshot_test.go`'s `shots`, after the `cleanup` entry, add:

```go
		{"history", "History", []string{"Review this:"}},
```

- [ ] **Step 2: Write the PNGs from the fixture**

```bash
D=$(mktemp -d) && SCREENSHOT_DIR=$D go test ./view -run TestScreenshots -count=1 && \
cp $D/history-1100.png docs/screenshots/history-light.png && \
cp $D/history-dark.png docs/screenshots/history-dark.png && \
cp $D/overview-1100.png docs/screenshots/overview-light.png && \
cp $D/overview-dark.png docs/screenshots/overview-dark.png
```

Expected: exit 0. Open both history PNGs and check:
- the table holds the fixture's prompts;
- `Review this:` is chosen, in graphite;
- the detail shows `Pasted text #1 (2 lines)` and the Copy and Open session buttons;
- there is no orange.

The fixture holds no real data, so the PNGs can be committed.

- [ ] **Step 3: Tick the roadmap**

In `docs/roadmap.md`, change:

```markdown
- [ ] Full-text search of `history.jsonl` (every prompt typed), filtered by project and date.
```

to:

```markdown
- [x] Full-text search of `history.jsonl` (every prompt typed), filtered by project and date.
```

- [ ] **Step 4: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/screenshot_test.go docs/screenshots/history-light.png docs/screenshots/history-dark.png docs/screenshots/overview-light.png docs/screenshots/overview-dark.png docs/roadmap.md
git commit -m "docs: History screenshots, tick the roadmap item

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
