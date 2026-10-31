package web_test

// The categories (D26) as the browser draws them: the badges of a page, the form that puts it in a category, the section
// Filed here of a category page, and the New page form that files a page where it is made. The claims are those of the
// HTML part of the category tests of the core (plan 161, stage 3), held here against the real core.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// travelLife is a file that has Alps 2030, Japan 2031 and the person Sam filed in Travel, and Travel filed in Life. It
// returns the ids of the pages by title.
func travelLife(t *testing.T) (*life, map[string]string) {
	t.Helper()
	l := emptyLife(t)
	ids := map[string]string{}
	for _, title := range []string{"Japan 2031", "Alps 2030"} {
		ids[title] = pageIDOf(t, href(l.do(t, "/", "create-page", map[string]string{"title": title}), "self"))
	}
	ids["Sam"] = pageIDOf(t, href(l.do(t, "/", "create-person", map[string]string{"title": "Sam"}), "self"))
	for _, title := range []string{"Japan 2031", "Alps 2030", "Sam"} {
		l.do(t, "/pages/"+ids[title], "file-pages", map[string]string{"category": "Travel"})
	}
	ids["Travel"] = pageIDOf(t, href(must(t)(l.api.Get(t.Context(), "/pages?title=Travel")), "self"))
	l.do(t, "/pages/"+ids["Travel"], "file-pages", map[string]string{"category": "Life"})
	ids["Life"] = pageIDOf(t, href(must(t)(l.api.Get(t.Context(), "/pages?title=Life")), "self"))
	return l, ids
}

// TestAPagesCategoriesAreBadges: a page shows its categories as badges; + Add puts it in one (a new name makes it), and
// x takes it out of that one only (file-pages with from). + Add is the form of file-pages: it sends this page's id and
// move 0, and its category offers the titles of the categories in a datalist. A day page has none.
func TestAPagesCategoriesAreBadges(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "create-page", map[string]string{"title": "Seeds"})
	l.do(t, "/pages?title=Seeds", "file-pages", map[string]string{"category": "Projects"})
	garden := l.do(t, "/", "create-page", map[string]string{"title": "Garden", "body": "See [[Seeds]]."})
	self := href(garden, "self")
	id := pageIDOf(t, self)

	before := htmlPageAt(t, l.web, self, http.StatusOK)
	wantInPage(t, "the page", before, `<div class="categories" role="group" aria-label="Categories"><span class="label">Categories</span>`, `<span class="meta">none</span>`,
		`<form method="POST" action="/pages/file" class="inline" data-action="file-pages">`+"\n"+`<input type="hidden" name="ids" value="`+id+`"><input type="hidden" name="move" value="0">`,
		`<input type="text" name="category" value="" required list="file-pages-category" placeholder="Bloodwork, Sport, Financial"><datalist id="file-pages-category"><option value="Projects"></datalist>`)
	wantNotInPage(t, "the page", before, `<option value="Garden">`) // a page is not offered as its own category

	for _, category := range []string{"Projects", "Outdoors"} {
		rec := postForm(l.web, "/pages/file", valuesAFormSends(t, before, `action="/pages/file"`, url.Values{"category": {category}}))
		if rec.Code != http.StatusSeeOther || !isReceiptLocation(rec.Header().Get("Location"), self) {
			t.Fatalf("+ Add %s: %d %q", category, rec.Code, rec.Header().Get("Location"))
		}
	}
	after := htmlPageAt(t, l.web, self, http.StatusOK)
	for _, category := range []string{"Projects", "Outdoors"} {
		if !regexp.MustCompile(`<span class="badge category"><a href="/pages/\d+">` + category + `</a><form method="POST" action="/pages/file" class="tiny">\s*<input type="hidden" name="ids" value="` + id + `"><input type="hidden" name="from" value="` + category + `">`).MatchString(after) {
			t.Errorf("the page has no badge for %s", category)
		}
	}

	rec := postForm(l.web, "/pages/file", url.Values{"ids": {id}, "from": {"Outdoors"}, "back": {self}})
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || (loc != self && !isReceiptLocation(loc, self)) {
		t.Fatalf("x: %d %q", rec.Code, loc)
	}
	links := map[string]string{}
	for _, link := range must(t)(l.api.Get(t.Context(), self)).Properties.(map[string]any)["links"].([]any) {
		link := link.(map[string]any)
		links[link["title"].(string)] = link["kind"].(string)
	}
	if len(links) != 2 || links["Projects"] != "part-of" || links["Seeds"] != "wikilink" {
		t.Errorf("after x the links are %v, want Projects part-of and Seeds wikilink", links)
	}

	day := l.do(t, "/", "capture", map[string]string{"text": "Dug the beds."})
	wantNotInPage(t, "a day page", htmlPageAt(t, l.web, href(day, "page"), http.StatusOK), `aria-label="Categories"`)
}

