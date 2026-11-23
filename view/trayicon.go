package view

import (
	"bytes"
	"cmp"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"github.com/lang315/claude-manager/account"
)

// TrayIcon is the menu bar icon at 16 pt @2x: two bars filled by the share
// used, the 5-hour window on top and the 7-day one below. Black and alpha
// only, so macOS tints it as a template image.
func TrayIcon(five, seven float64) []byte {
	const w, h, gap = 28, 10, 4
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	x0, y0 := (32-w)/2, (32-2*h-gap)/2
	for i, share := range []float64{five, seven} {
		fill := int(min(max(share, 0), 100) / 100 * w)
		top := y0 + i*(h+gap)
		for y := top; y < top+h; y++ {
			for x := x0; x < x0+w; x++ {
				a := uint8(0x59) // the track, 35%
				if x-x0 < fill {
					a = 0xff
				}
				img.SetNRGBA(x, y, color.NRGBA{A: a})
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img) // into memory: no error
	return b.Bytes()
}

// inUse is the block of the account Claude Code is signed in to.
func (a *App) inUse() (account.Account, bool) {
	for _, b := range a.limits.blocks {
		if b.InUse {
			return b, true
		}
	}
	return account.Account{}, false
}

// shares are b's 5-hour and 7-day shares used.
func shares(b account.Account) (five, seven float64) {
	for _, x := range b.Limits {
		switch x.Kind {
		case "session", "five_hour":
			five = x.Percent
		case "weekly_all", "seven_day":
			seven = x.Percent
		}
	}
	return five, seven
}

// TrayIcon is the menu bar icon for the account in use: empty bars before a
// reading.
func (a *App) TrayIcon() []byte {
	b, _ := a.inUse()
	return TrayIcon(shares(b))
}

// TrayTip is the menu bar item's tooltip: the account in use and both of
// its windows.
func (a *App) TrayTip() string {
	b, ok := a.inUse()
	if !ok || len(b.Limits) == 0 {
		return "Claude Manager"
	}
	five, seven := shares(b)
	return fmt.Sprintf("%s · 5h %.0f%% · 7d %.0f%%", cmp.Or(b.Alias, localPart(b.Email), "Claude Code"), five, seven)
}
