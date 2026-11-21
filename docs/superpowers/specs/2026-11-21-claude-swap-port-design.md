# claude-swap Port — Design

Date: 2026-11-21
Status: approved by the user 2026-11-21; part 1 built on `feat/native-switch`

## Goal

Claude Manager does what claude-swap (github.com/realiti4/claude-swap, fork
`lang315/claude-swap` cloned read-only into the ignored `example/`, MIT, © 2026
Onur Cetinkol) does on macOS,
in the app, without Pitboard:

1. **Native switch**: put Claude Code on any added account with one click.
2. **Auto-switch**: switch before the active account reaches its limit.
3. **Export / import**: move added accounts to another Mac.
4. **Run as account**: open Claude Code in a terminal as one account, while
   the rest of the Mac stays on the default one.

The user chose all four and chose that the app replaces Pitboard
(2026-11-21). This is a rewrite in Go of claude-swap's rules, not a code
copy. Each part is its own branch and PR, in the order above.

## What changes in the product

- `PRODUCT.md` "never edits Claude Code configuration" becomes: the app
  writes Claude Code's login and the `oauthAccount` key of `~/.claude.json`
  only when the user switches (or auto-switch, which the user turns on), and
  writes nothing else of Claude Code's.
- The Pitboard switch path goes: `Book.Switch` via `pitboard use`,
  `FindPitboard`, `PitboardCLI`, `SwitchError`, `ErrLiveRefused`,
  `SwitchTimeout`, and the "Replace Claude Code's sign-in?" dialog.
- `ReadPitboard` stays, read-only, so Pitboard's accounts still show their
  limits. They can't be switched to: the app never reads Pitboard's parked
  logins (two renewers break a refresh chain). Their block says "Add this
  account here to switch to it."

## Rules taken from claude-swap

These are claude-swap's hard rules (its `AGENTS.md`, `docs/architecture.md`,
`credentials.py`, `switcher.py`). The port keeps every one.

- A token or login is never logged or shown.
- A used refresh token's successor is saved before it is used, or the
  account is lost (the app already does this in `renew`).
- The live login is saved to its own account before it is overwritten, and
  only when it is known to be that account's. A failed save aborts the
  switch.
- Lock order: the app's lock, then Claude Code's credential locks, then its
  config lock. No network call under Claude Code's locks.
- Never write a hashed `Claude Code-credentials-<hash>` keychain item. Never
  write the default item for a non-default config directory.
- Only one renewer per refresh chain: whoever holds the login in use renews
  it, and nobody else.

## 1. Native switch

### Where logins live

- Added accounts: keychain service `claude-manager-park`, account
  `<accountUuid>/<orgUuid>` (exists today). The item now holds the whole
  `claudeAiOauth` object as Claude Code wrote it, not only the fields
  `Login` reads, so nothing Claude Code needs is lost through a switch.
  `renew` replaces only the token fields.
- Each overwrite keeps the previous item as `claude-manager-park-prev`, same
  account: one undo if a save turns out wrong.
- `accounts.json` gains each account's `oauthAccount` object from
  `~/.claude.json` (no secret: email, uuids, org name, display name), taken
  when the account is added and each time it is switched away from.

### Steps of a switch to account T

Before the locks (network allowed):

1. Read the live login; ask whose it is (`identify`, cached per token).
   This is account L. A lapsed login can't be asked about. It is L's when
   the uuids Claude Code writes inside `claudeAiOauth` name added account L
   and `~/.claude.json`'s `oauthAccount` names L too, or else when its
   refresh token equals L's saved one. When L's saved login expires later
   than the lapsed one (signed in again meanwhile), it is kept and the
   lapsed one is not saved. Anything else stops the switch ("Open Claude
   Code to renew it, then switch"): a wrong owner would overwrite that
   account's only saved login.

Under the app's lock and Claude Code's locks (proper-lockfile directories,
as Claude Code makes them: `~/.claude/.oauth_refresh.lock` then
`~/.claude.lock`, then `~/.claude.json.lock`; `mkdir` takes, `rmdir` frees,
mtime touched every 3 s, stale after 60 s / 10 s; wait 9 s per lock):

2. Read the live blob again. If it changed since step 1 (Claude Code
   renewed it meanwhile), abort with "Claude Code renewed its sign-in during
   the switch. Try again." The next try identifies the new login.
3. Save the outgoing login: L's `claudeAiOauth` into L's park item, and L's
   `oauthAccount` into `accounts.json`. If L is not an added account, it
   becomes one (the account in use joins the list, as claude-swap does with
   `cswap add`). A failed save aborts; nothing is written yet.
4. Write T's login to the live item (`Claude Code-credentials`, account
   `$USER`): the live blob with `claudeAiOauth` replaced by T's, every other
   key kept (`mcpOAuth`, `pluginSecrets`, …).
