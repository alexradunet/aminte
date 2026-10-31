// Package pixelart draws the small pictures that a gradient cannot be: the wood tile of the rail, the ground tile of
// the page, the sprites and the icons (docs/plans/137-pixel-wood-and-the-focus-pass.md,
// docs/plans/138-pixel-art-elsewhere.md, docs/plans/139-pixel-icons.md).
// A drawing is deterministic: the same call gives the same pixels, so a file that holds it is checked by comparing
// pixels with the drawing.
package pixelart

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
)

// The tiles are alpha shading only: shadow pixels and warm-light pixels over a transparent ground. The colour under
// the tile then shows through in both colour schemes.
const grainLines = 3 // grain lines a board

var (
	shadow = color.NRGBA{0, 0, 0, 0}
	light  = color.NRGBA{255, 214, 150, 0}
)

// boards is the drawing of a tile of vertical boards. The tile wraps at its edges.
type boards struct {
	width, height int
	board         int // the width of one board
	seed          uint64
	noise         int  // dither pixels a board, in percent of the board's area
	ends          bool // a staggered board end with two nail heads on each board
}

// Wood is the tile of the rail: 84 by 160 pixels, four vertical boards, each with a staggered end and two nail
// heads, grain lines and sparse dither. The grain wanders with a period of the tile's height, so the tile wraps.
func Wood() *image.NRGBA {
	return boards{width: 84, height: 160, board: 21, seed: 3, noise: 5, ends: true}.draw()
}

func (b boards) draw() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, b.width, b.height))
	rnd := rand.New(rand.NewPCG(b.seed, b.seed))
	for bx := 0; bx < b.width; bx += b.board {
		b.seams(img, bx)
		for range grainLines {
			b.grain(img, rnd, bx)
		}
		for range b.board * b.height * b.noise / 100 {
			x := bx + 2 + rnd.IntN(b.board-3)
			y := rnd.IntN(b.height)
			if rnd.Float64() < .6 {
				put(img, x, y, shadow, .07)
			} else {
				put(img, x, y, light, .07)
			}
		}
		if b.ends {
			b.boardEnd(img, rnd, bx)
		}
	}
	return img
}

// seams draws a shadow line at the board's left edge, a light line beside it and a soft shadow at its right.
func (b boards) seams(img *image.NRGBA, bx int) {
	for y := range b.height {
		put(img, bx, y, shadow, .5)
		put(img, bx+1, y, light, .07)
		put(img, bx+b.board-1, y, shadow, .22)
	}
}

// grain draws one grain line that wanders by a sine, with a light pixel beside it now and then.
func (b boards) grain(img *image.NRGBA, rnd *rand.Rand, bx int) {
	x0 := float64(bx + 3 + rnd.IntN(b.board-6))
	amp := 1 + 2*rnd.Float64()
	phase := 6.28 * rnd.Float64()
	k := float64(1 + rnd.IntN(2))
	inside := func(x int) bool { return bx+1 < x && x < bx+b.board-1 }
	for y := range b.height {
		// The conversions round each product before the sum: the compiler may not fuse them into one operation,
		// which differs by platform and would move a pixel.
		x := int(math.Round(x0 + float64(amp*math.Sin(float64(2*math.Pi*k*float64(y)/float64(b.height))+phase))))
		if inside(x) {
			put(img, x, y, shadow, .16)
		}
		if rnd.Float64() < .08 && inside(x+1) {
			put(img, x+1, y, light, .05)
		}
	}
}

// boardEnd draws a dark line across the board at a staggered height, with a light line under it and two nail heads.
func (b boards) boardEnd(img *image.NRGBA, rnd *rand.Rand, bx int) {
	ey := (bx*7 + rnd.IntN(b.height)) % b.height
	for x := bx + 1; x < bx+b.board; x++ {
		put(img, x, ey, shadow, .55)
		put(img, x, ey+1, light, .08)
	}
	for _, x := range []int{bx + 4, bx + b.board - 5} {
		put(img, x, ey+4, shadow, .6)
		put(img, x, ey+3, light, .18)
	}
}

// put draws a pixel of colour c with the opacity a, wrapped at the tile's edges. A stronger pixel wins over a
// weaker one; two pixels are never blended.
func put(img *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	x = (x%w + w) % w
	y = (y%h + h) % h
	alpha := uint8(a * 255)
	if alpha > img.NRGBAAt(x, y).A {
		c.A = alpha
		img.SetNRGBA(x, y, c)
	}
}
