# Paying Down the Debt — Design

Date: 2026-10-27
Status: approved in conversation, pending written-spec review

## Goal

Fix every item left open after v1 and after the Usage screen (PR #6), before
live refresh is built on top of them. Live refresh is the next PR, with its
own spec. It needs these fixes:
- a reload that rereads unchanged files would be too costly to run often;
- a reload that drops the user's choice, or toasts the same warning again,
  would get in the way on every change.

The scope is exactly the items listed below. Nothing else changes.

## Usage screen (deferred minors from the PR #6 review)

### U1. The "could not be read" toast repeats

**Now:** every scan that lands with `unreadable > 0` sets `a.notice`. So a
file that stays unreadable toasts on every Rescan. The toast can also replace
the Trash confirmation ("Moved N items to the Trash"), because `App.trash`
calls `Rescan` right after setting it.

**Fix:**
- `usage` remembers the count of the last scan that landed, starting at 0.
- It toasts only when the new count differs from that one and is above 0.
- It never replaces a notice that is still pending (`a.notice != ""`).

### U2. A Rescan drops the chosen session

**Now:** `refresh` sets `sessAt = -1` whenever its key changes. The key
includes `gen`, so every scan that lands clears the choice and disables
Open Transcript.

**Fix:**
- The choice is kept by session id.
- When only `gen` changed, `refresh` finds that id among the new rows and
  sets `sessAt` to its row, or to -1 if the session is gone.
- A change of period or project still clears the choice, as before.

### U3. The file size and the read offset can disagree

**Now:** `refresh` stats the file, reads to its current end, then stores the
stat's size. So the stored size can be smaller than `off`.
- A file later cut back to a size between the two counts as grown, and the
  scan seeks past its end.
- A file rewritten in place that ends up larger is read on from a stale
  offset. That fragment can count as a bad line.

**Fix:**
- Read at most `size − off` bytes, through `io.LimitReader`, so that `off`
  never passes the stat's size.
- A file whose size is below `off`, or whose inode changed, is read whole
  again.
- The test also covers a file cut back to a size between the stored size
  and `off`: its calls must not stay.

### U4. A row with some unpriced calls shows only the priced part

**Fix:** `costText` adds `+` after the amount when a row has both priced
and unpriced calls, as in `$12.30+`. The cost cell's tooltip says
`N calls unpriced`. A row whose calls are all unpriced still shows `—`.

### U5. An advisor call inherits fast mode but not US-only inference

**Fix:** an advisor iteration that lacks `speed` or `inference_geo` takes
the parent call's value, for both fields alike. One test pins each field.

### U6. A scan error clears the figures

**Fix:** when `Store.Usage` returns an error, the screen keeps the buckets
of the last scan and shows the error as a toast. A first scan that fails
still shows "No usage yet." and the toast.

### U7. An unreadable folder under `projects/` is skipped silently

**Fix:** a folder that `WalkDir` can't read counts as one in `unreadable`,
so the toast reports it. The root itself still returns the error, and a
missing `projects/` is still no usage and no error.

### U8. `tokenCount` rounds up past its unit

**Now:** 999,950 shows as `1000.0K`, and the same happens at the M and B
boundaries.

**Fix:** pick the unit from the rounded value, so 999,950 shows `1.0M` and
999,950,000 shows `1.0B`.

### U9. A period change is checked only through the header

**Fix:** add a test that changes the period and checks the Models tab's
rows and the number of chart bars.

## v1 known limitations (`docs/roadmap.md`)

### V1. Two project folders with the same cwd get the same label

**Now:** Claude Code can encode one cwd into two folder names, for example
across versions. Sessions then lists two projects with the same path.

**Fix:**
- `Projects()` merges projects that share a `Path`: their sessions join one
  list, sorted newest first, and their sizes add up.
- The merged project keeps the `Dir` of the folder with the newest session,
  so the Sessions table's row key stays stable.
- The sessions are not changed, and each keeps its own `File` and `Dir`.

### V2. Untitled sessions are reread, up to 8 MB, on every Rescan

**Fix:** a `store.SessionScan` cache, kept in memory like `UsageScan`.
- `Projects(scan *SessionScan)` reuses a session's head metadata when its
  `.jsonl` has the same size and mtime as last time: title, first prompt,
  cwd, branch, version and start.
- Two things are still worked out on every call, because they change
  without the `.jsonl` changing:
  - the side folder's size and subagent count (`dirSize`, `listFiles`);
  - whether the session is live (`LiveIDs`, `recent`).
- The cache is guarded by a mutex: the Sessions load and the Cleanup load
  can run at the same time.
- Files that are gone drop out of the cache.
- `App` owns one `SessionScan`, and both loads pass it. Tests pass a new
  one.

### V3. Credential masking is too broad

**Now:** a query parameter, `--flag=` or `Name: value` string whose name
merely contains `key`, `token`, `secret`, `pass`, `auth`, `cookie`, `sig` or
`credential` is masked. So `author: Jane`, `hotkey: ctrl+k` and
`--design=x` are masked too.

**Fix:** match names by their segments. A name is split on `-`, `_` and
`.`, and at camelCase boundaries, then lowercased.

It is masked when one of its segments:
- equals one of: `key`, `token`, `secret`, `secrets`, `pass`, `passwd`,
  `password`, `passphrase`, `auth`, `authorization`, `cookie`, `sig`,
  `signature`, `credential`, `credentials`, `bearer`; or
- ends in `key`, `token`, `secret`, `password` or `cookie`, except `hotkey`.

| Shown | Masked |
|---|---|
| `author: Jane` | `x-api-key: abc` |
| `hotkey: ctrl+k` | `apiKey=abc` |
| `--design=x` | `?privatekey=abc` |
| `?bypass=1` | `--auth-token=abc` |
| `monkey: 1` | `Authorization: Bearer x` (already caught by the bearer rule) |
| `?page=2` | `?sig=abc` |
| | `?access_token=abc` |
| | `--password=x` |
| | `Cookie: a=b` |

Only the name rule inside `secretValue` changes. `secretKey`, which masks
JSON keys, and the shape rules stay as they are: `bearer`, `sk-`, `gh?_`,
`AKIA`, `user:pass@` and the others.

**Risk:** a secret whose name is written as one run-together word in no
listed pattern, for example `sessionid=…`, now shows where the old
substring rule would have caught some such names. This is accepted. The app
shows only the user's own files, on their own screen.

### V4. Each click on the days stepper rescans every session

**Fix:** none beyond V2. The Cleanup load calls `Projects(scan)`, so a click
rereads only the sessions whose files changed. The existing `gen` guard
still drops results that land out of order. The test counts head reads
across two loads.

## `.impeccable`

- `git rm --cached .impeccable/design.json .impeccable/surfaces/view-app-go.md`.
  `/.impeccable/` is already ignored, so they stay on disk and leave the
  repo.
- Run `/impeccable doctor` to bring the local sidecar up to DESIGN.md's
  `hue-*` tokens. The sidecar is not committed.

## Roadmap

Tick the four known limitations in `docs/roadmap.md` (strike them, or move
them under a "Fixed" note), with the PR that fixed them.

## Testing

Every item gets a test that fails before the fix. That covers U1–U9, V1–V4
and the masking table. The full gate runs before every commit: `gofmt -l .`
prints nothing, `go vet ./...` exits 0, and
`CGO_ENABLED=1 go test -race ./...` exits 0. Tests never read the real
`~/.claude`.

## Out of scope

- Live refresh, which is the next PR.
- Changing `secretKey`'s JSON key list.
- Caching `dirSize` of side folders.
