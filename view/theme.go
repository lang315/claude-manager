package view

import "github.com/egoist/mygo/ui"

// palette is the instrument's colours: matte grey faces, graphite ink, orange
// only for what can be cleaned and the key that cleans it, green only for a
// running session.
type palette struct {
	ground, panel, face, ink, muted, rule, orange, orangeHover, onOrange, green ui.Color
	// sel marks the choice: a mid graphite, quieter than ink, with selText on it.
	sel, selText ui.Color
}

var (
	lightPalette = palette{
		ground: ui.Hex("#E3E3E1"), panel: ui.Hex("#EFEFEE"), face: ui.Hex("#F8F8F7"),
		ink: ui.Hex("#2E2E2C"), muted: ui.Hex("#62605A"), rule: ui.Hex("#D8D5CE"),
		orange: ui.Hex("#C9481C"), orangeHover: ui.Hex("#B33E16"), onOrange: ui.Hex("#FFFFFF"),
		green: ui.Hex("#4E9A49"),
		sel:   ui.Hex("#6B6963"), selText: ui.Hex("#FFFFFF"),
	}
	darkPalette = palette{
		ground: ui.Hex("#191918"), panel: ui.Hex("#212120"), face: ui.Hex("#2A2A28"),
		ink: ui.Hex("#ECECEA"), muted: ui.Hex("#A4A29C"), rule: ui.Hex("#3A3936"),
		orange: ui.Hex("#E2602F"), orangeHover: ui.Hex("#EC7346"), onOrange: ui.Hex("#1A1A19"),
		green: ui.Hex("#6DBB66"),
		sel:   ui.Hex("#5E5D59"), selText: ui.Hex("#ECECEA"),
	}
)

// paletteOf returns the palette of the frame's appearance, for code that
// draws without being handed one (table cells).
func paletteOf(c *ui.Context) *palette {
	if c.Theme().Dark {
		return &darkPalette
	}
	return &lightPalette
}

// applyTheme sets the frame's theme from the palette of the system's
// appearance and returns that palette.
func applyTheme(c *ui.Context) *palette {
	t := *c.Theme()
	p := paletteOf(c)
	t.Background = p.panel
	t.Text, t.TextMuted, t.Border = p.ink, p.muted, p.rule
	// Mid graphite, not orange, marks the choice and checked boxes: orange
	// is kept for what can be cleaned.
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = p.sel, p.sel.Mix(p.ink, 0.2), p.sel.Mix(p.ink, 0.35), p.selText
	t.Success = p.green
	t.Radius = 7
	c.SetTheme(&t)
	return p
}

// face is the instrument's panel: a matte rounded plate with a hairline edge.
func face(c *ui.Context, p *palette) ui.Element {
	return ui.Column(c).Background(p.face).Border(1, p.rule).Radius(12)
}

// legend is a face's printed caption: small tracked capitals.
func legend(c *ui.Context, p *palette, s string) ui.Element {
	return ui.Text(c, s).FontSize(11).Bold().LetterSpacing(0.6).TextColor(p.muted).SingleLine()
}

// lamp is the indicator of a running session, and says what it means.
func lamp(c *ui.Context, p *palette) ui.Element {
	return ui.Box(c).Size(9, 9).Radius(4.5).Background(p.green).Border(2, p.green.Alpha(0.25)).
		Tooltip("Running now; it can't be moved to the Trash")
}

// unlit is the lamp of a session that is not running.
func unlit(c *ui.Context, p *palette) ui.Element {
	return ui.Box(c).Size(9, 9).Radius(4.5).Background(p.rule)
}

// orangeKey is the one key that changes the disk.
func orangeKey(c *ui.Context, p *palette, label string, disabled bool) ui.Element {
	b := ui.PrimaryButton(c, label).Disabled(disabled).TextColor(p.onOrange)
	switch {
	case disabled:
		b.Background(p.orange.Alpha(0.4))
	case b.Hovered():
		b.Background(p.orangeHover)
	default:
		b.Background(p.orange)
	}
	return b
}

// scale draws value on the instrument's scale, out of most: graphite up to
// value, its orange part (what can be cleaned) at the end, on a hairline
// track. Every scale on a screen takes the same most, so lengths compare.
func scale(c *ui.Context, pal *palette, value, orange, most int64) ui.Element {
	return ui.Box(c).Height(6).Draw(func(pt *ui.Painter, r ui.Rect) {
		pt.Fill(ui.Rect{X: r.X, Y: r.Y + r.H/2 - 0.5, W: r.W, H: 1}, pal.rule, 0)
		if most <= 0 || value <= 0 {
			return
		}
		w := max(r.W*float32(value)/float32(most), 2)
		pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, pal.ink, 1.5)
		if orange > 0 {
			o := min(max(r.W*float32(min(orange, value))/float32(most), 2), w)
			pt.Fill(ui.Rect{X: r.X + w - o, Y: r.Y, W: o, H: r.H}, pal.orange, 1.5)
		}
	})
}
