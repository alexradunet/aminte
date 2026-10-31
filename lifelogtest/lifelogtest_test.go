package lifelogtest_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/app"
	"lifelog/client"
	"lifelog/lifelogtest"
	"lifelog/siren"
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

// TestAConnectedAppIsInTheCatalogAndAnswersItsRoutes: Connect registers an app as the server does for a child of
// `serve --with`: its action is in GET /actions with the prefix of the app, a write to /with/NAME/... reaches the app, and
// the owner mark of the action is kept.
func TestAConnectedAppIsInTheCatalogAndAnswersItsRoutes(t *testing.T) {
	s := lifelogtest.New(t, "2026-03-04")
	var got string
	ping := siren.Action{Name: "ping", Title: "Ping", Method: "POST", Href: "/ping", Owner: true,
		Fields: []siren.Field{{Name: "word", Type: "text", Title: "Word"}}}
	s.Connect(t, "demo", app.Catalog("Demo", func(*http.Request) (*siren.Entity, error) {
		return &siren.Entity{Class: []string{"demo"}, Title: "Demo"}, nil
	}, []siren.Action{ping}, map[string]func(*http.Request) (*siren.Entity, error){
		"ping": func(r *http.Request) (*siren.Entity, error) {
			got = r.FormValue("word")
			return &siren.Entity{Class: []string{"demo"}, Title: "Demo", Result: "pinged"}, nil
		},
	}))
	c := client.Remote(s.URL, "ui", "")
	catalog, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var found *siren.Action
	for i := range catalog {
		if catalog[i].Name == "demo-ping" {
			found = &catalog[i]
		}
	}
	if found == nil || found.Href != "/with/demo/ping" || !found.Owner {
		t.Fatalf("the action demo-ping in the catalog: %+v", found)
	}
	if _, err := c.Do(t.Context(), *found, map[string]string{"word": "hello"}); err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("the app got the word %q, want hello", got)
	}
	// The owner rule is the server's: an agent is refused and the app is not called.
	got = ""
	if _, err := client.Remote(s.URL, "agent:test", "").Do(t.Context(), *found, map[string]string{"word": "no"}); err == nil || got != "" {
		t.Errorf("an agent ran the owner action: err=%v, the app got %q", err, got)
	}
}
