package preview

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"

	"aminte/internal/photo"
	"aminte/internal/photo/phototest"
)

// halves is a w×h picture, its left half red and its right half blue, with noise so it compresses like a photo.
func halves(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for y := range h {
		for x := range w {
			n := uint8(r.Intn(40))
			c := color.RGBA{200 + n/2, n, n, 255}
			if x >= w/2 {
				c = color.RGBA{n, n, 200 + n/2, 255}
			}
			m.SetRGBA(x, y, c)
		}
	}
	return m
}

func encodeJPEG(t *testing.T, m image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	if err := Check(b); err != nil {
		t.Fatalf("not a preview: %v", err)
	}
	m, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func reddish(c color.Color) bool { r, _, b, _ := c.RGBA(); return r > 2*b }
func bluish(c color.Color) bool  { r, _, b, _ := c.RGBA(); return b > 2*r }

// Make scales the long edge to 1280 px, the writer's choice inside the contract's 1600 (Check): it is sharp at twice
// the size the browser draws a picture in a page, and about half the bytes of 1600.
func TestMakeScalesTheLongEdge(t *testing.T) {
	for _, sz := range [][4]int{{4000, 3000, 1280, 960}, {3000, 4000, 960, 1280}, {1600, 1200, 1280, 960}, {800, 600, 800, 600}, {5000, 7, 1280, 2}} {
		b, err := Make(encodeJPEG(t, halves(sz[0], sz[1])))
		if err != nil {
			t.Fatal(err)
		}
		m := decode(t, b)
		if got := m.Bounds().Size(); got.X != sz[2] || got.Y != sz[3] {
			t.Errorf("%dx%d became %v, want %dx%d", sz[0], sz[1], got, sz[2], sz[3])
		}
		if !reddish(m.At(m.Bounds().Dx()/4, m.Bounds().Dy()/2)) || !bluish(m.At(3*m.Bounds().Dx()/4, m.Bounds().Dy()/2)) {
			t.Errorf("%dx%d: the halves did not survive the scaling", sz[0], sz[1])
		}
	}
}

func TestMakeTurnsAJPEGUpright(t *testing.T) {
	src := encodeJPEG(t, halves(40, 20)) // red left, blue right
	if photo.Read(src).Orientation != 1 {
		t.Fatal("a JPEG without EXIF has orientation 1")
	}
	for o, want := range map[int][2]string{
		6: {"red", "blue"}, // a quarter clockwise: the left half goes to the top
		8: {"blue", "red"}, // a quarter counter-clockwise: the left half goes to the bottom
		3: {"blue", "red"}, // half a turn: the left half goes to the right
	} {
		j := phototest.WithExif(src, photo.Meta{Orientation: o}, false)
		if got := photo.Read(j).Orientation; got != o {
			t.Fatalf("orientation %d read as %d", o, got)
		}
		b, err := Make(j)
		if err != nil {
			t.Fatal(err)
		}
		m := decode(t, b)
		w, h := m.Bounds().Dx(), m.Bounds().Dy()
		var first, second color.Color
		if o == 3 {
			if w != 40 || h != 20 {
				t.Fatalf("orientation 3: %dx%d, want 40x20", w, h)
			}
			first, second = m.At(10, 10), m.At(30, 10)
		} else {
			if w != 20 || h != 40 {
				t.Fatalf("orientation %d: %dx%d, want 20x40", o, w, h)
			}
			first, second = m.At(10, 10), m.At(10, 30)
		}
		is := map[string]func(color.Color) bool{"red": reddish, "blue": bluish}
		if !is[want[0]](first) || !is[want[1]](second) {
			t.Errorf("orientation %d: want %s then %s", o, want[0], want[1])
		}
		if photo.Read(b).Orientation != 1 {
			t.Errorf("orientation %d: the preview keeps EXIF", o)
		}
	}
}

func TestMakeReadsPNGAndGIF(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 10, 10)) // transparent: over white
	var p bytes.Buffer
	png.Encode(&p, m)
	b, err := Make(p.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r, g, bl, _ := decode(t, b).At(5, 5).RGBA(); r < 0xF000 || g < 0xF000 || bl < 0xF000 {
		t.Errorf("a transparent PNG is not white: %x %x %x", r, g, bl)
	}
	var g bytes.Buffer
	gif.Encode(&g, halves(30, 30), nil)
	if b, err = Make(g.Bytes()); err != nil {
		t.Fatal(err)
	}
	decode(t, b)
}

