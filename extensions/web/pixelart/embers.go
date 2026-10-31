package pixelart

import (
	"image"
	"image/color"
	"math/rand/v2"
)

// emberSparks is how many sparks the tile holds. The sparks are few, so the tile reads as air with a little fire in it.
const emberSparks = 12

// Embers is the tile of the embers ground: transparent, with sparks of 1 or 2 pixels in gold and ember, placed by a
// seeded generator so the drawing is the same on every run. It is 96 by 96 pixels, and it wraps: a spark that
// reaches an edge goes on at the other, so the tile can slide by its own height without a seam.
func Embers() *image.NRGBA {
	const size = 96
	gold, ember := color.NRGBA{0xf2, 0xc1, 0x4e, 0xff}, color.NRGBA{0xff, 0x8c, 0x2e, 0xff}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	rnd := rand.New(rand.NewPCG(153, 153))
	for range emberSparks {
		x, y, side := rnd.IntN(size), rnd.IntN(size), 1+rnd.IntN(2)
		c := gold
		if rnd.IntN(2) == 0 {
			c = ember
		}
		for dy := range side {
			for dx := range side {
				img.SetNRGBA((x+dx)%size, (y+dy)%size, c)
			}
		}
	}
	return img
}
