package web_test

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog-web/view"
	"lifelog-web/web"
	"lifelog/siren"
)

// recorder is a handler between web and the real core. It records the requests that web makes, and passes each to the
// core. The test then asserts what web asked for, and the core answers.
type recorder struct {
	next http.Handler
	mu   sync.Mutex
	seen []*http.Request
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.seen = append(r.seen, req.Clone(context.Background()))
	r.mu.Unlock()
	r.next.ServeHTTP(w, req)
}

func (r *recorder) requests() []*http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*http.Request(nil), r.seen...)
}

// webOver is a web in front of h, which stands for the core.
func webOver(t *testing.T, h http.Handler, token string) *web.Browser {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	b, err := web.New(web.Options{CoreURL: server.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestWebAsksTheCoreForSirenAsTheUI: a page is one read of the core for the page and one for the shell, and one for each
// card of the column. Each is a GET that asks for Siren JSON with the bearer token, as the writer ui; the reads of the
// cards are the writer agent:embed. The query that the browser sent goes to the core, without the receipt of the toast
// (feedback), which web alone reads.
func TestWebAsksTheCoreForSirenAsTheUI(t *testing.T) {
	l := richLife(t)
	rec := &recorder{next: l.core.Handler}
	b := webOver(t, rec, "synthetic-token-synthetic-token-1234")
	if page := ask(b, http.MethodGet, "/browse?kind=person&feedback=once&unfiled=1"); page.Code != http.StatusOK {
		t.Fatalf("GET /browse: %d", page.Code)
	}
	var cards, ui int
	for _, req := range rec.requests() {
		if req.Method != http.MethodGet {
			t.Errorf("%s %s: web reads with GET only", req.Method, req.URL)
		}
		if got := req.Header.Get("Accept"); got != "application/vnd.siren+json" {
			t.Errorf("%s accepts %q, want Siren JSON", req.URL, got)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer synthetic-token-synthetic-token-1234" {
			t.Errorf("%s sends the credential %q", req.URL, got)
		}
		switch got := req.Header.Get(siren.SourceHeader); got {
		case "ui":
			ui++
		case "agent:embed":
			cards++
		default:
			t.Errorf("%s is read as %q, want ui or agent:embed", req.URL, got)
		}
	}
	if ui != 2 || cards != 6 {
		t.Errorf("%d reads as ui and %d as agent:embed, want 2 (the page and the shell) and 6 (the cards of the column)", ui, cards)
	}
	if first := rec.requests()[0].URL; first.Path != "/browse" || first.RawQuery != "kind=person&unfiled=1" {
		t.Errorf("the page was read as %s, want /browse?kind=person&unfiled=1", first)
	}
}

// cardKinds are the kinds of the cards of a page, in the order they are drawn, with the address of each: "day /days/2031-05-01".
func cardKinds(page string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<a class="card card-([a-z]+)" href="([^"]*)">`).FindAllStringSubmatch(page, -1) {
		out = append(out, m[1]+" "+m[2])
	}
	return out
}

// TestTheColumnDrawsSixCardsAndTheScreenDrawsEveryOne: two turns name six days each. The conversation view draws a
// card for each of the twelve. The column of any other page draws a card for the six of the newest turn, and the plain
// link for the six of the older turn.
func TestTheColumnDrawsSixCardsAndTheScreenDrawsEveryOne(t *testing.T) {
	l := emptyLife(t)
	day := func(n int) string { return fmt.Sprintf("/days/2031-05-%02d", n) }
	var older, newer []string
	for n := 1; n <= 6; n++ {
		older, newer = append(older, day(n)), append(newer, day(n+6))
	}
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "Show me"})
	conv := must(t)(l.api.Get(t.Context(), "/days/"+today+"/conversation"))
	page := must(t)(l.api.Get(t.Context(), href(conv, "page")))
	body, _ := page.Properties.(map[string]any)["body"].(string)
	must(t)(l.api.Do(t.Context(), act(t, page, "save-body"), map[string]string{"body": body + "\n\n" + toolLines(older...) +
		"\n**lifelog** (09:30): First.\n\n**Owner** (09:31): More.\n\n" + toolLines(newer...) + "\n**lifelog** (09:32): Second.\n"}))

	var every, column []string
	for _, ref := range append(older, newer...) { // the page shows the turns in order, the older first
		every, column = append(every, "day "+ref), append(column, "link "+ref)
	}
	for i := range 6 {
		column[6+i] = every[6+i] // the newer turn
	}
	if got := cardKinds(ask(l.web, http.MethodGet, "/days/today/conversation").Body.String()); !slices.Equal(got, every) {
		t.Errorf("the conversation view:\n%v\nwant\n%v", got, every)
	}
	if got := cardKinds(ask(l.web, http.MethodGet, "/browse").Body.String()); !slices.Equal(got, column) {
		t.Errorf("the column:\n%v\nwant\n%v", got, column)
	}
}

