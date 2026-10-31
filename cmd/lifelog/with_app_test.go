package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/siren"
)

var (
	withAppOnce  sync.Once
	withAppBytes []byte
	withAppErr   error
)

// withAppAs builds testdata/withapp once and writes a copy of it as lifelog-NAME into dir.
func withAppAs(t *testing.T, dir, name string) {
	t.Helper()
	withAppOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "withapp")
		if err != nil {
			withAppErr = err
			return
		}
		defer os.RemoveAll(tmp)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		bin := filepath.Join(tmp, "withapp")
		if out, err := exec.CommandContext(ctx, "go", "build", "-o", bin, "./testdata/withapp").CombinedOutput(); err != nil {
			withAppErr = errors.New("build: " + err.Error() + "\n" + string(out))
			return
		}
		withAppBytes, withAppErr = os.ReadFile(bin)
	})
	if withAppErr != nil {
		t.Fatal(withAppErr)
	}
	path := filepath.Join(dir, "lifelog-"+name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, withAppBytes, 0o755); err != nil {
		t.Fatal(err)
	}
}

// startWithAppServer starts `lifelog serve --with testapp` and returns when the app has registered its catalog.
func startWithAppServer(t *testing.T) *withServer {
	t.Helper()
	w := newWithServer(t)
	withAppAs(t, w.dir, "testapp")
	w.start(t, []string{"--db", w.db, "serve", "--addr", "127.0.0.1:0", "--with", "testapp"}, nil)
	w.waitFor(t, "with testapp: a catalog of 6 actions on 127.0.0.1:")
	return w
}

func catalogOf(t *testing.T, w *withServer, source string) map[string]siren.Action {
	t.Helper()
	actions, err := client.Remote(w.base, source, "").Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]siren.Action{}
	for _, a := range actions {
		byName[a.Name] = a
	}
	return byName
}

func countOf(t *testing.T, w *withServer) float64 {
	t.Helper()
	e, err := client.Remote(w.base, "", "").Get(t.Context(), "/with/testapp/")
	if err != nil {
		t.Fatal(err)
	}
	p, ok := e.Properties.(map[string]any)
	if !ok {
		t.Fatalf("the home of the app has no properties: %+v", e)
	}
	n, _ := p["count"].(interface{ Float64() (float64, error) })
	if n == nil {
		t.Fatalf("count is %T, want a number", p["count"])
	}
	f, _ := n.Float64()
	return f
}

// TestAConnectedAppIsInTheCatalogOfTheServer runs the whole mechanism on the real server: the child gets an address,
// the server reads its catalog, merges it, forwards requests, and takes the app out when the child ends.
func TestAConnectedAppIsInTheCatalogOfTheServer(t *testing.T) {
	w := startWithAppServer(t)
	ctx := t.Context()

	t.Run("the actions are in the catalog with the prefix", func(t *testing.T) {
		got := catalogOf(t, w, "")
		if a := got["testapp-add-item"]; a.Href != "/with/testapp/items" || a.Method != "POST" {
			t.Errorf("testapp-add-item = %s %s, want POST /with/testapp/items", a.Method, a.Href)
		}
		if a := got["testapp-look"]; a.Href != "/with/testapp/items/{id}" {
			t.Errorf("testapp-look href = %q, want /with/testapp/items/{id}", a.Href)
		}
		if !got["testapp-reset"].Owner {
			t.Errorf("testapp-reset must be owner-only: %+v", got["testapp-reset"])
		}
	})

	t.Run("do runs an action of the app and the app counts", func(t *testing.T) {
		e, stdout, _ := testEnv(t)
		if err := runArgs(ctx, e, []string{"do", "testapp-add-item", "text=hi", "--url", w.base}); err != nil {
			t.Fatalf("do: %v", err)
		}
		if !strings.Contains(stdout.String(), "/with/testapp/items/1") {
			t.Errorf("the output does not name /with/testapp/items/1:\n%s", stdout)
		}
		if got := countOf(t, w); got != 1 {
			t.Errorf("count = %v, want 1", got)
		}
	})

	t.Run("an unknown field is refused by the server", func(t *testing.T) {
		e, _, _ := testEnv(t)
		err := runArgs(ctx, e, []string{"do", "testapp-add-item", "text=hi", "bogus=1", "--url", w.base})
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 422 || !strings.Contains(ce.Error(), "bogus") {
			t.Errorf("got %v, want a 422 that names bogus", err)
		}
		if got := countOf(t, w); got != 1 {
			t.Errorf("count = %v, want 1: a refused request must not reach the app", got)
		}
	})

	t.Run("an owner action is refused to an agent and runs for the owner", func(t *testing.T) {
		reset := catalogOf(t, w, "")["testapp-reset"]
		_, err := client.Remote(w.base, "agent:x", "").Do(ctx, reset, nil)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 403 || ce.Code() != "forbidden" {
			t.Errorf("agent: got %v, want 403 forbidden", err)
		}
		if _, err := client.Remote(w.base, "", "").Do(ctx, reset, nil); err != nil {
			t.Fatalf("owner: %v", err)
		}
		if got := countOf(t, w); got != 0 {
			t.Errorf("count = %v, want 0 after the reset", got)
		}
	})

	t.Run("a client that accepts HTML gets the entity of the app", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, "GET", w.base+"/with/testapp/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/html")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		var e siren.Entity
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/vnd.siren+json" || json.Unmarshal(b, &e) != nil {
			t.Fatalf("status %d, type %q, want 200 and Siren JSON\n%s", res.StatusCode, res.Header.Get("Content-Type"), b)
		}
		var add string
		for _, a := range e.Actions {
			if a.Name == "testapp-add-item" {
				add = a.Href
			}
		}
		if e.Title != "Test app" || add != "/with/testapp/items" {
			t.Errorf("the entity of the app: title %q, add-item at %q, want Test app and /with/testapp/items", e.Title, add)
		}
		if l := mustGet(t, w, "", "/").LinkByRel("app"); l == nil || l.Href != "/with/testapp" {
			t.Errorf("the root links the app as %+v, want /with/testapp", l)
		}
	})

	t.Run("the app ends and the server takes it out", func(t *testing.T) {
		stop := catalogOf(t, w, "")["testapp-stop"]
		if _, err := client.Remote(w.base, "", "").Do(ctx, stop, nil); err != nil {
			t.Fatalf("stop: %v", err)
		}
		w.waitFor(t, "with testapp: ended")
		for name := range catalogOf(t, w, "") {
			if strings.HasPrefix(name, "testapp-") {
				t.Errorf("the catalog still holds %s", name)
			}
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", w.base+"/with/testapp/", nil)
		req.Header.Set("Accept", "application/vnd.siren+json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 503 || res.Header.Get("Retry-After") == "" {
			t.Errorf("status %d, Retry-After %q, want 503 with a Retry-After", res.StatusCode, res.Header.Get("Retry-After"))
		}
		_, err = client.Remote(w.base, "", "").Get(ctx, "/with/testapp/")
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Code() != "unavailable" {
			t.Errorf("got %v, want the code unavailable", err)
		}
		_, err = client.Remote(w.base, "", "").Get(ctx, "/with/nosuch/")
		if !errors.As(err, &ce) || ce.Status != 404 {
			t.Errorf("an unknown app: got %v, want 404", err)
		}
	})
}

