// Package preview makes the picture a file page keeps (docs/decisions/D09-binary-files.md, the files table of
// docs/schema/schema.sql): a JPEG, upright, its long edge at most 1600 px, at most 1 MB, with no metadata. It reads
// what the standard library reads: JPEG, PNG and GIF. Inside those limits it makes a smaller picture (makeEdge,
// qualities): the contract holds every writer, and this writer chooses fewer bytes.
package preview

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"aminte/internal/photo"
)

const (
	maxEdge  = 1600    // the longest edge of a preview, in pixels: the contract's (D9), which Check holds
	maxBytes = 1 << 20 // the size of a preview (files_preview)
	// makeEdge is the long edge Make scales to: twice the size the browser draws a picture in a page, so it is sharp
	// on a dense screen, at about half the bytes of maxEdge (docs/plans/128-smaller-pictures.md).
	makeEdge = 1280
	// The most memory a decoded picture may need, in bytes: 4 bytes a pixel is 120 MP (a 108 MP phone photo). A 16-bit
	// PNG needs 8 bytes a pixel, so its limit is 60 MP. A crafted file cannot exhaust memory.
	maxDecoded = 480e6
)

// errFormat is a picture this package cannot read.
var errFormat = errors.New("a preview is made from a JPEG, PNG or GIF picture; turn other formats (HEIC, a video's frame) into a JPEG first")

// Readable reports a type a preview can be made from.
func Readable(mime string) bool {
	return mime == "image/jpeg" || mime == "image/png" || mime == "image/gif"
}

// qualities are the JPEG qualities Make tries, in order, until the picture fits in maxBytes: 80 first, which on a
// screen looks as 85 does at about three quarters of its bytes.
var qualities = []int{80, 70, 60, 50, 40, 35}

// Make decodes a picture, scales its long edge to at most makeEdge, turns it upright by a JPEG's EXIF orientation, and
// encodes it as a JPEG of at most maxBytes. The result carries no metadata: a photo's GPS stays out (D21).
func Make(src []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, errFormat
	}
	bytesPerPixel := 4.0
	if cfg.ColorModel == color.RGBA64Model || cfg.ColorModel == color.NRGBA64Model {
		bytesPerPixel = 8 // the PNG decoder keeps 16 bits for each of the four channels
	}
	if float64(cfg.Width)*float64(cfg.Height)*bytesPerPixel > maxDecoded {
		return nil, fmt.Errorf("the picture is %d×%d: too large to read", cfg.Width, cfg.Height)
	}
	m, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errFormat, err)
	}
	w, h := m.Bounds().Dx(), m.Bounds().Dy()
	if w == 0 || h == 0 {
		return nil, errFormat
	}
	dw, dh := w, h
	if long := max(w, h); long > makeEdge {
		dw, dh = max(1, (w*makeEdge+long/2)/long), max(1, (h*makeEdge+long/2)/long)
	}
	out := orient(scale(m, dw, dh), photo.Read(src).Orientation)
	for _, q := range qualities {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, out, &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		if b.Len() <= maxBytes {
			return b.Bytes(), nil
		}
	}
	return nil, errors.New("the picture does not fit in 1 MB")
}

// Check reports whether the long edge of the JPEG b is at most maxEdge. It is the one rule of a preview that SQLite
// cannot hold, as it cannot decode a picture. The magic bytes and the size are held by files_preview, so the
// schema refuses them when the row is written. Bytes that are not a JPEG header cannot be measured: that is
// refused here.
func Check(b []byte) error {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("a preview is a JPEG: %v", err)
	}
	if max(cfg.Width, cfg.Height) > maxEdge {
		return fmt.Errorf("a preview's long edge is at most %d px, not %d", maxEdge, max(cfg.Width, cfg.Height))
	}
	return nil
}

// scale averages the source pixels that fall in each destination pixel (a box filter: what a downscale by a large
// factor needs), compositing any transparency over white, since a JPEG has none.
func scale(src image.Image, dw, dh int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	at := pixels(src)
	sum := make([]uint64, dw*4)
	flush := func(row int) {
		for x := range dw {
			s := sum[x*4 : x*4+4]
			if s[3] > 0 {
				i := dst.PixOffset(x, row)
				dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(s[0]/s[3]), uint8(s[1]/s[3]), uint8(s[2]/s[3]), 0xFF
			}
			s[0], s[1], s[2], s[3] = 0, 0, 0, 0
		}
	}
	row := 0
	for y := range sh {
		if ty := y * dh / sh; ty != row {
			flush(row)
			row = ty
		}
		for x := range sw {
			r, g, bl := at(b.Min.X+x, b.Min.Y+y)
			s := sum[(x*dw/sw)*4:]
			s[0] += uint64(r)
			s[1] += uint64(g)
			s[2] += uint64(bl)
			s[3]++
		}
	}
	flush(row)
	return dst
}

// pixels reads 8-bit RGB over white, with fast paths for what the decoders return.
func pixels(src image.Image) func(x, y int) (r, g, b uint8) {
	switch m := src.(type) {
	case *image.YCbCr:
		return func(x, y int) (uint8, uint8, uint8) {
			return color.YCbCrToRGB(m.Y[m.YOffset(x, y)], m.Cb[m.COffset(x, y)], m.Cr[m.COffset(x, y)])
		}
	case *image.Gray:
		return func(x, y int) (uint8, uint8, uint8) { v := m.Pix[m.PixOffset(x, y)]; return v, v, v }
	}
	return func(x, y int) (uint8, uint8, uint8) {
		r, g, b, a := src.At(x, y).RGBA() // premultiplied: over white is c + (1 - a)
		return uint8((r + 0xFFFF - a) >> 8), uint8((g + 0xFFFF - a) >> 8), uint8((b + 0xFFFF - a) >> 8)
	}
}

// orient turns m upright for an EXIF orientation (1-8): 2 mirrored, 3 turned half, 4 flipped, 5 transposed,
// 6 turned a quarter clockwise, 7 transversed, 8 turned a quarter counter-clockwise.
func orient(m *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return m
	}
	w, h := m.Bounds().Dx(), m.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			copy(out.Pix[out.PixOffset(nx, ny):out.PixOffset(nx, ny)+4], m.Pix[m.PixOffset(x, y):m.PixOffset(x, y)+4])
		}
	}
	return out
}
