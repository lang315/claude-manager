# Usage Statistics — Design

Date: 2026-10-27
Status: approved in conversation, pending written-spec review

## Goal

A Usage screen that shows the tokens Claude Code used, and an estimate of
their API cost, broken down by session, project, day and model. It answers
"where do my tokens go, and when".

Roadmap item: v2, "Token and cost statistics per session, project and day
(from `message.usage`)".

Reference: [codeburn](https://github.com/getagentseal/codeburn) (read, not
run), a Node token tracker. Its Claude parser is `src/parser.ts`. What this
design takes from it, and what it leaves out, is noted inline.

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Scope | Tokens and an estimated cost | The cost answers "how much"; tokens alone don't |
| Prices | A price table built into the app | No network calls. The table goes stale, so it carries its date |
| Cost label | "est. API cost" | A Claude subscription doesn't bill per token; this is what the same use would cost on the API |
| Cache | In memory, per app run | The app writes nothing to disk. A full scan takes about 6 s (measured below) |
| Scan | Filter lines by bytes before decoding JSON | Only about 15% of the 1.37M lines hold usage |
| Granularity | Deduped calls kept per file (about 100k); buckets per (session, local day, model) derived after each scan | Dedupe spans a session's files, so buckets can't be kept per file. Rebuilding the buckets from 100k calls takes milliseconds |
| Cost per call | Each call is priced as it is scanned | Haiku 5.5's price depends on that call's prompt length, and `inference_geo` is per call |

## Data (checked on the user's machine)

- **Transcripts:**
  - main sessions: `projects/<dir>/<uuid>.jsonl`, 159 files, 2.47 GB;
  - subagents: `projects/<dir>/<uuid>/subagents/**/*.jsonl`, 2,167 files, 2.08 GB.
- **A call** is a `type:"assistant"` line with `message.usage` and
  `message.model`.
- **Usage fields** on every call: `input_tokens`, `output_tokens`,
  `cache_read_input_tokens`, `cache_creation_input_tokens`,
  `cache_creation.ephemeral_5m_input_tokens`,
  `cache_creation.ephemeral_1h_input_tokens`, `service_tier`, `inference_geo`.
- **Usage fields on most calls:** `speed`,
  `server_tool_use.web_search_requests`, `iterations`.
- **Duplicates** (measured over all 2,327 files):
  - Claude Code writes one message as several lines, one per content block, so a `(message.id, requestId)` pair repeats.
  - In main transcripts, the copies of a pair carry identical usage.
  - In subagent transcripts, 29,310 pairs first carry a **streaming partial**: `output_tokens` of 2–16, no `iterations`, no `server_tool_use`. A later line carries the final usage. The later copy was never smaller.
  - 45 pairs span two files. All are subagent ↔ subagent files of the same parent session: one subagent's calls were copied into three others, apparently on resume. 11 of these 45 pair a partial with a final copy.
  - No pair spans two main transcripts, or two sessions.
  - 45 calls in a sample of 120 main transcripts lack the pair.
- **Scan time** (Go spike, one goroutine, byte filter then `encoding/json`):
  - 2,327 files, 4.55 GB, 1.37M lines; 206,860 lines pass the filter; 100,600 calls after dedupe;
  - 6.6 s on the first run (OS cache partly warm), 5.5 s warm; 822 MB/s.
  - A fully cold run was not measured: it needs `sudo purge`.
- **Models seen:** `claude-opus-5-5`, `claude-sonnet-5-5`,
  `claude-opus-4-8`, `claude-opus-5`, `claude-haiku-4-5-20251001`,
  `claude-sonnet-5`, `claude-haiku-5-5`, `claude-fable-5-1`, `<synthetic>`
  (always zero usage) and `claude-opus-4-6`.
- **`cwd`:** every call has it.

## Store

New file `store/usage.go`.

