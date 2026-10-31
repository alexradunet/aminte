package web_test

// The page of Browse as the browser draws it: the crumbs, the tree of categories, the chips, the rows of each kind, the
// pick bar and the empty words. The claims are those of the HTML tests of the browser of the core (plan 161, stage 3),
// held here against the real core: web draws the page from the answer of the API.

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog-web/view"
)

// pageIDOf is the id at the end of the address of a page, "/pages/7" or "/pages/7?x=y".
func pageIDOf(t *testing.T, address string) string {
	t.Helper()
	address, _, _ = strings.Cut(address, "?")
	rest, ok := strings.CutPrefix(address, "/pages/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		t.Fatalf("%q is no address of a page", address)
	}
	return rest
}

// browseLife is a file with the layout of the Browse tests of the core, made through the API (plan 161, stage 3).
//
//	Family:  Beta, Cousins, Ana, photo.jpg, scan.pdf
//	Cousins: Gamma, Weight
//	nowhere: Alpha, Bo, memo.m4a, Mood (the metric every file has)
//
// The day page 2031-05-01 mentions Ana. A day page is never filed in a category (D26). browseLife returns the ids of
// the pages by title.
func browseLife(t *testing.T) (*life, map[string]string) {
	t.Helper()
	l := emptyLife(t)
	ids := map[string]string{}
	for _, title := range []string{"Alpha", "Beta", "Gamma"} {
		ids[title] = pageIDOf(t, href(l.do(t, "/", "create-page", map[string]string{"title": title}), "self"))
	}
	for _, title := range []string{"Ana", "Bo"} {
		ids[title] = pageIDOf(t, href(l.do(t, "/", "create-person", map[string]string{"title": title}), "self"))
	}
	for i, f := range []struct{ title, mime, day string }{
		{"photo.jpg", "image/jpeg", "2031-05-02"}, {"memo.m4a", "audio/mp4", "2031-05-01"}, {"scan.pdf", "application/pdf", "2031-04-01"}} {
		kept := l.do(t, "/", "add-file", map[string]string{"title": f.title, "mime": f.mime, "day": f.day, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+1), 32)})
		ids[f.title] = pageIDOf(t, href(kept, "self"))
	}
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	for _, name := range []string{"Weight", "Mood"} {
		ids[name] = pageIDOf(t, pageOf(t, l, name))
	}
	l.do(t, "/days/2031-05-01", "capture", map[string]string{"text": "Lunch with [[Ana]]"})
	file := func(category string, titles ...string) {
		t.Helper()
		var list []string
		for _, title := range titles {
			list = append(list, ids[title])
		}
		filed := l.do(t, "/browse?kind=person", "file-pages", map[string]string{"ids": strings.Join(list, ","), "category": category})
		in, _ := filed.Properties.(map[string]any)["category"].(map[string]any)
		ids[category] = fmt.Sprint(in["id"])
	}
	file("Family", "Beta", "Ana", "photo.jpg", "scan.pdf")
	file("Cousins", "Gamma", "Weight")
	file("Family", "Cousins")
	return l, ids
}

// htmlPageAt asks h for the page at uri as a browser does. It fails the test unless the status is want and the body is a
// whole page.
func htmlPageAt(t *testing.T, h http.Handler, uri string, want int) string {
	t.Helper()
	rec := ask(h, http.MethodGet, uri)
	body := rec.Body.String()
	if rec.Code != want || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("GET %s: %d, want %d and a whole page: %.300s", uri, rec.Code, want, body)
	}
	return body
}

// wantInPage fails the test for each of the texts that the page does not hold. what names the page in the failure.
func wantInPage(t *testing.T, what, page string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if !strings.Contains(page, text) {
			t.Errorf("%s lacks %s", what, text)
		}
	}
}

// wantNotInPage fails the test for each of the texts that the page holds. what names the page in the failure.
func wantNotInPage(t *testing.T, what, page string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(page, text) {
			t.Errorf("%s holds %s", what, text)
		}
	}
}