5. If `~/.claude/.credentials.json` exists, write the same bytes there
   (Claude Code reloads on its mtime). Never create it.
6. Replace only `oauthAccount` in `~/.claude.json` with T's, written
   atomically (temp file + rename), every other key kept byte-for-byte
   where JSON allows.
7. Any failure in 4–6 restores the earlier live blob, `.credentials.json`
   and `~/.claude.json`, in reverse order, and says so. A failed restore
   says "Claude Code's sign-in may need `claude /login`."

After the switch, T's park item is no longer renewed by the app: Refresh
already reads the account in use with the live token only, and never renews
it. L's park item is renewed by the app from now on.

`CLAUDE_CONFIG_DIR` set when the app starts: the switch is off, with the
note "Switching works with Claude Code's default folder only." (claude-swap
writes `.credentials.json` there; not needed now.)

API-key accounts: not supported (the app has none today).

An added account with no `oauthAccount` kept (added before this change) is
refused: "Sign in again to this account to switch to it." A made-up object
might not satisfy Claude Code.

The app never renews a saved login whose refresh token is the one Claude
Code holds, even when it can't tell that account is in use: two renewers
break a refresh chain.

The login in use is followed (2026-11-21, after the user stopped switching
with Pitboard). On each Limits refresh, in Claude Code's default folder only:

- The account in use, once identified by Anthropic, is added when it isn't
  yet, as `cswap add` does: its `claudeAiOauth` from Claude Code's login, and
  its `oauthAccount` from `~/.claude.json`, which must name the same account.
- Its saved login is replaced by Claude Code's when Claude Code's expires
  later (claude-swap keeps only a strictly newer one too), the replaced one
  kept under `claude-manager-park-prev`.
- Only the login Anthropic identified is saved: if Claude Code changed it
  since, nothing is saved until the next refresh.
- Its menu has no Remove…: the next refresh would add it back.

Elsewhere than the default folder an added account's login is its own chain,
renewed by the app, and is never replaced.

Known limit: the saved copy is as fresh as the last Limits refresh (the page
shown, or auto-switch on). A change of account outside the app right after
Claude Code renews leaves it one renewal behind. Switching with Pitboard
beside the app gives both the same live refresh token: don't.

Pitboard: once the app switches, Pitboard must not switch too; two programs
saving the same login are two renewers on one refresh chain.

Keychain writes: Claude Code's login with MCP logins beside it is ~8 KB,
over `security -i`'s line, so it goes on `security`'s argument line,
hex-encoded, for the instant the command runs (claude-swap does the same).

### UI

The Limits page's switch button shows on every added account not in use.
No confirm (a switch is undone by another switch). After it: "Claude Code
now uses <email>. Running sessions follow within about a minute on macOS,
or when restarted."

### Verify

- Unit tests with `Memory` vault and a temp home: outgoing saved before the
  live write; failed park save leaves everything untouched; failed
  `~/.claude.json` write restores the live item; other blob keys and other
  `~/.claude.json` keys survive; lock directories made and removed; a held
  fresh lock makes the switch wait then fail; a stale one is taken over.
- Real check, by the user, with the user's go-ahead: switch A → B → A, then
  `claude -p hi` works on each, and `security dump-keychain` shows one park
  item per account.

## 2. Auto-switch

Runs inside the app every minute (and on each Limits refresh), whatever page
is shown and whether the app is in front, only while the app is open. Off by
default. A checkbox on the Limits page turns it on; it can't be turned on
when the app can't switch (another `CLAUDE_CONFIG_DIR`).

Settings, saved in `accounts.json` (`auto`), with claude-swap's defaults and
ranges, clamped on load:

| Setting | Default | Range |
|---|---|---|
| Five-hour line | 90 % | 50–99.9 |
| Weekly line (7-day) | none: the five-hour line | 50–99.9 |
| Cooldown | 5 min | 0–24 h |
| Margin (hysteresis) | 10 points | 0–50 |
| Strategy | Most left (`best`) | Most left, Weekly reset first, Spend soonest reset, By priority |

Per added account: held out of rotation, its own five-hour line, a
priority tier (0–1000). They survive switches and Sign In Again.

Rules: claude-swap's `docs/auto-switch.md` and `autoswitch.py`
(`_tick_inner`, `_rank_candidates`, `lines.py`), ported in `account.decide`:

- Windows: the five-hour and 7-day ones only. Per-model windows count in
  claude-swap only with `--model`; that option is not ported.
- Each window against its own line; the binding window is the one with the
  least line headroom, on the five-hour line's scale, over-line first.
- Triggers: over a line (proactive; at-limit at 100 %, which skips the
  cooldown); under its lines, Spend soonest reset and By priority still look
  for a better account.
- Landing: an added account, not held out, with a kept `oauthAccount`, a
  reading under 5 minutes old and no error, and room under 100 %.
