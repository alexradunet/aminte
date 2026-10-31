package web_test

// The test side by side (plan 161, stage 2). The core still draws the browser itself, and web draws it from the
// answers of the API. Over one synthetic file, for a list of routes that covers every template of the view and the shell,
// both give a page, and the pages are compared byte for byte. Stage 3 deletes the browser of the core and this test with it.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// importKey is the key of a capture or a reading in its form: 26 characters of crypto/rand that the view draws new at
// each draw, so a form sent twice is one write and the next draw is another (D28). It is the one part of a page that
// is not a function of its entity, so the test replaces it by the same word in both pages.
var importKey = regexp.MustCompile(`name="import_key" value="[A-Z2-7]{26}"`)

func withoutKeys(page string) string {
	return importKey.ReplaceAllString(page, `name="import_key" value="KEY"`)
}

// allowedHeaderDifferences are the headers that the two answers may not share, each with its reason. A difference that
// is not here fails the test.
var allowedHeaderDifferences = map[string]string{
	"Vary": "the core answers one URL as JSON or as HTML, so it sends Vary: Accept; web answers HTML only",
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
			want := ask(l.core.Handler, http.MethodGet, uri)
			got := ask(l.web, http.MethodGet, uri)
			if got.Code != want.Code {
				t.Fatalf("status %d, want %d\n%.300s", got.Code, want.Code, got.Body.String())
			}
			for name, values := range want.Header() {
				if _, allowed := allowedHeaderDifferences[name]; allowed {
					continue
				}
				if strings.Join(got.Header()[name], "|") != strings.Join(values, "|") {
					t.Errorf("header %s: %q, want %q", name, got.Header()[name], values)
				}
			}
			for name := range got.Header() {
				if _, ok := want.Header()[name]; !ok && allowedExtraHeaders[name] == "" {
					t.Errorf("header %s: %q, which the core does not send", name, got.Header()[name])
				}
			}
			if g, w := withoutKeys(got.Body.String()), withoutKeys(want.Body.String()); g != w {
				t.Errorf("the page differs from the page of the core:\n%s", firstDifference(g, w))
			}
			if want.Code == http.StatusOK && want.Body.Len() == 0 {
				t.Error("the page is empty")
			}
		})
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
