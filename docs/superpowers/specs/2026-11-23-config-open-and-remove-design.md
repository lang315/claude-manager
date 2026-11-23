# Config: open files elsewhere; Limits: remove a Pitboard-only account

Short design (a small change), agreed in conversation on 2026-11-23.

## Intent

- Roadmap v3 asked for editing `CLAUDE.md` and memory, toggling plugins and
  skills, and restoring a `settings.json.bak*` inside the app. The user chose
  the cheap path: the app stays read-only (`PRODUCT.md`: "never edits Claude
  Code configuration") and hands each file to the tools that already edit it.
- On Limits, an account the user removed came back as a card because Pitboard
  still follows it. The user wants it gone from this app, without touching
  Pitboard, since the app will move off Pitboard later.

## Goals

- Skills, Commands and Memory: "Open" (the default app for the file) and
  "Show in Finder" beside the file shown.
- Backups: "Show in Finder" for the one backup selected; restoring is a copy the
  user makes there.
- Limits: "Remove…" on an account only Pitboard follows; any removed account
  stays out of the list while only Pitboard follows it.

## Non-goals

- No editor in the app, no writes to `settings.json`: plugins are toggled with
  `/plugin` or `claude plugin enable|disable`, skills with `/skills`
  (`skillOverrides`).
- Pitboard's files and logins are never changed by Remove.

## Approaches

1. In-app editing (TextArea, backup-first settings writer): reverses
   `PRODUCT.md` and needs the whole v3 safe-writer first. Rejected.
2. Open / Show in Finder: about ten lines, keeps the app read-only. Chosen.

For Remove: forget the account in Pitboard (`pitboard forget`), hide it in the
app, or do nothing in code. The user chose to drop it in this app alone.

## Design

- `App.OpenPath` (wired to `mygo.Shell.OpenPath`) beside `App.Reveal`; a
  failure shows as the app's toast.
- `accounts.json` gains `hidden`: account keys removed here. `Book.Remove` adds
  the key; for an account only Pitboard follows it does nothing else. The
  Pitboard merge leaves out a hidden key unless the account is in use or added
  again here.

## Plan

1. Limits remove: test (`TestRemoveHidesPitboardOnly`, `TestLimitsShowsPitboardAccounts`), then code.
2. Config buttons: test in `TestConfigDocsAndBackups`, then code.
3. `go vet ./... && go test ./...`.
