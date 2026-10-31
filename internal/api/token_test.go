package api_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
	"lifelog/siren"
)

const testToken = "synthetic-token-0123456789abcdefghij" // 36 characters

// listenAddr is the loopback address that the guarded handler of a test stands for: the one NetworkAuthority knows.
var listenAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777}

// tokenHandler is the handler of lifelog serve with a token, from the outside in: NetworkAuthority, RequireToken, the
// API on a fresh file. The store is for a probe of the file.
func tokenHandler(t *testing.T) (http.Handler, *core.Store) {
	t.Helper()
	return tokenHandlerAt(t, listenAddr)
}

// tokenHandlerAt is tokenHandler for the listener at addr.
func tokenHandlerAt(t *testing.T, addr net.Addr) (http.Handler, *core.Store) {
	t.Helper()
	s := &core.Store{DB: dbtest.New(t)}
	bearer, err := api.RequireToken(testToken, api.New(s))
	if err != nil {
		t.Fatal(err)
	}
	h, err := api.NetworkAuthority(addr, bearer)
	if err != nil {
		t.Fatal(err)
	}
	return h, s
}

// request sends one request to h. Its Host is the one of a client on another device unless the headers name another.
func request(h http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "192.0.2.10:7777"
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestRequireTokenAcceptsTheBearerAlone: the token in the header Authorization is the one credential. Any other
// value, any cookie and any route without it get 401 with the code token_required and the challenge. A write without
// the token writes nothing.
func TestRequireTokenAcceptsTheBearerAlone(t *testing.T) {
	h, s := tokenHandler(t)
	local := map[string]string{"Host": "127.0.0.1:7777"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{"Host": "127.0.0.1:7777"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	bearer := with(map[string]string{"Authorization": "Bearer " + testToken})
	for _, tc := range []struct {
		name    string
		target  string
		headers map[string]string
		want    int
	}{
		{"no credential", "/days/2031-01-01", local, 401},
		{"wrong token", "/days/2031-01-01", with(map[string]string{"Authorization": "Bearer " + strings.Repeat("x", 36)}), 401},
		{"last byte differs", "/days/2031-01-01", with(map[string]string{"Authorization": "Bearer " + testToken[:35] + "k"}), 401},
		{"longer", "/days/2031-01-01", with(map[string]string{"Authorization": "Bearer " + testToken + "x"}), 401},
		{"shorter", "/days/2031-01-01", with(map[string]string{"Authorization": "Bearer " + testToken[:35]}), 401},
		{"empty credential", "/days/2031-01-01", with(map[string]string{"Authorization": "Bearer "}), 401},
		{"basic scheme", "/days/2031-01-01", with(map[string]string{"Authorization": "Basic " + testToken}), 401},
		{"the token without a scheme", "/days/2031-01-01", with(map[string]string{"Authorization": testToken}), 401},
		{"the token as the old cookie", "/days/2031-01-01", with(map[string]string{"Cookie": "lifelog_token=" + testToken}), 401},
		{"the token as a session cookie", "/days/2031-01-01", with(map[string]string{"Cookie": "lifelog_session=" + testToken}), 401},
		{"a route of an old login page", "/login", local, 401},
		{"a file that an app would install", "/assets/icon.svg", local, 401},
		{"the root", "/", local, 401},
		{"bearer", "/days/2031-01-01", bearer, 200},
		{"bearer case", "/days/2031-01-01", with(map[string]string{"Authorization": "bearer " + testToken}), 200},
		{"bearer with spaces around the token", "/days/2031-01-01", with(map[string]string{"Authorization": "Bearer  " + testToken + " "}), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request(h, "GET", tc.target, "", tc.headers)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want %d: %.200s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want != 401 {
				return
			}
			var body siren.Entity
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("the 401 is no entity: %v\n%.200s", err, rec.Body.String())
			}
			props, _ := body.Properties.(map[string]any)
			if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="lifelog"` {
				t.Errorf("WWW-Authenticate=%q", got)
			}
			if props["code"] != siren.CodeTokenRequired || props["message"] != "token required" || rec.Header().Get("Content-Type") != "application/vnd.siren+json" {
				t.Errorf("the 401 answer: code %v, message %v, type %q", props["code"], props["message"], rec.Header().Get("Content-Type"))
			}
			if strings.Contains(rec.Body.String(), "2031-01-01") {
				t.Errorf("the 401 answer names the resource:\n%s", rec.Body.String())
			}
		})
	}
	// A write with the token persists; without it nothing is written.
	if rec := request(h, "POST", "/days/2031-01-01/capture", "text=denied", local); rec.Code != 401 {
		t.Fatalf("unauthenticated write: %d", rec.Code)
	}
	if rec := request(h, "POST", "/days/2031-01-01/capture", "text=allowed", bearer); rec.Code != 200 {
		t.Fatalf("authenticated write: %d %.200s", rec.Code, rec.Body.String())
	}
	id, err := s.PageID(t.Context(), "2031-01-01")
	if err != nil || id == 0 {
		t.Fatalf("day page after the writes: id=%d err=%v", id, err)
	}
	page, err := s.PageByID(t.Context(), id)
	if err != nil || !strings.Contains(page.Body, "allowed") || strings.Contains(page.Body, "denied") {
		t.Fatalf("body=%q err=%v", page.Body, err)
	}
}

// TestAWrongHostIsRefusedBeforeTheToken: NetworkAuthority is the outer guard of lifelog serve, so a page that an
// attacker serves under another name (DNS rebinding) is refused with 403 whether it sends the token or not, and a
// request to the right Host without the token is 401. A target in absolute form is refused with the token too.
func TestAWrongHostIsRefusedBeforeTheToken(t *testing.T) {
	h, _ := tokenHandler(t)
	bearer := "Bearer " + testToken
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"a foreign Host with no token", map[string]string{"Host": "attacker.example:7777"}, 403},
		{"a foreign Host with the token", map[string]string{"Host": "attacker.example:7777", "Authorization": bearer}, 403},
		{"another port with the token", map[string]string{"Host": "127.0.0.1:8888", "Authorization": bearer}, 403},
		{"a device address with the token", map[string]string{"Host": "192.0.2.10:7777", "Authorization": bearer}, 403},
		{"the right Host with no token", map[string]string{"Host": "127.0.0.1:7777"}, 401},
		{"localhost with the token", map[string]string{"Host": "localhost:7777", "Authorization": bearer}, 200},
		{"the right Host with the token", map[string]string{"Host": "127.0.0.1:7777", "Authorization": bearer}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := request(h, "GET", "/", "", tc.headers); rec.Code != tc.want {
				t.Fatalf("status=%d want %d: %.200s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
	req := httptest.NewRequest("GET", "http://attacker.example/", nil)
	req.URL.Host = "attacker.example"
	req.Header.Set("Authorization", bearer)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("absolute-form target: %d", rec.Code)
	}
}

// TestRequireTokenRefusesATokenThatCheckTokenRefuses: the constructor refuses the token before the server listens. A
// token with a space or a tab at an end is one of them: the compare cuts the white space of the credential, and a
// header value has none at its ends, so no client could send that token.
func TestRequireTokenRefusesATokenThatCheckTokenRefuses(t *testing.T) {
	next := http.NotFoundHandler()
	for _, token := range []string{"", "short", strings.Repeat("x", 31), testToken + "\n", testToken + "\r\n", "line one line two\nline three abcdefghijklmnop",
		testToken + " ", " " + testToken, testToken + "\t", "\t" + testToken} {
		if h, err := api.RequireToken(token, next); err == nil || h != nil {
			t.Errorf("token %q accepted", token)
		}
		if err := api.CheckToken(token); err == nil {
			t.Errorf("CheckToken accepted %q", token)
		}
	}
	for _, token := range []string{strings.Repeat("x", 32), testToken} {
		if _, err := api.RequireToken(token, next); err != nil {
			t.Errorf("token %q refused: %v", token, err)
		}
	}
}

// TestARemoteClientNeedsTheToken: a real socket. A client without the token is refused, and a client with it reads,
// reads the catalog and writes as the writer it names.
func TestARemoteClientNeedsTheToken(t *testing.T) {
	srv := httptest.NewUnstartedServer(nil)
	h, s := tokenHandlerAt(t, srv.Listener.Addr())
	srv.Config.Handler = h
	srv.Start()
	t.Cleanup(srv.Close)
	c := client.Remote(srv.URL, "app:phone", "")
	if _, err := c.Get(t.Context(), "/"); err == nil {
		t.Fatal("a remote client without the token was served")
	}
	c = client.Remote(srv.URL, "app:phone", testToken)
	if _, err := c.Get(t.Context(), "/"); err != nil {
		t.Fatal(err)
	}
	catalog, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var capture siren.Action
	for _, a := range catalog {
		if a.Name == "capture" {
			capture = a
		}
	}
	if _, err := c.Do(t.Context(), capture, map[string]string{"day": "2031-01-03", "text": "from the phone"}); err != nil {
		t.Fatal(err)
	}
	id, err := s.PageID(t.Context(), "2031-01-03")
	if err != nil || id == 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	if got := scalar(t, s, fmt.Sprintf("SELECT source FROM entities WHERE id = %d", id)); got != "app:phone" {
		t.Fatalf("source=%q", got)
	}
}
