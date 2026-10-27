# Live Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The screens follow `~/.claude` as Claude Code writes to it, so the user no longer has to press Rescan.

**Architecture:**
- **Watching:** `mygo.WatchFiles` watches the root (`HintsOnly`, recursive, 1 s debounce, 5 s max delay), plus `~/.claude.json` when it lies outside the root. A goroutine turns each hint into a set of screens with the pure function `screensFor`.
- **Pacing:** a main-thread type, `live`, starts quiet reloads of those screens while the app is active, holds them while it is in the background, and never runs one screen twice at once.
- **Quiet reloads:** they go through `App.run` without touching `busy`.
- **The user's place:** three fixes make sure a reload keeps what the user chose.

**Tech Stack:** Go 1.27, `github.com/egoist/mygo` (`WatchFiles`, `App.OnDidBecomeActive`/`OnDidResignActive`), MyGo `ui`, `ui.Tester`.

**Spec:** `docs/superpowers/specs/2026-10-27-live-refresh-design.md`

## Global Constraints

- **Read-only:** the app never writes in `~/.claude`. Tests never read the real `~/.claude`; they use `fixture.New` or `t.TempDir()`.
- **Watch options:**
  - the root: `FileWatchOptions{Recursive: true, HintsOnly: true, Debounce: time.Second, MaxDelay: 5 * time.Second}`;
  - `~/.claude.json`: `FileWatchOptions{Debounce: time.Second, MaxDelay: 5 * time.Second}`.
- **Exact UI string:** the toast `Live refresh is off: <error>`.
- **Quiet reloads leave `busy` alone.** A manual Rescan and the reload after a Trash stay loud and count in `busy`.
- **Every commit:**
  - Before it, `gofmt -l .` prints nothing, `go vet ./...` exits 0, and `CGO_ENABLED=1 go test -race ./...` exits 0.
  - Its message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. No session URLs.

## Review Focus

1. **The watch hints `.`, at start or after the system lost events.** Everything reloads once. A hint for a screen that is already loading starts it again only after it lands. Test: `TestLiveHintWhileLoading` (Task 2).
2. **A quiet reload and a manual Rescan of the same screen overlap.** `busy` returns to 0, never below it. Test: `TestQuietAndLoudReloadsCount` (Task 3).
3. **The app stays in the background through many hints.** It gets one reload per screen when it becomes active. Test: `TestLiveHoldsInBackground` (Task 2).
4. **The watch ends with an error while the app runs.** One toast appears, and Rescan keeps working. Test: `TestFollowEndsWithToast` (Task 4).
5. **The newest session of a merged project lands in its other folder.** The chosen project and its ticks survive. Test: `TestSessionsReloadKeepsMergedProject` (Task 1).

---

## File Structure

| File | Change |
|---|---|
| `view/app.go` | `keepOnly`; `App.quiet`, `App.live`; quiet `run`; `reload`; `SetActive` |
| `view/sessions.go` | The project key becomes `Path`; a reload keeps the ticks (`fresh`) |
| `view/config.go` | A reload keeps backup ticks that are still listed |
| `view/live.go` (new) | `screens`, `screensFor`, `live`, `hints`, `follow`, `Watch` |
| `view/live_test.go` (new) | Tests for pacing, mapping, quiet reloads, follow and integration |
| `view/sessions_test.go`, `view/config_test.go` | Place-keeping tests |
| `view/usage.go` | The landing reports `live.landed(usageScreen)` |
| `main.go` | Starts `Watch`; `OnDidBecomeActive`/`OnDidResignActive` call `SetActive` |
| `docs/roadmap.md`, the spec | Tick the item; the `projects/<dir>/memory` row |

---

### Task 1: A reload keeps the user's place

**Files:**
- Modify: `view/app.go` (add `keepOnly` after `picked`), `view/sessions.go:66`, `:126`, `:180-185`, `view/config.go:64`
- Test: `view/sessions_test.go`, `view/config_test.go`

**Interfaces:**
- Produces:
  - `func keepOnly[K comparable](s *ui.Selection[K], keep map[K]bool)`;
  - the field `sessions.fresh bool`;
  - `ptable.Key` is `Project.Path`, and `sessions.shown` holds a `Path`.

