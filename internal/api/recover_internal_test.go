package api

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aminte/siren"
)

// TestAPanicInAHandlerAnswers500AndIsLogged: a handler that panics does not drop the connection. The client gets the
// 500 entity with no word of the panic. The log has the route's pattern (not the path, which can name a metric), the
// value of the panic and a stack trace.
func TestAPanicInAHandlerAnswers500AndIsLogged(t *testing.T) {
	h := &server{}
	m := http.NewServeMux()
	m.HandleFunc("GET /secret/{name}", h.serve(func(*http.Request) (*siren.Entity, error) {
		panic("synthetic panic value")
	}))
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest("GET", "/secret/weight", nil))
	body := rec.Body.String()
	if rec.Code != 500 {
		t.Fatalf("status %d, want 500: %.300s", rec.Code, body)
	}
	if strings.Contains(body, "synthetic panic value") || !strings.Contains(body, "internal error") {
		t.Errorf("the answer names the panic, or not the internal error: %.600s", body)
	}
	out := logged.String()
	for _, want := range []string{"GET /secret/{name}", "synthetic panic value", "goroutine"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not have %q: %.600s", want, out)
		}
	}
	if strings.Contains(out, "weight") {
		t.Errorf("the log names the path: %.600s", out)
	}
}

// TestAbortHandlerPassesThroughRecover: http.ErrAbortHandler is the standard way to end a response on purpose, so the
// boundary does not turn it into a 500.
func TestAbortHandlerPassesThroughRecover(t *testing.T) {
	h := &server{}
	f := h.serve(func(*http.Request) (*siren.Entity, error) { panic(http.ErrAbortHandler) })
	defer func() {
		if p := recover(); !errors.Is(p.(error), http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", p)
		}
	}()
	f(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	t.Error("the handler returned")
}
