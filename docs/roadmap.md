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

- [x] **1. Native switch.** (#12, draft) Switch to an added account from the Limits page;
  the login in use is saved first, under Claude Code's own locks. Replaces the
  `pitboard use` path. Owed: a real switch A → B → A on this Mac, together
  with the real Add Account check from PR #9.
- [x] **2. Auto-switch.** (#13, draft) Switch before the account in use reaches its
  five-hour or weekly line, while the app is open; strategies, cooldown,
  margin, per-account off / line / priority.
- [x] **1b. Follow the login in use.** (#14, draft) The account in use is added by itself,
  and its saved login follows Claude Code's renewals, so a change of account
  outside the app doesn't leave it spent. Done once Pitboard was no longer
  used for switching.
- [x] **3. Export / import.** (#15, draft) Move added accounts to another Mac in one
  private (0600, unencrypted) JSON file, in claude-swap's export format; the
  app reads claude-swap's exports. Owed: a real export and import between two
  Macs.
- [x] **4. Run as account.** (#16, #19, draft) Open Claude Code in Terminal as one account
  (its own `CLAUDE_CONFIG_DIR`), the rest of the Mac staying on the default;
  its renewed login is taken back once it exits.
- [x] **2b. The rest of auto-switch.** (#20, draft) The no-return bar, failover, the idle
  hold, trust in a failing account's last reading, adaptive polling, and the
  unknown-model-name warning.
- [x] **5. Weekly pace and per-model windows.** (#17, draft) Mark a weekly window used
  ahead of the week's pace (`pace.py`); let auto-switch also weigh one
  model's weekly window, as `cswap auto --model Fable`.
- [x] **7. Extra usage.** (#21, draft) Each account's pay-as-you-go spend of its monthly
  cap, as claude-swap's spend line.
- [x] **6. Menu bar and start at login.** (#18, draft) A menu bar item with each account's
  limits, Switch and Auto-switch, that keeps running with the window closed;
  Start at login (`menubar.py`, `launch_agent.py`, done with MyGo's tray and
  open-at-login instead of a LaunchAgent).
- [x] **2c. Tick interval and unhealthy ticks** as settings (#22, draft).
- [x] **3b. Full backup export** (`export --full`) (#23, draft).
- [x] **3c. Readings from another Mac** (`import-usage`, `--hold`) (#24, draft).
- [x] **8. Setup-token accounts and the header probe** (#25, draft).
- [x] **8b. Console API key accounts** (#26, draft).
- [x] **9b. Aliases, folder mappings, version** on the command line (#28, draft).
- [x] **10. Settings, appearance, updates, --debug, docs** (#29, draft).
- [x] **11. Slot numbers, move, swap** (#30, draft).
- [x] **12. Unclaimed logins** (#31, draft).
- [x] **13. Linux, WSL and Windows** (#32, draft).
- [x] **14. Command line on par with cswap's "Other commands"** (#33, draft).
- [x] **9. Command line.** `claude-manager list|status|switch|auto --once|add-token|remove|purge|export|import|run`, as cswap's (#27, draft).
- Owed for all: the real runs (Add Account, switch A → B → A, export and
  import between two Macs, Open in Terminal, the menu bar, a setup-token, an
  API key). Auto-switch stays off until the switch check passes.

### claude-swap features, ported or not

Every feature in claude-swap's README and `src/`, and where it went: each
subcommand of `cli.py` (`add`, `add-token`, `disable`, `enable`, `export`,
`import`, `import-usage`, `list`, `menubar`, `purge`, `remove`, `status`,
`tui`, `update`/`upgrade`, `watch`, and the verbs `switch`, `auto`, `run`,
`map`, `alias`, `move`, `swap`, `threshold`, `priority`, `config`,
`unclaimed`), its flags, and the settings of `settings.py`.

| Feature | Here |
|---|---|
| `add` (capture the live login), `remove`, `list`, `status` | Ported: Add Account, follow (1b), Remove, the Limits page; `remove`, `list [--json]`, `status [--json]` on the command line (9) |
| `switch` | Ported: 1 |
| `auto`: strategies, lines, weekly line, cooldown, margin, `disable`/`enable`, `threshold`, `priority` | Ported: 2 |
| `auto --model` per-model windows | 5 (Model windows in Auto-Switch Settings) |
| `auto`: no-return bar, failover on failed reads, idle hold, trust in the last reading, adaptive polling, unknown model names | 2b |
| `auto --once`, `--dry-run`, `--json`, exit codes | 9: `claude-manager auto --once [--dry-run] [--json]`, exit 0/1/2/3 as cswap's; `list --json` is cswap's schema, so either app's `import-usage` takes it |
| Dead-token quarantine | 2b: a refused refresh token is never an auto-switch candidate and is not posted again until the saved login changes; Sign In Again replaces it |
| `export` / `import` | Ported: 3; `claude-manager export|import` (9) |
| `run` (session mode), history and setup sharing, MCP mirror | 4; `claude-manager run <email> [folder] [--no-share] [--no-share-history]` (9); `--no-share` / `--no-share-history` as the dialog's two checkboxes (4b) |
| `map` / `unmap` (directory to account) | 9b: `claude-manager map|unmap`, and `run` with no account opens the one mapped to the folder |
| Weekly pace marker | 5 |
| Menu bar, LaunchAgent, Start at login | 6 (the app itself stays in the menu bar; MyGo's open-at-login, no LaunchAgent) |
| `alias` | 9b: `claude-manager alias`, usable wherever an email is; exported and imported |
| Slot numbers, `move`, `swap` | 11: each added account has a number from 1; `claude-manager switch 2`, `move`, `swap`; `list` by number; export and import keep it |
| `unclaimed` stash | 12: a lapsed login in use that names no added account is kept (`claude-manager-unclaimed`) before a switch replaces it; `claude-manager unclaimed [--purge ID]` |
| API-key accounts | 8b: added, switched to (Claude Code's managed key), exported and imported; no limits are read |
| `config` | Auto-Switch Settings, and `claude-manager config` (10) |
| TUI, `watch` | 14: `claude-manager tui` opens the app, `watch` on Limits |
| `add [--slot] [--alias]`, `disable`/`enable`, `threshold`, `priority` (command line) | 14 |
| `switch` with no account, `--strategy best|next-available` | 14 |
| `auto` in the foreground, `--threshold`, `--weekly-threshold`, `--model`, `--strategy`, `--interval`, `--include-api-key-accounts` | 14: for that run only, never saved |
| `list --token-status` | 14 |
| `run <account> -- <args>`, `--require-session` | 14 |
| `export -`, `import -` | 14 |
| `export --account`, `import --force`, `switch N --force` | 14 |
| `menubar --install-service`, `--uninstall-service`, `--service-status`, `--auto-switch`, `--no-auto-switch`, `--foreground` | 14: a LaunchAgent on macOS (as cswap's), an autostart entry on Linux, a Run key on Windows |
| `purge` | 9: `claude-manager purge --yes` |
| `upgrade`, update check (`updates.check`) | 10: a daily check and "Update to X…" in the menu bar, `claude-manager upgrade`, through MyGo's updates; they work once the app is built with an update feed (`mygo.config.ts`), which needs its signing key |
| Linux, Windows, WSL | 13: builds for all three; without a keychain Claude Code's login is `.credentials.json` and its API key `primaryApiKey`, the app's items a 0600 `vault.json`; Open in Terminal opens the system's terminal (Windows Terminal under WSL, a console on Windows) |
| Spend (extra usage) on each account | 7: an "Extra usage" row, and "extra N%" in the menu bar |
| `switch <account>` | 1; `claude-manager switch <email> [--json]` (9) |
| `add-token` with a setup-token; `usage.headerProbe` | 8: Add Token…, and "Read setup-token usage"; `claude-manager add-token <token|->` (9) |
| `add-token` with a Console API key | 8b: Add Token… takes either kind |
| `import-usage`, `--hold` | 3c: Import takes a `cswap list --json` file too, with "Don't read those accounts here for N minutes" |
| `export --full` (a same-Mac backup) | 3b: the Export dialog's "Full backup for this Mac" |
| `run --require-session` | Covered: Open in Terminal is never offered for the account in use |
| `auto --interval` | 2c (the setting) |
| `ui.theme` | 10: Appearance in the menu bar, `config set ui.theme` |
| `help` | 9: `claude-manager help` |
| `--version` | 9b: `claude-manager version` |
| `--debug` | 10: `claude-manager <command> --debug` prints each request (method, path, status, time) |
| Setting `autoswitch.threshold`, `weeklyThreshold`, `cooldownSeconds`, `hysteresisPct`, `strategy`, `model` | Ported: Auto-Switch Settings (2, 5) |
| Settings `autoswitch.intervalSeconds`, `unhealthyTicks` | 2c: Auto-Switch Settings, "Check every (seconds)" (15–3600, 60) and "Unreadable ticks before moving on" (1–100, 3) |
| Setting `autoswitch.includeApiKeyAccounts` | 8b: "Fall back on an API key account when no account has room" |
| Setting `run.shareHistory` | Covered: the Open in Terminal dialog's history checkbox (4b), asked each time |
| Settings `updates.check`, `ui.theme`, `run.shareHistory`; `config` get/set/unset/list | 10: `claude-manager config` |
| Setting `usage.headerProbe` | 8: "Read setup-token usage" in Add Token |
| README "How it works": a target's token freshened before a switch | Covered in part: auto-switch lands only on an account its own login read well in the last 5 minutes (an hour while Anthropic is out of reach); the login itself is not renewed first |
| README "How it works": plan refreshed every six hours | Covered: the plan comes with each login (its `subscriptionType`) |
| README "JSON output" | 9: `list`, `status`, `switch` take `--json` (cswap's schema 1); `auto --once --json` prints one event |
| README "Tips", "Data locations", "Uninstall" | 10: `docs/accounts.md`, for this app |

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
