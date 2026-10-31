package web_test

// The media list and the documents list of Browse as the browser draws them: the counts in words, the kinds as links,
// a tile for each file with its picture or the icon of its kind, and the empty words. The claims are those of the HTML
// part of the media tests of the core (plan 161, stage 3).

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pictureFileOf writes a synthetic JPEG of w by h pixels in a new folder of the test and returns its path. Two sizes
// are two originals: the file keeps one page for one original.
func pictureFileOf(t *testing.T, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMediaListsPicturesVideosAndRecordings: the media list of Browse says its counts in words, links the kinds, and
// draws a tile for each file, with its picture where one is kept and else the icon of its kind, and its kind and day
// in words. A file of another kind is a row of the documents, with the same words.
func TestMediaListsPicturesVideosAndRecordings(t *testing.T) {
	l := emptyLife(t)
	photo := l.do(t, "/", "add-file", map[string]string{"title": "Lake.jpg", "day": "2031-05-10", "original": pictureFileOf(t, "lake.jpg", 40, 30)})
	l.do(t, "/", "add-file", map[string]string{"title": "Walk.mp4", "sha256": strings.Repeat("ab", 32), "mime": "video/mp4", "day": "2031-05-12"})
	l.do(t, "/", "capture", map[string]string{"text": "Walked. [[Walk.mp4]]"})
	for i, title := range []string{"Memo 1.m4a", "Memo 2.m4a"} {
		l.do(t, "/", "add-file", map[string]string{"title": title, "sha256": strings.Repeat(string(rune('c'+i))+"d", 32), "mime": "audio/mp4", "day": "2031-05-0" + string(rune('1'+i))})
	}
	// A photo with no EXIF and no day has no day of its own: the day page that shows it gives it one, or it has none.
	l.do(t, "/", "add-file", map[string]string{"title": "Dawn.jpg", "original": pictureFileOf(t, "dawn.jpg", 32, 24)})
	l.do(t, "/", "capture", map[string]string{"text": "![[Dawn.jpg]]"})
	l.do(t, "/", "add-file", map[string]string{"title": "Blank.jpg", "original": pictureFileOf(t, "blank.jpg", 24, 16)})
	l.do(t, "/", "add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf"})

	body := htmlPageAt(t, l.web, "/browse?kind=media", http.StatusOK)
	wantInPage(t, "the media page", body, "<span>3 pictures, 1 video, 2 recordings</span>",
		`<nav class="kinds" aria-label="Media kind"><a href="/browse?kind=media" aria-current="page">All media</a><a href="/browse?kind=media&amp;media=image">Pictures</a><a href="/browse?kind=media&amp;media=video">Videos</a><a href="/browse?kind=media&amp;media=audio">Recordings</a></nav>`,
		`<ul class="plain tiles">`, `<img src="`+href(photo, "preview")+`" alt="" loading="lazy"><span class="first">Lake.jpg</span><span class="meta">picture · Sat 10 May</span>`,
		`<span class="tile-blank" aria-hidden="true"><span class="icon icon-video" aria-hidden="true"></span>`, `<span class="first">Walk.mp4</span><span class="meta">video · Mon 12 May</span>`, `<span class="first">Dawn.jpg</span><span class="meta">picture · Wed 14 May</span>`,
		`<span class="first">Memo 1.m4a</span><span class="meta">recording · Thu 1 May</span>`, `<span class="first">Blank.jpg</span><span class="meta">picture · no day yet</span>`,
		`<label class="pick-box"><input type="checkbox" name="ids" value="`)
	wantNotInPage(t, "the media page", body, "Scan.pdf") // the PDF is a document

	// The documents are every other file, as rows, with the same words.
	l.do(t, "/", "add-file", map[string]string{"title": "Notes.txt", "sha256": strings.Repeat("0f", 32), "mime": "text/plain", "day": "2031-05-03"})
	docs := htmlPageAt(t, l.web, "/browse?kind=document", http.StatusOK)
	wantInPage(t, "the documents page", docs, `<a href="/browse?kind=document" aria-current="page">Documents <small>2</small></a>`,
		`<input type="hidden" name="back" value="/browse?kind=document">`, `aria-label="Select Notes.txt">`,
		`<span class="first">Notes.txt</span><span class="meta">text/plain · Sat 3 May</span>`, `<ul class="plain rows">`)
	wantNotInPage(t, "the documents page", docs, "Lake.jpg", "Memo 1.m4a", " pictures, ") // no media here

	videos := htmlPageAt(t, l.web, "/browse?kind=media&media=video", http.StatusOK)
	wantInPage(t, "the videos page", videos, `<a href="/browse?kind=media&amp;media=video" aria-current="page">Videos</a>`, `value="/browse?kind=media&amp;media=video">`)
	wantNotInPage(t, "the videos page", videos, "Memo 1.m4a")
	wantInPage(t, "an empty kind", htmlPageAt(t, l.web, "/browse?kind=media&media=video&offset=1", http.StatusOK), `<p class="empty">Nothing of this kind yet. New, above, makes the first one.</p>`)
}

// TestTheMediaCountsFollowTheScope: inside a category the media list counts what is filed there, and Not filed what
// is in no category.
func TestTheMediaCountsFollowTheScope(t *testing.T) {
	l, _ := browseLife(t) // Family: photo.jpg and scan.pdf; nowhere: memo.m4a
	for path, want := range map[string]string{
		"/browse?kind=media":            "<span>1 picture, no videos, 1 recording</span>",
		"/browse?kind=media&in=Family":  "<span>1 picture, no videos, no recordings</span>",
		"/browse?kind=media&unfiled=1":  "<span>no pictures, no videos, 1 recording</span>",
		"/browse?kind=media&in=Cousins": "<span>no pictures, no videos, no recordings</span>",
	} {
		wantInPage(t, path, htmlPageAt(t, l.web, path, http.StatusOK), want)
	}
}

// TestMediaIsEmptyUntilAFileOfItsKindsIsKept: a PDF alone leaves the media list empty, with its counts and its empty
// words, and a photo alone leaves the documents empty.
func TestMediaIsEmptyUntilAFileOfItsKindsIsKept(t *testing.T) {
	const empty = `<p class="empty">Nothing of this kind yet. New, above, makes the first one.</p>`
	l := emptyLife(t)
	l.do(t, "/", "add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf"})
	wantInPage(t, "the empty media page", htmlPageAt(t, l.web, "/browse?kind=media", http.StatusOK), `<span>no pictures, no videos, no recordings</span>`, empty)
	// And the other way round: a photo alone leaves the documents empty.
	l = emptyLife(t)
	l.do(t, "/", "add-file", map[string]string{"title": "Lake.jpg", "original": pictureFileOf(t, "lake.jpg", 40, 30)})
	wantInPage(t, "the empty documents page", htmlPageAt(t, l.web, "/browse?kind=document", http.StatusOK), empty)
}
