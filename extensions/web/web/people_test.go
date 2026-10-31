package web_test

// The people list of Browse and the page of a person as the browser draws them: the words about the mentions, the face
// of a person, and the rename and the edit of a person through web. The claims are those of the HTML part of the people
// tests of the core (plan 161, stage 3), held here against the real core.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestPeopleListCarriesItsMentions: the sheet of the people list says in words how many pages link to each person and the
// newest day page among them.
func TestPeopleListCarriesItsMentions(t *testing.T) {
	l := lifeStartingOn(t, "2026-10-12") // the day the words are read against: a day of this year has no year
	for _, name := range []string{"Ana", "Sam", "Zed"} {
		l.do(t, "/", "create-person", map[string]string{"title": name})
	}
	l.do(t, "/days/2026-10-05", "capture", map[string]string{"text": "Lunch with [[Ana]]"})
	l.do(t, "/days/2026-10-09", "capture", map[string]string{"text": "Walk with [[Ana]] and [[Sam]]"})
	l.do(t, "/", "create-page", map[string]string{"title": "Trip", "body": "[[Ana]] came"})

	var anaHref string
	for _, item := range must(t)(l.api.Get(t.Context(), "/browse?kind=person")).Entities {
		if item.Title == "Ana" {
			anaHref = item.Href
		}
	}
	body := htmlPageAt(t, l.web, "/browse?kind=person", http.StatusOK)
	for _, s := range []string{
		"mentioned on 3 pages, last Fri 9 Oct",
		"mentioned on 1 page, last Fri 9 Oct",
		"not mentioned yet",
		`<a class="entry named face" href="` + anaHref + `">`,
	} {
		if !strings.Contains(body, s) {
			t.Errorf("the people sheet lacks %q", s)
		}
	}
}

// TestPeopleListShowsThePictureAPersonEmbeds: a person whose page embeds a picture has that picture in its row, round,
// before the name; a person with none has the icon of a person.
func TestPeopleListShowsThePictureAPersonEmbeds(t *testing.T) {
	l := emptyLife(t)
	photo := l.do(t, "/", "add-file", map[string]string{"title": "Ana face.jpg", "original": pictureFileOf(t, "ana.jpg", 40, 30)})
	ana := l.do(t, "/", "create-person", map[string]string{"title": "Ana"})
	l.do(t, href(ana, "self"), "save-body", map[string]string{"body": "![[Ana face.jpg]]\n\nMet at school."})
	l.do(t, "/", "create-person", map[string]string{"title": "Sam"})

	wantInPage(t, "the people sheet", htmlPageAt(t, l.web, "/browse?kind=person", http.StatusOK),
		`<img class="face-picture" src="`+href(photo, "preview")+`" alt="" loading="lazy">`, `<span class="face-picture" aria-hidden="true">`)
}

// TestMentionsHaveNoDayWhenNoDayPageLinks: a page that is not a day page gives a count and no last day.
func TestMentionsHaveNoDayWhenNoDayPageLinks(t *testing.T) {
	l := lifeStartingOn(t, "2026-10-12")
	l.do(t, "/", "create-person", map[string]string{"title": "Lake"})
	l.do(t, "/", "create-page", map[string]string{"title": "Trip", "body": "At [[Lake]]"})
	l.do(t, "/", "create-page", map[string]string{"title": "Swim", "body": "In [[Lake]]"})
	if body := htmlPageAt(t, l.web, "/browse?kind=person", http.StatusOK); !strings.Contains(body, "mentioned on 2 pages</span>") {
		t.Errorf("the people sheet lacks the count without a day:\n%.2000s", body)
	}
}

