# History Search — Design

Date: 2026-10-27
Status: approved in conversation, pending written-spec review

## Goal

A History screen that searches every prompt typed into Claude Code
(`history.jsonl`), filtered by project and date. The user finds an old
prompt to copy it back, with its pasted text, or to open the session it
came from.

Roadmap item: v2, "Full-text search of `history.jsonl` (every prompt typed),
filtered by project and date".

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Loading | Read the whole file into memory on each Rescan, off the main thread | 3.7 MB and 15,086 lines on this machine; no index needed |
| Search | Case-insensitive substring through `DataTable`'s own search, with `DataColumn.Match` | Reuses the Sessions pattern; finding a prompt you typed needs no ranking |
| Filters | `DataTableState.Filter` + `FilterKey` for project and date, alongside the search | One table, both filters and the search combine |
| Date filter | `Segmented` presets plus Custom with From / To fields | MyGo has no date picker; presets are one click, Custom covers any range |
| Writes | None. `history.jsonl` is only read | Claude Code appends to it on every prompt and uses it for ↑ recall; a rewrite could lose lines |

## Data source (checked on the user's machine)

`<root>/history.jsonl`: one JSON object per line, appended by Claude Code.

| Field | Type | Meaning |
|---|---|---|
| `display` | string | The prompt as typed. Pastes appear as placeholders: `[Pasted text #N +M lines]` (539 seen) or `[Pasted text #N]` (3 seen); images as `[Image #N]` (13 seen) |
| `timestamp` | int | Unix milliseconds |
| `project` | string | The working directory |
| `sessionId` | string | The session the prompt was typed in |
| `pastedContents` | object | Keyed by paste id `N`: `{id, type: "text", content}` (254 seen) or `{id, type: "text", contentHash}` (312 seen) |

- A paste stored only by `contentHash` is not readable: `paste-cache/` is
  empty here, and its file naming is unverified. It counts as lost.
- 23 prompts carry a paste whose placeholder is no longer in `display`. The
  paste is shown in the detail but not inserted on Copy.
- Prompts reach 13,007 bytes (p99 293).

## Store

New file `store/history.go`.

```go
// Prompt is one line of history.jsonl: a prompt typed into Claude Code.
type Prompt struct {
	Text      string    // display: the prompt as typed, placeholders included
	Time      time.Time // zero when the line has no timestamp
	Project   string    // the working directory
	SessionID string
	Pastes    []Paste   // the pastes kept inline, by id
	Lost      int       // pastes stored only by hash
}

type Paste struct {
	ID      int
	Content string
}

// History reads history.jsonl, newest first. Lines that are not JSON are
// skipped and counted in bad. A missing file is no prompts and no error.
func (s *Store) History() (prompts []Prompt, bad int, err error)

// Expand returns the prompt with each [Pasted text #N …] placeholder
// replaced by paste N's content. Placeholders of lost pastes and of images
// stay as they are.
func (p Prompt) Expand() string
```

- Sorted by `Time`, newest first; prompts without a time sort last.
- Never writes, never trashes.

## Screen: History

A new sidebar item, between Sessions and Config, with its own icon. The
header reads `15,086 prompts in 80 projects`, plus `· N unreadable lines`
when some are.

```
[Search prompts____] [All projects ▾] [Today|7 days|30 days|All|Custom]
                                       Custom → From [YYYY-MM-DD] To [YYYY-MM-DD]
┌ Time ─────── Project ──── Prompt ───────────────┐ ┌ PROMPT ──────── 27 Oct 12:57 ┐
│ 27 Oct 12:57  me/proj     fix the login bug…     │ │ lang315/claude-manager       │
│ 27 Oct 12:40  lang315/x   [Pasted text #1 +20 l… │ │ full prompt, selectable      │
│ …                                                │ │ ▸ Pasted text #1 (20 lines)  │
└──────────────────────────────────────────────────┘ │ 1 paste no longer stored     │
  4,210 prompts                                       │ [Copy]  [Open session]       │
                                                      └──────────────────────────────┘
```

### Table

- `DataTable` with three columns:
  - Time: `fmtTime`, sortable;
  - Project: `shortPath`, with the full path as tooltip;
  - Prompt: the first line, via `FirstLine`.
