package web

// The limits that web puts on what a browser sends, and on what it keeps for the browser: the size of a form, the shape
// of the page to return to (back) and the clock of the receipt of a toast. These are the tests of the browser of the core
// (internal/api/request_limits_test.go, serve_internal_test.go and feedback_internal_test.go), ported to web with their
// cases and their numbers.
//
// The file is in package web, because backOf and the clock of the receipts are not exported. So it makes its own small
// web in front of the real core (newLimitsBrowser). It cannot use the life of world_test.go, which is in package web_test.
//
// Not ported from TestRequestBufferLimits and TestARefusedLargeRequestClosesTheConnection:
//   - The rows of the JSON body. Web takes no JSON (415: TestAWriteThatWebCannotMakeIsRefusedAndTheCoreIsNotAsked).
//   - The rows of a body at the limit. They are writes of 20 MiB to the core. The core holds its own limit.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lifelog/lifelogtest"
)

// limitsToken is the token of the web of these tests: 36 characters of no meaning.
const limitsToken = "synthetic-token-synthetic-token-1234"

// limitsBrowser is a web in front of the real core, with the session of a browser that has logged in, and the number of
// requests that reached the core.
type limitsBrowser struct {
	web    *Browser
	cookie string
	asked  *atomic.Int64
}

func newLimitsBrowser(t *testing.T) *limitsBrowser {
	t.Helper()
	core := lifelogtest.New(t, "2031-05-14")
	asked := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		core.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	web, err := New(Options{CoreURL: server.URL, Token: limitsToken, StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"token": {limitsToken}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	web.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lifelog_session" {
			return &limitsBrowser{web: web, cookie: c.Name + "=" + c.Value, asked: asked}
		}
	}
	t.Fatalf("the login set no session cookie: %d", rec.Code)
	return nil
}

// limitsXs gives n bytes of the letter x.
func limitsXs(n int64) io.Reader { return io.LimitReader(limitsRepeat('x'), n) }

type limitsRepeat byte

func (b limitsRepeat) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}

// limitsCounted counts the bytes that the handler reads from a request body.
type limitsCounted struct {
	r io.Reader
	n int64
}

func (c *limitsCounted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// limitsRows are the two places that read a form of text of 20 MiB at most: the write of a capture, and the preview. The
// head is the first field of the body.
var limitsRows = []struct{ name, path, head string }{
	{"a capture", "/days/2031-05-14/capture", "text="},
	{"the preview", "/render", "body="},
}

// Core test: TestRequestBufferLimits (the rows of the urlencoded body and of /render). A form of 1 MiB more than the limit
// is refused with 413 and a page. Web reads the limit and one byte more, and no other byte. http.MaxBytesReader asks for
// one byte past the limit. It refuses the request when it gets that byte. Web does not ask the core.
func TestAFormOfMoreThanTheLimitIsRefusedAfterTheLimitAndOneByte(t *testing.T) {
	for _, row := range limitsRows {
		t.Run(row.name, func(t *testing.T) {
			lb := newLimitsBrowser(t)
			body := &limitsCounted{r: io.MultiReader(strings.NewReader(row.head), limitsXs(21<<20-int64(len(row.head))))}
			req := httptest.NewRequest(http.MethodPost, row.path, body)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "text/html")
			req.Header.Set("Cookie", lb.cookie)
			rec := httptest.NewRecorder()
			lb.web.ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "</html>") {
				t.Fatalf("%d, want 413 with a page: %.200s", rec.Code, rec.Body.String())
			}
			if body.n != 20<<20+1 {
				t.Errorf("web read %d bytes, want %d (the limit and one byte)", body.n, 20<<20+1)
			}
			if n := lb.asked.Load(); n != 0 {
				t.Errorf("the core was asked %d times, want none", n)
			}
		})
	}
}

// Core test: TestARefusedLargeRequestClosesTheConnection. Through a real server, a form of more than 20 MiB gets 413 with
// "Connection: close". Then the server closes the connection and reads no other request from it. Each request has 1 KiB
// more than the limit. Without the close, net/http reads the rest of so short a body after the handler, and it keeps the
// connection open for the next request.
func TestARefusedLargeFormClosesTheConnection(t *testing.T) {
	for _, row := range limitsRows {
		t.Run(row.name, func(t *testing.T) {
			lb := newLimitsBrowser(t)
			srv := httptest.NewServer(lb.web)
			t.Cleanup(srv.Close)
			conn, err := net.Dial("tcp", srv.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			size := int64(20<<20 + 1<<10)
			head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\n\r\n%s",
				row.path, srv.Listener.Addr(), lb.cookie, size, row.head)
			// The request is written while the answer is read. A write error is not checked: a server may close the
			// connection before it reads the whole body.
			wrote := make(chan struct{})
			go func() {
				defer close(wrote)
				_, _ = io.Copy(conn, io.MultiReader(strings.NewReader(head), limitsXs(size-int64(len(row.head)))))
			}()
			if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			r := bufio.NewReader(conn)
			resp, err := http.ReadResponse(r, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, resp.Body)
			if err := errors.Join(err, resp.Body.Close()); err != nil {
				t.Fatalf("the answer: %v", err)
			}
			if resp.StatusCode != http.StatusRequestEntityTooLarge || !resp.Close {
				t.Errorf("%d, close %v; want 413 and Connection: close", resp.StatusCode, resp.Close)
			}
			if _, err := r.ReadByte(); !errors.Is(err, io.EOF) {
				t.Errorf("after the 413, a read of the connection gives %v, want io.EOF: the server keeps the connection open", err)
			}
			conn.Close()
			<-wrote
			if n := lb.asked.Load(); n != 0 {
				t.Errorf("the core was asked %d times, want none", n)
			}
		})
	}
}