// TestTheTurnDrawsTheThingsTheAgentTouched: the tool lines of a turn name a day, a page, a person, a metric, a search,
// an address that answers 404 and, as the seventh address of the turn, today by its word. The first five are cards,
// the 404 is the plain link, and so is the seventh address: the server names six addresses of a turn.
func TestTheTurnDrawsTheThingsTheAgentTouched(t *testing.T) {
	l := richLife(t)
	want := []string{"day /days/2031-05-14", "page /pages/3", "person /pages/2", "series /metrics/Weight", "search /search?q=heron", "link /pages/999999", "link /days/today"}
	for _, uri := range []string{"/days/today/conversation", "/browse"} {
		if got := cardKinds(ask(l.web, http.MethodGet, uri).Body.String()); !slices.Equal(got, want) {
			t.Errorf("GET %s draws %v, want %v", uri, got, want)
		}
	}
}

// TestACardAsksForWhatThePageMayRead: a tool line that names an owner-only read draws the plain link, as the core's
// own page does. The card is read as agent:embed, which the core refuses the read of the settings and of the integrity check.
func TestACardAsksForWhatThePageMayRead(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "Settings?"})
	conv := must(t)(l.api.Get(t.Context(), "/days/"+today+"/conversation"))
	page := must(t)(l.api.Get(t.Context(), href(conv, "page")))
	body, _ := page.Properties.(map[string]any)["body"].(string)
	must(t)(l.api.Do(t.Context(), act(t, page, "save-body"), map[string]string{"body": body + "\n\n" + toolLines("/settings", "/integrity") + "\n**lifelog** (09:30): No.\n"}))
	for _, uri := range []string{"/days/today/conversation", "/browse"} {
		got := ask(l.web, http.MethodGet, uri).Body.String()
		if strings.Contains(got, "model_key") || strings.Contains(got, "card-settings") || strings.Contains(got, "card-integrity") {
			t.Errorf("%s draws an owner-only read", uri)
		}
		for _, ref := range []string{"/settings", "/integrity"} {
			if want := `<a class="card card-link" href="` + ref + `">`; !strings.Contains(got, want) && uri == "/days/today/conversation" {
				t.Errorf("%s lacks the plain link %s", uri, want)
			}
		}
	}
}

// TestAStreamKeepsItsTypeAndItsBytes: a picture that the core serves reaches the browser with its status, its type and
// its bytes, and a HEAD gets the headers alone.
func TestAStreamKeepsItsTypeAndItsBytes(t *testing.T) {
	l := richLife(t)
	for _, uri := range []string{l.hrefs["lake"] + "/preview", "/previews?title=" + url.QueryEscape(l.picture), l.hrefs["lake"] + "/preview?x=1"} {
		want, got := ask(l.core.Handler, http.MethodGet, uri), ask(l.web, http.MethodGet, uri)
		if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/jpeg" || got.Body.Len() == 0 {
			t.Errorf("GET %s: %d %q, %d bytes; want a JPEG", uri, got.Code, got.Header().Get("Content-Type"), got.Body.Len())
		}
		if got.Body.String() != want.Body.String() {
			t.Errorf("GET %s: the bytes differ from the bytes of the core", uri)
		}
		for _, name := range []string{"Content-Type", "Content-Length", "Cache-Control"} {
			if got.Header().Get(name) != want.Header().Get(name) {
				t.Errorf("GET %s: %s is %q, want %q", uri, name, got.Header().Get(name), want.Header().Get(name))
			}
		}
		head := ask(l.web, http.MethodHead, uri)
		if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "image/jpeg" {
			t.Errorf("HEAD %s: %d, %d bytes, %q; want the headers of the picture and no body", uri, head.Code, head.Body.Len(), head.Header().Get("Content-Type"))
		}
	}
	// A picture that does not exist is the error entity of the core, drawn as a page with the status of the core.
	if got := ask(l.web, http.MethodGet, "/pages/999999/preview"); got.Code != http.StatusNotFound || !strings.Contains(got.Body.String(), "</html>") {
		t.Errorf("a picture that is not there: %d, want a 404 page", got.Code)
	}
}

