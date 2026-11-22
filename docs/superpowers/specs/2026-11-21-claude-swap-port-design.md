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
- The no-return bar, failover, the idle hold, trust in a failing account's
  last reading and adaptive polling came in part 2b; `--model` in part 5.

Tested by `TestDecide` (cases from `docs/auto-switch.md`), `TestAutoSwitch`,
`TestAccountAutoSurvivesSwitches`, and view tests.

## 2b. The rest of auto-switch

From claude-swap's `autoswitch.py`, `usage_store.py` and `poll_policy.py`:

- Trust (`decision_value`, `TRUST_MAX_AGE_S`, `RATE_LIMIT_TRUST_MAX_AGE_S`):
  a reading is decided on when under 5 minutes old; while reads of the
  account fail for a passing reason (Anthropic out of reach, asking to wait,
  or answering 5xx), its last good one for an hour, two when it asked to
  wait. Any other failure (a refused, spent or lapsed login, another
  answer) is never trusted: a switch doesn't test the target's login.
- Idle hold: Claude Code's lapsed login on the account in use holds
  auto-switch for as long as it lasts. claude-swap caps the hold at 30
  minutes (`IDLE_HOLD_MAX_S`) because it renews the login in use itself, so
  a long lapse there means a dead token; the app never renews it, so on an
  idle Mac a lapse is the rule, and Claude Code renews it on the next
  message.
- Failover (`unhealthy_ticks`, 3 ticks of a minute): the account in use
  unreadable for 3 minutes, timed from when it was first seen so (Refresh by
  hand is no tick), moves to the account with the most room, ranked as at
  its limit, with no cooldown, no no-return bar and, by priority, no tier
  gate. Never when nobody is signed in to Claude Code, or its login can't
  be read at all (a locked keychain): that holds.
- Failure backoff (`_failure_backoff_s`): a failed read is tried again
  after 30 seconds, doubling to 10 minutes, not on every tick. The 2-hour
  trust needs the rate-limit error itself; any other failure clears the
  wait Anthropic asked for.
- Quarantine: a refresh token Anthropic refused (`invalid_grant`) is not
  posted again until the account's saved login changes (Sign In Again,
  Import, a terminal's take-back). In memory; a restart tries it once.
- No-return bar (`lastSwitchFrom`, `_left_account_recovered`,
  `_all_above_margin_met`): after a move, while auto-switch's landing is in
  use, a proactive or consume move doesn't land on the account it left until
  that account recovered (its line headroom up 3 points, its binding reset 5
  minutes nearer, or, left on a failover, under its lines) and leads the
  account in use by min(2×, +3) of line headroom. A switch by hand lifts it;
  at the limit and failover ignore it. Kept in memory, as the cooldown.
- Polling (`poll_policy.plan_after_fetch`, in the large): the account in use
  is read first, every 3 minutes, every minute while auto-switch is on and
  it climbed a point within 15 of a line; the others every 5 minutes, every
  3 while the account in use is within 15 of a line or unreadable; a spent
  account (a 5h or 7d window at 100%) other than the one in use every 10,
  but a minute after that window resets; the one in use stays at 3, so its
  reading stays fresh (claude-swap trusts it until its next poll instead). Refresh by hand reads all. Ported in `nextPoll`: movement scaling
  and jitter, and every account's poll capped a minute past its soonest
  reset. Not ported: one candidate per tick, the 429 AIMD (Anthropic's Retry-After
  already holds an account).
- Model names (`_check_model_names`): once every added account has been
  read, a Model windows name no account has a window for shows in the
  auto-switch row, "… matches no account's windows: only the 5h and 7d
  limits count for it (typo?)".

Verified: `TestDecideMemo` (17 cases), `TestDecideBarMargin`,
`TestAutoSwitchHoldsOnLapsedLogin`, `TestAutoSwitchFailover`,
`TestAutoSwitchNeverSignsIn`, `TestAutoSwitchSpentInUseStaysFresh`,
`TestAutoSwitchSetsTheBar`, `TestRefreshPolling`, `TestUnseenModels`,
`TestAutoSwitchNeverSignsInOnUnreadableLogin`,
`TestRefreshClearsWaitOnOtherFailure`, `TestRefreshQuarantinesARefusedLogin`,
`TestRefreshBacksOff`, `TestBackoffCaps`, `TestRefreshSpentWithPastReset`;
mutation checks (36 of 36 caught).

