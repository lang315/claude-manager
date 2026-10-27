# Live Refresh — Design

Date: 2026-10-27
Status: approved in conversation, pending written-spec review

## Goal

The screens follow `~/.claude` as Claude Code writes to it, so the user no
longer has to press Rescan. Roadmap item: v2, "Watch `~/.claude` for changes
and refresh live instead of manual Rescan".

It builds on the debt fixes (PR #7):
- session heads are cached, so a reload rereads only the files that changed;
- the Usage scan is incremental;
- every screen keeps the user's choice across a reload;
- the Usage warnings no longer repeat.

## Decisions

| Decision | Choice | Why |
|---|---|---|
| When | Only while the app is active. In the background it only notes which screens need a reload, and reloads them once when the app becomes active again | Claude Code writes transcripts all the time. Reloading in the background costs CPU when nobody is looking |
| How | `mygo.WatchFiles` on the root: recursive, `HintsOnly`, `Debounce` 1 s, `MaxDelay` 5 s | `HintsOnly` keeps no snapshot. `~/.claude` holds about 58,800 entries (measured), close to the 65,536 snapshot limit, and 44,673 of them are in `plugins/` |
| What reloads | Only the screens that read the folder that changed | A full Rescan costs about 0.95 s of CPU (measured); a targeted reload costs a fraction of that |
| Feedback | An automatic reload is quiet: no "Scanning…" in the header, and Rescan and Trash stay enabled | A spinner every 5 s would be noise. `TrashPaths` already checks again, as it moves each path, that its session is not live and was not written in the last 10 minutes |
| Rescan | Stays, and works as before: everything reloads, with the spinner | It is the fallback when watching fails, and the user's way to force a full reread |

## Measured (the user's machine, 2026-10-27, read-only)

| Load | Time, warm |
|---|---|
| `Scan` (Overview) | 312 ms |
| `Projects` (Sessions, Cleanup), cached | 27 ms |
| `Config` | 286 ms |
| `Usage`, incremental | 69 ms |
| `History` | 22 ms |
| `Cleanup` | 15 ms |

## Watching

New file `view/live.go`. The watch is started from `main.go` once the
window exists.

- **The root:** `mygo.WatchFiles(ctx, root, FileWatchOptions{Recursive: true, HintsOnly: true, Debounce: time.Second, MaxDelay: 5 * time.Second})`.
- **`~/.claude.json`:** it lives outside the root, so a second watch follows
  it alone, without `HintsOnly`, through its directory as mygo does for a
  file. When `CLAUDE_CONFIG_DIR` is set, the file is
  `$CLAUDE_CONFIG_DIR/.claude.json`, inside the root, and the root's watch
  already covers it.
- **The read loop:** one goroutine per watch calls `Next`, maps each event's
  path to the screens it affects, and hands that set to the main thread
  through `a.Update`.
- **Ending:** `Next` returns `io.EOF` when the watch closes or stops on its
  own. In that case, `Err` gives the reason.

### Which screens a path affects

The paths are relative to the root and separated by slashes. `screensFor`
is a pure function that returns a set of screens. Its rules, in order:

| Hint path | Screens |
|---|---|
| `projects` or `projects/…` | Sessions (which reloads Cleanup too), Usage, Overview; and Config for `projects/<dir>/memory`, where the memory files live |
| `sessions` or `sessions/…` | Sessions |
| `plugins/…`, `skills/…`, `commands/…`, `agents/…` | Config, Overview |
| `.` | everything: a file at the root changed (`history.jsonl`, `settings*.json`), or the system lost events and asks to look at everything |
| any other folder (`file-history`, `session-env`, `paste-cache`, `shell-snapshots`, `debug`, …) | Sessions, Overview |

A change to `~/.claude.json` affects Config.

The screens are Overview, Sessions, History, Usage and Config. Cleanup is
not one of them: Sessions reloads it, as it does today.

## Pacing

A small type, `live`, holds the pacing state and is tested on its own. It
runs on the main thread only:

- `active bool`, starting true;
- `pending` and `inflight`, the sets of screens waiting and loading.

It works like this:

- **A hint while active:** the screens in its set that are not loading
  start a quiet reload now. Those that are loading join `pending`.
- **A hint in the background:** the set joins `pending`, and nothing starts.
- **The app becomes active** (`mygo.App.OnDidBecomeActive`): `pending`
  starts, in the same way as an active hint.
- **The app resigns** (`OnDidResignActive`): `active` becomes false.
- **A quiet reload lands:** its screen leaves `inflight`. If the screen is
  still in `pending` and the app is active, it starts again, once.

The watch's `Debounce` and `MaxDelay` already limit how often hints come. A
burst of writes, such as a transcript in a running session, gives at most
one hint every 5 s.

## Quiet reloads

`App.run` gains a quiet mode.
- A load that starts while `a.quiet` is set does not touch `busy`.
- When it lands, it tells `live` that its screen is done.

`live` sets `a.quiet` around the call to each screen's `load` and clears it
after. `load` calls `run` before it returns, so the flag is seen in time.

`usage.load` does not go through `run`. It keeps its own `scanning`/`again`
guard, and it reports "done" to `live` from its landing closure.

A manual Rescan, and the reload after a Trash, stay loud. They count in
`busy` as they do today.

### Each screen keeps the user's place

A quiet reload must not undo what the user did. Each screen gets one test
that reloads it and checks that the user's place survives:

| Screen | What must survive |
|---|---|
| Sessions | the chosen project and its open session list; ticked sessions; an open transcript |
| History | the chosen prompt and the search text (already kept since PR #5) |
| Usage | the chosen session (PR #7), the period and the project filter |
| Config | the tab and the open file in Skills, Commands and Memory; ticked backups that still exist |
| Cleanup | ticked items that still exist |

Checked on 2026-10-27, with a throwaway test per row that reloads through
Rescan. Six of the eight items already survive. Two are lost today, on a
manual Rescan too, and this PR fixes them:

- **Sessions: ticked sessions.** `sessions.set` resets `shown`, so the next
  frame calls `table.Checked.Clear()` and `Chosen.Clear()`.
  - **Fix:** when the project shown is the same one, keep the ticks and
    drop only those of sessions that are gone.
  - The ticks and choice still clear when the user picks another project.
- **Config: ticked backups.** `configPage.load` calls
  `backups.Checked.Clear()`, so that a trashed file's tick doesn't linger.
  - **Fix:** drop only the ticks of backups no longer listed.
    `TestBackupsChecksClearOnReload` must stay green.

The chosen Sessions project is keyed by `Project.Dir`. A merged project's
`Dir` moves when a new session lands in its other folder (PR #7, a deferred
minor), and that drops the choice once. Live refresh makes this more likely,
so the Sessions project table keys by `Project.Path` instead. `Path` is
unique after the merge.

## Errors

- **The root's watch fails to start, or ends with an error** (for example
  `ErrWatchLimit`): one toast, `Live refresh is off: <error>`. The app
  carries on with manual Rescan only. There is no retry loop.
- **The `~/.claude.json` watch fails:** the same toast, and the root's watch
  keeps running.
- **A missing `~/.claude.json`:** not an error, and it is not watched. A
  manual Rescan reads it once it exists.

## Testing

- **`screensFor`:** a table test of every row above, including `.` and an
  unknown folder.
- **`live`:** tests that drive `hint`, `landed`, `activate` and `resign`
  with no watcher. They cover:
  - an active hint starts its screens;
  - a hint for a loading screen waits, then runs once;
  - background hints pile up, then run once on activation;
  - an overlap never starts the same screen twice.
- **Quiet `run`:** a quiet reload leaves `busy` at 0. A Rescan during a
  quiet reload is still loud.
- **The "keeps the place" table:** one test per screen.
- **Integration:**
  - `WatchFiles` on a `fixture.New` root;
  - write a new session `.jsonl` under `projects/`;
  - within 3 s, the Sessions screen lists it.

  It never touches the real `~/.claude`.
- **The gate:** `gofmt -l .` prints nothing, `go vet ./...` exits 0, and
  `CGO_ENABLED=1 go test -race ./...` exits 0.

## Out of scope

- Following an open transcript as it grows (tail). It can come later.
- A "live" indicator in the UI.
- Linux and Windows. The app ships for macOS.
- Watching anything that is not under the root, except `~/.claude.json`.
