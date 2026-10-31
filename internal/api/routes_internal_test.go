package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/siren"
)

// panicOf runs f and returns what it panics with, "" when it does not panic.
func panicOf(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return ""
}

// TestMountPanicsWhenTheCatalogAndTheHandlersDisagree: an action with no handler, or a handler with no action, stops
// the server at start. When they agree, each action gets a route with its method and its path.
func TestMountPanicsWhenTheCatalogAndTheHandlersDisagree(t *testing.T) {
	run := func(*http.Request, admitted) (*siren.Entity, error) { return &siren.Entity{}, nil }
	cat := []spec{{Name: "read", Method: "GET", Path: "/read"}, {Name: "write", Method: "POST", Path: "/things/{id}/write"}}
	for _, tc := range []struct {
		name     string
		handlers map[string]actionFunc
		want     string // what the panic says; "" when mount does not panic
	}{
		{"an action with no handler", map[string]actionFunc{"read": run}, "the action write has no handler"},
		{"an action with a nil handler", map[string]actionFunc{"read": run, "write": nil}, "the action write has no handler"},
		{"a handler with no action", map[string]actionFunc{"read": run, "write": run, "extra": run}, "the handler extra has no action"},
		{"the two agree", map[string]actionFunc{"read": run, "write": run}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := http.NewServeMux()
			got := panicOf(func() { (&server{}).mount(m, cat, tc.handlers) })
			if (got == "") != (tc.want == "") || !strings.Contains(got, tc.want) {
				t.Fatalf("panic %q, want %q", got, tc.want)
			}
			if tc.want != "" {
				return
			}
			for _, s := range cat {
				req := httptest.NewRequest(s.Method, strings.ReplaceAll(s.Path, "{id}", "7"), nil)
				if _, pattern := m.Handler(req); pattern != s.Method+" "+s.Path {
					t.Errorf("%s %s routes to %q, want %q", req.Method, req.URL.Path, pattern, s.Method+" "+s.Path)
				}
			}
		})
	}
}

// TestMountReadsTheWriterAndRefusesAnAgentAnOwnerAction: an action that writes reads its writer, and an Owner action
// refuses an agent or an import. A read that is not the owner's alone reads no writer: a source header that is not
// valid does not refuse it. A write passes its writer on without a check: the CHECKs of the rows refuse a name that
// is not valid (TestMalformedWriterIsRefusedByTheSchemaOrTheOwnerCheck). The handler gets the row of its own action.
func TestMountReadsTheWriterAndRefusesAnAgentAnOwnerAction(t *testing.T) {
	ran, src, given := "", "", ""
	handlers := map[string]actionFunc{}
	cat := []spec{
		{Name: "read", Method: "GET", Path: "/read"},
		{Name: "check", Method: "GET", Path: "/check", Owner: true},
		{Name: "write", Method: "POST", Path: "/things/{id}/write"},
		{Name: "keep", Method: "POST", Path: "/keep", Owner: true},
	}
	for _, s := range cat {
		handlers[s.Name] = func(_ *http.Request, a admitted) (*siren.Entity, error) {
			ran, src, given = s.Name, a.src, a.act.Method+" "+a.act.Path
			return &siren.Entity{Title: s.Name}, nil
		}
	}
	m := http.NewServeMux()
	(&server{}).mount(m, cat, handlers)
	for _, tc := range []struct {
		method, path, source string
		status               int
		src                  string // the writer the handler gets, when it runs
	}{
		{"GET", "/read", "Bad Source!", http.StatusOK, ""},
		{"GET", "/read", "agent:x", http.StatusOK, ""},
		{"GET", "/check", "", http.StatusOK, "api"},
		{"GET", "/check", "cli", http.StatusOK, "cli"},
		{"GET", "/check", "agent:x", http.StatusForbidden, ""},
		{"GET", "/check", "import:x", http.StatusForbidden, ""},
		{"GET", "/check", "Bad Source!", http.StatusUnprocessableEntity, ""},
		{"POST", "/things/7/write", "", http.StatusOK, "api"},
		{"POST", "/things/7/write", "agent:x", http.StatusOK, "agent:x"},
		{"POST", "/things/7/write", "Bad Source!", http.StatusOK, "Bad Source!"}, // the schema refuses it when a row carries it
		{"POST", "/keep", "cli", http.StatusOK, "cli"},
		{"POST", "/keep", "agent:x", http.StatusForbidden, ""},
		{"POST", "/keep", "import:x", http.StatusForbidden, ""},
	} {
		ran, src, given = "", "", ""
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.source != "" {
			req.Header.Set(siren.SourceHeader, tc.source)
		}
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s %s as %q: status %d, want %d: %.200s", tc.method, tc.path, tc.source, rec.Code, tc.status, rec.Body.String())
		}
		if didRun := ran != ""; didRun != (tc.status == http.StatusOK) || src != tc.src {
			t.Errorf("%s %s as %q: the handler ran %v with the writer %q, want %v with %q", tc.method, tc.path, tc.source, didRun, src, tc.status == http.StatusOK, tc.src)
		}
		if _, pattern := m.Handler(req); ran != "" && given != pattern {
			t.Errorf("%s %s: the handler of %s got the row of %q, want %q", tc.method, tc.path, ran, given, pattern)
		}
	}
}

