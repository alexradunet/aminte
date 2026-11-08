package client_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aminte/client"
	"aminte/internal/api"
	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/siren"
)

// These tests are in package client_test because internal/api imports client (a connected app, plan 154): a test
// inside package client cannot import internal/api.

func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("completion timed out")
	}
}

func TestClientCancellationQueuedWrite(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "remote"}[remote], func(t *testing.T) { testQueuedWrite(t, remote) })
	}
}

func testQueuedWrite(t *testing.T, remote bool) {
	d := dbtest.New(t)
	held, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- d.Write(context.Background(), func(*sql.Tx) error { close(held); <-release; return nil })
	}()
	wait(t, held)
	released := false

	entered := make(chan struct{})
	h := api.NewWith(&core.Store{DB: d}, nil)
	completed := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		defer close(completed)
		h.ServeHTTP(w, r)
	})
	c := client.InProcess(handler, "cli")
	if remote {
		srv := httptest.NewServer(handler)
		defer srv.Close()
		c = client.Remote(srv.URL, "cli", "")
	}
	defer func() {
		if released {
			return
		}
		close(release)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Do(ctx, siren.Action{Method: "POST", Href: "/days/{day}/capture", Fields: []siren.Field{{Name: "day", In: "path"}, {Name: "text"}}}, map[string]string{"day": "2026-10-05", "text": "synthetic queued"})
		result <- err
	}()
	wait(t, entered)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("queued write hung")
	}
	wait(t, completed)
	close(release)
	released = true
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE name_key = ?", "2026-10-05").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("canceled write committed")
	}
}
