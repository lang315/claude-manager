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
- [x] Limits of each Claude Code account: the five-hour, weekly and per-model weekly limits, for the account in use and accounts added through Claude Code's own sign-in (after [Pitboard](https://github.com/datlechin/pitboard)'s approach), and, read-only, the accounts Pitboard itself follows.
- [ ] Export a session to Markdown.

## claude-swap port — account switching without Pitboard

Claude-swap's macOS features (github.com/realiti4/claude-swap, MIT), rewritten
in Go, one PR each. Design: `superpowers/specs/2026-11-21-claude-swap-port-design.md`.

- [ ] **1. Native switch.** Switch to an added account from the Limits page;
  the login in use is saved first, under Claude Code's own locks. Replaces the
  `pitboard use` path. Owed: a real switch A → B → A on this Mac, together
  with the real Add Account check from PR #9.
- [ ] **2. Auto-switch.** Switch before the account in use reaches its
  five-hour or weekly line, while the app is open; strategies, cooldown,
  margin, per-account off / line / priority.
- [ ] **1b. Follow the login in use.** The account in use is added by itself,
  and its saved login follows Claude Code's renewals, so a change of account
  outside the app doesn't leave it spent. Done once Pitboard was no longer
  used for switching.
- [ ] **3. Export / import.** Move added accounts to another Mac in one
  private (0600, unencrypted) JSON file, in claude-swap's export format; the
  app reads claude-swap's exports. Owed: a real export and import between two
  Macs.
- [ ] **4. Run as account.** Open Claude Code in Terminal as one account
  (its own `CLAUDE_CONFIG_DIR`), the rest of the Mac staying on the default;
  its renewed login is taken back once it exits.
- [ ] **5. Weekly pace and per-model windows.** Mark a weekly window used
  ahead of the week's pace (`pace.py`); let auto-switch also weigh one
  model's weekly window, as `cswap auto --model Fable`.
- [ ] **6. Menu bar and start at login.** A menu bar item with each account's
  limits, Switch and Auto-switch, that keeps running with the window closed;
  Start at login (`menubar.py`, `launch_agent.py`, done with MyGo's tray and
  open-at-login instead of a LaunchAgent).

### claude-swap features, ported or not

Every feature in claude-swap's README and `src/`, and where it went.

| Feature | Here |
|---|---|
| `add` (capture the live login), `remove`, `list`, `status` | Ported: Add Account, follow (1b), Remove, the Limits page |
| `switch` | Ported: 1 |
| `auto`: strategies, lines, weekly line, cooldown, margin, `disable`/`enable`, `threshold`, `priority` | Ported: 2 |
| `auto --model` per-model windows | 5 |
| `auto`: no-return bar, failover on failed reads, idle hold, adaptive polling | Not ported: the app reads every 5 minutes (1 while auto-switch is on) and only acts on readings under 5 minutes old, so it holds instead of failing over |
| `auto --once`, `--dry-run`, `--json`, exit codes | Not applicable: CLI and cron only |
| Dead-token quarantine | Covered: a refused login shows its error and is never an auto-switch candidate; Sign In Again replaces it |
| `export` / `import` | Ported: 3 |
| `run` (session mode), history and setup sharing, MCP mirror | 4 |
| `map` / `unmap` (directory to account) | Not applicable: it picks the account for a bare `cswap run` typed in a folder; the app asks for the folder |
| Weekly pace marker | 5 |
| Menu bar, LaunchAgent, Start at login | 6 |
| `alias`, slot numbers, `move`, `swap` | Not applicable: accounts are named by email in a GUI |
| `unclaimed` stash | Not needed: an unknown outgoing login becomes an added account |
| API-key accounts | Not ported: Claude Manager reads subscription limits, which API keys have none of; Import leaves them out |
| `config` | Covered: Auto-Switch Settings |
| TUI, `watch` | Covered: the app itself |
| `upgrade`, update check, `purge` | Not applicable: app updates; removing every account empties the app's keychain items |
| Linux, Windows, WSL | Not applicable: macOS app |

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
