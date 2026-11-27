# Keep Mac Awake Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A "Keep Mac Awake" switch, in the menu bar's menu and the window's sidebar, that keeps the Mac from idle sleep while on.

**Architecture:** `view.App` gets an injected `KeepAwake` func (as `Reveal`, `OpenURL`) and holds the release func it returns; non-nil means on. The tray row and the sidebar checkbox both call one `setAwake(bool)`. `main.go` injects `mygo.Power.KeepAwake(reason, false)`.

**Tech Stack:** Go, MyGo (`mygo.Power`, package `ui`, `ui.Tester`).

**Spec:** `docs/superpowers/specs/2026-11-27-keep-awake-design.md`

## Global Constraints

- Menu label exactly `Keep Mac Awake`; window label exactly `Keep Mac awake`.
- Reason exactly `Keep Mac Awake is on in Claude Manager`.
- Window tooltip exactly `The display may still turn off. Closing the lid on battery still sleeps.`
- Display may sleep: `KeepAwake(reason, false)`.
- Not saved: every launch starts off. No prefs key.
- `KeepAwake == nil`: neither the row nor the checkbox shows.
- No mygo change, no `caffeinate` process.

## Review Focus

- Turning on twice (tray then window, same frame or not) must hold one activity, not two: on while on is a no-op.
- Turning off while off must not call a nil release.
- Tray with `Accounts == nil` (early return "Limits are unavailable.") still shows the row: the switch has nothing to do with accounts.
- The tray row and the sidebar checkbox show the same state after either is used.
- Quit/crash releases: covered by macOS ending the process's activity; nothing to test in-app.

---

### Task 1: The switch in view

**Files:**
- Modify: `view/app.go` (App fields near `Reveal`; sidebar in `View`)
- Modify: `view/tray.go` (`Tray()`: early return and before the Appearance rows; new `setAwake`, `awakeItem`)
- Test: `view/tray_test.go`

**Interfaces:**
- Produces: `App.KeepAwake func(reason string) (release func())` (exported field, nil hides the switch); unexported `App.awake func()`; `func (a *App) setAwake(on bool)`; `func (a *App) awakeItem() TrayItem`.

- [ ] **Step 1: Write the failing tests** in `view/tray_test.go`

```go
// fakeAwake counts held activities.
func fakeAwake(l *lim) *int {
	held := 0
	l.a.KeepAwake = func(reason string) func() {
		if reason != "Keep Mac Awake is on in Claude Manager" {
			panic(reason)
		}
		held++
		return func() { held-- }
	}
	return &held
}

func TestTrayKeepAwake(t *testing.T) {
	l := limitsApp(t)
	held := fakeAwake(l)
	it := trayItem(t, l, "Keep Mac Awake")
	if !it.Toggle || it.Checked || *held != 0 {
		t.Fatalf("starts off: %+v held %d", it, *held)
	}
	l.tt.Update(it.Do)
	if !trayItem(t, l, "Keep Mac Awake").Checked || *held != 1 {
		t.Fatalf("on: held %d", *held)
	}
	l.tt.Update(func() { l.a.setAwake(true) }) // on while on
	if *held != 1 {
		t.Fatalf("on twice holds %d", *held)
	}
	l.tt.Update(trayItem(t, l, "Keep Mac Awake").Do)
	l.tt.Update(func() { l.a.setAwake(false) }) // off while off
	if trayItem(t, l, "Keep Mac Awake").Checked || *held != 0 {
		t.Fatalf("off: held %d", *held)
	}
}

func TestWindowKeepAwake(t *testing.T) {
	l := limitsApp(t)
	held := fakeAwake(l)
	l.tt.Frame()
	must(t, l.tt.Click("Keep Mac awake"))
	l.tt.Frame()
	if *held != 1 || !trayItem(t, l, "Keep Mac Awake").Checked {
		t.Fatalf("window on: held %d", *held)
	}
	must(t, l.tt.Click("Keep Mac awake"))
	l.tt.Frame()
	if *held != 0 {
		t.Fatalf("window off: held %d", *held)
	}
}

func TestKeepAwakeHiddenWithoutPower(t *testing.T) {
	l := limitsApp(t) // KeepAwake nil
	l.tt.Frame()
	if l.tt.HasText("Keep Mac awake") {
		t.Error("window checkbox shown without KeepAwake")
	}
	l.tt.Update(func() {
		for _, it := range func() []TrayItem { _, i := l.a.Tray(); return i }() {
			if it.Label == "Keep Mac Awake" {
				t.Error("tray row shown without KeepAwake")
			}
		}
	})
}

func TestTrayKeepAwakeWithoutAccounts(t *testing.T) {
	l := limitsApp(t)
	fakeAwake(l)
	l.a.Accounts = nil
	trayItem(t, l, "Keep Mac Awake")
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./view -run 'KeepAwake' -count=1`
Expected: FAIL to compile: `l.a.KeepAwake undefined`.

