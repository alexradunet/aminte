package web_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// webOver is a web in front of h, which stands for the core, and a browser that has logged in to it.
func webOver(t *testing.T, h http.Handler) http.Handler {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	b := newBrowser(t, server.URL, newClock(), t.TempDir())
	return signedIn{b, login(t, b)}
}

// TestWebAsksTheCoreForSirenAsTheUI: each read of the core is a GET that asks for Siren JSON with the bearer token, as
// the writer ui. The core makes the reads of the cards as agent:embed, so web sends no other writer. The query that the
// browser sent goes to the core, without the receipt of the toast (feedback), which web alone reads. How many reads a
// page makes is held by TestAPageIsFewReadsOfTheCore.
func TestWebAsksTheCoreForSirenAsTheUI(t *testing.T) {
	l := richLife(t)
	rec := &recorder{next: l.core.Handler}
	b := webOver(t, rec)
	if page := ask(b, http.MethodGet, "/browse?kind=person&feedback=once&unfiled=1"); page.Code != http.StatusOK {
		t.Fatalf("GET /browse: %d", page.Code)
	}
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
		if got := req.Header.Get(siren.SourceHeader); got != "ui" {
			t.Errorf("%s is read as %q, want ui", req.URL, got)
		}
	}
	if first := rec.requests()[0].URL; first.Path != "/browse" || first.RawQuery != "kind=person&unfiled=1" {
		t.Errorf("the page was read as %s, want /browse?kind=person&unfiled=1", first)
	}
}

// TestAPageIsFewReadsOfTheCore: the core embeds what a page needs (API.md, "Embedding"), and web reads each embedding
// once. The day page, the page of the settings and the conversation of today make these reads, in this order. The
// cards of the chat column come with the conversation that is read with ?embed=ref, not with a read for each card. The
// conversation view takes its own cards from its own read.
func TestAPageIsFewReadsOfTheCore(t *testing.T) {
	const conversation = "/days/" + today + "/conversation"
	for _, c := range []struct {
		name, uri string
		want      []string
	}{
		{"the day page", "/days/2031-05-13", []string{"/days/2031-05-13", "/?embed=today,conversation", conversation + "?embed=ref"}},
		{"the settings page", "/settings", []string{"/settings", "/actions", "/?embed=today,conversation", conversation + "?embed=ref"}},
		{"the conversation of today", "/days/today/conversation", []string{"/days/today/conversation?embed=ref", "/?embed=today,conversation"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{next: richLife(t).core.Handler}
			if page := ask(webOver(t, rec), http.MethodGet, c.uri); page.Code != http.StatusOK {
				t.Fatalf("GET %s: %d", c.uri, page.Code)
			}
			var got []string
			for _, req := range rec.requests() {
				got = append(got, req.URL.RequestURI())
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("GET %s makes %d reads of the core:\n%v\nwant %d:\n%v", c.uri, len(got), got, len(c.want), c.want)
			}
		})
	}
}

