package web_test

// The test side by side (plan 161, stage 2). The core still draws the browser itself, and web draws it from the
// answers of the API. Over one synthetic file, for a list of routes that covers every template of the view and the shell,
// both give a page, and the pages are compared byte for byte. Stage 3 deletes the browser of the core and this test with it.

import (
	"html"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// importKey is the key of a capture or a reading in its form: 26 characters of crypto/rand that the view draws new at
// each draw, so a form sent twice is one write and the next draw is another (D28). It is the one part of a page that
// is not a function of its entity, so the test replaces it by the same word in both pages.
var importKey = regexp.MustCompile(`name="import_key" value="[A-Z2-7]{26}"`)

// receipt is the one-use receipt of a toast: 64 characters of crypto/rand in the address of the page that a write returns to.
var receipt = regexp.MustCompile(`feedback=[0-9a-f]{64}`)

// wallTime finds a time element of a page. The database stamps a row it creates with its own wall clock, and not with
// the fixed clock of the store, so two files made at two moments show two times. The fixed clock stands in 2031.
var wallTime = regexp.MustCompile(`<time datetime="[^"]*" title="[^"]*">[^<]*</time>`)

// withoutKeys replaces the parts of a page that are random or stamped by the wall clock: the key of a form, the receipt of
// a toast and the times of the rows that the test made. It is the same word on both sides of a comparison.
func withoutKeys(page string) string {
	page = importKey.ReplaceAllString(page, `name="import_key" value="KEY"`)
	page = receipt.ReplaceAllString(page, "feedback=RECEIPT")
	return wallTime.ReplaceAllStringFunc(page, func(t string) string {
		if strings.HasPrefix(t, `<time datetime="2031-`) {
			return t
		}
		return "<time>WALL</time>"
	})
}

// multipartForm finds, in a page of web, the form of a file: the page to return to (back) is in the query of its address,
// and not in a hidden field. Web streams the body of a file to the core and reads none of it (view.formView.ActionHref).
// Before 'its hidden fields' the page has the start tag; the tail of the line holds the fields that stay hidden.
var multipartForm = regexp.MustCompile(`(<form [^>]*action="[^"?]*)\?back=([^"]*)("[^>]*enctype="multipart/form-data"[^>]*>\n(?:<input type="hidden"[^>]*>)*)\n`)

// withBackInTheBody turns the one difference of the copy of the view into the page of the core: it moves back from the
// query of the address of a form of a file to the hidden field that the core draws, in its place.
func withBackInTheBody(page string) string {
	return multipartForm.ReplaceAllStringFunc(page, func(form string) string {
		m := multipartForm.FindStringSubmatch(form)
		back, _ := url.QueryUnescape(html.UnescapeString(m[2]))
		return m[1] + m[3] + `<input type="hidden" name="back" value="` + template.HTMLEscapeString(back) + `">` + "\n"
	})
}

// allowedHeaderDifferences are the headers that the two answers may not share, each with its reason and the check of
// the difference. The check gets the header of web and the header of the core, and says whether the difference is the
// one allowed. A difference that is not here fails the test.
var allowedHeaderDifferences = map[string]struct {
	reason string
	check  func(web, core []string) bool
}{
	"Vary": {"the core answers one URL as JSON or as HTML, so it sends Vary: Accept; web answers HTML only",
		func(web, core []string) bool { return len(web) == 0 && strings.Join(core, "|") == "Accept" }},
	"Cache-Control": {"every answer of a session but an asset is no-store (lock.go); the local handler of the core sets none for a page",
		func(web, core []string) bool { return strings.Join(web, "|") == "no-store" && len(core) == 0 }},
}

// allowedExtraHeaders are the headers that web may send and the core did not, each with its reason.
var allowedExtraHeaders = map[string]string{
	"Content-Length": "a short body of the core reaches web over a real connection, where the server of the core adds its length; the recorder of the core's side has none",
}

// routes covers every template of the view and the shell. A route that the core redirects is compared by its status
// and its Location.
func routes(l *life) []string {
	return []string{
		"/", "/?new=record&metric=Walk&value=1", "/?new=capture",
		"/days/" + today, "/days/2031-05-13", "/days/2031-05-12", "/days/2030-05-14", "/days/2031-05-01", "/days/today", "/days/" + today + "?new=rename",
		"/days", "/days?offset=2&limit=2",
		l.hrefs["river"], l.hrefs["sam"], l.hrefs["lake"], l.hrefs["river"] + "/cover", l.hrefs["lake"] + "?new=save-body",
		l.hrefs["weightPage"], l.hrefs["weightPage"] + "?new=edit-metric", l.hrefs["walkPage"], l.hrefs["convPage"], l.hrefs["convPage"] + "?new=rename",
		"/pages/999999", "/pages/notanumber",
		"/metrics", "/metrics/Weight", "/metrics/Weight?from=2031-05-12", "/metrics/Weight?new=edit-metric", "/metrics/Walk", "/metrics/NoSuch",
		l.hrefs["reading"], "/measurements/999999",
		"/browse", "/browse?kind=person", "/browse?kind=media", "/browse?kind=media&media=image", "/browse?kind=metric", "/browse?kind=document",
		"/browse?deleted=1", "/browse?in=Conversations", "/browse?unfiled=1", "/browse?ghosts=1", "/browse?kind=all", "/browse?new=create-page",
		"/search?q=heron", "/search?q=nothingwillmatchthis", "/search",
		"/months/2031-05", "/months/2031-04", "/weeks/today", "/weeks/2031-W20", "/years/2031", "/years/2030",
		"/actions", "/actions?new=rename&id=3", "/actions?new=capture", "/settings", "/integrity",
		"/days/today/conversation", "/days/" + today + "/conversation", "/days/2031-05-13/conversation", "/days/2031-05-10/conversation",
		"/days/2031-05-01/conversation", "/days/today/conversation?new=rename",
		"/tasks", "/with/none", "/nowhere", "/days/bogus",
	}
}

// TestPagesSideBySide: for every route, the page of web is the page of the core, with the same status, the same
// headers and the same bytes. The differences that the test allows are the entries of allowedHeaderDifferences.
func TestPagesSideBySide(t *testing.T) {
	l := richLife(t)
	for _, uri := range routes(l) {
		t.Run(uri, func(t *testing.T) {
			samePage(t, ask(l.web, http.MethodGet, uri), ask(l.core.Handler, http.MethodGet, uri))
		})
	}
}

// samePage fails the test when the answer of web is not the answer of the core: the status, the headers and the bytes. The
// differences that it allows are the entries of allowedHeaderDifferences and allowedExtraHeaders, the random keys (withoutKeys)
// and the place of back in a form of a file (withBackInTheBody).
func samePage(t *testing.T, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code {
		t.Fatalf("status %d, want %d\n%.300s", got.Code, want.Code, got.Body.String())
	}
	names := map[string]bool{}
	for name := range want.Header() {
		names[name] = true
	}
	for name := range got.Header() {
		names[name] = true
	}
	for name := range names {
		web, core := got.Header()[name], want.Header()[name]
		switch allowed, ok := allowedHeaderDifferences[name]; {
		case strings.Join(web, "|") == strings.Join(core, "|"):
		case ok && allowed.check(web, core):
		case len(core) == 0 && allowedExtraHeaders[name] != "":
		default:
			t.Errorf("header %s: web %q, the core %q", name, web, core)
		}
	}
	if g, w := withoutKeys(withBackInTheBody(got.Body.String())), withoutKeys(want.Body.String()); g != w {
		t.Errorf("the page differs from the page of the core:\n%s", firstDifference(g, w))
	}
	if want.Code == http.StatusOK && want.Body.Len() == 0 {
		t.Error("the page is empty")
	}
}

// firstDifference shows the place where two texts first differ, with a little of the text around it.
func firstDifference(got, want string) string {
	n := min(len(got), len(want))
	i := 0
	for i < n && got[i] == want[i] {
		i++
	}
	from := max(i-120, 0)
	return "web:  " + url.QueryEscape(got[from:min(i+160, len(got))]) + "\ncore: " + url.QueryEscape(want[from:min(i+160, len(want))])
}
