# Config: open files elsewhere

Short design (a small change), agreed in conversation on 2026-11-23.

## Intent

- Roadmap v3 asked for editing `CLAUDE.md` and memory, toggling plugins and
  skills, and restoring a `settings.json.bak*` inside the app. The user chose
  the cheap path: the app stays read-only (`PRODUCT.md`: "never edits Claude
  Code configuration") and hands each file to the tools that already edit it.
- A "Remove" for accounts only Pitboard follows was built here, then
  reverted: the app stops reading Pitboard instead (feat/no-pitboard).

## Goals

- Skills, Commands and Memory: "Open" (the default app for the file) and
  "Show in Finder" beside the file shown.
- Backups: "Show in Finder" for the one backup selected; restoring is a copy the
  user makes there.

## Non-goals

- No editor in the app, no writes to `settings.json`: plugins are toggled with
  `/plugin` or `claude plugin enable|disable`, skills with `/skills`
  (`skillOverrides`).

## Approaches

1. In-app editing (TextArea, backup-first settings writer): reverses
   `PRODUCT.md` and needs the whole v3 safe-writer first. Rejected.
2. Open / Show in Finder: about ten lines, keeps the app read-only. Chosen.

## Design

- `App.OpenPath` (wired to `mygo.Shell.OpenPath`) beside `App.Reveal`; a
  failure shows as the app's toast.

## Plan

1. Config buttons: test in `TestConfigDocsAndBackups`, then code.
2. `go vet ./... && go test ./...`.
