package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aminte/app"
	"aminte/internal/api"
	"aminte/internal/api/apitest"
	"aminte/siren"
)

// testChild is a connected app for a test: a home with a count, an action that adds an item, an owner action and a
// read of an item. hits counts the requests that reached it.
type testChild struct {
	srv   *httptest.Server
	hits  atomic.Int64
	query atomic.Value // the raw query of the last GET /
}

func childActions() []siren.Action {
	return []siren.Action{
		{Name: "add-item", Title: "Add an item", Method: "POST", Href: "/items", Type: "application/x-www-form-urlencoded",
			Fields: []siren.Field{{Name: "text", Type: "text", Title: "Text", Required: true}}},
		{Name: "reset", Title: "Reset", Method: "POST", Href: "/reset", Owner: true},
		{Name: "look", Title: "Look", Method: "GET", Href: "/items/{id}",
			Fields: []siren.Field{{Name: "id", Type: "number", Title: "Item", Required: true, In: "path"}}},
	}
}

func newChild(t *testing.T) *testChild {
	t.Helper()
	c := &testChild{}
	items := 0
	run := map[string]func(*http.Request) (*siren.Entity, error){
		"add-item": func(r *http.Request) (*siren.Entity, error) {
			items++
			return &siren.Entity{Class: []string{"item"}, Title: "Item " + strconv.Itoa(items),
				Properties: map[string]any{"text": r.PostFormValue("text")},
				Links:      []siren.Link{{Rel: []string{"self"}, Href: "/items/" + strconv.Itoa(items)}, {Rel: []string{"elsewhere"}, Href: "http://example.invalid/x"}},
				Actions:    []siren.Action{{Name: "add-item", Method: "POST", Href: "/items", Fields: []siren.Field{{Name: "text", Type: "text"}}}}}, nil
		},
		"reset": func(r *http.Request) (*siren.Entity, error) {
			items = 0
			return &siren.Entity{Class: []string{"reset"}, Title: "Reset"}, nil
		},
		"look": func(r *http.Request) (*siren.Entity, error) {
			if r.PathValue("id") == "9" {
				return nil, &app.Error{Status: 404, Code: "gone", Msg: "no item 9"}
			}
			return &siren.Entity{Class: []string{"item"}, Title: "Item " + r.PathValue("id")}, nil
		},
	}
	home := func(r *http.Request) (*siren.Entity, error) {
		c.query.Store(r.URL.RawQuery)
		return &siren.Entity{Class: []string{"testapp"}, Title: "Test app", Properties: map[string]any{"count": items},
			Entities: []siren.Link{{Rel: []string{"item"}, Href: "/items/1", Title: "First"}},
			Actions:  []siren.Action{{Name: "add-item", Title: "Add an item", Method: "POST", Href: "/items", Fields: []siren.Field{{Name: "text", Type: "text", Title: "Text", Required: true}}}}}, nil
	}
	h := app.Catalog("Test app", home, childActions(), run)
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.hits.Add(1)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// withServer is the API over a fresh file, with the connected app testapp running.
func withServer(t *testing.T) (http.Handler, *api.Apps, *testChild) {
	t.Helper()
	_, _, s := apitest.Fresh(t, "cli")
	apps := api.NewApps()
	apps.Expect("testapp")
	child := newChild(t)
	if _, err := apps.Register(t.Context(), "testapp", child.srv.URL); err != nil {
		t.Fatal(err)
	}
	child.hits.Store(0) // the handshake is not a request of the tests
	return api.NewWith(s, apps), apps, child
}

// appLinks are the hrefs of the apps that run, as the root of the server links them.
func appLinks(t *testing.T, h http.Handler) []string {
	t.Helper()
	var root siren.Entity
	if err := json.Unmarshal(call(h, "GET", "/", "", nil).Body.Bytes(), &root); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range root.Links {
		if slices.Contains(l.Rel, "app") {
			out = append(out, l.Href)
		}
	}
	return out
}

func call(h http.Handler, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheCatalogHoldsTheActionsOfARunningApp(t *testing.T) {
	h, _, _ := withServer(t)
	rec := call(h, "GET", "/actions", "", nil)
	body := rec.Body.String()
	for _, want := range []string{`"name": "testapp-add-item"`, `"href": "/with/testapp/items"`, `"name": "testapp-reset"`, `"owner": true`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /actions lacks %s", want)
		}
	}
	if strings.Index(body, `"testapp-add-item"`) < strings.Index(body, `"name": "resync-links"`) {
		t.Error("the actions of the app come before the static ones")
	}
	rec = call(h, "GET", "/actions?new=testapp-look&id=3", "", nil)
	if !strings.Contains(rec.Body.String(), `"href": "/with/testapp/items/3"`) {
		t.Errorf("new=testapp-look&id=3 did not fill the path: %s", rec.Body.String())
	}
}

func TestAWriteToAnAppIsForwardedAndItsAnswerIsInTheNamespace(t *testing.T) {
	h, _, child := withServer(t)
	rec := call(h, "POST", "/with/testapp/items", "text=hi", nil)
	if rec.Code != 200 || rec.Header().Get("Location") != "/with/testapp/items/1" {
		t.Fatalf("POST = %d, Location %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"/with/testapp/items/1"`) || !strings.Contains(body, `"testapp-add-item"`) || strings.Contains(body, "example.invalid") {
		t.Errorf("the answer is not in the namespace: %s", body)
	}
	if child.hits.Load() != 1 {
		t.Errorf("the child had %d requests, want 1", child.hits.Load())
	}
}

func TestAminteRefusesWhatTheCatalogRefusesBeforeTheAppIsCalled(t *testing.T) {
	h, _, child := withServer(t)
	for name, body := range map[string]string{"an unknown field": "text=hi&bogus=1", "a missing field": ""} {
		rec := call(h, "POST", "/with/testapp/items", body, nil)
		if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"invalid"`) {
			t.Errorf("%s: %d %s, want 422 invalid", name, rec.Code, rec.Body)
		}
	}
	rec := call(h, "POST", "/with/testapp/reset", "", map[string]string{siren.SourceHeader: "agent:x"})
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"forbidden"`) {
		t.Errorf("owner action as an agent: %d %s, want 403 forbidden", rec.Code, rec.Body)
	}
	if n := child.hits.Load(); n != 0 {
		t.Errorf("the child had %d requests for refused requests, want 0", n)
	}
	if rec := call(h, "POST", "/with/testapp/reset", "", nil); rec.Code != 200 {
		t.Errorf("owner action as the owner: %d %s", rec.Code, rec.Body)
	}
}

// TestAReadOfAnAppIsForwardedWithoutTheFieldsOfThisServer: new (the form that a link asks for) and embed are this
// server's names, and the app never sees them. Any other name is the app's.
func TestAReadOfAnAppIsForwardedWithoutTheFieldsOfThisServer(t *testing.T) {
	h, _, child := withServer(t)
	for _, target := range []string{"/with/testapp/", "/with/testapp"} {
		rec := call(h, "GET", target, "", nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"count"`) {
			t.Errorf("GET %s = %d %s", target, rec.Code, rec.Body)
		}
	}
	rec := call(h, "GET", "/with/testapp/?new=testapp-add-item&k=v", "", nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := child.query.Load(); got != "k=v" {
		t.Errorf("the child got the query %q, want k=v", got)
	}
}

func TestARefusalOfTheAppPassesWithItsStatusAndCode(t *testing.T) {
	h, _, _ := withServer(t)
	rec := call(h, "GET", "/with/testapp/items/9", "", nil)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"gone"`) {
		t.Errorf("%d %s, want the 404 gone of the app", rec.Code, rec.Body)
	}
	if rec := call(h, "DELETE", "/with/testapp/items/1", "", nil); rec.Code != 404 {
		t.Errorf("a method no action takes: %d, want 404", rec.Code)
	}
}

func TestAnAppThatDoesNotRunIsUnavailable(t *testing.T) {
	h, apps, _ := withServer(t)
	apps.Remove("testapp")
	rec := call(h, "GET", "/with/testapp/", "", nil)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"unavailable"`) || rec.Header().Get("Retry-After") == "" {
		t.Errorf("%d %v %s, want 503 unavailable with Retry-After", rec.Code, rec.Header(), rec.Body)
	}
	if strings.Contains(call(h, "GET", "/actions", "", nil).Body.String(), "testapp-") {
		t.Error("GET /actions still lists an action of the app")
	}
	if rec := call(h, "GET", "/with/nosuch/", "", nil); rec.Code != 404 {
		t.Errorf("an unknown app: %d, want 404", rec.Code)
	}
	if links := appLinks(t, h); len(links) != 0 {
		t.Errorf("the root links the apps %v, want none", links)
	}
	apps.Expect("later")
	if rec := call(h, "GET", "/with/later/", "", nil); rec.Code != 503 {
		t.Errorf("an expected app: %d, want 503", rec.Code)
	}
}

func TestRegisterRefusesABadCatalogAndKeepsTheOldOne(t *testing.T) {
	h, apps, _ := withServer(t)
	for _, c := range []struct {
		why, app string
		act      siren.Action
	}{
		{"a name of the static catalog (create-page)", "create", siren.Action{Name: "page", Method: "GET", Href: "/p"}},
		{"a name of another app (testapp-add-item)", "testapp-add", siren.Action{Name: "item", Method: "GET", Href: "/x"}},
		{"a bad href", "testapp", siren.Action{Name: "ok", Method: "GET", Href: "http://x/"}},
		{"a bad name", "testapp", siren.Action{Name: "Add_Item", Method: "GET", Href: "/x"}},
		{"a file field", "testapp", siren.Action{Name: "up", Method: "POST", Href: "/up", Fields: []siren.Field{{Name: "f", Type: "file"}}}},
	} {
		changed := catalogServer(t, "Changed", []string{"actions"}, c.act)
		if _, err := apps.Register(t.Context(), c.app, changed.URL); err == nil || !strings.Contains(err.Error(), "catalog refused") {
			t.Errorf("%s: error %v, want catalog refused", c.why, err)
		}
	}
	if rec := call(h, "GET", "/with/testapp/", "", nil); rec.Code != 200 {
		t.Errorf("the earlier app: %d, want 200", rec.Code)
	}
	if body := call(h, "GET", "/actions", "", nil).Body.String(); !strings.Contains(body, "testapp-add-item") || strings.Contains(body, "Changed") {
		t.Error("the earlier catalog is not as it was")
	}
	if links := appLinks(t, h); len(links) != 1 || links[0] != "/with/testapp" {
		t.Errorf("the root links the apps %v, want [/with/testapp]", links)
	}
}

// catalogServer is a child that serves a catalog of the class and the actions, and a home with the title, and nothing else.
func catalogServer(t *testing.T, title string, class []string, actions ...siren.Action) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := &siren.Entity{Class: class, Title: title, Actions: actions}
		if r.URL.Path == "/" {
			e = &siren.Entity{Class: []string{"home"}, Title: title}
		}
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		if err := json.NewEncoder(w).Encode(e); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRegisterSaysWhatTheChildBecame: the handshake gives an app its catalog and the title of its home (its name when the
// home gives none), and says what it made for the line of the log.
func TestRegisterSaysWhatTheChildBecame(t *testing.T) {
	apps := api.NewApps()
	ping := siren.Action{Name: "ping", Method: "POST", Href: "/ping"}
	got, err := apps.Register(t.Context(), "demo", catalogServer(t, "Demo home", []string{"actions"}, ping).URL)
	if err != nil || got != (api.Registered{Actions: 1}) {
		t.Fatalf("Register = %+v, %v; want an app of 1 action", got, err)
	}
	if _, err := apps.Register(t.Context(), "bare", catalogServer(t, "", []string{"actions"}, ping).URL); err != nil {
		t.Fatal(err)
	}
	_, _, s := apitest.Fresh(t, "cli")
	h := api.NewWith(s, apps)
	root := call(h, "GET", "/", "", nil).Body.String()
	if !strings.Contains(root, `"title": "Demo home"`) || !strings.Contains(root, `"title": "bare"`) {
		t.Errorf("the root does not link the apps by the title of the home, or by the name when the home has none: %s", root)
	}
}

// TestRegisterTellsAChildThatIsNotUpFromOneWithNoCatalog: a child that does not answer yet gives an error that wraps the
// dial, so a caller can try again. A child that answers and has no catalog gives an error that wraps nothing.
func TestRegisterTellsAChildThatIsNotUpFromOneWithNoCatalog(t *testing.T) {
	apps := api.NewApps()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	_, err := apps.Register(t.Context(), "demo", gone.URL)
	var op *net.OpError
	if err == nil || !errors.As(err, &op) || op.Op != "dial" {
		t.Errorf("a child that is not up: %v, want an error that wraps the dial", err)
	}
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
		want string
	}{
		{"an error entity", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.siren+json")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"class":["error"],"properties":{"status":404,"code":"not_found","message":"no"}}`)
		}, "no catalog (status 404)"},
		{"an error status with no entity", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusNotFound) }, "no catalog (HTTP 404"},
		{"an answer that is not an entity", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") }, "no catalog ("},
		{"an entity with no class actions", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.siren+json")
			io.WriteString(w, `{"class":["home"],"title":"Home"}`)
		}, "no catalog (the answer is not an actions entity)"},
	} {
		srv := httptest.NewServer(tc.h)
		_, err := apps.Register(t.Context(), "demo", srv.URL)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) || errors.As(err, &op) {
			t.Errorf("%s: %v, want %q that wraps no dial", tc.name, err, tc.want)
		}
	}
}

