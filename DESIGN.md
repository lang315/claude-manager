---
name: Claude Manager
description: ~/.claude read like a Braun instrument, a native macOS app drawn with MyGo.
colors:
  ground: "#E3E3E1"
  panel: "#EFEFEE"
  face: "#F8F8F7"
  ink: "#2E2E2C"
  muted: "#62605A"
  rule: "#D8D5CE"
  signal-orange: "#C9481C"
  signal-orange-hover: "#B33E16"
  on-orange: "#FFFFFF"
  lamp-green: "#4E9A49"
  selection-graphite: "#6B6963"
  on-selection: "#FFFFFF"
  hue-opus: "#2A78D6"
  hue-sonnet: "#E87BA4"
  hue-haiku: "#6250D6"
  hue-fable: "#159AA8"
  ground-dark: "#191918"
  panel-dark: "#212120"
  face-dark: "#2A2A28"
  ink-dark: "#ECECEA"
  muted-dark: "#A4A29C"
  rule-dark: "#3A3936"
  signal-orange-dark: "#E2602F"
  signal-orange-hover-dark: "#EC7346"
  on-orange-dark: "#1A1A19"
  lamp-green-dark: "#6DBB66"
  selection-graphite-dark: "#5E5D59"
  on-selection-dark: "#ECECEA"
  hue-opus-dark: "#3987E5"
  hue-sonnet-dark: "#D55181"
  hue-haiku-dark: "#9085E9"
  hue-fable-dark: "#169AAB"
typography:
  title:
    fontFamily: "system-ui, -apple-system, sans-serif"
    fontSize: "24px"
    fontWeight: 700
  heading:
    fontFamily: "system-ui, -apple-system, sans-serif"
    fontSize: "15px"
    fontWeight: 700
  body:
    fontFamily: "system-ui, -apple-system, sans-serif"
    fontSize: "13px"
    fontWeight: 400
  figure:
    fontFamily: "system-ui, -apple-system, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    fontFeature: "tnum"
  meta:
    fontFamily: "system-ui, -apple-system, sans-serif"
    fontSize: "12px"
    fontWeight: 400
  legend:
    fontFamily: "system-ui, -apple-system, sans-serif"
    fontSize: "11px"
    fontWeight: 700
    letterSpacing: "0.6px"
  scale-label:
    fontFamily: "system-ui, -apple-system, sans-serif"
    fontSize: "11px"
    fontWeight: 400
  mono:
    fontFamily: "ui-monospace, monospace"
    fontSize: "13px"
    fontWeight: 400
rounded:
  swatch: "2px"
  scale: "1.5px"
  key: "7px"
  face: "12px"
  lamp: "4.5px"
spacing:
  xs: "8px"
  sm: "10px"
  md: "12px"
  lg: "16px"
  xl: "24px"
components:
  orange-key:
    backgroundColor: "{colors.signal-orange}"
    textColor: "{colors.on-orange}"
    rounded: "{rounded.key}"
    typography: "{typography.body}"
  orange-key-hover:
    backgroundColor: "{colors.signal-orange-hover}"
    textColor: "{colors.on-orange}"
  face:
    backgroundColor: "{colors.face}"
    rounded: "{rounded.face}"
  face-caption-row:
    padding: "12px 16px"
  face-row:
    padding: "8px 16px"
  dial-face:
    backgroundColor: "{colors.face}"
    rounded: "{rounded.face}"
    padding: "18px 20px"
  sidebar:
    backgroundColor: "{colors.ground}"
    width: "200px"
  legend:
    textColor: "{colors.muted}"
    typography: "{typography.legend}"
  lamp:
    backgroundColor: "{colors.lamp-green}"
    size: "9px"
    rounded: "{rounded.lamp}"
  lamp-unlit:
    backgroundColor: "{colors.rule}"
    size: "9px"
    rounded: "{rounded.lamp}"
  selected-row:
    backgroundColor: "{colors.selection-graphite}"
    textColor: "{colors.on-selection}"
  scale:
    backgroundColor: "{colors.ink}"
    rounded: "{rounded.scale}"
    height: "6px"
---

# Design System: Claude Manager

## Overview

**Creative North Star: "The Braun Instrument"**

