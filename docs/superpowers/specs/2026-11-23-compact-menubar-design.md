# Compact menu bar item

## Why

The menu bar item is text only: `alice · 49% · 71%`, 136 px. On a MacBook with a notch and many items it is the first to be pushed out, and a menu bar manager (Ice) hides it. The user wants it to take less room.

## Goal

At about 55 px, show at a glance how full each window is, plus the one number that decides a switch:

```
▆▆▆░░ 71%
▆▆▆▆░
```

- **The icon:** 16 pt, two bars filled by the share used. The top bar is the 5-hour window, the bottom bar the 7-day window.
- **The text:** the share of the fuller window, `max(5h, 7d)`, using the existing `fullest()`.
- **The tooltip:** the full line, for example `alice · 5h 49% · 7d 71%`.

**Non-goals:**
- no colour, and no alert when a limit is near;
- no new menu items;
- no change to the menu's rows.

**Anti-goal:** an icon that is unreadable in light or dark menu bars. The icon is a template image, so macOS tints it.

## Approaches (the user picked A)

- **A. Bars icon and the fuller share** (about 55 px).
  - Shows the state of both windows, and the exact number that matters.
- **B. Bars icon only** (about 24 px).
  - The smallest, but exact numbers need the menu opened.
- **C. Text without the name, `49% · 71%`** (about 75 px).
  - The least code, but the two bare numbers don't say which window is which.

## Design

1. **A new default title mode, "compact": the icon and the fuller share.**
   - Stored as `""`. Every other mode is stored by name, `both` included. Before this change, `""` meant both.
   - `config get menubar.title` shows `compact`, and accepts `compact, both, 5h, 7d, off`.
   - In the `Menu Bar` menu, `Title: Compact` comes first.
2. **The icon is shown in every mode.**
   - The text modes (`both`, `5h`, `7d`) keep today's text beside it, with or without the name.
   - `off` shows the icon alone, instead of `CM`.
3. **In compact mode the name is never in the title.**
   - `Show Account Name` and `Show Model Limits in Title` are greyed out in compact mode.
4. **When nothing has been read**, the bars are empty tracks and there is no text.
   - A refused session shows `!` as its text.
5. **Drawing:**
   - `view.TrayIcon(five, seven float64) []byte` returns a 32×32 px black-and-alpha PNG, made with Go's `image/png`.
   - Each track is 35% alpha; the fill is opaque, with its width proportional to the share and clamped to 0–100.
   - `setTray` sets the icon with `SetIcon(png, true)`. It redraws only when one of the two shares changes, rounded to a whole pixel of fill.
   - A new icon is set on Windows and Linux too, where today there is none.

## Left to the builder

- The exact bar geometry, within the 16 pt box.
- Whether the tooltip is updated in `setTray` or set with the title.

## Checks

- **Unit tests:**
  - `trayTitle` in each mode;
  - `TrayIcon`'s pixels: fill width ∝ share, only black or transparent, 0% and 100% at the edges;
  - the prefs round trip for `compact`, and for `both` stored by name.
- **Real check:** System Events reports the item's width as about 55 px. Today it reports 136.
