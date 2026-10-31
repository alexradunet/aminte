package web

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A body that stops is cut. The tests shorten the limits and talk to a real server on a socket, because a read deadline is
// a deadline of the connection: a recorder has none.

// stalled sends the head of a POST that promises more bytes than it sends, then sends the rest of what it has and stops. It
// returns the first line of the answer and how long the answer took.
func stalled(t *testing.T, addr, path, contentType, cookie, sent string) (status string, took time.Duration) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: example.com\r\nCookie: %s\r\nContent-Type: %s\r\nContent-Length: 100000\r\n\r\n%s", path, cookie, contentType, sent)
	start := time.Now()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("%s: no answer in 10 seconds: %v", path, err)
	}
	return strings.TrimSpace(line), time.Since(start)
}

func TestABodyThatStopsIsCut(t *testing.T) {
	prevUpload, prevSmall := uploadStall, smallWithin
	uploadStall, smallWithin = 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { uploadStall, smallWithin = prevUpload, prevSmall })

	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // an upload waits for its body; a read of the catalog is not asked
	}))
	t.Cleanup(core.Close)
	b, err := New(Options{CoreURL: core.URL, Token: "synthetic-token-synthetic-token-1234", StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	door := httptest.NewServer(b)
	t.Cleanup(door.Close)
	addr := door.Listener.Addr().String()

	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=synthetic-token-synthetic-token-1234"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.ServeHTTP(login, req)
	cookie := login.Result().Cookies()[0].Name + "=" + login.Result().Cookies()[0].Value

	// A file that stops after its first bytes: the upload is cut, and the browser gets an error, not a redirect.
	status, took := stalled(t, addr, "/files", "multipart/form-data; boundary=b", cookie, "--b\r\n")
	if took > 5*time.Second || !strings.Contains(status, " 400 ") {
		t.Errorf("an upload that stopped: %q after %v, want 400 within the stall time", status, took)
	}
	// The form of the login that stops: refused as a wrong token, within the limit and not after two minutes.
	status, took = stalled(t, addr, "/login", "application/x-www-form-urlencoded", "", "token=")
	if took > 5*time.Second || !strings.Contains(status, " 303 ") {
		t.Errorf("a login form that stopped: %q after %v, want 303 to the form within the limit", status, took)
	}
}
