package web_test

// The door and the lock of the browser. They are the tests of the public listener of the core (internal/api/public_test.go,
// login_log_test.go and lock_test.go) ported to web, with their cases and their assertions; the setup changes (a web
// in front of a real core, a clock of the test, a settings file in the state folder of web). Each test says which test
// of the core it is, and what is different, with its reason.
//
// Not ported: the CORS answers of TestPublicAllowedOrigins and the remote client of TestPublicRemoteClientParity. Web
// is the door of a browser. It is no API for another origin, and a program that sends the bearer header goes to the core.
// (A bearer header opens nothing here: TestABearerHeaderOpensNothing.)

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog-web/web"
	"lifelog/lifelogtest"
)

const (
	testPIN   = "4242"
	secretDay = "2031-01-01"
	secret    = "Zebra Secret Page" // a title that the lock page must never show
)

// request is a request of a client of web, answered by h in the process. A body is a form.
func request(h http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "192.0.2.10:7780" // what a client on another device sends
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

// setLock sets the lock of the browser through its own catalog, as the core forwards the action: lock_after minutes and
// the PIN. "" for both is off.
func setLock(t *testing.T, l *life, minutes, pin string) {
	t.Helper()
	form := url.Values{"lock_after": {minutes}, "pin": {pin}}
	if minutes == "" {
		form = url.Values{"lock_after": {"off"}}
	}
	rec := request(l.browser.Catalog(), http.MethodPost, "/settings", form.Encode(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("set the lock: %d %.300s", rec.Code, rec.Body.String())
	}
}

// lockFixture is a web with the lock set to minutes ("" is off), a page with a secret title in the core, and a browser
// that has logged in. The clock of web is f.l.clock.
type lockFixture struct {
	t      *testing.T
	l      *life
	h      http.Handler // web, without the cookie
	cookie string       // the Cookie header of the logged-in browser
}

func newLockFixture(t *testing.T, minutes string) *lockFixture {
	t.Helper()
	l := emptyLife(t)
	if minutes != "" {
		setLock(t, l, minutes, testPIN)
	}
	l.do(t, "/", "create-page", map[string]string{"title": secret, "body": "synthetic"})
	f := &lockFixture{t: t, l: l, h: l.browser}
	f.cookie = f.login(f.h)
	return f
}

// login makes a browser log in on h and returns its Cookie header.
func (f *lockFixture) login(h http.Handler) string {
	f.t.Helper()
	rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"})
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lifelog_session" {
			return c.Name + "=" + c.Value
		}
	}
	f.t.Fatalf("login set no session cookie: %d", rec.Code)
	return ""
}

