package apitest

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"testing"
	"time"

	"aminte/client"
	"aminte/siren"
)

// recorder is a test whose Fatal and Fatalf keep the message and stop the goroutine, as the real ones do, but do not
// fail the real test. The other methods are those of the real test.
type recorder struct {
	testing.TB
	fatal string
}

func (r *recorder) Fatal(args ...any) { r.fatal = fmt.Sprint(args...); runtime.Goexit() }
func (r *recorder) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// fatalOf runs f on a recorder in a goroutine of its own and returns the message of its Fatal, "" for none.
func fatalOf(t *testing.T, f func(testing.TB)) string {
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
	return r.fatal
}

// TestMustAndActionFailTheTestAndDoNotPanic: Must gives the entity of a call with no error and fails the test with
// the error of a call; Action gives the named action and fails the test when e has none.
func TestMustAndActionFailTheTestAndDoNotPanic(t *testing.T) {
	e := &siren.Entity{Title: "kit", Actions: []siren.Action{{Name: "capture"}, {Name: "find"}}}
	if got := fatalOf(t, func(tb testing.TB) {
		if Must(tb)(e, nil) != e {
			t.Error("Must did not give the entity of a call with no error")
		}
		if Action(tb, e, "find").Name != "find" {
			t.Error("Action did not give the action find")
		}
	}); got != "" {
		t.Errorf("a call with no error and an action that e has: the test failed with %q", got)
	}
	if got := fatalOf(t, func(tb testing.TB) { Must(tb)(e, errors.New("synthetic refusal")) }); got != "synthetic refusal" {
		t.Errorf("Must with an error: the test failed with %q, want the error", got)
	}
	if got := fatalOf(t, func(tb testing.TB) { Action(tb, e, "tombstone-pages") }); got == "" {
		t.Error("Action of a name that e does not have: the test did not fail")
	}
}

func TestHrefAndStatus(t *testing.T) {
	e := &siren.Entity{Links: []siren.Link{{Rel: []string{"self"}, Href: "/pages/1"}, {Rel: []string{"prev", "first"}, Href: "/pages/0"}}}
	for rel, want := range map[string]string{"self": "/pages/1", "first": "/pages/0", "next": ""} {
		if got := Href(e, rel); got != want {
			t.Errorf("Href(%s) = %q, want %q", rel, got, want)
		}
	}
	for _, tc := range []struct {
		err  error
		want int
	}{
		{nil, 0},
		{&client.Error{Status: http.StatusConflict, Entity: &siren.Entity{}}, http.StatusConflict},
		{fmt.Errorf("save: %w", &client.Error{Status: http.StatusNotFound, Entity: &siren.Entity{}}), http.StatusNotFound},
		{errors.New("connection refused"), -1},
	} {
		if got := Status(tc.err); got != tc.want {
			t.Errorf("Status(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

// TestRemoteReachesTheHandlerOverASocket: the remote client sends the source to the handler over a socket, and its
// server is closed after the test.
func TestRemoteReachesTheHandlerOverASocket(t *testing.T) {
	var sources, peers []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sources, peers = append(sources, r.Header.Get(siren.SourceHeader)), append(peers, r.RemoteAddr)
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		fmt.Fprint(w, `{"title":"kit"}`)
	})
	var remote *client.Client
	t.Run("the test", func(t *testing.T) {
		remote = Remote(t, h, "agent:kit")
		if e, err := remote.Get(t.Context(), "/"); err != nil || e.Title != "kit" {
			t.Errorf("GET /: %+v %v", e, err)
		}
	})
	if fmt.Sprint(sources) != "[agent:kit]" || len(peers) != 1 || peers[0] == "" {
		t.Errorf("the handler saw the sources %q from the peers %q; want agent:kit one time, over a socket", sources, peers)
	}
	if remote == nil {
		return // Remote failed, and the sub-test said why
	}
	if _, err := remote.Get(t.Context(), "/"); Status(err) != -1 {
		t.Errorf("the remote client after the test: %v; want no answer, as the server is closed", err)
	}
}

// TestFreshOnFixesTheClockAtTheStartOfTheDay: the store of FreshOn reads its day from a fixed clock, in UTC.
func TestFreshOnFixesTheClockAtTheStartOfTheDay(t *testing.T) {
	_, _, s := FreshOn(t, "cli", "2031-05-14")
	if got, want := s.Clock(), time.Date(2031, 5, 14, 0, 0, 0, 0, time.UTC); !got.Equal(want) || s.Zone != time.UTC {
		t.Errorf("the clock reads %v in %v, want %v in UTC", got, s.Zone, want)
	}
}
