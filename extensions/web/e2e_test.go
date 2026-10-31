package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"maps"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"lifelog-web/internal/formdoc"
	"lifelog/client"
	"lifelog/siren"
)

// The end-to-end test builds the real lifelog and lifelog-web into one folder and runs the line of the live unit on a new
// life.db: `lifelog serve --token-file F --with web`, with LIFELOG_WEB_LISTEN, LIFELOG_WEB_ORIGINS and
// LIFELOG_WEB_STATE in the environment. Then it is a browser: a cookie jar, Accept: text/html, the fetch headers of a
// navigation, and no other door than the door of web. It holds what the tests of ./web cannot: the program, its
// environment, the handshake of the catalog "This browser" (served on LIFELOG_LISTEN, merged by the core as /with/web),
// the owner rule of the core, and the stop by an interrupt. The pages, the forms and the lock are held in detail by the
// tests of ./web; this test holds that the whole line carries them.
//
// Every wait has a deadline and polls a fact (a line of the log, a port, an exit). None sleeps a fixed time.

// wait is a guard against a deadlock, not a measure of speed.
const wait = 2 * time.Minute

// stopBound is the longest time that an interrupt of lifelog may take to end both processes: "Run by serve" in
// extensions/README.md gives the child 10 seconds, and the server then drains for 10 seconds at most. Five seconds of
// slack are for a slow machine.
const stopBound = 25 * time.Second

// ownerToken is the token of the test: 40 characters of no meaning. It is the login of the browser and the bearer token of
// the core.
const ownerToken = "e2e-token-e2e-token-e2e-token-e2e-token"

// ownerPIN is the PIN that the test sets for the lock: four digits of no meaning.
const ownerPIN = "4821"

// proxyOrigin is the origin that a proxy shows to the browser: web trusts it besides its own.
const proxyOrigin = "https://lifelog.example"

// lockedBuffer is the stderr of the server: the process writes it and the test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// bins is the folder with lifelog and lifelog-web.
type bins struct{ dir, lifelog string }

func build(t *testing.T) bins {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to build lifelog: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "lifelog")); err != nil {
		t.Skipf("the lifelog sources are not at %s: %v", root, err)
	}
	b := bins{dir: t.TempDir()}
	b.lifelog = filepath.Join(b.dir, "lifelog")
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.lifelog, "./cmd/lifelog"},
		{"build", "-o", filepath.Join(b.dir, "lifelog-web"), "./cmd/lifelog-web"},
	} {
		if out, err := exec.Command(goBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return b
}

// loopbackAddr is a loopback address that nothing listens on now. The program binds it a moment later, as `serve` does for
// the catalog of a child: another process may take the port between, and the test then fails with the log of the server.
func loopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// stack is a running `lifelog serve --token-file F --with web`.
type stack struct {
	t       *testing.T
	bin     string // the lifelog program, for the command line
	cmd     *exec.Cmd
	stderr  *lockedBuffer
	exited  chan error
	core    string // http://127.0.0.1:PORT, the core
	door    string // http://127.0.0.1:PORT, the door of the browser
	state   string // the state folder of web
	webPID  int
	stopped bool // the test stopped the server and read its end
}

// startStack builds nothing: b holds the programs. It makes a new life.db and a token file, starts the server as the unit
// does and returns when the door answers and web has registered its catalog.
func startStack(t *testing.T, b bins) *stack {
	t.Helper()
	work := t.TempDir()
	db, tokenFile := filepath.Join(work, "life.db"), filepath.Join(work, "token")
	if out, err := exec.Command(b.lifelog, "init", db).CombinedOutput(); err != nil {
		t.Fatalf("lifelog init: %v\n%s", err, out)
	}
	if err := os.WriteFile(tokenFile, []byte(ownerToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &stack{t: t, bin: b.lifelog, stderr: new(lockedBuffer), exited: make(chan error, 1), state: filepath.Join(work, "state")}
	listen := loopbackAddr(t)
	s.door = "http://" + listen
	s.cmd = exec.Command(b.lifelog, "--db", db, "serve", "--addr", "127.0.0.1:0", "--token-file", tokenFile, "--with", "web")
	// The environment of the unit, and nothing of the owner's: any LIFELOG_ variable of the shell could point the child
	// at another file or server.
	s.cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "LIFELOG_") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=")
	}), "LIFELOG_WEB_LISTEN="+listen, "LIFELOG_WEB_ORIGINS="+s.door+" "+proxyOrigin, "LIFELOG_WEB_STATE="+s.state,
		"XDG_CONFIG_HOME="+filepath.Join(work, "config"))
	s.cmd.Stderr = s.stderr
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exited <- s.cmd.Wait() }()
	t.Cleanup(s.stop)

	line := s.waitFor("lifelog: serving ")
	_, rest, _ := strings.Cut(line, " on http://")
	addr, _, _ := strings.Cut(rest, " ")
	s.core = "http://" + addr
	started := regexp.MustCompile(`lifelog: with web: started \(pid (\d+)\)`).FindStringSubmatch(s.waitFor("lifelog: with web: started (pid "))
	if started == nil {
		t.Fatalf("no pid of web in the log:\n%s", s.stderr)
	}
	s.webPID, _ = strconv.Atoi(started[1])
	s.waitFor("lifelog: with web: a catalog of 1 actions on 127.0.0.1:")
	s.waitForDoor()
	return s
}

