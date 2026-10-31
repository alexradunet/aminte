package api_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api/apitest"
)

// send makes one request to the handler and gives back the status and the body.
func send(h http.Handler, method, target, ctype string, body io.Reader) (int, string) {
	req := httptest.NewRequest(method, target, body)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestAnUnknownFieldIsRefused: a request names only the fields of its action. A GET or a HEAD may also name new (the
// action whose form a link asks for) and embed. Any other name is 422, in a form, in JSON, in a query and in a
// multipart body, and nothing is written. back and feedback were the names of a browser: they are unknown now, and so is new on a POST.
func TestAnUnknownFieldIsRefused(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	s.SnapshotDir = t.TempDir() // a snapshot that a refusal did not stop would land here
	const form = "application/x-www-form-urlencoded"
	pages := func() string { return scalar(t, s, "SELECT count(*) FROM entities") }
	before := pages()

	for _, tc := range []struct{ name, method, target, ctype, body string }{
		{"a form", "POST", "/days/2026-09-01/capture", form, "text=x&nope=1"},
		{"a JSON body", "POST", "/days/2026-09-01/capture", "application/json", `{"text":"x","nope":"1"}`},
		{"a query of a GET", "GET", "/search?q=a&nope=1", "", ""},
		{"a new that names no action", "GET", "/browse?new=typo", "", ""},
		{"a field of the asked form and one more", "GET", "/browse?new=create-page&nope=1", "", ""},
		{"back in a form", "POST", "/days/2026-09-01/capture", form, "text=x&back=%2F"},
		{"feedback in a form", "POST", "/days/2026-09-01/capture", form, "text=x&feedback=t"},
		{"back in a JSON body", "POST", "/days/2026-09-01/capture", "application/json", `{"text":"x","back":"/"}`},
		{"back in a query", "GET", "/search?q=a&back=%2F", "", ""},
		{"feedback in a query", "GET", "/search?q=a&feedback=t", "", ""},
		{"new in a form", "POST", "/days/2026-09-01/capture", form, "text=x&new=create-page"},
		// The actions that list no field, or only an id, refuse a name as the others do.
		{"a name sent to snapshot", "POST", "/snapshots", form, "nope=1"},
		{"a name sent to integrity-check", "GET", "/integrity?nope=1", "", ""},
		{"a name sent to rebuild-search", "POST", "/search/rebuild", form, "nope=1"},
		{"a name sent to resync-links", "POST", "/links/resync", form, "nope=1"},
		{"a name sent to revive", "POST", "/pages/1/revive", form, "nope=1"},
		{"a name sent to retract", "POST", "/measurements/1/retract", form, "nope=1"},
		{"a JSON name sent to snapshot", "POST", "/snapshots", "application/json", `{"nope":"1"}`},
	} {
		code, body := send(h, tc.method, tc.target, tc.ctype, strings.NewReader(tc.body))
		if code != 422 || !strings.Contains(body, "invalid") || strings.Contains(tc.name, "sent to") && !strings.Contains(body, "unknown field nope") {
			t.Errorf("%s: %d %s, want 422 invalid, unknown field nope", tc.name, code, body)
		}
	}
	if left, err := os.ReadDir(s.SnapshotDir); err != nil || len(left) != 0 {
		t.Errorf("the snapshot folder after the refusals: %v, %v, want it empty", left, err)
	}
	code, body := send(h, "POST", "/days/2026-09-01/capture", form, strings.NewReader("text=x&nope=1"))
	if code != 422 || !strings.Contains(body, "nope") || !strings.Contains(body, "capture") {
		t.Errorf("the message: %d %s, want the field and the action named", code, body)
	}
	if got := pages(); got != before {
		t.Errorf("entities %s, want %s: a refused request wrote", got, before)
	}

	// A HEAD gets the status of the GET (RFC 9110), so it takes new and embed too.
	for _, target := range []string{"/search?q=a", "/browse?new=create-page&title=x", "/search?q=a&new=create-page&embed=item"} {
		for _, method := range []string{"GET", "HEAD"} {
			if code, body := send(h, method, target, "", nil); code != 200 {
				t.Errorf("%s %s: %d %s, want 200", method, target, code, body)
			}
		}
	}
	for _, target := range []string{"/search?q=a&nope=1", "/browse?new=typo"} {
		if code, _ := send(h, "HEAD", target, "", nil); code != 422 {
			t.Errorf("HEAD %s: %d, want 422 as the GET", target, code)
		}
	}
	// A multipart body to an action that takes no file is 415, also when the action lists no field at all, and also
	// for a GET: it reads the query, so a body that it would drop is refused, as for a POST.
	for _, tc := range []struct{ method, target string }{
		{"POST", "/snapshots"}, {"POST", "/search/rebuild"}, {"POST", "/links/resync"}, {"POST", "/pages/1/revive"},
		{"POST", "/measurements/1/retract"}, {"GET", "/integrity"}, {"GET", "/search?q=a"}, {"GET", "/browse"},
	} {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		mw.WriteField("nope", "1")
		mw.Close()
		if code, body := send(h, tc.method, tc.target, mw.FormDataContentType(), &b); code != 415 {
			t.Errorf("a multipart body to %s %s: %d %s, want 415", tc.method, tc.target, code, body)
		}
	}
	// A multipart part named back, feedback or new is an unknown name too.
	for _, name := range []string{"back", "feedback", "new"} {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		mw.WriteField("title", "Scan.jpg")
		mw.WriteField(name, "x")
		mw.Close()
		if code, body := send(h, "POST", "/files", mw.FormDataContentType(), &b); code != 422 || !strings.Contains(body, "unknown field "+name) {
			t.Errorf("a multipart part %s: %d %s, want 422 that names it", name, code, body)
		}
	}
	if got := pages(); got != before {
		t.Errorf("entities %s, want %s: a refused request wrote", got, before)
	}
}

// TestAValueOutsideTheStaticOptionsIsRefused: the options that GET /actions lists bind the server. An empty value
// passes. The options of a metric are runtime, so a metric that does not exist reaches the handler's own refusal.
func TestAValueOutsideTheStaticOptionsIsRefused(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	const form = "application/x-www-form-urlencoded"
	for _, tc := range []struct{ method, target, body, field string }{
		{"GET", "/search?q=a&kind=planet", "", "kind"},
		{"GET", "/search?q=a&kind=metric", "", "kind"}, // a kind of Browse, not of a hit
		{"GET", "/search?q=a&order=oldest", "", "order"},
		{"GET", "/search?q=a&order=NEWEST", "", "order"},
		{"GET", "/search?q=a&order=%20newest", "", "order"},
		{"GET", "/search?q=a&order=rank%3B+DROP+TABLE+entities", "", "order"},
		{"GET", "/pages/random?kind=metrics", "", "kind"},
		{"GET", "/pages/random?kind=x", "", "kind"},
		{"GET", "/browse?unfiled=yes", "", "unfiled"},
		{"GET", "/browse?ghosts=2", "", "ghosts"},
		{"GET", "/browse?kind=pages", "", "kind"},
		{"GET", "/browse?kind=media&media=pdf", "", "media"},
		{"POST", "/pages/1/metric", "daily=2", "daily"},
		{"POST", "/days/2026-09-01/capture", "text=x&mood=6", "mood"},
	} {
		code, body := send(h, tc.method, tc.target, form, strings.NewReader(tc.body))
		if code != 422 || !strings.Contains(body, tc.field+" must be one of") {
			t.Errorf("%s %s %s: %d %s, want 422 %q must be one of", tc.method, tc.target, tc.body, code, body, tc.field)
		}
	}
	if code, body := send(h, "GET", "/search?q=a&kind=", "", nil); code != 200 {
		t.Errorf("an empty kind: %d %s, want 200", code, body)
	}
	code, body := send(h, "POST", "/measurements", form, strings.NewReader(url.Values{"metric": {"Unknown"}, "day": {"2026-09-01"}, "value": {"1"}}.Encode()))
	if code != 404 {
		t.Errorf("a metric that does not exist: %d %s, want the handler's 404", code, body)
	}
}

// zeros is a reader of n zero bytes that counts the bytes it gave.
type zeros struct{ left, given int64 }

func (z *zeros) Read(p []byte) (int, error) {
	if z.left == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), z.left)
	clear(p[:n])
	z.left -= n
	z.given += n
	return int(n), nil
}