// fileCore is a core that serves what the real core serves rarely: a file download with a Content-Disposition, an ETag
// and a modification time, that takes a range; and the answers a wrong core could give. It is a fake of the boundary,
// for the cases that the real core cannot make.
func fileCore(t *testing.T) (*web.Browser, *recorder) {
	t.Helper()
	body := strings.Repeat("0123456789", 100)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /snapshots/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.sqlite3")
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("name")+`"`)
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("ETag", `"v1"`)
		http.ServeContent(w, r, "x", time.Date(2031, 5, 14, 0, 0, 0, 0, time.UTC), strings.NewReader(body))
	})
	mux.HandleFunc("GET /away", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "//elsewhere.example/x", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /backslash", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, `/\elsewhere.example`, http.StatusSeeOther)
	})
	mux.HandleFunc("GET /garbage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		io.WriteString(w, "{not json")
	})
	rec := &recorder{next: mux}
	return webOver(t, rec, ""), rec
}

// TestAFileKeepsItsHeadersAndTakesARange: the headers that describe a download reach the browser, and a conditional or
// range request of the browser reaches the core, so the browser can resume a download.
func TestAFileKeepsItsHeadersAndTakesARange(t *testing.T) {
	b, rec := fileCore(t)
	got := ask(b, http.MethodGet, "/snapshots/life-2031-05-14.db")
	for name, want := range map[string]string{"Content-Type": "application/vnd.sqlite3", "Content-Disposition": `attachment; filename="life-2031-05-14.db"`,
		"Cache-Control": "private, no-cache", "Etag": `"v1"`, "Content-Length": "1000", "Accept-Ranges": "bytes"} {
		if got.Header().Get(name) != want {
			t.Errorf("%s is %q, want %q", name, got.Header().Get(name), want)
		}
	}
	if got.Header().Get("Last-Modified") == "" {
		t.Error("no Last-Modified")
	}
	req := httptest.NewRequest(http.MethodGet, "/snapshots/life-2031-05-14.db", nil)
	req.Header.Set("Range", "bytes=10-19")
	part := httptest.NewRecorder()
	b.ServeHTTP(part, req)
	if part.Code != http.StatusPartialContent || part.Body.String() != "0123456789" || part.Header().Get("Content-Range") != "bytes 10-19/1000" {
		t.Errorf("a range: %d %q %q, want 206, ten bytes, bytes 10-19/1000", part.Code, part.Body.String(), part.Header().Get("Content-Range"))
	}
	req = httptest.NewRequest(http.MethodGet, "/snapshots/life-2031-05-14.db", nil)
	req.Header.Set("If-None-Match", `"v1"`)
	same := httptest.NewRecorder()
	b.ServeHTTP(same, req)
	if same.Code != http.StatusNotModified {
		t.Errorf("a conditional request for a file that did not change: %d, want 304", same.Code)
	}
	if got := rec.requests()[1].Header.Get("Range"); got != "bytes=10-19" {
		t.Errorf("the core was asked for the range %q", got)
	}
}

// TestACoreThatSendsWhatIsNotAnAnswerIsAnErrorPage: a redirect that leaves the core (to another host), and a Siren answer
// that is not JSON, are 502: web sends the browser nowhere that the core did not name as a path, and draws no half entity.
func TestACoreThatSendsWhatIsNotAnAnswerIsAnErrorPage(t *testing.T) {
	b, _ := fileCore(t)
	for _, uri := range []string{"/away", "/backslash", "/garbage"} {
		got := ask(b, http.MethodGet, uri)
		if got.Code != http.StatusBadGateway || got.Header().Get("Location") != "" || !strings.Contains(got.Body.String(), "</html>") {
			t.Errorf("GET %s: %d to %q, want a 502 page and no redirect", uri, got.Code, got.Header().Get("Location"))
		}
	}
}