func TestTheBrowsePageDrawsTheTreeTheChipsAndTheRows(t *testing.T) {
	l, ids := browseLife(t)
	family := htmlPageAt(t, l.web, "/browse?in=Family", http.StatusOK)
	wantInPage(t, "/browse?in=Family", family,
		`<nav class="crumbs" aria-label="Where"><a href="/browse">Browse</a><span aria-current="page">Family</span></nav>`, `<h1>Family</h1>`, `<p class="sub"><a href="/pages/`+ids["Family"]+`">Open its page</a>`,
		`<details class="browse-tree panel"><summary>Categories <span class="meta">Family</span></summary><nav aria-label="Categories">`,
		`<a class="entry" href="/browse"><span class="first">Everything</span>`,
		// The tree folds (docs/plans/118-fold-and-find.md, rule 1): Cousins is in the fold of Family, open, since the
		// list is on Family.
		`<li class="branch"><a class="entry" href="/browse?in=Family" aria-current="page"><span class="first">Family</span><span class="meta">5</span></a><a class="tree-new" href="/browse?in=Family&amp;kind=page&amp;new=create-page" aria-label="New page in Family">+</a><details open><summary><span class="sr">Categories in Family</span></summary><ul class="plain rows tree"><li><a class="entry" href="/browse?in=Cousins"><span class="first">Cousins</span><span class="meta">2</span></a><a class="tree-new" href="/browse?in=Cousins&amp;kind=page&amp;new=create-page" aria-label="New page in Cousins">+</a></li>`,
		`<li class="tree-foot"><a href="/browse?kind=page&amp;new=file-pages">New category</a></li>`,
		`<a class="entry" href="/browse?unfiled=1"><span class="first">Not filed</span>`,
		`<nav class="kinds chips" aria-label="Kind"><a href="/browse?in=Family&amp;kind=all" aria-current="page">All <small>5</small></a><a href="/browse?in=Family&amp;kind=page">Pages <small>2</small></a>`,
		// The category under Family is a row with its badge.
		`<span class="first">Cousins</span><span class="meta"><span class="badge category">category</span></span>`,
		`<span class="first">Ana</span><span class="meta"><span class="badge">person</span></span>`)
	// All is the default view of a category, so it is where a thing is moved: the bar moves and removes, and never
	// deletes (a person is deleted from its own page).
	if !strings.Contains(family, `id="pick"`) || !strings.Contains(family, `<button class="primary">Move to</button>`) ||
		!strings.Contains(family, `<button form="pick" name="from" value="Family" formnovalidate>Remove from Family</button>`) || strings.Contains(family, `>Delete<`) {
		t.Error("All in a category does not draw the pick bar with Move to and Remove from Family, and no Delete")
	}
	if cousins := htmlPageAt(t, l.web, "/browse?in=Cousins&kind=page", http.StatusOK); !strings.Contains(cousins, `<nav class="crumbs" aria-label="Where"><a href="/browse?kind=page">Browse</a><a href="/browse?in=Family&amp;kind=page">Family</a><span aria-current="page">Cousins</span></nav>`) || !strings.Contains(cousins, `<h1>Cousins</h1>`) ||
		!strings.Contains(cousins, `<a href="/browse?in=Cousins&amp;kind=page" aria-current="page">Pages <small>1</small></a>`) {
		t.Errorf("/browse?in=Cousins&kind=page lacks its path or its chip")
	} else if !strings.Contains(cousins, `<details open><summary><span class="sr">Categories in Family</span>`) {
		t.Error("the fold of Family is not open on the path to Cousins")
	}

	people := htmlPageAt(t, l.web, "/browse?kind=person", http.StatusOK)
	wantInPage(t, "/browse?kind=person", people,
		fmt.Sprintf(`<a class="entry named face" href="/pages/%s"><span class="face-picture" aria-hidden="true">`, ids["Ana"]),
		`<span class="first">Ana</span><span class="meta">mentioned on 1 page, last `, `<span class="meta">not mentioned yet</span>`,
		`<form id="pick" method="POST" action="/pages/file" class="inline" data-action="file-pages">`,
		fmt.Sprintf(`<input type="checkbox" name="ids" value="%s" form="pick" aria-label="Select Ana">`, ids["Ana"]),
		`<datalist id="pick-category"><option value="Family"><option value="Cousins">Family › Cousins</datalist>`, `<a href="/browse?kind=person" aria-current="page">People <small>2</small></a>`,
		`<a class="entry" href="/browse?kind=person" aria-current="page"><span class="first">Everything</span>`)
	wantNotInPage(t, "/browse?kind=person", people, `formaction="/pages/tombstone"`, "delete them")

	media := htmlPageAt(t, l.web, "/browse?kind=media", http.StatusOK)
	wantInPage(t, "/browse?kind=media", media, `<ul class="plain tiles">`, `<li class="tile pick">`, `<span class="first">memo.m4a</span><span class="meta">recording · `,
		`<nav class="kinds" aria-label="Media kind"><a href="/browse?kind=media" aria-current="page">All media</a><a href="/browse?kind=media&amp;media=image">Pictures</a>`,
		`formaction="/pages/tombstone"`)
	wantNotInPage(t, "/browse?kind=media", media, `accept="`) // the media list limits no kind of file
	if docs := htmlPageAt(t, l.web, "/browse?kind=document", http.StatusOK); !strings.Contains(docs, `<span class="first">scan.pdf</span><span class="meta">application/pdf · `) {
		t.Error("the documents lack their row")
	}
	if metrics := htmlPageAt(t, l.web, "/browse?kind=metric", http.StatusOK); !strings.Contains(metrics, `<span class="first">Weight</span><span class="meta">kg<span class="badge category">Cousins</span></span>`) ||
		!strings.Contains(metrics, `<span class="first">Mood</span><span class="meta">of 5</span>`) || !strings.Contains(metrics, `<a href="/metrics">The values are on the Metrics board</a>`) {
		t.Errorf("the metrics lack their rows or the way to the board: %s", metrics[strings.Index(metrics, "<main"):])
	}
	pages := htmlPageAt(t, l.web, "/browse", http.StatusOK)
	// On Pages in Everything a category is a row with its box and its badge; the fold of Family is closed.
	wantInPage(t, "/browse", pages, `<a href="/browse?ghosts=1&amp;kind=page">Ghost pages 0</a>`,
		fmt.Sprintf(`<li class="pick"><label class="pick-box"><input type="checkbox" name="ids" value="%s" form="pick" aria-label="Select Family"></label>`, ids["Family"]),
		`<span class="first">Family</span><span class="meta"><span class="badge category">category</span></span>`,
		`<details><summary><span class="sr">Categories in Family</span>`,
		`formaction="/pages/tombstone"`, "type a category or delete them")
	// The form of the catalog's browse, on /actions: an optional select starts empty, so the form sends only what the
	// reader picks, and the empty values it sends are the defaults.
	wantInPage(t, "/actions", htmlPageAt(t, l.web, "/actions", http.StatusOK), `<form method="GET" action="/browse" data-action="browse">`, `<select name="kind"><option></option><option>page</option>`,
		`<select name="media"><option></option><option>image</option>`)
	if blank := htmlPageAt(t, l.web, "/browse?kind=&in=&media=&limit=&offset=", http.StatusOK); !strings.Contains(blank, `<a href="/browse?kind=page" aria-current="page">Pages <small>5</small></a>`) {
		t.Error("the blank form of browse does not give the pages")
	}
	// The ghost list looks at everything: the tree marks Everything.
	if ghosts := htmlPageAt(t, l.web, "/browse?ghosts=1&kind=page", http.StatusOK); !strings.Contains(ghosts, `<a class="entry" href="/browse?kind=page" aria-current="page"><span class="first">Everything</span>`) {
		t.Error("the ghost list marks no branch of the tree")
	}
	for path, want := range map[string]string{
		"/browse?ghosts=1":                "No ghost page: every page has text or a link.",
		"/browse?kind=document&unfiled=1": "Everything of this kind is in a category.",
		"/browse?in=Cousins&kind=person":  "Nothing of this kind is filed in Cousins.",
		"/browse?kind=media&in=Alpha":     "Nothing of this kind is filed in Alpha.",
	} {
		if body := htmlPageAt(t, l.web, path, http.StatusOK); !strings.Contains(body, `<p class="empty">`+want+`</p>`) {
			t.Errorf("%s lacks its empty state %q", path, want)
		}
	}
}

