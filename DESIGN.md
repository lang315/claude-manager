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

### Named Rules
**The One Meaning Rule.** Orange means "can be cleaned, or cleans". Green means "running". Neither colour appears for any other reason, including selection, focus, links or decoration.

**The Graphite Choice Rule.** Selection and checked state use mid graphite, never orange. Orange stays free to mean the disk.

## Typography

**Display Font:** none. The system sans (San Francisco) carries every role.
**Body Font:** system sans, MyGo's macOS default (13px).
**Label/Mono Font:** system monospace, only for tool input and output folded inside a transcript.

**Character:** One family, set like the printing on a panel. Weight and size do the work, and colour does not.

### Hierarchy
- **Title** (700, 24px): the header row's screen name or `~/.claude` path, single line.
- **Heading** (700, 15px): the session title at the top of a transcript.
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

Spacing uses 8, 10, 12, 16 and 24px steps. Sessions and Config use split views, with a list on the left and detail on the right. Text truncates on a single line instead of wrapping.

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
- **Bar scale:** a 6px bar on a 1px rule track. The kept part is ink and the cleanable part is orange, placed at the bar's end. Each bar is at least 2px. In the Cleanup list every bar is wholly orange, because every group there is cleanable.
- **Dial:** the Overview's full-width scale (86px tall). It has one segment per top-level folder. Every segment is full ink, with a 1px gap between neighbours and no alternating tone. Each segment's cleanable part is orange at its end. The dial has an ink baseline with a tick per half step: major ticks are 8px and numbered in muted, and minor ticks are 4px. The total is set at the end in weight 600. Markers sit in up to two rows above. **A marker points at the start of its segment.** A segment with an orange part is always named. Other segments are named only when the label fits.
- **Key:** a legend row with 10px ink and orange swatches, labelled "kept" and "can be cleaned".

### Selected Row
Selection graphite with `on-selection` text, in data tables and the sidebar.

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