- [ ] **Step 1: Write the failing tests**

Append to `view/sessions_test.go`, adding imports that are missing (`os`, `path/filepath`):

```go
// A reload keeps the ticked sessions that are still there, and drops the
// ticks of sessions gone.
func TestSessionsReloadKeepsTicks(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("me/proj"))
	tt.Frame()
	a.sessions.table.Checked.Add(fixture.Old)
	a.sessions.table.Checked.Add(fixture.Late)
	tt.Frame()
	must(t, os.Remove(filepath.Join(d.Root, fixture.Proj, fixture.Late+".jsonl")))
	a.Rescan()
	tt.Frame()
	tt.Frame()
	if !a.sessions.table.Checked.Has(fixture.Old) || a.sessions.table.Checked.Has(fixture.Late) {
		t.Errorf("after a reload: Old ticked %v (want true), Late ticked %v (want false)",
			a.sessions.table.Checked.Has(fixture.Old), a.sessions.table.Checked.Has(fixture.Late))
	}
}

// A merged project stays chosen, with its ticks, when its newest session
// lands in its other folder: the project table is keyed by path.
func TestSessionsReloadKeepsMergedProject(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("me/proj"))
	tt.Frame()
	a.sessions.table.Checked.Add(fixture.Old)
	tt.Frame()
	dir := filepath.Join(d.Root, "projects", "-Users-me-proj-v2")
	must(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "99999999-9999-4999-8999-999999999999.jsonl")
	must(t, os.WriteFile(file, []byte(`{"type":"user","cwd":"/Users/me/proj","timestamp":"2026-10-26T11:00:00.000Z","message":{"role":"user","content":"Moved folder"}}`+"\n"), 0o644))
	must(t, os.Chtimes(file, fixture.Now, fixture.Now))
	a.Rescan()
	tt.Frame()
	tt.Frame()
	p := a.sessions.ptable.Current()
	if p == nil || p.Path != "/Users/me/proj" || !a.sessions.table.Checked.Has(fixture.Old) {
		t.Errorf("after the merge: project %v, Old ticked %v", p, a.sessions.table.Checked.Has(fixture.Old))
	}
}
```

Append to `view/config_test.go`:

```go
// A reload keeps the ticks of backups still listed.
func TestBackupsReloadKeepsTicks(t *testing.T) {
	a, tt, d, _ := newApp(t)
	must(t, tt.Click("Config"))
	must(t, tt.Click("Backups"))
	p := filepath.Join(d.Root, "settings.json.bak-1")
	a.config.backups.Checked.Add(p)
	tt.Frame()
	a.Rescan()
	tt.Frame()
	if !a.config.backups.Checked.Has(p) {
		t.Error("a reload dropped the tick of a backup still there")
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestSessionsReloadKeepsTicks|TestSessionsReloadKeepsMergedProject|TestBackupsReloadKeepsTicks'`

Expected: all three FAIL, with `Old ticked false (want true)`, `after the merge: project <nil>…` (or `Old ticked false`), and `a reload dropped the tick`.

- [ ] **Step 3: Add `keepOnly`**

In `view/app.go`, after `picked`, add the following (and `"slices"` to the imports if it is missing):

```go
// keepOnly drops the keys of s that keep doesn't hold: rows gone since the
// last load. A reload keeps the user's other choices.
func keepOnly[K comparable](s *ui.Selection[K], keep map[K]bool) {
	for _, k := range slices.Collect(s.All()) {
		if !keep[k] {
			s.Remove(k)
		}
	}
}
```

- [ ] **Step 4: Key projects by path, and keep session ticks across a reload**

In `view/sessions.go`:
- add the field `fresh bool // set has new rows for the project shown` after `shown string`, and change `shown`'s comment to `// Path of the project whose sessions the table shows`;
- in `init`, change `s.ptable.Key = func(p *store.Project) string { return p.Dir }` to `return p.Path`;
- in `set`, replace `s.shown = "" // the sessions table takes the new rows on the next frame` with `s.fresh = true // the sessions table takes the new rows on the next frame`;
- in `sessionTable`, replace