// A 16-bit PNG decodes to 8 bytes a pixel, twice the 4 of an 8-bit picture: the limit counts bytes, not pixels.
func TestMakeCountsTheBytesOfADecodedPicture(t *testing.T) {
	m := image.NewNRGBA64(image.Rect(0, 0, 10, 10))
	for y := range 10 {
		for x := range 10 {
			m.SetNRGBA64(x, y, color.NRGBA64{R: 0xFFFF, G: 0x4000, B: 0x4000, A: 0x8000})
		}
	}
	var p bytes.Buffer
	if err := png.Encode(&p, m); err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(p.Bytes())); err != nil || cfg.ColorModel != color.NRGBA64Model {
		t.Fatalf("the synthetic PNG is not 16-bit: %+v %v", cfg, err)
	}
	if _, err := Make(p.Bytes()); err != nil {
		t.Fatalf("a small 16-bit PNG: %v", err)
	}

	// 9000×9000 is 648 MB at 8 bytes a pixel. Only the header says so: the decoder reads no more before the refusal.
	big := append([]byte{}, p.Bytes()...)
	const ihdr = 8 + 8 // the PNG signature, then the length and the name of the first chunk
	binary.BigEndian.PutUint32(big[ihdr:], 9000)
	binary.BigEndian.PutUint32(big[ihdr+4:], 9000)
	binary.BigEndian.PutUint32(big[ihdr+13:], crc32.ChecksumIEEE(big[ihdr-4:ihdr+13]))
	_, err := Make(big)
	if err == nil || err.Error() != "the picture is 9000×9000: too large to read" {
		t.Fatalf("a 9000×9000 16-bit PNG: %v", err)
	}
}

func TestMakeRefuses(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("not a picture"), {0xFF, 0xD8, 0xFF, 0xE0}} {
		if _, err := Make(b); err == nil {
			t.Errorf("%q made a preview", b)
		}
	}
}

// Make encodes at quality 80 first. A JPEG names its quality only in its tables: by the IJG scaling of the standard
// luminance table (Annex K of ITU T.81, first value 16), quality 80 makes the first value (16*40+50)/100 = 6, and
// quality 85 makes 5. The picture is small, so the first quality fits in 1 MB.
func TestMakeEncodesAtQuality80(t *testing.T) {
	b, err := Make(encodeJPEG(t, halves(64, 48)))
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte{0xFF, 0xDB}) // DQT: the marker, a length of 2 bytes, Pq/Tq, then 64 values
	if i < 0 || len(b) < i+6 {
		t.Fatal("no quantization table")
	}
	if pqtq, first := b[i+4], b[i+5]; pqtq != 0 || first != 6 {
		t.Errorf("the luminance table starts Pq/Tq %d, value %d; want 0 and 6 (quality 80)", pqtq, first)
	}
}

// Check holds the long edge only. The magic bytes and the size of a preview are files_preview's: the core suite
// witnesses that the schema refuses a preview over 1 MB.
func TestCheck(t *testing.T) {
	for name, b := range map[string][]byte{"a small JPEG": encodeJPEG(t, halves(16, 16)), "1600 px, the contract's edge": encodeJPEG(t, halves(1600, 10))} {
		if err := Check(b); err != nil { // a writer may send any edge up to the contract's, whatever Make chooses
			t.Errorf("%s: %v", name, err)
		}
	}
	var p bytes.Buffer
	png.Encode(&p, halves(4, 4))
	for name, b := range map[string][]byte{
		"a PNG":           p.Bytes(),
		"empty":           {},
		"too wide":        encodeJPEG(t, halves(1601, 10)),
		"JPEG bytes only": {0xFF, 0xD8, 0xFF, 0xE0, 0, 0},
	} {
		if Check(b) == nil {
			t.Errorf("%s passed", name)
		}
	}
	if !Readable("image/jpeg") || !Readable("image/png") || Readable("image/heic") || Readable("video/mp4") {
		t.Error("Readable")
	}
}