// backOfCases are the values of the field back that a browser form can send, and what backOf keeps of each: the value
// when it is a path on this server, "" otherwise. They are also the seed corpus of FuzzBackOf.
var backOfCases = []struct{ back, want string }{
	{"/days?limit=5&offset=10", "/days?limit=5&offset=10"},
	{"/", "/"},
	{"/days/2026-09-29?x=%2F%2F", "/days/2026-09-29?x=%2F%2F"},
	{"/" + strings.Repeat("a", 2047), "/" + strings.Repeat("a", 2047)}, // 2048 bytes: maxBack
	{"", ""},
	{"/" + strings.Repeat("a", 2048), ""}, // 2049 bytes
	{"//evil.example/", ""},
	{"///evil.example/", ""},
	{"/\\evil.example/", ""},
	{"https://evil.example/", ""},
	{"javascript:alert(1)", ""},
	{"evil", ""},
	{" /days", ""},
	{"/a\r\nSet-Cookie: x=1", ""},
	{"/\t/evil.example/", ""},
	{"/a\x7fb", ""},
	{"/page#part", "/page#part"}, // an id fragment names where the page opens
	{"/metrics#row-1-3", "/metrics#row-1-3"},
	{"/metrics?order=page#row-1-3", "/metrics?order=page#row-1-3"},
	{"/metrics#", ""},
	{"/metrics#a b", ""},
	{"/#<script>", ""},
	{"/metrics#a#b", ""},
	{"/metrics#" + strings.Repeat("a", 64), "/metrics#" + strings.Repeat("a", 64)},
	{"/metrics#" + strings.Repeat("a", 65), ""},
	{"/%zz", ""}, // not a URL: the escape is not hex
}

// Core test: TestBackOf. backOf keeps a path on this server and drops every other value (docs/plans/091-the-browser-for-daily-use.md).
func TestBackOfKeepsAPathOnThisServerAndDropsEveryOtherValue(t *testing.T) {
	if len(backOfCases) != 26 {
		t.Fatalf("%d cases, want the 26 of the core", len(backOfCases))
	}
	for _, tc := range backOfCases {
		if got := backOf(tc.back); got != tc.want {
			t.Errorf("backOf(%q) = %q, want %q", tc.back, got, tc.want)
		}
	}
}

// Core test: FuzzBackOf. Whatever a form sends, backOf gives the fallback "" or the value itself. A value that it keeps
// stays on this server when a browser follows it. The check reads the value as a browser does (the WHATWG URL standard): it
// removes a tab and a newline, and it reads a backslash as a slash. It then resolves the value against a page of this
// server. The host must not change. A kept value has no control character, because it goes into the header Location.
func FuzzBackOf(f *testing.F) {
	for _, tc := range backOfCases {
		f.Add(tc.back)
	}
	base, err := url.Parse("http://lifelog.local/days/2026-09-29")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, back string) {
		got := backOf(back)
		if got == "" {
			return
		}
		if got != back {
			t.Fatalf("backOf(%q) = %q, want the value or \"\"", back, got)
		}
		if strings.ContainsFunc(got, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			t.Fatalf("backOf kept %q, which has a control character", got)
		}
		read := strings.NewReplacer("\t", "", "\n", "", "\r", "", "\\", "/").Replace(got)
		ref, err := url.Parse(read)
		if err != nil {
			t.Fatalf("backOf kept %q, which a browser reads as %q, not a URL: %v", got, read, err)
		}
		if u := base.ResolveReference(ref); u.Scheme != base.Scheme || u.Host != base.Host {
			t.Fatalf("backOf kept %q, and a browser goes to %s", got, u)
		}
	})
}

// Core test: TestTheFeedbackReceiptsKeepTheMonotonicReading. The clock of the receipts is the clock of web. When the program
// gives none, it is time.Now, which has a monotonic reading. A receipt lives ten minutes (feedbackLifetime). That life must
// not change when someone sets the wall clock. A time that keeps the monotonic reading is not equal to its Round(0).
func TestTheClockOfTheReceiptsKeepsTheMonotonicReading(t *testing.T) {
	b, err := New(Options{CoreURL: "http://127.0.0.1:1", Token: limitsToken, StateDir: t.TempDir()}) // the core is not asked
	if err != nil {
		t.Fatal(err)
	}
	if now := b.feedback.now(); now == now.Round(0) {
		t.Errorf("the clock of the receipts gives %v, with no monotonic reading: want time.Now", now)
	}
}