```go
// Usage is one bucket: a session's calls to one model on one local day.
type Usage struct {
	SessionID string    // the parent session; a subagent's calls fold into it
	Project   string    // the session's cwd: the first cwd its calls carry
	Day       time.Time // local midnight
	Model     string    // as recorded, date suffix and all
	Calls     int
	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h int64
	WebSearches int64
	Cost        float64 // USD, estimated; calls of an unpriced model add nothing
	Unpriced    int     // calls whose model has no price
}

// UsageScan keeps what earlier scans read, per file, so a Rescan reads only
// files that are new, grew or changed. Not safe for concurrent use: one
// scan at a time, as App.run already guarantees per screen.
type UsageScan struct { /* per-file state: see below */ }

// Usage scans the transcripts and returns every bucket, with the count of
// lines that weren't JSON and of files that couldn't be read. progress, when
// set, is called after each file with how many are done of how many.
func (s *Store) Usage(scan *UsageScan, progress func(done, total int)) (buckets []Usage, bad, unreadable int, err error)
```

### Reading a file

1. Read line by line with `bufio.Reader.ReadBytes`, as `History` does, so a
   huge line has no size limit.
2. Skip a line unless it contains both `"usage"` and `"assistant"` as bytes.
3. Decode the lines that pass into a small struct:
   - `type`, `timestamp`, `cwd`, `requestId`;
   - `message.id`, `message.model`;
   - `message.usage`: the token fields, the `cache_creation` split, `speed`,
     `inference_geo`, `server_tool_use.web_search_requests` and `iterations`.
4. Count a line that is not JSON in `bad`.
5. Skip a call whose model is `<synthetic>`. Count negative numbers as 0.
6. **Duplicates.** Key a call by `message.id + "/" + requestId`. A call
   without the key is counted on every line.
   - **Usage:** of all copies of a key, the one with the largest
     `output_tokens` wins, and the later one on a tie. "The last line wins"
     would not do: across files, which copy comes last depends on file
     order. Codeburn keeps the last line, and notes that keeping the first
     undercounts output.
   - **Timestamp:** the first copy seen sets the call's timestamp.
   - **Scope:** one parent session, across its main transcript and all of
     its subagent files. Measured: no pair spans two sessions.
7. **Advisor tokens.** `usage.iterations` entries of type `advisor_message`
   are added as calls of their own, under the advisor's model. This follows
   codeburn: advisor tokens are not in the top-level totals.
8. **Cache writes.** When `cache_creation` has the 5m/1h split, use it. Without
   the split, count all of `cache_creation_input_tokens` as 5m writes.
9. **Cost.** Price each call with `price(model, call)` (below), then add it to
   its bucket.

### Attribution

- **Session:**
  - for `projects/<dir>/<uuid>.jsonl`, the session is `<uuid>`;
  - for a file under `projects/<dir>/<uuid>/subagents/`, at any depth, the
    session is the parent `<uuid>`.
- **Project:** the first `cwd` found in the session's main file, else in any
  of its subagent files.
  - A session that changes directory still counts whole in one project.
  - codeburn splits per call and folds worktrees into their repo; this
    design does neither.
- **Day:** the local midnight of the call's timestamp, in
  `Store.Now().Location()` (`time.Local` in the app, the fixture clock's
  zone in tests). A call that crosses midnight counts on the day of its
  first line.

### Incremental scan

`UsageScan` keeps, per file path:
- its size, its mtime and its inode;
- the byte offset after its last complete line;
- its calls, deduped within the file by the rule above. Each call keeps its
  key, timestamp, model, the token counts, `speed`, `inference_geo`, its web
  searches and its cost.

After each scan, the calls of each session's files are merged by key with
the same rule, then summed into buckets. That takes milliseconds for 100k
calls.

On a Rescan, for each file:

| Case | What happens |
|---|---|
| Size and mtime unchanged | Its calls are reused, unread |
| Same inode and the file grew | Read on from the saved offset, merging into its calls |
| Anything else (smaller, other inode) | The file is read again whole |
| A file gone since the last scan | Its calls are dropped |

- **A last line still being written:** it has no `\n`, so it is not read yet,
  and the saved offset stops before it.
- **The project of a session** is worked out again after each scan, from its
  files' first `cwd`.

### Prices

New file `store/prices.go`: a table of USD per million tokens, from
<https://platform.claude.com/docs/en/about-claude/pricing>, checked
2026-10-27. The constant `PricesAsOf = "2026-10-27"` carries the date.

