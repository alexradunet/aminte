package pixelart

import (
	"image"
	"image/color"
)

// The sprite sheet is one row of square cells. app.css shows a cell by its position: the cell n is at x = n * CellSize
// pixels of the sheet. Each drawing is at the top-left corner of its cell.
const (
	// CellSize is the width and the height of one cell, in pixels of the sheet.
	CellSize = 16
	// CellSeal is the cell of the wax seal on a sheet (5 by 5 pixels).
	CellSeal = 0
	// CellFace is the cell of the face of the mood 1, the lowest. The face of the mood n is the cell CellFace+n-1,
	// which is the cell n.
	CellFace = 1
	// CellInkwell is the cell of the inkwell of an empty list (12 by 12 pixels).
	CellInkwell = 6
	// Cells is the count of cells in the sheet.
	Cells = 7
)

// The colours of the sheet. A sprite is a picture, not a shade: its colours do not follow the colour scheme. The ink
// is the fixed ink of the design system (--ink in base.css).
var palette = map[byte]color.NRGBA{
	'X': {0x2c, 0x20, 0x12, 0xff}, // ink
	'R': {0x8f, 0x2a, 0x1c, 0xff}, // the seal's red
	'L': {0xc4, 0x55, 0x3f, 0xff}, // the seal's light
}

// A drawing is rows of characters, one pixel each. A dot is empty, a letter is a colour of the palette.
type drawing []string

var seal = drawing{
	".RRR.",
	"RRRRR",
	"RRLRR",
	"RRRRR",
	".RRR.",
}

// faces are the faces of the moods 1 to 5, from a frown to a grin.
var faces = [5]drawing{
	{"X...X", ".X.X.", "..X..", ".XXX.", "X...X"},
	{"X...X", ".....", ".....", ".XXX.", "X...X"},
	{"X...X", ".....", ".....", ".....", "XXXXX"},
	{"X...X", ".....", ".....", "X...X", ".XXX."},
	{"XX.XX", ".....", "XXXXX", "X...X", ".XXX."},
}

var inkwell = drawing{
	"....XXXX....",
	"....X..X....",
	"..XXXXXXXX..",
	"..X......X..",
	"..X......X..",
	"..X.XXXX.X..",
	"..X.XXXX.X..",
	"..X.XXXX.X..",
	"..XXXXXXXX..",
	"..........XX",
	".........XX.",
	"........XX..",
}

// Sprites is the sheet of the small drawings: Cells cells of CellSize by CellSize pixels in one row, with the seal,
// the five faces and the inkwell. The rest is transparent.
func Sprites() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, Cells*CellSize, CellSize))
	cells := map[int]drawing{CellSeal: seal, CellInkwell: inkwell}
	for i, f := range faces {
		cells[CellFace+i] = f
	}
	for cell, d := range cells {
		for y, row := range d {
			for x := 0; x < len(row); x++ {
				if c, ok := palette[row[x]]; ok {
					img.SetNRGBA(cell*CellSize+x, y, c)
				}
			}
		}
	}
	return img
}
