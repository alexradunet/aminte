package web_test

// What web sends to the core for a write, what it does with each answer, and what it refuses. The forms side by side
// (forms_sidebyside_test.go) hold that a browser sees what the browser of the core shows. These tests hold the requests that
// reach the core, with a recorder between web and the real core, and the answers that a real core does not give, with a fake.

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// seen is a request that reached the core, with its body.
type seen struct {
	method, uri string
	header      http.Header
	body        string
}

// bodies is a handler between web and the core that records each request with its body.
type bodies struct {
	next http.Handler
	mu   sync.Mutex
	got  []seen
}

func (b *bodies) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	b.mu.Lock()
	b.got = append(b.got, seen{r.Method, r.URL.RequestURI(), r.Header.Clone(), string(raw)})
	b.mu.Unlock()
	b.next.ServeHTTP(w, r)
}

func (b *bodies) requests() []seen {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]seen(nil), b.got...)
}

// postForm is a form of a browser, posted to h.
func postForm(h http.Handler, path string, form url.Values, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAWriteReachesTheCoreAsTheWriterUIWithTheFieldsOfTheAction: web reads the catalog, then sends one POST to the same
// path as the writer ui, with the bearer token and Siren as the answer it accepts. The fields that only web reads (back,
// feedback) do not go; the receipt in the query does not go; every other field goes, and the core refuses a field that
// it does not know.
func TestAWriteReachesTheCoreAsTheWriterUIWithTheFieldsOfTheAction(t *testing.T) {
	l := richLife(t)
	rec := &bodies{next: l.core.Handler}
	h := webOver(t, rec)
	got := postForm(h, "/days/"+today+"/capture?feedback=old", url.Values{"text": {"Words."}, "back": {"/days/" + today}, "feedback": {"x"}, "import_key": {"K"}})
	if got.Code != http.StatusSeeOther {
		t.Fatalf("POST: %d %.300s", got.Code, got.Body.String())
	}
	reqs := rec.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests reached the core, want the catalog and the write: %v", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodGet || reqs[0].uri != "/actions" {
		t.Errorf("the first request is %s %s, want GET /actions", reqs[0].method, reqs[0].uri)
	}
	w := reqs[1]
	if w.method != http.MethodPost || w.uri != "/days/"+today+"/capture" {
		t.Errorf("the write is %s %s, want POST /days/%s/capture", w.method, w.uri, today)
	}
	for name, want := range map[string]string{"Accept": "application/vnd.siren+json", "Lifelog-Source": "ui",
		"Authorization": "Bearer " + testToken, "Content-Type": "application/x-www-form-urlencoded"} {
		if w.header.Get(name) != want {
			t.Errorf("the write sends %s %q, want %q", name, w.header.Get(name), want)
		}
	}
	for _, name := range []string{"Cookie", "Origin", "Sec-Fetch-Site", "Referer"} {
		if w.header.Get(name) != "" {
			t.Errorf("the write passes the header %s of the browser to the core", name)
		}
	}
	if sent, _ := url.ParseQuery(w.body); sent.Encode() != (url.Values{"text": {"Words."}, "import_key": {"K"}}).Encode() {
		t.Errorf("the core got the fields %q, want text and import_key", w.body)
	}
	// The core refuses a field that it does not know: web does not drop it.
	got = postForm(h, "/days/"+today+"/capture", url.Values{"text": {"Words."}, "bogus": {"1"}})
	if got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "unknown field bogus") {
		t.Errorf("a field of no action: %d, want 422 with the words of the core", got.Code)
	}
}