// browse is a navigation of the owner's browser.
func (f *lockFixture) browse(path string, extra map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	h := map[string]string{"Accept": "text/html", "Cookie": f.cookie, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"}
	for k, v := range extra {
		if v == "" {
			delete(h, k)
		} else {
			h[k] = v
		}
	}
	return request(f.h, "GET", path, "", h)
}

// post is a form post of the owner's browser.
func (f *lockFixture) post(path, body string, extra map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	h := map[string]string{"Accept": "text/html", "Cookie": f.cookie, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"}
	for k, v := range extra {
		h[k] = v
	}
	return request(f.h, "POST", path, body, h)
}

// fetch is a script's GET from the page.
func (f *lockFixture) fetch(path string) *httptest.ResponseRecorder {
	f.t.Helper()
	return request(f.h, "GET", path, "", map[string]string{"Accept": "application/vnd.siren+json", "Cookie": f.cookie, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors"})
}

// isLocked probes the session with a script's fetch. The core answers its API with JSON. Web answers a page, so the
// probe reads the status and the first words of the page of the code locked.
func (f *lockFixture) isLocked() bool {
	f.t.Helper()
	rec := f.fetch("/days/" + secretDay)
	switch rec.Code {
	case 200:
		return false
	case 401:
		if !strings.Contains(rec.Body.String(), "Locked.") {
			f.t.Fatalf("401 without the words of the code locked: %.200s", rec.Body.String())
		}
		return true
	}
	f.t.Fatalf("probe: %d %.200s", rec.Code, rec.Body.String())
	return false
}

func (f *lockFixture) isEnded() bool {
	f.t.Helper()
	rec := f.browse("/", nil)
	return rec.Code == 303 && rec.Header().Get("Location") == "/login"
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	logs := &bytes.Buffer{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return logs
}

// Core test: TestPublicRequiresTheToken. Web takes no bearer header (nothing opens with it), the 401 has no
// WWW-Authenticate because no HTTP scheme opens the door, and the write with the token is a write with a session.
func TestTheDoorRequiresASession(t *testing.T) {
	l := emptyLife(t)
	h := l.browser
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"no credential", nil, 401},
		{"the token as a bearer", map[string]string{"Authorization": "Bearer " + testToken}, 401},
		{"the token as the old cookie", map[string]string{"Cookie": "lifelog_token=" + testToken}, 401},
		{"the token as a session cookie", map[string]string{"Cookie": "lifelog_session=" + testToken}, 401},
		{"a session", map[string]string{"Cookie": l.cookie}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request(h, "GET", "/days/2031-01-01", "", tc.headers)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want %d: %.200s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == 401 && (!strings.Contains(rec.Body.String(), "token required") || strings.Contains(rec.Body.String(), "2031-01-01")) {
				t.Fatalf("401 answer: %.300s", rec.Body.String())
			}
		})
	}
	// A write with a session persists; without it nothing is written.
	if rec := request(h, "POST", "/days/2031-01-01/capture", "text=denied", nil); rec.Code != 401 {
		t.Fatalf("unauthenticated write: %d", rec.Code)
	}
	if rec := request(h, "POST", "/days/2031-01-01/capture", "text=allowed", map[string]string{"Cookie": l.cookie}); rec.Code != 303 {
		t.Fatalf("authenticated write: %d %.200s", rec.Code, rec.Body.String())
	}
	day := l.do(t, "/days/2031-01-01", "capture", map[string]string{"text": "after"})
	page := must(t)(l.api.Get(t.Context(), href(day, "page")))
	if body, _ := page.Properties.(map[string]any)["body"].(string); !strings.Contains(body, "allowed") || strings.Contains(body, "denied") {
		t.Fatalf("body=%q", body)
	}
	// The Host of the request is not checked: the session is the defence.
	if rec := request(h, "GET", "/", "", map[string]string{"Cookie": l.cookie, "Host": "arbitrary.invalid:1"}); rec.Code != 200 {
		t.Fatalf("foreign Host with a session: %d", rec.Code)
	}
	// Absolute-form targets stay refused.
	req := httptest.NewRequest("GET", "http://arbitrary.invalid/", nil)
	req.URL.Host = "arbitrary.invalid"
	req.Header.Set("Cookie", l.cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("absolute-form target: %d", rec.Code)
	}
	// A browser without the cookie is sent to the login page, and only that page is served to it.
	rec = request(h, "GET", "/", "", map[string]string{"Accept": "text/html"})
	if rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Fatalf("browser without cookie: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = request(h, "GET", "/login", "", map[string]string{"Accept": "text/html"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `action="/login"`) || strings.Contains(rec.Body.String(), "2031-01-01") || strings.Contains(rec.Body.String(), "<dialog") {
		t.Fatalf("login page: %d %.300s", rec.Code, rec.Body.String())
	}
}

// Core test: TestABearerRequestNeverLocks. Web has no door for a program: the bearer header opens nothing, with or
// without the cookie beside it, so it cannot be a way round the lock.
func TestABearerHeaderOpensNothing(t *testing.T) {
	f := newLockFixture(t, "5")
	f.l.clock.add(10 * time.Hour)
	bearer := "Bearer " + testToken
	rec := request(f.h, "GET", "/days/"+secretDay, "", map[string]string{"Authorization": bearer})
	if rec.Code != 401 || strings.Contains(rec.Body.String(), secretDay+"</") {
		t.Errorf("bearer after ten idle hours: %d", rec.Code)
	}
	rec = request(f.h, "GET", "/days/"+secretDay, "", map[string]string{"Authorization": bearer, "Cookie": f.cookie})
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), "Locked.") {
		t.Errorf("bearer with a locked cookie: %d, want 401 locked", rec.Code)
	}
}

