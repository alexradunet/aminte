package web_test

// The forms side by side (plan 161, stage 2). The browser of the core still takes a form and answers with a redirect.
// For each form below, two synthetic files have the same history. The form is posted to the browser of the core with
// Accept: text/html on the first file, and to web on the second. Both answer with a redirect, or with the page of an error.
// The test follows the redirect on each side and compares the final pages, the status and the headers, with the same
// allowed differences as the test of the pages (samePage). The receipt of the toast and the key of a form are random, and
// the test replaces them by one word on both sides. Stage 3 deletes this test with the browser of the core.
//
// The allowed differences of the writes, each with its reason:
//   - A form of a file has its page to return to (back) in the query of its address, and not in a hidden field
//     (withBackInTheBody). Web streams the body of a file and does not read it.
//   - A refused form of a file does not show "What you sent": the text is in the body, and web does not read it
//     (TestARefusedFileFormKeepsNoText).
//   - The settings page has the form "This browser" when the core has the app web, and the core has not (the core of
//     this test has none): TestTheSettingsPageHasTheFormOfThisBrowser.

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
)

// formSent is a form of a browser: the address it posts to, its text (urlencoded) or its file (multipart), and the page it
// is on, which a browser names in the Referer.
type formSent struct {
	path    string
	text    url.Values  // a form of text
	file    *fileSent   // a form of a file, when it is not nil
	from    string      // the page the form is on
	headers [][2]string // more headers: the origin of a cross-site form
}

// fileSent is a form of a file: its text fields, its back and its original.
type fileSent struct {
	fields   [][2]string
	back     string
	original []byte
}

