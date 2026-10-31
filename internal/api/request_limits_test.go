package api_test

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

type repeatedByte byte

func (b repeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
func generated(n int64) io.Reader { return io.LimitReader(repeatedByte('x'), n) }

// blanks gives n spaces: JSON reads them as the white space between two tokens.
func blanks(n int64) io.Reader { return io.LimitReader(repeatedByte(' '), n) }

// counted counts the bytes that the handler reads from a request body.
type counted struct {
	r io.Reader
	n int64
}

func (c *counted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func limitsHandler(t *testing.T) (http.Handler, func()) {
	t.Helper()
	d := dbtest.New(t)
	counts := func() string {
		var out string
		for _, table := range []string{"entities", "entity_names", "measurements", "files"} {
			var n int
			if err := d.R.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil && err != sql.ErrNoRows {
				t.Fatal(err)
			}
			out += fmt.Sprint(n, ",")
		}
		return out
	}
	before := counts()
	return api.New(&core.Store{DB: d}), func() {
		t.Helper()
		if after := counts(); after != before {
			t.Fatalf("writes on refusal: %s -> %s", before, after)
		}
	}
}

// keptBody is the body of the page in the answer w.
func keptBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var page struct {
		Properties struct{ Body string } `json:"properties"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("the answer: %v", err)
	}
	return page.Properties.Body
}

// TestRequestBufferLimits: a JSON or URL-encoded request has at most 20 MiB. The limit is of the request, not of a
// field. One byte under and one byte over the limit, blanks between two tokens fill the request. At the limit, the
// body of the page fills it, and the page keeps the whole body. The framing is the same 26 bytes.
func TestRequestBufferLimits(t *testing.T) {
	h, unchanged := limitsHandler(t)
	head, tail := `{"title":"Long",`, `"body":""}`
	framing := int64(len(head) + len(tail))
	for _, delta := range []int64{-1, 0, 1} {
		boundaryHandler, boundaryUnchanged := limitsHandler(t)
		n := int64(20<<20) - framing + delta
		var body io.Reader
		if delta == 0 {
			body = io.MultiReader(strings.NewReader(`{"title":"Long","body":"`), generated(n), strings.NewReader(`"}`))
		} else {
			body = io.MultiReader(strings.NewReader(head), blanks(n), strings.NewReader(tail))
		}
		r := httptest.NewRequest("POST", "/pages", body)
		r.Header.Set("Content-Type", "application/json")
		r.TransferEncoding = []string{"chunked"}
		w := httptest.NewRecorder()
		boundaryHandler.ServeHTTP(w, r)
		want := http.StatusOK
		if delta > 0 {
			want = 413
		}
		if delta > 0 {
			boundaryUnchanged()
		}
		if w.Code != want {
			t.Fatalf("size %d: %d want %d: %.200s", n+framing, w.Code, want, w.Body.String())
		}
		if delta == 0 {
			if got := keptBody(t, w); got != strings.Repeat("x", int(n)) {
				t.Fatalf("the body at the limit: %d bytes, want %d bytes of x", len(got), n)
			}
		}
	}
	defer unchanged()
	// A request of 1 MiB more than the limit: the handler reads the limit and one byte more, and no other byte.
	// http.MaxBytesReader asks for one byte past the limit, and it refuses the request when it gets that byte.
	for _, ct := range []string{"application/json", "application/x-www-form-urlencoded"} {
		body := &counted{r: io.MultiReader(strings.NewReader(`{"title":"Overflow","body":"`), generated(21<<20), strings.NewReader(`"}`))}
		r := httptest.NewRequest("POST", "/pages", body)
		r.ContentLength = 1
		r.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 413 {
			t.Fatalf("%s: %d %.200s", ct, w.Code, w.Body.String())
		}
		if body.n != 20<<20+1 {
			t.Fatalf("%s: the handler read %d bytes, want %d (the limit and one byte)", ct, body.n, 20<<20+1)
		}
	}
	r := httptest.NewRequest("POST", "/pages", strings.NewReader(`{"title":"Suffix"} {}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("suffix: %d", w.Code)
	}
}

// firstFields is the length of the text of the three fields that come first in a form: title, sha256 and mime.
const firstFields = int64(len("Synthetic") + 64 + len("application/octet-stream"))

func TestMultipartAggregateLimits(t *testing.T) {
	for _, kind := range []string{"aggregate", "unique", "body-at", "text-at", "text-over", "parts", "original", "preview", "large-original"} {
		t.Run(kind, func(t *testing.T) {
			h, unchanged := limitsHandler(t)
			pr, pw := io.Pipe()
			mw := multipart.NewWriter(pw)
			go func() {
				defer pw.Close()
				defer mw.Close()
				mw.WriteField("title", "Synthetic")
				if kind != "large-original" {
					mw.WriteField("sha256", strings.Repeat("a", 64))
				}
				mw.WriteField("mime", "application/octet-stream")
				switch kind {
				case "body-at":
					// The body fills the text of the request to the limit, and the page keeps the whole body.
					p, e := mw.CreateFormField("body")
					if e != nil {
						return
					}
					if _, e = io.Copy(p, generated(16<<20-firstFields)); e != nil {
						return
					}
				case "text-at", "text-over":
					// A repeated body field fills the text of the request to the limit, less a short body. The body comes
					// last: it is kept only when the text of all the fields is at most 16 MiB.
					p, e := mw.CreateFormField("body")
					if e != nil {
						return
					}
					n := 16<<20 - firstFields - int64(len("the text"))
					if kind == "text-over" {
						n++
					}
					if _, e = io.Copy(p, generated(n)); e != nil {
						return
					}
					if mw.WriteField("body", "the text") != nil {
						return
					}
				case "aggregate", "unique":
					for i := 0; i < 17; i++ {
						p, e := mw.CreateFormField("body")
						if e != nil {
							return
						}
						if _, e = io.Copy(p, generated(1<<20)); e != nil {
							return
						}
					}
				case "parts":
					for i := 0; i < 65; i++ {
						if mw.WriteField("body", "") != nil {
							return
						}
					}
				case "original", "preview":
					for i := 0; i < 2; i++ {
						p, e := mw.CreateFormFile(kind, "synthetic.bin")
						if e != nil {
							return
						}
						if _, e = p.Write([]byte("x")); e != nil {
							return
						}
					}
				case "large-original":
					p, e := mw.CreateFormFile("original", "synthetic.bin")
					if e != nil {
						return
					}
					io.Copy(p, generated(65<<20))
				}
			}()
			defer pr.Close()
			r := httptest.NewRequest("POST", "/files", pr)
			r.Header.Set("Content-Type", mw.FormDataContentType())
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 413
			if kind == "large-original" || kind == "body-at" || kind == "text-at" {
				want = 200
			} // an original larger than the preview and JSON budgets still succeeds
			if want == 413 {
				unchanged()
			}
			if w.Code != want {
				t.Fatalf("%d want %d: %.200s", w.Code, want, w.Body.String())
			}
			switch kind {
			case "body-at":
				if got := keptBody(t, w); got != strings.Repeat("x", int(16<<20-firstFields)) {
					t.Fatalf("the body that fills the text: %d bytes, want %d bytes of x", len(got), 16<<20-firstFields)
				}
			case "text-at":
				if got := keptBody(t, w); got != "the text" {
					t.Fatalf("the body at the limit: %q; want it kept", got)
				}
			}
		})
	}
}

// TestMultipartTextLimitStopsTheRead: a form whose text goes 1 MiB past the limit of 16 MiB is refused, and the
// handler stops its read near the limit. An original has no limit, so the request as a whole has none. The handler
// reads the text up to the limit and one byte more. The multipart reader reads at most 4096 bytes more than the
// part gives (its buffer: peekBufferSize of mime/multipart).
func TestMultipartTextLimitStopsTheRead(t *testing.T) {
	h, unchanged := limitsHandler(t)
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	for _, f := range [][2]string{{"title", "Synthetic"}, {"sha256", strings.Repeat("a", 64)}, {"mime", "application/octet-stream"}} {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mw.CreateFormField("body"); err != nil {
		t.Fatal(err)
	}
	head := bytes.Clone(form.Bytes()) // the bytes before the text of the body
	form.Reset()
	if err := mw.Close(); err != nil { // the end of the form
		t.Fatal(err)
	}
	body := &counted{r: io.MultiReader(bytes.NewReader(head), generated(17<<20-firstFields), &form)}
	r := httptest.NewRequest("POST", "/files", body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("%d want 413: %.200s", w.Code, w.Body.String())
	}
	unchanged()
	if read := int64(len(head)) + 16<<20 - firstFields + 1; body.n < read || body.n > read+4096 {
		t.Fatalf("the handler read %d bytes, want %d to %d: the text up to the limit, one byte, and at most the buffer of the multipart reader", body.n, read, read+4096)
	}
}

// TestARefusedLargeRequestClosesTheConnection: through a real server, a JSON or URL-encoded request of more than 20 MiB
// gets 413 with "Connection: close", and then the server closes the connection: it reads no other request from it.
// Each request has 1 KiB more than the limit. Without the close, net/http reads the rest of so short a body after the
// handler, and it keeps the connection open for the next request.
func TestARefusedLargeRequestClosesTheConnection(t *testing.T) {
	h, unchanged := limitsHandler(t)
	defer unchanged()
	srv := httptest.NewServer(h)
	defer srv.Close()
	for _, c := range []struct{ path, ct, head, tail string }{
		{"/pages", "application/json", `{"title":"Overflow","body":"`, `"}`},
		{"/pages", "application/x-www-form-urlencoded", "title=Overflow&body=", ""},
	} {
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		size := int64(20<<20 + 1<<10)
		head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s",
			c.path, srv.Listener.Addr(), c.ct, size, c.head)
		// The request is written while the answer is read. A write error is not checked: a server may close the
		// connection before it reads the whole body.
		wrote := make(chan struct{})
		go func() {
			defer close(wrote)
			_, _ = io.Copy(conn, io.MultiReader(strings.NewReader(head),
				generated(size-int64(len(c.head)+len(c.tail))), strings.NewReader(c.tail)))
		}()
		if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		r := bufio.NewReader(conn)
		resp, err := http.ReadResponse(r, nil)
		if err != nil {
			t.Fatalf("%s %s: %v", c.path, c.ct, err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		if err := errors.Join(err, resp.Body.Close()); err != nil {
			t.Fatalf("%s %s: the answer: %v", c.path, c.ct, err)
		}
		if resp.StatusCode != http.StatusRequestEntityTooLarge || !resp.Close {
			t.Errorf("%s %s: %d, close %v; want 413 and Connection: close", c.path, c.ct, resp.StatusCode, resp.Close)
		}
		if _, err := r.ReadByte(); !errors.Is(err, io.EOF) {
			t.Errorf("%s %s: after the 413, a read of the connection gives %v, want io.EOF: the server keeps the connection open", c.path, c.ct, err)
		}
		conn.Close()
		<-wrote
	}
}
