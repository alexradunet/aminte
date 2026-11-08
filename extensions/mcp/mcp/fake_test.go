package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"aminte/client"
	"aminte/siren"
)

// remote is a client over an HTTP server that serves h, closed at the end of the test.
func remote(t *testing.T, h http.Handler, source string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return client.Remote(srv.URL, source, "")
}

// fakeClient is a client of a fake API: it serves a fixed catalog (fakeCatalog) and answers the few reads and writes
// that the tests make. The real API is held by the end-to-end test (e2e_test.go).
func fakeClient(t *testing.T) *client.Client {
	return remote(t, http.HandlerFunc(fakeAPI), "agent:test")
}

func props(t *testing.T, e *siren.Entity) map[string]any {
	t.Helper()
	p, ok := e.Properties.(map[string]any)
	if !ok {
		t.Fatalf("the entity %q has no properties", e.Title)
	}
	return p
}

func fakeAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.siren+json")
	e := siren.Entity{}
	switch {
	case r.URL.Path == "/actions":
		e.Actions = fakeCatalog()
	case strings.HasPrefix(r.URL.Path, "/fake/days/"):
		e.Title = strings.TrimPrefix(r.URL.Path, "/fake/days/")
		e.Properties = map[string]any{"view": map[string]any{"entries": []any{}}}
	case r.URL.Path == "/fake/months/2031-05":
		e.Title = "May 2031"
	case r.URL.Path == "/fake/files" && r.Method == "POST": // add-file: say what arrived
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f, header, err := r.FormFile("original")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()
		n, _ := io.Copy(io.Discard, f)
		e.Title = header.Filename
		e.Properties = map[string]any{"size": strconv.FormatInt(n, 10)}
		e.Links = []siren.Link{{Rel: []string{"preview"}, Href: "/fake/pages/1/preview"}}
	}
	json.NewEncoder(w).Encode(e)
}

// fakeCatalog is a small, synthetic catalog in the shape of GET /actions. Its paths are under /fake and it does not
// copy the real catalog. It has the kinds of action that the tests need: a read, a date, a number with options, a
// file, a path field, a dangerous action, and two owner actions (a read and a write) that are never tools.
func fakeCatalog() []siren.Action {
	day := func(title string, in string) siren.Field {
		return siren.Field{Name: "day", Type: "date", Title: title, In: in}
	}
	return []siren.Action{
		{Name: "find", Method: "GET", Href: "/fake/pages", Fields: []siren.Field{{Name: "title", Type: "text", Title: "Title"}}},
		{Name: "browse", Method: "GET", Href: "/fake/browse"},
		{Name: "random-page", Method: "GET", Href: "/fake/random"},
		{Name: "open-day", Method: "GET", Href: "/fake/days/{day}", Fields: []siren.Field{day("Day (default today)", "path")}},
		{Name: "capture", Method: "POST", Href: "/fake/days/{day}/capture", Fields: []siren.Field{
			{Name: "day", Type: "date", Title: "Day", In: "path", Required: true}, {Name: "text", Type: "textarea", Title: "Text"},
			{Name: "mood", Type: "number", Title: "Mood", Options: []string{"1", "2", "3", "4", "5"}},
			{Name: "import_key", Type: "text", Title: "Key"}}},
		{Name: "record", Method: "POST", Href: "/fake/measurements", Fields: []siren.Field{
			{Name: "metric", Type: "text", Title: "Metric", Required: true}, day("Day", ""), {Name: "value", Type: "number", Title: "Value", Required: true}}},
		{Name: "revive", Method: "POST", Href: "/fake/pages/{id}/revive", Fields: []siren.Field{{Name: "id", Type: "number", In: "path", Required: true}}},
		{Name: "add-file", Method: "POST", Href: "/fake/files", Fields: []siren.Field{
			{Name: "original", Type: "file", Title: "The file", Required: true},
			day("Its day (default: a JPEG's EXIF date, else none; any other file: today)", "")}},
		{Name: "create-person", Method: "POST", Href: "/fake/people", Fields: []siren.Field{
			{Name: "title", Type: "text", Title: "Name", Required: true}, {Name: "birth_day", Type: "date", Title: "Born"}, {Name: "death_day", Type: "date", Title: "Died"}}},
		{Name: "fake-forget", Danger: true, Method: "POST", Href: "/fake/pages/{id}/forget", Fields: []siren.Field{{Name: "id", Type: "number", In: "path", Required: true}}},
		{Name: "fake-owner-read", Owner: true, Method: "GET", Href: "/fake/owner/read"},
		{Name: "fake-owner-write", Owner: true, Method: "POST", Href: "/fake/owner/write"},
	}
}