## 2c. Tick interval and unhealthy ticks

- `Auto.Interval` (`autoswitch.intervalSeconds`, 15–3600 s, default 60) and
  `Auto.Ticks` (`autoswitch.unhealthyTicks`, 1–100, default 3), 0 meaning
  the default so earlier settings files read the same. main calls
  `AutoTick` every 15 seconds; it ticks once `Interval` has passed since the
  last tick. Failover waits `Ticks × Interval` of failed reads.
- Two fields in Auto-Switch Settings: "Check every (seconds)" and
  "Unreadable ticks before moving on".

Verified: `TestAutoSwitchTicksSetting`, `TestAutoSettingsClamped`,
`TestAutoTickInterval`, `TestAutoSwitchSettings`; mutation checks (6 of 6
caught).

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

## 3b. Full backup (`export --full`)

- Export opens a dialog first: the warnings, "Full backup for this Mac",
  Cancel and "Choose File…". Ticked, the account in use goes with Claude
  Code's whole login (its `mcpOAuth` too) and the whole of `~/.claude.json`,
  as claude-swap's `--full` does for its active account; the other accounts
  stay as they are, since the app keeps only their login and
  `oauthAccount`. Import takes only `claudeAiOauth` and `oauthAccount`
  either way, as before.

Verified: `TestExportFull`, `TestLimitsExportFull`; mutation checks (5 of 5
caught).

## 3c. Readings from another Mac (`import-usage`, `--hold`)

- Import opens a dialog: what each kind of file does, "Don't read those
  accounts here for N minutes" (off; 1–60, 10), Cancel, "Choose File…". A
  file with `schemaVersion` is a readings file (`cswap list --json`, schema
  1); otherwise it is an accounts export, as before.
- Readings (`json_output.usage_from_json`, `UsageStore.adopt`): the whole
  file is checked first (email and `organizationUuid` on every row; on an
  "ok" row a non-negative `usageAgeSeconds` and a usage with a window, each
  share valid, each reset a time, a spend with amounts and currency; no
  account twice). Rows not "ok" or for an account not here are skipped. A
  row's reading, taken `usageAgeSeconds` ago, replaces the account's when it
  is newer: `fiveHour`, `sevenDay`, `scoped` and `spend` become its session,
  weekly, model and extra usage limits.
- Hold: `HeldUntil` (saved) is the hold asked for, never past an hour after
  the reading nor its earliest reset still ahead. Until then Refresh, even
  by hand, doesn't read the account, and auto-switch trusts the reading.
  Unticked, holds stay as they are; claude-swap's `--hold 0` (lift) is not
  offered.

Verified: `TestImportUsage`, `TestImportUsageHold`,
`TestImportUsageHoldEndsAtReset`, `TestImportUsageRejectsWritingNothing`
(12 cases), `TestIsReadings`, `TestLimitsImportReadings`; mutation checks
(14 of 14 caught).

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

## 7. Extra usage