// The category tree of Browse (docs/plans/117-one-browse-view.md, rule 3): the categories by path with a count each,
// beside the list, and Not filed at its end.

func TestBrowseDrawsTheTreeAndTheUnfiledPages(t *testing.T) {
	l := emptyLife(t)
	ids := map[string]string{}
	for _, title := range []string{"Japan 2031", "Alps 2030", "Loose note", "Gone"} {
		ids[title] = pageIDOf(t, href(l.do(t, "/", "create-page", map[string]string{"title": title}), "self"))
	}
	l.do(t, "/", "capture", map[string]string{"text": "a day"})
	file := func(id, category string) {
		t.Helper()
		l.do(t, "/pages/"+id, "file-pages", map[string]string{"category": category})
	}
	file(ids["Japan 2031"], "Travel")
	file(ids["Alps 2030"], "Travel")
	file(ids["Gone"], "Travel")
	travel := pageIDOf(t, href(must(t)(l.api.Get(t.Context(), "/pages?title=Travel")), "self"))
	file(travel, "Life")
	l.do(t, "/pages/"+ids["Gone"], "tombstone-pages", nil)

	body := htmlPageAt(t, l.web, "/browse?unfiled=1", http.StatusOK)
	rows := regexp.MustCompile(`<li(?: class="branch")?><a class="entry" href="/browse\?in=(\w+)"><span class="first">([^<]*)</span><span class="meta">(\d+)</span></a>`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, r := range rows {
		got = append(got, r[2]+"="+r[3])
	}
	// The tree is drawn once: a fold that a wide column holds open. Life holds Travel; Travel holds the two live pages,
	// not the tombstone.
	if want := "Life=1 Travel=2"; strings.Join(got, " ") != want {
		t.Errorf("tree rows: %v, want %s", got, want)
	}
	// Travel is in the fold of Life (docs/plans/118-fold-and-find.md, rule 1), closed: the list is not on either.
	if want := `<li class="branch"><a class="entry" href="/browse?in=Life"><span class="first">Life</span><span class="meta">1</span></a><a class="tree-new" href="/browse?in=Life&amp;kind=page&amp;new=create-page" aria-label="New page in Life">+</a><details><summary><span class="sr">Categories in Life</span></summary><ul class="plain rows tree"><li><a class="entry" href="/browse?in=Travel">`; strings.Count(body, want) != 1 {
		t.Errorf("the tree does not hold Travel in the closed fold of Life")
	}
	if !strings.Contains(body, `<a class="entry" href="/browse?unfiled=1" aria-current="page"><span class="first">Not filed</span>`) ||
		!strings.Contains(body, `href="/pages/`+ids["Loose note"]+`"><span class="first">Loose note</span>`) {
		t.Error("Not filed is not marked, or does not list the loose page")
	}
	// Not filed: not a category (Life), not a filed page, not a day page, not a tombstone.
	wantNotInPage(t, "/browse?unfiled=1", body, `<span class="first">Gone</span>`, `<span class="first">Japan 2031</span>`)
}

func TestBrowseWithNoCategory(t *testing.T) {
	l := emptyLife(t)
	body := htmlPageAt(t, l.web, "/browse?unfiled=1", http.StatusOK)
	tree := regexp.MustCompile(`(?s)<nav aria-label="Categories">(.*?)</nav>`).FindStringSubmatch(body)
	if tree == nil || strings.Count(tree[1], "<li") != 4 || !strings.Contains(tree[1], "Everything") || !strings.Contains(tree[1], "Not filed") || !strings.Contains(tree[1], `<span class="first">Deleted, every kind</span><span class="meta">0</span>`) {
		t.Errorf("an empty tree is not Everything and Not filed: %v", tree)
	}
	if !strings.Contains(body, `<p class="empty">Everything of this kind is in a category.</p>`) {
		t.Error("an empty Not filed says nothing")
	}
}

// TestTheGhostPagesSayWhatTheyAre: the ghost list of Browse says what a ghost page is under its title, and an empty
// list says why it is empty, not how to make a page.
func TestTheGhostPagesSayWhatTheyAre(t *testing.T) {
	l := emptyLife(t)
	wantInPage(t, "/browse?ghosts=1", htmlPageAt(t, l.web, "/browse?ghosts=1", http.StatusOK),
		`<nav class="crumbs" aria-label="Where"><a href="/browse">Browse</a><span aria-current="page">Ghost pages</span></nav>`, `<h1>Ghost pages</h1>`, `<p class="sub"><span>Empty pages, older than 30 days, that nothing links to or from.</span></p>`,
		`<p class="empty">No ghost page: every page has text or a link.</p>`)
}

// TestBrowseListsTheDeletedSoTheyCanBeRestored: a tombstone is in Browse's list of the deleted, and its row opens its
// page, which offers revive (docs/plans/131-the-pareto-pass.md, part 1). A live page is not in the list.
func TestBrowseListsTheDeletedSoTheyCanBeRestored(t *testing.T) {
	l := emptyLife(t)
	gone := l.do(t, "/", "create-page", map[string]string{"title": "Gone"})
	kept := l.do(t, "/", "create-page", map[string]string{"title": "Kept"})
	l.do(t, "/pages/"+pageIDOf(t, href(gone, "self")), "tombstone-pages", nil)
	body := htmlPageAt(t, l.web, "/browse?deleted=1", http.StatusOK)
	wantInPage(t, "/browse?deleted=1", body, `href="`+href(gone, "self")+`"`)
	wantNotInPage(t, "/browse?deleted=1", body, `href="`+href(kept, "self")+`"`)
	// The page of a deleted page offers Restore.
	wantInPage(t, href(gone, "self"), htmlPageAt(t, l.web, href(gone, "self"), http.StatusOK), `data-action="revive"`)
}

// TestBrowseMovesAndFilesFromTheList: a row offers Move and its other categories; inside a category the bar moves and
// removes; in Everything it adds; the Move link opens the one bar with the row checked and returns to the quiet list.
func TestBrowseMovesAndFilesFromTheList(t *testing.T) {
	l, ids := browseLife(t)
	ana := ids["Ana"]
	people := htmlPageAt(t, l.web, "/browse?kind=person", http.StatusOK)
	wantInPage(t, "/browse?kind=person", people,
		`<a class="row-move" href="/browse?ids=`+ana+`&amp;kind=person&amp;new=file-pages">Move</a>`,
		`<span class="first">Ana</span><span class="meta">mentioned on 1 page, last `,
		`<span class="badge category">Family</span></span></a>`,
		`<button class="primary">Add to</button>`)
	if strings.Contains(people, `name="from"`) || strings.Contains(people, `class="pick-on" checked`) {
		t.Error("Everything offers Remove from, or opens Select without being asked")
	}
	in := htmlPageAt(t, l.web, "/browse?in=Family&kind=page", http.StatusOK)
	wantInPage(t, "/browse?in=Family&kind=page", in, `name="move" value="1" checked`, `<button class="primary">Move to</button>`,
		`<button form="pick" name="from" value="Family" formnovalidate>Remove from Family</button>`)
	wantNotInPage(t, "/browse?in=Family&kind=page", in, `<span class="badge category">Family</span>`) // a row in Family is not badged with Family
	asked := htmlPageAt(t, l.web, "/browse?ids="+ana+"&kind=person&new=file-pages", http.StatusOK)
	wantInPage(t, "the page of the Move link", asked, `class="pick-on" checked`, `name="ids" value="`+ana+`" form="pick" checked aria-label="Select Ana"`,
		`<input type="hidden" name="back" value="/browse?kind=person">`)
	wantInPage(t, "/browse?unfiled=1", htmlPageAt(t, l.web, "/browse?unfiled=1", http.StatusOK),
		`<nav class="crumbs" aria-label="Where"><a href="/browse">Browse</a><span aria-current="page">Not filed</span></nav>`, `<h1>Not filed</h1>`,
		`<li class="tree-foot"><a href="/browse?kind=page&amp;new=file-pages">New category</a></li>`)
}

// TestBrowseCountsTheDeletedOnEveryKind: the tombstones are one list of every kind, so its link and its count are in
// the category tree, and every kind's list draws them, not only the list of pages.
func TestBrowseCountsTheDeletedOnEveryKind(t *testing.T) {
	l := emptyLife(t)
	old := l.do(t, "/", "create-page", map[string]string{"title": "Old idea"})
	l.do(t, "/pages/"+pageIDOf(t, href(old, "self")), "tombstone-pages", nil)
	want := `<li><a class="entry" href="/browse?deleted=1"><span class="first">Deleted, every kind</span><span class="meta">1</span></a></li>`
	for _, path := range []string{"/browse", "/browse?kind=person", "/browse?kind=media", "/browse?kind=metric", "/browse?unfiled=1"} {
		wantInPage(t, path, htmlPageAt(t, l.web, path, http.StatusOK), want)
	}
	wantInPage(t, "the deleted list", htmlPageAt(t, l.web, "/browse?deleted=1", http.StatusOK), `<a class="entry" href="/browse?deleted=1" aria-current="page">`)
}

// TestNewCategoryOpensTheSelectBar: a page becomes a category when something is filed in it, so the link of the tree
// opens the Select bar with the words that say so, not the New page form.
func TestNewCategoryOpensTheSelectBar(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "create-page", map[string]string{"title": "Loose note"})
	wantInPage(t, "the list", htmlPageAt(t, l.web, "/browse?kind=page&new=file-pages", http.StatusOK), `<a href="/browse?kind=page&amp;new=file-pages">New category</a>`,
		`<input type="checkbox" class="pick-on" checked>`,
		`<span class="desc">Check what goes in it, then type its name. A new name makes a new category.</span>`)
	wantNotInPage(t, "the plain list", htmlPageAt(t, l.web, "/browse?kind=page", http.StatusOK), "then type its name", `class="pick-on" checked`) // the Select bar is not open unasked
}

