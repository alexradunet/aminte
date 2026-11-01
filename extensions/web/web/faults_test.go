package web_test

// The faults of web itself: a page that cannot be drawn, and a page that cannot be sent. The browser gets a page that
// names no cause. The log of the program holds the cause once. The log names the pattern of the route and never the path,
// because a path can name a person or a metric.
//
// The pattern of every page of web is "/": web has one route for each GET and one for each POST. So the log of web names
// "/" and the cause, and the tests hold that no word of the path is in the log.
//
// Web reads its entities from JSON, so a property that JSON cannot carry (a NaN, an infinity) cannot reach the view. The
// recover wrapper of web and its test are in recover_test.go.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// undrawableEntity is an error entity whose code is a number. The template of the error reads the code as words, so the
// view cannot be drawn. A correct core never sends it. Web must still survive a core that does.
const undrawableEntity = `{"class":["error"],"title":"Error","properties":{"code":5,"status":404,"message":"words"}}`

// undrawableCore is a web in front of a core that answers every thing with undrawableEntity, to a read and to a write.
func undrawableCore(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusOK, `{"class":["actions"],"actions":[]}`)
	})
	mux.HandleFunc("GET /things/{name}", func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusNotFound, undrawableEntity)
	})
	mux.HandleFunc("POST /things/{name}", func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusUnprocessableEntity, undrawableEntity)
	})
	return webOver(t, mux)
}

// The browser gets a whole page with the
// status 500 and no word of the cause. The log has the cause once, under the pattern of the route and not the path. A
// read and a write draw the answer of the core with the same call. The test holds both.
func TestAViewThatCannotBeDrawnGivesAWholePageThatNamesNoCause(t *testing.T) {
	h := undrawableCore(t)
	for name, send := range map[string]func() *httptest.ResponseRecorder{
		"a read": func() *httptest.ResponseRecorder { return ask(h, http.MethodGet, "/things/secret-name") },
		"a write": func() *httptest.ResponseRecorder {
			return postForm(h, "/things/secret-name", url.Values{"text": {"words"}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			logged := captureLog(t)
			rec := send()
			body := rec.Body.String()
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status %d, want 500", rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.HasSuffix(strings.TrimSpace(body), "</html>") ||
				!strings.Contains(body, "This page cannot be drawn.") {
				t.Errorf("the answer is not the whole page of a view that failed: %q %.300s", rec.Header().Get("Content-Type"), body)
			}
			for _, cause := range []string{"template:", "executing", "evaluate", "json:", "unsupported value", "wrong type"} {
				if strings.Contains(body, cause) {
					t.Errorf("the page names the cause (%q): %.300s", cause, body)
				}
			}
			log := logged.String()
			if n := strings.Count(log, "cannot be drawn"); n != 1 || !strings.Contains(log, "/: the view error cannot be drawn: ") {
				t.Errorf("the log does not have the cause one time under the pattern of the route: %q", log)
			}
			if !strings.Contains(log, "template:") {
				t.Errorf("the log does not have the cause: %q", log)
			}
			if strings.Contains(log, "secret-name") || strings.Contains(log, "things") {
				t.Errorf("the log names the path: %q", log)
			}
		})
	}
}

// goneClient is a client that went away: each write of a body fails.
type goneClient struct {
	header http.Header
	status int
}

func (g *goneClient) Header() http.Header       { return g.header }
func (g *goneClient) WriteHeader(status int)    { g.status = status }
func (g *goneClient) Write([]byte) (int, error) { return 0, errors.New("the client went away") }

// staleBodyForm is a form that saves the body of the page at self with a version that is no longer the version of the
// page: the core refuses it with 409 and web draws the page of the refusal.
func staleBodyForm(t *testing.T, l *life, self string) (path string, form url.Values) {
	t.Helper()
	save := act(t, must(t)(l.api.Get(t.Context(), self)), "save-body")
	version, _ := save.Field("version").Value.(string)
	if version == "" {
		t.Fatalf("the action save-body of %s has no version", self)
	}
	must(t)(l.api.Do(t.Context(), save, map[string]string{"body": "A newer body, saved first."}))
	return save.Href, url.Values{"body": {"my draft"}, "version": {version}}
}

// When the client went away, the write of the page fails. Nothing
// else can be done. The log has the fault once, with the cause, under the pattern of the route and not the path. The
// status and the type of the page were set before the write failed. A page of a read is sent with the same call.
func TestAPageThatCannotBeSentIsLoggedOnceUnderTheRouteAndNotThePath(t *testing.T) {
	l := richLife(t)
	path, form := staleBodyForm(t, l, l.hrefs["river"])
	for name, c := range map[string]struct {
		req    *http.Request
		status int
	}{
		"a refused write": {httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode())), http.StatusConflict},
		"a read":          {httptest.NewRequest(http.MethodGet, "/browse", nil), http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			logged := captureLog(t)
			c.req.Header.Set("Accept", "text/html,application/xhtml+xml")
			if c.req.Method == http.MethodPost {
				c.req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				c.req.Header.Set("Sec-Fetch-Site", "same-origin")
			}
			w := &goneClient{header: http.Header{}}
			l.web.ServeHTTP(w, c.req)
			if w.status != c.status || !strings.HasPrefix(w.header.Get("Content-Type"), "text/html") {
				t.Errorf("status %d %q, want the page with %d", w.status, w.header.Get("Content-Type"), c.status)
			}
			log := logged.String()
			if n := strings.Count(log, "the client went away"); n != 1 {
				t.Errorf("the log has the failed write %d times, want 1: %q", n, log)
			}
			if !strings.Contains(log, "/: the page was not sent: ") {
				t.Errorf("the log does not name the route and the fault: %q", log)
			}
			if strings.Contains(log, strings.TrimPrefix(c.req.URL.Path, "/")) && c.req.URL.Path != "/" {
				t.Errorf("the log names the path %s: %q", c.req.URL.Path, log)
			}
		})
	}
}

