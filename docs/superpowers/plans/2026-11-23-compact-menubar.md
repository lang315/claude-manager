# Compact Menu Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Shrink the menu bar item from `alice · 49% · 71%` (136 px) to a two-bar icon plus the fuller share (about 55 px).

**Architecture:**
- `view` draws the icon (a PNG) and builds the title and the tooltip.
- `main.go` puts all three on the `mygo.Tray`.
- The new default title mode, "compact", is stored as `""`.

**Tech Stack:** Go standard library (`image`, `image/png`); `mygo.Tray.SetIcon(png, template)` and `SetToolTip`.

**Spec:** `docs/superpowers/specs/2026-11-23-compact-menubar-design.md`

## Global Constraints

- Icon: 32×32 px (16 pt @2x), only black pixels with alpha, so it works as a template image.
- Each bar's track is 35% alpha. Its fill is opaque, with width proportional to the share, clamped to 0–100.
- The top bar is the 5-hour window, the bottom bar the 7-day window.
- Mode values: `compact` (stored `""`), `both`, `5h`, `7d`, `off`. `config get menubar.title` defaults to `compact`.

## Review Focus

- A share above 100 (extra usage), or below 0: the fill is clamped and nothing panics. Test in Task 1.
- An account in use with no limits read yet: empty tracks, title `""`. Test in Task 2.
- A file written before this change with `trayTitle: ""` now reads as compact, by design. Covered by the default test in Task 2.
- A refused session in compact mode: title `!`. Test in Task 2.
- The Show Account Name and Show Model Limits toggles while in compact mode: disabled. Test in Task 2.

---

### Task 1: Draw the icon

**Files:**
- Create: `view/trayicon.go`
- Test: `view/trayicon_test.go`

**Interfaces:**
- Produces: `func TrayIcon(five, seven float64) []byte`, which returns the PNG.

- [ ] **Step 1: Write a failing test, `TestTrayIconBars`.** Decode the PNG and check:
  - it is 32×32;
  - every pixel has R=G=B=0;
  - in each bar's row, the count of opaque pixels grows with the share;
  - `TrayIcon(0,0)` has no opaque pixel, and `TrayIcon(100,100)` has a full row;
  - `TrayIcon(150,-5)` equals `TrayIcon(100,0)`.
- [ ] **Step 2:** `go test ./view -run TrayIcon` fails: undefined.
- [ ] **Step 3: Implement.** Draw two bars, each 28 px wide and 10 px high, with 4 px between them, centred.
- [ ] **Step 4:** the test passes.

### Task 2: Compact title, tooltip and the mode setting

**Files:**
- Modify: `view/tray.go` (`trayTitle`, the `Menu Bar` submenu, a new `App.TrayIcon`, a new `App.TrayTip`)
- Modify: `account/prefs.go` (`menubar.title`)
- Test: `view/tray_test.go`, `account/prefs_test.go`

**Interfaces:**
- Consumes: `TrayIcon` from Task 1.
- Produces:
  - `func (a *App) TrayIcon() []byte`: the in-use account's 5h and 7d shares, drawn; empty tracks when there is none.
  - `func (a *App) TrayTip() string`: `name · 5h N% · 7d N%`.

- [ ] **Step 1: Write failing tests.**
  - In `TestTrayTitleModes`, `""` gives `"62%"`, and `"both"` gives `"alice · 62% · 40%"`. Add these cases:
    - compact, refused: `"!"`;
    - compact with no limits: `""`;
    - `"off"`: `""`, where it used to be `CM`.
  - In `TestTrayTitleSettings`: in compact mode, both toggles are `Disabled`. The existing flips run after setting `menubar.title=both`.
  - In `TestTrayTitleMode`, the submenu has `Title: Compact` first, and it is checked by default.
  - In the prefs tests:
    - `menubar.title` with `compact` stores `""`;
    - `both` stores `"both"`;
    - the `config get` default is `compact`;
    - `bad` gives the error `menubar.title is compact, both, 5h, 7d or off.`
- [ ] **Step 2:** the new cases fail.
- [ ] **Step 3: Implement.**
  - In compact mode, `trayTitle` returns the fuller share from `fullest()`, or `!` when refused.
  - `off` returns `""`, because the icon now carries the item.
  - Add `{"Title: Compact", "compact"}` to the `Menu Bar` submenu.
  - The prefs setter maps `compact` to `""`. Everything else is stored as given.
- [ ] **Step 4:** `go test ./view ./account ./cli` passes.

### Task 3: Put it on the tray

**Files:**
- Modify: `main.go` (`setTray`)

**Steps:**
- [ ] **Step 1: Set the icon and the tooltip.**
  - Call `tray.SetIcon(icon, true)` when `string(icon)` differs from the last icon set.
  - Call `tray.SetToolTip(a.TrayTip())` with the title.
  - The menu's `was` key stays as it is.
- [ ] **Step 2:** `go vet ./...`, `GOOS=windows` and `GOOS=linux` builds, and `go test -race ./...` all pass.
- [ ] **Step 3: Real check.** Build, install, and relaunch. Then
  `osascript -e 'tell application "System Events" to tell process "Claude Manager" to get size of every menu bar item of menu bar 2'`
  must report a width of about 55, where it was 136.
- [ ] **Step 4:** commit, push, and open a draft PR with this evidence.
