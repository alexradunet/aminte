package pixelart

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestIconsAreDeterministicAndHaveTheirSize(t *testing.T) {
	a, b := Icons(), Icons()
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("two calls of Icons draw different pixels")
	}
	if got, want := a.Bounds(), image.Rect(0, 0, 240, 16); got != want {
		t.Errorf("bounds %v, want %v", got, want)
	}
}

// inkBounds gives the first and the last column and row of the ink of a drawing. It is a loop of its own, so the test
// does not use the helper of the sheet.
func inkBounds(d drawing) (x0, y0, x1, y1 int) {
	x0, y0, x1, y1 = IconSize, IconSize, -1, -1
	for y, row := range d {
		for x := 0; x < len(row); x++ {
			if row[x] != 'X' {
				continue
			}
			x0, x1 = min(x0, x), max(x1, x)
			y0, y1 = min(y0, y), max(y1, y)
		}
	}
	return
}

// Every drawing is in the middle of its box: the margin at the left and at the top is half of the free space,
// rounded down, so an odd free space leaves the extra pixel at the right and at the bottom. No ink is outside the box.
func TestEachIconIsCentredInItsBox(t *testing.T) {
	img := Icons()
	for cell, name := range IconNames {
		x0, y0, x1, y1 := inkBounds(glyphs[name])
		w, h := x1-x0+1, y1-y0+1
		wantLeft, wantTop := (IconSize-w)/2, (IconSize-h)/2
		gx0, gy0, gx1, gy1 := CellSize, CellSize, -1, -1
		for y := 0; y < CellSize; y++ {
			for x := 0; x < CellSize; x++ {
				if img.NRGBAAt(cell*CellSize+x, y).A == 0 {
					continue
				}
				if x >= IconSize || y >= IconSize {
					t.Errorf("%s has ink at (%d,%d), outside the %d by %d box", name, x, y, IconSize, IconSize)
				}
				gx0, gx1 = min(gx0, x), max(gx1, x)
				gy0, gy1 = min(gy0, y), max(gy1, y)
			}
		}
		if gx0 != wantLeft || gy0 != wantTop {
			t.Errorf("%s (%d by %d) has margins left %d, top %d; want left %d, top %d", name, w, h, gx0, gy0, wantLeft, wantTop)
		}
		if gx1-gx0+1 != w || gy1-gy0+1 != h {
			t.Errorf("%s is %d by %d on the sheet, want %d by %d", name, gx1-gx0+1, gy1-gy0+1, w, h)
		}
	}
}

// Every glyph is 10 by 10 characters, has ink, and the sheet has one colour only.
func TestEachIconIsATenByTenGlyphInOneColour(t *testing.T) {
	img := Icons()
	for cell, name := range IconNames {
		d, ok := glyphs[name]
		x0, y0, x1, y1 := inkBounds(d)
		dx, dy := (IconSize-(x1-x0+1))/2-x0, (IconSize-(y1-y0+1))/2-y0
		if !ok || len(d) != IconSize {
			t.Fatalf("%s: %d rows, want %d", name, len(d), IconSize)
		}
		ink := 0
		inkOnSheet := 0
		for y := 0; y < CellSize; y++ {
			for x := 0; x < CellSize; x++ {
				if img.NRGBAAt(cell*CellSize+x, y) != (color.NRGBA{}) {
					inkOnSheet++
				}
			}
		}
		for y, row := range d {
			if len(row) != IconSize {
				t.Errorf("%s row %d is %d wide, want %d", name, y, len(row), IconSize)
			}
			for x := 0; x < len(row); x++ {
				got := img.NRGBAAt(cell*CellSize+x+dx, y+dy)
				if row[x] == 'X' {
					ink++
					if got != iconInk {
						t.Errorf("%s (%d,%d) is %v, want the ink", name, x, y, got)
					}
				} else if got != (color.NRGBA{}) {
					t.Errorf("%s (%d,%d) is %v, want empty", name, x, y, got)
				}
			}
		}
		if ink == 0 {
			t.Errorf("%s has no ink", name)
		}
		if inkOnSheet != ink {
			t.Errorf("%s has %d inked pixels on the sheet, want %d", name, inkOnSheet, ink)
		}
	}
	if len(glyphs) != len(IconNames) {
		t.Errorf("%d drawings for %d names", len(glyphs), len(IconNames))
	}
}

func TestIconCellIsTheIndexOfTheName(t *testing.T) {
	for want, name := range []string{"today", "calendar", "browse", "people", "files", "metrics", "search",
		"new", "more", "image", "video", "audio", "week", "chat"} {
		if got, ok := IconCell(name); !ok || got != want {
			t.Errorf("IconCell(%q) = %d, %v; want %d", name, got, ok, want)
		}
	}
	if _, ok := IconCell("nope"); ok {
		t.Error("IconCell answers for a name that is not a glyph")
	}
	// Known pixels, read by position as app.css reads them: the top of the cross of "new" (10 by 8, one row down)
	// and the dots of "more" (10 by 2, already in the middle).
	img := Icons()
	new, _ := IconCell("new")
	more, _ := IconCell("more")
	if img.NRGBAAt(new*CellSize+4, 1) != iconInk || img.NRGBAAt(new*CellSize+3, 1).A != 0 {
		t.Error("the cross of new is not where its drawing puts it")
	}
	if img.NRGBAAt(more*CellSize+9, 5) != iconInk || img.NRGBAAt(more*CellSize+2, 5).A != 0 {
		t.Error("the dots of more are not where their drawing puts them")
	}
}