// TestACoreThatIsDownIsAnErrorPage: nothing listens at the address of the core. The browser gets a 503 page with a
// Retry-After, and not a panic or an empty answer. The page names no address.
func TestACoreThatIsDownIsAnErrorPage(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	b, err := web.New(web.Options{CoreURL: "http://" + addr})
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{"/", "/days/2031-05-14", "/pages/1/preview"} {
		got := ask(b, http.MethodGet, uri)
		if got.Code != http.StatusServiceUnavailable || got.Header().Get("Retry-After") != "1" || !strings.Contains(got.Body.String(), "</html>") {
			t.Errorf("GET %s: %d, Retry-After %q; want a 503 page with Retry-After 1", uri, got.Code, got.Header().Get("Retry-After"))
		}
		if strings.Contains(got.Body.String(), addr) || got.Header().Get("Speculation-Rules") != "" {
			t.Errorf("GET %s: the page names the address of the core, or asks for speculation", uri)
		}
	}
}

// TestAContextThatEndsStopsTheRequestToTheCore: the core waits for the end of the request. When the browser goes away,
// the core sees its request end. When the request has a deadline that passes, the browser gets 504.
func TestAContextThatEndsStopsTheRequestToTheCore(t *testing.T) {
	arrived, stopped := make(chan struct{}, 1), make(chan struct{}, 1)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		select {
		case stopped <- struct{}{}:
		default:
		}
	}))
	t.Cleanup(core.Close)
	b, err := web.New(web.Options{CoreURL: core.URL})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		b.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/days/2031-05-14", nil))
		done <- rec
	}()
	<-arrived
	cancel()
	select {
	case rec := <-done:
		if rec.Body.Len() != 0 {
			t.Errorf("a browser that went away got %d bytes", rec.Body.Len())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("web still waits for the core after the end of the request")
	}
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the core did not see the request end")
	}

	late, stop := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer stop()
	rec := httptest.NewRecorder()
	b.ServeHTTP(rec, httptest.NewRequestWithContext(late, http.MethodGet, "/days/2031-05-14", nil))
	if rec.Code != http.StatusGatewayTimeout || !strings.Contains(rec.Body.String(), "</html>") {
		t.Errorf("a request with a deadline that passed: %d, want a 504 page", rec.Code)
	}
}

// TestAPageTakesNoEmbedAndAWriteTakesNoPost: embed is a parameter of the Siren answer, and the forms are the next part
// of web: both are refused with their reason, and the core is not asked.
func TestAPageTakesNoEmbedAndAWriteTakesNoPost(t *testing.T) {
	l := richLife(t)
	rec := &recorder{next: l.core.Handler}
	b := webOver(t, rec, "")
	if got := ask(b, http.MethodGet, "/days?embed=ref"); got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "embed") {
		t.Errorf("a page with embed: %d, want 422 with its reason", got.Code)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		got := ask(b, method, "/days/2031-05-14/capture")
		if got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: %d, Allow %q, want 405 with GET, HEAD", method, got.Code, got.Header().Get("Allow"))
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("a refused request made %d reads of the core, want none", n)
	}
}

// TestADeletedMetricStaysAPage: the page of a live metric is the series, and the page of a deleted metric is a page.
func TestADeletedMetricStaysAPage(t *testing.T) {
	l := richLife(t)
	page := l.hrefs["walkPage"]
	if got := ask(l.web, http.MethodGet, page); got.Code != http.StatusSeeOther || got.Header().Get("Location") != "/metrics/Walk" {
		t.Fatalf("the page of a live metric: %d to %q, want 303 to /metrics/Walk", got.Code, got.Header().Get("Location"))
	}
	l.do(t, "/browse", "tombstone-pages", map[string]string{"ids": strings.TrimPrefix(page, "/pages/")})
	if e := must(t)(l.api.Get(t.Context(), page)); !slices.Contains(e.Class, "deleted") {
		t.Fatalf("the page is not deleted: %v", e.Class)
	}
	want, got := ask(l.core.Handler, http.MethodGet, page), ask(l.web, http.MethodGet, page)
	if got.Code != http.StatusOK || got.Code != want.Code {
		t.Errorf("the page of a deleted metric: %d, the core %d, want 200", got.Code, want.Code)
	}
	if withoutKeys(got.Body.String()) != withoutKeys(want.Body.String()) {
		t.Errorf("the page of a deleted metric differs from the page of the core:\n%s", firstDifference(withoutKeys(got.Body.String()), withoutKeys(want.Body.String())))
	}
}