// TestThePickBarSendsTheActionOnlyItsOwnFields: the pick bar of a list posts the form of file-pages to tombstone-pages by
// a second button. Web sends tombstone-pages the fields that it lists (ids), and file-pages all of its own. A field that
// neither lists goes on, and the core refuses it.
func TestThePickBarSendsTheActionOnlyItsOwnFields(t *testing.T) {
	l := richLife(t)
	rec := &bodies{next: l.core.Handler}
	h := webOver(t, rec)
	form := url.Values{"ids": {"3"}, "category": {"Walks"}, "from": {""}, "move": {"1"}, "back": {"/browse"}}
	lastWrite := func() url.Values {
		t.Helper()
		reqs := rec.requests()
		sent, err := url.ParseQuery(reqs[len(reqs)-1].body)
		if err != nil || reqs[len(reqs)-1].method != http.MethodPost {
			t.Fatalf("the last request is %v", reqs[len(reqs)-1])
		}
		return sent
	}
	if got := postForm(h, "/pages/file", form); got.Code != http.StatusSeeOther {
		t.Fatalf("file-pages: %d %.300s", got.Code, got.Body.String())
	}
	if sent := lastWrite(); sent.Encode() != (url.Values{"ids": {"3"}, "category": {"Walks"}, "from": {""}, "move": {"1"}}).Encode() {
		t.Errorf("file-pages got %q", sent.Encode())
	}
	if got := postForm(h, "/pages/tombstone", form); got.Code != http.StatusSeeOther {
		t.Fatalf("tombstone-pages: %d %.300s", got.Code, got.Body.String())
	}
	if sent := lastWrite(); sent.Encode() != (url.Values{"ids": {"3"}}).Encode() {
		t.Errorf("tombstone-pages got %q, want ids alone", sent.Encode())
	}
	form.Set("bogus", "1")
	if got := postForm(h, "/pages/tombstone", form); got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "unknown field bogus") {
		t.Errorf("tombstone-pages with a field of no form: %d", got.Code)
	}
}

// TestAFormOfAFileStreamsToTheCore: web sends the body of a file to the core as it comes, and holds none of it. The test
// sends the first part, waits until the core has read it, and only then sends the rest. A web that held the body would
// wait for the end before it sent a byte, and the wait would end the test. The page to return to comes in the query of the
// address, and web does not send it on.
func TestAFormOfAFileStreamsToTheCore(t *testing.T) {
	first := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var query, contentType string
	var total int
	core := http.NewServeMux()
	core.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		io.WriteString(w, `{"class":["actions"],"actions":[]}`)
	})
	core.HandleFunc("POST /files", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		query, contentType = r.URL.RawQuery, r.Header.Get("Content-Type")
		mu.Unlock()
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			mu.Lock()
			total += n
			seenFirst := total >= 1024
			mu.Unlock()
			if seenFirst {
				once.Do(func() { close(first) })
			}
			if err != nil {
				break
			}
		}
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		w.Header().Set("Location", "/pages/7")
		io.WriteString(w, `{"class":["file"],"title":"Heron.jpg","links":[{"rel":["self"],"href":"/pages/7"}]}`)
	})
	h := webOver(t, core)

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	req := httptest.NewRequest(http.MethodPost, "/files?back=%2Fbrowse%3Fkind%3Dmedia", pr)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()
	part, err := mw.CreateFormFile("original", "Heron.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), 8192)); err != nil { // the first part: more than the core waits for
		t.Fatal(err)
	}
	select {
	case <-first:
	case <-time.After(10 * time.Second):
		t.Fatal("the core has not read a byte while the browser still sends: web holds the body")
	}
	if _, err := part.Write(bytes.Repeat([]byte("y"), 8192)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	pw.Close()
	rec := <-done
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/browse?feedback=") && rec.Header().Get("Location") != "/browse?kind=media" {
		t.Errorf("the answer: %d to %q, want a redirect to the page in back", rec.Code, rec.Header().Get("Location"))
	}
	mu.Lock()
	defer mu.Unlock()
	if query != "" || !strings.HasPrefix(contentType, "multipart/form-data; boundary=") || total < 16384 {
		t.Errorf("the core got query %q, type %q, %d bytes; want no query, the multipart type and the whole body", query, contentType, total)
	}
}