// deliver posts the form to h as a browser does. toWeb says that h is web: a form of a file then carries back in the
// query of its address, as the view of web draws it, and not in the body, as the view of the core draws it.
func deliver(h http.Handler, f formSent, toWeb bool) *httptest.ResponseRecorder {
	path, body, contentType := f.path, "", "application/x-www-form-urlencoded"
	var raw []byte
	if f.file == nil {
		body = f.text.Encode()
	} else {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if f.file.back != "" && !toWeb {
			mw.WriteField("back", f.file.back)
		} else if f.file.back != "" {
			path += "?back=" + url.QueryEscape(f.file.back)
		}
		for _, field := range f.file.fields {
			mw.WriteField(field[0], field[1])
		}
		part, _ := mw.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="original"; filename="Heron.jpg"`}, "Content-Type": {"image/jpeg"}})
		part.Write(f.file.original)
		mw.Close()
		raw, contentType = buf.Bytes(), mw.FormDataContentType()
	}
	var req *http.Request
	if raw != nil {
		req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	} else {
		req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	if f.from != "" {
		req.Header.Set("Referer", "http://example.com"+f.from)
	}
	for _, h := range f.headers {
		req.Header.Set(h[0], h[1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// bothAnswer posts f to the browser of the core of old and to web of fresh, and compares what the browser sees: a redirect
// to the same address, then the same page there; or the same page of an error.
func bothAnswer(t *testing.T, old, fresh *life, f formSent) {
	t.Helper()
	want, got := deliver(old.core.Handler, f, false), deliver(fresh.web, f, true)
	if got.Code != want.Code {
		t.Fatalf("status %d, the core %d\n%.400s", got.Code, want.Code, got.Body.String())
	}
	t.Logf("both answer %d %s", want.Code, want.Header().Get("Location"))
	if want.Code != http.StatusSeeOther {
		samePage(t, got, want)
		return
	}
	g, w := withoutKeys(got.Header().Get("Location")), withoutKeys(want.Header().Get("Location"))
	if g != w {
		t.Fatalf("redirect to %q, the core to %q", g, w)
	}
	samePage(t, ask(fresh.web, http.MethodGet, got.Header().Get("Location")), ask(old.core.Handler, http.MethodGet, want.Header().Get("Location")))
}

// TestFormsSideBySide: the forms of the browser, one by one, in order, on two files with the same history.
func TestFormsSideBySide(t *testing.T) {
	old, fresh := richLife(t), richLife(t)
	river, sam, lake := old.hrefs["river"], old.hrefs["sam"], old.hrefs["lake"]
	if fresh.hrefs["river"] != river || fresh.hrefs["lake"] != lake {
		t.Fatalf("the two files differ: %v and %v", old.hrefs, fresh.hrefs)
	}
	bio := "A **new** body about the [[Sam]] and a [[Nobody Here]] link. #birds"
	for _, c := range []struct {
		name string
		form formSent
	}{
		{"capture, back to the day", formSent{path: "/days/" + today + "/capture", from: "/days/" + today,
			text: url.Values{"text": {"Walked by the water. [[Sam]] came. #birds [[bad/name]]"}, "mood": {"4"}, "import_key": {"KEY"}, "back": {"/days/" + today}}}},
		{"capture, no back", formSent{path: "/days/2031-05-12/capture", from: "/days/2031-05-12",
			text: url.Values{"text": {"A second line."}, "import_key": {"KEY2"}}}},
		{"capture, a back that is no path", formSent{path: "/days/2031-05-11/capture", from: "/days/2031-05-11",
			text: url.Values{"text": {"A third line."}, "import_key": {"KEY3"}, "back": {"//evil.example/x"}}}},
		{"record", formSent{path: "/measurements", from: "/metrics/Weight",
			text: url.Values{"metric": {"Weight"}, "day": {today}, "value": {"70.9"}, "import_key": {"KEY4"}, "back": {"/metrics/Weight#row-1"}}}},
		{"record, a metric that is not registered", formSent{path: "/measurements", from: "/",
			text: url.Values{"metric": {"Nothing"}, "day": {today}, "value": {"1"}, "back": {"/"}}}},
		{"save-body", formSent{path: river + "/body", from: river, text: url.Values{"body": {bio}, "version": {fresh.versionOf(t, river)}, "back": {river}}}},
		{"save-body with a stale version", formSent{path: river + "/body", from: river, text: url.Values{"body": {bio + "\n\nThe words of the owner."}, "version": {"0"}, "back": {river}}}},
		{"rename", formSent{path: river + "/rename", from: river, text: url.Values{"title": {"River path"}, "back": {"/pages/3"}}}},
		{"rename to a title in use", formSent{path: river + "/rename", from: river, text: url.Values{"title": {"Sam"}}}},
		{"tombstone-pages, the pick bar", formSent{path: "/pages/tombstone", from: "/browse?kind=person",
			text: url.Values{"ids": {strings.TrimPrefix(sam, "/pages/")}, "category": {"Friends"}, "from": {""}, "move": {"1"}, "back": {"/browse?kind=person"}}}},
		{"tombstone-pages with a field of no form", formSent{path: "/pages/tombstone", from: "/browse",
			text: url.Values{"ids": {"3"}, "bogus": {"1"}, "back": {"/browse"}}}},
		{"file-pages", formSent{path: "/pages/file", from: "/browse", text: url.Values{"ids": {"3"}, "category": {"Walks"}, "back": {"/browse"}}}},
		{"a field that the action does not list", formSent{path: "/days/" + today + "/capture", from: "/days/" + today,
			text: url.Values{"text": {"Words that must come back."}, "bogus": {"1"}, "back": {"/days/" + today}}}},
		{"a value outside the options", formSent{path: "/days/" + today + "/capture", from: "/days/" + today,
			text: url.Values{"text": {"Words that must come back."}, "mood": {"9"}}}},
		{"a page that is not there", formSent{path: "/pages/999999/rename", from: "/pages/999999", text: url.Values{"title": {"X"}}}},
		{"a route that is not there", formSent{path: "/nowhere", from: "/", text: url.Values{"a": {"b"}}}},
		{"a method that the route does not take", formSent{path: "/days", from: "/days", text: url.Values{"a": {"b"}}}},
		{"a write from a foreign origin", formSent{path: "/days/" + today + "/capture", from: "/days/" + today,
			text:    url.Values{"text": {"csrf"}},
			headers: [][2]string{{"Sec-Fetch-Site", "cross-site"}, {"Origin", "https://evil.example"}}}},
		{"add-file, a picture", formSent{path: "/files", from: "/browse?new=add-file", file: &fileSent{
			fields: [][2]string{{"title", "Heron.jpg"}, {"body", "A heron at dawn."}, {"day", today}}, back: "/browse?kind=media", original: syntheticJPEG(t)}}},
		{"add-file, the same picture", formSent{path: "/files", from: "/browse?new=add-file", file: &fileSent{
			fields: [][2]string{{"title", "Heron.jpg"}, {"day", today}}, original: syntheticJPEG(t)}}},
		{"set-cover", formSent{path: river + "/cover", from: river + "/cover", file: &fileSent{
			fields: [][2]string{{"title", "A cover"}}, back: river, original: syntheticJPEG(t)}}},
		{"a file sent to an action with no file field", formSent{path: "/days/" + today + "/capture", from: "/days/" + today, file: &fileSent{
			fields: [][2]string{{"text", "Words."}}, original: syntheticJPEG(t)}}},
		{"add-file, a field that it does not list", formSent{path: "/files", from: "/browse", file: &fileSent{
			fields: [][2]string{{"title", "X.jpg"}, {"bogus", "1"}}, back: "/browse", original: syntheticJPEG(t)}}},
	} {
		t.Run(c.name, func(t *testing.T) { bothAnswer(t, old, fresh, c.form) })
	}
}

// versionOf is the version of the page at href, as a form carries it.
func (l *life) versionOf(t *testing.T, href string) string {
	t.Helper()
	page := must(t)(l.api.Get(t.Context(), href))
	v, _ := page.Properties.(map[string]any)["version"].(string)
	if v == "" {
		t.Fatalf("the page %s has no version", href)
	}
	return v
}