// TestTheOwnFilesAreTheFilesOfTheCore: web serves the files of the browser itself. Each has the type, the cache and the
// bytes that the core's own route gives, and a font that does not exist is a 404.
func TestTheOwnFilesAreTheFilesOfTheCore(t *testing.T) {
	l := emptyLife(t)
	paths := []string{"/manifest.webmanifest", "/assets/speculation.json", "/assets/chat.js", "/assets/lock.js", "/assets/icon.svg",
		"/assets/icon-192.png", "/assets/icon-512.png", "/assets/wood.png", "/assets/ground.png", "/assets/embers.png", "/assets/sprites.png", "/assets/icons.png"}
	fonts, err := fs.ReadDir(view.Files, "html/fonts")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fonts {
		if name := f.Name(); strings.HasSuffix(name, ".woff2") || strings.HasSuffix(name, ".txt") {
			paths = append(paths, "/assets/fonts/"+name)
		}
	}
	if len(paths) < 14 {
		t.Fatalf("%d files: the fonts are not found", len(paths))
	}
	for _, p := range paths {
		want, got := ask(l.core.Handler, http.MethodGet, p), ask(l.web, http.MethodGet, p)
		if got.Code != http.StatusOK || got.Body.Len() == 0 || got.Body.String() != want.Body.String() {
			t.Errorf("GET %s: %d, %d bytes; the core gave %d, %d bytes", p, got.Code, got.Body.Len(), want.Code, want.Body.Len())
		}
		for _, name := range []string{"Content-Type", "Cache-Control", "X-Content-Type-Options"} {
			if got.Header().Get(name) != want.Header().Get(name) || got.Header().Get(name) == "" {
				t.Errorf("GET %s: %s is %q, the core's is %q", p, name, got.Header().Get(name), want.Header().Get(name))
			}
		}
	}
	for _, p := range []string{"/assets/fonts/nothing.woff2", "/assets/fonts/x.exe", "/assets/fonts/..%2Fapp.css"} {
		if got := ask(l.web, http.MethodGet, p); got.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", p, got.Code)
		}
	}
}

