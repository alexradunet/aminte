package pixelart

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestSpritesAreDeterministicAndHaveTheirSize(t *testing.T) {
	a, b := Sprites(), Sprites()
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("two calls of Sprites draw different pixels")
	}
	if got, want := a.Bounds(), image.Rect(0, 0, 112, 16); got != want {
		t.Errorf("bounds %v, want %v", got, want)
	}
}

// One known pixel of each sprite, and the empty ones around it. The cells are read by position, as app.css reads them.
func TestEachSpriteHasItsKnownPixels(t *testing.T) {
	img := Sprites()
	ink := color.NRGBA{0x2c, 0x20, 0x12, 0xff}
	none := color.NRGBA{}
	at := func(cell, x, y int) color.NRGBA { return img.NRGBAAt(cell*CellSize+x, y) }
	for _, c := range []struct {
		what string
		cell int
		x, y int
		want color.NRGBA
	}{
		{"the seal's centre is the light red", CellSeal, 2, 2, color.NRGBA{0xc4, 0x55, 0x3f, 0xff}},
		{"the seal's body is the red", CellSeal, 1, 1, color.NRGBA{0x8f, 0x2a, 0x1c, 0xff}},
		{"the seal's corner is empty", CellSeal, 0, 0, none},
		{"the frown's mouth is ink", CellFace + 0, 2, 2, ink},
		{"the face 3 bottom row is ink", CellFace + 2, 0, 4, ink},
		{"the face 3 bottom row is ink to the end", CellFace + 2, 4, 4, ink},
		{"the face 3 has no mouth above", CellFace + 2, 2, 3, none},
		{"the grin's mouth is ink", CellFace + 4, 2, 2, ink},
		{"the inkwell's top row has ink at 4", CellInkwell, 4, 0, ink},
		{"the inkwell's top row has ink at 7", CellInkwell, 7, 0, ink},
		{"the inkwell's top row is empty at 3", CellInkwell, 3, 0, none},
		{"the quill's tip is ink", CellInkwell, 8, 11, ink},
		{"the cell after a drawing is empty beyond it", CellSeal, 5, 0, none},
	} {
		if got := at(c.cell, c.x, c.y); got != c.want {
			t.Errorf("%s: pixel (%d,%d) of cell %d is %v, want %v", c.what, c.x, c.y, c.cell, got, c.want)
		}
	}
}