// TestARefusedFileFormKeepsNoText: a form of a file is refused by the core, and the page of the error has the reason. It does
// not show "What you sent", because the text is in the body of the stream, which web does not read. The browser of the core
// shows it. This is the second allowed difference of the forms (forms_sidebyside_test.go).
func TestARefusedFileFormKeepsNoText(t *testing.T) {
	l := richLife(t)
	form := formSent{path: "/files", from: "/browse", file: &fileSent{
		fields: [][2]string{{"title", "X.jpg"}, {"body", "Words that the page of the error could show again."}, {"bogus", "1"}}, back: "/browse", original: syntheticJPEG(t)}}
	old, got := deliver(l.core.Handler, form, false), deliver(l.web, form, true)
	if old.Code != http.StatusUnprocessableEntity || got.Code != old.Code {
		t.Fatalf("the core %d, web %d, want 422 from both", old.Code, got.Code)
	}
	if !strings.Contains(old.Body.String(), "What you sent") {
		t.Error("the browser of the core does not show the text any more: this test is out of date")
	}
	if strings.Contains(got.Body.String(), "What you sent") || !strings.Contains(got.Body.String(), "unknown field bogus") {
		t.Errorf("the page of web: %.400s", got.Body.String())
	}
}

// TestTheErrorPageLinksBackToAPageOfThisServerOnly: a refused form has a link back to the page it came from, when the
// Referer is a page of the host of the request. A Referer of another host gives no link.
func TestTheErrorPageLinksBackToAPageOfThisServerOnly(t *testing.T) {
	l := richLife(t)
	form := url.Values{"text": {"Words."}, "bogus": {"1"}}
	for referer, want := range map[string]string{
		"http://example.com/days/2031-05-14?new=capture": `<a href="/days/2031-05-14?new=capture">Back</a>`,
		"http://evil.example/steal":                      "",
		"":                                               "",
	} {
		page := postForm(l.web, "/days/"+today+"/capture", form, "Referer", referer).Body.String()
		if got := strings.Contains(page, ">Back</a>"); got != (want != "") || want != "" && !strings.Contains(page, want) {
			t.Errorf("Referer %q: the page has the Back link = %v, want %q", referer, got, want)
		}
		if !strings.Contains(page, "Words.") { // the long text that the form sent
			t.Errorf("Referer %q: the page lost the text of the form", referer)
		}
	}
}

// TestAWriteThatWebCannotMakeIsRefusedAndTheCoreIsNotAsked: a body that is not a form, a body that is too large, a query
// that cannot be read and a method that is none of GET, HEAD and POST are refused with their reasons.
func TestAWriteThatWebCannotMakeIsRefusedAndTheCoreIsNotAsked(t *testing.T) {
	l := richLife(t)
	rec := &bodies{next: l.core.Handler}
	h := webOver(t, rec)
	send := func(contentType, body, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/days/"+today+"/capture"+query, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out
	}
	for name, c := range map[string]struct {
		got  *httptest.ResponseRecorder
		want int
	}{
		"json":                    {send("application/json", `{"text":"x"}`, ""), http.StatusUnsupportedMediaType},
		"plain text":              {send("text/plain", "text=x", ""), http.StatusUnsupportedMediaType},
		"a body with no type":     {send("", "text=x", ""), http.StatusUnsupportedMediaType},
		"too large":               {send("application/x-www-form-urlencoded", "text="+strings.Repeat("a", 20<<20), ""), http.StatusRequestEntityTooLarge},
		"a query that is no form": {send("application/x-www-form-urlencoded", "text=x", "?a=%zz"), http.StatusBadRequest},
	} {
		if c.got.Code != c.want || !strings.Contains(c.got.Body.String(), "</html>") {
			t.Errorf("%s: %d, want %d with a page", name, c.got.Code, c.want)
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("%d refused writes made %d requests to the core, want none", 5, n)
	}
}

// fakeCore is a core that gives the answers a real core rarely gives to a write: its catalog, and one route.
func fakeCore(t *testing.T, catalog int, route http.HandlerFunc) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		w.WriteHeader(catalog)
		io.WriteString(w, `{"class":["actions"],"actions":[]}`)
	})
	mux.HandleFunc("POST /thing", route)
	return webOver(t, mux)
}

