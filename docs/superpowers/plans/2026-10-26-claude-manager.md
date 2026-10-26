# Claude Manager v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A macOS app, drawn with MyGo's native UI, that shows what is in `~/.claude` and moves what the user picks to the Trash.

**Architecture:** Package `store` reads the config directory (no UI, tested against a synthetic fixture). Package `view` draws four screens with `github.com/egoist/mygo/ui` and does all disk work off the main thread through one helper, `App.run`. `main.go` wires `mygo.Shell.TrashItem` and the window together.

**Tech Stack:** Go 1.27, MyGo (`github.com/egoist/mygo`, local clone at `../mygo`), packages `ui` and `ui/markdown`. No cgo, no Bun, no other dependencies.

**Spec:** `docs/superpowers/specs/2026-10-26-claude-manager-design.md`

## Global Constraints

- Builds with `CGO_ENABLED=0`. macOS only (the app uses `syscall.Kill`).
- `go.mod`: `module github.com/lang315/claude-manager`, `go 1.27.1`, `replace github.com/egoist/mygo => ../mygo`. No other dependencies.
- Root is `$CLAUDE_CONFIG_DIR`, else `~/.claude`. `.claude.json` is `$CLAUDE_CONFIG_DIR/.claude.json`, else `~/.claude.json`.
- The app never writes to the root or to `.claude.json`. The only change it makes is moving paths to the Trash through `Store.TrashPaths`, which calls `Store.Trash` (`mygo.Shell.TrashItem` in the app) and refuses paths outside the root and the root itself. No `os.Remove*` outside tests.
- A live session (its id in `sessions/<pid>.json` with a live PID) and anything modified in the last 10 minutes are never trashed.
- Only UUID-named entries are session data. `memory/`, `*-memory/`, `*.wakatime`, `sessions-index.json` are never cleanup candidates.
- `store` never imports `ui` or `mygo`. `view` never imports `mygo`. Neither calls `os.UserHomeDir`, `time.Now` or `syscall.Kill` directly: `Store.Now`, `Store.Alive` and `Store.Trash` are injected.
- Views never touch the disk on the main thread: every `store` call that reads or trashes runs inside `App.run`.
- Tests never touch the real `~/.claude`. Fixtures are synthetic (`internal/fixture`), never copied from real sessions.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. No session links anywhere.

## Review Focus

1. **User `message.content` as a plain string** (10 941 real records) as well as a list of blocks: it must show as a user message and count as the first prompt. Pinned in Task 3 (`TestProjects`, first prompt of `fixture.Old`) and Task 4 (`TestReadTranscript`).
2. **`ai-title` past the first 64 KB** (107 of 164 real sessions, up to 3.4 MB in): the title must still be found. Pinned in Task 3 (`fixture.Late`).
3. **Orphan detection when the session list is empty or failed:** `file-history/` entries and UUID side dirs must not all become "orphans". Pinned in Task 5 (`TestCleanupWithoutSessions`).
4. **Live and recent sessions never trashed**, including a live PID owned by another user (`EPERM`). Pinned in Task 1 (`TestPIDAlive` with PID 1), Task 3 (`fixture.Live`, `fixture.Recent`), Task 5 (`TestCleanupGroups`), Task 8 (`TestTrashSkipsLiveSession`).
5. **Credentials in `settings.json` and `.claude.json`** (MCP `env`, `headers`) never shown in plain text. Pinned in Task 10 (`TestConfigMasksSecrets`).

## Deviations from the spec (decided while planning)

