package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lifelog/internal/db/dbtest"
)

// TestServeAddressFlag: --addr, in both of its forms, reaches the parse of the loopback address (api.LoopbackAddress,
// whose own test has the table of addresses): LOCALHOST becomes 127.0.0.1, and a refused address stops serve before
// the database opens, as the file is absent.
func TestServeAddressFlag(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.db")
	e, _, _ := testEnv(t)
	for _, at := range []func(addr string) []string{
		func(addr string) []string { return []string{"--addr", addr} },
		func(addr string) []string { return []string{"--addr=" + addr} },
	} {
		o, err := parse(append([]string{"serve"}, at("LOCALHOST:7777")...), noVars)
		if err != nil {
			t.Fatal(err)
		}
		if o, err = prepareServe(e, o); err != nil || o.serve.addr != "127.0.0.1:7777" {
			t.Errorf("serve %v: the address %q, %v; want 127.0.0.1:7777", at("LOCALHOST:7777"), o.serve.addr, err)
		}
		argv := append(at("0.0.0.0:7777"), "serve", "--db", absent)
		if err := runArgs(t.Context(), e, argv); err == nil || !strings.Contains(err.Error(), "serve address") {
			t.Errorf("%v: %v, want the refusal of the address", argv, err)
		}
	}
}

func TestServeAuthorityMount(t *testing.T) {
	// Run the real serve branch against a disposable synthetic database. Read
	// the printed ephemeral listener address rather than guessing a free port.
	p := dbtest.File(t)
	e, _, _ := testEnv(t)
	scheme, address := servedAt(t, serveInProcess(t, e, "serve", "--addr", "localhost:0", "--db", p))
	if strings.HasSuffix(address, ":0") {
		t.Fatalf("ephemeral port not reported: %s", address)
	}
	base := scheme + "://" + address
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	for _, tc := range []struct {
		host string
		want int
	}{{"", 200}, {"arbitrary.invalid:7777", 403}} {
		req, _ := http.NewRequest("GET", base+"/actions", nil)
		if tc.host != "" {
			req.Host = tc.host
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("Host=%q status=%d", tc.host, res.StatusCode)
		}
	}
}

const testToken = "synthetic-token-0123456789abcdefghij"

// TestServeFlagsParse: serve takes --addr, --snapshots, --token-file and --with. The flags of the network that the
// core had (--public, --allow-origin, --tls-cert, --tls-key) are unknown now: a line that names one is refused with the
// flag in the message, never ignored.
func TestServeFlagsParse(t *testing.T) {
	o, err := parse([]string{"serve", "--token-file", "t.txt", "--addr=127.0.0.1:0", "--with", "web"}, noVars)
	if err != nil {
		t.Fatal(err)
	}
	if o.serve.tokenFile != "t.txt" || o.serve.addr != "127.0.0.1:0" || !reflect.DeepEqual(o.serve.with, []string{"web"}) {
		t.Fatalf("%+v", o)
	}
	if _, err := parse([]string{"serve", "--token-file"}, noVars); err == nil {
		t.Fatal("--token-file without a value accepted")
	}
	for _, argv := range [][]string{
		{"serve", "--public"}, {"serve", "--allow-origin", "https://a.example"}, {"serve", "--allow-origin=https://a.example"},
		{"serve", "--tls-cert", "c.pem"}, {"serve", "--tls-key=k.pem"},
	} {
		flag, _, _ := strings.Cut(argv[1], "=")
		if _, err := parse(argv, noVars); err == nil || !strings.Contains(err.Error(), "unknown flag "+flag) {
			t.Errorf("%v: %v, want the refusal unknown flag %s", argv, err, flag)
		}
	}
	o, err = parse([]string{"serve", "--addr", "0.0.0.0:0"}, noVars)
	if err != nil || o.serve.addr != "0.0.0.0:0" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := prepareServe(env{}, o); err == nil || !strings.Contains(err.Error(), "serve address") {
		t.Fatalf("the address 0.0.0.0:0: %v, want the refusal of a non-loopback address", err)
	}
}