// TestTheHeadingOfBrowseNamesTheKind: with no category chosen, the heading is the kind's name, not Everything.
func TestTheHeadingOfBrowseNamesTheKind(t *testing.T) {
	l := emptyLife(t)
	h1 := regexp.MustCompile(`<h1>([^<]*)</h1>`)
	for kind, want := range map[string]string{"page": "Pages", "person": "People", "media": "Media", "document": "Documents", "metric": "Metrics"} {
		m := h1.FindStringSubmatch(htmlPageAt(t, l.web, "/browse?kind="+kind, http.StatusOK))
		if m == nil || m[1] != want {
			t.Errorf("kind %s: the heading is %v, want %s", kind, m, want)
		}
	}
}

// valuesAFormSends is what a browser sends with the form of page whose start tag holds mark (id="pick", or its action):
// the value of each hidden input and each text input of the form, then the values that the reader gives (typed) in place
// of these. Each name of typed must be a control of the form, or of a box outside it that joins it by its id
// (form="pick"), else the test fails: a form that draws no control of a field cannot send it.
func valuesAFormSends(t *testing.T, page, mark string, typed url.Values) url.Values {
	t.Helper()
	m := regexp.MustCompile(`(?s)(<form\b[^>]*` + regexp.QuoteMeta(mark) + `[^>]*>)(.*?)</form>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no form holds %s", mark)
	}
	controls := map[string]bool{}
	for _, n := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(m[2], -1) {
		controls[n[1]] = true
	}
	if id := regexp.MustCompile(`\sid="([^"]+)"`).FindStringSubmatch(m[1]); id != nil {
		for _, n := range regexp.MustCompile(`name="([^"]+)"[^>]*\sform="`+regexp.QuoteMeta(id[1])+`"`).FindAllStringSubmatch(page, -1) {
			controls[n[1]] = true
		}
	}
	sent := url.Values{}
	for _, in := range regexp.MustCompile(`<input type="(?:hidden|text)" name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(m[2], -1) {
		sent.Add(in[1], html.UnescapeString(in[2]))
	}
	for name, values := range typed {
		if !controls[name] {
			t.Fatalf("the form %s has no control %s", mark, name)
		}
		sent[name] = values
	}
	return sent
}

// isReceiptLocation says whether location is the path back with the one receipt of a toast in its query, and nothing
// else changed: a write returns to the page it came from, and the toast is at that address.
func isReceiptLocation(location, back string) bool {
	got, err := url.Parse(location)
	want, wantErr := url.Parse(back)
	if err != nil || wantErr != nil {
		return false
	}
	query := got.Query()
	receipt := query["feedback"]
	query.Del("feedback")
	return len(receipt) == 1 && len(receipt[0]) == 64 && got.Path == want.Path && query.Encode() == want.Query().Encode()
}

// refererOnWeb is the Referer of a form that was sent from the page at path of the host of the requests of the tests.
func refererOnWeb(path string) string { return "http://example.com" + path }

// TestTheListsDeleteWhatIsChecked follows the browser: the media list of Browse draws a box on each tile for one form,
// the Delete button of the form posts the checked ids to tombstone-pages and returns to the list with the count, and a
// post with no box checked comes back to the list with the refusal.
func TestTheListsDeleteWhatIsChecked(t *testing.T) {
	l := emptyLife(t)
	var files []string
	for i, title := range []string{"Scan 1.jpg", "Scan 2.jpg"} {
		e := l.do(t, "/", "add-file", map[string]string{"title": title, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+7), 32), "mime": "image/jpeg"})
		files = append(files, pageIDOf(t, href(e, "self")))
	}

	list := htmlPageAt(t, l.web, "/browse?kind=media", http.StatusOK)
	wantInPage(t, "the media list", list, `<form id="pick" method="POST" action="/pages/file" class="inline" data-action="file-pages">`,
		`<input type="hidden" name="back" value="/browse?kind=media">`, `<span class="desc">Check the rows, then type a category or delete them. A new name makes a new category.</span>`,
		`<button class="danger" formaction="/pages/tombstone" formnovalidate data-action="tombstone-pages">Delete</button></form></div></div>`,
		`<input type="checkbox" name="ids" value="`+files[0]+`" form="pick" aria-label="Select Scan 1.jpg">`, `name="ids" value="`+files[1]+`"`)
	wantNotInPage(t, "the people list", htmlPageAt(t, l.web, "/browse?kind=person", http.StatusOK), `formaction="/pages/tombstone"`)
	// The bar comes after the rows, so a CSS counter can count the checked boxes on its button (plan 108).
	if bar, last := strings.Index(list, `class="pick-bar"`), strings.LastIndex(list, `name="ids"`); bar < last || !strings.Contains(list[bar:], "</form>") {
		t.Error("the bar is not after the rows")
	}
	// With no box checked, Delete is refused: the error page says so and leads back to the list.
	rec := postForm(l.web, "/pages/tombstone", valuesAFormSends(t, list, `id="pick"`, nil), "Referer", refererOnWeb("/browse?kind=media"))
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "missing field ids") ||
		!strings.Contains(body, `<a href="/browse?kind=media">Back</a>`) {
		t.Errorf("no box checked: %d %.300s", rec.Code, body)
	}
	rec = postForm(l.web, "/pages/tombstone", valuesAFormSends(t, list, `id="pick"`, url.Values{"ids": files}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the form: %d %.300s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !isReceiptLocation(loc, "/browse?kind=media") {
		t.Fatalf("the form returned to %q", loc)
	}
	after := htmlPageAt(t, l.web, loc, http.StatusOK)
	wantInPage(t, "the list after the delete", after, "Deleted 2 pages.", `<a href="/browse?deleted=1">See the deleted pages</a>`)
	wantNotInPage(t, "the list after the delete", after, ">Undo<", `href="/pages/`+files[0]+`"`, `href="/pages/`+files[1]+`"`)
}

// TestDeletingOnePageOffersItsUndo: the toast of a delete of one page carries a form that restores that page and
// returns to where the owner is; the page is live again after the form is sent.
func TestDeletingOnePageOffersItsUndo(t *testing.T) {
	l := emptyLife(t)
	id := pageIDOf(t, href(l.do(t, "/", "create-page", map[string]string{"title": "Old idea"}), "self"))
	rec := postForm(l.web, "/pages/tombstone", url.Values{"ids": {id}, "back": {"/browse"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the delete: %d %.300s", rec.Code, rec.Body.String())
	}
	toast := htmlPageAt(t, l.web, rec.Header().Get("Location"), http.StatusOK)
	if !strings.Contains(toast, "Deleted 1 page.") || !strings.Contains(toast, `<form class="undo" method="post" action="/pages/`+id+`/revive">`) ||
		!strings.Contains(toast, ">Undo<") || strings.Contains(toast, "See the deleted pages") {
		t.Fatalf("the toast: %.900s", toast)
	}
	if rec = postForm(l.web, "/pages/"+id+"/revive", url.Values{"back": {"/browse"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("the undo: %d %.300s", rec.Code, rec.Body.String())
	}
	if slices.Contains(must(t)(l.api.Get(t.Context(), "/pages/"+id)).Class, "deleted") {
		t.Error("the page is still deleted after the Undo")
	}
}

// TestThePickBarSendsOnlyWhatTheAPIAllows: every control of the pick bar of a list is a field of file-pages or the field
// back of the browser, and its Delete button leads to tombstone-pages.
func TestThePickBarSendsOnlyWhatTheAPIAllows(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "add-file", map[string]string{"title": "Scan 1.jpg", "sha256": strings.Repeat("07", 32), "mime": "image/jpeg"})
	catalog := must(t)(l.api.Get(t.Context(), "/actions"))
	list := htmlPageAt(t, l.web, "/browse?kind=media", http.StatusOK)
	bar := regexp.MustCompile(`(?s)<form id="pick"[^>]*>(.*?)</form>`).FindStringSubmatch(list)
	if bar == nil {
		t.Fatal("the media list has no pick bar")
	}
	allowed := []string{"back"}
	for _, f := range act(t, catalog, "file-pages").Fields {
		allowed = append(allowed, f.Name)
	}
	for _, n := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(bar[1], -1) {
		if !slices.Contains(allowed, n[1]) {
			t.Errorf("the pick bar sends %s, which file-pages does not list", n[1])
		}
	}
	for _, n := range regexp.MustCompile(`name="([^"]+)"[^>]*\sform="pick"`).FindAllStringSubmatch(list, -1) {
		if !slices.Contains(allowed, n[1]) {
			t.Errorf("a box of the list sends %s, which file-pages does not list", n[1])
		}
	}
	if want := `<button class="danger" formaction="` + act(t, catalog, "tombstone-pages").Href + `"`; !strings.Contains(bar[1], want) {
		t.Errorf("the Delete button does not lead to tombstone-pages: want %s", want)
	}
	if view.SharedForms["tombstone-pages"] != "file-pages" {
		t.Errorf("SharedForms = %v", view.SharedForms)
	}
}

