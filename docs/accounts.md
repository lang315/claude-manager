# Accounts, switching and the command line

Claude Manager's Limits page, menu bar item and `claude-manager <command>`
manage several Claude Code accounts on one Mac: their limits, switching,
auto-switch, Open in Terminal, export and import. They port
[claude-swap](https://github.com/realiti4/claude-swap) (MIT); `docs/roadmap.md`
lists every claude-swap feature and where it went.

## Tips

- **Restart after switching?** Usually not. Claude Code keeps its login in
  the keychain and caches it for about 30 seconds: a running session takes
  the new account once that lapses. Restart Claude Code (or reopen the VS
  Code extension's tab) for it at once.
- **Carry on in the same session** after a switch, or start afresh and pick
  it up with `claude --resume`. The first message on the new account may use
  extra usage as its conversation cache rebuilds.
- **One account on one Mac.** An exported login renewed on two Macs breaks:
  sign the other out of it, or stop using it there.

## Data locations

| What | Where |
|---|---|
| Accounts, their last readings, auto-switch settings, aliases, folder mappings, preferences | `~/Library/Application Support/Claude Manager/accounts.json` (0600) |
| Each added account's saved login | macOS keychain, item `claude-manager-park`, account `<accountUuid>/<orgUuid>`; the one before it in `claude-manager-park-prev` |
| A setup-token or API key account's token or key | the same `claude-manager-park` item |
| The log: switches, failures, quarantines (never a token) | `claude-manager.log` in the app's folder, 1 MB, 3 kept |
| The menu bar service's output | `menubar.log` and `menubar.err` in the app's folder |
| A renewed login the keychain refused, until it can be written | `unsaved.json` in the app's folder (0600) |
| A login in use that named no account when switched off | keychain `claude-manager-unclaimed`, by id; `claude-manager unclaimed` lists them |
| Open in Terminal profiles | `~/Library/Application Support/Claude Manager/profiles/<hash>/` |
| Claude Code's own login, as the app switches it | keychain `Claude Code-credentials` (or `~/.claude/.credentials.json`), `Claude Code` for an API key, and `~/.claude.json`'s `oauthAccount` |

On Linux, WSL and Windows there is no keychain: the app's folder is
`~/.config/Claude Manager` (`%AppData%\Claude Manager` on Windows), its
saved logins are in `vault.json` there (0600), and Claude Code's own login
is `~/.claude/.credentials.json`, its API key `~/.claude.json`'s
`primaryApiKey`.

## Command line

```
claude-manager list [--json]                    every account and its limits (cswap's list --json schema)
claude-manager status [--json]                  the account in use
claude-manager switch <account> [--json]        put Claude Code on an added account
claude-manager auto --once [--dry-run] [--json] one auto-switch check: exit 0 switched, 1 error, 2 nothing, 3 blocked
claude-manager add-token <token|-> [--email E]  a setup-token or a Console API key
claude-manager remove <account> | purge [--yes]   purge asks first; --yes for scripts
claude-manager export <file> [--full] | import <file> [--hold SECONDS]
claude-manager run [<account>] [folder] [--no-share] [--no-share-history | --share-history]
claude-manager alias | map | unmap | move | swap | unclaimed | config | upgrade | version
claude-manager add [--slot N] [--alias A]       add the account Claude Code is signed in to
claude-manager disable | enable | threshold | priority <account> ...
claude-manager switch [--strategy rotate|best|next-available]   with no account: the next
claude-manager auto [--threshold P] [--weekly-threshold P] [--model M] [--strategy S] [--interval S]
                                                every interval until stopped; the flags for that run only
claude-manager list --token-status
claude-manager run <account> [folder] -- <claude args>
claude-manager export - | import -              stdout, stdin
claude-manager export <file> --account <account> | import <file> --force | switch <account> --force
claude-manager menubar --install-service | --uninstall-service | --service-status | --auto-switch | --no-auto-switch
claude-manager tui | watch | menubar [--foreground]   open the app
claude-manager <command> --debug                each request made to Anthropic, never a token
```

An account is named by its slot number, its email, its alias, or its key.
`claude-manager move <from> <to>` renumbers one (swapping when `<to>` is
taken); `swap <a> <b>` exchanges two.

## Settings

`claude-manager config` lists them, with claude-swap's names:
`autoswitch.threshold`, `weeklyThreshold`, `intervalSeconds`,
`cooldownSeconds`, `hysteresisPct`, `strategy`, `includeApiKeyAccounts`,
`unhealthyTicks`, `model`, `enabled`; `ui.theme` (auto, light, dark; also in
the menu bar); `run.shareHistory`; `updates.check`; `usage.headerProbe`.

## Uninstall

1. Quit the app.
2. `claude-manager purge` (it asks; `--yes` skips the question) removes every
   added account and its keychain items, the app's whole folder
   (`~/Library/Application Support/Claude Manager`: logs, memos, profiles)
   and the menu bar's start-at-login service. It doesn't touch Claude Code's
   own login.
3. Delete the app.