| Model | Input | 5m write | 1h write | Cache read | Output |
|---|---|---|---|---|---|
| `claude-fable-5-1` | 10 | 12.50 | 20 | 0.25 | 50 |
| `claude-fable-5` | 10 | 12.50 | 20 | 1 | 50 |
| `claude-opus-5-5` | 4 | 5 | 8 | 0.20 | 20 |
| `claude-opus-5`, `claude-opus-4-8`, `claude-opus-4-7`, `claude-opus-4-6`, `claude-opus-4-5` | 5 | 6.25 | 10 | 0.50 | 25 |
| `claude-sonnet-5-5` | 2 | 2.50 | 4 | 0.10 | 10 |
| `claude-sonnet-5` | 2 | 2.50 | 4 | 0.20 | 10 |
| `claude-sonnet-4-6`, `claude-sonnet-4-5` | 3 | 3.75 | 6 | 0.30 | 15 |
| `claude-haiku-5-5`, prompt ≤ 100,000 tokens | 0.10 | 0.125 | 0.20 | 0.01 | 0.50 |
| `claude-haiku-5-5`, prompt > 100,000 tokens | 0.50 | 0.625 | 1 | 0.05 | 2.50 |
| `claude-haiku-4-5` | 1 | 1.25 | 2 | 0.10 | 5 |

Rules for `price(model, call)`:
- **Model name:** a `-YYYYMMDD` suffix is dropped before the lookup, so
  `claude-haiku-4-5-20251001` matches `claude-haiku-4-5`.
- **Haiku 5.5 tier:** the prompt length is input + cache read + cache write
  of that one call.
- **Fast mode:** with `speed: "fast"`, every rate doubles. This applies on
  `claude-opus-5-5`, `claude-opus-5` and `claude-opus-4-8`; their fast
  prices are 2× standard.
- **US-only inference:** with `inference_geo: "us"`, every rate is multiplied
  by 1.1. This applies to Claude 4.6 and later, which covers every model in
  the table except the 4.5 ones.
- **Web search:** $10 per 1,000 searches.
- **Unknown model:** a model not in the table has no price. Its tokens still
  count, and the call counts in `Unpriced`.

## Screen: Usage

A new sidebar item after History, with its own icon. The header reads the
figures for the period chosen, for example
`$1,234.56 est. API cost · 2.1B tokens · 91% from cache`, then:
- `· N calls unpriced` when some are;
- `· N unreadable lines` when some are.

"From cache" is cache read ÷ (input + cache read + cache writes).

```
[Today|7 days|30 days|All]                 Project: lang315/claude-manager ✕
┌ COST BY DAY ─────────────────────────────────── $1,234.56 over 30 days ┐
│ ▂▃▅▂▁▇█▄▃▂ …   one ink bar a day, ticks at round dollars                │
└─────────────────────────────────────────────────────────────────────────┘
┌ [Projects | Sessions | Models] ─────────────────────────────────────────┐
│ Project              Sessions    Tokens       Cost   ▬▬▬▬▬▬ share       │
└─────────────────────────────────────────────────────────────────────────┘
```

### Period

- A `Segmented` with Today · 7 days · 30 days · All. 30 days is the default.
- The days count the same way as History's: whole local days, today
  included. History's `rangeOf` is reused.

### Project filter

- A click on a project row filters the whole screen to that project:
  - the header;
  - the chart;
  - the Sessions and Models tabs.
- A chip `Project: <shortPath> ✕` shows the filter; the ✕ clears it.

### Cost by day

- A face holding a `Painter` chart:
  - one ink bar per day of the period;
  - a gap for a day without calls;
  - ticks at round dollar amounts, labelled like the Overview's dial.
- Its caption is `COST BY DAY`, with the period's total at the right.
- With All, if the data spans more than 90 days, there is one bar per week
  (weeks start on Monday) and the caption reads `COST BY WEEK`.

### Tabs

The breakdown is one face holding `Tabs`. Rows are sorted by cost, highest
first; unpriced rows sort by tokens after the priced ones.
- **Projects:**
  - columns Project (`shortPath`, full path as tooltip), Sessions, Tokens,
    Cost, and an ink scale bar of the row's share of the period's cost;
  - a click on a row filters the screen to that project.