```go
	if p.Dir != s.shown {
		s.shown, s.rows = p.Dir, p.Sessions
		s.table.Checked.Clear()
		s.table.Chosen.Clear()
		s.table.Reset()
	}
```

with

```go
	if p.Path != s.shown { // another project: its own ticks
		s.shown, s.fresh = p.Path, true
		s.table.Checked.Clear()
		s.table.Chosen.Clear()
	}
	if s.fresh { // new rows: a reload keeps the ticks of sessions still there
		s.fresh, s.rows = false, p.Sessions
		ids := map[string]bool{}
		for _, x := range p.Sessions {
			ids[x.ID] = true
		}
		keepOnly(&s.table.Checked, ids)
		keepOnly(&s.table.Chosen, ids)
		s.table.Reset()
	}
```

Run: `grep -n 'shown' view/*.go`

Expected: no other use relies on `shown` being a `Dir`.

- [ ] **Step 5: Keep backup ticks across a reload**

In `view/config.go`'s `load`, replace `p.backups.Checked.Clear() // the files checked may be gone` with:

```go
			listed := map[string]bool{}
			for _, x := range cfg.Backups {
				listed[x.Path] = true
			}
			keepOnly(&p.backups.Checked, listed) // a trashed file's tick goes
```

- [ ] **Step 6: Run the tests and confirm they pass**

Run: `go test ./view -v -run 'TestSessions|TestBackups|TestTrash|TestOverview' 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`

Expected: `ok`, with no failures. `TestBackupsChecksClearOnReload` must stay green.

- [ ] **Step 7: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/app.go view/sessions.go view/config.go view/sessions_test.go view/config_test.go
git commit -m "fix(view): a reload keeps ticked sessions and backups, and the chosen project by path

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Which screens a change affects, and the pacing

**Files:**
- Create: `view/live.go`, `view/live_test.go`
- Modify: `docs/superpowers/specs/2026-10-27-live-refresh-design.md` (the memory row)

**Interfaces:**
- Produces:
  - `type screens uint8`, with `overviewScreen`, `sessionsScreen`, `historyScreen`, `usageScreen`, `configScreen` and `allScreens`;
  - `func screensFor(path string) screens`;
  - `type live struct { active bool; pending, inflight screens; start func(screens) }`, with the methods `hint(screens)`, `landed(screens)`, `activate()` and `resign()`.

- [ ] **Step 1: Write the failing tests**

Create `view/live_test.go`:

```go
package view

import "testing"

func TestScreensFor(t *testing.T) {
	for path, want := range map[string]screens{
		".":                       allScreens,
		"projects":                sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj": sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj/55555555-5555-4555-8555-555555555555/subagents": sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj/memory": sessionsScreen | usageScreen | overviewScreen | configScreen,
		"sessions":                       sessionsScreen,
		"plugins/marketplaces/x":         configScreen | overviewScreen,
		"skills/demo":                    configScreen | overviewScreen,
		"commands":                       configScreen | overviewScreen,
		"agents":                         configScreen | overviewScreen,
		"file-history/abc":               sessionsScreen | overviewScreen,
		"debug":                          sessionsScreen | overviewScreen,
	} {
		if got := screensFor(path); got != want {
			t.Errorf("screensFor(%q) = %05b, want %05b", path, got, want)
		}
	}
}

// newLive is a live whose starts are recorded, not run.
func newLive() (*live, *[]screens) {
	var started []screens
	return &live{active: true, start: func(s screens) { started = append(started, s) }}, &started
}

func TestLiveHintStarts(t *testing.T) {
	l, started := newLive()
	l.hint(sessionsScreen | usageScreen)
	if len(*started) != 1 || (*started)[0] != sessionsScreen|usageScreen || l.inflight != sessionsScreen|usageScreen {
		t.Errorf("started %v, inflight %05b", *started, l.inflight)
	}
}

// A hint for a screen that is loading waits for it to land, then runs once.
func TestLiveHintWhileLoading(t *testing.T) {
	l, started := newLive()
	l.hint(sessionsScreen)
	l.hint(sessionsScreen | historyScreen)
	l.hint(allScreens)
	if len(*started) != 3 || (*started)[1] != historyScreen || (*started)[2] != overviewScreen|usageScreen|configScreen {
		t.Fatalf("started %v: a loading screen started twice", *started)
	}
	l.landed(sessionsScreen)
	if len(*started) != 4 || (*started)[3] != sessionsScreen {
		t.Errorf("after it landed, started %v; want sessions once more", *started)
	}
	l.landed(sessionsScreen)
	if len(*started) != 4 {
		t.Errorf("a second landing started %v", *started)
	}
}

// In the background, hints pile up and run once when the app is back.
func TestLiveHoldsInBackground(t *testing.T) {
	l, started := newLive()
	l.resign()
	for range 10 {
		l.hint(sessionsScreen)
		l.hint(configScreen)
	}
	if len(*started) != 0 {
		t.Fatalf("started %v in the background", *started)
	}
	l.activate()
	if len(*started) != 1 || (*started)[0] != sessionsScreen|configScreen {
		t.Errorf("on activation, started %v; want sessions and config once", *started)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestScreensFor|TestLive'`