- Ranking per strategy as in `_rank_candidates`, including the all-over rule
  (at least `min(2 × room, room + 3)` points more) and By priority never
  landing on an account over a line.
- A manual switch starts the cooldown too (claude-swap counts only its own
  switches), so a strategy doesn't undo the user's choice a minute later.
  The cooldown is kept in memory: a restart clears it.
- Not ported: the no-return bar, failover when the account in use can't be
  read, the idle hold, adaptive polling, `--model`.

Tested by `TestDecide` (cases from `docs/auto-switch.md`), `TestAutoSwitch`,
`TestAccountAutoSurvivesSwitches`, and view tests.

## 3. Export / import

- The file is claude-swap's export (`transfer.py`, version 1): the app reads
  claude-swap's exports and writes that format: `{version: 1, exportedAt, exportedFrom, encrypted: false,
  accounts: [{number, email, uuid, organizationUuid, organizationName, added,
  credentials: {claudeAiOauth}, config: {oauthAccount}}]}`. This replaces the
  shape first planned here. Its `number` is claude-swap's slot, from 1.
- Export (toolbar "Export…", a save panel on the Desktop) writes each added
  account that has an `oauthAccount` and a complete saved login (access and
  refresh token, expiry), so its file always imports; the others are left
  out, and the toast counts them. The account in
  use goes with Claude Code's login when that login's own uuids name it and
  it lapses later than the saved one. Only `claudeAiOauth` goes out, never
  `mcpOAuth`. The file is 0600: a replaced file is made 0600 first, and a
  link is refused rather than followed. The panel and the toast say "This
  file holds your sign-ins, unencrypted. Keep it private and delete it after
  import." and the two-Macs warning below.
- Import (toolbar "Import…", an open panel) checks the whole file before it
  writes anything, and rejects it when an entry has no email, no complete
  login (access and refresh token, expiry), no `oauthAccount`; when its
  `uuid` / `organizationUuid` or its login's own uuids name another account
  than its `oauthAccount`; or when an account repeats. A JSON file with no
  `version` is "not an accounts file". Then, per entry: an
  API key is left alone (the app holds subscription logins only); the account
  in use is left alone, named by `~/.claude.json` or by Claude Code's login
  (follow keeps that login); a login no newer
  than the saved one is left alone; otherwise it is saved as an added
  account, keeping that account's auto-switch settings. From a `--full`
  export only `claudeAiOauth` and `oauthAccount` are taken. Import itself asks
  nothing of Anthropic; the Limits refresh after it reads each imported
  account, and renews one whose access token has lapsed: from then on this
  Mac renews that login.
- Warning to the user, both ways: the same login on two Macs is two
  renewers. The save and open panels and both toasts say "Use each account on one Mac
  only: two Macs renewing one sign-in break it."

Verified: round trip through a temp file and `Memory` vault; a claude-swap
export with an API-key entry; Export's own file always imports; each
rejection writes nothing and shows no token; mutation checks on every guard
(19 of 19 caught). Not verified: claude-swap importing
ours (checked against its `_validate_imported_account` only), and a real
export and import between two Macs.

## 4. Run as account

- "Open in Terminal…" in the "⋯" menu of an added account not in use (Claude
  Code's default folder only; "in use" also when Claude Code's login names
  it or shares its refresh token, whatever `~/.claude.json` says) asks for a folder, then runs in Terminal.app
  `cd <folder> && CLAUDE_CONFIG_DIR=<profile> exec claude`. The command goes
  to `osascript` as an argument, so only the shell's single quotes apply.
- Profile: `~/Library/Application Support/Claude Manager/profiles/<first 12
  hex of SHA-256 of the account key>/`, 0700: no email in a path, since an
  imported file's emails are not checked for one.
- Order: the "in a terminal" mark is saved first; then the profile's
  `Claude Code-credentials-<hash>` item is deleted (it would shadow the
  seed); `.credentials.json` (0600, `claudeAiOauth` only) is written from the
  saved login; `.claude.json` gets `oauthAccount`, `hasCompletedOnboarding`
  and the user's `mcpServers` spliced in (an earlier one is kept otherwise);
  then the launch. A failed seed removes the profile's logins and leaves the
  mark for take-back to clear.
- Shared by symlink from `~/.claude`, only where it exists and the profile
  has nothing of that name: `settings.json`, `keybindings.json`,
  `CLAUDE.md`, `skills`, `commands`, `agents`, `rules`, `projects`,
  `history.jsonl`. Nothing is written into `~/.claude`.
- While marked: Refresh reads its limits with the profile's login (its
  keychain item, else `.credentials.json`) and never renews; a refused token
  shows "Its sign-in in the terminal has lapsed…". Switch, Remove, Sign In
  Again, Export (left out, counted) and Import (left alone) refuse it, and
  auto-switch never lands on it. Opening it again launches the profile as it
  is, without a new seed, and restarts take-back's wait (`Since`, `Seen`), so
  it never clears the logins under the claude just opened. Outside the
  default folder a marked account is still read (any keychain account).
- Take back, on each Refresh: a claude runs on the profile when one of its
  `sessions/<pid>.json` has a live pid (claude-swap's `process_detection`);
  an unreadable folder or record keeps the mark. Not before 2 minutes from
  the launch unless a claude was seen. Once none runs: the profile's login,
  when complete and newer than the saved one, is saved and read back; then
  the profile's `.credentials.json` and keychain item are removed, so a
  claude run there later from shell history can't renew a login the app
  holds; then the mark is cleared.
- Cleanup: `Store.Profiles` makes each profile's `sessions/` count as live,
  since their transcripts are in the shared `projects/`.
- Sharing (part 4b; claude-swap's `--no-share` / `--no-share-history`): the
  Open in Terminal dialog has "Share settings, CLAUDE.md, skills and MCP
  servers" and "Share chat history with the other accounts", both on. Off,
  the profile's links of that group are removed (only links to `~/.claude`'s
  own item; the profile's own files stay) and, for setup, `mcpServers` is not
  spliced in again. They apply at the seed; opened again while marked, the
  profile is left as it is.
- Skipped: directory-to-account mapping (`cswap map`); merging a profile's
  own history into `~/.claude`; removing an `mcpServers` mirrored earlier
  (the app keeps no marker of what it wrote).

Verified: seed, order, refusals, reopen, read-only refresh, take-back
(grace, failed scan, running, newer, older), session-record scan, quoting
through `sh` with `'`, `"`, `$` and spaces; mutation checks (21 account,
5 view, all caught). Not verified: a real Terminal launch and a real claude
on a profile; that Claude Code reads the seeded `.credentials.json` on this
Mac (claude-swap relies on it).