// TestServeTokenOfTheFileOrTheEnvironment: tokenOf reads the token of the server before anything opens. The file wins
// over $LIFELOG_TOKEN; one line break is cut; no token anywhere is no token (the check is off); a token that is short,
// has two lines, has a space at an end or sits in an empty file is refused with its reason. An empty value of
// --token-file is refused too, whatever $LIFELOG_TOKEN holds: the owner asked for a token file and named none.
func TestServeTokenOfTheFileOrTheEnvironment(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.db")
	write := func(name, content string) string {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	short := write("short.txt", "too short\n")
	two := write("two.txt", testToken+"\n"+testToken+"\n")
	empty := write("empty.txt", "")
	good := write("token.txt", testToken+"\r\n")
	spaceAfter := write("space-after.txt", testToken+" \n")
	spaceBefore := write("space-before.txt", " "+testToken+"\n")
	tabAfter := write("tab-after.txt", testToken+"\t\r\n")
	for _, tc := range []struct {
		name string
		o    opts
		want string // a part of the refusal
	}{
		{"short token file", opts{serve: serveFlags{tokenFile: short}}, "at least 32"},
		{"two-line token file", opts{serve: serveFlags{tokenFile: two}}, "one line"},
		{"empty token file", opts{serve: serveFlags{tokenFile: empty}}, "at least 32"},
		{"token file with a space after the token", opts{serve: serveFlags{tokenFile: spaceAfter}}, "start or end with a space"},
		{"token file with a space before the token", opts{serve: serveFlags{tokenFile: spaceBefore}}, "start or end with a space"},
		{"token file with a tab after the token", opts{serve: serveFlags{tokenFile: tabAfter}}, "start or end with a space"},
		{"token in the environment with a space after it", opts{token: testToken + " "}, "start or end with a space"},
		{"token in the environment with a tab before it", opts{token: "\t" + testToken}, "start or end with a space"},
		{"missing token file", opts{serve: serveFlags{tokenFile: filepath.Join(t.TempDir(), "none")}}, "token file"},
		{"short token in the environment", opts{token: "short"}, "at least 32"},
		{"non-loopback address with a token", opts{serve: serveFlags{addr: "0.0.0.0:0", tokenFile: good}}, "serve address"},
		{"non-loopback address without a token", opts{serve: serveFlags{addr: "0.0.0.0:0"}}, "serve address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.o
			o.args = []string{"serve"}
			o.db = absent
			if o.serve.addr == "" {
				o.serve.addr = "127.0.0.1:0"
			}
			// Every refusal precedes the database opening: the file is absent.
			e, _, _ := testEnv(t)
			e.o = o
			err := runContext(t.Context(), e)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
	// An empty value of --token-file, in both forms of the flag, with and without a token in the environment: the line
	// is parsed as the program parses it, and it is refused before the file opens (it is absent).
	for _, vars := range []map[string]string{nil, {"LIFELOG_TOKEN": testToken}} {
		for _, line := range [][]string{{"serve", "--token-file="}, {"serve", "--token-file", ""}} {
			t.Run(fmt.Sprintf("%q with %d variables", line, len(vars)), func(t *testing.T) {
				o, err := parse(line, func(name string) string { return vars[name] })
				if err != nil {
					t.Fatal(err)
				}
				o.db = absent
				o.serve.addr = "127.0.0.1:0"
				e, _, _ := testEnv(t)
				e.o = o
				if err := runContext(t.Context(), e); err == nil || !strings.Contains(err.Error(), "--token-file needs a path") {
					t.Fatalf("error=%v want %q", err, "--token-file needs a path")
				}
			})
		}
	}
	for _, tc := range []struct {
		name  string
		o     opts
		token string
	}{
		{"a token file, with its line ending cut", opts{serve: serveFlags{tokenFile: good}}, testToken},
		{"the environment alone", opts{token: testToken}, testToken},
		{"the file wins over the environment", opts{token: strings.Repeat("e", 40), serve: serveFlags{tokenFile: good}}, testToken},
		{"no token anywhere", opts{}, ""},
	} {
		if got, err := tokenOf(tc.o); err != nil || got != tc.token {
			t.Errorf("%s: token %q, %v; want %q", tc.name, got, err, tc.token)
		}
	}
	// $LIFELOG_TOKEN reaches the options through parse, for the client (--url) and for serve alike.
	withToken := func(name string) string { return map[string]string{"LIFELOG_TOKEN": testToken}[name] }
	o, err := parse([]string{"serve"}, withToken)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := tokenOf(o); err != nil || got != testToken {
		t.Errorf("serve with $LIFELOG_TOKEN: %q, %v, want the token of the environment", got, err)
	}
}

// TestServeWithAToken runs serve --token-file in-process on a loopback address. From the outside in: a wrong Host is
// 403 whether the token is sent or not, a request on the right Host without the token is 401 with the challenge, the
// token opens the API, and the guard against a write that a page of another origin sends stays on for a request with the
// token. The startup line says that the token is required.
func TestServeWithAToken(t *testing.T) {
	live := dbtest.File(t)
	tokenFile := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	e, _, _ := testEnv(t) // no LIFELOG_TOKEN in its environment
	startup := serveInProcess(t, e, "--db", live, "serve", "--addr", "127.0.0.1:0", "--token-file", tokenFile)
	if !strings.Contains(startup, " on http://127.0.0.1:") || !strings.Contains(startup, " (token required) (version ") || strings.Contains(startup, "public") {
		t.Fatalf("startup line does not say what it serves: %q", startup)
	}
	scheme, address := servedAt(t, startup)
	if scheme != "http" || !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatalf("server startup: %q", startup)
	}
	base := scheme + "://" + address
	// The client has its own transport. Its idle connections close before serve stops: the shutdown of serve waits
	// 5 s for a connection that the client opened and sent no request on.
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	send := func(method, path, body string, header map[string]string) (int, http.Header) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for k, v := range header {
			if k == "Host" {
				req.Host = v
				continue
			}
			req.Header.Set(k, v)
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode, res.Header
	}
	bearer := map[string]string{"Authorization": "Bearer " + testToken}
	if got, h := send("GET", "/actions", "", nil); got != 401 || h.Get("WWW-Authenticate") != `Bearer realm="lifelog"` {
		t.Fatalf("without the token: %d, WWW-Authenticate %q", got, h.Get("WWW-Authenticate"))
	}
	if got, _ := send("GET", "/actions", "", map[string]string{"Authorization": "Bearer " + strings.Repeat("x", 36)}); got != 401 {
		t.Fatalf("with a wrong token: %d", got)
	}
	if got, _ := send("GET", "/actions", "", map[string]string{"Cookie": "lifelog_session=" + testToken}); got != 401 {
		t.Fatalf("with the token as a cookie: %d", got)
	}
	if got, _ := send("GET", "/actions", "", bearer); got != 200 {
		t.Fatalf("with the token: %d", got)
	}
	// A wrong Host is refused before the token is looked at.
	for _, header := range []map[string]string{{"Host": "arbitrary.invalid:7777"}, {"Host": "arbitrary.invalid:7777", "Authorization": bearer["Authorization"]}} {
		if got, _ := send("GET", "/actions", "", header); got != 403 {
			t.Fatalf("a wrong Host, headers %v: %d, want 403", header, got)
		}
	}
	// A write with the token is done; a write that a page of another origin sends through the browser is refused, also
	// with the token: the guard has no trusted origin.
	if got, _ := send("POST", "/pages", "title=From+a+program", bearer); got != 200 {
		t.Fatalf("a write with the token: %d", got)
	}
	cross := map[string]string{"Authorization": bearer["Authorization"], "Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}
	if got, _ := send("POST", "/pages", "title=From+elsewhere", cross); got != 403 {
		t.Fatalf("a write from another origin: %d, want 403", got)
	}
}

// TestServeWithNoToken: with no token the server has no bearer check and says nothing of one on its startup line. A
// program sends nothing and is served; the Host check stays.
func TestServeWithNoToken(t *testing.T) {
	live := dbtest.File(t)
	e, _, _ := testEnv(t)
	startup := serveInProcess(t, e, "--db", live, "serve", "--addr", "127.0.0.1:0")
	if strings.Contains(startup, "token") || !strings.Contains(startup, " on http://127.0.0.1:") {
		t.Fatalf("startup line: %q", startup)
	}
	_, address := servedAt(t, startup)
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	res, err := hc.Get("http://" + address + "/actions")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("WWW-Authenticate") != "" {
		t.Fatalf("without a token: %d, WWW-Authenticate %q, want 200 and no challenge", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
}

func TestServeShutdownDeadlineCancelsBlockedRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started := make(chan struct{})
	type outcome struct{ read, context error }
	finished := make(chan outcome, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, err := io.Copy(io.Discard, r.Body)
		finished <- outcome{err, r.Context().Err()}
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- serveHTTP(ctx, srv, listener, 20*time.Millisecond) }()
	defer srv.Close()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection, "POST / HTTP/1.1\r\nHost: %s\r\nContent-Length: 5\r\n\r\n", listener.Addr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error = %v; want expired drain deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked request prevented bounded shutdown")
	}
	select {
	case got := <-finished:
		if got.read == nil || !errors.Is(got.context, context.Canceled) {
			t.Fatalf("expired shutdown left the request running: read=%v context=%v", got.read, got.context)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expired shutdown did not release the blocked handler")
	}
}

func TestServeFailureJoinsShutdownWithoutContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	returned := make(chan error, 1)
	go func() { returned <- serveHTTP(context.Background(), &http.Server{}, listener, time.Second) }()
	select {
	case err := <-returned:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("serve error = %v; want closed listener", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server error left shutdown waiting for command cancellation")
	}
}
