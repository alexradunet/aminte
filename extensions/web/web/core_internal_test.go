package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestABodyThatStallsEndsTheRead: a core that sends the headers of a Siren answer and then no more is a core that hung.
// The read of the entity ends after the wait and is an error, so the page is a 502 and not a request that waits for the
// browser to go away.
func TestABodyThatStallsEndsTheRead(t *testing.T) {
	release := make(chan struct{})
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sirenType)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(core.Close)
	t.Cleanup(func() { close(release) })
	b, err := New(Options{CoreURL: core.URL, Token: "synthetic-token-synthetic-token-1234", StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	b.core.entityWait = 50 * time.Millisecond
	in := httptest.NewRecorder() // the browser logs in
	form := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=synthetic-token-synthetic-token-1234"))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.ServeHTTP(in, form)
	start := time.Now()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/days/2031-05-14", nil)
	for _, c := range in.Result().Cookies() {
		req.AddCookie(c)
	}
	b.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("a core that stalls: %d, want 502", rec.Code)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the page waited %v for a body that stalls", took)
	}
}
