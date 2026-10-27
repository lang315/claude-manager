# Claude Manager — Design

Date: 2026-10-26
Status: approved in conversation, pending written-spec review

## Goal

A macOS desktop app that reads `~/.claude` and shows everything in it visually,
so the user can see where the space goes (about 6 GB today) and clean it up
safely.

v1 scope: **view + clean up**. The app never edits Claude Code configuration.
Everything deferred is listed in [`docs/roadmap.md`](../../roadmap.md).

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Framework | MyGo native UI (package `ui`), no webview | One language, virtualized tables and Markdown built in, no Bun at dev or run time |
| Location | `claude-manager` repo, module `github.com/lang315/claude-manager` | The app lives apart from the framework |
| MyGo dependency | `replace github.com/egoist/mygo => ../mygo` | Local clone; no changes to the mygo repo |
| Deletion | `mygo.Shell.TrashItem` only | Moves to the macOS Trash; never deletes for good; fails instead of deleting |
| Data root | `$CLAUDE_CONFIG_DIR`, else `~/.claude` | Matches Claude Code |

## Data sources (checked on the user's machine)

| What | Where | Notes |
|---|---|---|
| Sessions | `projects/<encoded-dir>/<uuid>.jsonl` | One JSON record per line. The directory name is a lossy encoding of the path; the real path is the `cwd` field of records |
| Session side data | `projects/<encoded-dir>/<uuid>/` | `subagents/agent-*.jsonl` + `*.meta.json`, `tool-results/`. Large: 2.09 GB vs 2.39 GB of `.jsonl` on this machine |
| Orphan side data | `projects/<encoded-dir>/<uuid>/` with no `<uuid>.jsonl` | 782 such dirs (9.5 MB), almost all only `tool-results/` |
| Not sessions | `projects/<encoded-dir>/memory/`, `*-memory/`, `*.wakatime`, `sessions-index.json` | Never treated as session data; only UUID-named entries are |
| Session title | record `{"type":"ai-title","aiTitle":...}` | Fallback: first `user` record's text (no `last-prompt` record is read) |
| Messages | records `type` = `user` / `assistant` | `message.content` blocks: `text`, `thinking`, `tool_use`, `tool_result` |
| Ignored records | `attachment`, `file-history-snapshot`, `file-history-delta`, `queue-operation`, `mode`, `permission-mode`, `atis-latch`, `system` | Internal; not shown in the transcript |
| Session index | `projects/<dir>/sessions-index.json` | Present in only some projects; used only as a hint, never required |
| Live sessions | `sessions/<pid>.json` | Has `pid` and `sessionId`; a session is live when that PID is alive (`kill(pid, 0)`) |
| File history | `file-history/<sessionId>/` | |
| Session env | `session-env/<sessionId>/` | |
| Prompt history | `history.jsonl` | Viewed only in v2 (search) |
| Settings | `settings.json`, `settings.json.bak*` | Read-only in v1; backups can be trashed |
| Other | `skills/`, `plugins/`, `commands/`, `hooks/`, `projects/*/memory/`, `CLAUDE.md`, `transcripts/`, `paste-cache/`, `shell-snapshots/`, `debug/` | |

## Architecture

```
main.go            app, window, sidebar navigation, rescan
store/             reads ~/.claude; never imports ui; tested on its own
  scan.go          size and file count of each top-level entry
  sessions.go      projects → sessions with light metadata; live session detection
  record.go        one JSONL record → messages (shared by sessions and transcript)
  transcript.go    one session → messages, streamed
  config.go        settings.json, hooks, MCP servers, skills, plugins, commands, memory, CLAUDE.md, backups
  cleanup.go       cleanup candidates per group + trashing
  store.go         Store (injected Now, Alive, Trash), PIDAlive
view/              one file per screen: overview, sessions, transcript, config, cleanup
internal/fixture/  synthetic ~/.claude written into a temp dir for tests
```

### Store rules

- Listing sessions parses every line of the first 64 KB of each `.jsonl` plus
  `stat`: `cwd`, `gitBranch`, version, start time and, if they lie there, title
  and first prompt. Past the head it reads on, parsing only a line that holds an
  `ai-title` (while there is no title) or a user record (while there is no first
  prompt), and stops once both are found, at the end of the file, or after
  8 MB. A title past 8 MB reads as untitled. The table shows no message count:
  the transcript header shows the real one.
- `parseRecord` tolerates a record with a field of an unexpected type (a
  `*json.UnmarshalTypeError` is no parse failure); only a syntax error counts
  as a bad line.
- `live()` reads `sessions/<pid>.json`: a session is live when its PID is alive.
  A `startedAt` that is no integer makes the start unknown; the session still
  counts as live. The project's short label is the last two components of its
  `cwd` (the full path in a tooltip).
- A shell snapshot is written once at session start and carries no session id:
  `shell-snapshots/` offers only snapshots older than the earliest live
  session's `startedAt`, and none while a live session has no `startedAt`.
- Opening a transcript reads the file line by line (`bufio.Reader`, lines may
  be many MB) and appends messages as they parse, so large sessions show
  progressively.
- A line that fails to parse is skipped and counted; the session shows a badge
  with the count. It never stops the read.
- An unreadable file (permission, removed mid-scan) marks its row as an error;
  the rest of the scan continues.
- `TrashPaths` skips, without error and without counting, a path already
  handled in the call, one that is gone, one of a live session, one modified in
  the last 10 minutes, and one of a session whose `.jsonl` was modified in the
  last 10 minutes. The lists the user confirmed can be hours old, so it checks
  again at trash time.
