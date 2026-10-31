package web

// A handler of web that panics. The server would close the connection and the browser would see no page. Web answers
// with the page of an internal error and writes one record in the log. When the handler had started its answer, the page
// cannot replace the status, so web writes the one record and aborts the handler: the browser sees an answer that ends too
// early. The tests add a route that panics to the mux of a Browser: the real routes do not panic, and the wrapper must
// hold for any route.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

const recoverToken = "synthetic-token-synthetic-token-1234"

// logStamp finds the start of a record of the standard log: its date. A record may hold many lines (the stack).
var logStamp = regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} `)

// panickingBrowser is a Browser with a route that panics with value, and the Cookie header of a browser that has logged in.
func panickingBrowser(t *testing.T, value any) (b *Browser, cookie string) {
	t.Helper()
	// The core is never asked: the route panics first.
	b, err := New(Options{CoreURL: "http://127.0.0.1:1", Token: recoverToken, StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	b.mux.HandleFunc("GET /boom/{name}", func(http.ResponseWriter, *http.Request) { panic(value) })
	in := httptest.NewRecorder()
	form := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"token": {recoverToken}}.Encode()))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.ServeHTTP(in, form)
	for _, c := range in.Result().Cookies() {
		if c.Name == sessionCookie {
			return b, c.Name + "=" + c.Value
		}
	}
	t.Fatalf("the login set no session cookie: %d", in.Code)
	return nil, ""
}

// capturedLog captures the standard log for the test.
func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	out := &bytes.Buffer{}
	log.SetOutput(out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return out
}

// TestAPanicIsAnInternalErrorPageAndOneLogRecord: the browser gets a 500 page that says internal error and not the value
// of the panic. The log gets one record with the route pattern, the value and the stack, and not the path, which here
// names a person.
func TestAPanicIsAnInternalErrorPageAndOneLogRecord(t *testing.T) {
	b, cookie := panickingBrowser(t, "the secret value of the panic")
	logs := capturedLog(t)
	req := httptest.NewRequest(http.MethodGet, "/boom/Samuel-Ortiz", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	b.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, "internal error") || !strings.Contains(body, "</html>") {
		t.Errorf("a panic: %d, want a 500 page that says internal error\n%.300s", rec.Code, body)
	}
	if strings.Contains(body, "secret value") || strings.Contains(body, "goroutine") {
		t.Errorf("the page shows the value or the stack of the panic:\n%.300s", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the page of a panic in a session: Cache-Control %q, want no-store", got)
	}
	record := logs.String()
	for _, want := range []string{"GET /boom/{name}: panic: the secret value of the panic", "goroutine ", "recover_test.go"} {
		if !strings.Contains(record, want) {
			t.Errorf("the log lacks %q:\n%s", want, record)
		}
	}
	if strings.Contains(record, "Samuel-Ortiz") {
		t.Errorf("the log holds the path, which names a person:\n%s", record)
	}
	if n := len(logStamp.FindAllString(record, -1)); n != 1 {
		t.Errorf("the log holds %d records, want 1:\n%s", n, record)
	}
}

// TestAPanicAnswersAScriptWithJSON: the poll of a page asks for Siren JSON. It gets the error as JSON with the code
// internal, as every other error of web.
func TestAPanicAnswersAScriptWithJSON(t *testing.T) {
	b, cookie := panickingBrowser(t, "the secret value of the panic")
	capturedLog(t)
	req := httptest.NewRequest(http.MethodGet, "/boom/x", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", sirenType)
	rec := httptest.NewRecorder()
	b.ServeHTTP(rec, req)
	var e struct {
		Properties struct{ Code, Message string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || rec.Code != http.StatusInternalServerError ||
		rec.Header().Get("Content-Type") != sirenType || e.Properties.Code != "internal" || e.Properties.Message != "internal error" {
		t.Errorf("a panic for a script: %d %q, %v\n%.300s", rec.Code, rec.Header().Get("Content-Type"), err, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret value") {
		t.Errorf("the answer shows the value of the panic:\n%.300s", rec.Body.String())
	}
}

// TestAPanicWithErrAbortHandlerPassesThrough: http.ErrAbortHandler is how a handler cuts an answer on purpose. The server
// needs to see it, so the wrapper does not answer for it and writes no record.
func TestAPanicWithErrAbortHandlerPassesThrough(t *testing.T) {
	b, cookie := panickingBrowser(t, http.ErrAbortHandler)
	logs := capturedLog(t)
	req := httptest.NewRequest(http.MethodGet, "/boom/x", nil)
	req.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	var got any
	func() {
		defer func() { got = recover() }()
		b.ServeHTTP(rec, req)
	}()
	if got != http.ErrAbortHandler {
		t.Errorf("the panic that reached the server is %v, want http.ErrAbortHandler", got)
	}
	if rec.Body.Len() != 0 || logs.Len() != 0 {
		t.Errorf("an aborted handler: %d bytes of answer, log %q; want neither", rec.Body.Len(), logs.String())
	}
}

// TestAPanicBeforeTheAnswerRemovesTheCacheHeaderOfTheHandler: a handler that set a long cache for its picture and then
// panicked must not leave that header on the page of the internal error: a locked browser would show the page from the
// cache.
func TestAPanicBeforeTheAnswerRemovesTheCacheHeaderOfTheHandler(t *testing.T) {
	b, cookie := panickingBrowser(t, "x")
	b.mux.HandleFunc("GET /cached/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		panic("after the cache header")
	})
	capturedLog(t)
	req := httptest.NewRequest(http.MethodGet, "/cached/x", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	b.ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); rec.Code != http.StatusInternalServerError || got != "no-store" {
		t.Errorf("a panic after the handler set a cache: %d, Cache-Control %q, want 500 and no-store", rec.Code, got)
	}
}

// TestAPanicAfterTheAnswerStartedCutsTheAnswer: a handler sent 200 and the first part of its body, and then panicked. The
// page of an internal error cannot replace the status, and it must not follow the partial body: the browser would see a
// 200 that looks whole. Web writes one record in the log and aborts the handler, so the server closes the connection and
// the browser sees an answer that ends too early. The test runs over a real server, because a recorder cannot show it.
func TestAPanicAfterTheAnswerStartedCutsTheAnswer(t *testing.T) {
	b, cookie := panickingBrowser(t, "x")
	b.mux.HandleFunc("GET /half/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "the first half of the answer\n")
		if err := http.NewResponseController(w).Flush(); err != nil { // the first half is on the wire before the panic
			t.Errorf("flush: %v", err)
		}
		panic("the secret value of the panic")
	})
	logs := capturedLog(t)
	srv := httptest.NewServer(b)
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/half/Samuel-Ortiz", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "text/html")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res.Body)
	res.Body.Close()

	if readErr == nil {
		t.Errorf("the read of the answer ended with no error: the browser would take %d bytes for a whole answer", len(body))
	}
	if !strings.HasPrefix(string(body), "the first half of the answer\n") {
		t.Errorf("the answer lost the part that was sent: %.100q", body)
	}
	if strings.Contains(string(body), "internal error") || strings.Contains(string(body), "</html>") {
		t.Errorf("the page of the internal error follows the partial body (%d bytes)", len(body))
	}
	// The record is written when the handler returns, which can be after the client saw the cut.
	srv.Close()
	record := logs.String()
	for _, want := range []string{"GET /half/{name}: panic: the secret value of the panic", "goroutine "} {
		if !strings.Contains(record, want) {
			t.Errorf("the log lacks %q:\n%s", want, record)
		}
	}
	if strings.Contains(record, "Samuel-Ortiz") {
		t.Errorf("the log holds the path, which names a person:\n%s", record)
	}
	if n := len(logStamp.FindAllString(record, -1)); n != 1 {
		t.Errorf("the log holds %d records, want 1 (the server must not add a record for a second WriteHeader):\n%s", n, record)
	}
}

// TestAPanicAfterTheAnswerStartedAbortsTheHandler: the panic that reaches the server is http.ErrAbortHandler, and web has
// written no second status and no page. The answer starts with a byte, with a status, or with a flush, which sends a 200
// when the handler sent no status. This is the same claim as above, held without a server.
func TestAPanicAfterTheAnswerStartedAbortsTheHandler(t *testing.T) {
	starts := map[string]struct {
		start func(w http.ResponseWriter)
		body  string
	}{
		"a byte":   {func(w http.ResponseWriter) { io.WriteString(w, "the first half\n") }, "the first half\n"},
		"a status": {func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) }, ""},
		"a flush":  {func(w http.ResponseWriter) { http.NewResponseController(w).Flush() }, ""},
	}
	for name, c := range starts {
		t.Run(name, func(t *testing.T) {
			b, cookie := panickingBrowser(t, "x")
			b.mux.HandleFunc("GET /half/{name}", func(w http.ResponseWriter, r *http.Request) {
				c.start(w)
				panic("after the start")
			})
			logs := capturedLog(t)
			req := httptest.NewRequest(http.MethodGet, "/half/x", nil)
			req.Header.Set("Cookie", cookie)
			rec := httptest.NewRecorder()
			var got any
			func() {
				defer func() { got = recover() }()
				b.ServeHTTP(rec, req)
			}()
			if got != http.ErrAbortHandler {
				t.Errorf("the panic that reached the server is %v, want http.ErrAbortHandler", got)
			}
			if rec.Body.String() != c.body {
				t.Errorf("the body is %.200q, want only what the handler wrote: %q", rec.Body.String(), c.body)
			}
			if n := len(logStamp.FindAllString(logs.String(), -1)); n != 1 {
				t.Errorf("the log holds %d records, want 1:\n%s", n, logs.String())
			}
		})
	}
}
