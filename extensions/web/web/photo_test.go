package web_test

// A dated photo that a browser sends through web. The core reads the date from the EXIF of the file and says the result in
// words. The core is the real one, and the photo is made in the test: a JPEG of the tests with the EXIF block of a camera
// that has a date and nothing else.

import (
	"bytes"
	"encoding/binary"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// jpegDatedByExif is syntheticJPEG with an EXIF block in an APP1 segment right after the SOI marker. The block has the byte order
// of Motorola, IFD0 with the pointer to the Exif IFD, and in it the tag DateTimeOriginal with taken ("2019:06:04 10:00:00").
// Nothing else is in it: no place and no camera.
func jpegDatedByExif(t *testing.T, taken string) []byte {
	t.Helper()
	date := append([]byte(taken), 0)
	be := binary.BigEndian
	tiff := append([]byte("MM\x00\x2a"), be.AppendUint32(nil, 8)...) // the header: IFD0 follows at byte 8
	tiff = be.AppendUint16(tiff, 1)                                  // IFD0 has one entry: the Exif IFD, at byte 26
	tiff = be.AppendUint16(tiff, 0x8769)
	tiff = be.AppendUint16(tiff, 4) // type LONG
	tiff = be.AppendUint32(tiff, 1)
	tiff = be.AppendUint32(tiff, 26)
	tiff = be.AppendUint32(tiff, 0) // no next IFD
	tiff = be.AppendUint16(tiff, 1) // the Exif IFD has one entry: DateTimeOriginal, whose text follows the IFD at byte 44
	tiff = be.AppendUint16(tiff, 0x9003)
	tiff = be.AppendUint16(tiff, 2) // type ASCII
	tiff = be.AppendUint32(tiff, uint32(len(date)))
	tiff = be.AppendUint32(tiff, 44)
	tiff = be.AppendUint32(tiff, 0)
	tiff = append(tiff, date...)

	segment := append([]byte("Exif\x00\x00"), tiff...)
	plain := syntheticJPEG(t)
	out := append([]byte{}, plain[:2]...) // the SOI marker
	out = append(out, 0xFF, 0xE1, byte((len(segment)+2)>>8), byte(len(segment)+2))
	out = append(out, segment...)
	return append(out, plain[2:]...)
}

// postPhotoToWeb posts the form of a file to h as a browser does: the fields as text, and the file as the field original.
func postPhotoToWeb(t *testing.T, h http.Handler, fields map[string]string, name string, photo []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for field, value := range fields {
		if err := mw.WriteField(field, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("original", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(photo); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/files", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestADatedPhotoTellsItsResultInWordsOnTheDryRunAndOnTheWrite: the dry run of a photo with a date writes nothing and the
// page says so, with the result and the day that the photo has. The write is a redirect to a path with a receipt and
// nothing private in it (no name of the file, no place), and the page where it lands draws the same result once.
func TestADatedPhotoTellsItsResultInWordsOnTheDryRunAndOnTheWrite(t *testing.T) {
	l := emptyLife(t)
	photo := jpegDatedByExif(t, "2019:06:04 10:00:00")
	mediaCount := func() int { return len(must(t)(l.api.Get(t.Context(), "/browse?kind=media")).Entities) }

	dry := postPhotoToWeb(t, l.web, map[string]string{"title": "Dated synthetic.jpg", "dry_run": "1"}, "synthetic.jpg", photo)
	if dry.Code != http.StatusOK || dry.Header().Get("Location") != "" || !strings.Contains(dry.Body.String(), "nothing written") {
		t.Fatalf("dry run: %d, Location %q\n%.600s", dry.Code, dry.Header().Get("Location"), dry.Body.String())
	}
	for _, want := range []string{"Result", "Its own day is", `<time datetime="2019-06-04">`} { // the page says the result in words, with the day of the EXIF
		if !strings.Contains(dry.Body.String(), want) {
			t.Errorf("the dry run lacks %q", want)
		}
	}
	if n := mediaCount(); n != 0 {
		t.Errorf("the dry run kept %d files", n)
	}

	written := postPhotoToWeb(t, l.web, map[string]string{"title": "Dated synthetic.jpg"}, "synthetic.jpg", photo)
	location := written.Header().Get("Location")
	if written.Code != http.StatusSeeOther || !regexp.MustCompile(`^/pages/\d+\?feedback=[0-9a-f]{64}$`).MatchString(location) {
		t.Fatalf("photo POST: %d to %q, want 303 to a page with a receipt of 64 hexadecimal digits: %.300s", written.Code, location, written.Body.String())
	}
	if strings.Contains(location, "41.1496") || strings.Contains(strings.ToLower(location), "synthetic") {
		t.Fatalf("private redirect: %q", location)
	}
	landed := ask(l.web, http.MethodGet, location)
	for _, want := range []string{"Result", "Its own day is", `<time datetime="2019-06-04">`} {
		if !strings.Contains(landed.Body.String(), want) {
			t.Errorf("photo feedback missing %q", want)
		}
	}
	if n := mediaCount(); n != 1 {
		t.Errorf("the write kept %d files, want 1", n)
	}
	if again := ask(l.web, http.MethodGet, location); strings.Contains(again.Body.String(), "Its own day is") {
		t.Error("the receipt drew its toast twice")
	}
}