- **Sessions:**
  - columns Session (title from the Sessions scan, else the id), Project,
    Last day, Tokens, Cost;
  - a session with subagents counts them in its row;
  - an **Open Transcript** button opens the session chosen, through the
    Sessions screen. It is disabled when the session is not on disk.
- **Models:**
  - columns Model, Calls, Input, Output, Cache write, Cache read, Cost;
  - the model name as recorded, with ` · fast` when fast calls exist.

### Figures and look

- Tokens are short: `820.4M`, `2.1B`, `12.3K`, `950`.
- Cost is `$1,234.56`. A cost under $0.01 shows `<$0.01`. An unpriced row
  shows `—`.
- All figures use tabular numerals (`FontFeatures("tnum")`).
- The look follows DESIGN.md (Braun Instrument):
  - bars and scales in ink;
  - selection in graphite;
  - no orange, since orange marks only what can be cleaned up.

### While scanning

- The first scan of an app run shows a spinner. The header shows
  `Reading 812 of 2,326 transcripts`.
- The tables show once the scan lands; the screen does not stream partial
  sums.
- Later Rescans keep the last figures in place until the new ones land.

### Elsewhere

- The Overview's `projects` row stays as it is.
- Nothing else changes.

## Errors

| Case | Behavior |
|---|---|
| Line not JSON | Skipped, counted, shown in the header |
| File unreadable | Skipped; counted; one toast `N transcripts could not be read` |
| Last line half-written | Not read yet; the next Rescan reads it |
| File rewritten or shrunk | Read again whole |
| File gone (trashed) | Its calls drop at the next scan |
| Model not in the price table | Tokens counted; cost `—`; header `N calls unpriced` |
| `<synthetic>`, negative or missing counts | `<synthetic>` skipped; bad counts are 0 |
| Call without `message.id`/`requestId` | Counted on each line (no dedupe) |
| No transcripts | `No usage yet.` |
| Period with no calls | `No usage in this period.` |

## Testing

TDD. Tests never read the real `~/.claude`.

### Fixture

The fixture gains assistant lines with usage:
- In `Old`'s transcript:
  - one call written as 3 lines with the same key, whose last line has the
    higher output;
  - a call with a 5m/1h cache split;
  - a call with `speed: "fast"` and `inference_geo: "us"`;
  - a `<synthetic>` call;
  - a call with advisor iterations.
- In `Old`'s subagent `agent-a1.jsonl`:
  - one `claude-haiku-4-5-20251001` call;
  - one call whose first line is a streaming partial (`output_tokens` 4)
    and whose later line is final (`output_tokens` 300).
- In a second subagent of `Old`, `agent-a2.jsonl`: a copy of that partial
  line only. It is the resumed-subagent case, which must count once at
  300.
- In `Recent`'s transcript:
  - one `claude-haiku-5-5` call over 100,000 prompt tokens;
  - one call of an unknown model;
  - one call at 23:59:59.900 whose next line, of the same key, is at
    00:00:00.100 the next day.

The view tests check figures computed by hand from these lines.

### Store tests

- **Dedupe:**
  - 3 lines count once, with the first line's timestamp and the largest
    output's usage;
  - a partial in one subagent file and the final copy in another count once
    at the final usage, whichever file is read first.
- **Attribution:**
  - the subagent call folds into `Old`;
  - the project is `Old`'s cwd;
  - the day is local midnight.
- **Cache split:** the 5m/1h split is used; a call without the split counts
  all writes as 5m.
- **Skips and extras:**
  - `<synthetic>` is skipped;
  - advisor iterations add their own calls;
  - `bad` counts the broken line.
- **Prices:**
  - each rate of the table, applied per token type;
  - the date suffix is stripped from the model name;
  - the Haiku 5.5 tier is picked per call;
  - fast doubles every rate, and US-only inference multiplies by 1.1;
  - web search is priced;
  - an unknown model counts as `Unpriced`.
- **Incremental scan** (`UsageScan`):
  - **unchanged files:** a second scan opens no file. The test counts opens
    through an `open` hook the scan uses.
  - **appended lines:** they are read from the offset, and the totals grow
    by exactly the new calls.
  - **a rewritten file** (shorter) is read whole.
  - **a deleted file** drops its buckets.
  - **a half-written last line** is read only after its `\n` arrives.

