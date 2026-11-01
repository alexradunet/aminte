package web_test

// The forms that a link opens. A link of a page asks for the form of an action with ?new=NAME, and the page draws that form
// open, with the fields that the query names filled in. The New menu leads to the form of each kind of thing.

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// formStartTag finds the start tag of each form, with its attributes in any order; formActionAttribute finds the address
// that such a tag posts to.
var (
	formStartTag        = regexp.MustCompile(`<form\b[^>]*>`)
	formActionAttribute = regexp.MustCompile(`\saction="([^"]*)"`)
)

// TestAnOpenedActionWritesFromTheBrowser: the action that /actions opens posts to its resource, and the write is done there,
// as from the own page of the resource.
func TestAnOpenedActionWritesFromTheBrowser(t *testing.T) {
	l := emptyLife(t)
	p := l.do(t, "/", "create-page", map[string]string{"title": "Old name"})
	id := fmt.Sprint(p.Properties.(map[string]any)["id"])
	page := pageAt(t, l.web, "/actions?new=rename&id="+id)
	form := regexp.MustCompile(`<form[^>]* action="(/pages/` + id + `/rename)"`).FindStringSubmatch(page)
	if form == nil {
		t.Fatalf("/actions?new=rename&id=%s draws no form of the rename of the page:\n%s", id, mainPartOf(page))
	}
	if rec := postForm(l.web, form[1], url.Values{"title": {"New name"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d %s", form[1], rec.Code, rec.Body)
	}
	if got := must(t)(l.api.Get(t.Context(), "/pages/"+id)).Title; got != "New name" {
		t.Errorf("the page is titled %q after the rename, want New name", got)
	}
}

// TestTheFormOfReplyIsOnTheCatalogPageAndWritesTheAgentsTurn: reply is the agent's action. No page of a conversation draws
// it, because the entity of a conversation does not offer it (the README names /actions alone). The form on /actions
// posts to the day, takes the number of the turn that it answers, and the core writes the reply right after that turn.
// A second reply to the same turn is 409, and web draws that refusal with its status.
func TestTheFormOfReplyIsOnTheCatalogPageAndWritesTheAgentsTurn(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "How did I sleep?"})
	post := "/days/" + today + "/reply"

	// No page of the conversation draws a form of reply: the form of /actions is the only one.
	for _, uri := range []string{"/days/" + today + "/conversation", "/"} {
		if page := pageAt(t, l.web, uri); strings.Contains(page, `data-action="reply"`) || strings.Contains(page, post) {
			t.Errorf("%s draws the form of reply, which the agent alone uses", uri)
		}
	}
	form := pageAt(t, l.web, "/actions?new=reply&day="+today)
	for _, want := range []string{`<details class="action" open>`, `action="` + post + `"`,
		`<input type="number" name="turn"`, `<textarea name="text"`, `name="tools"`} {
		if !strings.Contains(form, want) {
			t.Errorf("/actions?new=reply draws no %s:\n%.1500s", want, mainPartOf(form))
		}
	}

	sent := url.Values{"turn": {"1"}, "text": {"You slept 7 hours."}, "tools": {"- `search`: no hits"}}
	if rec := postForm(l.web, post, sent); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d %.300s", post, rec.Code, rec.Body)
	}
	turns, _ := must(t)(l.api.Get(t.Context(), "/days/"+today+"/conversation")).Properties.(map[string]any)["turns"].([]any)
	if len(turns) != 2 {
		t.Fatalf("the conversation has %d turns after the reply, want the question and the answer: %v", len(turns), turns)
	}
	answer, _ := turns[1].(map[string]any)
	if answer["who"] != "lifelog" || answer["text"] != "You slept 7 hours." || fmt.Sprint(answer["tools"]) != "[- `search`: no hits]" {
		t.Errorf("the second turn is %v, want the reply of lifelog with its tool line", answer)
	}

	again := postForm(l.web, post, sent)
	if again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), "conflict") {
		t.Errorf("a second reply to the turn: %d, want the 409 page of the refusal: %.300s", again.Code, mainPartOf(again.Body.String()))
	}
	if turns, _ := must(t)(l.api.Get(t.Context(), "/days/"+today+"/conversation")).Properties.(map[string]any)["turns"].([]any); len(turns) != 2 {
		t.Errorf("a refused reply left %d turns, want 2", len(turns))
	}
}

// TestSnapshotIsTheOwnersFromABrowser: the form of the snapshot is the owner's. A snapshot taken by the form of the browser
// answers with the toast of the page it returns to, which says where the copy is, its restore check, and a link to download
// it. The form is open on /actions?new=snapshot with the owner mark, and Settings links to it.
func TestSnapshotIsTheOwnersFromABrowser(t *testing.T) {
	l := emptyLife(t)
	rec := postForm(l.web, "/snapshots", url.Values{})
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/?feedback=") {
		t.Fatalf("the form of the snapshot: %d %q, want 303 to the page it came from with a receipt", rec.Code, rec.Header().Get("Location"))
	}
	toast := ask(l.web, http.MethodGet, rec.Header().Get("Location")).Body.String()
	for _, want := range []string{"life-" + today, "Took a snapshot: <code>", "<dt>restore check</dt>",
		`<a href="/snapshots/life-` + today + `.db" download>Download it</a>`} {
		if !strings.Contains(toast, want) {
			t.Errorf("the toast of the snapshot lacks %s:\n%.1500s", want, mainPartOf(toast))
		}
	}
	// The link of Snapshot opens this form.
	body := pageAt(t, l.web, "/actions?new=snapshot")
	if !strings.Contains(body, `<details class="action" open><summary class="button">Take a snapshot <span class="owner">owner</span></summary>`) ||
		!strings.Contains(body, `action="/snapshots"`) {
		t.Errorf("the link of Snapshot does not open the form of the snapshot:\n%.800s", mainPartOf(body))
	}
	if body := pageAt(t, l.web, "/settings"); !strings.Contains(body, `<a class="button" href="/actions?new=snapshot">Take a snapshot</a>`) {
		t.Errorf("Settings has no link to the form of the snapshot:\n%.1500s", mainPartOf(body))
	}
}

