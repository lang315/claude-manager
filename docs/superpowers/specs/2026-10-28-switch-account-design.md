# Switch Account — Design

Date: 2026-10-28
Status: superseded by `2026-11-21-claude-swap-port-design.md` (the app now switches itself)

## Goal

On the Limits page, the user can put Claude Code on another of their
accounts with one click, as Pitboard does. Pitboard does the switch: the app
runs Pitboard's own command line and shows what it says.

This follows the Account Limits design
(`2026-10-28-account-limits-design.md`) and changes none of its rules. The
app still never writes `~/.claude`, `~/.claude.json` or Claude Code's
keychain item, and never touches Pitboard's parked logins. Pitboard writes
them, as it already does when the user switches from its own app.

## Why Pitboard does the switch

A safe switch needs these steps (Pitboard 0.15.1, read at commit `6784957`):

- hold Claude Code's own refresh lock (`.oauth_refresh.lock`), so that a
  running session can't write the old account's token back;
- park the outgoing login, and splice the incoming one into the live
  keychain blob, keeping its other keys;
- keep a journal, roll back a half-done write, and read the result back;
- replace `oauthAccount` in `~/.claude.json`, atomically, with a backup.

Pitboard does all of this and is installed on the user's Mac, with their
Claude Code accounts already enrolled. If the app wrote these itself, two
programs would hold the same logins, and two renewers would break each
other's refresh chain. The user chose to call Pitboard (2026-10-28).

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Who switches | `pitboard use claude/<label> --json`, run by the app | Pitboard has the locks, journal and rollback. The app writes no login |
| Which accounts can be switched to | A Claude Code account in `~/.pitboard/state.json` whose `parked` is not null, other than the account in use, while the `pitboard` command is found | Only Pitboard holds a login it can put in use. An account the app added itself but Pitboard doesn't know can't be switched to |
| The label | Pitboard's `label` from `state.json`, prefixed `claude/` | A bare label is refused when a Codex account has the same name |
| Confirm first | No | Pitboard's app doesn't ask either. A switch can be undone with another switch |
| Claude Code's login refused for good | Pitboard's error `live_login_refused` opens the dialog "Replace Claude Code's sign-in?"; **Replace** runs the command again with `--yes` | As Pitboard's app does. Without it, the user would need a terminal |
| Finding `pitboard` | `zsh -lc 'command -v pitboard'`, then `/opt/homebrew/bin/pitboard`, `/usr/local/bin/pitboard`, `/Applications/Pitboard.app/Contents/Helpers/pitboard` | The same idea as `FindClaude`. Homebrew's link points into the app bundle |
| How long | 60 s, then the command is killed | A switch asks Anthropic once at most; it takes a few seconds |
| Not in this version | Automatic switching, warm-up, enrolling the app's own accounts in Pitboard, `pitboard enroll` | Each is a later spec, if wanted |

## Pitboard's answer

With `--json`, Pitboard prints one line on standard output (its envelope,
version 1, which it changes only in a release marked Breaking):

```json
{"v":1,"command":"use","ok":true,
 "data":{"from":"Lãng1","to":"Lãng","provider":"claude","changed":true,
         "parked_at":1793149400,"adoption_ceiling_seconds":33,"adoption":{}},
 "warnings":[],"error":null}
```

- `ok` true: the switch is done, or there was nothing to do
  (`data.changed` false).
- `ok` false: `error.code` and `error.message` say why. The app shows
  `error.message` as it is. Pitboard writes it for a person.
- `data.adoption_ceiling_seconds`: how long running sessions take to follow.
  It is null or 0 when they follow only once they're started again.
- Exit status 0, 1, 2 or 3. The app reads the envelope, not the status. A
  line it can't parse, or no line at all, is the error "Pitboard gave no
  answer."
- Characters outside ASCII come escaped (`ã`), and a JSON decoder reads
  them back.

## Design

### `account` package

- `Account` gains `PitboardLabel string` (`json:"-"`). `ReadPitboard` sets
  it from `label` when `parked` is not null. `blocks()` copies it onto the
  matching block, whichever reading wins, so the account the app follows
  itself, and Pitboard too, can be switched to.
