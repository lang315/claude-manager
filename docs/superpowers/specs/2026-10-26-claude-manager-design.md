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
| Session side data | `projects/<encoded-dir>/<uuid>/` | `subagents/agent-*.jsonl` + `*.meta.json`, tool results |
| Session title | record `{"type":"ai-title","aiTitle":...}` | Fallback: first `user` record's text, then `last-prompt` record |
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
  sessions.go      projects → sessions with light metadata
  transcript.go    one session → messages, streamed
  config.go        settings.json, hooks, MCP servers, skills, plugins, commands, memory, CLAUDE.md, backups
  cleanup.go       cleanup candidates per group + trashing
  live.go          live session detection
view/              one file per screen: overview, sessions, transcript, config, cleanup
testdata/claude/   fake ~/.claude for tests
```

### Store rules

- Listing sessions reads only the first 64 KB of each `.jsonl` plus `stat`.
  Title, first prompt, `cwd`, `gitBranch` and start time come from that head.
  Message count shown in the table is an estimate from file size until the
  transcript is opened.
- Opening a transcript reads the file line by line (`bufio.Reader`, lines may
  be many MB) and appends messages as they parse, so large sessions show
  progressively.
- A line that fails to parse is skipped and counted; the session shows a badge
  with the count. It never stops the read.
- An unreadable file (permission, removed mid-scan) marks its row as an error;
  the rest of the scan continues.
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

The window has a sidebar with five entries and a content area. The toolbar has
**Rescan**.

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
- `transcripts/` older than N days
- sessions older than N days (same gathering rules as deleting a session)

N defaults to 30 and can be changed. **Move to Trash** asks for confirmation
with the grand total, then trashes; live sessions and anything modified in the
last 10 minutes are excluded. A toast reports what was moved and any failure.

## Testing

- `store/` unit tests against `testdata/claude/`: sessions cut from the real
  format (with `ai-title`, subagents, a broken line, a large-line case), a
  live session (fake `sessions/<pid>.json` pointing at the test's own PID),
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
