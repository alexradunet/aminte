package photo_test

import (
	"testing"

	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestReadsAJPEG(t *testing.T) {
	want := photo.Meta{Taken: "2019-06-03 07:41:12", Orientation: 6}
	south := photo.Meta{Taken: "2024-01-31 23:59:59", Orientation: 1}
	for _, little := range []bool{false, true} {
		got := photo.Read(phototest.WithExif(phototest.JPEG(40, 30), want, little))
		if got.Taken != want.Taken || got.Orientation != 6 || got.Day() != "2019-06-03" {
			t.Errorf("JPEG (little-endian %v): %+v", little, got)
		}
		if got := photo.Read(phototest.WithExif(phototest.JPEG(8, 8), south, little)); got.Taken != south.Taken {
			t.Errorf("south and east (little-endian %v): %+v", little, got)
		}
	}
}

func TestSaysNothingItDoesNotKnow(t *testing.T) {
	for name, b := range map[string][]byte{
		"empty":         {},
		"no EXIF":       phototest.JPEG(8, 8),
		"not a photo":   []byte("%PDF-1.7 hello"),
		"a HEIC's head": []byte("\x00\x00\x00\x10ftypheic\x00\x00\x00\x00\x00\x00\x00\x08meta"),
		"a JPEG's head": phototest.WithExif(phototest.JPEG(8, 8), photo.Meta{Taken: "2019-06-03 07:41:12"}, false)[:40],
	} {
		got := photo.Read(b)
		if got.Orientation != 1 && got.Orientation != 0 {
			t.Errorf("%s: %+v", name, got)
		}
		if got.Taken != "" {
			t.Errorf("%s: a date from nowhere: %+v", name, got)
		}
	}
	if got := photo.Read(phototest.WithExif(phototest.JPEG(8, 8), photo.Meta{Taken: "2019-06-03 07:41:12"}, true)); got.Day() != "2019-06-03" {
		t.Errorf("a date alone: %+v", got)
	}
	if (photo.Meta{}).Day() != "" {
		t.Error("no date, no day")
	}
}

func FuzzRead(f *testing.F) {
	meta := photo.Meta{Taken: "2026-10-05 12:34:56", Orientation: 6}
	for _, seed := range [][]byte{
		{},
		[]byte("%PDF-1.7 hello"),
		phototest.WithExif(phototest.JPEG(8, 8), meta, false),
		phototest.WithExif(phototest.JPEG(8, 8), meta, true),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, b []byte) { photo.Read(b) })
}
