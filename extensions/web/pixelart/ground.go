package pixelart

import "image"

// groundStrength is how much of the wood's opacity the ground keeps, in percent: the page's ground stays quieter
// than the sheet that lies on it.
const groundStrength = 30

// Ground is the tile under the page: the wood's drawing, transposed, boards of 32 with seams, grain and
// dither, no board ends, every alpha at 30 percent of the wood's. It is 160 by 96 pixels, and it wraps.
func Ground() *image.NRGBA {
	v := boards{width: 96, height: 160, board: 32, seed: 9, noise: 4}.draw()
	img := image.NewNRGBA(image.Rect(0, 0, 160, 96))
	for y := range 96 {
		for x := range 160 {
			c := v.NRGBAAt(y, x) // the transpose: the boards then run along x
			c.A = uint8(int(c.A) * groundStrength / 100)
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}
