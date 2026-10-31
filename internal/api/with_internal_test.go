package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
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
