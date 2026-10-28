# Account Limits — Design

Date: 2026-10-28
Status: approved in conversation, pending written-spec review

## Goal

A new **Limits** page shows how much of each Claude Code account's limits
is used, and when each resets: the five-hour session limit, the weekly
limit and each model's weekly limit. It covers the account Claude Code is
signed in to and any other account the user adds.

The idea comes from [Pitboard](https://github.com/datlechin/pitboard)
(Apache-2.0), read at commit `6784957`. This is a reimplementation in Go of
its approach, not a copy of its code.

This is a personal app: one user, their own accounts, on their own Mac, in
a private repository. If the app is ever shared with other people, revisit
Anthropic's terms first. Its Legal page says third-party developers "may
not collect, store, or intermediate Claude.ai credentials or session
tokens", and this feature stores the user's own refresh tokens.

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Scope | Limits for several accounts. No switching | The user's choice. Switching would write Claude Code's login and `~/.claude.json`, which the app never writes |
| The account in use | Read Claude Code's login, only to ask for its usage. Never store it, renew it or write it | As Pitboard does. Two holders renewing one refresh chain would break it for both. Pitboard: "Asking for usage never renews it" |
| Other accounts | Added through Claude Code's own sign-in, run in a private folder. The app keeps that login and renews it | As Pitboard's `enroll --sign-in`. The sign-in makes a login of its own, so the account in use is not touched |
| Where tokens live | The macOS login keychain, in items of the app's own, written through `/usr/bin/security` | No new dependency. Items that `security` creates trust it, so later reads do not prompt |
| Where it shows | A new sidebar page, **Limits**, under Usage | The figures belong to accounts, not to `~/.claude` |
| Not in this version | Switching, a 95% alert, automatic switching, warm-up, a status line, pace, Codex, Claude Desktop | Each is a later spec, if wanted |

## Measured (the user's machine, 2026-10-28, read-only)

- `security find-generic-password -s "Claude Code-credentials" -w`: exit 0
  in 0.04 s, with no password prompt.
- The item's account is `lang` (the user name), and its service is
  `Claude Code-credentials`.
- It holds one JSON object, `claudeAiOauth`, with these fields:
  `accessToken`, `refreshToken`, `expiresAt` (epoch ms),
  `refreshTokenExpiresAt`, `scopes`, `subscriptionType` (`max`),
  `subscriptionTypeSource` and `rateLimitTier`. It is 559 bytes.
- The scopes include `user:profile`, which the usage endpoint needs.
- The access token had 7.3 h left.
- Claude Code 2.1.312 has `claude auth login [--claudeai]`.
- No transcript and no key in `~/.claude.json` holds limit figures, so the
  endpoint is the only source without a status line.

## How Claude Code keeps its login

These are facts from Anthropic's Authentication page and from Pitboard's
source:
- On macOS, the login is a generic password with the service
  `Claude Code-credentials`.
- With `CLAUDE_CONFIG_DIR` set, the service is `Claude Code-credentials-`
  plus the first eight hex digits of the SHA-256 of that directory, as the
  variable gives it.
- When the keychain refuses a write, Claude Code keeps the login in
  `<config dir>/.credentials.json` (mode 0600) instead.
- Claude Code renews its own login only while a session uses it.

## Requests

All requests go over HTTPS, time out after 5 s, and send
`User-Agent: claude-manager/<version>`, where the version is
`mygo.json`'s. Requests to `api.anthropic.com`
also send `Authorization: Bearer <access token>` and
`anthropic-beta: oauth-2025-04-20`.

| Request | Used for |
|---|---|
| `GET https://api.anthropic.com/api/oauth/usage` | An account's limits |
| `GET https://api.anthropic.com/api/oauth/profile` | Which account and organisation a login belongs to: `account.uuid`, `account.email_address`, `organization.uuid`, `organization.name` |
| `POST https://platform.claude.com/v1/oauth/token` | Renewing an added account's login: `{"grant_type":"refresh_token","refresh_token":…,"client_id":…,"scope":"<the login's scopes, space-separated>"}`. The client id is the login's `clientId` when it has one, else Claude Code's `9d1c250a-e61b-44d9-88ed-5944d1962f5e` |

### The usage answer

```json
{
  "five_hour": {"utilization": 62, "resets_at": "2026-09-20T22:20:00.095287+00:00"},
  "seven_day": {"utilization": 48, "resets_at": "2026-09-27T02:00:00.095306+00:00"},
  "limits": [
    {"kind": "session", "percent": 62, "resets_at": "…", "scope": null},
    {"kind": "weekly_all", "percent": 48, "resets_at": "…", "scope": null},
    {"kind": "weekly_scoped", "percent": 0, "resets_at": "…",
     "scope": {"model": {"id": null, "display_name": "Fable"}}}
  ]
}
```

- **Rows.** `limits` is read when present. Each row gives a limit:
  - the kind;
  - the scope's `model.display_name`, or else its `surface.display_name`;
  - the percentage, which must be finite and ≥ 0, and is shown capped at
    100;
  - the reset time, as RFC 3339 with or without fractions.
- **Fallback.** Without `limits`, or when no row can be read, the app reads
  `five_hour` and `seven_day` instead.
- **Bad rows.** A row the app cannot read is skipped.
- **Labels:**
  - `session` and `five_hour` show as **Session (5h)**;
  - `weekly_all` and `seven_day` show as **Weekly**;
  - `weekly_scoped` shows as **Weekly · <scope>**;
  - any other kind shows its own name.

## The `account` package

The `account` package is new. It is separate from `store`, which only reads
`~/.claude`.

| File | What it holds |
|---|---|
| `vault.go` | `Vault` interface: `Get(service, acct string) ([]byte, error)`, `Put(service, acct string, secret []byte) error`, `Delete(service, acct string) error`. `Keychain` runs `/usr/bin/security`. `Put` sends `add-generic-password -U -s … -a … -X <hex>` to `security -i` on stdin, so no secret appears on an argument line. An empty `acct` matches any account, for Claude Code's items, whose account is the user name. `Memory` is the test implementation |
| `live.go` | Reads the login in use: the keychain item for the root (see above), else `<root>/.credentials.json`. Read only |
| `api.go` | `Usage`, `Profile` and `Renew`, with base URLs a test can replace |
| `limits.go` | `Limit{Kind, Scope string; Percent float64; ResetsAt time.Time}` and the parser |
| `signin.go` | Runs `claude auth login --claudeai` in a private folder, and reports its link, its request for a code, and the end of the run |
| `book.go` | The list of added accounts and the last reading of each, and `Refresh` |

### Added accounts

- **Login.** Each added account's login is one keychain item:
  - service `claude-manager-park`;
  - account `<account uuid>/<organization uuid>`;
  - holding Claude Code's `claudeAiOauth` object as Claude Code wrote it.
- **The book.** `accounts.json`, in `~/Library/Application Support/Claude Manager/`,
  lists each account with:
  - its uuids, email, organisation name and plan (`subscriptionType`);
  - the date its sign-in is good until (`refreshTokenExpiresAt`);
  - its last reading: the limits, when they were read, and the last error.

  It holds no secret. The folder is 0700 and the file is 0600, written
  atomically.
- **The same account twice.** Adding an account that is already listed
  replaces its login, as **Sign In Again** does.
- **Remove.** Remove deletes the keychain item and the entry. It does not
  revoke the login, which stays valid at Anthropic until it expires.

### Signing in

1. **Find `claude`.** Use `zsh -lc 'command -v claude'`, then
   `~/.local/bin/claude`, `/opt/homebrew/bin/claude` and
   `/usr/local/bin/claude`.
2. **Make a private folder.** Create `signin-<random>` (mode 0700) under the
   app's folder.
3. **Start the sign-in.** Run `claude auth login --claudeai` with
   `CLAUDE_CONFIG_DIR` set to that folder.
   - Its environment is `HOME`, `PATH`, `USER`, the proxy variables and
     `CLAUDE_CONFIG_DIR`, and nothing else.
   - The app reads its output. The first `https://` URL is the link. A line
     starting `Paste code here` means it waits for a code, which the app
     writes to its stdin.
4. **On exit 0:**
   - Read the login from the keychain item
     `Claude Code-credentials-<sha8(folder)>`, or from
     `<folder>/.credentials.json`.
   - Ask `profile` who it belongs to.
   - Keep the login in the account's item, then list the account in the
     book.
5. **Clean up, whatever happened:**
   - delete that temporary keychain item;
   - remove the folder;
   - kill the process if it still runs.

**Cancel and timeouts.** Cancel, or 10 minutes with no end, does the same
cleanup. When the app starts, it removes any `signin-*` folder left behind,
with its keychain item.

### Refresh

`Refresh(ctx, force bool)` runs one refresh at a time. A call that comes
while one runs waits for it, and gets its result.

1. **The login in use.**
   - Read it.
   - If its `expiresAt` has passed, send nothing: its status is "Claude
     Code's sign-in has expired".
   - Otherwise ask `profile` who it is. Remember the answer in memory,
     keyed by the SHA-256 of the access token, so a token is asked about
     once.
2. **The token for each listed account:**
   - an account that is the one in use takes the token in use;
   - an account that isn't takes its own login;
   - when its access token expires within 5 minutes, it is renewed first.
3. **Renewal.**
   - Send the refresh token, then `Put` the new tokens, with the new
     `expiresAt` = now + `expires_in`. Only then use them.
   - An answer without `refresh_token` keeps the old one.
   - If `Put` fails, the login is treated as lost ("Sign-in expired"),
     because the old refresh token may already be spent. The app does not
     retry.
   - Each account has a lock, so it is never renewed twice at once.
   - The login in use is never renewed.
4. **Usage requests.**
   - **How often.** `usage` is asked for every account at once. An account
     read in the last 3 minutes is skipped unless `force` is true.
   - **401.** An added account's login is renewed once and the request sent
     once more. For the login in use, a 401 means "Claude Code's sign-in has
     expired".
   - **429.** The account waits as long as `Retry-After` says, or 5 minutes
     without it, even when `force` is true.
5. **Saving.** Each answer replaces that account's reading. Each error is
   kept beside the last good reading. The book is saved.

**Status for each account.**
- **Ready:** the limits, and when they were read.
- **No figures**, with one of these messages:
  - Not signed in to Claude Code
  - Keychain is locked
  - Claude Code's sign-in has expired. Open Claude Code to renew it.
  - Sign-in expired
  - Anthropic's own message, for `account_on_hold`, `verification_required`
    or an organisation that refuses OAuth
- **Stale:** the last good limits, an error such as "Couldn't reach
  Anthropic" or "Rate limited", and when the limits were read.

## The Limits page

- **Sidebar.** A **Limits** item, under Usage.
- **Toolbar.** The title, **Add Account…** and **Refresh**, then
  "Updated N min ago".
- **Account blocks.**
  - **Order.** The account in use comes first, then the added accounts by
    email.
  - **Header.** The email · the plan · the organisation name for a team
    account · **In use** where it applies.
  - **One row per limit:** its label, a `ui.Meter` from 0 to 100, the
    percentage, and `resets 22:50` (a reset within 24 hours) or
    `resets Nov 3, 02:00`. At 100% the row also reads **Limit reached**.
  - **Meter levels:**

    | Use | Colour |
    |---|---|
    | under 80% | normal |
    | 80–94% | warning |
    | 95% and up | critical |

    The percentage is always printed, so colour is never the only signal.
  - **Footer, added accounts.** "Sign-in good until Nov 27". Within 3 days
    of that date it reads "Sign-in expires in 2 days".
  - **The ⋯ menu, added accounts only.** **Sign In Again** and
    **Remove…**. Remove asks first, in a `ui.AlertDialog`, and says that it
    does not revoke the sign-in.
  - **Status lines.**
    - A block with no figures shows its status message in place of the rows.
    - "Sign-in expired" offers **Sign In Again**.
    - A stale block keeps its rows and adds the error and the time the
      limits were read.
- **Add Account (`ui.Modal`).**
  - It says: "Sign in, in the browser that opens, as the account to add."
  - **Copy Link** and **Open Browser** use the link.
  - A **Code** field and **Submit Code** appear only once `claude` asks for
    a code.
  - **Cancel** stops the sign-in.
  - On success the modal closes, the new block appears and its limits are
    read at once.
  - On failure the modal shows the last line `claude` wrote, with
    **Try Again**.
- **When it refreshes.**
  - When the page opens.
  - Every 5 minutes while the page is the one shown and the app is active,
    through live refresh's `visible` set (PR #8).
  - On **Refresh**, which passes `force`.
  - The book's last readings show at once when the app starts, with their
    age.
- **Empty state.** With no login in use and no added account: "No accounts
  yet", and **Add Account…**.

## Security

- **Keychain only.** Tokens live only in the keychain:
  - in `claude-manager-park` items for added accounts;
  - in Claude Code's own item, read and never written, for the account in
    use.
- **Never written out.** No token ever goes to a file, log, toast, error
  message or screenshot. The token endpoint's answer body never goes into
  an error.
- **Where tokens go.** A token goes only to `api.anthropic.com` or
  `platform.claude.com`.
- **Through `security` only.** The app reads and writes keychain items only
  through `/usr/bin/security`, and passes secrets to it on stdin.
- **Temporary sign-in.** The sign-in folder and its keychain item live only
  as long as the sign-in.
- **What the app never touches.** It never writes `~/.claude`,
  `~/.claude.json`, or Claude Code's keychain item. It never renews Claude
  Code's login.

## Testing

- **`account` package.**
  - It uses `Memory` vaults, and `httptest` servers for `usage`, `profile`
    and `token`.
  - A fake `claude`, a script in `t.TempDir()`, prints a link, asks for a
    code and writes `.credentials.json`.
  - The keychain read in `signin.go` and `live.go` goes through the `Vault`
    interface, so tests never reach the real keychain.
- **Cases.**
  - **Parser:** the `limits` rows; the fallback; a bad row; a percentage
    above 100 or NaN; both time shapes.
  - **Renewal:** it is saved before use; a failed save means "Sign-in
    expired"; `invalid_grant`.
  - **401** leads to one renewal and one retry.
  - **429** is honoured, even with `force`.
  - **The login in use:** an expired token sends no request; it is never
    renewed.
  - **One account:** the account in use and the same added account show as
    one block, using the token in use.
  - **The 3-minute floor,** and `force` going past it.
  - **Sign-in:** the code goes to stdin; Cancel and the timeout clean up;
    leftovers are removed at start; adding the same account again replaces
    its login.
  - **The book:** files are 0600 and the folder 0700; it holds no token.
- **`view`.** Through `ui.Tester`:
  - each block state;
  - the meter levels;
  - "Limit reached";
  - the order of the blocks;
  - the Add modal's steps;
  - the Remove confirmation;
  - the refresh when the page opens, and the visible-only pacing.
- **Isolation.** No test touches the real keychain, the network or
  `~/.claude`.
- **The gate.** `gofmt -l .` prints nothing, `go vet ./...` exits 0, and
  `CGO_ENABLED=1 go test -race ./...` exits 0.
- **On the real app, before the PR.** Read the limits of the account in use
  once, which sends the user's token to `api.anthropic.com` (the user has
  agreed). The user adds a second account themselves, in their browser.
  Screenshots of real accounts stay local.

## Out of scope

- Switching accounts, and anything that writes Claude Code's login.
- Alerts, automatic switching, warm-up, a status line, pace and run-out
  times.
- Codex, Claude Desktop, Linux and Windows.
- Revoking a login on Remove.
