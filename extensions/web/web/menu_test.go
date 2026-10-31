package web_test

// The menu and the head of a page, as web draws them from the actions of the entity: the owner mark, the menu of the
// actions that no template places, the pencil beside the title, the buttons that create, and the meta line.

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// headOfPage is the head of a page: from its <header class="page-head"> to the end of the header.
func headOfPage(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<header class="page-head">`)
	if i < 0 {
		t.Fatalf("no head in the page:\n%.400s", body)
	}
	head := body[i:]
	return head[:strings.Index(head, "</header>")]
}

// TestAnOwnerActionCarriesTheOwnerMark: the owner flag of the catalog, and not a template, puts the mark on the button.
func TestAnOwnerActionCarriesTheOwnerMark(t *testing.T) {
	l := emptyLife(t)
	body := pageAt(t, l.web, "/actions")
	if want := `<summary class="button">Take a snapshot <span class="owner">owner</span></summary>`; !strings.Contains(body, want) {
		t.Errorf("/actions lacks %s", want)
	}
	if m := pageAt(t, l.web, "/metrics"); !regexp.MustCompile(`class="button primary" popovertarget="new-register-metric">[^/]*</span>New metric <span class="owner">owner</span></button>`).MatchString(m) {
		t.Errorf("/metrics lacks the New metric button with the owner mark")
	}
	if strings.Contains(body, `New page <span class="owner">`) {
		t.Error("an action that is not the owner's alone carries the owner mark")
	}
}

// TestAnOwnerActionOfTheMenuCarriesTheOwnerMark: the integrity page offers two repairs that are the owner's alone, and no
// other action; its menu marks both, on the button and in the dialog.
func TestAnOwnerActionOfTheMenuCarriesTheOwnerMark(t *testing.T) {
	l := emptyLife(t)
	body := mainPartOf(pageAt(t, l.web, "/integrity"))
	for _, name := range []string{"rebuild-search", "resync-links"} {
		btn := regexp.MustCompile(`<button type="button" class="menu-item" commandfor="do-` + name + `" command="show-modal">[^<]*<span class="owner">owner</span></button>`)
		if !btn.MatchString(body) {
			t.Errorf("the button of %s has no owner mark:\n%s", name, body)
		}
		if !strings.Contains(body, `id="do-`+name+`"`) {
			t.Errorf("no dialog for %s", name)
		}
	}
}

// TestTheMenuHoldsTheActionsNoTemplatePlaces: a page draws the actions that it does not place itself in the menu: the edit
// group with its label first, then the danger group, last and apart. Each item is a button that opens a dialog with the form
// in it (no script), and the page has no More section.
func TestTheMenuHoldsTheActionsNoTemplatePlaces(t *testing.T) {
	l := emptyLife(t)
	essay := l.do(t, "/", "create-page", map[string]string{"title": "Essay"})
	body := pageAt(t, l.web, href(essay, "self"))
	for _, want := range []string{
		`<button type="button" class="button menu-button" popovertarget="menu" aria-label="More actions">`,
		`<div class="menu-group" role="group" aria-label="Edit"><span class="menu-label">Edit</span>`,
		`<button type="button" class="menu-item" commandfor="do-promote" command="show-modal">Make it a person</button>`,
		`<dialog class="menu-dialog" id="do-promote" closedby="any" aria-label="Make it a person">`,
		`<button type="button" class="menu-item" commandfor="do-tombstone-pages" command="show-modal">Delete</button>`,
		`commandfor="do-promote" command="close">Close</button>`,
		`commandfor="do-tombstone-pages" command="close" autofocus>Cancel</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the menu of a page lacks %s", want)
		}
	}
	if strings.Contains(body, `aria-label="More"><h2>More</h2>`) || strings.Contains(body, `<details class="action" name="more">`) {
		t.Error("the page still draws the More section")
	}
	// The danger group is last and apart: after every other group, and its own group in the danger colour class.
	list := body[strings.Index(body, `<div class="menu-list"`):]
	list = list[:strings.Index(list, `<dialog`)]
	if danger := strings.Index(list, `class="menu-group danger"`); danger < strings.Index(list, `aria-label="Edit"`) ||
		strings.Count(list, `class="menu-group`) != 2 || strings.Index(list, "Make it a person") > danger || strings.Index(list, ">Delete<") < danger {
		t.Errorf("Delete is not alone in the last group of the menu:\n%s", list)
	}
	// The delete dialog is the confirm step: its button is the danger one, and the page returns to itself.
	del := regexp.MustCompile(`(?s)<dialog class="menu-dialog" id="do-tombstone-pages".*?</dialog>`).FindString(body)
	if !strings.Contains(del, `<button class="danger">Delete</button>`) || !strings.Contains(del, `name="back" value="`+href(essay, "self")+`"`) {
		t.Errorf("the dialog of Delete is not the danger form that returns to the page:\n%s", del)
	}
}

// TestAnEmptyMenuDrawsNoButton: a list whose every action has a place of its own (Browse, Metrics, Days) draws no menu button.
func TestAnEmptyMenuDrawsNoButton(t *testing.T) {
	l := emptyLife(t)
	for _, path := range []string{"/browse", "/metrics", "/days"} {
		if body := mainPartOf(pageAt(t, l.web, path)); strings.Contains(body, `menu-button`) || strings.Contains(body, `class="menu"`) {
			t.Errorf("%s draws a menu with no item", path)
		}
	}
}

