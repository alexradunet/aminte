package web_test

// The synthetic life that the tests of web read: a small file made through the API, as an owner and an agent would
// make it. The clock stands at the start of today, so every page is the same at every run.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aminte-web/web"
	"aminte/amintetest"
	"aminte/client"
	"aminte/siren"
)

const today = "2031-05-14"

// testToken is the owner's token of the tests: 36 characters of no meaning.
const testToken = "synthetic-token-synthetic-token-1234"

// life is an aminte server with a synthetic file, and web in front of it. web is a browser that has logged in: it sends
// its session cookie with each request. browser is the handler itself, for a test that needs no cookie, or another.
type life struct {
	core    *amintetest.Server
	api     *client.Client
	web     http.Handler
	browser *web.Browser
	cookie  string // the Cookie header of the logged-in browser
	clock   *clock
	state   string            // the state folder of web
	hrefs   map[string]string // the addresses of the things that the file holds, by a short name
	picture string            // the title of the file page that has a picture
}

// clock is the clock of web in the tests: it stands at 9 o'clock of the day, and a test moves it.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2031, 5, 14, 9, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newBrowser is a web over the core at coreURL, with its settings in the folder state.
func newBrowser(t *testing.T, coreURL string, c *clock, state string) *web.Browser {
	t.Helper()
	b, err := web.New(web.Options{CoreURL: coreURL, Token: testToken, StateDir: state, Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signedIn is a browser that has logged in to h: it adds its cookie to each request that has none.
type signedIn struct {
	h      http.Handler
	cookie string
}

func (s signedIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Cookie") == "" {
		r.Header.Set("Cookie", s.cookie)
	}
	s.h.ServeHTTP(w, r)
}

// login logs in to h with the token of the tests and returns the Cookie header of the browser.
func login(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"token": {testToken}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "aminte_session" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("the login set no session cookie: %d", rec.Code)
	return ""
}

// must returns the entity of a call of the client, or fails the test.
func must(t *testing.T) func(*siren.Entity, error) *siren.Entity {
	return func(e *siren.Entity, err error) *siren.Entity {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
}

// act is the action of e with the name, or it fails the test.
func act(t *testing.T, e *siren.Entity, name string) siren.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action %s in %v", name, e.Actions)
	return siren.Action{}
}

func href(e *siren.Entity, rel string) string {
	if l := e.LinkByRel(rel); l != nil {
		return l.Href
	}
	return ""
}

// emptyLife is a fresh file with web in front of it and nothing written.
func emptyLife(t *testing.T) *life {
	t.Helper()
	return lifeStartingOn(t, today)
}

// lifeStartingOn is emptyLife with the clock of the core at the start of day (YYYY-MM-DD): a test uses it when the words
// and the years of the page are those of that day. The clock of web stays as it is: web uses it for the sessions and the
// receipts only.
func lifeStartingOn(t *testing.T, day string) *life {
	t.Helper()
	core := amintetest.New(t, day)
	clk := newClock()
	state := t.TempDir()
	browser := newBrowser(t, core.URL, clk, state)
	cookie := login(t, browser)
	return &life{core: core, api: client.Remote(core.URL, "cli", ""), web: signedIn{browser, cookie}, browser: browser, cookie: cookie, clock: clk, state: state, hrefs: map[string]string{}}
}

// do runs the action of the resource at from.
func (l *life) do(t *testing.T, from, action string, values map[string]string) *siren.Entity {
	t.Helper()
	e := must(t)(l.api.Get(t.Context(), from))
	return must(t)(l.api.Do(t.Context(), act(t, e, action), values))
}

// earlierConversations makes the conversation pages of earlier days, each with the first line that say writes: [[DAY]].
// say is for today alone, and no write but a wikilink makes a page under a conversation title (API.md, the title of a
// conversation page). So the page "Earlier talks" links the titles, the save contract makes an empty plain page for each,
// and the owner writes the first line of each with save-body, as an owner edits the page of a conversation. The day bar
// of the conversation names these pages: a page that is live and plain is a conversation of its day.
func (l *life) earlierConversations(t *testing.T, days ...string) {
	t.Helper()
	var links strings.Builder
	for _, day := range days {
		fmt.Fprintf(&links, "[[Conversation %s]]\n", day)
	}
	l.do(t, "/", "create-page", map[string]string{"title": "Earlier talks", "body": links.String()})
	for _, day := range days {
		l.do(t, "/days/"+day+"/conversation", "save-body", map[string]string{"body": "[[" + day + "]]\n"})
	}
}

// toolLines are the tool lines that name each address, as an agent writes them (the field tools of reply, or the lines of
// a turn that a test writes into the page).
func toolLines(refs ...string) string {
	var b strings.Builder
	for i, ref := range refs {
		fmt.Fprintf(&b, "- `tool%d` → [%s](%s)\n", i+1, ref, ref)
	}
	return b.String()
}

// richLife is a file with every kind of thing a page of the shell shows: days with words, a mood, wikilinks and tags;
// a page, a person, a file with a picture; two metrics, one of them a habit, with readings; the conversation of today
// with a tool line that names things, and the conversations of two earlier days.
func richLife(t *testing.T) *life {
	t.Helper()
	l := emptyLife(t)
	sam := l.do(t, "/", "create-person", map[string]string{"title": "Sam", "name": "Samuel Ortiz"})
	river := l.do(t, "/", "create-page", map[string]string{"title": "River walk", "body": "A long path along the water.\nA heron lives there. #birds Walked with [[Sam]]."})
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Walk", "daily": "1"})
	for _, d := range []string{"2031-05-10", "2031-05-11", "2031-05-12", "2031-05-13", today} {
		l.do(t, "/days/"+d, "capture", map[string]string{"text": "Synthetic words of " + d + ". Met [[Sam]] at the [[River walk]]. #birds"})
	}
	l.do(t, "/days/"+today, "capture", map[string]string{"mood": "4"})
	l.do(t, "/days/2030-05-14", "capture", map[string]string{"text": "A year ago.", "mood": "5"})
	l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": "2031-05-12", "value": "70.4"})
	weight := l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": "2031-05-13", "value": "71.2"})
	l.do(t, "/", "record", map[string]string{"metric": "Walk", "day": today, "value": "1"})
	l.do(t, "/", "record", map[string]string{"metric": "Walk", "day": "2031-05-13", "value": "1"})

	l.picture = "Lake.jpg"
	photo := filepath.Join(t.TempDir(), "IMG_0001.JPG")
	if err := os.WriteFile(photo, syntheticJPEG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	lake := l.do(t, "/", "add-file", map[string]string{"title": l.picture, "body": "Dawn at the lake.", "day": today, "original": photo})

	l.earlierConversations(t, "2031-05-10", "2031-05-13") // the days of the day bar
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "How did I **sleep**? See [[River walk]]."})
	conv := must(t)(l.api.Get(t.Context(), "/days/"+today+"/conversation"))
	page := must(t)(l.api.Get(t.Context(), href(conv, "page")))
	body, _ := page.Properties.(map[string]any)["body"].(string)
	refs := []string{"/days/" + today, href(river, "self"), href(sam, "self"), "/metrics/Weight", "/search?q=heron", "/pages/999999", "/days/today"}
	more := "\n\n" + toolLines(refs...) + "\n**aminte** (09:30): You slept *well*.\n"
	must(t)(l.api.Do(t.Context(), act(t, page, "save-body"), map[string]string{"body": body + more}))

	l.hrefs["river"], l.hrefs["sam"] = href(river, "self"), href(sam, "self")
	l.hrefs["lake"] = href(lake, "self")
	l.hrefs["weightPage"] = pageOf(t, l, "Weight")
	l.hrefs["walkPage"] = pageOf(t, l, "Walk")
	l.hrefs["convPage"] = href(conv, "page")
	l.hrefs["reading"] = href(weight, "self")
	return l
}

// pageOf is the address of the page of the metric with the name: a series carries the properties of its page.
func pageOf(t *testing.T, l *life, name string) string {
	t.Helper()
	series := must(t)(l.api.Get(t.Context(), siren.MetricHref(name)))
	props, _ := series.Properties.(map[string]any)
	id, ok := props["id"].(json.Number)
	if !ok {
		t.Fatalf("the series %s has no page id: %v", name, props)
	}
	return "/pages/" + id.String()
}

// syntheticJPEG is a small JPEG with a gradient.
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

// ask is a GET of uri as a browser sends it, answered by h in the process.
func ask(h http.Handler, method, uri string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, uri, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// pageAt is the body of GET uri through h, as a browser asks for it. The test stops unless the answer is a whole page
// with the status 200, so that a test of the markup never reads the page of an error.
func pageAt(t *testing.T, h http.Handler, uri string) string {
	t.Helper()
	return pageWithStatus(t, h, uri, http.StatusOK)
}

// pageWithStatus is pageAt for a page that is not a 200: the page of a refusal. The test stops unless the answer has
// the status and is a whole page.
func pageWithStatus(t *testing.T, h http.Handler, uri string, status int) string {
	t.Helper()
	rec := ask(h, http.MethodGet, uri)
	body := rec.Body.String()
	if rec.Code != status || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("GET %s: %d, want %d and a whole page: %.500s", uri, rec.Code, status, body)
	}
	return body
}

// versionOf is the version of the page at href, as a form carries it.
func (l *life) versionOf(t *testing.T, href string) string {
	t.Helper()
	page := must(t)(l.api.Get(t.Context(), href))
	v, _ := page.Properties.(map[string]any)["version"].(string)
	if v == "" {
		t.Fatalf("the page %s has no version", href)
	}
	return v
}
