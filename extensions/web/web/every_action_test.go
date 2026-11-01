package web_test

// Every action that an address offers is drawn once. The expected answer is the JSON of the same address, read from the
// real core, and never a table of the views. The life is small and synthetic, made through the API of the core.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"lifelog/app"
	"lifelog/siren"
)

// actionsDrawnByLink names the actions that a page opens by a link and not by a form of its own: the picker of the cover
// (rel choose-cover) is the one place of set-cover.
var actionsDrawnByLink = map[string]string{"set-cover": "choose-cover"}

// mainPartOf is the part of a page between its main tags: what a failure of a test needs to show.
func mainPartOf(page string) string {
	_, rest, _ := strings.Cut(page, `<main id="main">`)
	rest, _, _ = strings.Cut(rest, "</main>")
	return rest
}

// checkActionsDrawnOnce holds the rule of the tests below for one address: body is the HTML and e the JSON of the same
// address. Each action of e is one form with its data-action, or one link that opens it, and the page draws no form of an
// action that e does not offer.
func checkActionsDrawnOnce(t *testing.T, path string, e *siren.Entity, body string) {
	t.Helper()
	offered := map[string]bool{}
	for _, a := range e.Actions {
		offered[a.Name] = true
		if rel := actionsDrawnByLink[a.Name]; rel != "" {
			if l := e.LinkByRel(rel); l == nil || strings.Count(body, `href="`+l.Href+`"`) != 1 {
				t.Errorf("%s: the link %s of %s is not drawn once", path, rel, a.Name)
			}
			continue
		}
		forms := strings.Count(body, `data-action="`+a.Name+`"`)
		links := strings.Count(body, `?new=`+a.Name)
		// A form is the one place. Only an action with no form is opened by a link, once (the quarters of a year link the
		// same create-page beside its form: they open it for a title of their own).
		if forms != 1 && !(forms == 0 && links == 1) {
			t.Errorf("%s: the action %s is drawn %d times as a form and %d as a link, want once", path, a.Name, forms, links)
		}
	}
	// Nothing is drawn that the entity does not offer.
	for _, m := range regexp.MustCompile(` data-action="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if !offered[m[1]] {
			t.Errorf("%s draws a form of %s, which the entity does not offer", path, m[1])
		}
	}
}

// TestEveryActionOfAnEntityIsDrawnOnce: for each kind of entity that web draws, the HTML of an address draws each action
// that the JSON of the same address offers exactly once: one form that carries data-action="NAME" (its domain place, or
// the menu, or the creates of the head), or one link that opens it. A template that places an action that the table of the
// views does not list, or that lists one it does not draw, fails here.
func TestEveryActionOfAnEntityIsDrawnOnce(t *testing.T) {
	l := lifeStartingOn(t, "2026-09-30")
	l.do(t, "/", "capture", map[string]string{"text": "Ran with [[Sam]] and [[Ana]]", "mood": "4"})
	ana := must(t)(l.api.Get(t.Context(), "/pages?title=Ana"))
	l.do(t, href(ana, "self"), "promote", map[string]string{"name": "Ana Popescu"})
	lisbon := l.do(t, "/", "create-page", map[string]string{"title": "Lisbon", "body": "A city."})
	l.do(t, href(lisbon, "self"), "file-pages", map[string]string{"category": "Cities"})
	cities := must(t)(l.api.Get(t.Context(), "/pages?title=Cities"))
	l.do(t, "/", "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	reading := l.do(t, "/", "record", map[string]string{"metric": "weight", "day": "2026-09-15", "value": "71.2"})
	l.do(t, "/", "add-file", map[string]string{"title": "Pic.png", "sha256": strings.Repeat("cd", 32), "mime": "image/png"})
	l.do(t, "/", "add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf"})
	scan := must(t)(l.api.Get(t.Context(), "/pages?title=Scan.pdf"))
	gone := l.do(t, "/", "create-page", map[string]string{"title": "Scratch"})
	l.do(t, href(gone, "self"), "tombstone-pages", nil)

	for _, row := range []struct{ name, path string }{
		{"a page", href(lisbon, "self")},
		{"a category page", href(cities, "self")},
		{"a person", href(ana, "self")},
		{"a file", href(scan, "self")},
		{"a metric and its series", "/metrics/weight"},
		{"a reading", href(reading, "self")},
		{"a deleted page", href(gone, "self")},
		{"the stream", "/days"},
		{"browse", "/browse"},
		{"browse, the people", "/browse?kind=person"},
		{"browse, a category", "/browse?in=Cities"},
		{"browse, the media", "/browse?kind=media"},
		{"the metrics", "/metrics"},
		{"a week", "/weeks/2026-W40"},
		{"a month", "/months/2026-09"},
		{"a year", "/years/2026"},
		{"the integrity page", "/integrity"},
		{"the settings", "/settings"},
		{"a search", "/search?q=ran"},
		{"the conversation of a day", "/days/today/conversation"},
	} {
		t.Run(row.name, func(t *testing.T) {
			e := must(t)(l.api.Get(t.Context(), row.path))
			checkActionsDrawnOnce(t, row.path, e, mainPartOf(pageAt(t, l.web, row.path)))
		})
	}
}

// fakeAppOfEveryAction is a connected app with a home that offers add-item, an owner action and a read of an item.
func fakeAppOfEveryAction() http.Handler {
	actions := []siren.Action{
		{Name: "add-item", Title: "Add an item", Method: "POST", Href: "/items", Type: "application/x-www-form-urlencoded",
			Fields: []siren.Field{{Name: "text", Type: "text", Title: "Text", Required: true}}},
		{Name: "reset", Title: "Reset", Method: "POST", Href: "/reset", Owner: true},
		{Name: "look", Title: "Look", Method: "GET", Href: "/items/{id}",
			Fields: []siren.Field{{Name: "id", Type: "number", Title: "Item", Required: true, In: "path"}}},
	}
	home := func(*http.Request) (*siren.Entity, error) {
		return &siren.Entity{Class: []string{"testapp"}, Title: "Test app", Properties: map[string]any{"count": 1},
			Entities: []siren.Link{{Rel: []string{"item"}, Href: "/items/1", Title: "First"}},
			Actions:  []siren.Action{{Name: "add-item", Title: "Add an item", Method: "POST", Href: "/items", Fields: []siren.Field{{Name: "text", Type: "text", Title: "Text", Required: true}}}}}, nil
	}
	answer := func(class, title string) func(*http.Request) (*siren.Entity, error) {
		return func(*http.Request) (*siren.Entity, error) {
			return &siren.Entity{Class: []string{class}, Title: title}, nil
		}
	}
	return app.Catalog("Test app", home, actions, map[string]func(*http.Request) (*siren.Entity, error){
		"add-item": answer("item", "Item 1"), "reset": answer("reset", "Reset"), "look": answer("item", "Item"),
	})
}

// fakeTaskServiceOfEveryAction answers the reads of the task protocol: two open tasks, and the first of them by its
// address. Its catalog has the class tasks and the six writes of the protocol.
func fakeTaskServiceOfEveryAction(t *testing.T) http.Handler {
	t.Helper()
	var writes []siren.Action
	for _, href := range []string{"/tasks", "/tasks/{id}", "/tasks/{id}/complete", "/tasks/{id}/reopen", "/tasks/{id}/delete", "/sync"} {
		writes = append(writes, siren.Action{Name: "w" + href, Method: "POST", Href: href})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := &siren.Entity{Class: []string{"tasks"}, Properties: map[string]any{"tasks": []map[string]any{}}}
		switch r.URL.Path {
		case "/actions":
			e = &siren.Entity{Class: []string{"actions", "tasks"}, Title: "Actions", Actions: writes}
		case "/lists":
			e.Properties = map[string]any{"lists": []map[string]string{{"id": "home", "name": "Home"}, {"id": "work", "name": "Work"}}}
		case "/open":
			e.Properties = map[string]any{"tasks": []map[string]any{
				{"id": "t1", "title": "Water the plants", "list": "home", "due": "2000-01-01"}, {"id": "t2", "title": "Call Ana", "list": "work"}}}
		case "/tasks/t1":
			e.Properties = map[string]any{"id": "t1", "title": "Water the plants", "list": "home", "due": "2000-01-01"}
		}
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		if err := json.NewEncoder(w).Encode(e); err != nil {
			t.Error(err)
		}
	})
}

// TestEveryActionOfAnAppAndOfTheTaskServiceIsDrawnOnce is the test above for the pages that a connected app and the task
// service send: the actions of their entities are drawn by class, and each of them once.
func TestEveryActionOfAnAppAndOfTheTaskServiceIsDrawnOnce(t *testing.T) {
	l := lifeStartingOn(t, "2026-09-30")
	l.core.Connect(t, "testapp", fakeAppOfEveryAction())
	l.core.Connect(t, "svc", fakeTaskServiceOfEveryAction(t))
	for _, path := range []string{"/with/testapp/", "/tasks", "/tasks/week", "/tasks/items/t1"} {
		t.Run(path, func(t *testing.T) {
			e := must(t)(l.api.Get(t.Context(), path))
			if len(e.Actions) == 0 {
				t.Fatalf("%s offers no action: the test proves nothing", path)
			}
			checkActionsDrawnOnce(t, path, e, mainPartOf(pageAt(t, l.web, path)))
		})
	}
}

// TestADayDrawsNoMenu: every action of a day has a place of its own (the capture box, the tiles, the editor, the cover
// link), so its menu is empty and draws no button.
func TestADayDrawsNoMenu(t *testing.T) {
	l := lifeStartingOn(t, "2026-09-30")
	l.do(t, "/", "capture", map[string]string{"text": "A line", "mood": "3"})
	l.do(t, "/", "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	for _, path := range []string{"/days/2026-09-30", "/days/2026-09-01"} {
		e := must(t)(l.api.Get(t.Context(), path))
		body := mainPartOf(pageAt(t, l.web, path))
		if strings.Contains(body, `aria-label="More actions"`) {
			t.Errorf("%s draws a menu button: a day has no action without a place", path)
		}
		for _, a := range e.Actions {
			if _, byLink := actionsDrawnByLink[a.Name]; !byLink && !strings.Contains(body, fmt.Sprintf(`data-action=%q`, a.Name)) {
				t.Errorf("%s does not draw the action %s", path, a.Name)
			}
		}
	}
}

// TestAnAskedFormIsNotInTheBackOfAForm: a form that the link of the page asked for (?new=NAME) is open on that page only.
// No form on it carries that query in its back, so another write does not open the dialog again.
func TestAnAskedFormIsNotInTheBackOfAForm(t *testing.T) {
	l := lifeStartingOn(t, "2026-09-30")
	lisbon := l.do(t, "/", "create-page", map[string]string{"title": "Lisbon", "body": "A city."})
	l.do(t, href(lisbon, "self"), "file-pages", map[string]string{"category": "Cities"})
	self := href(lisbon, "self")
	backs := regexp.MustCompile(`name="back" value="([^"]*)"`)
	for _, path := range []string{self + "?new=rename", self + "?new=tombstone-pages", self + "?new=file-pages", "/browse?in=Cities&new=file-pages"} {
		page := pageAt(t, l.web, path)
		found := backs.FindAllStringSubmatch(page, -1)
		if len(found) == 0 {
			t.Errorf("%s draws no form with a back: the test proves nothing", path)
		}
		for _, m := range found {
			if strings.Contains(m[1], "new=") {
				t.Errorf("%s: a form has the back %q, which asks for a form again", path, m[1])
			}
		}
	}
}