func sirenAnswer(w http.ResponseWriter, status int, body string, header ...string) {
	for i := 0; i+1 < len(header); i += 2 {
		w.Header().Set(header[i], header[i+1])
	}
	w.Header().Set("Content-Type", "application/vnd.siren+json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

// TestWhatTheCoreAnswersToAWriteIsWhatTheBrowserGets: a redirect of the core is a redirect of the browser, an off-site
// Location is a fault of the core, an answer with no Location is drawn as a page, an error is its page with the status of
// the core, a body that is not an entity is 502, and bytes that are no entity stream through.
func TestWhatTheCoreAnswersToAWriteIsWhatTheBrowserGets(t *testing.T) {
	entity := `{"class":["day"],"title":"2031-05-14","properties":{"day":"2031-05-14"}}`
	for name, c := range map[string]struct {
		route  http.HandlerFunc
		status int
		want   string // a text of the page or the body, or the Location
	}{
		"a redirect": {func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/days/2031-05-14", http.StatusSeeOther)
		},
			http.StatusSeeOther, "/days/2031-05-14"},
		"a redirect to another host": {func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "//evil.example/", http.StatusSeeOther)
		},
			http.StatusBadGateway, "not a path"},
		"a Location to another host": {func(w http.ResponseWriter, r *http.Request) {
			sirenAnswer(w, 200, entity, "Location", "//evil.example/x")
		}, http.StatusBadGateway, "not a path"},
		"no Location": {func(w http.ResponseWriter, r *http.Request) { sirenAnswer(w, 200, entity) }, http.StatusOK, "2031-05-14"},
		"an error": {func(w http.ResponseWriter, r *http.Request) {
			sirenAnswer(w, 409, `{"class":["error"],"title":"Error","properties":{"status":409,"code":"conflict","message":"changed since you read it"}}`)
		}, http.StatusConflict, "changed since you read it"},
		"a busy file": {func(w http.ResponseWriter, r *http.Request) {
			sirenAnswer(w, 503, `{"class":["error"],"title":"Error","properties":{"status":503,"code":"busy","message":"the file is busy"}}`, "Retry-After", "1")
		}, http.StatusServiceUnavailable, "the file is busy"},
		"not an entity": {func(w http.ResponseWriter, r *http.Request) {
			sirenAnswer(w, 200, "{not json", "Location", "/days/2031-05-14")
		},
			http.StatusBadGateway, "not an entity"},
		"bytes": {func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "too much", http.StatusRequestEntityTooLarge)
		},
			http.StatusRequestEntityTooLarge, "too much"},
	} {
		t.Run(name, func(t *testing.T) {
			got := postForm(fakeCore(t, http.StatusOK, c.route), "/thing", url.Values{"a": {"b"}})
			if got.Code != c.status {
				t.Fatalf("%d, want %d: %.300s", got.Code, c.status, got.Body.String())
			}
			if !strings.Contains(got.Body.String(), c.want) && got.Header().Get("Location") != c.want {
				t.Errorf("the answer lacks %q: Location %q, %.300s", c.want, got.Header().Get("Location"), got.Body.String())
			}
			if c.status == http.StatusServiceUnavailable && got.Header().Get("Retry-After") != "1" {
				t.Errorf("Retry-After %q, want the 1 of the core", got.Header().Get("Retry-After"))
			}
		})
	}
	// A catalog that the core cannot give is a core that web cannot use for a write: nothing is sent.
	var posted bool
	h := fakeCore(t, http.StatusInternalServerError, func(w http.ResponseWriter, r *http.Request) { posted = true; sirenAnswer(w, 200, entity) })
	if got := postForm(h, "/thing", url.Values{"a": {"b"}}); got.Code != http.StatusBadGateway || posted {
		t.Errorf("a core with no catalog: %d, posted=%v, want 502 and no write", got.Code, posted)
	}
}

