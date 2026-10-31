package web

// A handler of web that panics. The server would close the connection and the browser would see no page. Web answers
// with the page of an internal error and writes one record in the log. The tests add a route that panics to the mux of a
// Browser: the real routes do not panic, and the wrapper must hold for any route.

import (
	"bytes"
	"encoding/json"
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