- `FindPitboard() (string, error)` looks in the places above.
  `ErrNoPitboard` means Pitboard isn't installed.
- `Book` gains `PitboardCLI func() (string, error)`. When it is nil or
  fails, nothing can be switched to.
- `(*Book).CanSwitch() bool` says whether the command was found. It looks
  once per `Blocks` call, not on every frame.
- `(*Book).Switch(ctx, label string, replace bool) (Switched, error)` runs
  `pitboard use claude/<label> --json`, plus `--yes` when `replace` is set.
  - It runs in a process group of its own, killed at the deadline.
  - It returns `Switched{To string; Changed bool; Follow time.Duration}`.
  - Errors:
    - `ErrLiveRefused` for `live_login_refused`;
    - `*SwitchError{Code, Message}` for any other refusal;
    - `ErrNoPitboard`;
    - "Pitboard gave no answer." for no envelope;
    - "Pitboard took too long." at the deadline.
  - Standard error is never shown. It may name files and labels.

### View (`view/limits.go`)

- The ⋯ menu of a block whose account has a `PitboardLabel`, isn't in use,
  and `CanSwitch` is true shows **Switch to This Account** first.
- A click sets `switching` to the block's key and runs `Switch` in `a.Go`.
  While it runs:
  - the block's footer says "Switching…";
  - Refresh, Add Account and every Switch item are off.
- **Done.** The toast says one of:
  - "Switched to <email>. Running sessions follow within N seconds.";
  - "Switched to <email>. Running sessions keep the old account until they're started again." when `Follow` is 0;
  - "<email> is already in use." when `Changed` is false.

  Then the page loads with `force`, so the block in use moves to the top.
- **`ErrLiveRefused`.** An `AlertDialog` opens: "Replace Claude Code's
  sign-in?", with the body "Claude Code's sign-in no longer works, so it
  can't be put away. Put <email>'s sign-in in its place?". It has
  **Replace** (runs `Switch` with `replace`) and **Cancel**.
- **Any other error.** A toast shows its message, and the page loads with
  `force`, because a failed switch may have changed what Pitboard knows.

## Errors and safety

- No token passes through the app. It hands Pitboard a label and reads back
  a label, an email and a number.
- The command runs with the app's environment, as the user. When
  `CLAUDE_CONFIG_DIR` is set, Pitboard reads it and switches that slot,
  which is the folder the app reads too.
- A switch never runs twice at once. Clicks are off while one runs.
- Closing the app mid-switch doesn't kill it. Pitboard finishes, or its
  journal settles the switch on its next run.

## Testing

All tests use a fake `pitboard` shell script, as `fakeClaude` does. No test
runs the real Pitboard, touches the real keychain, `~/.claude` or
`~/.pitboard`, or reaches the network.

- `account`:
  - the arguments are exact, with and without `--yes`, and a non-ASCII
    label goes through intact;
  - `ok` true is parsed, including `adoption_ceiling_seconds` as a number,
    as null, and missing;
  - `live_login_refused` gives `ErrLiveRefused`, and another code gives
    `*SwitchError` with its message;
  - empty output and output that isn't JSON give "Pitboard gave no answer.";
  - a script that sleeps is killed at a short test deadline;
  - `ReadPitboard` sets `PitboardLabel` only when parked;
  - `blocks()` carries the label onto an account the app follows too.
- `view`:
  - the menu item shows only on a block that has a label, isn't in use, and
    can switch;
  - a click shows "Switching…", turns the other items off, then shows the
    toast and reloads;
  - an error shows its message;
  - `live_login_refused` opens the dialog, and Replace runs again with
    `--yes`;
  - with no `pitboard`, there is no item.

## Real check (only when the user says so)

It switches the user's real Claude Code login:

1. `Lãng1` → `Lãng`, from the app.
2. `pitboard status` and the Limits page both show `Lãng` in use.
3. Switch back to `Lãng1`.

Evidence: the app's toast, the `pitboard log` lines, and a local screenshot
that is not committed.

## Not verified by design

- How Claude Code sessions that are already running pick up the switch.
  That is Pitboard's and Claude Code's behaviour; the app only reports
  Pitboard's figure.
- Pitboard versions other than 0.15.1.
