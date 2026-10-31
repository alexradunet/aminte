// Package photo reads what a photo's metadata says about when it was taken and its orientation
// (docs/cookbook/keep-a-file.md): the EXIF of a JPEG (its APP1 segment). It reads no other format: a HEIC says nothing.
// It does not read where a photo was taken: it does not read the GPS IFD (D21). It reads bytes it is handed, never
// more: a caller holding only a file's head gets what the head holds.
package photo

import (
	"encoding/binary"
	"strings"
	"time"
)

// Meta is what the metadata says; the zero of each field is "it does not say".
type Meta struct {
	Taken       string // the local date and time it was taken (EXIF DateTimeOriginal, the camera's clock), "2006-01-02 15:04:05"
	Orientation int    // the EXIF orientation, 1-8; 1 when it has none
}

// Day is the local day it was taken (lifelog_meta.days: the calendar of the device that captured it), "" when none.
func (m Meta) Day() string {
	if len(m.Taken) < 10 {
		return ""
	}
	return m.Taken[:10]
}

// Read is the metadata of a JPEG; a file of any other kind, or with no metadata, says nothing.
func Read(b []byte) Meta {
	if t := jpegExif(b); t != nil {
		return parseTIFF(t)
	}
	return Meta{Orientation: 1}
}

// jpegExif is the TIFF data of a JPEG's APP1 Exif segment, nil when it has none.
func jpegExif(b []byte) []byte {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return nil
		}
		marker := b[i+1]
		switch {
		case marker == 0xFF: // fill byte
			i++
			continue
		case marker == 0xDA || marker == 0xD9: // the image data: no header follows
			return nil
		case marker == 0x01 || marker >= 0xD0 && marker <= 0xD7: // markers without a length
			i += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return nil
		}
		if seg := b[i+4 : i+2+n]; marker == 0xE1 && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00" {
			return seg[6:]
		}
		i += 2 + n
	}
	return nil
}

// parseTIFF reads IFD0 (orientation, the Exif IFD pointer) and the Exif IFD (DateTimeOriginal, else DateTimeDigitized).
// It does not read the GPS IFD (D21).
func parseTIFF(t []byte) Meta {
	m := Meta{Orientation: 1}
	if len(t) < 8 {
		return m
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return m
	}
	ifd0 := entries(t, bo, int(bo.Uint32(t[4:])))
	if v, ok := ifd0[0x0112]; ok {
		if o := int(v.short(t, bo)); o >= 1 && o <= 8 {
			m.Orientation = o
		}
	}
	if p, ok := ifd0[0x8769]; ok {
		exif := entries(t, bo, int(p.long(t, bo)))
		for _, tag := range []uint16{0x9003, 0x9004} { // DateTimeOriginal, else DateTimeDigitized
			if v, ok := exif[tag]; ok {
				if s, err := time.Parse("2006:01:02 15:04:05", strings.TrimRight(v.ascii(t, bo), "\x00 ")); err == nil {
					m.Taken = s.Format("2006-01-02 15:04:05")
					break
				}
			}
		}
	}
	return m
}

// entry is one IFD entry: its type, count, and the 4 bytes holding the value or its offset.
type entry struct {
	typ   uint16
	count uint32
	raw   []byte
}

func entries(t []byte, bo binary.ByteOrder, off int) map[uint16]entry {
	out := map[uint16]entry{}
	if off < 8 || off+2 > len(t) {
		return out
	}
	for k := range int(bo.Uint16(t[off:])) {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			break
		}
		out[bo.Uint16(t[e:])] = entry{bo.Uint16(t[e+2:]), bo.Uint32(t[e+4:]), t[e+8 : e+12]}
	}
	return out
}

var typeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}

// value is the entry's bytes: inline when they fit in 4, else at the offset.
func (e entry) value(t []byte, bo binary.ByteOrder) []byte {
	n := typeSize[e.typ] * int(e.count)
	if n == 0 || e.raw == nil {
		return nil
	}
	if n <= 4 {
		return e.raw[:n]
	}
	off := int(bo.Uint32(e.raw))
	if off < 0 || off+n > len(t) {
		return nil
	}
	return t[off : off+n]
}

func (e entry) short(t []byte, bo binary.ByteOrder) uint16 {
	if v := e.value(t, bo); e.typ == 3 && len(v) >= 2 {
		return bo.Uint16(v)
	}
	return 0
}

func (e entry) long(t []byte, bo binary.ByteOrder) uint32 {
	if v := e.value(t, bo); e.typ == 4 && len(v) >= 4 {
		return bo.Uint32(v)
	}
	return 0
}

func (e entry) ascii(t []byte, bo binary.ByteOrder) string {
	if e.typ != 2 {
		return ""
	}
	return string(e.value(t, bo))
}
