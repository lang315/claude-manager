package view

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// opaque counts the fully opaque pixels in each bar: top (5h) and bottom (7d).
func opaque(t *testing.T, b []byte) (top, bottom int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 32, 32) {
		t.Fatalf("bounds %v", img.Bounds())
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if r != 0 || g != 0 || bl != 0 {
				t.Fatalf("(%d,%d) is not black: a template image is black and alpha", x, y)
			}
			if a == 0xffff {
				if y < 16 {
					top++
				} else {
					bottom++
				}
			}
		}
	}
	return top, bottom
}

func TestTrayIconBars(t *testing.T) {
	if top, bottom := opaque(t, TrayIcon(0, 0)); top != 0 || bottom != 0 {
		t.Errorf("empty: %d, %d opaque", top, bottom)
	}
	fullTop, fullBottom := opaque(t, TrayIcon(100, 100))
	if fullTop == 0 || fullTop != fullBottom {
		t.Fatalf("full: %d, %d", fullTop, fullBottom)
	}
	top, bottom := opaque(t, TrayIcon(49, 71))
	if !(0 < top && top < bottom && bottom < fullBottom) {
		t.Errorf("49/71: top %d, bottom %d, full %d", top, bottom, fullBottom)
	}
	if half, _ := opaque(t, TrayIcon(50, 0)); half*2 != fullTop {
		t.Errorf("50%%: %d of %d", half, fullTop)
	}
	if !bytes.Equal(TrayIcon(150, -5), TrayIcon(100, 0)) {
		t.Error("shares out of 0–100 are not clamped")
	}
}

// The menu bar is set again after every redraw: an unchanged icon is not
// drawn and encoded again.
func TestTrayIconCached(t *testing.T) {
	a, _, _, _ := newApp(t)
	first := a.TrayIcon()
	if again := a.TrayIcon(); &again[0] != &first[0] {
		t.Error("the same icon was encoded again")
	}
}