Claude Manager reads `~/.claude` the way a Braun instrument reads a signal. It has one calibrated scale for space, indicator lamps for what is running, and one orange key for the one job that changes the disk. Matte neutral-grey faces sit on a slightly darker ground. They are printed in graphite ink and divided by hairline rules, and they carry no shadows. Colour is functional, never decorative. Signal orange means "can be cleaned, or cleans". Lamp green means "running, so protected". Everything else is grey.

Density is a working tool's density. Rows sit 8px from their rules, figures align right in tabular numerals, and captions are small tracked capitals like the printing on a panel. The app keeps MyGo's native controls (sidebar, buttons, data tables, search field, checkboxes) and restyles them only through the palette. It must read as a Mac app, not a web dashboard. The system refuses the SaaS card-and-donut dashboard and the dark terminal costume.

The app follows the system appearance. Light and dark are the same instrument in two finishes: every token has a dark sibling, and no role changes between them.

**Key Characteristics:**
- Matte faces (radius 12, 1px rule edge) on a darker panel ground; sidebar on the darkest ground.
- One calibrated scale, reused wherever sizes are compared, on one shared maximum per screen.
- Signal orange only for cleanable space and the keys that clean it.
- Lamp green only for running sessions.
- Mid graphite, never orange, for selection and checked controls.
- System sans throughout, tabular figures for every size, tracked capital legends as face captions.

## Colors

A grey instrument with two signal colours, each bound to one meaning.

### Primary
- **Signal Orange** (light `signal-orange`, dark `signal-orange-dark`): the cleanable part of the dial and of each scale, the "can be cleaned" swatch, and the keys that move things to the Trash ("Clean up…", "Move to Trash…"). Hover deepens in light and brightens in dark (`signal-orange-hover`). Disabled keys show the same orange at 40% alpha.

### Secondary
- **Lamp Green** (light `lamp-green`, dark `lamp-green-dark`): the running-session lamp and nothing else. It appears in the sidebar beside Sessions, in session rows, and in the transcript header. The light value ships darker than the contract's first-draft `#5FA35B`. The build wins.

### Neutral
- **Ground** (`ground`): the sidebar, and the darkest layer of the window.
- **Panel** (`panel`): the content area's background (the theme background), and the hover fill of a clickable row on a face.
- **Face** (`face`): the matte plates that hold every group of content, and your own turns in a transcript.
- **Graphite Ink** (`ink`): text, the dial's segments, ticks and baseline, and the kept part of each scale.
- **Muted** (`muted`): secondary text, notes, legends, and tick numbers.
- **Rule** (`rule`): hairline borders, row dividers, the scale's empty track, and the unlit lamp.
- **Selection Graphite** (`selection-graphite`, with `on-selection` text): the chosen row in a table, checked boxes, and the selected sidebar item. It is quieter than ink. Its hover mixes 20% toward ink, and pressed mixes 35% toward ink.

### Model Hues
- **Model hues** (`hue-opus`, `hue-sonnet`, `hue-haiku`, `hue-fable`, each with a `-dark` sibling): the model families in the Usage chart's stacked bars and its legend, and nowhere else.
  - Stack order from the baseline up is Opus, Sonnet, Haiku, Fable, Other. Other is `muted`.
  - A 2px gap of the face parts each segment from the one under it.
  - Labels and figures stay in ink and muted: a hue marks a bar segment or a legend swatch, never text.
  - The four steps pass the data-viz validator in both finishes, on `face` and `face-dark`: lightness band, chroma, adjacent colour-blind separation (ΔE ≥ 8), and the normal-vision floor.
  - In light, Sonnet's pink sits under 3:1 against the face. The legend names every family, and the Models tab gives the same figures as a table.
  - The hues keep clear of orange and green, so the One Meaning Rule holds.

### Limit Meters
- **Limit meter** (Limits page): MyGo's level indicator, 0 to 100.
  - Below 80% it is selection graphite.
  - From 80% it takes MyGo's Warning amber, and from 95% its Danger red.
  - Amber and red mean "near a limit", and appear nowhere else.
  - The percentage is always printed beside the meter, with "Limit reached" at 100%, so colour is never the only signal.
  - Below 100%, a weekly row used 15 points or more faster than its week has gone shows a muted "Ahead of pace" in that slot, with the on-pace share as its tooltip; never in a cycle's first day.
  - Green stays the lamp's.

### Named Rules
**The One Meaning Rule.** Orange means "can be cleaned, or cleans". Green means "running". Neither colour appears for any other reason, including selection, focus, links or decoration. The model hues mean "this model family", only in the Usage chart.

