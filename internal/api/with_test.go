package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"lifelog/app"
	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/siren"
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
	if err := apps.Set("testapp", child.srv.URL, "Test app", childActions()); err != nil {
		t.Fatal(err)
	}
	return api.NewWith(s, apps), apps, child
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

func TestLifelogRefusesWhatTheCatalogRefusesBeforeTheAppIsCalled(t *testing.T) {
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
	if names := apps.Names(); len(names) != 0 {
		t.Errorf("Names = %v, want none", names)
	}
	apps.Expect("later")
	if rec := call(h, "GET", "/with/later/", "", nil); rec.Code != 503 {
		t.Errorf("an expected app: %d, want 503", rec.Code)
	}
}

func TestAppsSetRefusesABadCatalogAndKeepsTheOldOne(t *testing.T) {
	h, apps, child := withServer(t)
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
		if err := apps.Set(c.app, child.srv.URL, "Changed", []siren.Action{c.act}); err == nil {
			t.Errorf("%s: want an error", c.why)
		}
	}
	if rec := call(h, "GET", "/with/testapp/", "", nil); rec.Code != 200 {
		t.Errorf("the earlier app: %d, want 200", rec.Code)
	}
	if body := call(h, "GET", "/actions", "", nil).Body.String(); !strings.Contains(body, "testapp-add-item") || strings.Contains(body, "Changed") {
		t.Error("the earlier catalog is not as it was")
	}
	if names := apps.Names(); len(names) != 1 || names[0] != "testapp" {
		t.Errorf("Names = %v, want [testapp]", names)
	}
}

func TestAnAppThatAnswersNoEntityIsBadGateway(t *testing.T) {
	_, _, s := apitest.Fresh(t, "cli")
	text := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") }))
	t.Cleanup(text.Close)
	apps := api.NewApps()
	if err := apps.Set("plain", text.URL, "Plain", nil); err != nil {
		t.Fatal(err)
	}
	rec := call(api.NewWith(s, apps), "GET", "/with/plain/", "", nil)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), `"upstream"`) || strings.Contains(rec.Body.String(), "hello") {
		t.Errorf("%d %s, want 502 upstream with no cause", rec.Code, rec.Body)
	}
}