// TestEveryKindOfTheNewMenuOpensItsForm: the New menu of the rail is a menu of the kinds, each made on its own list, and
// ?new=NAME opens the form of that action on any page.
func TestEveryKindOfTheNewMenuOpensItsForm(t *testing.T) {
	l := lifeStartingOn(t, today)
	day := pageAt(t, l.web, "/days/"+today)
	menu := day[strings.Index(day, `<div class="new-menu" id="new-menu" popover>`):]
	menu = menu[:strings.Index(menu, "</nav></div>")]
	for _, kind := range []struct{ title, href, action, post string }{ // post: the address that the form of the action posts to
		{"Page", "/browse?kind=page&new=create-page", "create-page", "/pages"},
		{"Person", "/browse?kind=person&new=create-person", "create-person", "/people"},
		{"Metric", "/metrics?new=register-metric", "register-metric", "/metrics"},
		{"Reading", "/?new=record#record", "record", "/measurements"},
		{"File", "/browse?kind=media&new=add-file", "add-file", "/files"},
	} {
		link := regexp.MustCompile(`<a href="` + regexp.QuoteMeta(strings.ReplaceAll(kind.href, "&", "&amp;")) + `">(<span class="icon[^>]*></span>)?` + kind.title + `</a>`)
		if !link.MatchString(menu) {
			t.Errorf("the New menu has no %s at %s", kind.title, kind.href)
			continue
		}
		to, _, _ := strings.Cut(kind.href, "#") // a browser sends no fragment
		page := pageAt(t, l.web, to)
		// An asked form of a head is an open dialog, not a modal one, in the place of its popover. The day draws a reading in
		// a place of its own: a form that is open.
		open := regexp.MustCompile(`(?s)<dialog class="menu-dialog" id="new-` + kind.action + `" closedby="any" aria-label="[^"]*" open>.*?<form method="POST" action="` + kind.post + `"`)
		if kind.action == "record" {
			open = regexp.MustCompile(`<details class="action" open><summary class="button">[^<]*(<span class="owner">owner</span>)?</summary><div class="action-form">(<p class="desc">[^<]*</p>)?<form method="POST" action="` + kind.post + `"`)
		}
		if !open.MatchString(page) {
			t.Errorf("%s does not open the form of %s", kind.href, kind.action)
		}
	}
	if strings.Count(menu, "<li>") != 5 {
		t.Errorf("the New menu has %d items, want 5", strings.Count(menu, "<li>"))
	}
}

// TestALinkOpensAFormAndFillsItsFields: a link with ?new= opens the form of the action and fills the fields that the query
// names. With no ?new= every form stays closed. A name that no action has is refused (422) and opens nothing.
func TestALinkOpensAFormAndFillsItsFields(t *testing.T) {
	l := lifeStartingOn(t, today)
	people := pageAt(t, l.web, "/browse?kind=person&new=create-person&title=Ana+Popescu&name=Ana")
	form := people[strings.Index(people, `<dialog class="menu-dialog" id="new-create-person"`):]
	form = form[:strings.Index(form, "</dialog>")]
	for _, want := range []string{`<form method="POST" action="/people"`, `name="title" value="Ana Popescu"`, `name="name" value="Ana"`, `autofocus`} {
		if !strings.Contains(form, want) {
			t.Errorf("the asked form lacks %s:\n%s", want, form)
		}
	}
	if strings.Count(people, `" open>`) != 1 {
		t.Error("more than the asked form is open")
	}

	if body := pageAt(t, l.web, "/browse?kind=person"); strings.Contains(body, `<details class="action" open>`) {
		t.Error("a list with no ?new= opens a form")
	}
	if body := pageWithStatus(t, l.web, "/browse?kind=person&new=nothing&title=Ana", http.StatusUnprocessableEntity); strings.Contains(body, `<details class="action" open>`) {
		t.Error("a name that no action has opens a form")
	}

	// Home is the page of today: New page is in the New menu of the rail, and the page has no fold of every create form.
	home := pageAt(t, l.web, "/days/"+today)
	if strings.Contains(home, `<details id="new">`) {
		t.Error(`Home draws <details id="new">`)
	}
	// No form on Home makes a person or keeps a file, whatever the order of the attributes of its tag.
	for _, tag := range formStartTag.FindAllString(home, -1) {
		if m := formActionAttribute.FindStringSubmatch(tag); m != nil && (m[1] == "/people" || m[1] == "/files") {
			t.Errorf("Home draws the form %s", tag)
		}
	}
}

// TestAFormDrawsTheHintAndTheActionsPageTheDescription: a form says what the action does in the words of its hint. /actions
// lists every action with the text of the API.
func TestAFormDrawsTheHintAndTheActionsPageTheDescription(t *testing.T) {
	l := emptyLife(t)
	hint := "Make a person. The title is the short name that links use. The name is the full name."
	if page := pageAt(t, l.web, "/browse?kind=person"); !strings.Contains(page, hint) {
		t.Errorf("/browse?kind=person does not draw the hint of create-person")
	}
	page := pageAt(t, l.web, "/actions")
	if strings.Contains(page, hint) || !strings.Contains(page, "Create a person: title is the permanent handle") {
		t.Errorf("/actions does not draw the description of create-person")
	}
}