- The sidebar has **four** entries (Overview, Sessions, Config, Cleanup); the transcript opens inside Sessions, as the spec's screen list says.
- Overview shows each entry's share as a `ui.Meter` in the table instead of a separate bar chart: same information, one widget.
- The sessions table has no "estimated messages" column: no honest estimate exists without reading the whole file. The transcript header shows the real count.
- MyGo's `DataTable` has no double-click event: an **Open Transcript** button (and **Open** on Overview) acts on the chosen row.
- "Multi-select" means: the checked rows, or the chosen rows when none is checked.
- Title fallback is `ai-title`, then the first user prompt (no `last-prompt` record).
- Skills, Commands and Memory show a list beside the file rendered as Markdown (the skill's description is in it).
- Fixtures are generated into `t.TempDir()` by `internal/fixture` instead of committed under `testdata/claude/`, because tests move files to a fake Trash.

## File Structure

```
go.mod, .gitignore
main.go                      window, Store wiring
store/store.go               Store, PIDAlive, shared types
store/scan.go                Scan: size and file count per top-level entry
store/sessions.go            Projects, LiveIDs, session metadata, FirstLine
store/record.go              one JSONL record → messages (shared by sessions and transcript)
store/transcript.go          ReadTranscript: streamed messages
store/cleanup.go             SessionPaths, Cleanup groups, TrashPaths
store/config.go              Config: settings, MCP, plugins, skills, commands, memory, backups
internal/fixture/fixture.go  synthetic ~/.claude for tests
view/app.go                  App, sidebar, run/trash helpers, picked
view/format.go               humanSize, fmtTime, errText, containsFold, clip
view/overview.go             Overview screen
view/sessions.go             Sessions screen (projects + sessions tables, trash)
view/transcript.go           transcript inside Sessions
view/config.go               Config screen
view/cleanup.go              Cleanup screen
```

---

### Task 1: Scaffold the module, `Store`, `PIDAlive`, window and sidebar

**Files:**
- Create: `go.mod`, `.gitignore`, `main.go`, `store/store.go`, `store/store_test.go`, `view/app.go`, `view/app_test.go`

**Interfaces:**
- Produces: `store.Store{Root, ClaudeJSON string; Trash func(string) error; Now func() time.Time; Alive func(int) bool}`, `store.PIDAlive(pid int) bool`, const `store.recent = 10 * time.Minute`, `view.New(*store.Store) *App`, `(*App).View(*ui.Context)`, `(*App).Rescan()`, fields `App.Go`, `App.Update`, unexported `page`, `busy`, `notice`.

- [ ] **Step 1: Create the module**

`go.mod`:

```
module github.com/lang315/claude-manager

go 1.27.1

replace github.com/egoist/mygo => ../mygo
```

`.gitignore`:

```
/claude-manager
/build/
```

- [ ] **Step 2: Write the failing tests**

`store/store_test.go`:

```go
package store

import (
	"os"
	"os/exec"
	"testing"
)

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Error("own PID reads as dead")
	}
	// launchd belongs to root: Kill answers EPERM, and the process is alive.
	if !PIDAlive(1) {
		t.Error("PID 1 reads as dead")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if PIDAlive(cmd.Process.Pid) {
		t.Error("exited process reads as alive")
	}
	if PIDAlive(0) || PIDAlive(-1) {
		t.Error("non-positive PID reads as alive")
	}
}
```

`view/app_test.go`:

```go
package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

func TestSidebarChoosesPage(t *testing.T) {
	a := New(&store.Store{Root: t.TempDir()})
	tt := ui.NewTester(a.View, 1200, 800)
	for _, label := range []string{"Overview", "Sessions", "Config", "Cleanup"} {
		if !tt.HasText(label) {
			t.Errorf("sidebar lacks %q", label)
		}
	}
	if err := tt.Click("Cleanup"); err != nil {
		t.Fatal(err)
	}
	if a.page != "cleanup" {
		t.Errorf("page %q after clicking Cleanup", a.page)
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./...`
Expected: FAIL to build: `undefined: PIDAlive`, `undefined: New`.

- [ ] **Step 4: Implement**

`store/store.go`:

```go
// Package store reads a Claude Code config directory (~/.claude) and moves
// what the user picks to the Trash. It never writes anything else.
package store

import (
	"errors"
	"syscall"
	"time"
)

// Store reads one Claude Code config directory.
type Store struct {
	Root       string                  // ~/.claude, or $CLAUDE_CONFIG_DIR
	ClaudeJSON string                  // ~/.claude.json, read for its MCP servers
	Trash      func(path string) error // moves a path to the Trash: mygo.Shell.TrashItem in the app
	Now        func() time.Time
	Alive      func(pid int) bool // PIDAlive in the app
}

// recent is how long after its last write a file counts as in use.
const recent = 10 * time.Minute

// PIDAlive reports whether a process exists. EPERM means it exists but
// belongs to another user.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
```

`view/app.go`:

```go
// Package view draws Claude Manager's screens in MyGo's native UI.
package view

import (
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

// App is the window's state. The view reads and writes it on the main thread.
type App struct {
	Store *store.Store
	// Go runs slow work (the disk) off the main thread; tests run it inline.
	Go func(func())
	// Update runs a change on the main thread and redraws: Window.Update in
	// the app, Tester.Update in tests.
	Update func(func())

	page   string
	busy   int    // loads and moves in flight
	notice string // shown as a toast on the next frame
}

func New(s *store.Store) *App {
	return &App{Store: s, Go: func(f func()) { go f() }, page: "overview"}
}

// Rescan reloads every screen from disk.
func (a *App) Rescan() {
	// Each screen adds its load here.
}

func (a *App) View(c *ui.Context) {
	if a.notice != "" {
		c.Toast(a.notice)
		a.notice = ""
	}
	ui.Row(c).Fill().Children(func() {
		ui.Sidebar(c, &a.page, func() {
			ui.SidebarItem(c, "overview", nil, "Overview")
			ui.SidebarItem(c, "sessions", nil, "Sessions")
			ui.SidebarItem(c, "config", nil, "Config")
			ui.SidebarItem(c, "cleanup", nil, "Cleanup")
		}).Width(180)
		ui.Column(c).Grow(1).Padding(12).Gap(8).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				if a.busy > 0 {
					ui.Text(c, "Scanning…").TextColor(c.Theme().TextMuted)
				}
				ui.Spacer(c)
				if ui.Button(c, "Rescan").Disabled(a.busy > 0).Clicked() {
					a.Rescan()
				}
			})
			switch a.page {
			// Each screen adds its case here.
			}
		})
	})
}
```

`main.go`:

```go
// Claude Manager shows what is in ~/.claude and moves what you pick to the
// Trash.
package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
	"github.com/lang315/claude-manager/view"
)

func main() {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	claudeJSON := filepath.Join(root, ".claude.json")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		root, claudeJSON = filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")
	}
	a := view.New(&store.Store{
		Root:       root,
		ClaudeJSON: claudeJSON,
		Trash:      mygo.Shell.TrashItem,
		Now:        time.Now,
		Alive:      store.PIDAlive,
	})
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "Claude Manager",
			Width:   1100,
			Height:  720,
			Content: ui.View(a.View),
		})
		a.Update = win.Update
		a.Rescan()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 5: Resolve dependencies and run the tests**

Run: `go mod tidy && go test ./... && CGO_ENABLED=0 go build ./...`
Expected: `go.mod` gains `require github.com/egoist/mygo v0.0.0-…` and mygo's indirect requirements; `ok` for `store` and `view`; build exits 0.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum .gitignore main.go store view
git commit -m "feat: scaffold app window, sidebar and PIDAlive

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Synthetic fixture and `Scan`

**Files:**
- Create: `internal/fixture/fixture.go`, `store/scan.go`, `store/scan_test.go`
- Modify: `store/store_test.go` (add `newStore`)

**Interfaces:**
- Consumes: `store.Store`.
- Produces: `fixture.New(testing.TB) fixture.Dirs{Root, ClaudeJSON string}`, `fixture.Now time.Time`, `fixture.Alive(pid int) bool`, session id consts `fixture.Old, Late, Live, OrphanDir, Recent, OrphanEnv, OrphanHistory`, const `fixture.Proj = "projects/-Users-me-proj"`, `fixture.Other = "projects/-Users-me-other"`; `store.Entry{Name string; Size int64; Files int; Cleanable bool}`, `(*Store).Scan() ([]Entry, error)` sorted by size, largest first; unexported `dirSize(path) (int64, int)`; test helper `newStore(t) (*Store, fixture.Dirs, *[]string)`.

- [ ] **Step 1: Write the fixture**

`internal/fixture/fixture.go`:

```go
// Package fixture writes a synthetic Claude Code config directory for tests.
// Its sessions have the shape of real ones but no real data.
package fixture

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Now is the tests' clock.
var Now = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

const (
	Old           = "11111111-1111-4111-8111-111111111111" // 60 days old, titled "Login bug fix", one broken line, one subagent
	Late          = "22222222-2222-4222-8222-222222222222" // 2 days old, its ai-title past the first 64 KB
	Live          = "33333333-3333-4333-8333-333333333333" // 60 days old, live: sessions/4242.json, PID 4242 alive
	OrphanDir     = "44444444-4444-4444-8444-444444444444" // side dir under Proj without a .jsonl
	Recent        = "55555555-5555-4555-8555-555555555555" // in Other, modified a minute before Now
	OrphanEnv     = "66666666-6666-4666-8666-666666666666" // session-env entry without a session
	OrphanHistory = "99999999-9999-4999-8999-999999999999" // file-history entry without a session

	Proj  = "projects/-Users-me-proj"
	Other = "projects/-Users-me-other"
)

// Alive is the tests' PID check: only PID 4242 runs.
func Alive(pid int) bool { return pid == 4242 }

// Dirs are where New wrote the config directory and .claude.json.
type Dirs struct{ Root, ClaudeJSON string }

var oldSession = strings.Join([]string{
	`{"type":"permission-mode","permissionMode":"default","sessionId":"` + Old + `"}`,
	`{"type":"user","isMeta":true,"cwd":"/Users/me/proj","gitBranch":"main","version":"2.1.300","timestamp":"2026-08-27T10:00:00.000Z","message":{"role":"user","content":"<local-command-caveat>ignore</local-command-caveat>"}}`,
	`{"type":"user","cwd":"/Users/me/proj","gitBranch":"main","version":"2.1.300","timestamp":"2026-08-27T10:00:01.000Z","message":{"role":"user","content":"Fix the login bug\nIt fails on Safari"}}`,
	`{"type":"assistant","timestamp":"2026-08-27T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Look at auth.go"},{"type":"text","text":"Reading **auth.go**."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"auth.go"}}]}}`,
	`{"type":"user","timestamp":"2026-08-27T10:00:03.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"package auth"}]}]}}`,
	`{not json`,
	`{"type":"attachment","attachment":{"type":"hook"}}`,
	`{"type":"ai-title","aiTitle":"Login bug fix","sessionId":"` + Old + `"}`,
	`{"type":"user","timestamp":"2026-08-27T10:00:04.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"plain result"}]}}`,
}, "\n") + "\n"

var lateSession = fmt.Sprintf(`{"type":"user","cwd":"/Users/me/proj","timestamp":"2026-10-24T08:00:00.000Z","message":{"role":"user","content":"Paste: %s"}}`, strings.Repeat("x", 70000)) +
	"\n" + `{"type":"ai-title","aiTitle":"Late title","sessionId":"` + Late + `"}` + "\n"

// New writes the config directory into a temporary home. Every path is 60
// days old but a few, which are newer on purpose.
func New(t testing.TB) Dirs {
	t.Helper()
	home := t.TempDir()
	d := Dirs{Root: filepath.Join(home, ".claude"), ClaudeJSON: filepath.Join(home, ".claude.json")}
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		Proj + "/" + Old + ".jsonl":                     oldSession,
		Proj + "/" + Old + "/subagents/agent-a1.jsonl":  "{}\n",
		Proj + "/" + Old + "/tool-results/r.txt":        "result",
		Proj + "/" + Late + ".jsonl":                    lateSession,
		Proj + "/" + Live + ".jsonl":                    `{"type":"user","cwd":"/Users/me/proj","timestamp":"2026-08-27T09:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"Live work"}]}}` + "\n",
		Proj + "/" + OrphanDir + "/tool-results/a.txt":  "orphan",
		Proj + "/memory/MEMORY.md":                      "# Memory\n- likes Go\n",
		Proj + "/mygo-memory/MEMORY.md":                 "# mygo\n",
		Proj + "/x.wakatime":                            "",
		Other + "/" + Recent + ".jsonl":                 `{"type":"user","cwd":"/Users/me/other","timestamp":"2026-10-26T11:58:00.000Z","message":{"role":"user","content":"Recent thing"}}` + "\n",
		"sessions/4242.json":                            `{"pid":4242,"sessionId":"` + Live + `"}`,
		"sessions/9999.json":                            `{"pid":9999,"sessionId":"` + Old + `"}`,
		"file-history/" + Old + "/v1":                   "a",
		"file-history/" + OrphanHistory + "/v1":         "b",
		"file-history/.DS_Store":                        "x",
		"session-env/" + Old + "/env":                   "1",
		"session-env/" + Live + "/env":                  "2",
		"session-env/" + OrphanEnv + "/env":             "3",
		"paste-cache/p1.txt":                            "paste",
		"shell-snapshots/s.sh":                          "snap",
		"debug/d.txt":                                   "debug",
		"transcripts/old.jsonl":                         "{}\n",
		"transcripts/new.jsonl":                         "{}\n",
		"settings.json":                                 `{"env":{"API_TOKEN":"secret123","EDITOR":"vim"},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]},"mcpServers":{"local":{"command":"local-mcp"}}}`,
		"settings.json.bak":                             "{}",
		"settings.json.bak-1":                           "{}",
		"skills/demo/SKILL.md":                          "---\nname: demo\ndescription: A demo skill\n---\n# Demo\n",
		"commands/hello.md":                             "Say hello\n",
		"CLAUDE.md":                                     "# Global rules\n",
		"plugins/installed_plugins.json":                `{"plugins":{"demo@market":[{"version":"1.0.0"}]}}`,
		"plugins/known_marketplaces.json":               `{"market":{"source":"github"}}`,
	}
	for rel, content := range files {
		write(filepath.Join(d.Root, rel), content)
	}
	write(d.ClaudeJSON, `{"mcpServers":{"remote":{"type":"http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer abc"}}}}`)

	old := Now.AddDate(0, 0, -60)
	err := filepath.WalkDir(d.Root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, old, old)
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, mod := range map[string]time.Time{
		Proj + "/" + Late + ".jsonl":    Now.AddDate(0, 0, -2),
		Other + "/" + Recent + ".jsonl": Now.Add(-time.Minute),
		"transcripts/new.jsonl":         Now.AddDate(0, 0, -2),
	} {
		if err := os.Chtimes(filepath.Join(d.Root, rel), mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	return d
}
```

- [ ] **Step 2: Write the failing test**

Append to `store/store_test.go` (add imports `"time"` and `"github.com/lang315/claude-manager/internal/fixture"`):

```go
// newStore returns a Store on a fresh fixture, with a fake Trash that
// removes what it gets and records it.
func newStore(t *testing.T) (*Store, fixture.Dirs, *[]string) {
	t.Helper()
	d := fixture.New(t)
	var trashed []string
	s := &Store{
		Root:       d.Root,
		ClaudeJSON: d.ClaudeJSON,
		Now:        func() time.Time { return fixture.Now },
		Alive:      fixture.Alive,
		Trash: func(p string) error {
			trashed = append(trashed, p)
			return os.RemoveAll(p)
		},
	}
	return s, d, &trashed
}
```

`store/scan_test.go`:

```go
package store

import "testing"

func TestScan(t *testing.T) {
	s, _, _ := newStore(t)
	entries, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Name != "projects" {
		t.Errorf("largest entry %q, want projects", entries[0].Name)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Size > entries[i-1].Size {
			t.Errorf("%q (%d) after %q (%d): not sorted", entries[i].Name, entries[i].Size, entries[i-1].Name, entries[i-1].Size)
		}
	}
	by := map[string]Entry{}
	for _, e := range entries {
		by[e.Name] = e
	}
	if e := by["paste-cache"]; e.Files != 1 || e.Size != 5 || !e.Cleanable {
		t.Errorf("paste-cache %+v, want 1 file of 5 bytes, cleanable", e)
	}
	if by["settings.json"].Cleanable || !by["settings.json.bak"].Cleanable || by["skills"].Cleanable {
		t.Errorf("cleanable wrong: settings.json %v, settings.json.bak %v, skills %v",
			by["settings.json"].Cleanable, by["settings.json.bak"].Cleanable, by["skills"].Cleanable)
	}
}

func TestScanMissingRoot(t *testing.T) {
	s := &Store{Root: t.TempDir() + "/nope"}
	if _, err := s.Scan(); err == nil {
		t.Error("no error for a missing root")
	}
}
```

- [ ] **Step 3: Run it to see it fail**

Run: `go test ./store/ -run TestScan`
Expected: FAIL to build: `s.Scan undefined`.

- [ ] **Step 4: Implement**

`store/scan.go`:

```go
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
```

- [ ] **Step 5: Run the tests**

Run: `go test ./store/ ./internal/...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal store
git commit -m "feat(store): scan sizes of ~/.claude entries

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Session records, live sessions and `Projects`

**Files:**
- Create: `store/record.go`, `store/sessions.go`, `store/sessions_test.go`

**Interfaces:**
- Consumes: `newStore`, `fixture.*`, `dirSize`, `recent`.
- Produces:
  - `store.Message{Role, Kind, Text string; Time time.Time}` — Role `"user"`/`"assistant"`, Kind `"text"`/`"thinking"`/`"tool_use"`/`"tool_result"`.
  - unexported `record` with `messages() []Message` and `time() time.Time` (used by Task 4).
  - `store.Session{ID, Dir, File, Cwd, Branch, Version, Title, FirstPrompt string; Started, Modified time.Time; Size int64; Subagents int; Live bool; Err string}` — `Dir` is the absolute `projects/<encoded>` dir, `File` the `.jsonl`.
  - `store.Project{Dir, Path string; Sessions []Session; Size int64}` — `Path` is the real path from `cwd`, else the encoded dir name.
  - `(*Store).LiveIDs() map[string]bool`, `(*Store).Projects() ([]Project, error)` (largest project first, sessions newest first), `store.FirstLine(s string, n int) string`, var `uuidRe *regexp.Regexp`.

- [ ] **Step 1: Write the failing tests**

`store/sessions_test.go`:

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

func TestLiveIDs(t *testing.T) {
	s, _, _ := newStore(t)
	got := s.LiveIDs()
	if len(got) != 1 || !got[fixture.Live] {
		t.Errorf("LiveIDs %v, want only %s (9999.json's PID is dead)", got, fixture.Live)
	}
}

func TestProjects(t *testing.T) {
	s, d, _ := newStore(t)
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 || projects[0].Path != "/Users/me/proj" || projects[1].Path != "/Users/me/other" {
		t.Fatalf("projects %+v, want /Users/me/proj then /Users/me/other", projects)
	}
	if n := len(projects[0].Sessions); n != 3 {
		t.Fatalf("%d sessions in proj, want 3 (the orphan dir, memory dirs and .wakatime are not sessions)", n)
	}
	by := map[string]Session{}
	for _, p := range projects {
		for _, x := range p.Sessions {
			by[x.ID] = x
		}
	}

	old := by[fixture.Old]
	info, err := os.Stat(filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Title != "Login bug fix" || old.FirstPrompt != "Fix the login bug" || old.Cwd != "/Users/me/proj" ||
		old.Branch != "main" || old.Version != "2.1.300" || old.Subagents != 1 || old.Live || old.Err != "" {
		t.Errorf("old session %+v", old)
	}
	if want := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC); !old.Started.Equal(want) {
		t.Errorf("started %v, want %v", old.Started, want)
	}
	if want := info.Size() + int64(len("{}\n")+len("result")); old.Size != want {
		t.Errorf("size %d, want %d (jsonl + side dir)", old.Size, want)
	}

	late := by[fixture.Late]
	if late.Title != "Late title" {
		t.Errorf("late title %q: the ai-title past 64 KB was missed", late.Title)
	}
	if want := "Paste: " + strings.Repeat("x", 193); late.FirstPrompt != want {
		t.Errorf("late first prompt has %d runes, want 200", len([]rune(late.FirstPrompt)))
	}
	if late.Live {
		t.Error("2-day-old session reads as live")
	}
	if x := by[fixture.Live]; !x.Live || x.FirstPrompt != "Live work" {
		t.Errorf("live session %+v", x)
	}
	if x := by[fixture.Recent]; !x.Live {
		t.Error("session written a minute ago is not live")
	}
}

func TestFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"  one\ntwo": "one",
		"":           "",
		"ééééé":      "ééé",
	} {
		if got := FirstLine(in, 3); got != want {
			t.Errorf("FirstLine(%q, 3) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./store/ -run 'TestLiveIDs|TestProjects|TestFirstLine'`
Expected: FAIL to build: `s.LiveIDs undefined`.

- [ ] **Step 3: Implement the record parser**

`store/record.go`:

```go
package store

import (
	"encoding/json"
	"strings"
	"time"
)

// Message is one block of a user or assistant turn.
type Message struct {
	Role string // "user" or "assistant"
	Kind string // "text", "thinking", "tool_use" or "tool_result"
	Text string
	Time time.Time
}

// record is one line of a session's JSONL. Fields whose shape varies by
// record type stay raw, so that a type mismatch never fails the line.
type record struct {
	Type      string          `json:"type"`
	IsMeta    bool            `json:"isMeta"`
	Cwd       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Version   string          `json:"version"`
	Timestamp json.RawMessage `json:"timestamp"`
	AITitle   string          `json:"aiTitle"`
	Message   json.RawMessage `json:"message"`
}

type block struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
}

func (r record) time() time.Time {
	var s string
	if json.Unmarshal(r.Timestamp, &s) != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// messages returns the blocks of a user or assistant record. Records Claude
// Code injects (isMeta) and every other record type have none.
func (r record) messages() []Message {
	if (r.Type != "user" && r.Type != "assistant") || r.IsMeta {
		return nil
	}
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(r.Message, &m) != nil {
		return nil
	}
	var out []Message
	for _, b := range blocks(m.Content) {
		msg := Message{Role: r.Type, Kind: b.Type, Time: r.time()}
		switch b.Type {
		case "text":
			msg.Text = b.Text
		case "thinking":
			msg.Text = b.Thinking
		case "tool_use":
			msg.Text = b.Name + " " + string(b.Input)
		case "tool_result":
			msg.Text = plain(b.Content)
		default:
			continue
		}
		out = append(out, msg)
	}
	return out
}

// blocks reads content that is either a string or a list of blocks.
func blocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	json.Unmarshal(raw, &bs)
	return bs
}

// plain joins the text of content that is a string or a list of blocks.
func plain(raw json.RawMessage) string {
	var b strings.Builder
	for _, x := range blocks(raw) {
		b.WriteString(x.Text)
	}
	return b.String()
}
```

- [ ] **Step 4: Implement sessions**

`store/sessions.go`:

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
	"strings"
	"time"
)

// Session is one conversation: projects/<dir>/<ID>.jsonl and its side dir
// projects/<dir>/<ID>/.
type Session struct {
	ID, Dir, File       string // Dir is projects/<dir>, File the .jsonl
	Cwd, Branch, Version string
	Title, FirstPrompt  string
	Started, Modified   time.Time
	Size                int64 // the .jsonl and its side dir
	Subagents           int
	Live                bool // running, or written in the last 10 minutes
	Err                 string
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
// lines holding an ai-title are parsed.
const headLimit = 64 << 10

var aiTitle = []byte(`"ai-title"`)

// LiveIDs returns the ids of the sessions whose process runs, from
// sessions/<pid>.json.
func (s *Store) LiveIDs() map[string]bool {
	ids := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(s.Root, "sessions", "*.json"))
	for _, f := range files {
		var r struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
		}
		b, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(b, &r) != nil || r.SessionID == "" {
			continue
		}
		if s.Alive(r.PID) {
			ids[r.SessionID] = true
		}
	}
	return ids
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
		files, _ := filepath.Glob(filepath.Join(p.Dir, "*.jsonl"))
		for _, f := range files {
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
	agents, _ := filepath.Glob(filepath.Join(side, "subagents", "*.jsonl"))
	x.Subagents = len(agents)
	if err := readHead(file, &x); err != nil {
		x.Err = err.Error()
	}
	x.Live = live[id] || s.Now().Sub(x.Modified) < recent
	return x
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
		if head || bytes.Contains(line, aiTitle) {
			var rec record
			if json.Unmarshal(line, &rec) == nil {
				x.note(rec, head)
			}
		}
		if err == io.EOF || (!head && x.Title != "") {
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
	if !head {
		return
	}
	x.Cwd = cmp.Or(x.Cwd, r.Cwd)
	x.Branch = cmp.Or(x.Branch, r.GitBranch)
	x.Version = cmp.Or(x.Version, r.Version)
	if x.Started.IsZero() {
		x.Started = r.time()
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
```

- [ ] **Step 5: Run the tests**

Run: `go test ./store/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add store
git commit -m "feat(store): list projects and sessions with titles and live state

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Streamed transcript

**Files:**
- Create: `store/transcript.go`, `store/transcript_test.go`

**Interfaces:**
- Consumes: `record`, `Message`.
- Produces: `store.ReadTranscript(file string, emit func([]Message)) (bad int, err error)` — calls `emit` with batches of up to 200 messages, in file order; `bad` counts non-blank lines that are not JSON.

- [ ] **Step 1: Write the failing test**

`store/transcript_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestReadTranscript(t *testing.T) {
	_, d, _ := newStore(t)
	var got []Message
	batches := 0
	bad, err := ReadTranscript(filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"), func(ms []Message) {
		batches++
		got = append(got, ms...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 {
		t.Errorf("bad = %d, want 1", bad)
	}
	want := []Message{
		{Role: "user", Kind: "text", Text: "Fix the login bug\nIt fails on Safari"},
		{Role: "assistant", Kind: "thinking", Text: "Look at auth.go"},
		{Role: "assistant", Kind: "text", Text: "Reading **auth.go**."},
		{Role: "assistant", Kind: "tool_use", Text: `Read {"file_path":"auth.go"}`},
		{Role: "user", Kind: "tool_result", Text: "package auth"},
		{Role: "user", Kind: "tool_result", Text: "plain result"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d messages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Kind != want[i].Kind || got[i].Text != want[i].Text {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[0].Time.IsZero() {
		t.Error("message time not parsed")
	}
	if batches != 1 {
		t.Errorf("%d batches for 6 messages, want 1", batches)
	}
}

func TestReadTranscriptMissing(t *testing.T) {
	if _, err := ReadTranscript(filepath.Join(t.TempDir(), "nope.jsonl"), func([]Message) {}); err == nil {
		t.Error("no error for a missing file")
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./store/ -run TestReadTranscript`
Expected: FAIL to build: `undefined: ReadTranscript`.

- [ ] **Step 3: Implement**

`store/transcript.go`:

```go
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
)

const batchSize = 200

// ReadTranscript reads a session's messages in file order, handing them to
// emit in batches as it goes, so a long session shows while it loads. Lines
// that are not JSON are skipped and counted in bad.
func ReadTranscript(file string, emit func([]Message)) (bad int, err error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var batch []Message
	for {
		line, rerr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var rec record
			if json.Unmarshal(line, &rec) != nil {
				bad++
			} else {
				batch = append(batch, rec.messages()...)
			}
		}
		if len(batch) >= batchSize || (rerr != nil && len(batch) > 0) {
			emit(batch)
			batch = nil
		}
		if rerr == io.EOF {
			return bad, nil
		}
		if rerr != nil {
			return bad, rerr
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./store/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add store
git commit -m "feat(store): stream a session's transcript

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Session paths, cleanup groups, trashing

**Files:**
- Create: `store/cleanup.go`, `store/cleanup_test.go`

**Interfaces:**
- Consumes: `Session`, `Project`, `uuidRe`, `dirSize`, `recent`, `LiveIDs`.
- Produces: `store.Item{Path string; Size int64; Modified time.Time}`, `store.Group{Name string; Items []Item; Size int64}`, `(*Store).SessionPaths(Session) []string` (existing paths only, in the order jsonl, side dir, file-history, session-env), `(*Store).Cleanup(projects []Project, days int) []Group` (always 8 groups, in a fixed order), `(*Store).TrashPaths(paths []string) (moved int, err error)`, unexported `item(path) Item`.

- [ ] **Step 1: Write the failing tests**

`store/cleanup_test.go`:

```go
package store

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func rel(t *testing.T, root string, items []Item) []string {
	t.Helper()
	var out []string
	for _, it := range items {
		r, err := filepath.Rel(root, it.Path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func session(t *testing.T, projects []Project, id string) Session {
	t.Helper()
	for _, p := range projects {
		for _, x := range p.Sessions {
			if x.ID == id {
				return x
			}
		}
	}
	t.Fatalf("no session %s", id)
	return Session{}
}

func TestSessionPaths(t *testing.T) {
	s, d, _ := newStore(t)
	projects, _ := s.Projects()
	got := s.SessionPaths(session(t, projects, fixture.Old))
	want := []string{
		filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"),
		filepath.Join(d.Root, fixture.Proj, fixture.Old),
		filepath.Join(d.Root, "file-history", fixture.Old),
		filepath.Join(d.Root, "session-env", fixture.Old),
	}
	if !slices.Equal(got, want) {
		t.Errorf("paths %q, want %q", got, want)
	}
	if got := s.SessionPaths(session(t, projects, fixture.Recent)); len(got) != 1 {
		t.Errorf("paths of a session without side data %q, want only its .jsonl", got)
	}
}

func TestCleanupGroups(t *testing.T) {
	s, d, _ := newStore(t)
	projects, _ := s.Projects()
	groups := s.Cleanup(projects, 30)
	want := map[string][]string{
		"session-env":                     {"session-env/" + fixture.Old, "session-env/" + fixture.OrphanEnv},
		"paste-cache":                     {"paste-cache/p1.txt"},
		"shell-snapshots":                 {"shell-snapshots/s.sh"},
		"debug":                           {"debug/d.txt"},
		"file-history without a session":  {"file-history/.DS_Store", "file-history/" + fixture.OrphanHistory},
		"transcripts older than 30 days":  {"transcripts/old.jsonl"},
		"session data without a session":  {fixture.Proj + "/" + fixture.OrphanDir},
		"sessions older than 30 days": {
			fixture.Proj + "/" + fixture.Old + ".jsonl",
			fixture.Proj + "/" + fixture.Old,
			"file-history/" + fixture.Old,
			"session-env/" + fixture.Old,
		},
	}
	if len(groups) != len(want) {
		t.Fatalf("%d groups, want %d", len(groups), len(want))
	}
	for _, g := range groups {
		w, ok := want[g.Name]
		if !ok {
			t.Errorf("unexpected group %q", g.Name)
			continue
		}
		if got := rel(t, d.Root, g.Items); !slices.Equal(got, w) {
			t.Errorf("%s: %q, want %q", g.Name, got, w)
		}
		var size int64
		for _, it := range g.Items {
			size += it.Size
		}
		if g.Size != size {
			t.Errorf("%s: size %d, items add up to %d", g.Name, g.Size, size)
		}
		for _, it := range g.Items {
			base := filepath.Base(it.Path)
			if base == fixture.Live || strings.Contains(it.Path, "memory") || strings.HasSuffix(base, ".wakatime") {
				t.Errorf("%s offers %s, which must never be a candidate", g.Name, it.Path)
			}
		}
	}
}

// With no sessions listed (projects/ unreadable, or not loaded yet), nothing
// may count as an orphan: every file-history entry would.
func TestCleanupWithoutSessions(t *testing.T) {
	s, _, _ := newStore(t)
	for _, g := range s.Cleanup(nil, 30) {
		if (g.Name == "file-history without a session" || g.Name == "session data without a session") && len(g.Items) > 0 {
			t.Errorf("%s offers %d items without a session list", g.Name, len(g.Items))
		}
	}
}

func TestTrashPaths(t *testing.T) {
	s, d, trashed := newStore(t)
	in := filepath.Join(d.Root, "paste-cache", "p1.txt")
	moved, err := s.TrashPaths([]string{in})
	if err != nil || moved != 1 || !slices.Equal(*trashed, []string{in}) {
		t.Errorf("moved %d, err %v, trashed %q", moved, err, *trashed)
	}

	for _, bad := range []string{d.Root, filepath.Dir(d.Root), filepath.Join(d.Root, "..", "x"), d.ClaudeJSON} {
		if _, err := s.TrashPaths([]string{bad}); err == nil {
			t.Errorf("TrashPaths(%q) did not refuse", bad)
		}
	}

	s.Trash = func(p string) error {
		if strings.Contains(p, "debug") {
			return errors.New("no trash here")
		}
		return nil
	}
	moved, err = s.TrashPaths([]string{
		filepath.Join(d.Root, "shell-snapshots", "s.sh"),
		filepath.Join(d.Root, "debug", "d.txt"),
		filepath.Join(d.Root, "transcripts", "old.jsonl"),
	})
	if moved != 1 || err == nil || !strings.Contains(err.Error(), "d.txt") {
		t.Errorf("moved %d, err %v: want 1 moved and an error naming d.txt", moved, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./store/ -run 'TestSessionPaths|TestCleanup|TestTrashPaths'`
Expected: FAIL to build: `s.SessionPaths undefined`.

- [ ] **Step 3: Implement**

`store/cleanup.go`:

```go
package store

import (
	"fmt"
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
	live := s.LiveIDs()
	now := s.Now()
	cutoff := now.AddDate(0, 0, -days)
	skip := func(it Item) bool { return live[filepath.Base(it.Path)] || now.Sub(it.Modified) < recent }
	ids := map[string]bool{}
	for _, p := range projects {
		for _, x := range p.Sessions {
			ids[x.ID] = true
		}
	}
	orphan := func(it Item) bool { return len(ids) > 0 && !ids[filepath.Base(it.Path)] }

	groups := []Group{
		s.entries("session-env", "session-env", nil, skip),
		s.entries("paste-cache", "paste-cache", nil, skip),
		s.entries("shell-snapshots", "shell-snapshots", nil, skip),
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

// TrashPaths moves paths to the Trash in order and stops at the first that
// fails, naming it. It refuses the root itself and anything outside it.
func (s *Store) TrashPaths(paths []string) (moved int, err error) {
	for _, p := range paths {
		if !s.inside(p) {
			return moved, fmt.Errorf("%s is not inside %s", p, s.Root)
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
```

- [ ] **Step 4: Run the tests**

Run: `go test ./store/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add store
git commit -m "feat(store): cleanup groups and trashing inside the root only

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Configuration

**Files:**
- Create: `store/config.go`, `store/config_test.go`

**Interfaces:**
- Consumes: `item`, `Item`.
- Produces: `store.Doc{Name, Path, Text string}`, `store.Config{Settings, MCP, Plugins map[string]any; Skills, Commands, Memory []Doc; Backups []Item; Errs []string}`, `(*Store).Config() Config`.

- [ ] **Step 1: Write the failing test**

`store/config_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func names(ds []Doc) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}

func TestConfig(t *testing.T) {
	s, d, _ := newStore(t)
	cfg := s.Config()
	if len(cfg.Errs) > 0 {
		t.Fatalf("errors: %q", cfg.Errs)
	}
	if env, _ := cfg.Settings["env"].(map[string]any); env["EDITOR"] != "vim" {
		t.Errorf("settings env %v", cfg.Settings["env"])
	}
	if _, ok := cfg.MCP["local"]; !ok {
		t.Error("MCP lacks local, from settings.json")
	}
	if _, ok := cfg.MCP["remote"]; !ok {
		t.Error("MCP lacks remote, from .claude.json")
	}
	if cfg.Plugins["installed"] == nil || cfg.Plugins["marketplaces"] == nil {
		t.Errorf("plugins %v", cfg.Plugins)
	}
	if got := names(cfg.Skills); len(got) != 1 || got[0] != "demo" {
		t.Errorf("skills %q", got)
	}
	if got := names(cfg.Commands); len(got) != 1 || got[0] != "hello" {
		t.Errorf("commands %q", got)
	}
	want := []string{"CLAUDE.md", "-Users-me-proj/memory/MEMORY.md", "-Users-me-proj/mygo-memory/MEMORY.md"}
	if got := names(cfg.Memory); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("memory %q, want %q", got, want)
	}
	if len(cfg.Backups) != 2 {
		t.Errorf("%d backups, want 2", len(cfg.Backups))
	}

	if err := os.WriteFile(filepath.Join(d.Root, "settings.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := s.Config(); len(cfg.Errs) != 1 {
		t.Errorf("errors for broken settings.json: %q", cfg.Errs)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./store/ -run TestConfig`
Expected: FAIL to build: `s.Config undefined`.

- [ ] **Step 3: Implement**

`store/config.go`:

```go
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// Doc is a Markdown file shown as is.
type Doc struct{ Name, Path, Text string }

// Config is what Claude Code is configured with. The app only reads it.
type Config struct {
	Settings map[string]any // settings.json
	MCP      map[string]any // mcpServers of settings.json and .claude.json
	Plugins  map[string]any // plugins/installed_plugins.json and known_marketplaces.json
	Skills   []Doc          // skills/*/SKILL.md
	Commands []Doc          // commands/**/*.md
	Memory   []Doc          // CLAUDE.md and projects/*/*memory/*.md
	Backups  []Item         // settings.json.bak*
	Errs     []string       // files that exist but could not be read
}

func (s *Store) Config() Config {
	var cfg Config
	read := func(path string) map[string]any {
		b, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				cfg.Errs = append(cfg.Errs, err.Error())
			}
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			cfg.Errs = append(cfg.Errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		return m
	}
	cfg.Settings = read(filepath.Join(s.Root, "settings.json"))
	cfg.MCP = map[string]any{}
	for _, m := range []map[string]any{cfg.Settings, read(s.ClaudeJSON)} {
		if servers, ok := m["mcpServers"].(map[string]any); ok {
			maps.Copy(cfg.MCP, servers)
		}
	}
	cfg.Plugins = map[string]any{
		"installed":    read(filepath.Join(s.Root, "plugins", "installed_plugins.json")),
		"marketplaces": read(filepath.Join(s.Root, "plugins", "known_marketplaces.json")),
	}
	cfg.Skills = docs(filepath.Join(s.Root, "skills", "*", "SKILL.md"), func(p string) string { return filepath.Base(filepath.Dir(p)) })
	cfg.Commands = commands(filepath.Join(s.Root, "commands"))
	projects := filepath.Join(s.Root, "projects")
	cfg.Memory = append(docs(filepath.Join(s.Root, "CLAUDE.md"), filepath.Base),
		docs(filepath.Join(projects, "*", "*memory", "*.md"), func(p string) string {
			r, _ := filepath.Rel(projects, p)
			return r
		})...)
	baks, _ := filepath.Glob(filepath.Join(s.Root, "settings.json.bak*"))
	for _, p := range baks {
		cfg.Backups = append(cfg.Backups, item(p))
	}
	return cfg
}

func docs(pattern string, name func(path string) string) []Doc {
	files, _ := filepath.Glob(pattern)
	var out []Doc
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			out = append(out, Doc{Name: name(f), Path: f, Text: string(b)})
		}
	}
	return out
}

// commands lists the .md files under dir, named by their path in it
// without .md, as Claude Code names slash commands.
func commands(dir string) []Doc {
	var out []Doc
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil {
			r, _ := filepath.Rel(dir, p)
			out = append(out, Doc{Name: strings.TrimSuffix(r, ".md"), Path: p, Text: string(b)})
		}
		return nil
	})
	return out
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./store/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add store
git commit -m "feat(store): read settings, MCP servers, plugins, skills, commands, memory

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: View plumbing, formatting and the Overview screen

**Files:**
- Create: `view/format.go`, `view/format_test.go`, `view/overview.go`, `view/overview_test.go`, `view/helpers_test.go`
- Modify: `view/app.go`

**Interfaces:**
- Consumes: `store.Store.Scan`, `store.Entry`, `fixture.*`.
- Produces: `(*App).run(work func() (apply func()))`, `(*App).trash(paths func() []string)`, `picked[T, K](s *ui.DataTableState[T, K], rows []T) []*T`, `humanSize(int64) string`, `fmtTime(time.Time) string`, `errText(error) string`, `containsFold(s, sub string) bool`, `clip(string) string`; type `overview` with `init()`, `load(*App)`, `view(*ui.Context, *App)`; App field `overview overview`; test helper `newApp(t) (*App, *ui.Tester, fixture.Dirs, *[]string)`.

- [ ] **Step 1: Write the failing tests**

`view/helpers_test.go`:

```go
package view

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/internal/fixture"
	"github.com/lang315/claude-manager/store"
)

// newApp shows the app on a fresh fixture. Work runs inline, and its result
// lands on the next frame, as Window.Update would land it.
func newApp(t *testing.T) (*App, *ui.Tester, fixture.Dirs, *[]string) {
	t.Helper()
	d := fixture.New(t)
	var trashed []string
	a := New(&store.Store{
		Root:       d.Root,
		ClaudeJSON: d.ClaudeJSON,
		Now:        func() time.Time { return fixture.Now },
		Alive:      fixture.Alive,
		Trash: func(p string) error {
			trashed = append(trashed, p)
			return os.RemoveAll(p)
		},
	})
	a.Go = func(f func()) { f() }
	tt := ui.NewTester(a.View, 1200, 800)
	a.Update = tt.Update
	a.Rescan()
	tt.Frame()
	return a, tt, d, &trashed
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
```

`view/format_test.go`:

```go
package view

import "testing"

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KB", 4_617_089_843: "4.3 GB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}
```

`view/overview_test.go`:

```go
package view

import "testing"

func TestOverviewOpensScreen(t *testing.T) {
	a, tt, _, _ := newApp(t)
	for _, name := range []string{"projects", "paste-cache", "settings.json"} {
		if !tt.HasText(name) {
			t.Errorf("overview lacks %q", name)
		}
	}
	must(t, tt.Click("projects"))
	must(t, tt.Click("Open"))
	if a.page != "sessions" {
		t.Errorf("page %q after opening projects, want sessions", a.page)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./view/`
Expected: FAIL to build: `undefined: humanSize`.

- [ ] **Step 3: Implement formatting**

`view/format.go`:

```go
package view

import (
	"fmt"
	"strings"
	"time"
)

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// maxShown is how much of a tool call or result the transcript shows.
const maxShown = 20000

// clip cuts text the transcript shows to maxShown bytes.
// shortcut: cuts on a byte, so a multi-byte rune at the cut may show as �; fine for logs, cut on a rune if users notice.
func clip(s string) string {
	if len(s) <= maxShown {
		return s
	}
	return s[:maxShown] + fmt.Sprintf("\n… %s more", humanSize(int64(len(s)-maxShown)))
}
```

- [ ] **Step 4: Add the plumbing to `view/app.go`**

Add the field after `notice`:

```go
	overview overview
```

Change `New` to:

```go
func New(s *store.Store) *App {
	a := &App{Store: s, Go: func(f func()) { go f() }, page: "overview"}
	a.overview.init()
	return a
}
```

Replace the body comment of `Rescan` with:

```go
	a.overview.load(a)
	// Each screen adds its load here.
```

Replace the switch comment with:

```go
			case "overview":
				a.overview.view(c, a)
			// Each screen adds its case here.
```

Append to `view/app.go` (add import `"fmt"`):

```go
// run does work off the main thread, then applies the change it returns on
// the main thread.
func (a *App) run(work func() (apply func())) {
	a.busy++
	a.Go(func() {
		apply := work()
		a.Update(func() {
			a.busy--
			apply()
		})
	})
}

// trash moves the paths that paths returns to the Trash, off the main
// thread, then reports and rescans.
func (a *App) trash(paths func() []string) {
	a.run(func() func() {
		moved, err := a.Store.TrashPaths(paths())
		return func() {
			a.notice = fmt.Sprintf("Moved %d items to the Trash", moved)
			if err != nil {
				a.notice = fmt.Sprintf("Moved %d items to the Trash, then stopped: %v", moved, err)
			}
			a.Rescan()
		}
	})
}

// picked returns the rows a table's actions apply to: the checked rows, or
// the chosen ones when none is checked.
func picked[T any, K comparable](s *ui.DataTableState[T, K], rows []T) []*T {
	set := &s.Checked
	if set.Len() == 0 {
		set = &s.Chosen
	}
	var out []*T
	for i := range rows {
		if set.Has(s.Key(&rows[i])) {
			out = append(out, &rows[i])
		}
	}
	return out
}
```

- [ ] **Step 5: Implement the Overview screen**

`view/overview.go`:

```go
package view

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

type overview struct {
	rows  []store.Entry
	max   int64
	err   string
	table ui.DataTableState[store.Entry, string]
}

func (o *overview) init() {
	bySize := func(x, y *store.Entry) int { return cmp.Compare(x.Size, y.Size) }
	o.table.Key = func(e *store.Entry) string { return e.Name }
	o.table.NoSearchBar = true
	o.table.Columns = []ui.DataColumn[store.Entry]{
		{TableColumn: ui.TableColumn{Title: "Name", Width: 240}, Text: func(e *store.Entry) string { return e.Name },
			Compare: func(x, y *store.Entry) int { return cmp.Compare(x.Name, y.Name) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 90}, Text: func(e *store.Entry) string { return humanSize(e.Size) }, Compare: bySize},
		{TableColumn: ui.TableColumn{Title: "Share", Width: 200}, Text: func(e *store.Entry) string { return humanSize(e.Size) }, Compare: bySize,
			Cell: func(c *ui.Context, e *store.Entry) {
				ui.Meter(c, float64(e.Size), 0, float64(max(o.max, 1)), nil).Label(e.Name + " share").Grow(1)
			}},
		{TableColumn: ui.TableColumn{Title: "Files", Width: 80}, Text: func(e *store.Entry) string { return strconv.Itoa(e.Files) },
			Compare: func(x, y *store.Entry) int { return cmp.Compare(x.Files, y.Files) }},
		{TableColumn: ui.TableColumn{Title: "Cleanable", Width: 90}, Text: func(e *store.Entry) string {
			if e.Cleanable {
				return "yes"
			}
			return ""
		}},
	}
}

func (o *overview) load(a *App) {
	a.run(func() func() {
		rows, err := a.Store.Scan()
		return func() {
			o.rows, o.err, o.max = rows, errText(err), 0
			for _, e := range rows {
				o.max = max(o.max, e.Size)
			}
			o.table.Reset()
		}
	})
}

func (o *overview) view(c *ui.Context, a *App) {
	if o.err != "" {
		ui.Text(c, o.err).TextColor(c.Theme().Danger)
	}
	target := ""
	if e := o.table.Current(); e != nil {
		target = screenOf(e)
	}
	if ui.Button(c, "Open").Disabled(target == "").Clicked() {
		a.page = target
	}
	ui.DataTable(c, &o.table, o.rows).Grow(1)
}

// screenOf returns the screen that shows an entry, "" for none.
func screenOf(e *store.Entry) string {
	switch {
	case e.Name == "projects":
		return "sessions"
	case e.Name == "settings.json", e.Name == "CLAUDE.md", e.Name == "skills", e.Name == "plugins",
		e.Name == "commands", strings.HasPrefix(e.Name, "settings.json.bak"):
		return "config"
	case e.Cleanable:
		return "cleanup"
	}
	return ""
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./view/ && CGO_ENABLED=0 go build ./...`
Expected: `ok`; build exits 0.

- [ ] **Step 7: Commit**

```bash
git add view
git commit -m "feat(view): overview of ~/.claude sizes, off-thread loading

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Sessions screen and trashing sessions

**Files:**
- Create: `view/sessions.go`, `view/sessions_test.go`
- Modify: `view/app.go`

**Interfaces:**
- Consumes: `store.Project`, `store.Session`, `(*Store).Projects`, `(*Store).SessionPaths`, `(*App).run`, `(*App).trash`, `picked`, `humanSize`, `fmtTime`, `containsFold`, `errText`.
- Produces: type `sessions` with `init()`, `load(*App)`, `view(*ui.Context, *App)`, fields `projects []store.Project`, `table ui.DataTableState[store.Session, string]`, `open *store.Session` (nil while the table shows; Task 9 fills the transcript), `sessionLabel(*store.Session) string`. App field `sessions sessions`. Task 11 adds the cleanup reload to `load`.

- [ ] **Step 1: Write the failing tests**

`view/sessions_test.go`:

```go
package view

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestTrashSession(t *testing.T) {
	a, tt, d, trashed := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("/Users/me/proj"))
	must(t, tt.Click("Login bug fix"))
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("Move 1 sessions to the Trash?") {
		t.Fatalf("no confirmation; texts %q", tt.Texts())
	}
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	want := []string{
		filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"),
		filepath.Join(d.Root, fixture.Proj, fixture.Old),
		filepath.Join(d.Root, "file-history", fixture.Old),
		filepath.Join(d.Root, "session-env", fixture.Old),
	}
	if !slices.Equal(*trashed, want) {
		t.Errorf("trashed %q, want %q", *trashed, want)
	}
	if tt.HasText("Login bug fix") {
		t.Error("the trashed session still shows after the rescan")
	}
	if a.busy != 0 {
		t.Errorf("busy %d after the move", a.busy)
	}
}

func TestTrashSkipsLiveSession(t *testing.T) {
	a, tt, d, trashed := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("/Users/me/proj"))
	must(t, tt.Click("Live work (live)"))
	tt.Click("Move to Trash…") // disabled: a click does nothing
	if tt.HasText("to the Trash?") || len(*trashed) > 0 {
		t.Fatalf("a live session alone can be trashed: %q", *trashed)
	}

	a.sessions.table.Checked.Add(fixture.Old)
	a.sessions.table.Checked.Add(fixture.Live)
	tt.Frame()
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("1 live sessions are left alone") {
		t.Errorf("the confirmation does not say a live session is skipped; texts %q", tt.Texts())
	}
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	for _, p := range *trashed {
		if filepath.Base(p) == fixture.Live || filepath.Base(p) == fixture.Live+".jsonl" {
			t.Errorf("trashed live session data %s", p)
		}
	}
	if len(*trashed) != 4 || (*trashed)[0] != filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl") {
		t.Errorf("trashed %q, want only the old session's 4 paths", *trashed)
	}
}

func TestSessionSearch(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("/Users/me/proj"))
	a.sessions.table.SetSearch("Session", "login")
	tt.Frame()
	if a.sessions.table.Len() != 1 || !tt.HasText("Login bug fix") || tt.HasText("Late title") {
		t.Errorf("search for login shows %d rows; texts %q", a.sessions.table.Len(), tt.Texts())
	}
	a.sessions.table.SetSearch("Session", "paste")
	tt.Frame()
	if a.sessions.table.Len() != 1 || !tt.HasText("Late title") {
		t.Errorf("search by first prompt shows %d rows", a.sessions.table.Len())
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./view/ -run 'TestTrash|TestSessionSearch'`
Expected: FAIL: `ui: no element shows "/Users/me/proj"`.

- [ ] **Step 3: Implement**

`view/sessions.go`:

```go
package view

import (
	"cmp"
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lang315/claude-manager/store"
)

type sessions struct {
	projects []store.Project
	err      string
	split    float32
	ptable   ui.DataTableState[store.Project, string]
	table    ui.DataTableState[store.Session, string]
	shown    string // Dir of the project whose sessions the table shows
	rows     []store.Session
	asking   bool
	pending  []store.Session
	skipped  int

	// The transcript shown, nil while the tables show (transcript.go).
	open    *store.Session
	msgs    []msgRow
	bad     int
	loading bool
	gen     int // which open the messages arriving belong to
	list    *ui.ListState
	md      markdown.Options
}

func sessionLabel(x *store.Session) string {
	label := cmp.Or(x.Title, x.FirstPrompt, "(untitled)")
	if x.Live {
		label += " (live)"
	}
	return label
}

func (s *sessions) init() {
	s.split = 320
	bySize := func(x, y *store.Project) int { return cmp.Compare(x.Size, y.Size) }
	s.ptable.Key = func(p *store.Project) string { return p.Dir }
	s.ptable.NoSearchBar = true
	s.ptable.Columns = []ui.DataColumn[store.Project]{
		{TableColumn: ui.TableColumn{Title: "Project", Width: 200}, Text: func(p *store.Project) string { return p.Path },
			Compare: func(x, y *store.Project) int { return cmp.Compare(x.Path, y.Path) }},
		{TableColumn: ui.TableColumn{Title: "Count", Width: 60}, Text: func(p *store.Project) string { return strconv.Itoa(len(p.Sessions)) },
			Compare: func(x, y *store.Project) int { return cmp.Compare(len(x.Sessions), len(y.Sessions)) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 80}, Text: func(p *store.Project) string { return humanSize(p.Size) }, Compare: bySize},
	}

	s.table.Key = func(x *store.Session) string { return x.ID }
	s.table.Footer = true
	s.table.SearchDelay = 200 * time.Millisecond
	s.table.Columns = []ui.DataColumn[store.Session]{
		{TableColumn: ui.TableColumn{Title: "Session", Width: 380}, Text: sessionLabel,
			Match: func(x *store.Session, kw string) bool {
				return containsFold(x.Title, kw) || containsFold(x.FirstPrompt, kw)
			},
			Cell: func(c *ui.Context, x *store.Session) {
				t := ui.Text(c, sessionLabel(x)).NoWrap()
				if x.Live {
					t.Opacity(0.5)
				}
			}},
		{TableColumn: ui.TableColumn{Title: "Modified", Width: 140}, Text: func(x *store.Session) string { return fmtTime(x.Modified) }, NoSearch: true,
			Compare: func(x, y *store.Session) int { return x.Modified.Compare(y.Modified) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 90}, Text: func(x *store.Session) string { return humanSize(x.Size) }, NoSearch: true,
			Compare: func(x, y *store.Session) int { return cmp.Compare(x.Size, y.Size) }},
		{TableColumn: ui.TableColumn{Title: "Subagents", Width: 80}, Text: func(x *store.Session) string { return strconv.Itoa(x.Subagents) }, NoSearch: true,
			Compare: func(x, y *store.Session) int { return cmp.Compare(x.Subagents, y.Subagents) }},
	}
}

func (s *sessions) load(a *App) {
	a.run(func() func() {
		projects, err := a.Store.Projects()
		return func() { s.set(projects, err) }
	})
}

func (s *sessions) set(projects []store.Project, err error) {
	s.projects, s.err = projects, errText(err)
	s.ptable.Reset()
	s.shown = "" // the sessions table takes the new rows on the next frame
}

func (s *sessions) view(c *ui.Context, a *App) {
	if s.open != nil {
		s.transcript(c)
		return
	}
	if s.err != "" {
		ui.Text(c, s.err).TextColor(c.Theme().Danger)
	}
	ui.Split(c, &s.split, func() {
		ui.Column(c).Fill().Children(func() {
			ui.DataTable(c, &s.ptable, s.projects).Grow(1)
		})
	}, func() {
		ui.Column(c).Fill().Gap(8).Children(func() { s.sessionTable(c, a) })
	}).Grow(1)
}

func (s *sessions) sessionTable(c *ui.Context, a *App) {
	p := s.ptable.Current()
	if p == nil {
		ui.Text(c, "Choose a project").TextColor(c.Theme().TextMuted)
		return
	}
	if p.Dir != s.shown {
		s.shown, s.rows = p.Dir, p.Sessions
		s.table.Checked.Clear()
		s.table.Chosen.Clear()
		s.table.Reset()
	}
	targets, live := s.targets()
	ui.Row(c).Gap(8).Children(func() {
		cur := s.table.Current()
		if ui.Button(c, "Open Transcript").Disabled(cur == nil).Clicked() {
			s.openTranscript(a, *cur)
		}
		if ui.Button(c, "Move to Trash…").Disabled(len(targets) == 0).Clicked() {
			s.pending, s.skipped, s.asking = targets, live, true
		}
	})
	ui.DataTable(c, &s.table, s.rows).Grow(1)
	s.confirm(c, a)
}

// targets are the sessions to trash, less the live ones, which it counts.
func (s *sessions) targets() (out []store.Session, live int) {
	for _, x := range picked(&s.table, s.rows) {
		if x.Live {
			live++
		} else {
			out = append(out, *x)
		}
	}
	return out, live
}

func (s *sessions) confirm(c *ui.Context, a *App) {
	var size int64
	for _, x := range s.pending {
		size += x.Size
	}
	msg := humanSize(size) + " moves to the Trash, with each session's subagents, file history and environment. You can put it back from the Trash."
	if s.skipped > 0 {
		msg += fmt.Sprintf(" %d live sessions are left alone.", s.skipped)
	}
	title := fmt.Sprintf("Move %d sessions to the Trash?", len(s.pending))
	if ui.AlertDialog(c, &s.asking, title, msg, "Cancel", "Move to Trash") == 1 {
		pending := s.pending
		a.trash(func() []string {
			var paths []string
			for _, x := range pending {
				paths = append(paths, a.Store.SessionPaths(x)...)
			}
			return paths
		})
	}
}
```

Create a stub `view/transcript.go` so the package builds (Task 9 replaces it):

```go
package view

import (
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

type msgRow struct {
	store.Message
	open bool
}

func (s *sessions) openTranscript(a *App, x store.Session) {}

func (s *sessions) transcript(c *ui.Context) {}
```

- [ ] **Step 4: Wire the screen into `view/app.go`**

Add the field `sessions sessions` after `overview overview`; in `New` add `a.sessions.init()` after `a.overview.init()`; in `Rescan` add `a.sessions.load(a)` after `a.overview.load(a)`; in the switch add before the marker comment:

```go
			case "sessions":
				a.sessions.view(c, a)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./view/ && CGO_ENABLED=0 go build ./...`
Expected: `ok`; build exits 0.

- [ ] **Step 6: Commit**

```bash
git add view
git commit -m "feat(view): sessions by project, trash with live sessions left alone

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Transcript

**Files:**
- Modify: `view/transcript.go` (replace the stub)
- Create: `view/transcript_test.go`

**Interfaces:**
- Consumes: `store.ReadTranscript`, `store.Message`, `store.FirstLine`, fields `open`, `msgs`, `bad`, `loading`, `gen`, `list`, `md` of `sessions`, `(*App).run`, `clip`, `fmtTime`, `sessionLabel`.
- Produces: `(*sessions).openTranscript(*App, store.Session)`, `(*sessions).transcript(*ui.Context)`.

- [ ] **Step 1: Write the failing test**

`view/transcript_test.go`:

```go
package view

import (
	"strings"
	"testing"
)

func TestTranscript(t *testing.T) {
	a, tt, _, _ := newApp(t)
	// count is how many texts contain s: HasText would also match the
	// collapsed label "tool_result · package auth".
	count := func(s string) (n int) {
		for _, x := range tt.Texts() {
			if strings.Contains(x, s) {
				n++
			}
		}
		return n
	}
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("/Users/me/proj"))
	must(t, tt.Click("Login bug fix"))
	must(t, tt.Click("Open Transcript"))
	tt.Frame()
	if n := len(a.sessions.msgs); n != 6 {
		t.Fatalf("%d messages, want 6", n)
	}
	for _, s := range []string{
		"1 unreadable lines",
		`tool_use · Read {"file_path":"auth.go"}`,
		"thinking · Look at auth.go",
		"6 messages",
	} {
		if !tt.HasText(s) {
			t.Errorf("transcript lacks %q; texts %q", s, tt.Texts())
		}
	}
	if n := count("package auth"); n != 1 {
		t.Errorf("%d texts hold the tool result before it is expanded, want 1 (its label)", n)
	}
	must(t, tt.Click("tool_result · package auth"))
	tt.Frame()
	if n := count("package auth"); n != 2 {
		t.Errorf("%d texts hold the tool result once expanded, want 2 (label and body)", n)
	}
	must(t, tt.Click("Back"))
	if a.sessions.open != nil || !tt.HasText("Open Transcript") {
		t.Error("Back does not return to the table")
	}
	if cur := a.sessions.table.Current(); cur == nil || cur.Title != "Login bug fix" {
		t.Error("Back loses the chosen row")
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./view/ -run TestTranscript`
Expected: FAIL: `0 messages, want 6` (the stub loads nothing).

- [ ] **Step 3: Implement**

Replace `view/transcript.go`:

```go
package view

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lang315/claude-manager/store"
)

type msgRow struct {
	store.Message
	open bool // a collapsed block (thinking, tool call, tool result) is expanded
}

// openTranscript shows a session's transcript, its messages coming in as
// they are read.
func (s *sessions) openTranscript(a *App, x store.Session) {
	s.open, s.msgs, s.bad, s.loading = &x, nil, 0, true
	s.list = &ui.ListState{}
	s.gen++
	gen := s.gen
	a.run(func() func() {
		bad, err := store.ReadTranscript(x.File, func(ms []store.Message) {
			a.Update(func() {
				if s.gen != gen {
					return
				}
				for _, m := range ms {
					s.msgs = append(s.msgs, msgRow{Message: m})
				}
			})
		})
		return func() {
			if s.gen != gen {
				return
			}
			s.bad, s.loading = bad, false
			if err != nil {
				a.notice = err.Error()
			}
		}
	})
}

func (s *sessions) transcript(c *ui.Context) {
	t := c.Theme()
	x := s.open
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, "Back").Clicked() {
			s.open = nil
			s.gen++ // messages still arriving are dropped
		}
		ui.Text(c, sessionLabel(x)).Bold().NoWrap()
		if s.bad > 0 {
			ui.Badge(c, fmt.Sprintf("%d unreadable lines", s.bad))
		}
	})
	status := ""
	if s.loading {
		status = " · loading…"
	}
	ui.Textf(c, "%s · %s · Claude Code %s · %s – %s · %d messages%s",
		x.Cwd, x.Branch, x.Version, fmtTime(x.Started), fmtTime(x.Modified), len(s.msgs), status).
		TextColor(t.TextMuted).FontSize(12)
	ui.List(c, s.list, len(s.msgs), func(i int) { s.message(c, &s.msgs[i]) }).Grow(1).Gap(8)
}

func (s *sessions) message(c *ui.Context, m *msgRow) {
	t := c.Theme()
	who := "You"
	if m.Role == "assistant" {
		who = "Claude"
	}
	ui.Column(c).Gap(4).Padding(8).Children(func() {
		ui.Text(c, who+" · "+fmtTime(m.Time)).FontSize(11).TextColor(t.TextMuted)
		if m.Kind == "text" {
			markdown.View(c, m.Text, &s.md)
			return
		}
		ui.Collapsible(c, m.Kind+" · "+store.FirstLine(m.Text, 120), &m.open, func() {
			ui.Text(c, clip(m.Text)).Font("monospace").Selectable()
		})
	})
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./view/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add view
git commit -m "feat(view): read a session's transcript, tool calls collapsed

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Config screen

**Files:**
- Create: `view/config.go`, `view/config_test.go`
- Modify: `view/app.go`

**Interfaces:**
- Consumes: `(*Store).Config`, `store.Config`, `store.Doc`, `store.Item`, `(*App).run`, `(*App).trash`, `picked`, `humanSize`, `fmtTime`.
- Produces: type `configPage` with `init()`, `load(*App)`, `view(*ui.Context, *App)`; `scalar(key string, v any) string`; var `secretKey`. App field `config configPage`.

- [ ] **Step 1: Write the failing tests**

`view/config_test.go`:

```go
package view

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScalarMasksSecrets(t *testing.T) {
	for _, c := range []struct {
		key  string
		v    any
		want string
	}{
		{"API_TOKEN", "secret123", "••••••"},
		{"Authorization", "Bearer abc", "••••••"},
		{"OPENAI_API_KEY", "sk-1", "••••••"},
		{"EDITOR", "vim", `"vim"`},
		{"count", 3.0, "3"},
		{"on", true, "true"},
		{"none", nil, "null"},
	} {
		if got := scalar(c.key, c.v); got != c.want {
			t.Errorf("scalar(%q, %v) = %q, want %q", c.key, c.v, got, c.want)
		}
	}
}

func TestConfigMasksSecrets(t *testing.T) {
	_, tt, _, _ := newApp(t)
	must(t, tt.Click("Config"))
	must(t, tt.Click("env"))
	if !tt.HasText("API_TOKEN: ••••••") || !tt.HasText(`EDITOR: "vim"`) {
		t.Errorf("env not shown masked; texts %q", tt.Texts())
	}
	must(t, tt.Click("MCP servers"))
	must(t, tt.Click("remote"))
	must(t, tt.Click("headers"))
	if !tt.HasText("Authorization: ••••••") {
		t.Errorf("MCP header not masked; texts %q", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if strings.Contains(s, "secret123") || strings.Contains(s, "Bearer abc") {
			t.Errorf("a credential shows: %q", s)
		}
	}
}

func TestConfigDocsAndBackups(t *testing.T) {
	_, tt, d, trashed := newApp(t)
	must(t, tt.Click("Config"))
	must(t, tt.Click("Memory"))
	must(t, tt.Click("-Users-me-proj/memory/MEMORY.md"))
	if !tt.HasText("likes Go") {
		t.Errorf("memory file not rendered; texts %q", tt.Texts())
	}
	must(t, tt.Click("Backups"))
	must(t, tt.Click("settings.json.bak-1"))
	must(t, tt.Click("Move to Trash…"))
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	if len(*trashed) != 1 || (*trashed)[0] != filepath.Join(d.Root, "settings.json.bak-1") {
		t.Errorf("trashed %q, want only settings.json.bak-1", *trashed)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./view/ -run 'TestScalar|TestConfig'`
Expected: FAIL to build: `undefined: scalar`.

- [ ] **Step 3: Implement**

`view/config.go`:

```go
package view

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lang315/claude-manager/store"
)

var configTabs = []string{"Settings", "Hooks", "MCP servers", "Plugins", "Skills", "Commands", "Memory", "Backups"}

type configPage struct {
	cfg     store.Config
	tab     int
	open    map[string]*bool // tree items expanded, by path
	doc     string           // Path of the doc shown on the Skills, Commands and Memory tabs
	md      markdown.Options
	backups ui.DataTableState[store.Item, string]
	asking  bool
	pending []store.Item
}

func (p *configPage) init() {
	p.open = map[string]*bool{}
	p.backups.Key = func(x *store.Item) string { return x.Path }
	p.backups.NoSearchBar = true
	p.backups.Columns = []ui.DataColumn[store.Item]{
		{TableColumn: ui.TableColumn{Title: "File", Width: 320}, Text: func(x *store.Item) string { return filepath.Base(x.Path) }},
		{TableColumn: ui.TableColumn{Title: "Modified", Width: 140}, Text: func(x *store.Item) string { return fmtTime(x.Modified) },
			Compare: func(x, y *store.Item) int { return x.Modified.Compare(y.Modified) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 90}, Text: func(x *store.Item) string { return humanSize(x.Size) }},
	}
}

func (p *configPage) load(a *App) {
	a.run(func() func() {
		cfg := a.Store.Config()
		return func() {
			p.cfg = cfg
			p.backups.Reset()
		}
	})
}

func (p *configPage) view(c *ui.Context, a *App) {
	for _, e := range p.cfg.Errs {
		ui.Text(c, e).TextColor(c.Theme().Danger)
	}
	ui.Tabs(c, &p.tab, configTabs...)
	switch p.tab {
	case 0:
		p.tree(c, "settings", p.cfg.Settings)
	case 1:
		p.tree(c, "hooks", p.cfg.Settings["hooks"])
	case 2:
		p.tree(c, "mcp", p.cfg.MCP)
	case 3:
		p.tree(c, "plugins", p.cfg.Plugins)
	case 4:
		p.docs(c, p.cfg.Skills)
	case 5:
		p.docs(c, p.cfg.Commands)
	case 6:
		p.docs(c, p.cfg.Memory)
	case 7:
		p.backupTable(c, a)
	}
}

func (p *configPage) tree(c *ui.Context, root string, v any) {
	if v == nil {
		ui.Text(c, "Nothing configured").TextColor(c.Theme().TextMuted)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Tree(c, func() { p.node(c, root, "", v) })
	})
}

// node builds a JSON value as tree items; the root ("" key) builds only its
// children. A click on an item's label opens it, as its arrow does.
func (p *configPage) node(c *ui.Context, path, key string, v any) {
	item := func(label string, children func()) {
		open := p.opened(path)
		if ui.TreeItem(c.Key(path), label, open, children).Clicked() {
			*open = !*open
		}
	}
	switch x := v.(type) {
	case map[string]any:
		build := func() {
			for _, k := range slices.Sorted(maps.Keys(x)) {
				p.node(c, path+"/"+k, k, x[k])
			}
		}
		if key == "" {
			build()
			return
		}
		item(key, build)
	case []any:
		build := func() {
			for i, e := range x {
				p.node(c, fmt.Sprintf("%s/%d", path, i), strconv.Itoa(i), e)
			}
		}
		if key == "" {
			build()
			return
		}
		item(fmt.Sprintf("%s [%d]", key, len(x)), build)
	default:
		ui.TreeItem(c.Key(path), key+": "+scalar(key, x), nil, nil)
	}
}

func (p *configPage) opened(path string) *bool {
	b := p.open[path]
	if b == nil {
		b = new(bool)
		p.open[path] = b
	}
	return b
}

// secretKey matches keys whose values are credentials, which the tree hides.
// shortcut: by key name only; a token inside a command line or an args list still shows. Match values too if one leaks in a screenshot.
var secretKey = regexp.MustCompile(`(?i)token|secret|password|passwd|api[-_]?key|auth|credential`)

func scalar(key string, v any) string {
	if s, ok := v.(string); ok {
		if secretKey.MatchString(key) {
			return "••••••"
		}
		return strconv.Quote(s)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (p *configPage) docs(c *ui.Context, ds []store.Doc) {
	if len(ds) == 0 {
		ui.Text(c, "Nothing here").TextColor(c.Theme().TextMuted)
		return
	}
	ui.Row(c).Grow(1).Gap(12).Children(func() {
		ui.Sidebar(c, &p.doc, func() {
			for _, d := range ds {
				ui.SidebarItem(c.Key(d.Path), d.Path, nil, d.Name)
			}
		}).Width(280)
		ui.Scroll(c).Grow(1).Children(func() {
			for _, d := range ds {
				if d.Path == p.doc {
					markdown.View(c, d.Text, &p.md)
				}
			}
		})
	})
}

func (p *configPage) backupTable(c *ui.Context, a *App) {
	targets := picked(&p.backups, p.cfg.Backups)
	if ui.Button(c, "Move to Trash…").Disabled(len(targets) == 0).Clicked() {
		p.pending = nil
		for _, x := range targets {
			p.pending = append(p.pending, *x)
		}
		p.asking = true
	}
	ui.DataTable(c, &p.backups, p.cfg.Backups).Grow(1)
	title := fmt.Sprintf("Move %d settings backups to the Trash?", len(p.pending))
	if ui.AlertDialog(c, &p.asking, title, "settings.json itself is not touched. You can put them back from the Trash.", "Cancel", "Move to Trash") == 1 {
		pending := p.pending
		a.trash(func() []string {
			var out []string
			for _, x := range pending {
				out = append(out, x.Path)
			}
			return out
		})
	}
}
```

- [ ] **Step 4: Wire the screen into `view/app.go`**

Add the field `config configPage` after `sessions sessions`; in `New` add `a.config.init()`; in `Rescan` add `a.config.load(a)`; in the switch add before the marker comment:

```go
			case "config":
				a.config.view(c, a)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./view/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add view
git commit -m "feat(view): read-only config with credentials masked, backups to Trash

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Cleanup screen

**Files:**
- Create: `view/cleanup.go`, `view/cleanup_test.go`
- Modify: `view/app.go`, `view/sessions.go`

**Interfaces:**
- Consumes: `(*Store).Cleanup`, `store.Group`, `sessions.projects`, `(*App).run`, `(*App).trash`, `humanSize`.
- Produces: type `cleanup` with fields `days float64`, `groups []store.Group`, `on []bool`; methods `set([]store.Group)`, `load(*App)`, `view(*ui.Context, *App)`. App field `cleanup cleanup`.

- [ ] **Step 1: Write the failing tests**

`view/cleanup_test.go`:

```go
package view

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestCleanupTrashesCheckedGroups(t *testing.T) {
	_, tt, d, trashed := newApp(t)
	must(t, tt.Click("Cleanup"))
	must(t, tt.Click("paste-cache"))
	must(t, tt.Click("debug"))
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("Move 2 items to the Trash?") {
		t.Fatalf("no confirmation; texts %q", tt.Texts())
	}
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	want := []string{filepath.Join(d.Root, "paste-cache", "p1.txt"), filepath.Join(d.Root, "debug", "d.txt")}
	if !slices.Equal(*trashed, want) {
		t.Errorf("trashed %q, want %q", *trashed, want)
	}
}

// The view must never touch the disk on the main thread: until the work
// handed to App.Go runs, nothing is trashed.
func TestCleanupTrashesOffTheMainThread(t *testing.T) {
	a, tt, _, trashed := newApp(t)
	var queued []func()
	a.Go = func(f func()) { queued = append(queued, f) }
	must(t, tt.Click("Cleanup"))
	must(t, tt.Click("paste-cache"))
	must(t, tt.Click("Move to Trash…"))
	must(t, tt.Click("Move to Trash"))
	if len(*trashed) != 0 {
		t.Fatalf("trashed on the main thread: %q", *trashed)
	}
	if len(queued) != 1 {
		t.Fatalf("%d jobs queued, want 1", len(queued))
	}
	queued[0]()
	if len(*trashed) != 1 {
		t.Errorf("trashed %q after the job ran, want 1 path", *trashed)
	}
}

func TestCleanupDays(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Cleanup"))
	if !tt.HasText("transcripts older than 30 days") {
		t.Fatalf("texts %q", tt.Texts())
	}
	a.cleanup.days = 1
	a.cleanup.load(a)
	tt.Frame()
	if !tt.HasText("transcripts older than 1 days") {
		t.Errorf("days not applied; texts %q", tt.Texts())
	}
	for _, g := range a.cleanup.groups {
		if g.Name == "transcripts older than 1 days" && len(g.Items) != 2 {
			t.Errorf("%d transcripts older than 1 day, want 2", len(g.Items))
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./view/ -run TestCleanup`
Expected: FAIL: `ui: no element shows "paste-cache"` (the Cleanup page is empty).

- [ ] **Step 3: Implement**

`view/cleanup.go`:

```go
package view

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

type cleanup struct {
	days    float64
	groups  []store.Group
	on      []bool // groups checked, by index: the order of groups is fixed
	asking  bool
	pending []string
	size    int64
}

func (p *cleanup) set(groups []store.Group) {
	p.groups = groups
	if len(p.on) != len(groups) {
		p.on = make([]bool, len(groups))
	}
}

// load finds the groups again from the sessions already listed.
func (p *cleanup) load(a *App) {
	projects, days := a.sessions.projects, int(p.days)
	a.run(func() func() {
		groups := a.Store.Cleanup(projects, days)
		return func() { p.set(groups) }
	})
}

func (p *cleanup) view(c *ui.Context, a *App) {
	t := c.Theme()
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, "Older than")
		if ui.NumberInput(c, &p.days, 1, 3650, 1).Width(90).Label("Days").Changed() {
			p.load(a)
		}
		ui.Text(c, "days")
	})
	var total int64
	count := 0
	for i, g := range p.groups {
		ui.Row(c.Key(i)).Gap(8).Children(func() {
			ui.Checkbox(c, &p.on[i], g.Name)
			ui.Spacer(c)
			ui.Textf(c, "%d items · %s", len(g.Items), humanSize(g.Size)).TextColor(t.TextMuted)
		})
		if p.on[i] {
			total += g.Size
			count += len(g.Items)
		}
	}
	ui.Textf(c, "Checked: %d items · %s", count, humanSize(total)).Bold()
	if ui.PrimaryButton(c, "Move to Trash…").Disabled(count == 0).Clicked() {
		p.pending, p.size = nil, total
		for i, g := range p.groups {
			if p.on[i] {
				for _, it := range g.Items {
					p.pending = append(p.pending, it.Path)
				}
			}
		}
		p.asking = true
	}
	msg := humanSize(p.size) + " moves to the Trash. Live sessions and anything changed in the last 10 minutes are left alone. You can put it back from the Trash."
	if ui.AlertDialog(c, &p.asking, fmt.Sprintf("Move %d items to the Trash?", len(p.pending)), msg, "Cancel", "Move to Trash") == 1 {
		pending := p.pending
		a.trash(func() []string { return pending })
	}
}
```

- [ ] **Step 4: Load the groups with the sessions**

In `view/sessions.go`, replace `load` so the groups come from the same listing:

```go
func (s *sessions) load(a *App) {
	days := int(a.cleanup.days)
	a.run(func() func() {
		projects, err := a.Store.Projects()
		groups := a.Store.Cleanup(projects, days)
		return func() {
			s.set(projects, err)
			a.cleanup.set(groups)
		}
	})
}
```

In `view/app.go`: add the field `cleanup cleanup` after `config configPage`; in `New` add `a.cleanup.days = 30`; in the switch add before the marker comment:

```go
			case "cleanup":
				a.cleanup.view(c, a)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./... && CGO_ENABLED=0 go build ./...`
Expected: `ok` for every package; build exits 0.

- [ ] **Step 6: Commit**

```bash
git add view
git commit -m "feat(view): cleanup groups with preview and confirmation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Verify on the real machine

**Files:**
- Create: `view/screenshot_test.go`
- Modify: `docs/roadmap.md` (only if something found here is deferred)

**Interfaces:**
- Consumes: everything.
- Produces: evidence: test, vet and build output, PNGs of each screen from the fixture, and a manual run on the real `~/.claude`.

- [ ] **Step 1: Write the screenshot test**

`view/screenshot_test.go`:

```go
package view

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestScreenshots writes a PNG of each screen, drawn from the fixture, into
// $SCREENSHOT_DIR. It shows no real data, so the PNGs can go in a PR.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set SCREENSHOT_DIR to write PNGs of each screen")
	}
	_, tt, _, _ := newApp(t)
	shots := []struct{ name, click, then string }{
		{"overview", "Overview", ""},
		{"sessions", "Sessions", "/Users/me/proj"},
		{"config", "Config", "env"},
		{"cleanup", "Cleanup", "paste-cache"},
	}
	for _, s := range shots {
		must(t, tt.Click(s.click))
		if s.then != "" {
			must(t, tt.Click(s.then))
		}
		f, err := os.Create(filepath.Join(dir, s.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
```

- [ ] **Step 2: Run every check and keep the output**

```bash
go test ./... 2>&1 | tee /tmp/cm-test.txt; echo "exit=$?"
go vet ./... ; echo "vet exit=$?"
CGO_ENABLED=0 go build -o build/claude-manager . ; echo "build exit=$?"
go run ../mygo/cmd/mygo vet ; echo "mygo vet exit=$?"
mkdir -p build/shots && SCREENSHOT_DIR=$PWD/build/shots go test ./view/ -run TestScreenshots -v
ls -la build/claude-manager build/shots
```

Expected: every exit 0, four PNGs. If `mygo vet` does not exist in this mygo version, record its error output instead of claiming it passed.

- [ ] **Step 3: Look at the PNGs**

Open each PNG (Read tool) and check: the sidebar shows four entries, tables have rows, the Config tree shows `API_TOKEN: ••••••`, the Cleanup screen shows the groups with sizes. Fix any layout problem in the view it belongs to, rerun Step 2.

- [ ] **Step 4: Run on the real `~/.claude`**

```bash
./build/claude-manager
```

Check by hand, without trashing anything you need:
1. Overview lists `projects` near the top at about 4 GB.
2. Sessions: the projects list shows real paths; a session opens and its transcript loads progressively; the session of this very Claude Code run shows `(live)` and its Move to Trash is disabled.
3. Config: settings tree, MCP servers from `~/.claude.json` with tokens masked.
4. Cleanup: groups and sizes appear; the window stays responsive while scanning.
5. Trash one harmless item (a `paste-cache` entry) and confirm it is in the macOS Trash.

Screenshots of the real data hold private prompts and paths: keep them local, do not commit or attach them.

- [ ] **Step 5: Commit**

```bash
git add view/screenshot_test.go
git commit -m "test(view): screenshots of each screen from the fixture

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

After this task, the roadmap's v1.x item (UI/UX polish with the `impeccable` skill) is the next piece of work.