// TestTheRowsOfAnAppCarryTheirActions: an embedded link of an app carries a properties and an action; the server puts
// the action in its namespace. The form of the row posts through /with/testapp and the app runs the action.
func TestTheRowsOfAnAppCarryTheirActions(t *testing.T) {
	w := startWithAppServer(t)
	ctx := t.Context()
	e, _, _ := testEnv(t)
	if err := runArgs(ctx, e, []string{"do", "testapp-add-item", "text=milk", "--url", w.base}); err != nil {
		t.Fatalf("do: %v", err)
	}

	home, err := client.Remote(w.base, "", "").Get(ctx, "/with/testapp/")
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Entities) != 1 || len(home.Entities[0].Actions) != 1 {
		t.Fatalf("the home embeds %+v, want one item with one action", home.Entities)
	}
	if a := home.Entities[0].Actions[0]; a.Href != "/with/testapp/items/1/done" || a.Name != "testapp-done-item" {
		t.Errorf("the action of the row = %s %s, want testapp-done-item at /with/testapp/items/1/done", a.Name, a.Href)
	}
	if c := home.Entities[0].Actions[0].Class; len(c) != 1 || c[0] != "toggle" {
		t.Errorf("the class of the action of the row = %v, want [toggle]", c)
	}
	if p := home.Entities[0].Properties; p == nil {
		t.Error("the row carries no properties")
	}

	res, err := http.PostForm(w.base+"/with/testapp/items/1/done", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("the form of the row: status %d, want 200", res.StatusCode)
	}
	if p, _ := countsOf(t, w)["done"].(interface{ Float64() (float64, error) }); p == nil {
		t.Error("the home has no done count")
	} else if f, _ := p.Float64(); f != 1 {
		t.Errorf("done = %v, want 1: the app did not run the action", f)
	}
}
func countsOf(t *testing.T, w *withServer) map[string]any {
	t.Helper()
	e, err := client.Remote(w.base, "", "").Get(t.Context(), "/with/testapp/")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.Properties.(map[string]any)
	return p
}

// TestAConnectedAppIsReadAgainAfterARestart:a child that ends with status 1 is started again at a new address, and
// the server reads its catalog again. The new process has no state.
func TestAConnectedAppIsReadAgainAfterARestart(t *testing.T) {
	w := startWithAppServer(t)
	c := client.Remote(w.base, "", "")
	if _, err := c.Do(t.Context(), catalogOf(t, w, "")["testapp-add-item"], map[string]string{"text": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(t.Context(), catalogOf(t, w, "")["testapp-crash"], nil); err != nil {
		t.Fatal(err)
	}
	w.waitFor(t, "with testapp: ended with status 1, started again in ")
	// The first catalog line is the one of the first start: wait for a second one.
	waitUntil(t, func() (struct{}, bool) {
		return struct{}{}, strings.Count(w.stderr.String(), "with testapp: a catalog of 6 actions on 127.0.0.1:") == 2
	}, "the catalog of the second start")
	if _, ok := catalogOf(t, w, "")["testapp-add-item"]; !ok {
		t.Error("testapp-add-item is not in the catalog after the restart")
	}
	if got := countOf(t, w); got != 0 {
		t.Errorf("count = %v, want 0: the new process starts with no items", got)
	}
}

// TestAChildThatServesNoCatalogStaysAChild: the fake extension does not serve LIFELOG_LISTEN. After probeFor the
// server says so, goes on, and answers 503 for the app.
func TestAChildThatServesNoCatalogStaysAChild(t *testing.T) {
	old := probeFor
	probeFor = 300 * time.Millisecond
	t.Cleanup(func() { probeFor = old })
	w := startWithServer(t, map[string]string{"fake": "wait"})
	w.waitFor(t, "with fake: serves no catalog")
	c := client.Remote(w.base, "", "")
	if _, err := c.Get(t.Context(), "/"); err != nil {
		t.Errorf("GET /: %v", err)
	}
	_, err := c.Get(t.Context(), "/with/fake/")
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 503 || ce.Code() != "unavailable" {
		t.Errorf("/with/fake/: got %v, want 503 unavailable", err)
	}
}