- `Cleanup` builds its known-session ids from the projects it is given plus the
  `<uuid>.jsonl` names it reads from each project directory now, so an id with a
  `.jsonl` on disk is never an orphan whatever list the caller passes; the
  Cleanup screen also reloads the projects fresh. With no projects, nothing is
  an orphan.
- Credentials never show in plain text on the Config screen. A value is masked
  when its key looks secret (`token`, `secret`, `password`, `api_key`,
  `auth`, `headers`, …), when it sits under an MCP server's `env` or `headers`,
  or when the string holds a known shape: `Bearer`/`Basic` credentials,
  `sk-…`, `gh?_…`, `github_pat_…`, `xox?-…`, `AKIA…`, `user:pass@` in a URL, a
  query parameter or `--flag=value` named like a secret. On the MCP tab an
  `args` element after a secret-named flag (`"--api-key", "abc123"`) is masked.
  A token with no known shape, outside those places, still shows.
- All scanning and reading runs on goroutines; results are posted to the main
  thread, which redraws. The UI thread never waits on disk.

### Deleting a session

The paths of a session `id` are gathered first:

1. `projects/<dir>/<id>.jsonl`
2. `projects/<dir>/<id>/`
3. `file-history/<id>/`
4. `session-env/<id>/`

Only paths that exist are trashed. A live session is never trashed. A session
counts as live if its id is in `sessions/<pid>.json` with a live PID, or its
`.jsonl` was modified less than 10 minutes ago. On the first `TrashItem` error
the operation stops, a toast names the failing path, and the app rescans so
the UI matches the disk.

`store` takes the trash function as a parameter (`func(path string) error`).
The app passes `mygo.Shell.TrashItem`; tests pass a fake that records paths.

## Screens

The window has a sidebar with four entries (Overview, Sessions, Config,
Cleanup; the transcript opens inside Sessions) and a content area. The toolbar
has **Rescan**. Choices made while planning are listed under "Deviations from
the spec" in `docs/superpowers/plans/2026-10-26-claude-manager.md`.

### 1. Overview

- Table: top-level entry of `~/.claude` | size | file count | cleanable.
  Sorted by size.
- Horizontal bar chart of the 10 largest (`ui/chart`).
- Clicking a row opens the matching screen when there is one.

### 2. Sessions

- Split view. Left: projects (real path from `cwd`, session count, total
  size). Right: data table of the project's sessions: title or first prompt,
  last modified, estimated messages, size, subagent count.
- Search field filters by title and first prompt.
- Multi-select → **Move to Trash** → confirmation dialog showing the item
  count and total size. Live sessions are dimmed and cannot be selected for
  trashing.
- Double-click opens the transcript.

### 3. Transcript (inside Sessions)

- Header: `cwd`, `gitBranch`, Claude Code `version`, start and end time.
- Virtualized message list. User and assistant `text` render with
  `ui/markdown`. `thinking`, `tool_use` and `tool_result` are collapsed by
  default and expand on click.
- Back returns to the session table with its selection kept.

### 4. Config (read-only)

Tabs:

- **Settings**: `settings.json` as a tree.
- **Hooks**: hooks from `settings.json`, by event.
- **MCP servers**: `mcpServers` of `~/.claude.json` (it sits next to
  `~/.claude`, not inside it; on this machine it holds all 7 user servers)
  and of `settings.json` when present. Read-only; the app never writes
  `~/.claude.json`.
- **Skills**: `skills/*` with name and description from `SKILL.md` front matter.
- **Plugins**: `plugins/installed_plugins.json` and marketplaces.
- **Commands**: `commands/*`.
- **Memory**: `CLAUDE.md` and `projects/*/memory/*.md`, rendered as Markdown.
- **Backups**: `settings.json.bak*` with date and size → Move to Trash.

### 5. Cleanup

Groups with a checkbox each, and the item count and total size of each:

- `session-env/`
- `paste-cache/`
- `shell-snapshots/`
- `debug/`
- `file-history/` entries whose session no longer exists
- `session-env/` is listed whole above; most of it (2201 of 2363 entries) has
  no session left
- orphan session side data: UUID-named dirs under `projects/<dir>/` with no
  matching `<uuid>.jsonl` (the name must match the UUID pattern, so memory
  dirs such as `mygo-memory/` are never candidates)
- `transcripts/` older than N days
- sessions older than N days (same gathering rules as deleting a session)

N defaults to 30 and can be changed. **Move to Trash** asks for confirmation
with the grand total, then trashes; live sessions and anything modified in the
last 10 minutes are excluded. A toast reports what was moved and any failure.

## Testing

- `store/` unit tests against `internal/fixture` (a synthetic `~/.claude` written into a temp dir): sessions cut from the real
  format (with `ai-title`, subagents, a broken line, a large-line case), a
  live session (fake `sessions/<pid>.json` pointing at the test's own PID),
  a stale `sessions/<pid>.json` whose PID is dead, an orphan UUID side dir,
  a `*-memory/` dir and a `.wakatime` file that must never be candidates,
  backups, cleanup groups. They check listing, metadata, transcript parsing,
  the paths gathered for a session, live exclusion and group sizes.
- Trashing is tested with the fake trash function only. No test touches the
  real `~/.claude`.
- `view/` tests with `ui.Tester`: select a session → Move to Trash → confirm →
  the fake trash received exactly the session's paths; live rows cannot be
  trashed.
- Manual check: run the app on the real `~/.claude` and take screenshots of
  each screen.

## After v1 works

UI/UX polish with the `impeccable` skill, as a separate step after the
functional build passes its tests (tracked in the roadmap).

## Out of scope for v1

See [`docs/roadmap.md`](../../roadmap.md).
