// Package phototest builds synthetic photos for tests: EXIF (a TIFF block) in either byte order, and a JPEG with or
// without it. No real photo is used.
package phototest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"strings"

	"aminte/internal/photo"
)

type entry struct {
	tag, typ uint16
	count    uint32
	data     []byte
}

// TIFF is EXIF saying what m says: IFD0 (orientation, the pointers) and the Exif IFD (DateTimeOriginal).
func TIFF(m photo.Meta, little bool) []byte {
	var bo interface {
		binary.ByteOrder
		binary.AppendByteOrder
	} = binary.BigEndian
	head := []byte("MM\x00\x2a")
	if little {
		bo, head = binary.LittleEndian, []byte("II\x2a\x00")
	}
	u16 := func(v uint16) []byte { return bo.AppendUint16(nil, v) }
	u32 := func(v uint32) []byte { return bo.AppendUint32(nil, v) }
	var ifd0, exif []entry
	if m.Orientation > 0 {
		ifd0 = append(ifd0, entry{0x0112, 3, 1, u16(uint16(m.Orientation))})
	}
	if m.Taken != "" {
		s := strings.Replace(m.Taken[:10], "-", ":", 2) + m.Taken[10:] + "\x00"
		exif = append(exif, entry{0x9003, 2, uint32(len(s)), []byte(s)})
	}
	size := func(es []entry) int { return 2 + 12*len(es) + 4 }
	n0 := len(ifd0)
	if exif != nil {
		ifd0 = append(ifd0, entry{0x8769, 4, 1, nil})
	}
	offExif := 8 + size(ifd0)
	cursor := offExif
	if exif != nil {
		cursor += size(exif)
	}
	for i := n0; i < len(ifd0); i++ {
		ifd0[i].data = u32(uint32(offExif)) // the only pointer is the one to the Exif IFD
	}
	var dataArea []byte
	write := func(es []entry) []byte {
		b := u16(uint16(len(es)))
		for _, e := range es {
			b = append(b, u16(e.tag)...)
			b = append(b, u16(e.typ)...)
			b = append(b, u32(e.count)...)
			if len(e.data) <= 4 {
				b = append(b, append(append([]byte{}, e.data...), make([]byte, 4-len(e.data))...)...)
			} else {
				b = append(b, u32(uint32(cursor+len(dataArea)))...)
				dataArea = append(dataArea, e.data...)
			}
		}
		return append(b, 0, 0, 0, 0) // no next IFD
	}
	out := append(append(head, u32(8)...), write(ifd0)...)
	if exif != nil {
		out = append(out, write(exif)...)
	}
	return append(out, dataArea...)
}

// JPEG is a w×h JPEG (a gradient) with no metadata. It panics with the error of jpeg.Encode when the encode fails (a
// side of 65,536 pixels or more): such a size is a defect of the test.
func JPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		panic(fmt.Errorf("phototest: a %d×%d JPEG: %w", w, h, err))
	}
	return b.Bytes()
}

// WithExif is the JPEG j with m's EXIF in an APP1 segment right after its SOI marker.
func WithExif(j []byte, m photo.Meta, little bool) []byte {
	seg := append([]byte("Exif\x00\x00"), TIFF(m, little)...)
	out := append([]byte{}, j[:2]...)
	out = append(out, 0xFF, 0xE1, byte((len(seg)+2)>>8), byte(len(seg)+2))
	out = append(out, seg...)
	return append(out, j[2:]...)
}
