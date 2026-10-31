package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"lifelog/lifelogtest"
)

func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(name string) string { return m[name] }
}

// TestTheEnvironmentIsRefusedWithAReason: no address of the core, an address that is not the core, and an address of the
// door that is not a loopback address are each refused, and the refusal is a configuration error: the owner acts first,
// so the exit status is not the one that makes `serve` start the program again.
func TestTheEnvironmentIsRefusedWithAReason(t *testing.T) {
	for name, c := range map[string]struct {
		getenv func(string) string
		want   string
	}{
		"no LIFELOG_URL":       {env(), "no LIFELOG_URL"},
		"a bad core":           {env("LIFELOG_URL", "127.0.0.1:7777"), "use http://host:port"},
		"all interfaces":       {env("LIFELOG_URL", "http://127.0.0.1:7777", "LIFELOG_WEB_LISTEN", "0.0.0.0:7780"), "loopback address only"},
		"a name":               {env("LIFELOG_URL", "http://127.0.0.1:7777", "LIFELOG_WEB_LISTEN", "localhost:7780"), "loopback address only"},
		"another host":         {env("LIFELOG_URL", "http://127.0.0.1:7777", "LIFELOG_WEB_LISTEN", "192.168.1.5:7780"), "loopback address only"},
		"no port":              {env("LIFELOG_URL", "http://127.0.0.1:7777", "LIFELOG_WEB_LISTEN", "127.0.0.1"), "not host:port"},
		"a token of two lines": {env("LIFELOG_URL", "http://127.0.0.1:7777", "LIFELOG_TOKEN", "a\nb"), "one line"},
	} {
		err := run(t.Context(), c.getenv, func(string) { t.Error("the door opened") })
		var config *configError
		if !errors.As(err, &config) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a configuration error with %q", name, err, c.want)
		}
	}
}

func TestTheDefaultDoorIsOnTheLoopback(t *testing.T) {
	if err := checkLoopback(defaultListen); err != nil {
		t.Errorf("the default address %s: %v", defaultListen, err)
	}
	for _, ok := range []string{"127.0.0.1:0", "[::1]:7780", "127.9.9.9:80"} {
		if err := checkLoopback(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

// TestRunServesTheBrowserAndDrainsOnTheEndOfItsContext: the program listens on a loopback port, draws a page of the real
// core through its door, and ends with no error when its context ends.
func TestRunServesTheBrowserAndDrainsOnTheEndOfItsContext(t *testing.T) {
	core := lifelogtest.New(t, "2031-05-14")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	opened := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, env("LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", "127.0.0.1:0"), func(addr string) { opened <- addr })
	}()
	var addr string
	select {
	case addr = <-opened:
	case err := <-done:
		t.Fatalf("run ended before it opened its door: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the door did not open")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/days/2031-05-14", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || !strings.Contains(string(page), "2031") || res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("GET /days/2031-05-14 through the door: %d %q, %v", res.StatusCode, res.Header.Get("Content-Type"), err)
	}
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run ended with %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not end after the end of its context")
	}
}

// TestRunFailsWhenTheDoorIsTaken: a second program on the same address fails with an error that is not a configuration
// error, so `serve` starts it again.
func TestRunFailsWhenTheDoorIsTaken(t *testing.T) {
	core := lifelogtest.New(t, "2031-05-14")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	opened := make(chan string, 1)
	first := make(chan error, 1)
	go func() {
		first <- run(ctx, env("LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", "127.0.0.1:0"), func(addr string) { opened <- addr })
	}()
	addr := <-opened
	err := run(ctx, env("LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", addr), func(string) { t.Error("a second door opened") })
	var config *configError
	if err == nil || errors.As(err, &config) {
		t.Errorf("a taken door: %v, want an error that is not a configuration error", err)
	}
	stop()
	<-first
}
