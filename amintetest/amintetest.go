// Package amintetest is the test kit of the extensions. It is for the tests of an extension only (a file that ends in
// _test.go): a program never imports it. It gives a test a real aminte server over a fresh, synthetic life.db in the
// folder of the test, with a fixed clock, so an extension is tested against the real handler and not against a
// copy of its answers. It opens no other file.
package amintetest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aminte/internal/api"
	"aminte/internal/core"
	"aminte/internal/db/dbtest"
)

// Server is an aminte server in the process of the test.
type Server struct {
	// URL is the address of the server on a loopback socket, for example http://127.0.0.1:41234. A client of the API
	// (aminte/client) dials it, and so does an extension under test.
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
		t.Fatalf("amintetest.New: %v", err)
	}
	store := &core.Store{DB: dbtest.New(t), Clock: func() time.Time { return start }, Zone: time.UTC}
	apps := api.NewApps()
	handler := api.NewWith(store, apps)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close) // t.Cleanup runs last in, first out: the server closes before the file
	return &Server{URL: server.URL, Handler: handler, apps: apps}
}

// Connect registers h as the child name of `serve --with name`, by the same handshake (api.Apps.Register): the server
// reads GET /actions of h, which must have the class actions, as serve requires (extensions/README.md, "A catalog of
// its own"). With the class tasks too, h is the task service (extensions/README.md, "A task service"): the server reads
// GET /lists of h and draws Tasks under /tasks from it. Else h is a connected app: its actions join GET /actions as
// name-action, and the routes under /with/name forward to it. h runs in the process, on a loopback socket that the
// cleanup of t closes before the server. It fails t when the server refuses the catalog, with the words of the server.
func (s *Server) Connect(t testing.TB, name string, h http.Handler) {
	t.Helper()
	child := httptest.NewServer(h)
	t.Cleanup(child.Close)
	if _, err := s.apps.Register(t.Context(), name, child.URL); err != nil {
		t.Fatalf("amintetest.Connect %s: %v", name, err)
	}
}
