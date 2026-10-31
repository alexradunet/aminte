// Package lifelogtest is the test kit of the extensions. It is for the tests of an extension only (a file that ends in
// _test.go): a program never imports it. It gives a test a real lifelog server over a fresh, synthetic life.db in the
// folder of the test, with a fixed clock, so an extension is tested against the real handler and not against a
// copy of its answers. It opens no other file.
package lifelogtest

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// Server is a lifelog server in the process of the test.
type Server struct {
	// URL is the address of the server on a loopback socket, for example http://127.0.0.1:41234. A client of the API
	// (lifelog/client) dials it, and so does an extension under test.
	URL string
	// Handler is the handler that answers at URL. A test calls it in the process to compare its answer with the
	// answer of another program that reads through URL.
	Handler http.Handler
	apps    *api.Apps
}

// New makes a fresh life.db in the folder of t, and a server over it. The clock of the server stands at the start of
// day (YYYY-MM-DD) in UTC, so "today" and every word for a time are fixed. The server needs no token. The cleanup of
// t closes the server and the file. A test that needs a second clock makes a second server.
func New(t testing.TB, day string) *Server {
	t.Helper()
	start, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatalf("lifelogtest.New: %v", err)
	}
	store := &core.Store{DB: dbtest.New(t), Clock: func() time.Time { return start }, Zone: time.UTC}
	store.SettingsPath = core.SettingsPathFor(store.DB.Path()) // beside the file, as the program puts it
	apps := api.NewApps()
	handler := api.NewWith(store, apps)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close) // t.Cleanup runs last in, first out: the server closes before the file
	return &Server{URL: server.URL, Handler: handler, apps: apps}
}

// Connect registers h as the connected app name, as `lifelog serve --with name` does when the program answers its
// handshake (extensions/README.md, "A catalog of its own"): its actions join GET /actions as name-action, and the routes
// under /with/name forward to it. h runs in the process, on a loopback socket that the cleanup of t closes before the
// server. It fails t when the catalog is refused, with the words of the server.
func (s *Server) Connect(t testing.TB, name string, h http.Handler) {
	t.Helper()
	app := httptest.NewServer(h)
	t.Cleanup(app.Close)
	c := client.Remote(app.URL, "", "")
	catalog, err := c.Get(t.Context(), "/actions")
	if err != nil {
		t.Fatalf("lifelogtest.Connect %s: GET /actions: %v", name, err)
	}
	title := name
	if home, err := c.Get(t.Context(), "/"); err == nil && home.Title != "" {
		title = home.Title
	}
	if err := s.apps.Set(name, app.URL, title, catalog.Actions); err != nil {
		t.Fatalf("lifelogtest.Connect %s: %v", name, err)
	}
}

// ConnectTasks registers h as the task service name, as `lifelog serve --with name` does when the catalog of the program
// has the class tasks (extensions/README.md, "A task service"): the server reads GET /lists of h and draws Tasks under
// /tasks from it. h runs in the process, on a loopback socket that the cleanup of t closes before the server. It fails
// t when the catalog has no class tasks, when the lists cannot be read, or when the server refuses the service.
func (s *Server) ConnectTasks(t testing.TB, name string, h http.Handler) {
	t.Helper()
	service := httptest.NewServer(h)
	t.Cleanup(service.Close)
	c := client.Remote(service.URL, "", "")
	catalog, err := c.Get(t.Context(), "/actions")
	if err != nil {
		t.Fatalf("lifelogtest.ConnectTasks %s: GET /actions: %v", name, err)
	}
	if !slices.Contains(catalog.Class, "tasks") {
		t.Fatalf("lifelogtest.ConnectTasks %s: the catalog has the class %v, not tasks", name, catalog.Class)
	}
	listed, err := c.Get(t.Context(), "/lists")
	if err != nil {
		t.Fatalf("lifelogtest.ConnectTasks %s: GET /lists: %v", name, err)
	}
	lists, err := api.TaskListsOf(listed)
	if err != nil {
		t.Fatalf("lifelogtest.ConnectTasks %s: %v", name, err)
	}
	if err := s.apps.SetTasks(name, service.URL, catalog.Actions, lists); err != nil {
		t.Fatalf("lifelogtest.ConnectTasks %s: %v", name, err)
	}
}
