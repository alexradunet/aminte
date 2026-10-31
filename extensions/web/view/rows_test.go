package view

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/siren"
)

func drawApp(t *testing.T, e *siren.Entity, path string) string {
	t.Helper()
	b, err := Draw(e, Facts{URL: &url.URL{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func textField(name string, required bool) siren.Field {
	return siren.Field{Name: name, Type: "text", Title: name, Required: required}
}

// TestTheQuickAddIsTheFirstCreateAction: the first action of the class create, with exactly one required text field, is
// one line at the top, and it is not drawn again: the other action is in the menu.
func TestTheQuickAddIsTheFirstCreateAction(t *testing.T) {
	add := siren.Action{Name: "app-add", Method: "POST", Href: "/with/app/items", Title: "Add it", Class: []string{"create"},
		Fields: []siren.Field{textField("text", true), {Name: "due", Type: "date"}}}
	other := siren.Action{Name: "app-clear", Method: "POST", Href: "/with/app/clear", Title: "Clear"}
	page := drawApp(t, &siren.Entity{Title: "App", Actions: []siren.Action{other, add}}, "/with/app/")
	if n := strings.Count(page, `action="/with/app/items"`); n != 1 {
		t.Errorf("the add form is drawn %d times, want 1", n)
	}
	form := page[strings.Index(page, `<form`):]
	if !strings.Contains(page, `class="quick-add"`) || strings.Index(page, `class="quick-add"`) < strings.Index(page, `<div class="pane-main">`) {
		t.Errorf("the quick-add is not first:\n%s", form)
	}
	if !strings.Contains(page, `<details class="more-fields">`) || !strings.Contains(page, ">Add it</button>") {
		t.Errorf("the quick-add has no folded fields or no button:\n%s", form)
	}
	if !strings.Contains(page, `aria-label="More actions"`) || !strings.Contains(page, `<dialog class="menu-dialog" id="do-app-clear"`) || strings.Count(page, `data-action="app-add"`) != 1 {
		t.Errorf("the other action is not in the menu, or the quick-add is not drawn once:\n%s", form)
	}
}

func TestTheQuickAddNeedsTheClassCreateAndExactlyOneRequiredTextField(t *testing.T) {
	two := siren.Action{Name: "two", Method: "POST", Href: "/with/app/two", Title: "Two", Class: []string{"create"},
		Fields: []siren.Field{textField("a", true), textField("b", true)}}
	area := siren.Action{Name: "area", Method: "POST", Href: "/with/app/area", Title: "Area", Class: []string{"create"},
		Fields: []siren.Field{{Name: "body", Type: "textarea", Required: true}}}
	read := siren.Action{Name: "find", Method: "GET", Href: "/with/app/find", Title: "Find", Class: []string{"create"}, Fields: []siren.Field{textField("q", true)}}
	path := siren.Action{Name: "put", Method: "POST", Href: "/with/app/items/{id}", Title: "Put", Class: []string{"create"},
		Fields: []siren.Field{{Name: "id", Type: "number", Required: true, In: "path"}, textField("text", true)}}
	classless := siren.Action{Name: "classless", Method: "POST", Href: "/with/app/classless", Title: "Classless", Fields: []siren.Field{textField("text", true)}}
	edit := siren.Action{Name: "edit", Method: "POST", Href: "/with/app/edit", Title: "Edit", Class: []string{"edit"}, Fields: []siren.Field{textField("text", true)}}
	for _, a := range []siren.Action{two, area, read, path, classless, edit} {
		page := drawApp(t, &siren.Entity{Title: "App", Actions: []siren.Action{a}}, "/with/app/")
		if strings.Contains(page, `class="quick-add"`) {
			t.Errorf("%s: drawn as a quick-add", a.Name)
		}
		if !strings.Contains(page, `commandfor="do-`+a.Name+`"`) && !strings.Contains(page, `id="new-`+a.Name+`" popover`) {
			t.Errorf("%s: drawn neither in the menu nor as a create button", a.Name)
		}
	}
}

func TestACorePageHasNoQuickAdd(t *testing.T) {
	add := siren.Action{Name: "add", Method: "POST", Href: "/items", Title: "Add", Class: []string{"create"}, Fields: []siren.Field{textField("text", true)}}
	page := drawApp(t, &siren.Entity{Class: []string{"actions"}, Title: "Actions", Actions: []siren.Action{add}}, "/actions")
	if strings.Contains(page, `class="quick-add"`) {
		t.Error("a page of this server has a quick-add")
	}
}

func TestARowDrawsItsActionsAndChips(t *testing.T) {
	round := siren.Action{Name: "app-done", Method: "POST", Href: "/with/app/items/1/done", Title: "Mark done", Class: []string{"toggle"}}
	noClass := siren.Action{Name: "app-clear", Method: "POST", Href: "/with/app/items/1/clear", Title: "Clear it"}
	boxed := siren.Action{Name: "app-move", Method: "POST", Href: "/with/app/items/1/move", Title: "Move it",
		Fields: []siren.Field{textField("to", true)}}
	e := &siren.Entity{Title: "App", Entities: []siren.Link{
		{Rel: []string{"item"}, Href: "/with/app/items/1", Title: "Buy milk",
			Properties: map[string]any{"due": "2026-10-30", "n": 3, "flag": true, "off": false, "empty": "", "list": []any{1}},
			Actions:    []siren.Action{round, boxed, noClass}},
		{Rel: []string{"item"}, Href: "/with/app/items/2", Title: "Plain"},
	}}
	page := drawApp(t, e, "/with/app/")
	rows := regexp.MustCompile(`(?s)<li class="row">.*?</li>`).FindAllString(page, -1)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2:\n%s", len(rows), page)
	}
	r := rows[0]
	if !strings.Contains(r, `<form method="POST" action="/with/app/items/1/done" class="round">`) || !strings.Contains(r, `aria-label="Mark done"`) {
		t.Errorf("no round button with its aria-label:\n%s", r)
	}
	if strings.Index(r, `class="round"`) > strings.Index(r, "Buy milk") || strings.Index(r, "Buy milk") > strings.Index(r, `<details class="action">`) {
		t.Errorf("the order is not round button, title, boxed action:\n%s", r)
	}
	if strings.Count(r, `<details class="action">`) != 2 || !strings.Contains(r, `action="/with/app/items/1/move"`) || strings.Count(r, `class="round"`) != 1 {
		t.Errorf("the action with a field and the action with no class are not the two boxed forms:\n%s", r)
	}
	if strings.Contains(r, `action="/with/app/items/1/clear" class="round"`) || !strings.Contains(r, `action="/with/app/items/1/clear"`) {
		t.Errorf("an action with no class and no field is not boxed:\n%s", r)
	}
	for _, chip := range []string{"due: 2026-10-30", "flag: yes", "n: 3"} {
		if !strings.Contains(r, `<span class="chip">`+chip+`</span>`) {
			t.Errorf("no chip %q:\n%s", chip, r)
		}
	}
	for _, none := range []string{"off", "empty", "list"} {
		if strings.Contains(r, `<span class="chip">`+none) {
			t.Errorf("a chip for %q", none)
		}
	}
	if !strings.Contains(r, `name="back"`) {
		t.Errorf("a row form has no back:\n%s", r)
	}
	if p := rows[1]; strings.Contains(p, "chip") || strings.Contains(p, "<form") || !strings.Contains(p, `<span class="badge">item</span>`) {
		t.Errorf("a row with nothing is not the link and its badge:\n%s", p)
	}
}

func TestTheLinksAreASideColumnWithTheCurrentOneMarked(t *testing.T) {
	e := &siren.Entity{Title: "App", Links: []siren.Link{
		{Rel: []string{"self"}, Href: "/with/app/lists/a", Title: "List A"},
		{Rel: []string{"collection"}, Href: "/with/app/", Title: "Today"},
		{Rel: []string{"collection"}, Href: "/with/app/lists/a", Title: "A"},
		{Rel: []string{"collection"}, Href: "/with/app/lists/b", Title: "B"},
	}}
	page := drawApp(t, e, "/with/app/lists/a")
	nav := regexp.MustCompile(`(?s)<nav class="side"[^>]*>.*?</nav>`).FindString(page)
	if !strings.Contains(nav, `aria-label="Links"`) || strings.Contains(nav, "List A") {
		t.Errorf("the side column is wrong (self must be left out):\n%s", nav)
	}
	if n := strings.Count(nav, `aria-current="page"`); n != 1 || !strings.Contains(nav, `href="/with/app/lists/a" rel="collection" aria-current="page"`) {
		t.Errorf("the current link is not the one of the page:\n%s", nav)
	}
	if strings.Index(page, `<nav class="side"`) > strings.Index(page, `class="pane-main"`) {
		t.Error("the side column is not before the main column")
	}
	if none := drawApp(t, &siren.Entity{Title: "App", Links: e.Links[:1]}, "/with/app/lists/a"); strings.Contains(none, `class="side"`) {
		t.Error("a page with only a self link has a side column")
	}
}