// TestRegisterEndsWithItsContext: a child that holds the connection open does not stop the handshake after the context
// of the caller ends.
func TestRegisterEndsWithItsContext(t *testing.T) {
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(release) }) // runs before hang.Close: the open request ends first
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := api.NewApps().Register(ctx, "demo", hang.URL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Register against a child that holds the connection: %v, want the deadline", err)
	}
}

// TestARegisteredTaskServiceNeedsItsListsAndItsWrites: a child with the class tasks is the task service, not an app. The
// registry refuses it when it gives no lists, and the line says so.
func TestARegisteredTaskServiceNeedsItsListsAndItsWrites(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		if r.URL.Path == "/lists" {
			http.Error(w, "no", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, `{"class":["actions","tasks"],"title":"Actions"}`)
	}))
	t.Cleanup(service.Close)
	_, err := api.NewApps().Register(t.Context(), "svc", service.URL)
	if err == nil || !strings.Contains(err.Error(), "catalog refused: the task service gives no lists") {
		t.Errorf("a task service with no lists: %v, want catalog refused: the task service gives no lists", err)
	}
}

func TestAnAppThatAnswersNoEntityIsBadGateway(t *testing.T) {
	_, _, s := apitest.Fresh(t, "cli")
	text := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/actions" { // a catalog with no action, then text for anything else
			w.Header().Set("Content-Type", "application/vnd.siren+json")
			io.WriteString(w, `{"class":["actions"],"title":"Actions"}`)
			return
		}
		io.WriteString(w, "hello")
	}))
	t.Cleanup(text.Close)
	apps := api.NewApps()
	if _, err := apps.Register(t.Context(), "plain", text.URL); err != nil {
		t.Fatal(err)
	}
	rec := call(api.NewWith(s, apps), "GET", "/with/plain/", "", nil)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), `"upstream"`) || strings.Contains(rec.Body.String(), "hello") {
		t.Errorf("%d %s, want 502 upstream with no cause", rec.Code, rec.Body)
	}
}