// TestAMultipartPartWithAnUnknownNameIsRefusedBeforeItIsRead: the refusal is 422, not the limit 413, and the handler
// stops before it reads the part's bytes.
func TestAMultipartPartWithAnUnknownNameIsRefusedBeforeItIsRead(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	const size = 20 << 20
	body := &zeros{left: size}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	wrote := make(chan struct{}) // the writer ended: body.given is read only after it
	go func() {
		defer close(wrote)
		p, err := mw.CreateFormField("unknown")
		if err == nil {
			_, err = io.Copy(p, body)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	req := httptest.NewRequest("POST", "/files", pr)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the handler did not answer")
	}
	pr.Close() // the writer's next write fails, and it ends
	<-wrote
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), "unknown") {
		t.Fatalf("%d %s, want 422 naming the field", rec.Code, rec.Body.String())
	}
	if body.given > 1<<20 {
		t.Errorf("the handler read %d bytes of the part before it refused it", body.given)
	}
}

// TestSetCoverTakesOnlyItsOwnFields: set-cover lists original, title and preview. The other names of a file form are
// refused, in a plain form and in a multipart form.
func TestSetCoverTakesOnlyItsOwnFields(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	trip := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Trip"}))
	scan, _ := jpegFile(t, "scan.jpg", 20, 20)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "add-file"), map[string]string{"title": "Scan.jpg", "original": scan}))
	cover := apitest.Href(trip, "self") + "/cover"
	for _, name := range []string{"dry_run", "day", "body", "sha256", "mime"} {
		code, body := send(h, "POST", cover, "application/x-www-form-urlencoded", strings.NewReader("title=Scan.jpg&"+name+"=1"))
		if code != 422 || !strings.Contains(body, name) {
			t.Errorf("%s: %d %s, want 422", name, code, body)
		}
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		mw.WriteField("title", "Scan.jpg")
		mw.WriteField(name, "1")
		mw.Close()
		if code, body := send(h, "POST", cover, mw.FormDataContentType(), &b); code != 422 {
			t.Errorf("multipart %s: %d %s, want 422", name, code, body)
		}
	}
	if code, body := send(h, "POST", cover, "application/x-www-form-urlencoded", strings.NewReader("title=Scan.jpg")); code >= 400 {
		t.Errorf("a kept picture: %d %s, want success", code, body)
	}
}
