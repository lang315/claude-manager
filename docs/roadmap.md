# Roadmap

Work deferred from v1 (see the
[v1 design](superpowers/specs/2026-10-26-claude-manager-design.md)).
Each item gets its own design before it is built.

## v1.x — polish

- [ ] UI/UX polish of every screen with the `impeccable` skill.
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
