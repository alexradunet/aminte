package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aminte/internal/core"
	"aminte/internal/db/dbtest"
)

// TestASlowAppIsTheTimeoutOfTheRequest: an app that answers after forwardTimeout gives 504 timeout.
func TestASlowAppIsTheTimeoutOfTheRequest(t *testing.T) {
	old := forwardTimeout
	forwardTimeout = 50 * time.Millisecond
	t.Cleanup(func() { forwardTimeout = old })
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	apps := NewApps()
	if err := apps.set("slow", slow.URL, "Slow", nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	NewWith(&core.Store{DB: dbtest.New(t)}, apps).ServeHTTP(rec, httptest.NewRequest("GET", "/with/slow/", nil))
	if rec.Code != 504 || !strings.Contains(rec.Body.String(), `"timeout"`) {
		t.Errorf("%d %s, want 504 timeout", rec.Code, rec.Body)
	}
}

// TestRegisterEndsAChildThatHoldsTheConnection: the handshake limits each of its reads to registerTimeout, so a child
// that takes the connection and never answers does not hold the caller. The caller's context has no deadline here: the
// limit of the handshake itself ends the wait, with an error that wraps context.DeadlineExceeded (the caller tells a
// child that is not up yet from a child that has no catalog by it).
func TestRegisterEndsAChildThatHoldsTheConnection(t *testing.T) {
	old := registerTimeout
	registerTimeout = 50 * time.Millisecond
	t.Cleanup(func() { registerTimeout = old })
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(release) }) // runs before hang.Close: the open request ends first
	_, err := NewApps().Register(t.Context(), "demo", hang.URL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Register against a child that holds the connection: %v, want an error that wraps the deadline", err)
	}
}