// Core test: TestPublicLoginCookie. The cookie is never Secure, because web serves plain HTTP (the core marks it Secure
// when it serves TLS itself).
func TestTheLoginSetsASessionCookie(t *testing.T) {
	l := emptyLife(t)
	h := l.browser
	rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Accept": "text/html", "Sec-Fetch-Site": "same-origin"})
	if rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lifelog_session" {
			cookie = c
		}
	}
	if cookie == nil || strings.Contains(cookie.Value, testToken) || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Secure || cookie.MaxAge != 365*24*60*60 { // one year: an installed app keeps the login
		t.Fatalf("cookie=%+v", cookie)
	}
	// The cookie opens the HTML face; a same-origin form writes; a cross-site form with the cookie is refused.
	with := map[string]string{"Accept": "text/html", "Cookie": cookie.Name + "=" + cookie.Value}
	if home := request(h, "GET", "/", "", with); home.Code != 200 || !strings.Contains(home.Body.String(), "Capture") { // Home is the page of today
		t.Fatalf("home with the cookie: %d", home.Code)
	}
	same := map[string]string{"Accept": "text/html", "Cookie": with["Cookie"], "Sec-Fetch-Site": "same-origin"}
	if rec := request(h, "POST", "/days/2031-01-02/capture", "text=form", same); rec.Code != 303 {
		t.Fatalf("same-origin form with the cookie: %d %.200s", rec.Code, rec.Body.String())
	}
	cross := map[string]string{"Accept": "text/html", "Cookie": with["Cookie"], "Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}
	if rec := request(h, "POST", "/days/2031-01-02/capture", "text=csrf", cross); rec.Code != 403 {
		t.Fatalf("cross-site form with the cookie: %d", rec.Code)
	}
	// A wrong token is sent back to the form; a cross-site login is refused; logout clears the cookie.
	if rec := request(h, "POST", "/login", "token=wrong", map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != 303 || rec.Header().Get("Location") != "/login?failed=1" || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("wrong login: %d %q cookies=%d", rec.Code, rec.Header().Get("Location"), len(rec.Result().Cookies()))
	}
	if rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}); rec.Code != 403 {
		t.Fatalf("cross-site login: %d", rec.Code)
	}
	rec = request(h, "POST", "/logout", "", map[string]string{"Cookie": with["Cookie"], "Sec-Fetch-Site": "same-origin"})
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == "lifelog_session" && c.MaxAge < 0)
	}
	if rec.Code != 303 || !cleared {
		t.Fatalf("logout: %d cleared=%v", rec.Code, cleared)
	}
}

// Core test: TestSessionCookie (the part about the old cookie).
func TestTheLoginClearsTheOldCookie(t *testing.T) {
	l := emptyLife(t)
	// The browser still has the cookie of an older login, which held the token itself.
	rec := request(l.browser, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "same-origin", "Cookie": "lifelog_token=" + testToken})
	var session, legacy *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "lifelog_session":
			session = c
		case "lifelog_token":
			legacy = c
		}
	}
	if session == nil || strings.Contains(session.Value, testToken) {
		t.Errorf("session cookie %+v", session)
	}
	if legacy == nil || legacy.MaxAge >= 0 {
		t.Errorf("the login does not clear the old cookie: %+v", legacy)
	}
}