// TestBrowseRowsNameTheirPicture: a file kept with a picture and a person whose cover it is: the tile draws the
// picture, and the face of the person draws it round.
func TestBrowseRowsNameTheirPicture(t *testing.T) {
	l, ids := browseLife(t)
	lake := l.do(t, "/", "add-file", map[string]string{"title": "lake.jpg", "day": "2031-06-01", "original": pictureFileOf(t, "lake.jpg", 40, 30)})
	l.do(t, "/pages/"+ids["Bo"], "set-cover", map[string]string{"title": "lake.jpg"})
	preview := href(lake, "preview")

	if body := htmlPageAt(t, l.web, "/browse?kind=media", http.StatusOK); !strings.Contains(body, `<img src="`+preview+`" alt="" loading="lazy"><span class="first">lake.jpg</span>`) {
		t.Error("the media tile does not draw the picture")
	}
	people := htmlPageAt(t, l.web, "/browse?kind=person", http.StatusOK)
	if !strings.Contains(people, fmt.Sprintf(`<a class="entry named face" href="/pages/%s"><img class="face-picture" src="%s" alt="" loading="lazy">`, ids["Bo"], preview)) {
		t.Error("the face does not draw the picture")
	}
	// Ana has no picture: her face is the icon of a person, and no picture is drawn for her.
	if !strings.Contains(people, fmt.Sprintf(`<a class="entry named face" href="/pages/%s"><span class="face-picture" aria-hidden="true">`, ids["Ana"])) {
		t.Error("a person with no picture does not draw the icon")
	}
}

// TestBrowserStableNameResolution: a rename through web goes to the page of the person, the page shows the new title,
// and a page that said [[Old person]] links the person at the same address.
func TestBrowserStableNameResolution(t *testing.T) {
	l := emptyLife(t)
	person := pageIDOf(t, href(l.do(t, "/", "create-person", map[string]string{"title": "Old person", "name": "Synthetic Person"}), "self"))
	ref := pageIDOf(t, href(l.do(t, "/", "create-page", map[string]string{"title": "Old reference", "body": "See [[Old person]]."}), "self"))

	rec := postForm(l.web, "/pages/"+person+"/rename", url.Values{"title": {"New person"}})
	if rec.Code != http.StatusSeeOther || locationWithoutFeedback(t, rec.Header().Get("Location")) != "/pages/"+person {
		t.Fatalf("rename: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if page := htmlPageAt(t, l.web, "/pages/"+person, http.StatusOK); !strings.Contains(page, "New person") {
		t.Error("the new title is absent from the page of the person")
	}
	if page := htmlPageAt(t, l.web, "/pages/"+ref, http.StatusOK); !strings.Contains(page, `href="/pages/`+person+`"`) {
		t.Error("the old reference does not link the person at the same address")
	}
}

// TestEditPersonThroughTheAPIAndTheBrowser: the page of a person offers Edit name and dates with the values, the form
// of the browser sends every field and an empty date removes it, and a page that is not a person refuses the route.
func TestEditPersonThroughTheAPIAndTheBrowser(t *testing.T) {
	l := emptyLife(t)
	sam := l.do(t, "/", "create-person", map[string]string{"title": "Sam (barber)", "name": "Samuel Ortiz", "birth_day": "1980-02-03"})
	self := href(sam, "self")
	edit := act(t, sam, "edit-person")

	wantInPage(t, "the person's page", htmlPageAt(t, l.web, self, http.StatusOK), `Edit name and dates`, `value="Samuel Ortiz"`, `value="1980-02-03"`)
	// A form of the browser sends every field; an empty date removes it.
	rec := postForm(l.web, edit.Href, url.Values{"name": {"Samuel A. Ortiz"}, "birth_day": {""}, "death_day": {"2020-01-01"}}, "Referer", refererOnWeb(self))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the form answered %d, want 303: %.300s", rec.Code, rec.Body.String())
	}
	person, _ := must(t)(l.api.Get(t.Context(), self)).Properties.(map[string]any)["person"].(map[string]any)
	if person["name"] != "Samuel A. Ortiz" || person["birth_day"] != nil || person["death_day"] != "2020-01-01" {
		t.Errorf("the person is %v, want a new name, no birth day and a death day", person)
	}
	// A page that is not a person has no such route.
	plain := l.do(t, "/", "create-page", map[string]string{"title": "Plain"})
	if rec := postForm(l.web, href(plain, "self")+"/person", url.Values{"name": {"X"}}); rec.Code != http.StatusConflict {
		t.Errorf("edit-person on a plain page: %d, want 409", rec.Code)
	}
}