Expected: a build failure, `undefined: screens`.

- [ ] **Step 3: Write `view/live.go`**

```go
package view

import "strings"

// screens is a set of the pages whose loads read ~/.claude. Cleanup is not
// one: the Sessions load reloads it.
type screens uint8

const (
	overviewScreen screens = 1 << iota
	sessionsScreen
	historyScreen
	usageScreen
	configScreen

	allScreens = overviewScreen | sessionsScreen | historyScreen | usageScreen | configScreen
)

// screensFor maps a folder the root's watch hints at, relative to the root
// and slash-separated, to the pages that read it. "." is a file at the
// root, or the system asking to look at everything.
func screensFor(path string) screens {
	parts := strings.Split(path, "/")
	switch parts[0] {
	case ".":
		return allScreens
	case "projects":
		s := sessionsScreen | usageScreen | overviewScreen
		if len(parts) >= 3 && parts[2] == "memory" { // a project's memory files: Config shows them
			s |= configScreen
		}
		return s
	case "sessions":
		return sessionsScreen
	case "plugins", "skills", "commands", "agents":
		return configScreen | overviewScreen
	}
	return sessionsScreen | overviewScreen
}

// live paces the reloads the watch asks for. While the app is active it
// starts them; in the background it holds them until the app is back. It
// never starts a screen still loading: that one waits, and runs once when
// it lands. It runs on the main thread.
type live struct {
	active            bool
	pending, inflight screens
	start             func(screens) // starts the quiet loads of a set
}

// hint asks for the screens of s to reload.
func (l *live) hint(s screens) {
	l.pending |= s
	l.run()
}

// landed says the quiet loads of s are done.
func (l *live) landed(s screens) {
	l.inflight &^= s
	l.run()
}

func (l *live) activate() {
	l.active = true
	l.run()
}

func (l *live) resign() { l.active = false }

func (l *live) run() {
	if !l.active {
		return
	}
	s := l.pending &^ l.inflight
	if s == 0 {
		return
	}
	l.pending &^= s
	l.inflight |= s
	l.start(s)
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./view -v -run 'TestScreensFor|TestLive' 2>&1 | grep -E '^(---|ok|FAIL)'`

Expected: all PASS.

- [ ] **Step 5: Record the memory row in the spec**

In the spec's "Which screens a path affects" table, change the `projects` row to:

```
| `projects` or `projects/…` | Sessions (which reloads Cleanup too), Usage, Overview; and Config for `projects/<dir>/memory`, where the memory files live |
```

- [ ] **Step 6: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/live.go view/live_test.go docs/superpowers/specs/2026-10-27-live-refresh-design.md
git commit -m "feat(view): map watch hints to screens and pace their reloads

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Quiet reloads

