# Roadmap

Work deferred from v1 (see the
[v1 design](superpowers/specs/2026-10-26-claude-manager-design.md)).
Each item gets its own design before it is built.

## v1.x — polish

- [x] UI/UX polish of every screen with the `impeccable` skill.
- [ ] Build and sign a `.app` / `.dmg` with `mygo build`.

## v2 — search and insight

- [x] Full-text search of `history.jsonl` (every prompt typed), filtered by project and date.
- [ ] Full-text search across session transcripts.
- [x] Token and cost statistics per session, project and day (from `message.usage`).
- [x] Watch `~/.claude` for changes and refresh live instead of manual Rescan.
- [x] Limits of each Claude Code account: the five-hour, weekly and per-model weekly limits, for the account in use and accounts added through Claude Code's own sign-in (after [Pitboard](https://github.com/datlechin/pitboard)'s approach).
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

- ~~Two encoded project dirs that share a `cwd` get the same project label.~~ Fixed: see docs/superpowers/specs/2026-10-27-debt-design.md.
- ~~Untitled sessions are rescanned up to 8 MB on each rescan.~~ Fixed: see docs/superpowers/specs/2026-10-27-debt-design.md.
- ~~Credential masking is over-broad: `basic …` and any query parameter, `--flag=` or `Name: value` string whose name holds key, token, secret, pass, auth, cookie, sig or credential (as `author: Jane`, `hotkey: ctrl+k` or `--design=x`) also hide ordinary config strings (fails safe: hides, never reveals).~~ Fixed for names: see docs/superpowers/specs/2026-10-27-debt-design.md. The `basic …` shape rule stays.
- ~~Each days-stepper click rescans all sessions (correct, but costly on rapid clicks).~~ Fixed: see docs/superpowers/specs/2026-10-27-debt-design.md.
