package pixelart

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestEmbersIsDeterministicAndHasItsSize(t *testing.T) {
	a, b := Embers(), Embers()
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("two calls of Embers draw different pixels")
	}
	if got, want := a.Bounds(), image.Rect(0, 0, 96, 96); got != want {
		t.Errorf("bounds %v, want %v", got, want)
	}
}

// The sparks are the only opaque pixels, they are in the two colours, and there are few of them: twelve sparks of 1
// or 2 pixels square are at most 48 pixels. The drawing wraps, so a spark at an edge loses no pixel.
func TestEmbersHasSparksOfTwoColoursOnly(t *testing.T) {
	gold, ember := color.NRGBA{0xf2, 0xc1, 0x4e, 0xff}, color.NRGBA{0xff, 0x8c, 0x2e, 0xff}
	img := Embers()
	golds, embers := 0, 0
	for y := range 96 {
		for x := range 96 {
			switch c := img.NRGBAAt(x, y); c {
			case gold:
				golds++
			case ember:
				embers++
			case color.NRGBA{}:
			default:
				t.Fatalf("pixel (%d,%d) is %v: neither a spark nor clear", x, y, c)
			}
		}
	}
	if n := golds + embers; n < 8 || n > 48 {
		t.Errorf("%d spark pixels, want 8 to 48", n)
	}
	if golds == 0 || embers == 0 {
		t.Errorf("%d gold and %d ember pixels, want both colours", golds, embers)
	}
}
