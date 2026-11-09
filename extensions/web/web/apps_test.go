package web_test

// The pages of the connected apps, as web draws them over the real core. The core merges an app:
// it puts the actions of the app in its own namespace and forwards the routes under /with/NAME. Web draws what the core
// answers. The view tests hold the pieces drawn from a hand-made entity; these hold the whole path.

import (
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"aminte/app"
	"aminte/siren"
)

// itemsAppHandler is a small connected app: a home with the title Test app, one item as a row with a chip and a round
// button, and the action add-item. done counts the calls of done-item. Each action answers with the home, which has a
// self link, so that the core sends the Location of the home (app.Catalog). The tests read pages, so add-item adds nothing.
func itemsAppHandler(done *atomic.Int32) http.Handler {
	add := siren.Action{Name: "add-item", Class: []string{"create"}, Method: http.MethodPost, Href: "/items", Title: "Add an item", Description: "Add an item with a text.",
		Fields: []siren.Field{{Name: "text", Type: "text", Title: "Text", Required: true}}}
	mark := siren.Action{Name: "done-item", Class: []string{"toggle"}, Method: http.MethodPost, Href: "/items/{id}/done", Title: "Done", Description: "Mark an item done.",
		Fields: []siren.Field{{Name: "id", Type: "number", Title: "Item", Required: true, In: "path"}}}
	home := func(*http.Request) (*siren.Entity, error) {
		row := siren.Link{Rel: []string{"item"}, Href: "/items/1", Title: "milk", Properties: map[string]any{"n": 1},
			Actions: []siren.Action{{Name: "done-item", Class: []string{"toggle"}, Method: http.MethodPost, Href: "/items/1/done", Title: "Done"}}}
		return &siren.Entity{Class: []string{"testapp"}, Title: "Test app", Properties: map[string]any{"done": int(done.Load())},
			Links: []siren.Link{{Rel: []string{"self"}, Href: "/", Title: "Test app"}, {Rel: []string{"collection"}, Href: "/", Title: "Items"}}, Entities: []siren.Link{row}, Actions: []siren.Action{add, mark}}, nil
	}
	return app.Catalog("Test app", home, []siren.Action{add, mark}, map[string]func(*http.Request) (*siren.Entity, error){
		"add-item":  home,
		"done-item": func(r *http.Request) (*siren.Entity, error) { done.Add(1); return home(r) },
	})
}

// TestAnAppPageIsDrawnWithTheRail: the home of a connected app is a page of the shell, with its title and its form. The
// link of the app is in the rail, marked on the page of the app and not marked on another page.
func TestAnAppPageIsDrawnWithTheRail(t *testing.T) {
	l := emptyLife(t)
	l.core.Connect(t, "testapp", itemsAppHandler(new(atomic.Int32)))
	rec := ask(l.web, http.MethodGet, "/with/testapp/")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Test app") || !strings.Contains(body, `action="/with/testapp/items"`) {
		t.Fatalf("%d: the page lacks the title or the form:\n%.1500s", rec.Code, body)
	}
	if !strings.Contains(body, `href="/with/testapp" aria-current="page"`) {
		t.Error("the rail has no current link to /with/testapp")
	}
	if other := pageAt(t, l.web, "/browse"); !strings.Contains(other, `href="/with/testapp"`) || strings.Contains(other, `href="/with/testapp" aria-current`) {
		t.Error("another page: the link is missing or marked")
	}
}

// TestTheBrowserDrawsTheRowsOfAnApp: an embedded link of an app carries a properties and an action. The core puts the
// action in its namespace, and the generic view draws the quick-add, the row with its round button and its chip, and
// the side column. The form of the row posts through /with/testapp, and the app runs the action.
func TestTheBrowserDrawsTheRowsOfAnApp(t *testing.T) {
	var done atomic.Int32
	l := emptyLife(t)
	l.core.Connect(t, "testapp", itemsAppHandler(&done))

	page := pageAt(t, l.web, "/with/testapp/")
	for _, want := range []string{
		`<form method="POST" action="/with/testapp/items/1/done" class="round">`, `aria-label="Done"`,
		`<span class="chip">n: 1</span>`, `<nav class="side" aria-label="Links">`, `aria-current="page"`, `class="quick-add"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not hold %q", want)
		}
	}
	if n := strings.Count(page, `action="/with/testapp/items"`); n != 1 {
		t.Errorf("add-item is drawn %d times, want once (the quick-add, not Actions again)", n)
	}
	if quick, row := strings.Index(page, `class="quick-add"`), strings.Index(page, `<li class="row">`); quick < 0 || row < 0 || quick > row {
		t.Errorf("the quick-add (at %d) is not before the first row (at %d) in the main column", quick, row)
	}

	form := regexp.MustCompile(`(?s)<form method="POST" action="/with/testapp/items/1/done" class="round">.*?</form>`).FindString(page)
	action, sent := sentByTheBrowser(t, form)
	rec := postForm(l.web, action, sent)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d, want 303", action, rec.Code)
	}
	if got := returnedPathOf(t, rec.Header().Get("Location")); got != "/with/testapp/" {
		t.Errorf("the form of the row returned to %q, want /with/testapp/", got)
	}
	if n := done.Load(); n != 1 {
		t.Errorf("the app ran done-item %d times, want once", n)
	}
}
