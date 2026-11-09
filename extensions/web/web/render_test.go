package web_test

// POST /render: the Preview of a form that writes a body. Web draws the field body (or text, which a capture sends) as
// its page will show it once saved. The page has no shell, it is not kept, and it asks the core for nothing and writes
// nothing. The expected words are constants, never the page of another program.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// renderedBody has a heading, a wikilink, a tag, raw HTML, a picture embed and a list: every kind of prose that the page
// of a body draws.
const renderedBody = "# Notes\n\nRan with [[Sam]] #running, <b>raw</b> and ![[IMG_0001.jpg]].\n\n- one\n- two"

// renderProse is the HTML that a page draws for a body: the inside of the element with the class prose, up to its end tag.
var renderProse = regexp.MustCompile(`(?s)<div class="prose">(.*?)</div>`)

// postRender posts the urlencoded form to /render as a browser does, and with the extra headers (name, value pairs).
func postRender(h http.Handler, form url.Values, header ...string) *httptest.ResponseRecorder {
	return postRenderRaw(h, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), header...)
}

func postRenderRaw(h http.Handler, contentType string, body io.Reader, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/render", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestRenderDrawsTheBodyAndWritesNothing: POST /render draws the field body (or text) as a page would show it saved. The
// page has no shell, nothing is cached, and nothing is asked of the core or written to it. A body that is not a form, a
// form from another origin and a body over the limit are refused with a page.
func TestRenderDrawsTheBodyAndWritesNothing(t *testing.T) {
	l := richLife(t)
	rec := &recorder{next: l.core.Handler}
	b := webOver(t, rec)
	for _, field := range []string{"body", "text"} {
		got := postRender(b, url.Values{field: {"# Heading\n\nA **bold** word and a [[River walk]] link. #birds"}})
		page := got.Body.String()
		if got.Code != http.StatusOK || got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Errorf("render of %s: %d, Cache-Control %q, Content-Type %q", field, got.Code, got.Header().Get("Cache-Control"), got.Header().Get("Content-Type"))
		}
		for _, want := range []string{"<h1>Heading</h1>", "<strong>bold</strong>", `<a href="/pages?title=River+walk" class="wikilink">River walk</a>`, "<h1>Preview</h1>"} {
			if !strings.Contains(page, want) {
				t.Errorf("render of %s lacks %s\n%.600s", field, want, page)
			}
		}
		if strings.Contains(page, `<section class="rail-today"`) || strings.Contains(page, `class="rail"`) {
			t.Errorf("render of %s has a shell", field)
		}
		if got.Header().Get("Speculation-Rules") != "" {
			t.Errorf("render of %s asks the browser to prerender", field)
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("render asked the core %d times, want none", n)
	}
	form := "application/x-www-form-urlencoded"
	for name, c := range map[string]struct {
		contentType string
		body        string
		header      []string
		want        int
	}{
		"json":         {"application/json", `{"body":"x"}`, nil, http.StatusUnsupportedMediaType},
		"multipart":    {"multipart/form-data; boundary=x", "--x--", nil, http.StatusUnsupportedMediaType},
		"cross-origin": {form, "body=x", []string{"Sec-Fetch-Site", "cross-site", "Origin", "https://elsewhere.example"}, http.StatusForbidden},
		"too large":    {form, "body=" + strings.Repeat("a", 20<<20), nil, http.StatusRequestEntityTooLarge},
	} {
		if got := postRenderRaw(b, c.contentType, strings.NewReader(c.body), c.header...); got.Code != c.want || !strings.Contains(got.Body.String(), "</html>") {
			t.Errorf("render, %s: %d, want %d with a page", name, got.Code, c.want)
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("a refused render asked the core %d times, want none", n)
	}
}

// TestRenderIsTheSavedPage: the preview of a body is a page whose prose is the prose that the page of the same body draws
// once saved. The raw HTML of the body is not passed on. A capture's text is previewed too, a GET has one decided answer,
// and a form that writes a body offers Preview in a new tab, before Save, so the form keeps its words.
func TestRenderIsTheSavedPage(t *testing.T) {
	l := richLife(t)
	page := l.do(t, "/", "create-page", map[string]string{"title": "Notes", "body": renderedBody})
	saved := ask(l.web, http.MethodGet, href(page, "self")).Body.String()

	got := postRender(l.web, url.Values{"body": {renderedBody}})
	preview := renderProse.FindStringSubmatch(got.Body.String())
	if got.Code != http.StatusOK || !strings.HasPrefix(got.Header().Get("Content-Type"), "text/html") || preview == nil {
		t.Fatalf("render: %d %q\n%.600s", got.Code, got.Header().Get("Content-Type"), got.Body.String())
	}
	if strings.Contains(preview[1], "<b>raw</b>") || !strings.Contains(preview[1], "<!-- raw HTML omitted -->raw<!-- raw HTML omitted -->") {
		t.Errorf("the raw HTML of the body reaches the preview:\n%s", preview[1])
	}
	for _, want := range []string{"<h1>Notes</h1>", `class="wikilink">Sam</a>`, `class="wikilink">#running</a>`, `<img src="/previews?title=IMG_0001.jpg"`, "<li>one</li>", "<li>two</li>"} {
		if !strings.Contains(preview[1], want) {
			t.Errorf("the preview lacks %s:\n%s", want, preview[1])
		}
	}
	if !strings.Contains(got.Body.String(), "<h1>Preview</h1>") || strings.Contains(got.Body.String(), `class="rail"`) {
		t.Errorf("the preview has no title of its own, or it has the rail:\n%.600s", got.Body.String())
	}
	if !strings.Contains(saved, `<div class="prose">`+preview[1]+`</div>`) {
		t.Errorf("the preview is not the saved page:\npreview %s\npage %.2000s", preview[1], saved)
	}
	const button = `<button type="submit" class="quiet" formaction="/render" formmethod="POST" formenctype="application/x-www-form-urlencoded" formtarget="_blank">Preview</button><button class="primary">Save</button>`
	if !strings.Contains(saved, button) {
		t.Errorf("the editor of the page offers no Preview before Save:\n%.2000s", saved)
	}
	if text := postRender(l.web, url.Values{"text": {"A *capture*"}}); !strings.Contains(text.Body.String(), "<em>capture</em>") {
		t.Error("the text of a capture is not previewed")
	}

	// A GET has one answer: 405, with the one method that the route takes. Web decides it, and the core is not asked.
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut} {
		if got := ask(l.web, method, "/render?body=x"); got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "POST" {
			t.Errorf("%s /render: %d, Allow %q, want 405 with POST", method, got.Code, got.Header().Get("Allow"))
		}
	}
}

// TestEveryTextAreaOfTheCatalogIsBodyOrText: a form with a text area offers Preview (view, TestPreviewKeysOnATextArea),
// which sends the form to POST /render, and /render reads the field body or text and no other. So each text area of the
// catalog of the real core has one of these two names.
func TestEveryTextAreaOfTheCatalogIsBodyOrText(t *testing.T) {
	l := emptyLife(t)
	catalog, err := l.api.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	areas := 0
	for _, a := range catalog {
		for _, f := range a.Fields {
			if f.Type != "textarea" {
				continue
			}
			areas++
			if f.Name != "body" && f.Name != "text" {
				t.Errorf("%s: the text area %s is not body or text, which POST /render reads", a.Name, f.Name)
			}
		}
	}
	if areas == 0 {
		t.Error("the catalog has no text area: the test checks nothing")
	}
}