// TestACategoryPageListsWhatIsFiledInIt: the page of a category has one section, Filed here, with the sub-categories
// first and a type badge on what is not a page; the margin's Backlinks has no part-of group. A page that is no category
// has no such section.
func TestACategoryPageListsWhatIsFiledInIt(t *testing.T) {
	l, ids := travelLife(t)
	row := regexp.MustCompile(`<li><a class="entry" href="/pages/(\d+)"><span class="first">([^<]*)</span><span class="meta">(?:<span class="badge">([^<]*)</span>)?</span></a></li>`)
	section := func(body string) string {
		t.Helper()
		i := strings.Index(body, `<section class="group filed" aria-label="Filed here">`)
		if i < 0 {
			t.Fatal("no Filed here section")
		}
		body = body[i:]
		return body[:strings.Index(body, "</section>")]
	}
	var rows []string
	for _, m := range row.FindAllStringSubmatch(section(htmlPageAt(t, l.web, "/pages/"+ids["Life"], http.StatusOK)), -1) {
		rows = append(rows, m[2]+":"+m[3])
	}
	if want := "Travel:category"; strings.Join(rows, " ") != want {
		t.Errorf("Life's Filed here: %v, want %s", rows, want)
	}
	body := htmlPageAt(t, l.web, "/pages/"+ids["Travel"], http.StatusOK)
	rows = nil
	for _, m := range row.FindAllStringSubmatch(section(body), -1) {
		rows = append(rows, m[2]+":"+m[3])
	}
	if want := "Alps 2030: Japan 2031: Sam:person"; strings.Join(rows, " ") != want {
		t.Errorf("Travel's Filed here: %v, want %s", rows, want)
	}
	wantNotInPage(t, "the page of Travel", body, `<p class="sub">part-of</p>`) // the margin's Backlinks has no part-of group
	wantInPage(t, "the page of Travel", body, `<h2>Filed here <small>3</small></h2>`)
	// A page in no category has no section.
	wantNotInPage(t, "a page that is no category", htmlPageAt(t, l.web, "/pages/"+ids["Japan 2031"], http.StatusOK), `aria-label="Filed here"`)
}

// TestANewPageIsFiledWhereItIsMade: a category page offers New page here, with its own title as the category of the page;
// a page that is no category does not offer it.
func TestANewPageIsFiledWhereItIsMade(t *testing.T) {
	l, ids := travelLife(t)
	body := htmlPageAt(t, l.web, "/pages/"+ids["Travel"], http.StatusOK)
	if !regexp.MustCompile(`<form method="POST" action="/pages"[^>]*>(?s:.*?)<input type="hidden" name="category" value="Travel">`).MatchString(body) ||
		!strings.Contains(body, `<summary class="button">New page here</summary>`) {
		t.Error("Travel has no New page here form with its title filled")
	}
	wantNotInPage(t, "a page that is no category", htmlPageAt(t, l.web, "/pages/"+ids["Japan 2031"], http.StatusOK), `<summary class="button">New page here</summary>`)
}

// TestACategoryPageOpensInBrowse: Filed here links the list of the category in Browse, its title escaped in the query.
func TestACategoryPageOpensInBrowse(t *testing.T) {
	l, ids := travelLife(t)
	wantInPage(t, "the page of Travel", htmlPageAt(t, l.web, "/pages/"+ids["Travel"], http.StatusOK), `<h2>Filed here <small>3</small></h2><a href="/browse?in=Travel">Open in Browse</a>`)
	note := l.do(t, "/", "create-page", map[string]string{"title": "A note"})
	l.do(t, href(note, "self"), "file-pages", map[string]string{"category": "Rock & roll 2031"})
	cat := must(t)(l.api.Get(t.Context(), "/pages?title="+url.QueryEscape("Rock & roll 2031")))
	body := htmlPageAt(t, l.web, href(cat, "self"), http.StatusOK)
	m := regexp.MustCompile(`<a href="(/browse\?in=[^"]*)">Open in Browse</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the category page has no link to Browse")
	}
	u, err := url.Parse(strings.ReplaceAll(m[1], "&amp;", "&"))
	if err != nil || u.Query().Get("in") != "Rock & roll 2031" || len(u.Query()) != 1 {
		t.Fatalf("the link %s does not name the category: %v", m[1], err)
	}
	wantInPage(t, "Browse in the category", htmlPageAt(t, l.web, u.String(), http.StatusOK), `<span class="first">A note</span>`)
	// A page with nothing filed in it has no Filed here, and no link.
	wantNotInPage(t, "a page with nothing filed in it", htmlPageAt(t, l.web, href(note, "self"), http.StatusOK), "Open in Browse")
}

// TestBrowseNewFormsInACategoryCarryIt: Browse inside a category fills category in its New page form, and the form, sent
// as it is filled, files the new page there.
func TestBrowseNewFormsInACategoryCarryIt(t *testing.T) {
	l, _ := browseLife(t)
	body := htmlPageAt(t, l.web, "/browse?in=Family&kind=page", http.StatusOK)
	form := regexp.MustCompile(`(?s)<form method="POST" action="/pages"[ >].*?</form>`).FindString(body)
	if !regexp.MustCompile(`<input[^>]*name="category"[^>]*value="Family"`).MatchString(form) {
		t.Errorf("pages in Family: the page's create-page form has no category Family: %s", form)
	}
	wantNotInPage(t, "pages everywhere", htmlPageAt(t, l.web, "/browse?kind=page", http.StatusOK), `name="category" value="Family"`)

	// The form, sent as it is filled, files the new page in Family.
	rec := postForm(l.web, "/pages", valuesAFormSends(t, body, `action="/pages"`, url.Values{"title": {"Cy"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the form: %d %.300s", rec.Code, rec.Body.String())
	}
	var filed []string
	for _, link := range must(t)(l.api.Get(t.Context(), "/pages?title=Cy")).Entities {
		if len(link.Rel) == 1 && link.Rel[0] == "part-of" {
			filed = append(filed, link.Title)
		}
	}
	if strings.Join(filed, ",") != "Family" {
		t.Errorf("Cy made from Browse in Family is part-of %v", filed)
	}
}
