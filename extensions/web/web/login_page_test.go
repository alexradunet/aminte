package web_test

// The login page, the rules of speculation and the ground. The ground is a setting of web (settings.go). A save goes to
// the catalog of web, not to the settings of the core.
//
// Web sends no Vary header. Every answer of a session is no-store (lock.go), so no cache holds two forms of one address.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"aminte/siren"
)

// The login page shows nothing of the life log. It is a page with no shell: a bare column, no sidebar, no script, no
// request for a prerender and no ground. A page of the shell has all four. The test checks this first, so that a wrong
// string cannot make the absences below true by accident.
func TestTheLoginPageIsABarePageAndAPageOfTheShellIsNot(t *testing.T) {
	l := emptyLife(t)
	shell := ask(l.web, http.MethodGet, "/browse")
	for _, want := range []string{`<div class="shell">`, `<aside class="rail">`, `<script src="/assets/chat.js" defer></script>`, `<body data-ground="boards">`} {
		if !strings.Contains(shell.Body.String(), want) {
			t.Errorf("a page of the shell lacks %s", want)
		}
	}
	if shell.Header().Get("Speculation-Rules") == "" {
		t.Error("a page of the shell does not ask for a prerender")
	}

	login := request(l.browser, http.MethodGet, "/login", "", map[string]string{"Accept": "text/html"}) // no cookie: it is the one page that needs none
	body := login.Body.String()
	if login.Code != http.StatusOK || !strings.HasPrefix(login.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, "<form") {
		t.Fatalf("GET /login: %d %q, want a page with a form", login.Code, login.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, `<div class="shell bare">`) || strings.Contains(body, `<aside class="rail">`) {
		t.Errorf("the login page is not a bare column, or it has the sidebar")
	}
	if strings.Contains(body, "<script") {
		t.Errorf("the login page has a script, want none")
	}
	if got := login.Header().Get("Speculation-Rules"); got != "" {
		t.Errorf("the login page asks for a prerender: Speculation-Rules = %q", got)
	}
	if !strings.Contains(body, "<body>") || strings.Contains(body, "<body data-ground") {
		t.Errorf("the login page carries a ground, want a plain body")
	}
}

// A page of the shell names the file of the rules (plan 149). An answer in Siren JSON does not. The file is JSON. Its
// first rule keeps the prerender away from an address with a query string (a receipt of the toast is read once, and
// ?new= opens a form) and from the assets.
func TestAShellPageAsksForPrerenderAndTheRulesKeepItAwayFromAQueryAndTheAssets(t *testing.T) {
	l := emptyLife(t)
	for _, uri := range []string{"/actions", "/", "/days/" + today} {
		if got := ask(l.web, http.MethodGet, uri).Header().Get("Speculation-Rules"); got != `"/assets/speculation.json"` {
			t.Errorf("GET %s: Speculation-Rules = %q, want the quoted address of the rules", uri, got)
		}
	}
	// The script of the page reads the entity as Siren JSON. That answer is no page, so it asks for nothing.
	entity := request(l.web, http.MethodGet, "/actions", "", map[string]string{"Accept": "application/vnd.siren+json"})
	if entity.Code != http.StatusOK || !strings.HasPrefix(entity.Header().Get("Content-Type"), "application/vnd.siren+json") {
		t.Fatalf("GET /actions as Siren: %d %q", entity.Code, entity.Header().Get("Content-Type"))
	}
	if got := entity.Header().Get("Speculation-Rules"); got != "" {
		t.Errorf("the Siren answer asks for a prerender: Speculation-Rules = %q, want none", got)
	}

	rules := request(l.web, http.MethodGet, "/assets/speculation.json", "", map[string]string{"Accept": "*/*"})
	if rules.Code != http.StatusOK || rules.Header().Get("Content-Type") != "application/speculationrules+json" {
		t.Fatalf("the rules: %d %q", rules.Code, rules.Header().Get("Content-Type"))
	}
	var doc struct {
		Prerender []json.RawMessage `json:"prerender"`
	}
	if err := json.Unmarshal(rules.Body.Bytes(), &doc); err != nil || len(doc.Prerender) == 0 {
		t.Fatalf("the rules are not JSON with a prerender rule: %v", err)
	}
	first := string(doc.Prerender[0])
	for _, want := range []string{`/*\\?*=*`, `/assets/*`} {
		if !strings.Contains(first, want) {
			t.Errorf("the first prerender rule does not exclude %s: %s", want, first)
		}
	}
}

// groundOnTheBody finds the ground in the body element of a page.
var groundOnTheBody = regexp.MustCompile(`<body data-ground="([^"]*)">`)

// A page names its ground on the body. With nothing set, the ground is boards. A save of embers shows on the next page
// and in the form of the settings. A ground outside the four is refused with 422, and the words name the field. The
// ground that was set stays. An empty ground clears the setting, and the page is on the boards again. The login page has
// no ground, whatever is set.
func TestThePageDrawsTheGroundOfTheBrowserSettings(t *testing.T) {
	l := emptyLife(t)
	l.core.Connect(t, "web", l.browser.Catalog()) // the form "This browser" is drawn from the catalog of the core
	ground := func() string {
		t.Helper()
		m := groundOnTheBody.FindStringSubmatch(ask(l.web, http.MethodGet, "/browse").Body.String())
		if m == nil {
			t.Fatal("a page of the shell has no ground on its body")
		}
		return m[1]
	}
	if !strings.Contains(ask(l.web, http.MethodGet, "/browse").Body.String(), `<body data-ground="boards">`) || ground() != "boards" {
		t.Errorf("a page with nothing set has no boards ground")
	}

	if rec := saveSettings(l, url.Values{"ground": {"embers"}}); rec.Code != http.StatusOK {
		t.Fatalf("save embers: %d %.300s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(ask(l.web, http.MethodGet, "/browse").Body.String(), `<body data-ground="embers">`) {
		t.Errorf("a page after a save of embers has the ground %q", ground())
	}
	form := ask(l.web, http.MethodGet, "/settings").Body.String()
	if !strings.Contains(form, `<option selected>embers</option>`) || strings.Contains(form, `<option selected>boards</option>`) {
		t.Errorf("the form of the settings does not hold embers")
	}

	refused := saveSettings(l, url.Values{"ground": {"flagstones"}})
	var e siren.Entity
	props := map[string]any{}
	if json.Unmarshal(refused.Body.Bytes(), &e) == nil {
		props, _ = e.Properties.(map[string]any)
	}
	if message, _ := props["message"].(string); refused.Code != http.StatusUnprocessableEntity || !strings.Contains(message, "ground") {
		t.Errorf("ground flagstones: %d %v, want 422 that names the field", refused.Code, props)
	}
	if got := ground(); got != "embers" || settingsFile(t, l)["ground"] != "embers" {
		t.Errorf("after a refused save the page has the ground %q and the file %q, want embers in both", got, settingsFile(t, l)["ground"])
	}

	login := request(l.browser, http.MethodGet, "/login", "", map[string]string{"Accept": "text/html"}).Body.String()
	if !strings.Contains(login, "<body>") || strings.Contains(login, "<body data-ground") {
		t.Errorf("the login page carries a ground")
	}

	if rec := saveSettings(l, url.Values{"ground": {""}}); rec.Code != http.StatusOK {
		t.Fatalf("save an empty ground: %d", rec.Code)
	}
	if got := ground(); got != "boards" {
		t.Errorf("after an empty save the page has the ground %q, want boards", got)
	}
}
