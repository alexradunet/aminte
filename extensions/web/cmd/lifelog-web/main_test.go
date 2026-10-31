package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/lifelogtest"
	"lifelog/siren"
)

func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(name string) string { return m[name] }
}

// token is the owner's token of the tests: 36 characters of no meaning.
const token = "synthetic-token-synthetic-token-1234"

// good is the environment that runs: the core, the token and a state folder of the test, and what else the test adds.
func good(t *testing.T, pairs ...string) func(string) string {
	t.Helper()
	return env(append([]string{"LIFELOG_URL", "http://127.0.0.1:7777", "LIFELOG_TOKEN", token, "LIFELOG_WEB_STATE", t.TempDir()}, pairs...)...)
}

// freeAddress is a loopback address that nothing listens on now.
func freeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// TestTheEnvironmentIsRefusedWithAReason: no address of the core, no token or a bad one, an address that is not the core,
// an origin that is not an origin, a state folder or a settings file that cannot be used, and an address of the door or
// of the catalog that is not a loopback address are each refused, and the refusal is a configuration error: the owner
// acts first, so the exit status is not the one that makes `serve` start the program again.
func TestTheEnvironmentIsRefusedWithAReason(t *testing.T) {
	file := t.TempDir() + "/a-file"
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	damaged := t.TempDir()
	if err := os.WriteFile(damaged+"/settings.json", []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		getenv func(string) string
		want   string
	}{
		"no LIFELOG_URL":                  {env("LIFELOG_TOKEN", token), "no LIFELOG_URL"},
		"no LIFELOG_TOKEN":                {env("LIFELOG_URL", "http://127.0.0.1:7777"), "no LIFELOG_TOKEN"},
		"a short token":                   {good(t, "LIFELOG_TOKEN", "short"), "at least 32 characters"},
		"a token of two lines":            {good(t, "LIFELOG_TOKEN", token+"\nb"), "one line"},
		"a bad core":                      {good(t, "LIFELOG_URL", "127.0.0.1:7777"), "use http://host:port"},
		"all interfaces":                  {good(t, "LIFELOG_WEB_LISTEN", "0.0.0.0:7780"), "loopback address only"},
		"a name":                          {good(t, "LIFELOG_WEB_LISTEN", "localhost:7780"), "loopback address only"},
		"another host":                    {good(t, "LIFELOG_WEB_LISTEN", "192.168.1.5:7780"), "loopback address only"},
		"no port":                         {good(t, "LIFELOG_WEB_LISTEN", "127.0.0.1"), "not host:port"},
		"a catalog elsewhere":             {good(t, "LIFELOG_LISTEN", "0.0.0.0:9000"), "LIFELOG_LISTEN"},
		"a bad origin":                    {good(t, "LIFELOG_WEB_ORIGINS", "https://lifelog.example/path"), "use scheme://host[:port] exactly"},
		"one bad origin":                  {good(t, "LIFELOG_WEB_ORIGINS", "https://a.example, https://B.example"), "lower case"},
		"a state folder that is a file":   {good(t, "LIFELOG_WEB_STATE", file+"/inside"), "state folder"},
		"a settings file that is damaged": {good(t, "LIFELOG_WEB_STATE", damaged), "settings"},
	} {
		err := run(t.Context(), c.getenv, func(string) { t.Errorf("%s: the door opened", name) })
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

// started runs the program in a goroutine of the test and waits for its door. It returns the address of the door, the
// channel that gets the end of run, and the function that ends its context. The test ends the program before it ends.
func started(t *testing.T, getenv func(string) string) (addr string, done <-chan error, stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	opened := make(chan string, 1)
	ended := make(chan error, 1)
	go func() { ended <- run(ctx, getenv, func(addr string) { opened <- addr }) }()
	select {
	case addr = <-opened:
	case err := <-ended:
		t.Fatalf("run ended before it opened its door: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the door did not open")
	}
	return addr, ended, cancel
}

func waitFor(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("run did not end")
		return nil
	}
}

// loggedIn is a client that has logged in at the door addr.
func loggedIn(t *testing.T, addr string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Jar: jar, Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.PostForm("http://"+addr+"/login", url.Values{"token": {token}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("the login: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	return c
}

// TestRunServesTheBrowserAndDrainsOnTheEndOfItsContext: the program listens on a loopback port, asks for the login, draws a
// page of the real core through its door for a browser that has logged in, and ends with no error when its context ends.
func TestRunServesTheBrowserAndDrainsOnTheEndOfItsContext(t *testing.T) {
	core := lifelogtest.New(t, "2031-05-14")
	addr, done, stop := started(t, good(t, "LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", "127.0.0.1:0"))
	anonymous := &http.Client{Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := anonymous.Get("http://" + addr + "/days/2031-05-14")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusSeeOther {
		t.Errorf("a client with no login got %d", res.StatusCode)
	}
	browser := loggedIn(t, addr)
	res, err = browser.Get("http://" + addr + "/days/2031-05-14")
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || !strings.Contains(string(page), "2031") || res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("GET /days/2031-05-14 through the door: %d %q, %v", res.StatusCode, res.Header.Get("Content-Type"), err)
	}
	// The drain waits for a connection that has sent no request for five seconds: the test closes its own.
	anonymous.CloseIdleConnections()
	browser.CloseIdleConnections()
	stop()
	if err := waitFor(t, done); err != nil {
		t.Errorf("run ended with %v, want nil", err)
	}
}

// TestRunFailsWhenTheDoorIsTaken: a second program on the same address fails with an error that is not a configuration
// error, so `serve` starts it again.
func TestRunFailsWhenTheDoorIsTaken(t *testing.T) {
	core := lifelogtest.New(t, "2031-05-14")
	addr, done, stop := started(t, good(t, "LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", "127.0.0.1:0"))
	err := run(t.Context(), good(t, "LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", addr), func(string) { t.Error("a second door opened") })
	var config *configError
	if err == nil || errors.As(err, &config) {
		t.Errorf("a taken door: %v, want an error that is not a configuration error", err)
	}
	stop()
	waitFor(t, done)
}

// TestRunServesTheCatalogBesideTheDoor: with LIFELOG_LISTEN, the program serves its catalog on that address as well, and
// both end with the context. The catalog offers the action settings, owner-only. Without LIFELOG_LISTEN there is none.
func TestRunServesTheCatalogBesideTheDoor(t *testing.T) {
	core := lifelogtest.New(t, "2031-05-14")
	listen := freeAddress(t)
	t.Setenv("LIFELOG_LISTEN", listen) // lifelog/app reads the variable itself
	addr, done, stop := started(t, good(t, "LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", "127.0.0.1:0", "LIFELOG_LISTEN", listen))
	var catalog *siren.Entity
	for range 100 { // the second goroutine opens its socket a moment after the door
		var err error
		if catalog, err = client.Remote("http://"+listen, "ui", "").Get(t.Context(), "/actions"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if catalog == nil || len(catalog.Actions) != 1 || catalog.Actions[0].Name != "settings" || !catalog.Actions[0].Owner {
		t.Fatalf("the catalog: %+v", catalog)
	}
	stop()
	if err := waitFor(t, done); err != nil {
		t.Errorf("run ended with %v, want nil", err)
	}
	for _, a := range []string{addr, listen} {
		if c, err := net.Dial("tcp", a); err == nil {
			c.Close()
			t.Errorf("%s still listens after the end", a)
		}
	}
}

// TestAFailureOfTheCatalogEndsTheDoor: a catalog that cannot listen (its address is taken) ends the program with an
// error, and the door closes. One owner ends both, so nothing is left half alive.
func TestAFailureOfTheCatalogEndsTheDoor(t *testing.T) {
	core := lifelogtest.New(t, "2031-05-14")
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { taken.Close() })
	listen := taken.Addr().String()
	t.Setenv("LIFELOG_LISTEN", listen)
	addr, done, _ := started(t, good(t, "LIFELOG_URL", core.URL, "LIFELOG_WEB_LISTEN", "127.0.0.1:0", "LIFELOG_LISTEN", listen))
	err = waitFor(t, done)
	var config *configError
	if err == nil || errors.As(err, &config) || !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("a catalog that cannot listen: %v, want an error that is not a configuration error", err)
	}
	if c, err := net.Dial("tcp", addr); err == nil {
		c.Close()
		t.Error("the door still listens after the catalog failed")
	}
}