// TestThePollOfTheChatReadsTheConversationWithoutItsCards: a script that asks for the Siren entity of a conversation
// gets the answer of the core as it is. It reads the version only, so no card is embedded for it.
func TestThePollOfTheChatReadsTheConversationWithoutItsCards(t *testing.T) {
	rec := &recorder{next: richLife(t).core.Handler}
	req := httptest.NewRequest(http.MethodGet, "/days/today/conversation", nil)
	req.Header.Set("Accept", "application/vnd.siren+json")
	got := httptest.NewRecorder()
	webOver(t, rec).ServeHTTP(got, req)
	if got.Code != http.StatusOK {
		t.Fatalf("GET /days/today/conversation as Siren: %d", got.Code)
	}
	if reads := rec.requests(); len(reads) != 1 || reads[0].URL.RequestURI() != "/days/today/conversation" {
		t.Errorf("the poll made these reads of the core: %v, want the conversation alone", reads)
	}
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

// TestACardAsksForWhatThePageMayRead: a tool line that names an owner-only read draws the plain link, as the core's
// own page does. The core reads the card as agent:embed, which the core refuses the read of the settings and of the integrity check.
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
		for _, name := range []string{"Content-Type", "Content-Length"} {
			if got.Header().Get(name) != want.Header().Get(name) {
				t.Errorf("GET %s: %s is %q, want %q", uri, name, got.Header().Get(name), want.Header().Get(name))
			}
		}
		if got.Header().Get("Cache-Control") != "no-store" { // the core keeps a picture for a year, and a locked browser would show it from the cache
			t.Errorf("GET %s: Cache-Control is %q, want no-store", uri, got.Header().Get("Cache-Control"))
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
func fileCore(t *testing.T) (http.Handler, *recorder) {
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
	return webOver(t, rec), rec
}

// TestAFileKeepsItsHeadersAndTakesARange: the headers that describe a download reach the browser, and a conditional or
// range request of the browser reaches the core, so the browser can resume a download. The Cache-Control of the core is
// "private, no-cache"; every answer of a session is no-store (lock.go), so a snapshot of the owner is not kept either.
func TestAFileKeepsItsHeadersAndTakesARange(t *testing.T) {
	b, rec := fileCore(t)
	got := ask(b, http.MethodGet, "/snapshots/life-2031-05-14.db")
	for name, want := range map[string]string{"Content-Type": "application/vnd.sqlite3", "Content-Disposition": `attachment; filename="life-2031-05-14.db"`,
		"Cache-Control": "no-store", "Etag": `"v1"`, "Content-Length": "1000", "Accept-Ranges": "bytes"} {
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
	b := signedBrowser(t, "http://"+addr)
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
	b := signedBrowser(t, core.URL)

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

// TestAPageTakesNoEmbedAndAMethodThatIsNoneOfTheThreeIsRefused: embed is a parameter of the Siren answer, and a page has
// no PUT or DELETE: both are refused with their reason, and the core is not asked.
func TestAPageTakesNoEmbedAndAMethodThatIsNoneOfTheThreeIsRefused(t *testing.T) {
	l := richLife(t)
	rec := &recorder{next: l.core.Handler}
	b := webOver(t, rec)
	if got := ask(b, http.MethodGet, "/days?embed=ref"); got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "embed") {
		t.Errorf("a page with embed: %d, want 422 with its reason", got.Code)
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		got := ask(b, method, "/days/2031-05-14/capture")
		if got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "GET, HEAD, POST" {
			t.Errorf("%s: %d, Allow %q, want 405 with GET, HEAD, POST", method, got.Code, got.Header().Get("Allow"))
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("a refused request made %d reads of the core, want none", n)
	}
}

// TestADeletedMetricStaysAPage: the page of a live metric is the series, and the page of a deleted metric is a page. The
// page of a deleted metric says what it is, when it was deleted and how to restore it, and it leads to the readings.
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
	got := ask(l.web, http.MethodGet, page)
	if got.Code != http.StatusOK || got.Header().Get("Location") != "" {
		t.Fatalf("the page of a deleted metric: %d to %q, want 200 and no redirect", got.Code, got.Header().Get("Location"))
	}
	for _, want := range []string{
		`<span class="kind">metric, deleted</span>`,
		"<h1>Walk</h1>",
		`<div class="alert" role="alert"><p><strong>Deleted `,
		"Nothing is ever removed: restore it to bring it back.",
		`<form method="POST" action="` + page + `/revive" class="tiny" data-action="revive">`,
		`<a href="/metrics/Walk">Its readings</a>`,
	} {
		if !strings.Contains(got.Body.String(), want) {
			t.Errorf("the page of a deleted metric lacks %s\n%.1500s", want, got.Body.String())
		}
	}
}

// TestAReadNeverSendsBackOrFeedbackToTheCore: back and feedback are names that only web reads. The core takes no client's
// back or feedback, so a read that carries them goes through web as a page, and the core gets neither. new stays: the
// core takes it on a GET, to open a form.
func TestAReadNeverSendsBackOrFeedbackToTheCore(t *testing.T) {
	l := richLife(t)
	rec := &recorder{next: l.core.Handler}
	b := webOver(t, rec)
	for _, c := range []struct{ uri, want string }{
		{"/browse?kind=media&back=/x", "kind=media"},
		{"/browse?back=%2Fx&kind=media&feedback=once", "kind=media"},
		{"/browse?kind=media&new=add-file&back=/x&feedback=once", "kind=media&new=add-file"},
		{"/browse?kind=person&%62ack=/x", "kind=person"}, // the name b-a-c-k, written with an escape
	} {
		before := len(rec.requests())
		if got := ask(b, http.MethodGet, c.uri); got.Code != http.StatusOK {
			t.Errorf("GET %s: %d, want 200\n%.300s", c.uri, got.Code, got.Body.String())
			continue
		}
		first := rec.requests()[before].URL
		if first.Path != "/browse" || first.RawQuery != c.want {
			t.Errorf("GET %s was read as %s, want /browse?%s", c.uri, first, c.want)
		}
		for _, req := range rec.requests()[before:] {
			if req.URL.Query().Has("back") || req.URL.Query().Has("feedback") {
				t.Errorf("GET %s: the core was asked for %s", c.uri, req.URL)
			}
		}
	}
}

// TestNewRefusesAnAddressThatIsNotTheCore: the address must be http or https with a host and nothing else.
func TestNewRefusesAnAddressThatIsNotTheCore(t *testing.T) {
	options := func(base string) web.Options {
		return web.Options{CoreURL: base, Token: testToken, StateDir: t.TempDir()}
	}
	for _, base := range []string{"", "127.0.0.1:7777", "ftp://127.0.0.1", "http://", "http://user:pass@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?x=1", "http://127.0.0.1#x"} {
		if _, err := web.New(options(base)); err == nil {
			t.Errorf("New(%q) is accepted", base)
		}
	}
	if _, err := web.New(options("http://127.0.0.1:7777/")); err != nil {
		t.Errorf("New with a trailing slash: %v", err)
	}
}

// signedBrowser is a web over the core at coreURL and a browser that has logged in to it.
func signedBrowser(t *testing.T, coreURL string) http.Handler {
	t.Helper()
	b := newBrowser(t, coreURL, newClock(), t.TempDir())
	return signedIn{b, login(t, b)}
}

// TestTheScriptsAreSmallAndAlone: the rule of the scripts (README.md). A page of the shell carries the one script
// chat.js, and lock.js only when the lock is on. Each script is under 80 lines. The forms of the pages work without
// them, so the page has no script that a form needs.
func TestTheScriptsAreSmallAndAlone(t *testing.T) {
	l := richLife(t)
	const chat = `<script src="/assets/chat.js" defer></script>`
	uris := []string{"/", "/browse", "/days/today/conversation", "/pages/999999", "/settings", "/actions"}
	for _, uri := range uris {
		page := ask(l.web, http.MethodGet, uri).Body.String()
		if strings.Count(page, "<script") != 1 || !strings.Contains(page, chat) {
			t.Errorf("GET %s has %d script elements, want %s alone", uri, strings.Count(page, "<script"), chat)
		}
		if strings.Contains(page, "lock.js") {
			t.Errorf("GET %s has the script of the lock, and the lock is off", uri)
		}
	}
	setLock(t, l, "5", "4242")
	const lock = `<script src="/assets/lock.js" data-minutes="5" defer></script>`
	for _, uri := range uris {
		page := ask(l.web, http.MethodGet, uri).Body.String()
		if strings.Count(page, "<script") != 2 || !strings.Contains(page, chat) || !strings.Contains(page, lock) {
			t.Errorf("GET %s with the lock on has %d script elements, want %s and %s", uri, strings.Count(page, "<script"), chat, lock)
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