// TestACoreThatIsDownRefusesAWriteWithAPage: nothing listens at the address of the core. A form gets a 503 page with a
// Retry-After, and the page names no address.
func TestACoreThatIsDownRefusesAWriteWithAPage(t *testing.T) {
	l := richLife(t)
	down := httptest.NewServer(http.NotFoundHandler())
	addr := down.URL
	down.Close()
	b := signedBrowser(t, addr)
	got := postForm(b, "/days/"+today+"/capture", url.Values{"text": {"x"}})
	if got.Code != http.StatusServiceUnavailable || got.Header().Get("Retry-After") != "1" || strings.Contains(got.Body.String(), addr) {
		t.Errorf("a write to a core that is down: %d, Retry-After %q", got.Code, got.Header().Get("Retry-After"))
	}
	_ = l
}

// TestTheToastIsTakenOnceAtItsOwnPath: a write returns to its page with a receipt, and the page draws the result of the write
// once. The receipt is for its own path alone, a forged receipt draws nothing, a page with a toast is not kept or sent on in a
// Referer, and the core never sees the receipt.
func TestTheToastIsTakenOnceAtItsOwnPath(t *testing.T) {
	l := richLife(t)
	rec := &bodies{next: l.core.Handler}
	h := webOver(t, rec)
	got := postForm(h, "/pages", url.Values{"title": {"Notes"}, "body": {"[[bad/name]] and [[Sam]]"}, "back": {"/browse?kind=person"}})
	loc := got.Header().Get("Location")
	if got.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/browse?") || !strings.Contains(loc, "feedback=") {
		t.Fatalf("the write: %d to %q", got.Code, loc)
	}
	other := ask(h, http.MethodGet, strings.Replace(loc, "/browse", "/days", 1))
	if strings.Contains(other.Body.String(), "bad/name") {
		t.Error("the receipt drew its toast on another path")
	}
	first := ask(h, http.MethodGet, loc)
	if !strings.Contains(first.Body.String(), "<h2>Result</h2>") || !strings.Contains(first.Body.String(), "bad/name") || first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("the page with the receipt: Cache-Control %q, Referrer-Policy %q\n%.300s", first.Header().Get("Cache-Control"), first.Header().Get("Referrer-Policy"), first.Body.String())
	}
	if again := ask(h, http.MethodGet, loc); strings.Contains(again.Body.String(), "bad/name") {
		t.Error("the receipt drew its toast twice")
	}
	if forged := ask(h, http.MethodGet, "/browse?feedback="+strings.Repeat("0", 64)); forged.Code != http.StatusOK || strings.Contains(forged.Body.String(), "<h2>Result</h2>") {
		t.Errorf("a forged receipt: %d, with a toast: %v", forged.Code, strings.Contains(forged.Body.String(), "<h2>Result</h2>"))
	}
	for _, r := range rec.requests() {
		if strings.Contains(r.uri, "feedback") {
			t.Errorf("the core saw the receipt: %s %s", r.method, r.uri)
		}
	}
}

// TestTheToastFollowsAMetricToItsSeries: a write that returns to the page of a live metric lands on its series, and the toast
// follows. The page of a metric has one view, the series (plan 144), and the receipt moves with the redirect.
func TestTheToastFollowsAMetricToItsSeries(t *testing.T) {
	l := richLife(t)
	page := l.hrefs["walkPage"]
	got := postForm(l.web, "/pages/"+strings.TrimPrefix(l.hrefs["river"], "/pages/")+"/body", url.Values{
		"body": {"Walked by [[bad/name]]."}, "version": {l.versionOf(t, l.hrefs["river"])}, "back": {page}})
	loc := got.Header().Get("Location")
	if got.Code != http.StatusSeeOther || !strings.HasPrefix(loc, page+"?feedback=") {
		t.Fatalf("the write: %d to %q, want the page of the metric with a receipt", got.Code, loc)
	}
	hop := ask(l.web, http.MethodGet, loc)
	next := hop.Header().Get("Location")
	if hop.Code != http.StatusSeeOther || !strings.HasPrefix(next, "/metrics/Walk?feedback=") {
		t.Fatalf("the page of the metric: %d to %q, want its series with the receipt", hop.Code, next)
	}
	if !strings.Contains(ask(l.web, http.MethodGet, next).Body.String(), "bad/name") {
		t.Error("the toast did not follow the redirect to the series")
	}
}