// TestAnAskedFormOfTheMenuIsAnOpenDialog: ?new=rename opens the form of rename as an open dialog, not a modal one, with the
// fields that the query names filled in.
func TestAnAskedFormOfTheMenuIsAnOpenDialog(t *testing.T) {
	l := emptyLife(t)
	essay := l.do(t, "/", "create-page", map[string]string{"title": "Essay"})
	self := href(essay, "self")
	body := pageAt(t, l.web, self+"?new=rename&title="+url.QueryEscape("A better title"))
	dialog := regexp.MustCompile(`(?s)<dialog class="menu-dialog" id="pencil"[^>]*>.*?</dialog>`).FindString(body)
	if !strings.Contains(dialog, ` aria-label="Rename" open>`) || !strings.Contains(dialog, `name="title" value="A better title"`) ||
		!strings.Contains(dialog, `command="close">Close</button>`) {
		t.Errorf("the asked dialog is not open and filled:\n%s", dialog)
	}
	if other := pageAt(t, l.web, self); strings.Contains(other, `id="pencil" closedby="any" aria-label="Rename" open`) {
		t.Error("the dialog of Rename is open without a link that asked for it")
	}
}

// TestATitleEditIsThePencilAndNotInTheMenu: rename is a pencil beside the title, named by the title of the action, with its
// form in a popover under it, filled with the current title. It is drawn once, and the menu does not list it.
func TestATitleEditIsThePencilAndNotInTheMenu(t *testing.T) {
	l := emptyLife(t)
	essay := l.do(t, "/", "create-page", map[string]string{"title": "Essay"})
	body := pageAt(t, l.web, href(essay, "self"))
	head := headOfPage(t, body)
	if !regexp.MustCompile(`<h1>Essay</h1><div class="pop"><button type="button" class="button pencil" aria-label="Rename" title="Rename" popovertarget="pencil">`).MatchString(head) {
		t.Errorf("the pencil is not beside the title:\n%s", head)
	}
	if !strings.Contains(head, `<div class="head-pop" id="pencil" popover`) || !strings.Contains(head, `<input type="text" name="title" value="Essay" required autofocus>`) {
		t.Errorf("the popover of the pencil lacks its form, filled with the title and focused:\n%s", head)
	}
	if n := strings.Count(body, `data-action="rename"`); n != 1 {
		t.Errorf("rename is drawn %d times, want once", n)
	}
	if strings.Contains(body, `do-rename`) {
		t.Error("the menu lists rename")
	}
}

// TestACreateActionIsAButtonWithAPopover: a create action of a list is a button of its head, and its form is in a popover
// under it. The first button is the primary one; the others are plain.
func TestACreateActionIsAButtonWithAPopover(t *testing.T) {
	l := emptyLife(t)
	people := headOfPage(t, pageAt(t, l.web, "/browse?kind=person"))
	if !regexp.MustCompile(`<button type="button" class="button primary" popovertarget="new-create-person"><span class="icon icon-new" aria-hidden="true"></span>New person`).MatchString(people) ||
		!strings.Contains(people, `<div class="head-pop" id="new-create-person" popover`) || !strings.Contains(people, `data-action="create-person"`) {
		t.Errorf("New person is not a primary button with a popover and its form:\n%s", people)
	}
	if strings.Contains(people, `<details class="action"`) {
		t.Error("the head draws a create action as a fold")
	}
	board := headOfPage(t, pageAt(t, l.web, "/metrics"))
	first := strings.Index(board, `class="button primary" popovertarget="new-register-metric"`)
	second := strings.Index(board, `class="button" popovertarget="new-record"`)
	if first < 0 || second < first || strings.Count(board, `button primary`) != 1 {
		t.Errorf("the board does not draw New metric as the one primary button, before Add a reading:\n%s", board)
	}
}

// TestTheMetaLineIsUnderTheTitleAndTheFactsSectionIsGone: a person's name and dates, a file's type and short SHA-256 and
// when the page was created are one line in the head; the margin has no Facts section.
func TestTheMetaLineIsUnderTheTitleAndTheFactsSectionIsGone(t *testing.T) {
	l := emptyLife(t)
	sam := l.do(t, "/", "create-person", map[string]string{"title": "Sam", "name": "Samuel Ortiz", "birth_day": "1980-02-03", "death_day": "2020-01-02"})
	l.do(t, "/", "add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf"})
	scan := must(t)(l.api.Get(t.Context(), "/pages?title=Scan.pdf"))
	for _, row := range []struct {
		name, path string
		want       []string
	}{
		{"a person", href(sam, "self"), []string{`<p class="meta-line">Samuel Ortiz · born Sun 3 Feb 1980 · died Thu 2 Jan 2020 · created <time `}},
		{"a file", href(scan, "self"), []string{`<p class="meta-line">application/pdf · <span title="SHA-256 of the original: ` + strings.Repeat("ab", 32) + `">` + strings.Repeat("ab", 6) + `…</span> · created <time `}},
	} {
		body := pageAt(t, l.web, row.path)
		head := headOfPage(t, body)
		for _, want := range row.want {
			if !strings.Contains(head, want) {
				t.Errorf("%s: the head lacks %s:\n%s", row.name, want, head)
			}
		}
		if strings.Contains(body, `aria-label="Facts"`) || strings.Contains(body, `<dt>SHA-256</dt>`) {
			t.Errorf("%s: the page draws a Facts section", row.name)
		}
	}
}