// TestRenderDrawsTheBodyAndWritesNothing: POST /render draws the field body (or text) as a page would show it saved. The
// page is the page of the core, it has no shell, nothing is cached, and nothing is asked of the core or written to it.
func TestRenderDrawsTheBodyAndWritesNothing(t *testing.T) {
	l := richLife(t)
	rec := &recorder{next: l.core.Handler}
	b := webOver(t, rec, "")
	post := func(h http.Handler, contentType, body string, header ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/render", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out
	}
	form := "application/x-www-form-urlencoded"
	for _, field := range []string{"body", "text"} {
		values := url.Values{field: {"# Heading\n\nA **bold** word and a [[River walk]] link. #birds"}}.Encode()
		want, got := post(l.core.Handler, form, values), post(b, form, values)
		if got.Code != http.StatusOK || got.Header().Get("Cache-Control") != "no-store" || !strings.Contains(got.Body.String(), "<h1>Heading</h1>") ||
			!strings.Contains(got.Body.String(), "<strong>bold</strong>") {
			t.Errorf("render of %s: %d, %q\n%.300s", field, got.Code, got.Header().Get("Cache-Control"), got.Body.String())
		}
		if got.Body.String() != want.Body.String() || got.Header().Get("Content-Type") != want.Header().Get("Content-Type") {
			t.Errorf("render of %s differs from the page of the core:\n%s", field, firstDifference(got.Body.String(), want.Body.String()))
		}
		if strings.Contains(got.Body.String(), `<section class="rail-today"`) || strings.Contains(got.Body.String(), `id="chat"`) {
			t.Errorf("render of %s has a shell", field)
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("render asked the core %d times, want none", n)
	}
	for name, c := range map[string]struct {
		contentType, body string
		header            []string
		want              int
	}{
		"json":         {"application/json", `{"body":"x"}`, nil, http.StatusUnsupportedMediaType},
		"multipart":    {"multipart/form-data; boundary=x", "--x--", nil, http.StatusUnsupportedMediaType},
		"cross-origin": {form, "body=x", []string{"Sec-Fetch-Site", "cross-site", "Origin", "https://elsewhere.example"}, http.StatusForbidden},
		"too large":    {form, "body=" + strings.Repeat("a", 20<<20), nil, http.StatusRequestEntityTooLarge},
	} {
		if got := post(b, c.contentType, c.body, c.header...); got.Code != c.want || !strings.Contains(got.Body.String(), "</html>") {
			t.Errorf("render, %s: %d, want %d with a page", name, got.Code, c.want)
		}
	}
}

// TestNewRefusesAnAddressThatIsNotTheCore: the address must be http or https with a host and nothing else.
func TestNewRefusesAnAddressThatIsNotTheCore(t *testing.T) {
	for _, base := range []string{"", "127.0.0.1:7777", "ftp://127.0.0.1", "http://", "http://user:pass@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?x=1", "http://127.0.0.1#x"} {
		if _, err := web.New(web.Options{CoreURL: base}); err == nil {
			t.Errorf("New(%q) is accepted", base)
		}
	}
	if _, err := web.New(web.Options{CoreURL: "http://127.0.0.1:7777/"}); err != nil {
		t.Errorf("New with a trailing slash: %v", err)
	}
	if _, err := web.New(web.Options{CoreURL: "http://127.0.0.1:7777", Token: "two\nlines"}); err == nil {
		t.Error("New with a token of two lines is accepted")
	}
}

// TestTheScriptsAreSmallAndAlone: the rule of the scripts (README.md). A page of the shell carries the one script
// chat.js, and lock.js only when the lock is on, which it is not until the browser keeps its own settings. Each script
// is under 80 lines. The forms of the pages work without them, so the page has no script that a form needs.
func TestTheScriptsAreSmallAndAlone(t *testing.T) {
	l := richLife(t)
	const chat = `<script src="/assets/chat.js" defer></script>`
	for _, uri := range []string{"/", "/browse", "/days/today/conversation", "/pages/999999", "/settings", "/actions"} {
		page := ask(l.web, http.MethodGet, uri).Body.String()
		if strings.Count(page, "<script") != 1 || !strings.Contains(page, chat) {
			t.Errorf("GET %s has %d script elements, want %s alone", uri, strings.Count(page, "<script"), chat)
		}
		if strings.Contains(page, "lock.js") {
			t.Errorf("GET %s has the script of the lock, and the lock is off", uri)
		}
	}
	for _, name := range []string{"chat.js", "lock.js"} {
		if lines := strings.Count(string(view.MustFile(name)), "\n"); lines >= 80 {
			t.Errorf("%s has %d lines, want under 80", name, lines)
		}
	}
}

// TestAHeadGetsTheHeadersOfThePage: a HEAD of a page, of a redirect and of a file gets the status and the headers that
// the GET gets, and no body.
func TestAHeadGetsTheHeadersOfThePage(t *testing.T) {
	l := richLife(t)
	for _, uri := range []string{"/days/2031-05-14", "/days/today", "/pages/999999", "/assets/chat.js"} {
		get, head := ask(l.web, http.MethodGet, uri), ask(l.web, http.MethodHead, uri)
		if head.Code != get.Code || head.Body.Len() != 0 || head.Header().Get("Content-Type") != get.Header().Get("Content-Type") ||
			head.Header().Get("Location") != get.Header().Get("Location") {
			t.Errorf("HEAD %s: %d %q, %d bytes; GET gave %d %q", uri, head.Code, head.Header().Get("Content-Type"), head.Body.Len(), get.Code, get.Header().Get("Content-Type"))
		}
	}
}
