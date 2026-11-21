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
| A login in use that named no account when switched off | keychain `claude-manager-unclaimed`, by id; `claude-manager unclaimed` lists them |
| Open in Terminal profiles | `~/Library/Application Support/Claude Manager/profiles/<hash>/` |
| Claude Code's own login, as the app switches it | keychain `Claude Code-credentials` (or `~/.claude/.credentials.json`), `Claude Code` for an API key, and `~/.claude.json`'s `oauthAccount` |

## Command line

```
claude-manager list [--json]                    every account and its limits (cswap's list --json schema)
claude-manager status [--json]                  the account in use
claude-manager switch <account> [--json]        put Claude Code on an added account
claude-manager auto --once [--dry-run] [--json] one auto-switch check: exit 0 switched, 1 error, 2 nothing, 3 blocked
claude-manager add-token <token|-> [--email E]  a setup-token or a Console API key
claude-manager remove <account> | purge --yes
claude-manager export <file> [--full] | import <file> [--hold SECONDS]
claude-manager run [<account>] [folder] [--no-share] [--no-share-history | --share-history]
claude-manager alias | map | unmap | config | upgrade | version
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

1. Turn Start at Login off (menu bar item) and quit the app.
2. `claude-manager purge --yes` removes every added account and its keychain
   items. It doesn't touch Claude Code's own login.
3. Delete the app and `~/Library/Application Support/Claude Manager`.
