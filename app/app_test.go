package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aminte/siren"
)

func TestListenRefuses(t *testing.T) {
	for _, tc := range []struct{ env, want string }{
		{"", "aminte serve --with <name>"},
		{"0.0.0.0:1", "loopback"},
		{"example.com:80", "loopback"},
		{"nonsense", "host:port"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("AMINTE_LISTEN", tc.env)
			err := Listen(context.Background(), http.NotFoundHandler())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error with %q", err, tc.want)
			}
		})
	}
}

func TestListenServesUntilTheContextEnds(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	t.Setenv("AMINTE_LISTEN", addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := Catalog("T", func(*http.Request) (*siren.Entity, error) { return &siren.Entity{}, nil }, nil, nil)
	ended := make(chan error, 1)
	go func() { ended <- Listen(ctx, h) }()
	var res *http.Response
	for i := 0; i < 200; i++ {
		if res, err = http.Get("http://" + addr + "/actions"); err == nil {
			break
		}
		select {
		case e := <-ended:
			t.Fatalf("Listen ended early: %v", e)
		case <-time.After(25 * time.Millisecond):
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	cancel()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("Listen: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Listen did not end")
	}
}

func entityOf(t *testing.T, h http.Handler, method, path string) (*siren.Entity, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	var e siren.Entity
	if err := json.NewDecoder(rec.Body).Decode(&e); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return &e, rec
}

func props(e *siren.Entity) map[string]any { return e.Properties.(map[string]any) }

func testCatalog() http.Handler {
	home := func(*http.Request) (*siren.Entity, error) {
		return &siren.Entity{Class: []string{"home"}, Title: "Home of T"}, nil
	}
	actions := []siren.Action{
		{Name: "T-show", Method: "GET", Href: "/items/{id}", Fields: []siren.Field{{Name: "id", Type: "text", In: "path", Required: true}}},
		{Name: "T-add", Method: "POST", Href: "/items", Description: "add one", Owner: true},
		{Name: "T-deny", Method: "POST", Href: "/deny"},
		{Name: "T-break", Method: "POST", Href: "/break"},
	}
	run := map[string]func(*http.Request) (*siren.Entity, error){
		"T-show": func(r *http.Request) (*siren.Entity, error) {
			return &siren.Entity{Class: []string{"item"}, Properties: map[string]any{"id": r.PathValue("id")}}, nil
		},
		"T-add": func(*http.Request) (*siren.Entity, error) {
			return &siren.Entity{Class: []string{"item"}, Links: []siren.Link{{Rel: []string{"self"}, Href: "/items/7"}}}, nil
		},
		"T-deny": func(*http.Request) (*siren.Entity, error) {
			return nil, &Error{Status: 409, Code: "stale_version", Msg: "stale"}
		},
		"T-break": func(*http.Request) (*siren.Entity, error) { return nil, errors.New("secret detail") },
	}
	return Catalog("T", home, actions, run)
}

func TestCatalogServesActionsAndHome(t *testing.T) {
	h := testCatalog()
	e, rec := entityOf(t, h, "GET", "/actions")
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.siren+json" {
		t.Errorf("content type %q", ct)
	}
	if len(e.Class) != 1 || e.Class[0] != "actions" || e.Title != "Actions" || e.LinkByRel("self").Href != "/actions" || e.LinkByRel("index").Href != "/" {
		t.Errorf("actions entity: %+v", e)
	}
	got := map[string]siren.Action{}
	for _, a := range e.Actions {
		got[a.Name] = a
	}
	if len(got) != 4 || got["T-show"].Href != "/items/{id}" || got["T-show"].Method != "GET" || !got["T-add"].Owner || got["T-add"].Description != "add one" {
		t.Errorf("actions: %+v", got)
	}
	home, _ := entityOf(t, h, "GET", "/")
	if home.Title != "Home of T" || home.LinkByRel("self").Href != "/" {
		t.Errorf("home: %+v", home)
	}
}

func TestCatalogRunsAnActionsHandler(t *testing.T) {
	h := testCatalog()
	e, rec := entityOf(t, h, "GET", "/items/42")
	if rec.Code != 200 || props(e)["id"] != "42" {
		t.Errorf("show: %d %+v", rec.Code, e)
	}
	_, rec = entityOf(t, h, "POST", "/items")
	if rec.Code != 200 || rec.Header().Get("Location") != "/items/7" {
		t.Errorf("add: %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCatalogAnswersErrors(t *testing.T) {
	h := testCatalog()
	for _, tc := range []struct {
		method, path, code string
		status             int
	}{
		{"POST", "/deny", "stale_version", 409},
		{"POST", "/break", "internal", 500},
		{"GET", "/nowhere", "not_found", 404},
		{"DELETE", "/items/1", "not_found", 404},
	} {
		e, rec := entityOf(t, h, tc.method, tc.path)
		p := props(e)
		if rec.Code != tc.status || p["code"] != tc.code || len(e.Class) != 1 || e.Class[0] != "error" {
			t.Errorf("%s %s: %d %+v", tc.method, tc.path, rec.Code, e)
		}
		if strings.Contains(rec.Body.String(), "secret detail") {
			t.Errorf("%s %s: the error text leaked", tc.method, tc.path)
		}
	}
}

// TestEntityAnswersAnEntityAndEveryKindOfError holds the one helper that answers a handler: the callers are the
// actions of Catalog and the reads that a program serves beside it. An Error keeps its status and its code, also when a
// handler wraps it. Any other error, and a missing entity, is 500 internal, and the text of the cause is not sent.
func TestEntityAnswersAnEntityAndEveryKindOfError(t *testing.T) {
	selfLink := []siren.Link{{Rel: []string{"self"}, Href: "/things/7", Title: "Seven"}}
	for _, tc := range []struct {
		name, method string
		entity       *siren.Entity
		err          error
		status       int
		code         string
		location     string
	}{
		{"a read", "GET", &siren.Entity{Class: []string{"thing"}, Links: selfLink}, nil, 200, "", ""},
		{"a write with a self link", "POST", &siren.Entity{Class: []string{"thing"}, Links: selfLink}, nil, 200, "", "/things/7"},
		{"an Error", "GET", nil, &Error{Status: 404, Code: "not_found", Msg: "no such thing"}, 404, "not_found", ""},
		{"a wrapped Error", "GET", nil, fmt.Errorf("the thing: %w", &Error{Status: 409, Code: "stale_version", Msg: "stale"}), 409, "stale_version", ""},
		{"another error", "GET", nil, errors.New("secret detail"), 500, "internal", ""},
		{"no entity and no error", "GET", nil, nil, 500, "internal", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Entity(func(*http.Request) (*siren.Entity, error) { return tc.entity, tc.err })
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, "/things/7", nil))
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d:\n%s", rec.Code, tc.status, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.siren+json" {
				t.Errorf("content type %q", ct)
			}
			if got := rec.Header().Get("Location"); got != tc.location {
				t.Errorf("Location %q, want %q", got, tc.location)
			}
			var e siren.Entity
			if err := json.NewDecoder(rec.Body).Decode(&e); err != nil {
				t.Fatalf("the body is not an entity: %v", err)
			}
			if tc.code != "" && props(&e)["code"] != tc.code {
				t.Errorf("code %v, want %s", props(&e)["code"], tc.code)
			}
			if tc.err != nil && strings.Contains(rec.Body.String(), "secret detail") {
				t.Error("the text of the cause was sent")
			}
		})
	}
}