- No checkbox column; the footer shows the count of rows shown.
- Newest first by default.

### Search

- Search applies to the Prompt column.
- Its `Match` covers the prompt text and every inline paste.
- Matching is case-insensitive.
- Search runs through `SearchField` + `SetSearch`, as Sessions does.

### Project filter

- A `Select` with "All projects", then each project by `shortPath`.
- Projects are ordered by their newest prompt.

### Date filter

- Dates use the local time zone.
- "Now" is `Store.Now`.

| Preset | Keeps prompts from |
|---|---|
| Today | local midnight today |
| 7 days | local midnight 6 days ago, today included |
| 30 days | local midnight 29 days ago |
| All | everything, including prompts without a time |
| Custom | From 00:00 to To 23:59:59.999, both inclusive |

Custom rules:
- An empty From or To leaves that side open.
- A field that is not `YYYY-MM-DD` shows an error under it, and the date
  filter is then ignored. The table does not empty.
- Prompts without a time match only All.

### Detail face

The detail face shows the row chosen, styled like Config's reader:

- legend `PROMPT`, the time, and the full project path;
- the full prompt, selectable, clipped as transcript text is (`clip`);
- each inline paste as a `Collapsible` labelled `Pasted text #N (M lines)`,
  in monospace;
- a note `N paste(s) no longer stored` when `Lost > 0`.

Two buttons:
- **Copy** writes `Expand()` to the clipboard (`Context.WriteClipboard`) and
  toasts `Copied`.
- **Open session** looks for `SessionID` among the projects the Sessions
  screen scanned.
  - If it is found, Open session goes to Sessions and opens its transcript.
  - If it is gone, the button is disabled, with the tooltip
    `Session no longer on disk`.

With no row chosen, the face says `Choose a prompt to see it whole.`

### Look

- The screen follows DESIGN.md (Braun Instrument):
  - graphite selection;
  - tabular figures;
  - no orange, which marks only what can be cleaned up.
- Screenshots `docs/screenshots/history-{light,dark}.png` come from the
  fixture.

## Errors

| Case | Behavior |
|---|---|
| No `history.jsonl` | Empty table, `No prompt history yet.`, no error |
| Unreadable file | Toast with the error, empty table |
| Line not JSON | Skipped, counted, shown in the header |
| No or zero `timestamp` | Time shows `—`; only All keeps it |
| Bad Custom date | Error under the field; date filter ignored |
| Session of the prompt gone | Open session disabled, with tooltip |

## Testing

TDD. Tests never read the real `~/.claude`.

The fixture gains a `history.jsonl` with these lines:
- prompts in two projects (`fixture.Proj`'s cwd and `fixture.Other`'s);
- one broken line;
- one prompt with an inline paste and its placeholder;
- one prompt with a hash-only paste;
- one prompt with `[Image #1]`;
- one prompt without `timestamp`;
- one prompt whose `sessionId` is `fixture.Old`;
- one prompt whose `sessionId` is no session.

Store tests:
- order is newest first, and prompts without a time come last;
- `bad` is counted;
- `Pastes` and `Lost` are filled;
- a missing file gives no prompts and no error;
- `Expand`:
  - replaces both placeholder shapes;
  - keeps `[Image #N]`;
  - keeps lost pastes' placeholders;
  - ignores a paste without a placeholder.

View tests (Tester):
- search finds a prompt by text inside its paste;
- the project filter keeps one project's prompts;
- the date presets are tested against a fixed `Store.Now`;
- Custom with a bad date shows the error and keeps rows;
- Copy puts `Expand()` on the clipboard;
- Open session shows the transcript of `fixture.Old`;
- Open session is disabled for a gone session;
- the empty state shows when there is no file.

## Out of scope

| Left out | Why |
|---|---|
| Editing or deleting history | Claude Code appends to the file and uses it for ↑ recall; a rewrite races it. Belongs with v3 editing (backup, diff) |
| Regex, word search, ranking | Substring finds a typed prompt; 15k lines need no index |
| Reading `paste-cache/` | Empty on this machine; the naming can't be checked against real data |
| Live refresh | Its own roadmap item, for every screen; Rescan stays |
| Bulk actions | Nothing to act on in bulk without deletion |
