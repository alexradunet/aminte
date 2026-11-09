// Package apitest is the kit of the tests of the surfaces of the API: a fresh handler with its clients, and the small
// reads of a Siren entity that these tests make. The tests inside package api cannot import it (an import cycle), nor
// can the tests inside package client; they use dbtest.
package apitest

import (
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

// Fresh makes a fresh life.db (dbtest.New), the handler of the program over it, and an in-process client with the
// source. The store is for a probe of the file: a test reads a row straight from it, never as the behaviour under test.
func Fresh(t testing.TB, source string) (*client.Client, http.Handler, *core.Store) {
	t.Helper()
	return over(&core.Store{DB: dbtest.New(t)}, source)
}

// FreshOn is Fresh with the store's clock fixed at the start of the day, in UTC: a test of today, or of the words for
// a day, uses fixed dates.
func FreshOn(t testing.TB, source, day string) (*client.Client, http.Handler, *core.Store) {
	t.Helper()
	start, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatal(err)
	}
	return over(&core.Store{DB: dbtest.New(t), Clock: func() time.Time { return start }, Zone: time.UTC}, source)
}

func over(s *core.Store, source string) (*client.Client, http.Handler, *core.Store) {
	h := api.NewWith(s, nil)
	return client.InProcess(h, source), h, s
}

// Remote returns a client of h with the source over a socket: an httptest server that the cleanup of the test closes.
// The client in process is client.InProcess(h, source).
func Remote(t testing.TB, h http.Handler, source string) *client.Client {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return client.Remote(server.URL, source, "")
}

// Must returns a function that gives the entity of a call of the client, or fails the test with the error of the
// call: apitest.Must(t)(c.Get(ctx, "/")). Go gives the two values of a call only to a function with exactly these two
// parameters, so the test comes first, in its own call.
func Must(t testing.TB) func(*siren.Entity, error) *siren.Entity {
	return func(e *siren.Entity, err error) *siren.Entity {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
}

// Action returns the action of e with the name, or fails the test.
func Action(t testing.TB, e *siren.Entity, name string) siren.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action %s in %v", name, e.Actions)
	return siren.Action{}
}

// Href returns the href of the link of e with the rel, or "" when e has no such link.
func Href(e *siren.Entity, rel string) string {
	if l := e.LinkByRel(rel); l != nil {
		return l.Href
	}
	return ""
}

// Status returns the HTTP status of an error of the client: 0 for no error, and -1 for an error with no status (the
// request did not get an answer).
func Status(err error) int {
	if err == nil {
		return 0
	}
	var ce *client.Error
	if errors.As(err, &ce) {
		return ce.Status
	}
	return -1
}