## 5. Weekly pace and per-model windows

- Pace (`pace.py` `compute_pace`): a weekly window (`weekly_all`,
  `seven_day`, `weekly_scoped`) read at `ReadAt` is in its cycle by the reset
  rolled by whole 7-day steps; expected use is the elapsed share of the week.
  Nothing in the first 24 hours of a cycle. "Ahead of pace", muted, in the
  row's status slot, at 15 points or more over expected; "Limit reached"
  takes the slot first. The projection fields (`willLastToReset`, ETA) stay
  out, as claude-swap keeps them out of its UI.
- Per-model windows (`--model`, `autoswitch.model`): `Auto.Model`, a comma
  list matched case-insensitively on the scoped window's name, `all` for
  every one. A named window is one more window in `stand`, on the weekly line
  when one is set, else the account's line; its reset does not stand in for
  the 7-day reset in the weekly strategies. Set in Auto-Switch Settings,
  "Model windows".
- Not ported: the one-shot warning for a model name no account reports.

Verified: `TestPace` (rolling both ways, the first day, the 15-point edge,
kinds), `TestDecide` (named, `all`, unnamed, a spent model window as no
landing, the weekly line), view tests; mutation checks (10 of 10 caught).

## 6. Menu bar and start at login

- claude-swap's menu bar (`menubar.py`) runs under launchd from a wrapper
  app; here the app itself stays up: closing the window hides it (unless a
  quit is under way, so Quit still quits), the Dock icon shows it again.
- The item (MyGo's `NewTray`, title only) shows the fullest five-hour or
  7-day share of the account in use. Its menu, built by `view.App.Tray` and
  set by main after every view update: a status row, one row per account
  (switches on a click when Switch would be offered: added, not in use, not
  in a terminal, no switch running, default folder), Auto-switch, Refresh,
  Open Claude Manager, Start at Login (MyGo's `SetOpenAtLogin`), Quit.
- Limits are read every 5 minutes on any page (`TrayTick`; the 3-minute
  floor holds), where they were read only on the Limits page in front.
- Not ported: rotate / best / next-available switch actions (auto-switch
  covers them), add / disable / remove from the menu (in the window), the
  ⚠ stopped-engine title, the wrapper app and LaunchAgent.

Verified: `TestTray`, `TestTrayRows`, `TestTrayNoSwitchOutsideDefaultFolder`,
`TestTrayTickReadsOnAnyPage`; mutation checks (9 of 9 caught). Not
verified: the real menu bar item, hide-on-close and Start at Login (main's
wiring has no test).

## Not ported

Superseded by the table in `docs/roadmap.md` ("claude-swap features, ported
or not"): the menu bar, start at login, pace and per-model windows moved to
parts 5 and 6.

## Open before PR 1

- The real Add Account check (PR #9) is still owed; native switch builds on
  it. Do it with PR 1's real switch check.
- Credit claude-swap (MIT) in the README and in this spec.