**Files:**
- Modify: `view/app.go` (`App` fields, `New`, `run`; add `reload`, `SetActive`), `view/usage.go` (`load`'s landing)
- Test: `view/live_test.go`

**Interfaces:**
- Consumes: `screens`, `live` and the `*Screen` constants (Task 2).
- Produces:
  - the fields `App.quiet screens` and `App.live live`;
  - `func (a *App) reload(s screens)`;
  - `func (a *App) SetActive(on bool)`.

- [ ] **Step 1: Write the failing tests**

Append to `view/live_test.go` (no new imports needed):

```go
// A quiet reload leaves busy alone and tells live when it lands.
func TestQuietReloadLeavesBusy(t *testing.T) {
	a, tt, q := heldApp(t)
	a.live.hint(sessionsScreen | historyScreen | configScreen | overviewScreen)
	if a.busy != 0 || a.live.inflight == 0 {
		t.Fatalf("busy %d, inflight %05b; want 0 and the screens loading", a.busy, a.live.inflight)
	}
	runQueued(q)
	tt.Frame()
	if a.busy != 0 || a.live.inflight != 0 {
		t.Errorf("after landing: busy %d, inflight %05b; want 0, 0", a.busy, a.live.inflight)
	}
}

// A Rescan during a quiet reload is still loud, and busy ends at 0.
func TestQuietAndLoudReloadsCount(t *testing.T) {
	a, tt, q := heldApp(t)
	a.live.hint(sessionsScreen)
	a.Rescan()
	if a.busy == 0 {
		t.Fatal("a Rescan during a quiet reload isn't counted")
	}
	runQueued(q)
	tt.Frame()
	runQueued(q)
	tt.Frame()
	if a.busy != 0 || a.live.inflight != 0 {
		t.Errorf("busy %d, inflight %05b; want 0, 0", a.busy, a.live.inflight)
	}
}

// A quiet Usage reload folded into a scan already running still lands.
func TestQuietUsageLandsDuringScan(t *testing.T) {
	a, tt, q := heldApp(t)
	a.Rescan() // a loud scan runs
	a.live.hint(usageScreen)
	for range 3 {
		runQueued(q)
		tt.Frame()
	}
	if a.live.inflight&usageScreen != 0 {
		t.Error("the quiet Usage reload never landed")
	}
}

// In the background, SetActive(false) holds hints until SetActive(true).
func TestSetActive(t *testing.T) {
	a, tt, q := heldApp(t)
	a.SetActive(false)
	tt.Frame()
	a.live.hint(historyScreen)
	if len(*q) != 0 {
		t.Fatal("a hint in the background started a load")
	}
	a.SetActive(true)
	tt.Frame()
	if len(*q) == 0 {
		t.Error("activation didn't start the held reload")
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestQuiet|TestSetActive'`

Expected: a build failure, `a.live undefined`.

- [ ] **Step 3: Quiet `run`, `reload` and `SetActive`**

In `view/app.go`'s `App` struct, after `notice string …`, add:

```go
	quiet  screens // the screen a quiet reload is starting for; 0 for a load the user started
	live   live    // paces the reloads the watch asks for
```

In `New`, after `a.cleanup.days = 30`, add:

```go
	a.live = live{active: true, start: a.reload}
```

Replace `run` with:

```go
// run does work off the main thread, then applies the change it returns on
// the main thread. A load the user started counts in busy; a quiet reload
// doesn't, and tells live when it lands.
func (a *App) run(work func() (apply func())) {
	q := a.quiet
	if q == 0 {
		a.busy++
	}
	a.Go(func() {
		apply := work()
		a.Update(func() {
			if q == 0 {
				a.busy--
			}
			apply()
			if q != 0 {
				a.live.landed(q)
			}
		})
	})
}

// reload starts the quiet loads of s: they leave busy alone, so Rescan and
// Trash stay enabled. TrashPaths checks each path again as it moves it.
func (a *App) reload(s screens) {
	for _, x := range []struct {
		s    screens
		load func(*App)
	}{
		{overviewScreen, a.overview.load}, {sessionsScreen, a.sessions.load}, {historyScreen, a.history.load},
		{usageScreen, a.usage.load}, {configScreen, a.config.load},
	} {
		if s&x.s != 0 {
			a.quiet = x.s
			x.load(a)
			a.quiet = 0
		}
	}
}

// SetActive tells the app whether it is in front. In the background, the
// reloads the watch asks for wait until it is back.
func (a *App) SetActive(on bool) {
	a.Update(func() {
		if on {
			a.live.activate()
		} else {
			a.live.resign()
		}
	})
}
```

- [ ] **Step 4: Usage reports its landing**

In `view/usage.go`'s `load`, inside the `a.Update(func() { … })`, add this just before `if u.again {`:

```go
			// Any scan that lands is the latest: a quiet reload folded into
			// it is done.
			a.live.landed(usageScreen)
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./view -race -v -run 'TestQuiet|TestSetActive|TestLive|TestUsage|TestSidebar' 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`

Expected: `ok`, with no failures.

- [ ] **Step 6: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/app.go view/usage.go view/live_test.go
git commit -m "feat(view): quiet reloads that leave busy alone, paced by live

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Watching `~/.claude`

**Files:**
- Modify: `view/live.go` (add `hints`, `follow`, `Watch`), `main.go`
- Test: `view/live_test.go`

**Interfaces:**
- Consumes: `screensFor`, `configScreen`, `App.live` and `App.Update` (Tasks 2 and 3).
- Produces:
  - `type hints interface { Next(context.Context) ([]mygo.FileEvent, error); Err() error }`;
  - `func (a *App) follow(ctx context.Context, w hints, affects func(string) screens)`;
  - `func (a *App) Watch(ctx context.Context, root, claudeJSON string)`.

- [ ] **Step 1: Write the failing tests**

Append to `view/live_test.go`, adding the imports `context`, `errors`, `io`, `os`, `path/filepath`, `time`, `github.com/egoist/mygo` and `github.com/lang315/claude-manager/internal/fixture`:

```go
// fakeHints hands out batches, then ends with err (io.EOF when nil).
type fakeHints struct {
	batches [][]mygo.FileEvent
	err     error
}

func (f *fakeHints) Next(context.Context) ([]mygo.FileEvent, error) {
	if len(f.batches) == 0 {
		return nil, io.EOF
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, nil
}

func (f *fakeHints) Err() error { return f.err }

func TestFollowHintsScreens(t *testing.T) {
	a, tt, q := heldApp(t)
	a.follow(context.Background(), &fakeHints{batches: [][]mygo.FileEvent{
		{{Op: mygo.FileHint, Path: "projects/-Users-me-proj"}},
	}}, screensFor)
	tt.Frame()
	if a.live.inflight != sessionsScreen|usageScreen|overviewScreen || len(*q) == 0 {
		t.Errorf("inflight %05b, %d queued", a.live.inflight, len(*q))
	}
}

func TestFollowEndsWithToast(t *testing.T) {
	a, _, _ := heldApp(t)
	a.Update = func(f func()) { f() }
	a.follow(context.Background(), &fakeHints{err: errors.New("mygo: file watch entry limit exceeded")}, screensFor)
	if a.notice != "Live refresh is off: mygo: file watch entry limit exceeded" {
		t.Errorf("notice %q", a.notice)
	}
	a.notice = ""
	a.follow(context.Background(), &fakeHints{}, screensFor) // closed, no error
	if a.notice != "" {
		t.Errorf("a clean end toasts %q", a.notice)
	}
}

// The real watch: a new session written under projects/ shows in Sessions.
func TestWatchFindsNewSession(t *testing.T) {
	a, tt, d, _ := newApp(t)
	before := a.sessions.count
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Watch(ctx, d.Root, d.ClaudeJSON)
	file := filepath.Join(d.Root, fixture.Other, "77777777-7777-4777-8777-777777777777.jsonl")
	must(t, os.WriteFile(file, []byte(`{"type":"user","cwd":"/Users/me/other","message":{"role":"user","content":"New"}}`+"\n"), 0o644))
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		tt.Frame()
		if a.sessions.count == before+1 {
			return
		}
	}
	t.Errorf("sessions %d, want %d within 3 s", a.sessions.count, before+1)
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./view -run 'TestFollow|TestWatch'`

Expected: a build failure, `a.follow undefined`.

- [ ] **Step 3: Write the watching code**

Append to `view/live.go`, and change its imports to `context`, `os`, `path/filepath`, `strings`, `time`, `github.com/egoist/mygo`:

```go
// hints is what follow reads: a mygo watch, or a test's fake.
type hints interface {
	Next(context.Context) ([]mygo.FileEvent, error)
	Err() error
}

// follow hands the screens each batch of changes affects to live, on the
// main thread, until the watch ends. A watch that ends with an error says
// so once; Rescan still works.
func (a *App) follow(ctx context.Context, w hints, affects func(string) screens) {
	for {
		events, err := w.Next(ctx)
		if err != nil {
			if werr := w.Err(); werr != nil {
				a.Update(func() { a.notice = "Live refresh is off: " + werr.Error() })
			}
			return
		}
		var s screens
		for _, e := range events {
			s |= affects(e.Path)
		}
		if s != 0 {
			a.Update(func() { a.live.hint(s) })
		}
	}
}

// Watch follows root, and claudeJSON when it lies outside root, until ctx
// ends. A watch that can't start says so in a toast; Rescan stays. Call it
// once the app is ready, off the main thread: it waits for the watch to
// start.
func (a *App) Watch(ctx context.Context, root, claudeJSON string) {
	opts := mygo.FileWatchOptions{Recursive: true, HintsOnly: true, Debounce: time.Second, MaxDelay: 5 * time.Second}
	if w, err := mygo.WatchFiles(ctx, root, opts); err != nil {
		a.Update(func() { a.notice = "Live refresh is off: " + err.Error() })
	} else {
		go a.follow(ctx, w, screensFor)
	}
	// ~/.claude.json lies beside the root, unless CLAUDE_CONFIG_DIR puts it in.
	if rel, err := filepath.Rel(root, claudeJSON); err == nil && !strings.HasPrefix(rel, "..") {
		return
	}
	if _, err := os.Stat(claudeJSON); err != nil {
		return // not there: a Rescan reads it once it is
	}
	w, err := mygo.WatchFiles(ctx, claudeJSON, mygo.FileWatchOptions{Debounce: time.Second, MaxDelay: 5 * time.Second})
	if err != nil {
		a.Update(func() { a.notice = "Live refresh is off: " + err.Error() })
		return
	}
	go a.follow(ctx, w, func(string) screens { return configScreen })
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./view -race -v -run 'TestFollow|TestWatch|TestQuiet|TestLive' 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`

Expected: `ok`. If `TestWatchFindsNewSession` is flaky under `-race` at 3 s, raise the deadline to 5 s, and record that as a ruling.

- [ ] **Step 5: Wire it into the app**

In `main.go`, add `"context"` to the imports. In `WhenReady`, after `a.Rescan()`, add:

```go
		go a.Watch(context.Background(), root, claudeJSON)
		mygo.App.OnDidBecomeActive(func() { a.SetActive(true) })
		mygo.App.OnDidResignActive(func() { a.SetActive(false) })
```

Run: `go build ./... && go vet ./...`

Expected: exit 0.

- [ ] **Step 6: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/live.go view/live_test.go main.go
git commit -m "feat: watch ~/.claude and reload what changes, while the app is active

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Roadmap and a check by hand

**Files:**
- Modify: `docs/roadmap.md`

- [ ] **Step 1: Tick the roadmap item**

In `docs/roadmap.md`, change `- [ ] Watch \`~/.claude\` for changes and refresh live instead of manual Rescan.` to `- [x] …`, with the rest of the line unchanged.

- [ ] **Step 2: Run the app and watch it follow**

```bash
go build -o "$SCRATCH/claude-manager" . && "$SCRATCH/claude-manager" &
```

`$SCRATCH` is the session scratchpad. With the app on Sessions, use Claude Code in another terminal. Expected: within about 5 s, the running session's size and modified time change in the table, and no "Scanning…" appears.

Then switch to another app for a minute, and come back. Expected: one refresh on return.

Stop the app afterwards.

- [ ] **Step 3: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add docs/roadmap.md
git commit -m "docs: tick live refresh on the roadmap

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