func TestCatalogPanicsOnAMismatch(t *testing.T) {
	home := func(*http.Request) (*siren.Entity, error) { return &siren.Entity{}, nil }
	noop := func(*http.Request) (*siren.Entity, error) { return &siren.Entity{}, nil }
	t.Run("action without handler", func(t *testing.T) {
		defer func() {
			if p, _ := recover().(string); !strings.Contains(p, "lonely") {
				t.Errorf("panic %v", p)
			}
		}()
		Catalog("T", home, []siren.Action{{Name: "lonely", Method: "GET", Href: "/x"}}, nil)
	})
	t.Run("handler without action", func(t *testing.T) {
		defer func() {
			if p, _ := recover().(string); !strings.Contains(p, "orphan") {
				t.Errorf("panic %v", p)
			}
		}()
		Catalog("T", home, nil, map[string]func(*http.Request) (*siren.Entity, error){"orphan": noop})
	})
}

func TestFailShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Fail(rec, 422, "invalid", "bad")
	b, _ := io.ReadAll(rec.Body)
	var e siren.Entity
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	p := props(&e)
	if rec.Code != 422 || p["code"] != "invalid" || p["message"] != "bad" || p["status"] != float64(422) {
		t.Errorf("%d %s", rec.Code, b)
	}
}

func TestStateDir(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir, err := StateDir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(cfg, "aminte-demo") {
		t.Errorf("dir %q", dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("the folder is not there: %v", err)
	}
}

// TestListenLetsInTheServerAlone: the address of a connected app is a loopback address that the server alone calls,
// with the Host of that address. A request with another Host (a page that a DNS name points at the loopback) and a
// write of a browser from another site are refused, and the handler never sees them.
func TestListenLetsInTheServerAlone(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	t.Setenv("AMINTE_LISTEN", addr)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); write(w, 200, &siren.Entity{Title: "ok"}) })
	ended := make(chan error, 1)
	go func() { ended <- Listen(ctx, h) }()
	t.Cleanup(func() { cancel(); <-ended })
	send := func(method, host string, header map[string]string) int {
		req, err := http.NewRequest(method, "http://"+addr+"/settings", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		for k, v := range header {
			req.Header.Set(k, v)
		}
		for range 200 {
			res, err := http.DefaultClient.Do(req)
			if err == nil {
				res.Body.Close()
				return res.StatusCode
			}
			time.Sleep(10 * time.Millisecond) // the listener is not up yet
		}
		t.Fatal("the app never answered")
		return 0
	}
	if got := send("POST", addr, nil); got != 200 {
		t.Fatalf("a call of the server: %d", got)
	}
	for _, tc := range []struct {
		name, host string
		header     map[string]string
	}{
		{"another host", "evil.example", nil},
		{"the loopback by another name", "localhost:" + strings.Split(addr, ":")[1], nil},
		{"a browser write from another site", addr, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}},
	} {
		if got := send("POST", tc.host, tc.header); got != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", tc.name, got)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("the handler saw %d requests, want the one of the server", n)
	}
}
