package phototest

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"runtime"
	"strings"
	"testing"

	"lifelog/internal/photo"
)

// nothing is what photo.Read says of a photo whose metadata it cannot read.
var nothing = photo.Meta{Orientation: 1}

// TestJPEGPanicsWithTheEncodeError: a size that a JPEG cannot hold stops JPEG with the error of jpeg.Encode, not
// with an index error after the encode failed.
func TestJPEGPanicsWithTheEncodeError(t *testing.T) {
	const w, h = 1 << 16, 1
	want := jpeg.Encode(io.Discard, image.NewRGBA(image.Rect(0, 0, w, h)), nil)
	if want == nil {
		t.Fatalf("jpeg.Encode accepts a %d×%d image: the test needs another size", w, h)
	}
	defer func() {
		got, ok := recover().(error)
		var index runtime.Error
		if !ok || errors.As(got, &index) || !strings.Contains(got.Error(), want.Error()) {
			t.Errorf("JPEG panicked with %v, want the error of jpeg.Encode: %v", got, want)
		}
	}()
	JPEG(w, h)
}

// TestJPEGAndWithExif: JPEG is a w×h JPEG with no metadata; WithExif adds the EXIF of m, in the byte order it is
// given, and keeps the JPEG around its segment.
func TestJPEGAndWithExif(t *testing.T) {
	plain := JPEG(40, 30)
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(plain)); err != nil || cfg.Width != 40 || cfg.Height != 30 {
		t.Fatalf("JPEG(40, 30) decodes as %+v, %v", cfg, err)
	}
	if got := photo.Read(plain); got != nothing {
		t.Errorf("JPEG(40, 30) has metadata: %+v", got)
	}
	m := photo.Meta{Taken: "2019-06-03 07:41:12", Orientation: 6}
	for _, little := range []bool{false, true} {
		b := WithExif(plain, m, little)
		if got := photo.Read(b); got != m {
			t.Errorf("little-endian %v: photo.Read = %+v, want %+v", little, got, m)
		}
		if got := bytes.Contains(b, []byte("Exif\x00\x00II")); got != little {
			t.Errorf("little-endian %v: the TIFF block is little-endian: %v", little, got)
		}
		if !bytes.Equal(b[:2], plain[:2]) || !bytes.HasSuffix(b, plain[2:]) {
			t.Errorf("little-endian %v: the JPEG around the segment changed", little)
		}
	}
}