// TestPropertiesThatAreNotAnObjectAreAFaultOfTheCore: Siren says that properties is an object. A core or an app that sends a
// list, a string or a number sends an answer that is not an entity: the browser gets the 502 upstream page, whatever the
// status of the answer, for a read and for a write. The log has the fault once, under the pattern of the route, and holds
// none of the data. Properties that are absent or null, and an object, are drawn as before.
func TestPropertiesThatAreNotAnObjectAreAFaultOfTheCore(t *testing.T) {
	const secret = "secret-row"
	for name, c := range map[string]struct {
		status int
		body   string
		fault  bool // the answer is a fault of the core, not a page
	}{
		"a list":                      {http.StatusOK, `{"class":["thing"],"title":"A thing","properties":[{"name":"` + secret + `"}]}`, true},
		"a string":                    {http.StatusOK, `{"class":["thing"],"title":"A thing","properties":"` + secret + `"}`, true},
		"a number":                    {http.StatusOK, `{"class":["thing"],"title":"A thing","properties":5}`, true},
		"an error with a string":      {http.StatusNotFound, `{"class":["error"],"title":"Error","properties":"` + secret + `"}`, true},
		"an error with a list":        {http.StatusUnprocessableEntity, `{"class":["error"],"title":"Error","properties":["` + secret + `"]}`, true},
		"null properties":             {http.StatusOK, `{"class":["thing"],"title":"A thing","properties":null}`, false},
		"no properties":               {http.StatusOK, `{"class":["thing"],"title":"A thing"}`, false},
		"an object with a list in it": {http.StatusOK, `{"class":["thing"],"title":"A thing","properties":{"rows":[{"name":"` + secret + `"}]}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
				sirenAnswer(w, http.StatusOK, `{"class":["actions"],"actions":[]}`)
			})
			answer := func(w http.ResponseWriter, r *http.Request) { sirenAnswer(w, c.status, c.body) }
			mux.HandleFunc("GET /things/{name}", answer)
			mux.HandleFunc("POST /things/{name}", answer)
			h := webOver(t, mux)
			for how, send := range map[string]func() *httptest.ResponseRecorder{
				"a read": func() *httptest.ResponseRecorder { return ask(h, http.MethodGet, "/things/secret-name") },
				"a write": func() *httptest.ResponseRecorder {
					return postForm(h, "/things/secret-name", url.Values{"text": {"words"}})
				},
			} {
				logged := captureLog(t)
				rec := send()
				page, log := rec.Body.String(), logged.String()
				// The fake core has no root, so web also logs that it cannot read the shell: the fault is the line that names the answer.
				n := strings.Count(log, "/: the answer of the core cannot be read: ")
				if !c.fault {
					if rec.Code != c.status || !strings.Contains(page, "A thing") || n != 0 {
						t.Errorf("%s: status %d, log %q, want the page of the thing with %d and no fault in the log", how, rec.Code, log, c.status)
					}
					continue
				}
				if rec.Code != http.StatusBadGateway || !strings.Contains(page, "not an entity") || !strings.Contains(page, "upstream") {
					t.Errorf("%s: status %d, want the 502 page with the code upstream: %.400s", how, rec.Code, mainPartOf(page))
				}
				if n != 1 {
					t.Errorf("%s: the log has %q, want the fault once, under the pattern of the route", how, log)
				}
				for _, leak := range []string{secret, "secret-name"} {
					if strings.Contains(page, leak) || strings.Contains(log, leak) {
						t.Errorf("%s: the page or the log holds %q", how, leak)
					}
				}
			}
		})
	}
}
