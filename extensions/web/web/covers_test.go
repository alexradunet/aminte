package web_test

// The covers and the pictures as the browser draws them: the way to the picker of a cover, the picker, the upload and the
// tile that set a cover, the cover on a page and on a day, the cell of the calendar, and the form of a new file. They run
// against the real core.

import (
	"bytes"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// photoPosted sends a photo as the form of a cover does: a multipart body with the empty title, the file and the empty
// folded preview input. The page to return to is in the query of the address, as the view of web draws the form.
func photoPosted(t *testing.T, h http.Handler, path, file, back string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mw.WriteField("title", "")
	part, err := mw.CreateFormFile("original", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.Copy(part, f); err != nil {
		t.Fatal(err)
	}
	mw.CreateFormFile("preview", "") // the folded preview input, left empty
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, path+"?back="+url.QueryEscape(back), &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// pictureTakenOn writes a synthetic JPEG whose EXIF says that it was taken at taken ("2019-06-03 07:41:12"), and returns
// its path. The EXIF is one APP1 segment right after the start of the JPEG. It holds a little-endian TIFF: IFD0 has the
// pointer to the Exif IFD (at byte 26), which has DateTimeOriginal (its text at byte 44).
func pictureTakenOn(t *testing.T, name, taken string) string {
	t.Helper()
	jpg, err := os.ReadFile(pictureFileOf(t, name, 64, 48))
	if err != nil {
		t.Fatal(err)
	}
	stamp := strings.Replace(taken[:10], "-", ":", 2) + taken[10:] + "\x00"
	var tiff bytes.Buffer
	put := func(values ...any) {
		for _, v := range values {
			if err := binary.Write(&tiff, binary.LittleEndian, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	tiff.WriteString("II*\x00")
	put(uint32(8))                                                                       // IFD0 follows the header
	put(uint16(1), uint16(0x8769), uint16(4), uint32(1), uint32(26), uint32(0))          // IFD0: the pointer to the Exif IFD, no next IFD
	put(uint16(1), uint16(0x9003), uint16(2), uint32(len(stamp)), uint32(44), uint32(0)) // the Exif IFD: DateTimeOriginal, no next IFD
	tiff.WriteString(stamp)
	segment := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, 0xFF, 0xE1, byte((len(segment)+2)>>8), byte(len(segment)+2))
	out = append(out, segment...)
	out = append(out, jpg[2:]...)
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSetCoverFromAnyPage: every page but a file page leads to the picker of its cover, and the people sheet draws no
// form to set one. A browser opens the picker from the page, sends a new photo there and comes back to the page; the
// photo is the page's cover, drawn once above it, and the person's picture in People, kept with a title of the page and
// today. A tile of the picker sets a kept picture.
func TestSetCoverFromAnyPage(t *testing.T) {
	l := emptyLife(t)
	ana := l.do(t, "/", "create-person", map[string]string{"title": "Ana"})
	l.do(t, href(ana, "self"), "save-body", map[string]string{"body": "Met at school."})
	lake := l.do(t, "/", "create-page", map[string]string{"title": "Lake"})
	trip := l.do(t, "/", "create-page", map[string]string{"title": "Trip"})
	scan := l.do(t, "/", "add-file", map[string]string{"title": "Scan.jpg", "original": pictureFileOf(t, "scan.jpg", 20, 20)})
	for _, e := range []struct {
		title string
		self  string
	}{{"Ana", href(ana, "self")}, {"Lake", href(lake, "self")}, {"Trip", href(trip, "self")}} {
		wantInPage(t, "the page of "+e.title, pageAt(t, l.web, e.self), `<a href="`+e.self+`/cover">Set cover photo</a>`)
	}
	wantNotInPage(t, "the people sheet", pageAt(t, l.web, "/browse?kind=person"), `/cover"`) // no form to set a cover
	self := href(ana, "self")
	wantNotInPage(t, "the page of Ana", pageAt(t, l.web, self), `<figure class="cover">`) // it has no cover yet

	picker := pageAt(t, l.web, self+"/cover")
	wantInPage(t, "the picker", picker, `action="`+self+`/cover?back=`, `enctype="multipart/form-data"`, `name="original"`, `accept="image/*"`,
		`<input type="hidden" name="title" value="Scan.jpg">`, `<img src="`+href(scan, "preview")+`" alt="" loading="lazy">`)

	rec := photoPosted(t, l.web, self+"/cover", pictureFileOf(t, "ana.jpg", 40, 30), self)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), self) {
		t.Fatalf("POST %s/cover: %d to %q %.300s", self, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	title := "Ana " + today + ".jpg"
	page := must(t)(l.api.Get(t.Context(), self))
	if got, _ := page.Properties.(map[string]any)["body"].(string); got != "![["+title+"]]\n\nMet at school." {
		t.Errorf("Ana's body = %q", got)
	}
	cover := href(page, "cover")
	if !strings.HasSuffix(cover, "/preview") {
		t.Fatalf("Ana's page has no cover link: %+v", page.Links)
	}
	body := pageAt(t, l.web, self)
	if !strings.Contains(body, `<section class="cover-box" aria-label="Cover photo">`+"\n"+`<figure class="cover"><img src="`+cover+`" alt="`+title+`"></figure>`) ||
		!strings.Contains(body, `<a href="`+self+`/cover">Change cover photo</a>`) || strings.Contains(body, "/previews?title=") {
		t.Errorf("the cover is not drawn once, at the head of the margin, with the way to the picker")
	}
	wantInPage(t, "the people sheet", pageAt(t, l.web, "/browse?kind=person"), `<img class="face-picture" src="`+cover+`" alt="" loading="lazy">`)

	// A tile of the picker is a form of the picture's title; it sends the reader back to the page.
	rec = postForm(l.web, self+"/cover", url.Values{"title": {"Scan.jpg"}, "back": {self}}, "Referer", refererOnWeb(self+"/cover"))
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), self) {
		t.Fatalf("the tile: %d to %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	chosen, _ := must(t)(l.api.Get(t.Context(), self)).Properties.(map[string]any)["cover"].(map[string]any)
	if chosen["title"] != "Scan.jpg" {
		t.Errorf("the cover after the tile = %v, want Scan.jpg", chosen)
	}
	// The picker then marks the tile of the cover, and only that one.
	marked := pageAt(t, l.web, self+"/cover")
	wantInPage(t, "the picker after the tile", marked, `<li class="tile current"><form method="post" action="`+self+`/cover"><input type="hidden" name="back" value="`+self+`"><input type="hidden" name="title" value="Scan.jpg">`)
	if n := strings.Count(marked, `<li class="tile current">`); n != 1 {
		t.Errorf("the picker marks %d tiles as the cover, want 1", n)
	}
}

// TestTheCoverOfADay: the first picture of a day page is its cover, in the day page and in the day view, which draw it
// above and not again in the journal. A day view whose day page has no cover leads to the picker from the journal; a day
// with no day page yet has no picker, since there is no page to take a cover.
func TestTheCoverOfADay(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/days/2026-01-02", "capture", map[string]string{"text": "Words."})
	plain := must(t)(l.api.Get(t.Context(), "/days/2026-01-02"))
	if body := pageAt(t, l.web, "/days/2026-01-02"); href(plain, "choose-cover") == "" ||
		!strings.Contains(body, `<a href="`+href(plain, "choose-cover")+`">Set cover photo</a>`) {
		t.Errorf("a day with no cover does not lead to the picker")
	}
	wantNotInPage(t, "a day with no day page", pageAt(t, l.web, "/days/2026-01-03"), "Set cover photo")
	f := l.do(t, "/", "add-file", map[string]string{"title": "Lake.jpg", "day": today, "original": pictureFileOf(t, "lake.jpg", 40, 30)})
	day := must(t)(l.api.Get(t.Context(), "/days/"+today))
	for _, path := range []string{"/days/" + today, href(day, "page")} {
		body := pageAt(t, l.web, path)
		if !strings.Contains(body, `<figure class="cover"><img src="`+href(f, "preview")+`"`) || strings.Contains(body, "/previews?title=Lake.jpg") ||
			strings.Contains(body, "Set cover photo") {
			t.Errorf("%s does not draw its cover once, above it, or still offers to set one", path)
		}
	}
}

// TestAPictureTheBodyEmbedsBecomesTheCover: the picker's form of a kept picture that the body embeds makes it the cover
// by trading places with the old cover's embed, and the browser comes back to the page. The margin draws the cover and
// one link to the picker, no other picture (docs/plans/131-the-pareto-pass.md, part 2).
func TestAPictureTheBodyEmbedsBecomesTheCover(t *testing.T) {
	l := emptyLife(t)
	for i, title := range []string{"A.jpg", "B.jpg"} {
		l.do(t, "/", "add-file", map[string]string{"title": title, "original": pictureFileOf(t, strings.ToLower(title), 40, 30+i)}) // another size: another original
	}
	trip := l.do(t, "/", "create-page", map[string]string{"title": "Trip", "body": "![[A.jpg]]\n\nSwam. ![[B.jpg]]"})
	self := href(trip, "self")
	if body := pageAt(t, l.web, self); strings.Count(body, "/preview\"") != 1 {
		t.Errorf("the page draws %d pictures besides its body, want the cover alone", strings.Count(body, "/preview\""))
	}
	wantInPage(t, "the picker", pageAt(t, l.web, self+"/cover"),
		`<form method="post" action="`+self+`/cover"><input type="hidden" name="back" value="`+self+`"><input type="hidden" name="title" value="B.jpg">`)
	rec := postForm(l.web, self+"/cover", url.Values{"title": {"B.jpg"}, "back": {self}}, "Referer", refererOnWeb(self))
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), self) {
		t.Fatalf("the picker's B.jpg: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	p, _ := must(t)(l.api.Get(t.Context(), self)).Properties.(map[string]any)
	if got, _ := p["body"].(string); got != "![[B.jpg]]\n\nSwam. ![[A.jpg]]" {
		t.Errorf("the body after B.jpg = %q", got)
	}
	if cover, _ := p["cover"].(map[string]any); cover["title"] != "B.jpg" {
		t.Errorf("the cover = %v, want B.jpg", p["cover"])
	}
}

// TestTheCalendarShowsTheCovers: a day page with a cover gives the month its picture, and the month draws it behind the
// day, once; a day page without one has none.
func TestTheCalendarShowsTheCovers(t *testing.T) {
	l := emptyLife(t)
	f := l.do(t, "/", "add-file", map[string]string{"title": "Lake.jpg", "day": "2031-05-02", "original": pictureFileOf(t, "lake.jpg", 40, 30)})
	l.do(t, "/days/2031-05-01", "capture", map[string]string{"text": "No photo."})
	body := pageAt(t, l.web, "/months/2031-05")
	if !strings.Contains(body, `<td class="written pictured"><a href="/days/2031-05-02"><img class="cell-cover" src="`+href(f, "preview")+`" alt="" loading="lazy">`) ||
		strings.Count(body, `class="cell-cover"`) != 1 {
		t.Errorf("the month does not draw the one cover behind its day")
	}
}

// TestFileViews: the lists, the page of a file and the day page show a picture: the form of a new document, the tile of
// the picture, the preview on the page of the file with its type and its backlinks, and the cover on the day page that
// embeds it, drawn once.
func TestFileViews(t *testing.T) {
	l := emptyLife(t)
	f := l.do(t, "/", "add-file", map[string]string{"title": "Lake.jpg", "body": "dawn", "original": pictureFileOf(t, "lake.jpg", 40, 30)})
	day := l.do(t, "/", "capture", map[string]string{"text": "Swam. ![[Lake.jpg|the lake]]"})
	for path, want := range map[string][]string{
		"/browse?kind=document": {`enctype="multipart/form-data"`, `type="file" name="original"`, `type="hidden" name="sha256"`},
		"/browse?kind=media":    {"Lake.jpg", `action="/files"`, `<img src="` + href(f, "preview") + `" alt="" loading="lazy">`},
		href(f, "self"):         {`<img class="preview" src="` + href(f, "preview") + `"`, "image/jpeg", "Backlinks"},
		// the first picture of a day page is its cover, drawn above it and not again in the body
		href(day, "page"): {`<figure class="cover"><img src="` + href(f, "preview") + `" alt="Lake.jpg">`, "Swam."},
	} {
		wantInPage(t, path, pageAt(t, l.web, path), want...)
	}
	wantNotInPage(t, "the day page", pageAt(t, l.web, href(day, "page")), `/previews?title=Lake.jpg`) // the cover is not drawn twice
}

// TestAPhotosDay: a photo's own day is read from the original: the file keeps that day, and the page of that day shows
// the photo as its cover.
func TestAPhotosDay(t *testing.T) {
	l := emptyLife(t)
	e := l.do(t, "/", "add-file", map[string]string{"title": "2019-06-03 IMG_1.jpg", "original": pictureTakenOn(t, "IMG_1.jpg", "2019-06-03 07:41:12")})
	if day, _ := e.Properties.(map[string]any)["day"].(string); day != "2019-06-03" {
		t.Fatalf("the day of the photo is %q, want the day of its EXIF", day)
	}
	day := must(t)(l.api.Get(t.Context(), "/days/2019-06-03"))
	wantInPage(t, "the day page of the photo", pageAt(t, l.web, href(day, "page")), `<figure class="cover"><img src="`+href(e, "preview")+`"`)
}

// TestTheFileFormAsksForTheFileFirst: the file is the first field of New file, and it says what is kept.
func TestTheFileFormAsksForTheFileFirst(t *testing.T) {
	l := emptyLife(t)
	page := pageAt(t, l.web, "/browse?kind=media&new=add-file")
	form := regexp.MustCompile(`(?s)<form[^>]*action="/files".*?</form>`).FindString(page)
	file, title := strings.Index(form, `name="original"`), strings.Index(form, `name="title"`)
	if form == "" || file < 0 || title < 0 || file > title || !strings.Contains(form, "the original stays where it is") {
		t.Errorf("the file is not first, or the label does not say that the original stays:\n%.2500s", form)
	}
}

// TestTheCoverChooserLabelsTheNewPhotoOnce: the section says A new photo, so the field does not say it again.
func TestTheCoverChooserLabelsTheNewPhotoOnce(t *testing.T) {
	l := emptyLife(t)
	trip := l.do(t, "/", "create-page", map[string]string{"title": "Trip"})
	page := pageAt(t, l.web, href(trip, "self")+"/cover")
	if strings.Count(page, ">A new photo<") != 1 || strings.Contains(page, "never stored") {
		t.Errorf("the new photo is labelled twice, or the old label is back")
	}
}
