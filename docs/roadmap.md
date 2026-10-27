# Roadmap

Work deferred from v1 (see the
[v1 design](superpowers/specs/2026-10-26-claude-manager-design.md)).
Each item gets its own design before it is built.

## v1.x — polish

- [x] UI/UX polish of every screen with the `impeccable` skill.
- [ ] Build and sign a `.app` / `.dmg` with `mygo build`.

## v2 — search and insight

- [ ] Full-text search of `history.jsonl` (every prompt typed), filtered by project and date.
- [ ] Full-text search across session transcripts.
- [ ] Token and cost statistics per session, project and day (from `message.usage`).
- [ ] Watch `~/.claude` for changes and refresh live instead of manual Rescan.
- [ ] Export a session to Markdown.

## v3 — editing configuration

- [ ] Edit `settings.json` safely (backup first, validate JSON, show a diff before saving).
- [ ] Add, edit and remove hooks.
- [ ] Add, edit and remove MCP servers.
- [ ] Enable and disable plugins and skills.
- [ ] Edit `CLAUDE.md` and memory files.
- [ ] Restore a `settings.json.bak*` over `settings.json`.

## Known limitations (v1)

Found in review and left for later.

- Two encoded project dirs that share a `cwd` get the same project label.
- Transcript reading continues after Back.
- `.claude.json` is decoded whole (only `mcpServers` is needed).
- Read errors on skills, commands and memory are not surfaced.
- The config tree is not virtualized.
- Checked rows hidden by the search are still trashed.
- `int(days)` truncates a fractional number of days.
- Untitled sessions are rescanned up to 8 MB on each rescan.
- The file-history orphan group offers non-UUID entries such as `.DS_Store`.
- Credential masking is over-broad: `basic …`, `--…key…=` flags and `sig`/`pass` inside query words also hide ordinary config strings (fails safe: hides, never reveals). An MCP `"--header", "X-API-Key: …"` pair still shows the key.
- Move to Trash stays disabled until a transcript left with Back finishes reading.
- Each days-stepper click rescans all sessions (correct, but costly on rapid clicks).
- The spec says a title past 8 MB "reads as untitled"; the row actually falls back to the first prompt.