**The Graphite Choice Rule.** Selection and checked state use mid graphite, never orange. Orange stays free to mean the disk.

## Typography

**Display Font:** none. The system sans (San Francisco) carries every role.
**Body Font:** system sans, MyGo's macOS default (13px).
**Label/Mono Font:** system monospace, only for code: tool input and output folded inside a transcript, and hook commands in Config (12px there).

**Character:** One family, set like the printing on a panel. Weight and size do the work, and colour does not.

### Hierarchy
- **Title** (700, 24px): the header row's screen name or `~/.claude` path, single line.
- **Heading** (700, 15px): the session title at the top of a transcript, and the file title in a Config reader.
- **Body** (400, 13px): row names, table cells, and messages.
- **Figure** (400, 13px, `tnum`): every size and count, aligned right so columns read down. The Selected total uses it in bold.
- **Meta** (400, 12px, muted): notes, paths, timestamps, item counts, and the speaker line in a transcript (the speaker in bold).
- **Legend** (700, 11px, 0.6px tracking, uppercase, muted): a face's caption.
- **Scale label** (400, 11px): dial markers in ink, tick numbers in muted. The total at the scale's end is set at weight 600.

### Named Rules
**The Printed Caption Rule.** A legend is the face's only title. It sits in the face's caption row, with an optional muted note at the right. Never stack a heading under it, and never place it above a heading outside a face.

**The Tabular Rule.** Every size, count and total is set in tabular figures and aligned right.

## Layout

A fixed 200px sidebar on the ground, a 1px rule, then the content column. The content column has 24px horizontal and 18px vertical padding, and a 16px gap between the header and the screen. The header row holds the title with its muted summary line below (2px gap). The secondary key (Rescan) and the screen's orange key sit at the far right, 10px apart.

On the Overview, faces stack with a 16px gap: the full-width dial, then two equal faces side by side, then the full-width folder list. Inside faces, caption rows are padded 12px by 16px and list rows 8px by 16px. Row dividers are 1px rules inset 16px from the face edges. Column widths for figures are fixed so values align down a face.

Spacing uses 8, 10, 12, 16 and 24px steps. Sessions uses a resizable split view, with a list on the left and detail on the right. Config's Skills, Commands and Memory tabs use a fixed 300px list, a 1px rule, then the reader. Text truncates on a single line instead of wrapping.

## Elevation & Depth

The instrument is flat. Depth comes from three tonal layers: ground, then panel, then face. A 1px rule edges each face. Faces, rows, keys, scales and lamps carry no shadow. The lit lamp's soft ring is a 2px border of its own green at 25% alpha, not a shadow.

MyGo's floating chrome (toasts, tooltips, confirm dialogs) keeps the framework's own shadow. It is native window furniture, not part of the instrument's surfaces.

### Named Rules
**The Matte Face Rule.** App-drawn surfaces separate by tone and a hairline rule, never by shadow.

## Shapes

Faces are softly rounded plates (12px). Keys and controls use the theme radius (7px). The scale's bars round at 1.5px, legend swatches at 2px, and lamps are full circles (9px). The dial's segments are square-cornered, so they read as one bar cut by 1px gaps. A transcript message uses the face radius (12px). Your own turns are drawn as a face with a rule edge, and the other turns (Claude, Tool) sit unboxed on the panel.

## Components

### Buttons
- **Shape:** gently rounded (7px).
- **Orange key:** signal orange with `on-orange` text, and only for an action that leads to or performs a move to the Trash: "Clean up…" in the Overview header, and "Move to Trash…" at the foot of a cleanable list or beside a table (Overview, Sessions, Config, Cleanup).
- **Hover / Disabled:** hover switches to `signal-orange-hover`. Disabled is orange at 40% alpha.
- **Secondary:** MyGo's standard button (Rescan, Open Transcript), unrestyled, sitting just left of the orange key.

### Inputs / Fields
- **Search:** MyGo's native search field (160px, minimum 100px). It filters the table as you type, so it has no search button.
- **Number input:** MyGo's native number field (140px) for the Cleanup age in days.
- **Checkboxes:** native. Checked state uses selection graphite.

