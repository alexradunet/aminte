package lifelogtest_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
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

// taskService is a small task service: the catalog has the class tasks and the six writes, and GET /lists gives one list.
func taskService(t *testing.T) http.Handler {
	var writes []siren.Action
	for _, href := range []string{"/tasks", "/tasks/{id}", "/tasks/{id}/complete", "/tasks/{id}/reopen", "/tasks/{id}/delete", "/sync"} {
		writes = append(writes, siren.Action{Name: "w" + href, Method: "POST", Href: href})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := &siren.Entity{Class: []string{"tasks"}, Properties: map[string]any{"tasks": []any{}}}
		switch r.URL.Path {
		case "/actions":
			e = &siren.Entity{Class: []string{"actions", "tasks"}, Title: "Actions", Actions: writes}
		case "/lists":
			e.Properties = map[string]any{"lists": []map[string]string{{"id": "home", "name": "Home"}}}
		}
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		if err := json.NewEncoder(w).Encode(e); err != nil {
			t.Error(err)
		}
	})
}

// TestAChildWithTheClassTasksMakesTheServiceTasks registers a small task service and reads Tasks through the server:
// the list of the service is a list of the page. Connect reads the class, as serve does.
func TestAChildWithTheClassTasksMakesTheServiceTasks(t *testing.T) {
	s := lifelogtest.New(t, "2031-05-14")
	s.Connect(t, "svc", taskService(t))
	tasks, err := client.Remote(s.URL, "cli", "").Get(t.Context(), "/tasks")
	if err != nil {
		t.Fatalf("GET /tasks: %v", err)
	}
	b, err := json.Marshal(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"Home"`) {
		t.Errorf("GET /tasks does not name the list Home: %s", b)
	}
}

// failure is a testing.TB that records the first Fatalf and ends the goroutine, as t.Fatalf does: a test sees the
// refusal of a helper that fails its test. The other methods are those of the test that made it.
type failure struct {
	testing.TB
	said chan string
}

func (f failure) Fatalf(format string, args ...any) {
	f.said <- fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// refusal runs Connect of a child on its own goroutine, as a test does, and gives what Connect said when it failed the
// test, or "" when it did not.
func refusal(t *testing.T, name string, h http.Handler) string {
	t.Helper()
	s := lifelogtest.New(t, "2031-05-14")
	f := failure{TB: t, said: make(chan string, 1)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Connect(f, name, h)
	}()
	<-done
	select {
	case said := <-f.said:
		return said
	default:
		return ""
	}
}

// TestTheKitRefusesWhatServeRefuses: Connect makes the handshake of serve, so a catalog that serve refuses fails the
// test with the words of the server. A child with no class actions has no catalog; a child with the class tasks and
// not the six writes is refused by the registry.
func TestTheKitRefusesWhatServeRefuses(t *testing.T) {
	answer := func(class []string, actions ...siren.Action) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.siren+json")
			e := &siren.Entity{Class: class, Title: "Child", Actions: actions, Properties: map[string]any{"lists": []any{}}}
			if err := json.NewEncoder(w).Encode(e); err != nil {
				t.Error(err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		h    http.Handler
		want string // what the failure says; "" when the catalog is taken
	}{
		{"no class actions", answer([]string{"demo"}, siren.Action{Name: "ping", Method: "POST", Href: "/ping"}), "no catalog"},
		{"a class tasks with no writes", answer([]string{"actions", "tasks"}), "catalog refused"},
		{"an app", answer([]string{"actions"}, siren.Action{Name: "ping", Method: "POST", Href: "/ping"}), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			said := refusal(t, "demo", tc.h)
			if tc.want == "" && said != "" || !strings.Contains(said, tc.want) {
				t.Errorf("Connect said %q, want %q", said, tc.want)
			}
		})
	}
}
