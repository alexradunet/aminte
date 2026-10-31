package web_test

// The parity of the browser (plan 161, stage 2). Web holds its own parity, as the table "Every action on every surface"
// of the README says for the surfaces other than the API and the CLI: each action of GET /actions, the actions of the core
// and the actions of a connected app, has its form in the pages that web draws. The test reads the catalog of the core,
// asks web for the page of every action (/actions), and reads the forms of the HTML as a browser reads them.

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"lifelog-web/internal/formdoc"
	"lifelog/siren"
)

// pathValues are synthetic values for the path fields of the actions. A path field that is not here fails the test, so a
// new kind of path field is a decision of the author and not a guess of the test.
var pathValues = map[string]string{"id": "7", "day": "2031-05-03"}

// TestEveryActionOfTheCatalogHasItsFormInWeb: for each action of the catalog of the core with the app web connected,
// /actions draws a form with the method and the address of the action. An action with a path field is drawn open, with its
// address filled in, on /actions?new=NAME&FIELD=VALUE, the way the pages link to it. No action is exempt: a browser reaches
// every one. An action that a page cannot draw as a form must be named here, with its reason, in the change that adds it.
func TestEveryActionOfTheCatalogHasItsFormInWeb(t *testing.T) {
	l := emptyLife(t)
	l.core.Connect(t, "web", l.browser.Catalog()) // the app, as `serve --with web` registers it: its action joins the catalog
	catalog := must(t)(l.api.Get(t.Context(), "/actions")).Actions
	if len(catalog) < 20 || !slices.ContainsFunc(catalog, func(a siren.Action) bool { return a.Name == "web-settings" }) {
		t.Fatalf("the catalog has %d actions and not web-settings: the test reads the wrong catalog", len(catalog))
	}
	every := ask(l.web, http.MethodGet, "/actions").Body.String()
	for _, f := range formdoc.Forms(every) {
		if strings.ContainsAny(f.Action, "{}") || strings.Contains(strings.ToLower(f.Action), "%7b") {
			t.Errorf("/actions draws a form that posts to a templated address: %s", f.Action)
		}
	}
	for _, a := range catalog {
		t.Run(a.Name, func(t *testing.T) {
			uri, want := "/actions", a.Href
			if strings.Contains(a.Href, "{") {
				q := url.Values{"new": {a.Name}}
				for _, f := range a.Fields {
					if f.In != "path" {
						continue
					}
					v, ok := pathValues[f.Name]
					if !ok {
						t.Fatalf("no synthetic value for the path field %s", f.Name)
					}
					q.Set(f.Name, v)
					want = strings.ReplaceAll(want, "{"+f.Name+"}", v)
				}
				uri += "?" + q.Encode()
			}
			rec := ask(l.web, http.MethodGet, uri)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d", uri, rec.Code)
			}
			forms := formdoc.Forms(rec.Body.String())
			if !slices.ContainsFunc(forms, func(f formdoc.Form) bool {
				action, _, _ := strings.Cut(f.Action, "?") // a form of a file keeps its back in the query
				return f.Method == a.Method && action == want
			}) {
				t.Errorf("GET %s draws no form %s %s (%d forms)", uri, a.Method, want, len(forms))
			}
		})
	}
}