// TestEveryListSelectsTheSameWay: the lists of Browse and the Metrics board draw one pick bar, after their boxes, with
// Select and a button for each action the list offers: the form of file-pages (id pick), and Delete pages as its second
// button; a list of metrics offers no Delete of several.
func TestEveryListSelectsTheSameWay(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	for title, f := range map[string]struct{ mime, sum string }{"Scan.jpg": {"image/jpeg", "ab"}, "Notes.pdf": {"application/pdf", "cd"}} {
		l.do(t, "/", "add-file", map[string]string{"title": title, "sha256": strings.Repeat(f.sum, 32), "mime": f.mime})
	}
	for _, list := range []struct{ path, put, del string }{
		{"/browse?kind=media", "/pages/file", "/pages/tombstone"}, {"/browse?kind=document", "/pages/file", "/pages/tombstone"},
		{"/browse?kind=metric", "/pages/file", ""}, {"/metrics", "/pages/file", ""},
	} {
		body := htmlPageAt(t, l.web, list.path, http.StatusOK)
		if strings.Count(body, `id="pick"`) != 1 || !strings.Contains(body, `<label class="pick-toggle button small"><input type="checkbox" class="pick-on">Select</label>`) {
			t.Errorf("%s has no one pick bar with Select", list.path)
			continue
		}
		if bar, last := strings.Index(body, `id="pick"`), strings.LastIndex(body, `form="pick"`); last < 0 || bar < last {
			t.Errorf("%s: the bar is not after its boxes", list.path)
		}
		put := strings.Contains(body, `<form id="pick" method="POST" action="`+list.put+`" class="inline" data-action="file-pages">`) &&
			strings.Contains(body, `<button class="primary">Add to</button>`)
		del := strings.Contains(body, `<button class="danger" formaction="`+list.del+`" formnovalidate data-action="tombstone-pages">Delete</button></form>`)
		if put != (list.put != "") || del != (list.del != "") {
			t.Errorf("%s: Add to %v, Delete %v; want %v, %v", list.path, put, del, list.put != "", list.del != "")
		}
	}
}
