---
version: 1
slug: "view-app-go"
primary_target: "view/app.go"
related_targets: ["view/overview.go","view/sessions.go","view/transcript.go","view/config.go","view/cleanup.go"]
---

# Surface: Claude Manager app (all screens)

Scope: the whole window — Overview (home), Sessions + Transcript, Config, Cleanup. Mode: Operate.

Audience/job: Claude Code users on macOS, newcomers included; see where ~/.claude's space goes, which sessions are running, read old transcripts, check config, and clean up safely to the Trash. Opened for periodic cleanup, looking back, alongside work all day, and config checks. Follows the system's light/dark appearance.

Must not feel like: a generic SaaS dashboard, a non-Mac app, or decorated. Controls stay MyGo's native-looking ones.

## Direction contract

THESIS: ~/.claude read like a Braun instrument: one calibrated scale for space, indicator lamps for what is running, one orange key for the one job that changes the disk. It refuses the SaaS card-and-donut dashboard and the dark terminal costume.

OWN-WORLD: Matte neutral-grey faces on a slightly darker panel ground, graphite ink, hairline rules, no shadows. Signal orange only for what can be cleaned and the key that cleans it; lamp green only for running sessions. Light: ground #E3E3E1, panel #EFEFEE, face #F8F8F7, ink #2E2E2C, muted #62605A, rule #D8D5CE, orange #C9481C, green #5FA35B. Dark: ground #191918, panel #212120, face #2A2A28, ink #ECECEA, muted #A4A29C, rule #3A3936, orange #E2602F, green #6DBB66. System sans, tabular figures, small tracked uppercase legends like printed panel captions. Faces radius 12, keys radius 7.

STORY: The user opens the app and reads the scale: how big ~/.claude is, what fills it, how much is orange. Lamps show what is running and therefore protected. One orange key, Clean up, leads to a reviewed, confirmed move to the Trash.

FIRST VIEWPORT: Sidebar (Overview, Sessions with a green lamp while any session runs, Config, Cleanup). Header row: "~/.claude" title, "6.8 GB · 3 sessions running" summary, Rescan key, orange "Clean up…" key at the far right. Full-width dial face: legend caps, calibrated scale with a tick per GB, one segment per top-level folder (graphite, cleanable ones orange), labelled markers above, numbers below. Below, two faces side by side: Sessions (running first with green lamps, then most recent) and Can be cleaned (group checkboxes, counts, sizes, Selected total, orange Move to Trash).

FORM: Braun Instrument — position 1 on my ordered list (IMPECCABLE'S PICK). seed:1e1a89fba4e096c7. Signature move: the calibrated scale, reused wherever sizes are compared (Overview dial, Cleanup groups, folder list), always on one shared scale per screen; plus indicator lamps for running sessions everywhere they appear.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Unresolved
- Whether the name "Claude Manager" changes for a public release (PRODUCT.md).