### View tests (Tester)

- **Header:** the totals for 30 days and for All.
- **Period:** a change updates the chart and tables.
- **Project filter:** a click filters the screen; the ✕ clears it.
- **Sessions tab:** Open Transcript opens `Old`'s transcript.
- **Models tab:** the rows, with `· fast`.
- **Empty states and progress:**
  - no transcripts shows `No usage yet.`;
  - a period with no calls shows `No usage in this period.`;
  - the progress text shows while the scan is held.

### Screenshots

`usage-{light,dark}.png`, drawn from the fixture.

## How the data was checked

These checks ran on the user's machine on 2026-10-27. They only read
`~/.claude`; nothing in it was written.

- **Field census.** A Python sample of 359 transcripts (983k lines)
  counted record types, usage fields and models.
- **Go spike.** A throwaway program, kept out of the repo, read all 2,327
  transcripts the way this design does: a byte filter on `"usage"` and
  `"assistant"`, then `encoding/json` into a small struct. It measured:
  - the scan time;
  - every `(message.id, requestId)` pair seen in two files, and whether
    the two files share a parent session;
  - every pair whose copies differ, within a file or across two, with the
    fields that differ.
- **Results:**
  - **Speed.** 5.5–6.6 s, single goroutine.
  - **Cross-file pairs.** 45, all subagent files of one session.
  - **Partials.** 29,310 within single subagent files, and 11 of the
    cross-file pairs. In every case the later copy had the larger output.
  - **Main transcripts.** No differing copies.
- **Prices.** Read from
  <https://platform.claude.com/docs/en/about-claude/pricing> on 2026-10-27,
  and cross-checked with the Claude API reference that ships with Claude
  Code:
  - the per-model table;
  - the cache multipliers: 1.25× for a 5m write, 2× for a 1h write, and
    0.1× for a read (0.05× on Opus 5.5 and Sonnet 5.5, 0.025× on Fable 5.1);
  - fast mode;
  - Haiku 5.5's 100K-token tier;
  - the `inference_geo` multiplier (1.1×);
  - web search ($10 per 1,000).

## Risks and assumptions

| Risk or assumption | Effect if wrong | How it is contained |
|---|---|---|
| The copy with the largest `output_tokens` is the final usage. Observed in every case, never the reverse | If Claude Code ever writes a larger partial first, that call is overcounted | A test pins the rule; the data checks above can be rerun |
| No pair spans two sessions | A call copied into another session would count twice | Dedupe is per session. A global set would cost little; switch if this is ever seen |
| The first scan takes about 6 s. A fully cold scan was not measured (it needs `sudo purge`) | A cold first open could be slower, a few times 6 s on a slow disk | The scan runs off the main thread and shows progress. Later Rescans read only what changed |
| Prices are those of 2026-10-27 | Costs drift as prices change; a new model shows as unpriced | `PricesAsOf` is shown with the cost. Unpriced calls are counted in the header. The table is one file to update |
| The cost is the API list price of the same use | Someone on a Claude subscription pays a flat fee, not this sum | The header says "est. API cost" |
| Memory | The spike peaked at 260 MB because it kept every raw usage string. The design keeps about 100k small call records, roughly 10–20 MB | Calls hold numbers only, no raw JSON |
| Advisor tokens are not in the top-level usage (codeburn's finding) | Without them, advisor use is undercounted | `usage.iterations` entries of type `advisor_message` are added, with a test |

## Out of scope

| Left out | Why |
|---|---|
| Fetching prices from the network (LiteLLM) | Decided: a built-in table, dated |
| A cache on disk | Decided: in memory only; the app writes nothing |
| Splitting a session's calls by each call's `cwd`; folding worktrees | One project per session is enough for a first version |
| Batch, data-residency on other platforms, priority tier | Claude Code transcripts don't record a batch; `inference_geo` is handled |
| Tools, MCP, git branch, activity classification, quotas, "optimize" | Codeburn features beyond "tokens by session, project, day" |
| Export (CSV/JSON) | Not asked for |
| Keeping totals after transcripts are deleted | Codeburn keeps 10 years of daily totals; this app reads what is on disk |
| Live refresh | Its own roadmap item; Rescan stays |