### Navigation
- **Sidebar:** MyGo's native sidebar on the ground colour, 200px wide, with Lucide line icons (pie-chart, message-square, sliders-horizontal, trash-2; 24-unit viewBox, stroke 2, round caps). The selected item is selection graphite. Sessions carries a green lamp while any session runs.

### Face
Matte plate (`face`, 12px radius, 1px rule edge). It opens with a caption row: legend at the left and a muted note at the right. Rows below are separated by inset rules. Clickable rows fill with `panel` on hover and show a pointer cursor.

### Lamp
A 9px circle. **Lit:** lamp green with a 2px ring of green at 25% alpha, and a tooltip saying the session cannot be moved to the Trash. **Unlit:** a flat circle in the rule colour. Lists that keep a lamp column, like the Overview Sessions face, show lit or unlit on every row. Data tables and the transcript header show the lit lamp only for a running session.

### Calibrated Scale (signature)
The instrument's one measure, reused wherever sizes are compared. All scales on a screen share one maximum, so their lengths compare.
- **Bar scale:** a 6px bar on a 1px rule track. The kept part is ink and the cleanable part is orange, placed at the bar's end. Each bar is at least 2px. In the Cleanup list every bar is wholly orange, because every group there is cleanable. In Plugins every bar is wholly ink, because plugins are not cleanable.
- **Dial:** the Overview's full-width scale (86px tall). It has one segment per top-level folder. Every segment is full ink, with a 1px gap between neighbours and no alternating tone. Each segment's cleanable part is orange at its end. The dial has an ink baseline with a tick per half step: major ticks are 8px and numbered in muted, and minor ticks are 4px. The total is set at the end in weight 600. Markers sit in up to two rows above. **A marker points at the start of its segment.** A segment with an orange part is always named. Other segments are named only when the label fits.
- **Key:** a legend row with 10px ink and orange swatches, labelled "kept" and "can be cleaned".

### Selected Row
Selection graphite with `on-selection` text, in data tables, lists and the sidebar. On the chosen row, a file's description turns `on-selection` at 75% alpha, and a container's "N items" drops its muted colour.

### Sheet
Each Config tab except Plugins is one face filling the room under the tabs. It opens with the caption row (legend and muted note), then a full-width rule, then the content. Rows inside keep their inset rules. An empty sheet says, in muted centred text with 24px padding, why it is empty and what would fill it ("No hooks. A hook runs a command at a point in a session, set under "hooks" in settings.json.").

### Property Outline
Settings and MCP servers use MyGo's native outline table with two columns, Key (260) and Value, like Xcode's property list editor. A container's value is a muted "N items". An MCP server's row sums it up as "transport · command or URL". A click on the arrow, a double click or Enter opens a row. Credentials are masked as `••••••`. This covers keys named like secrets, values shaped like known credentials, anything under an MCP server's `env` or `headers` (including settings.json's own `mcpServers`), and an `args` value that follows a secret-named flag.

### Hooks Table
MyGo's native table: Event (170), Matcher (160, muted), Command, and Timeout (80, muted, tabular, right, "N s"). Events follow the order a session meets them, and unknown events come last. An event is named only on the first row of its block, so each event reads as one block. Commands are set in system monospace at 12px, on one line, with the full command in a tooltip.

### Plugin Row
Plugins is a scroll of two faces, 16px apart: Installed plugins, then Marketplaces. A plugin row holds the name with a muted 12px "marketplace · version" below it (260 wide), an all-ink bar scale on the plugins' shared maximum, the size (tabular, 72, right), and a muted 12px "Off" when settings.json turns the plugin off. Rows run largest first. A marketplace row holds the name (260), the muted source, and the muted 12px update date (130, right).

### File List and Reader
Skills, Commands and Memory use MyGo's native list (300px, padded 6 by 8), a 1px rule, then the reader. A row is the name with a muted 12px description below it. Memory groups files under sticky project headers: 12px bold muted text, with a tabular count at the right, on the face colour. "All projects" heads `~/.claude/CLAUDE.md`. The first file is chosen by default, so the reader is never empty.

The reader is padded 16 by 24. It shows the title (Heading), then the meta line: the path shrinks and truncates first, followed by " · size · date" in tabular figures. Front-matter fields follow as rows aligned to the top: a 12px muted key (110) and its value. A rule then separates the fields from the Markdown body.

### Backups Table
MyGo's native data table, with the File column filling the width. Its footer matches Cleanup's: a muted "Selected", the bold tabular "N files · size", and the orange "Move to Trash…" at the right.

