# Product

<!-- impeccable:product-schema 1 -->

## Platform

macos

## Users

Developers who use Claude Code every day, on their own Mac. Claude Code leaves
gigabytes in `~/.claude` (transcripts, subagent output, file history, shell
snapshots) and gives no way to see or manage it. The app is first built for its
author and will be released to other Claude Code users, so a newcomer must
understand every screen on first open without a guide.

## Product Purpose

Claude Manager reads `~/.claude` and shows what is in it: where the space goes,
every session with its transcript, and how Claude Code is configured. It lets
the user clean up safely. Success: the user sees in seconds what takes space and
frees it without fear of losing a running session or anything they cannot get
back.

## Positioning

A local, read-first tool. It never sends data anywhere, never edits Claude Code
configuration, and never deletes: everything it removes goes to the macOS Trash,
and running sessions are protected by checks made again at trash time.

## Operating Context

Four situations, all real:

- Periodic cleanup: open, see what grew, trash it, close.
- Looking back: find an old session and read its transcript.
- Alongside work: left open next to the terminal for a whole session, showing
  which sessions are running.
- Checking config: hooks, MCP servers, plugins, skills, commands, memory.

Data sizes on the author's machine: about 6.8 GB in `~/.claude`, 158 sessions
in 31 projects, transcripts up to hundreds of MB.

## Capabilities and Constraints

- Desktop app for macOS, built in Go with MyGo's native UI (package `ui`): the
  framework draws its own widgets on the GPU. No webview, no HTML or CSS.
  Styling is per element (background, border, radius, shadow, gradients, blur),
  colors come from a theme (`c.Theme()`), icons are SVG.
- Screens: Overview (sizes), Sessions (projects, sessions, transcript), Config
  (read-only), Cleanup (groups by age and orphan state).
- Deletion only through `mygo.Shell.TrashItem`; live sessions and anything
  changed in the last 10 minutes are never trashed.
- Credentials on the Config screen are always masked.
- Deferred: search, token statistics, live refresh, export, config editing (see
  `docs/roadmap.md`).

## Brand Commitments

- Name: "Claude Manager".
- It should feel at home in the Claude ecosystem. It is not an Anthropic
  product: no Anthropic or Claude logo, no official marks, nothing that claims
  or implies it is made or endorsed by Anthropic.
- Open decision: whether the name needs changing for a public release because
  "Claude" is Anthropic's trademark.

## Evidence on Hand

- Real behavior on real data, verified locally (screenshots of real data stay
  local and are never committed).
- Fixture screenshots: `SCREENSHOT_DIR=<dir> go test ./view -run TestScreenshots`.
- No users, testimonials, metrics or press exist yet; do not invent them.

## Product Principles

1. Safety is visible: the user always sees what will move to the Trash, what is
   protected, and why.
2. Read before act: viewing never changes anything; every change is explicit and
   confirmed.
3. Sizes lead: the biggest consumers come first everywhere.
4. Clear on first open: a Claude Code user new to the app understands each
   screen without docs.
5. Local only: no network, no telemetry.
