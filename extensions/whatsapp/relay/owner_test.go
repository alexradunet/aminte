package relay

import (
	"bytes"
	"context"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aminte/client"
)

func waitOwnerOn(t *testing.T, api *fakeAPI, tick <-chan time.Time) (chan string, *bytes.Buffer, context.CancelFunc) {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	got := make(chan string, 1)
	go func() {
		n, err := WaitOwner(ctx, client.Remote(srv.URL, "", token), log.New(&logs, "", 0), tick)
		if err != nil {
			n = "error: " + err.Error()
		}
		got <- n
	}()
	return got, &logs, cancel
}

func TestWaitOwnerReturnsTheNumberThatIsSetAtOnce(t *testing.T) {
	api := &fakeAPI{}
	api.setOwnerNumber("40712345678")
	got, _, _ := waitOwnerOn(t, api, make(chan time.Time))
	select {
	case n := <-got:
		if n != "40712345678" {
			t.Errorf("number %q, want 40712345678", n)
		}
	case <-time.After(wait):
		t.Fatal("WaitOwner did not return")
	}
}

func TestWaitOwnerLogsOnceWhenNoNumberIsSetAndReadsAgainAtEachTickUntilItIsSet(t *testing.T) {
	api := &fakeAPI{}
	tick := make(chan time.Time)
	got, logs, _ := waitOwnerOn(t, api, tick)
	for range 3 { // each tick is taken only after the earlier read is done
		select {
		case tick <- fixedNow:
		case <-time.After(wait):
			t.Fatal("WaitOwner did not take the tick")
		}
	}
	select {
	case n := <-got:
		t.Fatalf("returned %q with no number set", n)
	default:
	}
	api.setOwnerNumber("40712345678")
	select { // the read after the third tick may see the number already: then it has returned
	case tick <- fixedNow:
	case <-time.After(wait):
	case n := <-got:
		got <- n
	}
	select {
	case n := <-got:
		if n != "40712345678" {
			t.Errorf("number %q, want 40712345678", n)
		}
	case <-time.After(wait):
		t.Fatal("WaitOwner did not return after the number was set")
	}
	if c := strings.Count(logs.String(), "no owner number: set it in Settings (owner_number)"); c != 1 {
		t.Errorf("%d log lines about the missing number, want 1; log %q", c, logs.String())
	}
	reads := 0
	for _, q := range api.requests() {
		if q.Path == "/settings" {
			reads++
			if q.Source != "" {
				t.Errorf("settings read with source %q, want none", q.Source)
			}
		}
	}
	if reads < 4 {
		t.Errorf("%d settings reads, want at least 4", reads)
	}
}

func TestWaitOwnerEndsWithTheContext(t *testing.T) {
	got, _, cancel := waitOwnerOn(t, &fakeAPI{}, make(chan time.Time))
	cancel()
	select {
	case n := <-got:
		if !strings.Contains(n, "context canceled") {
			t.Errorf("result %q, want the error of the context", n)
		}
	case <-time.After(wait):
		t.Fatal("WaitOwner did not end with the context")
	}
}