### Export and Import
On the Limits page's toolbar, after Refresh: "Export…" opens a save panel on the Desktop ("Claude accounts.json", JSON) and "Import…" an open panel (JSON, or all files: claude-swap's exports go by any name), both off while a switch runs. The save panel's message is both warnings (the file holds sign-ins, unencrypted; one Mac per account), the open panel's the second, and the toast after repeats them with what was done: "Exported 2 accounts. Left out 1: sign in again to it first.", "Imported: added 1 account, updated 1, left 1 as they were."

### Auto-Switch Row
First on the Limits page, above the account blocks: the "Auto-switch" checkbox, then a muted 12px status (why it stays, or whom it last switched to; "Checks every minute while the app is open." with nothing to report; the reason it is off outside Claude Code's default folder), then, at the right, "Auto-Switch Settings…", which opens a modal: five-hour line, an optional weekly line, cooldown in minutes, margin in points, strategy (Most left, Weekly reset first, Spend soonest reset, By priority), and Model windows: the per-model weekly windows that count too, comma separated, or "all".

### Account Block
A face per account on the Limits page, padded 14 by 16.
- **Header:** the email in bold, the plan in muted (with the organisation for a team plan), a small bold muted "In use" on the account Claude Code is signed in to, and a "⋯" menu on an added account: Switch to This Account (when it is not in use, and Claude Code runs from its default folder), Hold Out of / Put Back in Auto-Switch, Auto-Switch Line and Priority… (a modal: its own five-hour line, its priority tier), Open in Terminal… (not on the account in use; a modal: the folder with "Choose Folder…", the two sharing checkboxes, Cancel and Open, Open off until a folder is chosen), Sign In Again and Remove… (not on the account in use while the app follows its login: it would come back at the next refresh). An account open in a terminal carries a small bold muted "In a terminal" and offers neither Switch, Sign In Again nor Remove… until claude quits there. The account in use is added by itself in Claude Code's default folder.
- **Rows:** one per limit:
  - the name (150);
  - the meter, filling the width;
  - the percentage, tabular (44);
  - the muted reset time (150).
- **Footer:**
  - An added account ends with its sign-in date, muted, at 12px, then its own auto-switch settings ("Held out of auto-switch · Own line 85% · Priority 3"), muted, at 12px.
  - Figures from Pitboard end with "Read N min ago by Pitboard", muted, at 12px.
  - An account only Pitboard holds, while the app can switch, ends with "Add this account here to switch to it.", muted, at 12px.
  - A block with no figures says why, in muted text.
  - A switch under way ends the block with "Switching…", muted, at 12px.

### Menu Bar Item
A text-only menu bar item: the account in use's fullest five-hour or 7-day share, "62%" ("–" before a reading). Its menu: a disabled status row (auto-switch's last reason, else "Updated …"), a separator, one row per account, "email  5h 62% · 7d 40%", checked on the account in use and "(in a terminal)" after one open there, a click switching to it; then Auto-switch (a checkbox) and Refresh; then Open Claude Manager, Start at Login (a checkbox) and Quit Claude Manager. Closing the window hides it: the item and auto-switch keep running until Quit.

## Do's and Don'ts

### Do:
- **Do** put content groups on a matte face (12px radius, 1px rule edge) over the panel ground.
- **Do** use the calibrated scale wherever sizes are compared, with one shared maximum per screen.
- **Do** keep every dial segment full ink with 1px gaps, point each marker at its segment's start, and always name a segment that has an orange part.
- **Do** show a green lamp wherever a running session appears.
- **Do** set every size and count in tabular figures, aligned right.
- **Do** caption a face with one legend (11px, bold, 0.6px tracking, uppercase, muted).
- **Do** mark selection and checked state in mid graphite.

### Don't:
- **Don't** use orange for anything but cleanable space and the key that moves it to the Trash.
- **Don't** use green for anything but a running session's lamp.
- **Don't** put a shadow on a face, row, key or scale.
- **Don't** alternate tones between dial segments or put colour on a segment that has nothing cleanable.
- **Don't** replace MyGo's native controls with custom-drawn look-alikes; restyle through the palette.
- **Don't** add a search button beside a field that filters as you type.
- **Don't** stack a heading under a legend, or use a legend as a kicker above a heading.
