package api_test

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"aminte/internal/api"
	"aminte/internal/api/apitest"
)

// TestLoopbackAddress: the address of a local serve is a numeric loopback IP or localhost, which binds to 127.0.0.1, and
// a numeric port. Any other address is refused with "serve address": no DNS, no service name, no zone, brackets only
// around an IPv6 address.
func TestLoopbackAddress(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"127.0.0.1:7777", "127.0.0.1:7777"}, {"127.2.3.4:0", "127.2.3.4:0"}, {"localhost:0", "127.0.0.1:0"}, {"LOCALHOST:7777", "127.0.0.1:7777"}, {"[::1]:0", "[::1]:0"},
		{"[0:0::1]:007", "[0:0::1]:7"}, // a long IPv6 form and a port with zeros: the host stays as it is typed
	} {
		if got, err := api.LoopbackAddress(tc.input); err != nil || got != tc.want {
			t.Errorf("LoopbackAddress(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, addr := range []string{"", ":7777", "0.0.0.0:7777", "[::]:7777", "192.0.2.1:7777", "custom.invalid:7777", "localhoſt:7777", "localhost:http", "localhost:65536", "localhost:-1", "localhost:+1", "localhost.", "[::1%zone]:7777", "[localhost]:7777", "127.0.0.1", "localhost:", "[127.0.0.1]:7777", "::1:7777"} {
		if got, err := api.LoopbackAddress(addr); err == nil || !strings.Contains(err.Error(), "serve address") {
			t.Errorf("LoopbackAddress(%q) = %q, %v; want the refusal", addr, got, err)
		}
	}
}

func TestHTTPAuthority(t *testing.T) {
	for _, bind := range []string{"127.0.0.1:0", "[::1]:0"} {
		t.Run(bind, func(t *testing.T) {
			ln, err := net.Listen("tcp", bind)
			if err != nil {
				if bind != "127.0.0.1:0" {
					t.Skipf("no IPv6 loopback on this host: %v", err)
				}
				t.Fatal(err)
			}
			var calls atomic.Int32
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) })
			h, err := api.NetworkAuthority(ln.Addr(), inner)
			if err != nil {
				ln.Close()
				t.Fatal(err)
			}
			srv := httptest.NewUnstartedServer(h)
			srv.Listener.Close()
			srv.Listener = ln
			srv.Start()
			defer srv.Close()
			_, port, _ := net.SplitHostPort(ln.Addr().String())
			allowed := []string{"127.0.0.1:" + port, "127.2.3.4:" + port, "localhost:" + port, "LOCALHOST:" + port, "[::1]:" + port, "[0:0:0:0:0:0:0:1]:" + port, "[::ffff:127.0.0.1]:" + port}
			denied := []string{"arbitrary.invalid:" + port, "aminte.local:" + port, "localhost:1", "[::1]:1", "127.0.0.1:1", "localhost", "localhost:", "localhost:http", "localhost:+" + port, "localhost:65536", "localhost.:" + port, "0.0.0.0:" + port, "192.0.2.1:" + port, "[::]:" + port, "[localhost]:" + port, "[127.0.0.1]:" + port, "::1:" + port, "user@localhost:" + port, "localhost:" + port + ":2"}
			tr := &http.Transport{Proxy: nil}
			defer tr.CloseIdleConnections()
			hc := &http.Client{Transport: tr}
			for _, group := range []struct {
				hosts []string
				allow bool
			}{{allowed, true}, {denied, false}} {
				for _, host := range group.hosts {
					for _, route := range []struct{ method, path string }{{"GET", "/"}, {"POST", "/pages"}, {"GET", "/pages/1/preview"}, {"GET", "/previews?title=Probe"}} {
						t.Run(host+route.method+route.path, func(t *testing.T) {
							before := calls.Load()
							req, _ := http.NewRequest(route.method, srv.URL+route.path, nil)
							req.Host = host
							req.Header.Set("Forwarded", "host=localhost:"+port)
							req.Header.Set("X-Forwarded-Host", "localhost:"+port)
							res, err := hc.Do(req)
							if err != nil {
								t.Fatal(err)
							}
							res.Body.Close()
							want, delta := 403, int32(0)
							if group.allow {
								want, delta = 204, 1
							}
							if res.StatusCode != want || calls.Load() != before+delta {
								t.Fatalf("status=%d calls delta=%d want %d/%d", res.StatusCode, calls.Load()-before, want, delta)
							}
						})
					}
				}
			}
			// Absolute-form request targets override Host during Go's HTTP parsing.
			// Refuse them rather than silently accepting a conflicting wire Host.
			for _, target := range []string{"http://arbitrary.invalid:" + port + "/", "http://localhost:" + port + "/"} {
				for _, host := range []string{"localhost:" + port, "arbitrary.invalid:" + port} {
					before := calls.Load()
					conn, err := net.Dial("tcp", ln.Addr().String())
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target, host)
					res, err := http.ReadResponse(bufio.NewReader(conn), nil)
					if err != nil {
						conn.Close()
						t.Fatal(err)
					}
					res.Body.Close()
					conn.Close()
					if res.StatusCode != 403 || calls.Load() != before {
						t.Fatalf("absolute target=%s Host=%s status=%d calls changed=%v", target, host, res.StatusCode, calls.Load() != before)
					}
				}
			}
			// The Go client strips IPv6 zones from Host; use raw HTTP to
			// verify the network guard sees and rejects a supplied zone.
			before := calls.Load()
			conn, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: [::1%%zone]:%s\r\nConnection: close\r\n\r\n", port)
			res, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				conn.Close()
				t.Fatal(err)
			}
			res.Body.Close()
			conn.Close()
			if res.StatusCode != 403 || calls.Load() != before {
				t.Fatalf("zoned wire Host: status=%d", res.StatusCode)
			}
			// Malformed Hosts that a client/server parser may reject before middleware.
			for _, host := range []string{"[::1%zone]:" + port, "", " localhost:" + port, "localhoſt:" + port, "localhost:" + port + "/", "localhost:" + port + "?x", "localhost:" + port + "#x"} {
				req := httptest.NewRequest("GET", "/", nil)
				req.Host = host
				rec := httptest.NewRecorder()
				before := calls.Load()
				h.ServeHTTP(rec, req)
				if rec.Code != 403 || calls.Load() != before {
					t.Fatalf("malformed Host %q status=%d", host, rec.Code)
				}
			}
		})
	}
}