// TestEveryActionFormSendsToItsAction: a form drawn from an action sends to the href of the action with the method of the
// action (a box, a row with one button, the capture), and every POST form of the page of today sends to an action of that page.
// That every action of the catalog has its form on /actions is held by TestEveryActionOfTheCatalogHasItsFormInWeb.
func TestEveryActionFormSendsToItsAction(t *testing.T) {
	l := lifeStartingOn(t, today)
	l.do(t, "/", "register-metric", map[string]string{"name": "walk"})
	dayEntity := must(t)(l.api.Get(t.Context(), "/days/"+today))
	body := pageAt(t, l.web, "/days/"+today)
	posts := map[string]bool{}
	for _, a := range dayEntity.Actions {
		if a.Method == http.MethodPost {
			posts[a.Href] = true
		}
	}
	for _, name := range []string{"capture", "record"} {
		if a := act(t, dayEntity, name); !strings.Contains(body, `<form method="`+a.Method+`" action="`+a.Href+`"`) {
			t.Errorf("the page of today has no form of %s: %+v", name, a)
		}
	}
	// The chat column is the popover #chat, up to the end of its form and its hint: it posts to the day of today, and not to
	// an action of this day.
	chatColumn := regexp.MustCompile(`(?s)<div class="chat compact" id="chat".*?</form>\s*<p class="hint">.*?</p>\s*</div>`)
	besideChat := chatColumn.ReplaceAllString(body, "")
	if besideChat == body {
		t.Fatal("the page of today has no chat column: the test cuts nothing")
	}
	forms := regexp.MustCompile(`<form method="POST" action="([^"]*)"`).FindAllStringSubmatch(besideChat, -1)
	if len(forms) < 2 {
		t.Errorf("the page of today has %d POST forms beside the chat column, want the capture and the record at least", len(forms))
	}
	for _, m := range forms {
		if !posts[m[1]] {
			t.Errorf("a form of the page of today posts to %s, which is no action of that page", m[1])
		}
	}
}