- [ ] **Step 3: Implement**

- `view/app.go`: field `KeepAwake func(reason string) (release func())` with a doc comment beside `Reveal`; unexported `awake func() // KeepAwake's release; non-nil while on`.
- `view/tray.go`: `setAwake(on bool)`: no-op when `on == (a.awake != nil)` or `KeepAwake == nil`; on sets `a.awake = a.KeepAwake("Keep Mac Awake is on in Claude Manager")`; off calls and clears it. `awakeItem()` returns `TrayItem{Label: "Keep Mac Awake", Toggle: true, Checked: a.awake != nil, Do: func() { a.setAwake(a.awake == nil) }}`. In `Tray()`, append it (when `KeepAwake != nil`) plus a separator before the Appearance loop, and to the `Accounts == nil` early-return list.
- `view/app.go` `View`: wrap `ui.Sidebar(...)` in a `ui.Column` keeping its `Width(200).Background(pal.ground)`, sidebar `Grow(1)`, then, when `KeepAwake != nil`, a padded `ui.Checkbox(c, &on, "Keep Mac awake").Tooltip(<constraint copy>)` with `on := a.awake != nil` and `if on != (a.awake != nil) { a.setAwake(on) }` (the `auto.go:76` pattern).

- [ ] **Step 4: Run to verify they pass, then the whole package**

Run: `go test ./view -run 'KeepAwake' -count=1 && go test ./... -count=1`
Expected: `ok` for every package.

- [ ] **Step 5: Commit**

```bash
git add view/app.go view/tray.go view/tray_test.go
git commit -m "feat(view): Keep Mac Awake switch in the menu bar and sidebar"
```

### Task 2: Wire mygo, document, check on the real Mac

**Files:**
- Modify: `main.go` (beside `a.OpenURL, a.CopyText, a.OpenPath = ...`, ~line 84)
- Modify: `PRODUCT.md` (Positioning)

**Interfaces:**
- Consumes: `App.KeepAwake` from Task 1; `mygo.Power.KeepAwake(reason string, display bool) (release func())`.

- [ ] **Step 1: Inject** `a.KeepAwake = func(r string) func() { return mygo.Power.KeepAwake(r, false) }` in the GUI branch only (where `a.Reveal` is set).

- [ ] **Step 2: PRODUCT.md** — one sentence in Positioning: Keep Mac Awake, off at every launch, stops the Mac's idle sleep while on (the display may still turn off) and ends with the app.

- [ ] **Step 3: Build and test**

Run: `go vet ./... && go test ./... -count=1 && go build -o "$TMPDIR/claude-manager-keepawake" .`
Expected: no output from vet, `ok` everywhere, build exit 0.

- [ ] **Step 4: Real run** — launch the build, turn on Keep Mac Awake from the menu bar, then:

Run: `pmset -g assertions | grep -i "Keep Mac Awake"`
Expected: one line with `PreventUserIdleSystemSleep` and the reason. Turn it off in the window; the same command prints nothing. Turn on, Quit; prints nothing.

- [ ] **Step 5: Commit**

```bash
git add main.go PRODUCT.md
git commit -m "feat: Keep Mac Awake uses mygo.Power.KeepAwake"
```