// lifelogCLI runs `lifelog --url CORE args...` as the owner does at a shell: with the token in $LIFELOG_TOKEN and no other
// LIFELOG_ variable. It returns what the program wrote and how it ended.
func (s *stack) lifelogCLI(args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(s.t.Context(), wait)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.bin, append([]string{"--url", s.core}, args...)...)
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "LIFELOG_") }), "LIFELOG_TOKEN="+ownerToken)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// savedSettings are the settings of web as its state folder holds them.
func (s *stack) savedSettings() map[string]string {
	s.t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.state, "settings.json"))
	if err != nil {
		s.t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(raw, &saved); err != nil {
		s.t.Fatalf("the settings file is not JSON: %v\n%s", err, raw)
	}
	return saved
}

// waitFor waits until the stderr holds the text and returns the line that holds it. It reads the log, never the clock.
func (s *stack) waitFor(text string) string {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		for l := range strings.SplitSeq(s.stderr.String(), "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		select {
		case err := <-s.exited:
			s.exited <- err
			s.t.Fatalf("the server ended before %q: %v\n%s", text, err, s.stderr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	s.t.Fatalf("no %q in the log after %s:\n%s", text, wait, s.stderr)
	return ""
}

// waitForDoor polls the login page until web answers on its door.
func (s *stack) waitForDoor() {
	s.t.Helper()
	probe := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		res, err := probe.Get(s.door + "/login")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case err := <-s.exited:
			s.exited <- err
			s.t.Fatalf("the server ended before the door answered: %v\n%s", err, s.stderr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	s.t.Fatalf("the door %s did not answer after %s:\n%s", s.door, wait, s.stderr)
}

// stop ends the server at the end of the test, when the test did not stop it. A server that does not end is killed.
func (s *stack) stop() {
	if s.stopped {
		return
	}
	select {
	case err := <-s.exited:
		s.exited <- err
		return
	default:
	}
	_ = s.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-s.exited:
	case <-time.After(wait):
		_ = s.cmd.Process.Kill()
		s.t.Error("the server did not stop")
	}
}

// api is the core as a program reads it: with the bearer token, as the writer source.
func (s *stack) api(source string) *client.Client { return client.Remote(s.core, source, ownerToken) }

// today is the day of today for the core.
func (s *stack) today() string {
	s.t.Helper()
	root, err := s.api("cli").Get(s.t.Context(), "/")
	if err != nil {
		s.t.Fatal(err)
	}
	props, _ := root.Properties.(map[string]any)
	day, _ := props["today"].(string)
	if day == "" {
		s.t.Fatalf("the root has no day of today: %+v", props)
	}
	return day
}

// dayWords is the text of the page of the day, as the core reads it back: the stored state, not a page of web.
func (s *stack) dayWords(day string) string {
	s.t.Helper()
	e, err := s.api("cli").Get(s.t.Context(), "/days/"+day)
	if err != nil {
		s.t.Fatal(err)
	}
	props, _ := e.Properties.(map[string]any)
	rows, _ := props["view"].([]any)
	for _, row := range rows {
		r, _ := row.(map[string]any)
		if r["what"] == "day page" {
			body, _ := r["detail"].(string)
			return body
		}
	}
	return ""
}

// getFromCore reads path from the core as a program does, with the bearer token and no cookie.
func (s *stack) getFromCore(path string) page {
	s.t.Helper()
	req, err := http.NewRequestWithContext(s.t.Context(), http.MethodGet, s.core+path, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return page{res.StatusCode, res.Header, string(body)}
}

// page is an answer that a browser got: the status, the headers and the body.
type page struct {
	Status int
	Header http.Header
	Body   string
}

// location is the address of a redirect, or "".
func (p page) location() string { return p.Header.Get("Location") }

// browser is a person with a browser: it keeps the cookies of the door, sends the headers of a navigation, and does not
// follow a redirect by itself, so the test sees each one.
type browser struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func newBrowser(t *testing.T, base string) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{} // no proxy: the door is on the loopback
	t.Cleanup(transport.CloseIdleConnections)
	return &browser{t: t, base: base, hc: &http.Client{Jar: jar, Transport: transport, Timeout: wait,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// request is a navigation of the browser to path: it accepts HTML and says, as a browser does, that it is a navigation
// inside the site.
func (b *browser) request(method, path string, body io.Reader) *http.Request {
	b.t.Helper()
	req, err := http.NewRequestWithContext(b.t.Context(), method, b.base+path, body)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	return req
}

// send sends req and reads the answer.
func (b *browser) send(req *http.Request) page {
	b.t.Helper()
	res, err := b.hc.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		b.t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	return page{res.StatusCode, res.Header, string(body)}
}

func (b *browser) get(path string) page {
	b.t.Helper()
	return b.send(b.request(http.MethodGet, path, nil))
}

// post sends the form values to path, as a browser sends a form of text. from is the page that holds the form. Headers
// are name, value pairs that replace the ones of the browser; an empty value takes the header away.
func (b *browser) post(path string, values url.Values, from string, headers ...string) page {
	b.t.Helper()
	req := b.request(http.MethodPost, path, strings.NewReader(values.Encode()))
	b.formHeaders(req, "application/x-www-form-urlencoded", from, headers)
	return b.send(req)
}

// formHeaders adds the headers of a form post: the type of the body, the page it comes from and its origin.
func (b *browser) formHeaders(req *http.Request, contentType, from string, headers []string) {
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", b.base)
	if from != "" {
		req.Header.Set("Referer", b.base+from)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
}

// fieldsOf are the fields that a browser sends for the form: the ones the page gives it, and the ones the reader changed.
func fieldsOf(f formdoc.Form, changed url.Values) url.Values {
	values := maps.Clone(f.Values)
	maps.Copy(values, changed)
	return values
}

// submit sends the form as a browser sends it, with the fields that the reader changed.
func (b *browser) submit(f formdoc.Form, from string, changed url.Values) page {
	b.t.Helper()
	if f.Method != http.MethodPost {
		b.t.Fatalf("the form %s is not a POST", f.Action)
	}
	return b.post(f.Action, fieldsOf(f, changed), from)
}

// follow follows the redirects of p, at most five, and returns the page where the browser lands.
func (b *browser) follow(p page) page {
	b.t.Helper()
	for range 5 {
		if p.Status < 300 || p.Status > 399 {
			return p
		}
		p = b.get(p.location())
	}
	b.t.Fatalf("more than five redirects, the last to %q", p.location())
	return p
}

// formOf is the form of the page that matches.
func formOf(t *testing.T, body string, match func(formdoc.Form) bool) formdoc.Form {
	t.Helper()
	f, ok := formdoc.Find(formdoc.Forms(body), match)
	if !ok {
		var seen []string
		for _, f := range formdoc.Forms(body) {
			seen = append(seen, f.Method+" "+f.Action+" ["+f.Attr["data-action"]+"]")
		}
		t.Fatalf("no such form in the page; the forms are %q", seen)
	}
	return f
}

// named is the form whose data-action is the name of an action.
func named(name string) func(formdoc.Form) bool {
	return func(f formdoc.Form) bool { return f.Attr["data-action"] == name }
}

// posting is the form that posts to path.
func posting(path string) func(formdoc.Form) bool {
	return func(f formdoc.Form) bool {
		action, _, _ := strings.Cut(f.Action, "?")
		return f.Method == http.MethodPost && action == path
	}
}

// syntheticJPEG is a small JPEG with a gradient: the picture of a synthetic file.
func syntheticJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 128, A: 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// upload sends the form of a file as a browser does: multipart/form-data with the fields of the form, the changed ones,
// and the file in the field named field.
func (b *browser) upload(f formdoc.Form, from string, changed url.Values, field, name, mimeType string, content []byte) page {
	b.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for n, vs := range fieldsOf(f, changed) {
		for _, v := range vs {
			if err := mw.WriteField(n, v); err != nil {
				b.t.Fatal(err)
			}
		}
	}
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename=%q`, field, name)}, "Content-Type": {mimeType}})
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		b.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		b.t.Fatal(err)
	}
	req := b.request(http.MethodPost, f.Action, &body)
	b.formHeaders(req, mw.FormDataContentType(), from, nil)
	return b.send(req)
}

// The words of the test. They are synthetic.
const (
	captureWords = "Walked by the lake and saw a heron. Words of the end-to-end test."
	fileTitle    = "E2E heron.jpg"
)

func TestWebOnTheRealLifelog(t *testing.T) {
	if testing.Short() {
		t.Skip("the end-to-end test builds lifelog; -short skips it")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot send an interrupt to another process")
	}
	s := startStack(t, build(t))
	person := newBrowser(t, s.door)
	today := s.today()

	t.Run("a page before the login goes to the login", func(t *testing.T) {
		if p := person.get("/"); p.Status != http.StatusSeeOther || p.location() != "/login" {
			t.Errorf("GET /: %d to %q, want 303 to /login", p.Status, p.location())
		}
		// A client that is no browser gets 401 and not a page, and a bearer header opens nothing here: a program uses the core.
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.door+"/", nil)
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET / with the bearer token and no cookie: %d, want 401", res.StatusCode)
		}
		login := person.get("/login")
		if login.Status != http.StatusOK {
			t.Fatalf("GET /login: %d", login.Status)
		}
		formOf(t, login.Body, posting("/login"))
	})

	t.Run("a wrong token stays out and the right token logs in", func(t *testing.T) {
		wrong := person.post("/login", url.Values{"token": {strings.Repeat("x", len(ownerToken))}}, "/login")
		if wrong.Status != http.StatusSeeOther || wrong.location() != "/login?failed=1" {
			t.Errorf("a wrong token: %d to %q, want 303 to /login?failed=1", wrong.Status, wrong.location())
		}
		if p := person.get("/"); p.Status != http.StatusSeeOther || p.location() != "/login" {
			t.Errorf("GET / after a wrong token: %d to %q, want 303 to /login", p.Status, p.location())
		}
		right := person.post("/login", url.Values{"token": {ownerToken}}, "/login")
		if right.Status != http.StatusSeeOther || right.location() != "/" {
			t.Fatalf("the right token: %d to %q, want 303 to /", right.Status, right.location())
		}
		cookie := strings.Join(right.Header.Values("Set-Cookie"), "\n")
		for _, want := range []string{"lifelog_session=", "HttpOnly", "SameSite=Strict"} {
			if !strings.Contains(cookie, want) {
				t.Errorf("the session cookie %q lacks %s", cookie, want)
			}
		}
		if strings.Contains(cookie, ownerToken) {
			t.Error("the session cookie holds the token")
		}
	})

	t.Run("Home draws the day of today in the shell", func(t *testing.T) {
		home := person.get("/")
		if home.Status != http.StatusOK || !strings.HasPrefix(home.Header.Get("Content-Type"), "text/html") {
			t.Fatalf("GET /: %d %s", home.Status, home.Header.Get("Content-Type"))
		}
		// The day, its capture form, the search of the rail and the pictures and fonts of the shell. The sheets are in the page.
		for _, want := range []string{"<title>", "/days/" + today + "/capture", `action="/search"`, "/assets/fonts/", "/assets/chat.js"} {
			if !strings.Contains(home.Body, want) {
				t.Errorf("Home lacks %q", want)
			}
		}
		script := person.get("/assets/chat.js")
		if script.Status != http.StatusOK || !strings.Contains(script.Header.Get("Content-Type"), "javascript") || script.Header.Get("Cache-Control") == "no-store" {
			t.Errorf("GET /assets/chat.js: %d %s, Cache-Control %q, want the script, cacheable", script.Status, script.Header.Get("Content-Type"), script.Header.Get("Cache-Control"))
		}
	})

	t.Run("a capture through the form of the day is stored, and the toast shows once", func(t *testing.T) {
		home := person.get("/")
		form := formOf(t, home.Body, named("capture"))
		if form.Action != "/days/"+today+"/capture" {
			t.Fatalf("the capture form of Home posts to %s", form.Action)
		}
		done := person.submit(form, "/", url.Values{"text": {captureWords}})
		if done.Status != http.StatusSeeOther || !strings.Contains(done.location(), "feedback=") {
			t.Fatalf("the capture: %d to %q, want 303 with a receipt of the toast", done.Status, done.location())
		}
		landed := person.get(done.location())
		if landed.Status != http.StatusOK || !strings.Contains(landed.Body, `class="toast"`) || !strings.Contains(landed.Body, captureWords) {
			t.Errorf("the page after the capture has no toast, or lacks the words:\n%.800s", landed.Body)
		}
		if again := person.get(done.location()); strings.Contains(again.Body, `class="toast"`) {
			t.Error("the toast shows a second time: a reload would show the old result")
		}
		if got := s.dayWords(today); !strings.Contains(got, captureWords) {
			t.Errorf("the core holds %q for today, want it to hold the captured words", got)
		}
	})

	t.Run("a file goes to the core through the form add-file", func(t *testing.T) {
		fresh := person.get("/browse?kind=media&new=add-file")
		form := formOf(t, fresh.Body, named("add-file"))
		if !form.Multipart() || !slices.Contains(form.Files, "original") || form.Action != "/files" {
			t.Fatalf("the form add-file is not a form of a file that posts to /files: %+v", form)
		}
		done := person.upload(form, "/browse?kind=media&new=add-file", url.Values{"title": {fileTitle}, "body": {"A heron at dawn."}, "day": {today}},
			"original", "IMG_0001.JPG", "image/jpeg", syntheticJPEG(t))
		if done.Status != http.StatusSeeOther || !strings.Contains(done.location(), "feedback=") {
			t.Fatalf("add-file: %d to %q\n%.500s", done.Status, done.location(), done.Body)
		}
		if landed := person.follow(done); landed.Status != http.StatusOK || !strings.Contains(landed.Body, `class="toast"`) {
			t.Errorf("the page after add-file: %d, no toast", landed.Status)
		}
		file, _, _ := strings.Cut(done.location(), "?")
		stored, err := s.api("cli").Get(t.Context(), file)
		if err != nil {
			t.Fatalf("the core holds no page at %s, where the form sent the browser: %v", file, err)
		}
		if stored.Title != fileTitle || !slices.Contains(stored.Class, "file") {
			t.Errorf("the core holds %q of class %v at %s, want the file %q", stored.Title, stored.Class, file, fileTitle)
		}
		// The picture that the core made streams through web: the same type and the same bytes as the core gives a program.
		shown := person.get(file)
		img := regexp.MustCompile(`src="(/pages/\d+/preview)"`).FindStringSubmatch(shown.Body)
		if img == nil {
			t.Fatalf("the page of the file %s shows no preview:\n%.1500s", file, shown.Body)
		}
		pic := person.get(img[1])
		direct := s.getFromCore(img[1])
		if pic.Status != http.StatusOK || !strings.HasPrefix(pic.Header.Get("Content-Type"), "image/") || pic.Body == "" {
			t.Errorf("GET %s: %d %s, %d bytes, want a picture", img[1], pic.Status, pic.Header.Get("Content-Type"), len(pic.Body))
		}
		if pic.Body != direct.Body || pic.Header.Get("Content-Type") != direct.Header.Get("Content-Type") {
			t.Errorf("the picture through web (%s, %d bytes) is not the picture of the core (%s, %d bytes)",
				pic.Header.Get("Content-Type"), len(pic.Body), direct.Header.Get("Content-Type"), len(direct.Body))
		}
	})

	t.Run("the guard trusts the origin of the proxy and no other", func(t *testing.T) {
		home := person.get("/")
		form := formOf(t, home.Body, named("capture"))
		before := s.dayWords(today)
		// A browser that sends no Sec-Fetch-Site sends Origin: the origin of the proxy is listed in LIFELOG_WEB_ORIGINS.
		viaProxy := person.post(form.Action, url.Values{"text": {"Said through the proxy."}}, "/", "Sec-Fetch-Site", "", "Origin", proxyOrigin)
		if viaProxy.Status != http.StatusSeeOther {
			t.Errorf("a write from the origin of the proxy: %d, want 303", viaProxy.Status)
		}
		foreign := person.post(form.Action, url.Values{"text": {"Said from another site."}}, "/", "Sec-Fetch-Site", "", "Origin", "https://evil.example")
		if foreign.Status != http.StatusForbidden {
			t.Errorf("a write from a foreign origin: %d, want 403", foreign.Status)
		}
		after := s.dayWords(today)
		if !strings.Contains(after, "Said through the proxy.") || strings.Contains(after, "Said from another site.") || len(after) <= len(before) {
			t.Errorf("the day after the two writes:\n%s", after)
		}
	})

	t.Run("the settings of this browser are saved by the form of the catalog of web", func(t *testing.T) {
		settings := person.get("/settings")
		if settings.Status != http.StatusOK || !strings.Contains(settings.Body, "<h2>This browser</h2>") {
			t.Fatalf("GET /settings: %d, no form \"This browser\"", settings.Status)
		}
		form := formOf(t, settings.Body, named("web-settings"))
		if form.Action != "/with/web/settings" {
			t.Fatalf("the form \"This browser\" posts to %s, want /with/web/settings", form.Action)
		}
		done := person.submit(form, "/settings", url.Values{"ground": {"oak"}, "lock_after": {"5"}, "pin": {ownerPIN}})
		if done.Status != http.StatusSeeOther || !strings.HasPrefix(done.location(), "/settings?feedback=") {
			t.Fatalf("the save: %d to %q\n%.500s", done.Status, done.location(), done.Body)
		}
		if landed := person.get(done.location()); !strings.Contains(landed.Body, "Settings saved.") || !strings.Contains(landed.Body, `data-ground="oak"`) {
			t.Errorf("the page after the save lacks the toast or the ground:\n%.800s", landed.Body)
		}
		path := filepath.Join(s.state, "settings.json")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("the settings file has mode %v, want 0600", info.Mode().Perm())
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]string
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatalf("the settings file is not JSON: %v\n%s", err, raw)
		}
		if saved["ground"] != "oak" || saved["lock_after"] != "5" || !strings.HasPrefix(saved["lock_pin_hash"], "pbkdf2-sha256$") {
			t.Errorf("the settings file: %v", saved)
		}
		if strings.Contains(string(raw), ownerPIN) {
			t.Error("the settings file holds the PIN and not only its hash")
		}
		if entries, _ := os.ReadDir(s.state); len(entries) != 1 {
			t.Errorf("the state folder holds %d files, want only settings.json", len(entries))
		}
	})

	t.Run("the core lists web-settings as the owner's and refuses it to an agent", func(t *testing.T) {
		catalog, err := s.api("cli").Catalog(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(catalog, func(a siren.Action) bool { return a.Name == "web-settings" })
		if i < 0 {
			t.Fatal("GET /actions does not list web-settings")
		}
		action := catalog[i]
		if !action.Owner || action.Href != "/with/web/settings" || action.Method != http.MethodPost {
			t.Errorf("web-settings in the catalog: %+v", action)
		}
		var refusal *client.Error
		_, err = s.api("agent:x").Do(t.Context(), action, map[string]string{"ground": "boards"})
		if !errors.As(err, &refusal) || refusal.Status != http.StatusForbidden {
			t.Errorf("an agent ran web-settings: %v, want 403", err)
		}
		raw, err := os.ReadFile(filepath.Join(s.state, "settings.json"))
		if err != nil || !strings.Contains(string(raw), `"ground": "oak"`) {
			t.Errorf("the settings after the refusal: %s %v, want the ground oak", raw, err)
		}
	})

	t.Run("the ground is set on the command line, and a floor outside the four is refused", func(t *testing.T) {
		out, errOut, err := s.lifelogCLI("do", "web-settings", "ground=embers")
		if err != nil {
			t.Fatalf("lifelog do web-settings ground=embers: %v\n%s%s", err, out, errOut)
		}
		if saved := s.savedSettings(); saved["ground"] != "embers" || saved["lock_after"] != "5" {
			t.Errorf("the settings file after the command: %v, want the ground embers and the lock as it was", saved)
		}
		if landed := person.get("/settings"); landed.Status != http.StatusOK || !strings.Contains(landed.Body, `data-ground="embers"`) {
			t.Errorf("the page of the settings after the command: %d, no data-ground=embers", landed.Status)
		}
		out, errOut, err = s.lifelogCLI("do", "web-settings", "ground=flagstones")
		var exit *exec.ExitError
		if !errors.As(err, &exit) || !strings.Contains(out+errOut, "ground") {
			t.Errorf("ground=flagstones: %v, want a refusal that names the field\n%s%s", err, out, errOut)
		}
		if saved := s.savedSettings(); saved["ground"] != "embers" {
			t.Errorf("the settings file after the refusal: %v, want the ground embers", saved)
		}
	})

	t.Run("the lock shuts the session and the PIN opens it", func(t *testing.T) {
		home := person.get("/")
		lock := formOf(t, home.Body, posting("/lock"))
		if shut := person.submit(lock, "/", nil); shut.Status != http.StatusSeeOther || shut.location() != "/locked" {
			t.Fatalf("POST /lock: %d to %q, want 303 to /locked", shut.Status, shut.location())
		}
		if p := person.get("/"); p.Status != http.StatusSeeOther || !strings.HasPrefix(p.location(), "/locked?next=") {
			t.Errorf("GET / while locked: %d to %q, want 303 to /locked", p.Status, p.location())
		}
		// A write is refused while the session is locked, and nothing is stored.
		form := formOf(t, home.Body, named("capture"))
		if p := person.submit(form, "/", url.Values{"text": {"Written while locked."}}); p.Status != http.StatusUnauthorized {
			t.Errorf("a capture while locked: %d, want 401", p.Status)
		}
		if strings.Contains(s.dayWords(today), "Written while locked.") {
			t.Error("a locked session wrote to the core")
		}
		locked := person.get("/locked")
		if locked.Status != http.StatusOK || strings.Contains(locked.Body, captureWords) {
			t.Fatalf("GET /locked: %d, or the lock page shows the life log", locked.Status)
		}
		unlock := formOf(t, locked.Body, posting("/unlock"))
		wrong := person.submit(unlock, "/locked", url.Values{"pin": {"0000"}})
		if wrong.Status != http.StatusSeeOther || !strings.HasPrefix(wrong.location(), "/locked?failed=1") {
			t.Errorf("a wrong PIN: %d to %q, want 303 to /locked?failed=1", wrong.Status, wrong.location())
		}
		if p := person.get("/"); p.Status != http.StatusSeeOther {
			t.Errorf("GET / after a wrong PIN: %d, want it still locked", p.Status)
		}
		right := person.submit(unlock, "/locked", url.Values{"pin": {ownerPIN}})
		if right.Status != http.StatusSeeOther {
			t.Fatalf("the right PIN: %d, want 303", right.Status)
		}
		if p := person.follow(right); p.Status != http.StatusOK || !strings.Contains(p.Body, "/days/"+today+"/capture") {
			t.Errorf("Home after the right PIN: %d, want the page", p.Status)
		}
	})

	t.Run("an interrupt of lifelog ends both programs", func(t *testing.T) {
		if strings.Contains(s.stderr.String(), "started again in") {
			t.Errorf("a program ended and started again during the test:\n%s", s.stderr)
		}
		began := time.Now()
		if err := s.cmd.Process.Signal(syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-s.exited:
			s.stopped = true
			if err != nil {
				t.Errorf("lifelog ended with %v, want status 0", err)
			}
		case <-time.After(stopBound):
			_ = s.cmd.Process.Kill()
			t.Fatalf("lifelog did not end within %s of the interrupt:\n%s", stopBound, s.stderr)
		}
		t.Logf("both ended %s after the interrupt", time.Since(began).Round(time.Millisecond))
		log := s.stderr.String()
		t.Logf("the log of the server:\n%s", log)
		if !strings.Contains(log, "lifelog: with web: ended\n") || strings.Contains(log, "ended by the stop") {
			t.Errorf("web did not end cleanly on the interrupt:\n%s", log)
		}
		if !strings.Contains(log, "lifelog: stopped serving ") || strings.Contains(log, "with an error") {
			t.Errorf("lifelog did not stop cleanly:\n%s", log)
		}
		for _, secret := range []string{ownerToken, ownerPIN, captureWords} {
			if strings.Contains(log, secret) {
				t.Errorf("the log of the server holds %q: a token, a PIN or a body must not be in a log", secret)
			}
		}
		if proc, err := os.FindProcess(s.webPID); err == nil && proc.Signal(syscall.Signal(0)) == nil {
			t.Errorf("web (pid %d) still runs after lifelog ended", s.webPID)
		}
		for name, base := range map[string]string{"the door": s.door, "the core": s.core} {
			if conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), time.Second); err == nil {
				conn.Close()
				t.Errorf("%s still listens after lifelog ended", name)
			}
		}
	})
}
