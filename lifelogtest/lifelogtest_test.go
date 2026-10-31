package lifelogtest_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/lifelogtest"
)

// TestTheServerAnswersAtItsAddressAndInProcess: one server answers the same entity over its socket and through its
// handler, and its clock stands on the day that the test gave.
func TestTheServerAnswersAtItsAddressAndInProcess(t *testing.T) {
	s := lifelogtest.New(t, "2026-03-04")
	root, err := client.Remote(s.URL, "api", "").Get(t.Context(), "/")
	if err != nil {
		t.Fatal(err)
	}
	props, _ := root.Properties.(map[string]any)
	if got := props["today"]; got != "2026-03-04" {
		t.Errorf("today = %v, want 2026-03-04", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/days/today", nil)
	req.Header.Set("Accept", "application/vnd.siren+json")
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/days/2026-03-04" {
		t.Errorf("GET /days/today in process: %d to %q, want 303 to /days/2026-03-04", rec.Code, rec.Header().Get("Location"))
	}
}

// TestTwoServersAreTwoFiles: a write to one server is not seen by the other.
func TestTwoServersAreTwoFiles(t *testing.T) {
	a, b := lifelogtest.New(t, "2026-03-04"), lifelogtest.New(t, "2026-03-04")
	ca := client.Remote(a.URL, "api", "")
	actions, err := ca.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var capture = actions[0]
	for _, act := range actions {
		if act.Name == "capture" {
			capture = act
		}
	}
	if _, err := ca.Do(t.Context(), capture, map[string]string{"text": "only in the first file", "day": "2026-03-04"}); err != nil {
		t.Fatal(err)
	}
	for url, want := range map[string]bool{a.URL: true, b.URL: false} {
		res, err := http.Get(url + "/days/2026-03-04")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(body), "only in the first file"); got != want {
			t.Errorf("%s: day holds the capture = %v, want %v", url, got, want)
		}
	}
}