// TestMountAdmitsARequestBeforeTheHandlerRuns: admit is the one opening of an action. An Owner row refuses a writer that
// is not the owner first, then a path id that is not a number is 404, then the fields are checked (required, unknown
// name, 415 for a multipart body). The handler runs only after all that, and gets the id and the fields. An action with a
// file field gets no fields from admit: parseFileForm reads them.
func TestMountAdmitsARequestBeforeTheHandlerRuns(t *testing.T) {
	var got admitted
	var ran bool
	handlers := map[string]actionFunc{}
	cat := []spec{
		{Name: "edit", Method: "POST", Path: "/things/{id}/edit",
			Fields: []siren.Field{path("id", "number", "Thing"), req("title", "text", "Title")}},
		{Name: "keep", Method: "POST", Path: "/keep", Owner: true, Fields: []siren.Field{opt("word", "text", "Word")}},
		{Name: "send", Method: "POST", Path: "/send", Fields: []siren.Field{opt("original", "file", "File")}},
		{Name: "word", Method: "POST", Path: "/words/{id}", Fields: []siren.Field{path("id", "text", "Word")}},
	}
	for _, s := range cat {
		handlers[s.Name] = func(_ *http.Request, a admitted) (*siren.Entity, error) {
			ran, got = true, a
			return &siren.Entity{Title: s.Name}, nil
		}
	}
	m := http.NewServeMux()
	(&server{}).mount(m, cat, handlers)
	const form = "application/x-www-form-urlencoded"
	const multipart = "multipart/form-data; boundary=x"
	post := func(path, source, ctype, body string) *httptest.ResponseRecorder {
		ran, got = false, admitted{}
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		if source != "" {
			req.Header.Set(siren.SourceHeader, source)
		}
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		name, path, source, ctype, body string
		status                          int
	}{
		{"the id and the fields", "/things/7/edit", "", form, "title=a", http.StatusOK},
		{"an id that is not a number", "/things/seven/edit", "", form, "title=a", http.StatusNotFound},
		{"a missing field", "/things/7/edit", "", form, "", http.StatusUnprocessableEntity},
		{"an unknown name", "/things/7/edit", "", form, "title=a&nope=1", http.StatusUnprocessableEntity},
		{"a multipart body", "/things/7/edit", "", multipart, "--x--", http.StatusUnsupportedMediaType},
		{"an agent at an owner action, with an unknown name", "/keep", "agent:x", form, "nope=1", http.StatusForbidden},
		{"the owner at an owner action, with an unknown name", "/keep", "cli", form, "nope=1", http.StatusUnprocessableEntity},
		{"a file field", "/send", "", multipart, "--x--", http.StatusOK},
		{"an id that is text", "/words/seven", "", form, "", http.StatusOK},
	} {
		rec := post(tc.path, tc.source, tc.ctype, tc.body)
		if rec.Code != tc.status || ran != (tc.status == http.StatusOK) {
			t.Errorf("%s: status %d, handler ran %v; want %d, ran %v: %.200s", tc.name, rec.Code, ran, tc.status, tc.status == http.StatusOK, rec.Body.String())
		}
	}
	post("/things/7/edit", "", form, "title=a")
	if got.id != 7 || got.v.Get("title") != "a" || got.src != "api" || got.act.Name != "edit" {
		t.Errorf("the handler got id %d, title %q, writer %q, action %q; want 7, a, api, edit", got.id, got.v.Get("title"), got.src, got.act.Name)
	}
	post("/send", "", multipart, "--x--")
	if got.v != nil {
		t.Errorf("the action with a file field got fields from admit: %v", got.v)
	}
}

// TestTheForwardAdmitsTheRowOfTheAppAndParsesNoId: the route of the forward to an app names no {id}, so admit parses no
// id, whatever the app says of its path fields. The values that the forward sends are the fields that the request names.
func TestTheForwardAdmitsTheRowOfTheAppAndParsesNoId(t *testing.T) {
	row := specOfApp("demo", siren.Action{Name: "touch", Method: "POST", Href: "/things/{id}/touch",
		Fields: []siren.Field{path("id", "number", "Thing"), opt("note", "text", "Note"), opt("other", "text", "Other")}})
	req := httptest.NewRequest("POST", "/with/demo/things/abc/touch", strings.NewReader("note=n"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a, err := admit(req, row)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if a.id != 0 {
		t.Errorf("id %d, want 0: the route has no {id}", a.id)
	}
	if got := a.sent(); len(got) != 1 || got["note"] != "n" {
		t.Errorf("sent %v, want only note=n: a field that the request leaves out stays out", got)
	}
}

// TestTheServerRoutesEachCatalogActionToItsOwnRoute asks the mux of the real server, which mount and the routes that
// are not actions fill, which pattern takes a request of each catalog action. The pattern must be the action's own
// method and path. A route that only takes the same path is not enough: GET /days/go must go to open-day, not to
// GET /days/{day}. The routes do not use the store until a request runs, so the test gives none.
func TestTheServerRoutesEachCatalogActionToItsOwnRoute(t *testing.T) {
	h := newServer(nil, NewApps())
	fill := strings.NewReplacer("{id}", "7", "{day}", "2026-09-01")
	for _, s := range catalog {
		path := fill.Replace(s.Path)
		if strings.Contains(path, "{") {
			t.Fatalf("%s: the test fills no value for a path field of %s", s.Name, s.Path)
		}
		req := httptest.NewRequest(s.Method, path, nil)
		if _, pattern := h.mux.Handler(req); pattern != s.Method+" "+s.Path {
			t.Errorf("%s %s (%s) goes to the route %q, want %q", s.Method, path, s.Name, pattern, s.Method+" "+s.Path)
		}
	}
}