func TestHTTPAuthorityOriginsAndInProcess(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	if _, err := c.Get(t.Context(), "/actions"); err != nil {
		t.Fatal(err)
	}
	guarded, err := api.NetworkAuthority(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 7777}, h)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		host, origin, site string
		want               int
	}{
		{"localhost:7777", "", "", 200}, {"localhost:7777", "http://localhost:7777", "", 200}, {"localhost:7777", "http://localhost:7777", "same-origin", 200},
		{"localhost:7777", "http://arbitrary.invalid:7777", "", 403}, {"localhost:7777", "http://localhost:7777", "cross-site", 403},
		{"arbitrary.invalid:7777", "http://arbitrary.invalid:7777", "same-origin", 403}, {"localhost:1", "", "", 403},
	} {
		req := httptest.NewRequest("POST", "/pages", strings.NewReader(fmt.Sprintf("title=Origin+probe+%d", i)))
		req.Host = tc.host
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", tc.site)
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%+v status=%d body=%s", tc, rec.Code, rec.Body.String())
		}
	}
}

func TestHTTPAuthorityDefaultPort(t *testing.T) {
	// Browsers canonicalize an explicit :80 away. An omitted port is HTTP's
	// default, not permission to use any port the server happens to bind.
	for _, port := range []int{80, 7777} {
		calls := 0
		h, err := api.NetworkAuthority(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusNoContent)
		}))
		if err != nil {
			t.Fatal(err)
		}
		for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "localhost:", "::1", "localhoſt", "localhost.", "arbitrary.invalid"} {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = host
			w := httptest.NewRecorder()
			before := calls
			h.ServeHTTP(w, r)
			want, delta := http.StatusForbidden, 0
			if port == 80 && (host == "localhost" || host == "LOCALHOST" || host == "127.0.0.1" || host == "[::1]" || host == "[::ffff:127.0.0.1]") {
				want, delta = http.StatusNoContent, 1
			}
			if w.Code != want || calls != before+delta {
				t.Errorf("listener port=%d Host=%q: status=%d calls delta=%d want %d/%d", port, host, w.Code, calls-before, want, delta)
			}
		}
	}
}

func TestHTTPAuthorityListenerValidation(t *testing.T) {
	for _, addr := range []*net.TCPAddr{
		{IP: net.ParseIP("0.0.0.0"), Port: 7777}, {IP: net.ParseIP("192.0.2.1"), Port: 7777},
		{IP: net.ParseIP("::"), Port: 7777}, {IP: net.ParseIP("127.0.0.1"), Port: 0},
	} {
		if _, err := api.NetworkAuthority(addr, http.NotFoundHandler()); err == nil {
			t.Fatalf("listener %s accepted", addr)
		}
	}
}