- `extra_usage` of the usage answer (claude-swap's `oauth.py` spend): when
  `is_enabled` and `used_credits`, `monthly_limit` and `utilization` are all
  given (cents; a null cap is unlimited and shows nothing), it becomes a
  limit of kind `extra_usage`: "Extra usage", its share as a meter, and
  "12.34 of 50.00 USD" where a reset would be. "extra N%" joins the menu bar
  row. Never weighed by auto-switch, pace, the menu bar title or polling.

Verified: `TestParseUsageExtraUsage`, `TestLimitsExtraUsage`; mutation
checks (7 of 7 caught).

## 8. Setup-token accounts and the header probe

- "Add Token…" (toolbar): a setup-token (`sk-ant-oat…`, from `claude
  setup-token`), an optional email as its label (else
  `setup-token-N@token.local`), and "Read setup-token usage". Nothing is
  asked of Anthropic. As claude-swap's `add-token`: the saved login is
  `{"claudeAiOauth":{"accessToken":…,"scopes":["user:inference"]}}`, the
  `oauthAccount` `{"emailAddress":…,"accountUuid":"",…}`; the account key is
  `token:<email>/`; adding the same label again replaces its token.
- Identity: a login with no refresh token is a setup-token's when it is a
  setup-token account's saved access token (`tokenOwner`), in `identify`
  and `whose`, before Anthropic is asked; no other tokenless login is ever
  filed under one (the refresh-token match needs a refresh token).
- Reading (`oauth.py` `request_usage_via_probe`, `usage.headerProbe`): off,
  a setup-token account shows why and nothing is asked. On, a 1-token Haiku
  `/v1/messages` request's `anthropic-ratelimit-unified-5h/7d-*` headers
  give its session and weekly windows, and a 1-token Fable request its
  `7d_oi` window. A 429 with `unified-status: rejected` reads as the window
  at 100% until Retry-After, else the reset header; a refused Haiku probe
  stops there. A setup-token is never renewed.
- Not offered for a setup-token: Sign In Again, Open in Terminal, Export
  (left out: no refresh token).

Verified: `TestAddToken`, `TestProbe`, `TestRefreshSetupToken`,
`TestSwitchSetupTokens`, `TestSwitchUnknownTokenlessLogin`,
`TestLimitsAddToken`; mutation checks (21 of 21 caught).

## 8b. Console API key accounts

- Add Token… takes a Console API key too (`sk-ant-…`, not `sk-ant-oat…`, as
  claude-swap's `looks_like_api_key`): saved raw in its park item, keyed
  `apikey:<email>/`, labelled by the email given or `api-key-N@token.local`.
  Nothing is read for it: it has no subscription limits.
- Switching to it (claude-swap's `_write_managed_credentials`): the key goes
  in Claude Code's keychain item "Claude Code"; `~/.claude.json` gets its
  last 20 characters in `customApiKeyResponses.approved`, the synthesized
  `oauthAccount`, and loses `primaryApiKey`; the login item loses
  `claudeAiOauth`. Unlike claude-swap, which deletes the whole login item,
  its MCP sign-ins are kept. Switching to an OAuth account clears the
  managed key (`removeApiKey`): the item deleted, `primaryApiKey` dropped. A
  managed key the app doesn't hold is added as an account before it is
  cleared. On a failed write the login item, the key item and
  `~/.claude.json` are put back.
- In use: the key item (else `primaryApiKey`) equal to an API key account's
  key. Auto-switch holds on it ("it has no limits"); with "Fall back on an
  API key account when no account has room" (`includeApiKeyAccounts`) it
  lands on one whenever it must move (past the line, at the limit, on a
  failover) and no subscription account has room; never for a
  consume-first or priority nudge.
- Export writes it as claude-swap does (`kind: api_key`, the key as a
  string); Import adds it, or leaves it when the same key is there.

Verified: `TestAddAPIKey`, `TestSwitchAPIKey`, `TestAPIKeyInUseByPrimaryApiKey`,
`TestSwitchAPIKeyRestores`, `TestDecideAPIKeyLastResort`,
`TestExportImportAPIKey`, `TestDropKey`, `TestLimitsAddAPIKey`; mutation
checks (21 of 21 caught).

## 9. Command line

- `claude-manager <command>` runs one command on the same accounts as the
  app and exits (package `cli`); any other start opens the app. Commands:
  `list [--json]`, `status [--json]`, `switch <email> [--json]`,
  `auto --once [--dry-run] [--json]`, `add-token <token|-> [--email E]`,
  `remove <email>`, `purge [--yes]` (asks "Are you sure you want to purge all data? [y/N]" unless --yes; removes the whole data folder and, on macOS, the menu bar LaunchAgent), `export <file> [--full]`,
  `import <file> [--hold SECONDS]` (also `import-usage`), `run <email>
  [folder] [--no-share] [--no-share-history]`, `help`.
- `list --json` is cswap's schema 1 (`number`, `email`, `organizationUuid`,
  `active`, `usageStatus`, `usageAgeSeconds`, `usage.fiveHour/sevenDay/
  scoped/spend`), plus `key` and `added`, so it is a readings file for either
  app's import. On a handled error `--json` prints `{"schemaVersion":1,
  "error":{…}}` and exits 1.
- `auto --once` is one tick whether or not auto-switch is on, with cswap's
  exit codes: 0 switched, 1 error, 2 nothing done (a dry run that would
  switch too), 3 blocked (it should move and no account has room). The
  failover count and the bar don't carry across runs: they live in memory.
- An account is named by its email, or its key when an email has two
  organizations.

Verified: `TestListJSONIsCswapsAndImports`, `TestStatusAndSwitch`,
`TestAutoOnce`, `TestAddRemovePurge`, `TestExportImportRun`, `TestReorder`,
`TestFindAmbiguous`, `TestListUnavailable`; mutation checks (13 of 13
caught).

## 9b. Aliases and folder mappings

- `alias` (claude-swap's `normalize_alias`): lower case, a letter or digit
  first, then letters, digits, `-`, `_`, `.`; not all digits; one account
  per alias. `claude-manager alias [<account> <name> | <account> --unset]`;
  an alias names the account wherever an email does. Export writes it,
  Import takes it when free.
- `map` (`mappings.py`): `accounts.json` `maps`, a full folder path to an
  account key; the folder and those below it. `claude-manager map
  [<account> [folder]]`, `unmap [folder]`; `run` with no account, or with a
  folder alone, opens the account mapped to the nearest mapped folder, and
  says so when none is. Removing an account removes its mappings.
- `version`.

Verified: `TestSetAlias`, `TestMaps`, `TestAliasTravels`, `TestAliasMapRun`;
mutation checks (10 of 10 caught, after a redundant check went).

## 11. Slot numbers

- Each added account has `slot`, the smallest number free from 1, given when
  it is added (and to accounts saved before slots). Saving its login again
  keeps it. `claude-manager list` goes by slot, and `--json`'s `number` is
  it; an account is named by its number too. `move <from> <to>` (an account
  in `<to>` takes `<from>`), `swap <a> <b>`. Export writes it as `number`;
  Import keeps an imported account's number when it is free.

Verified: `TestSlots`, `TestSlotsTravel`, `TestSlotKeptBySwitches`; mutation
checks (9 of 9 caught).

## 12. Unclaimed logins

- Before: a switch off a lapsed login in use that named no added account
  was refused ("Open Claude Code to renew it"). Now, as claude-swap's
  unclaimed stash: the login (`claudeAiOauth` only) is kept in keychain
  `claude-manager-unclaimed` under an id (the first 12 hex of its SHA-256),
  read back, listed in `accounts.json` `unclaimed` with when and why, and the
  switch goes on; if it can't be kept, nothing is switched. Any other
  failure to tell whose it is (Anthropic out of reach) still refuses.
- `claude-manager unclaimed` lists them; `--purge ID` deletes one (its
  account comes back by signing in with /login and Add Account).

Verified: `TestSwitchLapsedLiveLogin`, `TestSwitchLapsedLoginDisagreeing`,
`TestSwitchUnknownTokenlessLogin`, `TestUnclaimedKeptOrNothingSwitched`,
`TestUnclaimedOnlyForAnUnknownLogin`, `TestUnclaimed`; mutation checks (6 of
6 caught).

## 13. Linux, WSL and Windows

- Builds: `GOOS=linux` and `GOOS=windows` build and vet; the Unix-only
  process calls (`kill(pid, 0)`, process groups, inodes) sit behind build
  tags (`proc_unix.go`, `proc_windows.go`).
- Without a macOS keychain (claude-swap's Linux and Windows paths): the app's
  items are a `FileVault`, one 0600 `vault.json` in the app's folder
  (`os.UserConfigDir()/Claude Manager`); `Home.FileOnly` makes Claude Code's
  login `.credentials.json` alone (read, written, created, put back), and an
  API key `primaryApiKey` in `~/.claude.json` rather than a keychain item.
- Open in Terminal: Linux tries `$TERMINAL`, `x-terminal-emulator`,
  `gnome-terminal`, `konsole`, `xfce4-terminal`, `kitty`, `alacritty`,
  `xterm`; WSL opens Windows Terminal (`wt.exe wsl.exe -d <distro>`);
  Windows opens a console (`cmd /k`).

Verified: `TestFileVault`, `TestSwitchFileOnly` (OAuth and API key),
`TestSwitchFileOnlyRestores`, `TestSwitchFileOnlySignedOutAndStale`;
`GOOS=linux|windows go vet ./...` clean; mutation checks (7 of 7 caught).
Not verified: a run on Linux, WSL or Windows, and the terminals there.

## 14. The rest of cswap's commands

Each line of claude-swap's README "Other commands", checked against the
command line: `add [--slot] [--alias]` (the account in use, followed);
`disable`, `enable`, `threshold`, `priority` (with no account they list);
bare `switch` (the next slot after the one in use, round; `--strategy best`:
most quota left; `next-available`: the next with room; held-out accounts
skipped); `auto` in the foreground, its flags an `Override` for that run,
never saved; `list --token-status`; `run … -- <args>` forwarded to claude,
shell-quoted (`--require-session` accepted: the account in use is refused
unless Claude Code's login for it holds no usable token and one is saved: then
it opens from its profile, as cswap's `_default_login_signed_out`); `export -`, `import -`; `menubar --install-service` (a LaunchAgent
plist bootstrapped with launchctl, as cswap's; an autostart `.desktop` on
Linux; an HKCU Run value on Windows), `--uninstall-service`,
`--service-status`, `--auto-switch`, `--no-auto-switch`; `tui`, `watch`,
`menubar [--foreground]` open the app (`watch` on Limits); `export
--account` (one account), `import --force` (replaces a saved login however
old, the account in use's too), `switch <account> --force` (puts the saved
login in use even for the account in use, not keeping the old one over it).
A script checked every `cswap <command> --flag` in claude-swap's README
against the command line: only claude's own `--resume`, passed after `--`,
is not one of its flags.

Verified: `TestAddAndTune`, `TestSwitchWithoutAccount`,
`TestAutoLoopAndFlags`, `TestTokenStatusRunArgsStdio`, `TestMenubarService`,
`TestGUI`; mutation checks (16 of 16 caught). Not verified: launchctl, the
Run key and autostart for real.

## 15. Session identity and history merge

- Drift (`session_identity_drifted`): a terminal profile whose
  `.claude.json` or login names another account than the one it was opened
  as is read as "signed in to another one now", and at take-back its login
  is kept as unclaimed, never saved as the account's. Unreadable is no
  drift. This closed a bug: a `/login` to another account in that terminal
  had its login saved under the first one.
- History merge (`_merge_history_into_source`): opened with history shared,
  a profile's own `projects/` and `history.jsonl` go into `~/.claude` before
  it links them: a file `~/.claude` lacks is moved; one whose copy there
  holds every byte of it is dropped; any other clash keeps the shared copy
  and parks the profile's under `history-conflicts/<time>-<profile>/`.
  `history.jsonl` takes the lines it lacks. Not while a claude runs on the
  profile. Simpler than claude-swap's: a later profile transcript doesn't
  replace the shared one, it is parked.
- Not a gap: claude-swap's stale-profile re-seed; take-back removes a
  profile's login, so each opening after it seeds afresh.

Verified: `TestTakeBackDrifted`, `TestMergeHistory`,
`TestRunMergesProfileHistory`; mutation checks (11 of 11 caught).

## 16. Plans and error notes

- Plan (`subscription.py`): after a good read of an account, its profile
  (`/api/oauth/profile`: `rate_limit_tier`, `organization_type`,
  `subscription_status`, `plan_display_name`) is read when 6 hours old, kept
  only when its account (and organization, when known) uuid is the
  account's (`matches_slot`), else tried again in 15 minutes; the login in
  use's comes from the answer that said whose it is. `PlanLabel` is
  `plan_label` (organization type, else the server's name unless the
  tier's label begins with it, else the tier's label, else the raw tier);
  `PlanLapsed` is `is_expired` (canceled, on the free tier), shown as a bold
  orange "Free (lapsed)"; a plan read more than 12 hours ago shows its age.
  `list` shows the label; `list --json` has cswap's `plan` object.
- Error notes (`ERROR_NOTES`): an organization barring OAuth sign-in says a
  lapsed plan or its policy is why and that renewing restores it; a refused
  certificate (`x509` errors) is `ErrTLS`, with the proxy, `SSL_CERT_FILE`,
  captive portal and clock advice, not "Couldn't reach Anthropic";
  `CLAUDE_SECURESTORAGE_CONFIG_DIR` set says Claude Code keeps its login
  elsewhere (Limits footer, `list` on stderr).

Verified: `TestPlan`, `TestTLSRefused`, `TestStoreNote`, `TestRefusals`,
`TestPlanText`, `TestLimitsLapsedPlan`; mutation checks (10 of 10 caught).

## 17. Notifications, history, log, instances, JSON

- `Book.Events` (claude-swap's menu bar notifications): auto-switched,
  account quarantined, all accounts exhausted (once until there is room
  again), auto-switch failed; deduped per kind and body. main shows them as
  MyGo notifications.
- Switch history: each switch (by hand, forced, auto: when, from, to, why)
  in `accounts.json` `history`, 50 kept; "Recent switches" (5) in the menu
  bar; `claude-manager history`.
- `Book.Log` (claude-swap's `claude-swap.log`): a line per switch, failure
  and quarantine, never a token; main writes `claude-manager.log` in the
  app's folder, rotated at 1 MB, 3 kept (`internal/logfile`).
- `status` lists the running instances (`process_detection`): sessions from
  `sessions/<pid>.json` in `~/.claude` and each profile with claude-swap's
  entrypoint labels, and IDEs from `~/.claude/ide/*.lock`.
- `menubar --service-status` says when macOS was told not to start it
  (`launchctl print-disabled`).
- `--json`: weekly windows carry `expectedPct`, `aheadOfPace`,
  `projectedExhaustionAt`, `willLastToReset` (pace.py's projections); a row
  without decision-grade usage carries `lastGoodUsage`, `lastGoodFetchedAt`,
  `lastGoodAgeSeconds` and `usageRetryAt`.

Verified: `TestHistoryEventsLog`, `TestQuarantineTold`, `TestHistoryCapped`,
`TestProjection`, `TestRotates`, `TestInstances`, `TestObserve`,
`TestServiceDisabled`, `TestTrayRecentSwitches`; mutation checks (14 of 14
caught).

## 18. switcher.py's prompts, notices and refusals

Each from a method of `ClaudeAccountSwitcher` (all 176 listed by an audit):
`remove` asks `[y/N]` unless `--yes`, and says when the account is the one
in use; `add`/`add-token --slot` on a taken slot asks before moving its
account; an email naming several accounts lists them and asks for one
(`switch`, `remove`); `switch` to the account in use says "Already on" with
the `--force` hint; after a switch, the keychain-cache restart hint, and a
notice when Claude Code's login was kept as unclaimed; `list` shows
`[organization]` for a team seat or `[personal]` (`[API key]`,
`[setup-token]`), aliases, running instances, "No accounts are added yet",
and warns of two accounts holding one login (`Duplicates`); an API key
under an OAuth account's email, or a setup-token under an API key's, is
refused (`_reject_cross_kind_collision`); `list` and `auto` warn of
`autoswitch.model` names no account has; from a terminal opened as an
account (its `CLAUDE_CONFIG_DIR` a profile) every command that changes
accounts is refused (`_refuse_session_shell`).

Verified: `TestRemoveConfirms`, `TestSwitchNotices`, `TestAmbiguousAsks`,
`TestListTagsAndWarnings`, `TestSessionShellRefused`, `TestAddSlotAsks`,
`TestCrossKindRefused`, `TestDuplicates`; mutation checks (13 of 13
caught).

## 19. The TUI's actions in the app

From an item-by-item audit of `tui/`: "Add This Account" on the account in
use when the app doesn't hold it (`Book.AddInUse`, its login saved, never
renewed while in use); Add Token's slot, asking "Slot N is taken" before
moving its account; "Switch to the Best Account" (`Book.Pick("best")`, also
⌘B); a dry run (`Auto.DryRun`: "Would switch to X … (dry run)"), turned off
only after "Go live?"; "Next: …" the first three by quota left, unknown
last (`Book.Ranking`); "Log…", the last 50 decisions; a tick on each 5h and
7d meter at its auto-switch line; a refused header probe as a "Refused"
row; ⌘R refresh, ⌘B best, ⌘L auto-switch on/off, ⌘N add account. Not
ported: the TUI's captured-output dialog (the app's toasts carry the whole
error) and its vi-style navigation keys.

Verified: `TestRankingAndAddInUse`, `TestDryRun`, `TestAddThisAccount`,
`TestDryRunGoLive`, `TestNextBestLogAndShortcuts`, `TestSwitchBest`,
`TestLineTick`, `TestTokenSlotAsks`; mutation checks (14 of 14 caught).

## 20. End to end, the output dialog, the keys

- `Keychain.File` runs every `security` command on a keychain file of its
  own; `CLAUDE_MANAGER_KEYCHAIN=<file>` makes the app and its command line
  use one, never the user's; `CLAUDE_MANAGER_KEYCHAIN_GUARD=<file>` refuses
  any command not naming that file (tests set it).
- `TestE2ESwitchOnARealKeychain`: macOS's own `security` on a temporary
  keychain: an 8 KB Claude Code login (past `security -i`'s line, so the
  argv path), the app's park items, a switch A → B → A under Claude Code's
  locks, `~/.claude.json`, Refresh following the account in use, Remove.
- The built binary, run from a shell with a temporary HOME and keychain:
  `add-token` (two API keys), `switch` A → B → A (the "Claude Code" item
  read back each time), `history`, `export --account`, `remove --yes`,
  `import`, `list`, `config`, `menubar --service-status`.
- The TUI's output dialog: an action the user started that fails shows
  "Couldn't Finish" with the whole error, selectable, and Copy. Its keys:
  j/k choose an account (its edge marked), s switches to it.

- Claude Code itself (`TestClaudeCodeReadsWhatTheAppWrites`, opt in with
  `CLAUDE_MANAGER_CLAUDE_E2E=1`): the real `claude auth status`, on folders
  of the test's own with fake tokens, reads what the app wrote: after a
  switch to an OAuth account (`claude.ai`, its email from `oauthAccount`),
  to a setup-token (`claude.ai`, its label), to an API key (`api_key`, "/login
  managed key" from `primaryApiKey`), and an Open in Terminal seed. Claude
  Code 2.1.330 on 2026-11-21. A fake refresh token makes Claude Code drop
  the login after its refresh fails, as claude-swap notes.

Verified: the three above, `TestKeychainGuard`, `TestFailureDialog`,
`TestViKeys`; mutation checks (6 of 6 caught). Not verified: the default
folder's keychain items read by Claude Code (writing them would change the
user's own Claude Code), and real Anthropic answers for an OAuth switch
through the binary.

- Auto-switch freshen checks identity (`_note_token_identity`): the renewal's
  answer names its account and organization; one of another account, or of
  another organization (both known), quarantines the target like a refused
  token, and the loop moves to the next candidate. No account uuid in the
  answer is no conflict.
- `error.type` in JSON keeps Go's four kinds (network, auth, keychain, error)
  rather than claude-swap's Python class names: deliberate, a user decision.

## Not ported

Superseded by the table in `docs/roadmap.md` ("claude-swap features, ported
or not"): the menu bar, start at login, pace and per-model windows moved to
parts 5 and 6.

## Open before PR 1

- The real Add Account check (PR #9) is still owed; native switch builds on
  it. Do it with PR 1's real switch check.
- Credit claude-swap (MIT) in the README and in this spec.