// Core test: TestSessionCookieChecks.
func TestSessionCookieChecks(t *testing.T) {
	f := newLockFixture(t, "")
	value := strings.TrimPrefix(f.cookie, "lifelog_session=")
	id, mac, _ := strings.Cut(value, ".")
	flip := func(s string) string {
		if s[0] == '0' {
			return "1" + s[1:]
		}
		return "0" + s[1:]
	}
	for name, cookie := range map[string]string{
		"changed mac":     "lifelog_session=" + id + "." + flip(mac),
		"changed id":      "lifelog_session=" + flip(id) + "." + mac,
		"no mac":          "lifelog_session=" + id,
		"the token":       "lifelog_session=" + testToken,
		"the old cookie":  "lifelog_token=" + testToken,
		"nothing at all":  "",
		"an empty cookie": "lifelog_session=",
	} {
		rec := request(f.h, "GET", "/", "", map[string]string{"Accept": "text/html", "Cookie": cookie})
		if rec.Code != 303 || rec.Header().Get("Location") != "/login" {
			t.Errorf("%s: %d %q, want the login redirect", name, rec.Code, rec.Header().Get("Location"))
		}
		if rec := request(f.h, "GET", "/pages", "", map[string]string{"Cookie": cookie}); rec.Code != 401 || !strings.Contains(rec.Body.String(), "token required") {
			t.Errorf("%s: other client %d", name, rec.Code)
		}
	}
	// A new token ends every session: the same cookie meets a web with another token.
	other, err := web.New(web.Options{CoreURL: f.l.core.URL, Token: strings.Repeat("y", 40), StateDir: t.TempDir(), Now: f.l.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(other, "GET", "/", "", map[string]string{"Accept": "text/html", "Cookie": f.cookie}); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("a cookie of another token: %d", rec.Code)
	}
	// Logout ends the session: its cookie is dead from then on, even though the HMAC is right.
	if rec := f.post("/logout", "", nil); rec.Code != 303 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := f.browse("/", nil); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("an ended session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// Core test: TestSessionPagesAreNotCachedAndAssetsKeepTheirHeaders. The core's own handler keeps a preview for a year;
// the session of web makes it no-store.
func TestSessionPagesAreNotCachedAndAssetsKeepTheirHeaders(t *testing.T) {
	f := newLockFixture(t, "")
	if got := f.browse("/days/"+secretDay, nil).Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("a page of a session: Cache-Control %q", got)
	}
	rec := request(f.h, "GET", "/assets/chat.js", "", map[string]string{"Cookie": f.cookie})
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("an asset: %d Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	// A picture of the journal is not kept either, though its route asks for a long cache: a locked browser would show it
	// from the cache.
	photo := t.TempDir() + "/lake.jpg"
	if err := os.WriteFile(photo, syntheticJPEG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	file := f.l.do(t, "/", "add-file", map[string]string{"title": "lake.jpg", "day": secretDay, "original": photo})
	preview := href(file, "self") + "/preview"
	if core := request(f.l.core.Handler, "GET", preview, "", nil); !strings.Contains(core.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("the preview of the core: Cache-Control %q, want a long cache", core.Header().Get("Cache-Control"))
	}
	rec = request(f.h, "GET", preview, "", map[string]string{"Cookie": f.cookie})
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a preview of a session: %d Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

// Core test: TestTheLockPageKeepsThePathOnly. The lock page and its address hold the path of the page, never its query:
// a search's words are the owner's.
func TestTheLockPageKeepsThePathOnly(t *testing.T) {
	f := newLockFixture(t, "15")
	f.post("/lock", "", nil)
	rec := f.browse("/search?q=PurpleOctopus", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/locked?next="+url.QueryEscape("/search") {
		t.Errorf("a locked search: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// Core test: TestTheOldCookieIsClearedOnAnyRequest. The old cookie held the token in clear: any request that still
// carries it gets it cleared.
func TestTheOldCookieIsClearedOnAnyRequest(t *testing.T) {
	f := newLockFixture(t, "")
	rec := request(f.h, "GET", "/", "", map[string]string{"Accept": "text/html", "Cookie": "lifelog_token=" + testToken + "; " + f.cookie})
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == "lifelog_token" && c.MaxAge < 0)
	}
	if !cleared {
		t.Errorf("the old cookie is not cleared: %d %v", rec.Code, rec.Header().Values("Set-Cookie"))
	}
}

// Core test: TestAWrongLoginLeavesOneLogLine. A wrong token on /login is sent back to the form, and the owner can see it
// in the log: one line with the route. The line never has the token that was sent, and a right token leaves no line.
func TestAWrongLoginLeavesOneLogLine(t *testing.T) {
	l := emptyLife(t)
	logged := captureLog(t) // no test of this package runs in parallel
	const wrong = "synthetic-wrong-token-9876543210"
	rec := request(l.browser, "POST", "/login", "token="+url.QueryEscape(wrong), map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != 303 || rec.Header().Get("Location") != "/login?failed=1" {
		t.Fatalf("wrong login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	out := logged.String()
	if n := strings.Count(out, "\n"); n != 1 || !strings.Contains(out, "POST /login") {
		t.Errorf("want one line about POST /login, got %q", out)
	}
	if strings.Contains(out, wrong) || strings.Contains(out, testToken) {
		t.Errorf("the log has a token: %q", out)
	}
	logged.Reset()
	if rec := request(l.browser, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("right login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if logged.Len() != 0 {
		t.Errorf("a right login was logged: %q", logged.String())
	}
}

// Core test: TestPublicAllowedOrigins, the guard part. An origin that the owner lists writes; any other is refused, also
// with a session. No CORS headers are sent (see the head of this file).
func TestAnOriginThatIsListedMayWrite(t *testing.T) {
	const proxy = "https://lifelog.example"
	l := emptyLife(t)
	b, err := web.New(web.Options{CoreURL: l.core.URL, Token: testToken, StateDir: t.TempDir(), Origins: []string{proxy}, Now: l.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	cookie := login(t, b)
	post := func(origin string) *httptest.ResponseRecorder {
		return request(b, "POST", "/days/2031-01-02/capture", "text=from+the+proxy",
			map[string]string{"Cookie": cookie, "Origin": origin, "Sec-Fetch-Site": "cross-site", "Accept": "text/html"})
	}
	if rec := post(proxy); rec.Code != 303 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("write from the listed origin: %d, %v", rec.Code, rec.Header())
	}
	if rec := post("https://evil.example"); rec.Code != 403 || !strings.Contains(rec.Body.String(), "cross-origin") {
		t.Errorf("write from another origin: %d", rec.Code)
	}
	// A program that sends no browser headers is not a cross-origin write.
	if rec := request(b, "POST", "/days/2031-01-02/capture", "text=again", map[string]string{"Cookie": cookie}); rec.Code != 303 {
		t.Errorf("write without browser headers: %d", rec.Code)
	}
}

// Core test: TestPublicOptionsValidation (the token and the origins).
func TestNewRefusesABadTokenOrOrigin(t *testing.T) {
	options := func(token string, origins ...string) web.Options {
		return web.Options{CoreURL: "http://127.0.0.1:7777", Token: token, Origins: origins, StateDir: t.TempDir()}
	}
	for _, token := range []string{"", "short", strings.Repeat("x", 31), testToken + "\n", "line one line two\nline three abcdefghijklmnop"} {
		if _, err := web.New(options(token)); err == nil {
			t.Errorf("token %q accepted", token)
		}
	}
	for _, origin := range []string{"", "app.example", "https://app.example/", "https://app.example/path", "https://user@app.example", "ftp://app.example", "https://app.example?x=1", "https://", "http://App.Example", "HTTP://app.example"} {
		if _, err := web.New(options(testToken, origin)); err == nil {
			t.Errorf("origin %q accepted", origin)
		}
	}
	for _, origin := range []string{"https://app.example", "http://localhost:3000", "http://192.168.1.20:8080", "http://[::1]:3000"} {
		if _, err := web.New(options(testToken, origin)); err != nil {
			t.Errorf("origin %q refused: %v", origin, err)
		}
	}
	for _, origin := range []string{"http://App.Example", "HTTP://app.example"} {
		if _, err := web.New(options(testToken, origin)); err == nil || !strings.Contains(err.Error(), "lower case") {
			t.Errorf("origin %q: %v, want the words lower case", origin, err)
		}
	}
}

// Core test: TestHeldUnlocksGetNoMoreThanFiveTries. Requests that the program holds while their bodies come (or that come
// at once) get no more tries than the limit: a try is counted before the PIN is compared, and an ended session compares
// no PIN.
func TestHeldUnlocksGetNoMoreThanFiveTries(t *testing.T) {
	logs := captureLog(t)
	f := newLockFixture(t, "15")
	f.post("/lock", "", nil)
	pins := []string{"1001", "1002", "1003", "1004", "1005", "1006", "1007", "1008", testPIN}
	type held struct {
		body *io.PipeWriter
		done chan *httptest.ResponseRecorder
	}
	var all []held
	for range pins {
		r, w := io.Pipe()
		req := httptest.NewRequest("POST", "/unlock", r)
		for k, v := range map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Cookie": f.cookie, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"} {
			req.Header.Set(k, v)
		}
		h := held{w, make(chan *httptest.ResponseRecorder, 1)}
		go func() { rec := httptest.NewRecorder(); f.h.ServeHTTP(rec, req); h.done <- rec }()
		all = append(all, h)
	}
	// Every request has passed the check of its cookie and waits for its body. The bodies come one by one.
	var last *httptest.ResponseRecorder
	for i, h := range all {
		if _, err := io.WriteString(h.body, "pin="+pins[i]); err != nil {
			t.Fatal(err)
		}
		h.body.Close()
		last = <-h.done
	}
	if got := strings.Count(logs.String(), "the PIN does not match"); got > 5 {
		t.Errorf("%d PINs were compared, want at most 5:\n%s", got, logs.String())
	}
	if loc := last.Header().Get("Location"); last.Code == 303 && loc == "/" {
		t.Errorf("the right PIN opened the session after %d wrong ones", len(pins)-1)
	}
	if !f.isEnded() {
		t.Error("the session is not ended")
	}
}

// Core test: TestLockAfterTheIdleMinutes.
func TestLockAfterTheIdleMinutes(t *testing.T) {
	f := newLockFixture(t, "5")
	f.l.clock.add(6 * time.Minute) // lock_after plus one minute, to the second: still open
	if f.isLocked() {
		t.Fatal("locked at lock_after plus one minute")
	}
	f.l.clock.add(6*time.Minute + time.Second)
	rec := f.browse("/days/"+secretDay+"?x=1", nil)
	if want := "/locked?next=" + url.QueryEscape("/days/"+secretDay); rec.Code != 303 || rec.Header().Get("Location") != want {
		t.Fatalf("idle session: %d %q, want %q", rec.Code, rec.Header().Get("Location"), want)
	}
	if !f.isLocked() {
		t.Error("an idle session is open to a script")
	}
	// A locked session stays locked: a navigation does not open it.
	f.l.clock.add(time.Second)
	if rec := f.browse("/", nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/locked") {
		t.Errorf("navigation of a locked session: %d", rec.Code)
	}
}

// Core test: TestOnlyTheOwnersActivityKeepsTheSessionOpen. Each case: one request at four minutes, then a probe at seven
// minutes. The session is open at seven minutes only if the request counted as activity (the limit is six).
func TestOnlyTheOwnersActivityKeepsTheSessionOpen(t *testing.T) {
	for _, tc := range []struct {
		name  string
		do    func(f *lockFixture) *httptest.ResponseRecorder
		count bool
	}{
		{"navigation", func(f *lockFixture) *httptest.ResponseRecorder { return f.browse("/days/"+secretDay, nil) }, true},
		{"form post", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.post("/days/"+secretDay+"/capture", "text=synthetic", nil)
		}, true},
		{"awake", func(f *lockFixture) *httptest.ResponseRecorder { return f.post("/awake", "", nil) }, true},
		{"old browser navigation", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.browse("/days/"+secretDay, map[string]string{"Sec-Fetch-Mode": "", "Sec-Fetch-Site": ""})
		}, true},
		{"fetch", func(f *lockFixture) *httptest.ResponseRecorder { return f.fetch("/days/" + secretDay) }, false},
		{"prerender", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.browse("/days/"+secretDay, map[string]string{"Sec-Purpose": "prefetch;prerender"})
		}, false},
		{"fetch post", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.post("/days/"+secretDay+"/capture", "text=synthetic", map[string]string{"Sec-Fetch-Mode": "same-origin"})
		}, false},
		{"script poll with html", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.browse("/days/"+secretDay, map[string]string{"Sec-Fetch-Mode": "cors"})
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLockFixture(t, "5")
			f.l.clock.add(4 * time.Minute)
			if rec := tc.do(f); rec.Code >= 400 {
				t.Fatalf("the request failed: %d %.200s", rec.Code, rec.Body.String())
			}
			f.l.clock.add(3 * time.Minute)
			if locked := f.isLocked(); locked == tc.count {
				t.Errorf("locked at seven minutes = %v, but counts as activity = %v", locked, tc.count)
			}
		})
	}
}

// Core test: TestLockNowAndTheLockPage.
func TestLockNowAndTheLockPage(t *testing.T) {
	f := newLockFixture(t, "15")
	rec := f.post("/lock", "", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/locked" {
		t.Fatalf("lock: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := f.browse("/days/"+secretDay, nil); rec.Code != 303 || rec.Header().Get("Location") != "/locked?next="+url.QueryEscape("/days/"+secretDay) {
		t.Errorf("HTML GET: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if !f.isLocked() {
		t.Error("a script is let in")
	}
	if rec := f.post("/days/"+secretDay+"/capture", "text=late", nil); rec.Code != 401 || !strings.Contains(rec.Body.String(), "Locked.") {
		t.Errorf("form post: %d %.300s", rec.Code, rec.Body.String())
	}
	if rec := f.post("/awake", "", nil); rec.Code != 401 {
		t.Errorf("awake of a locked session: %d", rec.Code)
	}
	page := f.browse("/locked?next="+url.QueryEscape("/days/"+secretDay), nil)
	body := page.Body.String()
	if page.Code != 200 || page.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, `name="pin"`) || !strings.Contains(body, `value="/days/`+secretDay+`"`) {
		t.Fatalf("the lock page: %d %q %.300s", page.Code, page.Header().Get("Cache-Control"), body)
	}
	for _, leak := range []string{secret, "<dialog", `id="chat"`} {
		if strings.Contains(body, leak) {
			t.Errorf("the lock page holds %q", leak)
		}
	}
	// next goes to another origin: dropped.
	for _, next := range []string{"//evil.example/x", `/\evil.example`, "https://evil.example/", "/\t/evil.example", "evil", "/a\nb"} {
		body := f.browse("/locked?next="+url.QueryEscape(next), nil).Body.String()
		if !strings.Contains(body, `name="next" value=""`) {
			t.Errorf("next %q was kept", next)
		}
	}
}

// Core test: TestUnlock.
func TestUnlock(t *testing.T) {
	logs := captureLog(t)
	f := newLockFixture(t, "15")
	f.post("/lock", "", nil)
	next := url.QueryEscape("/days/" + secretDay)
	// A wrong PIN goes back with the page to return to; the right one opens and goes there.
	rec := f.post("/unlock", "pin=1111&next="+next, nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/locked?failed=1&next="+next {
		t.Fatalf("wrong PIN: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if body := f.browse("/locked?failed=1", nil).Body.String(); !strings.Contains(body, "Refused.") {
		t.Error("the lock page does not say that the PIN was refused")
	}
	rec = f.post("/unlock", "pin="+testPIN+"&next="+next, nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/days/"+secretDay {
		t.Fatalf("right PIN: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if f.isLocked() {
		t.Error("still locked after the right PIN")
	}
	// The unlock gave the count back: four more wrong PINs do not end the session.
	f.post("/lock", "", nil)
	for range 4 {
		f.post("/unlock", "pin=1111", nil)
	}
	if rec := f.post("/unlock", "pin="+testPIN+"&next=//evil.example", nil); rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("right PIN, foreign next: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// The fifth wrong PIN in a row ends the session and clears the cookie.
	f.post("/lock", "", nil)
	for i := range 4 {
		if rec := f.post("/unlock", "pin=2222", nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/locked") {
			t.Fatalf("wrong PIN %d: %d %q", i+1, rec.Code, rec.Header().Get("Location"))
		}
	}
	rec = f.post("/unlock", "pin=2222", nil)
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == "lifelog_session" && c.MaxAge < 0)
	}
	if rec.Code != 303 || rec.Header().Get("Location") != "/login" || !cleared {
		t.Fatalf("fifth wrong PIN: %d %q cleared=%v", rec.Code, rec.Header().Get("Location"), cleared)
	}
	// The right PIN no longer helps: the session is ended.
	if rec := f.post("/unlock", "pin="+testPIN, nil); rec.Code != 401 {
		t.Errorf("unlock of an ended session: %d", rec.Code)
	}
	if rec := f.browse("/", nil); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("an ended session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, pin := range []string{"1111", "2222", testPIN} {
		if strings.Contains(logs.String(), pin) {
			t.Errorf("the log holds the PIN %s", pin)
		}
	}
	if got := strings.Count(logs.String(), "the PIN does not match"); got != 10 {
		t.Errorf("%d log lines for wrong PINs, want 10:\n%s", got, logs.String())
	}
}

// Core test: TestALockKeepsTheCountOfWrongPINs. The Lock button does not give the tries back: four wrong PINs, a lock,
// and the next wrong PIN ends the session.
func TestALockKeepsTheCountOfWrongPINs(t *testing.T) {
	f := newLockFixture(t, "15")
	f.post("/lock", "", nil)
	for range 4 {
		f.post("/unlock", "pin=1111", nil)
	}
	if rec := f.post("/lock", "", nil); rec.Code != 303 {
		t.Fatalf("lock of a locked session: %d", rec.Code)
	}
	if rec := f.post("/unlock", "pin=1111", nil); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("fifth wrong PIN after a lock: %d %q, want the end of the session", rec.Code, rec.Header().Get("Location"))
	}
}

// Core test: TestARestartLocksEverySession. A new web over the same state folder does not know the sessions.
func TestARestartLocksEverySession(t *testing.T) {
	f := newLockFixture(t, "15")
	if f.isLocked() {
		t.Fatal("locked at once")
	}
	restarted, err := web.New(web.Options{CoreURL: f.l.core.URL, Token: testToken, StateDir: f.l.state, Now: f.l.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	f.h = restarted
	if !f.isLocked() {
		t.Fatal("a session that the new web does not know is open")
	}
	if rec := f.post("/unlock", "pin="+testPIN, nil); rec.Code != 303 {
		t.Fatalf("unlock: %d", rec.Code)
	}
	if f.isLocked() {
		t.Error("the PIN did not open the session")
	}
}

// Core test: TestWithTheLockOffNothingLocks. A change of the settings is live at once, because the settings are in
// memory and the catalog is the only writer.
func TestWithTheLockOffNothingLocks(t *testing.T) {
	f := newLockFixture(t, "")
	f.l.clock.add(10 * time.Hour)
	if f.isLocked() {
		t.Error("locked with lock_after off")
	}
	rec := f.post("/lock", "", nil)
	if rec.Code != 409 {
		t.Errorf("/lock with the lock off: %d, want 409", rec.Code)
	}
	if f.isLocked() {
		t.Error("/lock locked a session with the lock off")
	}
	// lock_after without a PIN is off. The catalog refuses to store it, so the file is made by hand.
	if rec := f.post("/unlock", "pin=1111", nil); rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Errorf("/unlock with the lock off: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// A change is live at once: the owner turns the lock on, and the idle session is locked.
	f.l.clock.add(10 * time.Hour)
	setLock(t, f.l, "5", testPIN)
	if !f.isLocked() {
		t.Error("an idle session is open after the lock was turned on")
	}
}

// Core test: TestAnUnreadableLockSettingLocksTheBrowser. With the settings in memory, a file that cannot be read can only
// be met at the start. Then the program does not start: the lock would have to guess what the file says.
func TestSettingsThatCannotBeReadStopTheStart(t *testing.T) {
	core := lifelogtest.New(t, today)
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/settings.json", []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := web.New(web.Options{CoreURL: core.URL, Token: testToken, StateDir: dir}); err == nil || !strings.Contains(err.Error(), "settings") {
		t.Errorf("New with a settings file that is not JSON: %v", err)
	}
	if err := os.WriteFile(dir+"/settings.json", []byte(`{"lock_after":"15"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := web.New(web.Options{CoreURL: core.URL, Token: testToken, StateDir: dir})
	if err != nil {
		t.Fatalf("New with lock_after and no PIN: %v", err)
	}
	// lock_after without a PIN is off: nothing could open the lock.
	cookie := login(t, b)
	if rec := request(b, "GET", "/days/"+secretDay, "", map[string]string{"Cookie": cookie, "Accept": "text/html"}); rec.Code != 200 || strings.Contains(rec.Body.String(), "lock.js") {
		t.Errorf("lock_after with no PIN: %d, the lock must be off", rec.Code)
	}
}

// Core test: TestTheOriginGuardHoldsForTheLockRoutes.
func TestTheOriginGuardHoldsForTheLockRoutes(t *testing.T) {
	f := newLockFixture(t, "15")
	cross := map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}
	for _, path := range []string{"/lock", "/unlock", "/awake", "/logout", "/login"} {
		if rec := f.post(path, "pin="+testPIN, cross); rec.Code != 403 {
			t.Errorf("cross-site POST %s: %d", path, rec.Code)
		}
	}
	if f.isLocked() {
		t.Error("a cross-site POST /lock locked the session")
	}
	if f.isEnded() {
		t.Error("a cross-site POST /logout ended the session")
	}
}

// Core test: TestThePageDrawsTheLockOnlyWhenTheLockIsOn. The Lock button, its access key, the script with the minutes and
// the key in the two lists are all there with the lock on, and none of them with it off.
func TestThePageDrawsTheLockOnlyWhenTheLockIsOn(t *testing.T) {
	marks := []string{
		`<form class="rail-lock" method="POST" action="/lock">`, `accesskey="l"`,
		`<script src="/assets/lock.js" data-minutes="5" defer></script>`,
		`<kbd>l</kbd> lock`,
	}
	on := newLockFixture(t, "5").browse("/days/"+secretDay, nil)
	if on.Code != 200 {
		t.Fatalf("page with the lock on: %d", on.Code)
	}
	for _, m := range marks {
		if !strings.Contains(on.Body.String(), m) {
			t.Errorf("lock on: the page lacks %s", m)
		}
	}
	if n := strings.Count(on.Body.String(), `<kbd>l</kbd> lock`); n != 2 {
		t.Errorf("lock on: %d lists of access keys have l, want 2 (the footer and the search palette)", n)
	}
	off := newLockFixture(t, "").browse("/days/"+secretDay, nil)
	for _, m := range []string{`action="/lock"`, `accesskey="l"`, "lock.js", `<kbd>l</kbd>`} {
		if strings.Contains(off.Body.String(), m) {
			t.Errorf("lock off: the page holds %s", m)
		}
	}
}

// Core test: TestLockJSIsServedLikeChatJS.
func TestLockJSIsServedLikeChatJS(t *testing.T) {
	f := newLockFixture(t, "5")
	rec := request(f.h, "GET", "/assets/lock.js", "", map[string]string{"Cookie": f.cookie})
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") ||
		rec.Header().Get("Cache-Control") != "public, max-age=300" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("GET /assets/lock.js: %d %v", rec.Code, rec.Header())
	}
	for _, want := range []string{"/awake", "/lock", "localStorage", "prerendering"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("lock.js does not mention %s", want)
		}
	}
}

// Core test: TestOnlyTheLoginFieldOffersTheSavedLogin. A browser fills an empty password field with the saved login, and
// the settings form then sends the token as the PIN. Every password field of the settings page says new-password; the
// login form keeps current-password. The page has the field of the core and the field of this browser.
func TestOnlyTheLoginFieldOffersTheSavedLogin(t *testing.T) {
	f := newLockFixture(t, "5")
	f.l.core.Connect(t, "web", f.l.browser.Catalog())
	page := f.browse("/settings", nil).Body.String()
	pw := regexp.MustCompile(`<input type="password"[^>]*>`).FindAllString(page, -1)
	if len(pw) != 3 {
		t.Fatalf("the settings page has %d password fields, want model_key, lock_pin and pin", len(pw))
	}
	for _, in := range pw {
		if !strings.Contains(in, `autocomplete="new-password"`) {
			t.Errorf("a password field of the settings form may take the saved login: %s", in)
		}
	}
	login := request(f.h, "GET", "/login", "", map[string]string{"Accept": "text/html"}).Body.String()
	if !strings.Contains(login, `name="token" required autocomplete="current-password"`) {
		t.Error("the login field lost current-password")
	}
}
