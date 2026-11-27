# Keep Mac Awake

## Intent

Let Claude Code finish a long task while the user is away from the Mac: a
switch, in the menu bar and in the window, that keeps the Mac from sleeping
when idle, as `caffeinate -i` does.

## Goals

- One on/off switch, "Keep Mac Awake", in the menu bar's menu and in the window.
- On: the Mac does not sleep when idle. The display may still turn off.
- Off, Quit, or a crash: the Mac sleeps as usual again.

## Non-goals

- Keeping the display on.
- Turning itself on or off with Claude sessions, or after a set time (approaches
  2 and 3 below; can come later).
- Keeping the Mac awake with the lid closed: macOS sleeps on lid close on battery
  whatever the app does.

## Anti-goals

- A Mac left awake after the app is gone.
- A Mac kept awake by a setting the user forgot from an earlier launch.

## Approaches

1. **Manual on/off (picked).** The user turns it on and off. Smallest, does what
   was asked. A forgotten switch keeps the Mac awake until off or Quit.
2. On, only while a Claude session runs. A `claude` waiting at its prompt counts
   as running, so it seldom lets go, and it needs the session scan running while
   the window is hidden. More code for little gain.
3. On for 1, 2 or 4 hours. A long task's length is hard to guess: too short and
   the Mac sleeps mid-task.

Where: mygo already has `mygo.Power.KeepAwake(reason, display) (release func())`
(`../mygo/power.go`), an `NSProcessInfo` activity that shows in
`pmset -g assertions` and ends with the process. No mygo change, no `caffeinate`
child process.

## Design

- `view.App` gets `KeepAwake func(reason string) (release func())`, set in
  `main.go` to `mygo.Power.KeepAwake(reason, false)`, as `Reveal` and `OpenURL`
  are injected; tests pass a fake. nil: the switch is hidden.
- `App` holds the release func; non-nil means on. Turning it on calls
  `KeepAwake("Keep Mac Awake is on in Claude Manager")`; off calls release.
- Menu bar: a checkbox row "Keep Mac Awake" in `view/tray.go`'s `Tray()`,
  before the Appearance rows.
- Window: a checkbox "Keep Mac awake" at the bottom of the sidebar, so every
  page shows it, with a tooltip: "The display may still turn off. Closing the
  lid on battery still sleeps."
- Not saved: every launch starts off (anti-goal: a forgotten setting).
- `PRODUCT.md` Positioning gets one line on it.

## Left to the builder

- Exact sidebar layout around the checkbox.
- Test: the fake is called once on, release once off; the tray row and the
  window checkbox show the same state.
