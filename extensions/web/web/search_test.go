package web_test

// The search page as the browser draws it: the groups of hits, the order links, the empty search, the offer to make a
// page, the fold of the paging fields and the links to the next and the previous page. They run against the real core.

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestSearchListsTitlesFirst: the search lists the pages whose title starts with the words before the pages whose words
// match, so a page is one search away by its title, and a page is in one group only. An old name that a rename kept
// finds its page.
func TestSearchListsTitlesFirst(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "create-page", map[string]string{"title": "Garden log", "body": "The roses."})
	l.do(t, "/", "create-page", map[string]string{"title": "Roses", "body": "In the garden."})
	l.do(t, "/", "create-page", map[string]string{"title": "Garden"})
	draft := l.do(t, "/", "create-page", map[string]string{"title": "Gardening draft"})
	l.do(t, href(draft, "self"), "rename", map[string]string{"title": "Allotment"})

	body := pageAt(t, l.web, "/search?q=garden")
	// The hits are in groups by kind (docs/plans/130-the-explore-pass.md, part 6): these are all pages.
	titles, words := strings.Index(body, `aria-label="Pages by title"`), strings.Index(body, `<section class="result-group" aria-label="Pages">`)
	if titles < 0 || words < 0 || titles > words {
		t.Fatalf("the search does not list the pages by title first:\n%.3000s", body)
	}
	if !strings.Contains(body[titles:words], `>Garden log</a>`) || strings.Contains(body[titles:words], `>Roses</a>`) {
		t.Errorf("the pages by title: %.600s", body[titles:words])
	}
	if !strings.Contains(body[words:], `>Roses</a>`) {
		t.Errorf("the pages by their words lack Roses: %.600s", body[words:])
	}
	// A page is in one group only: the words group leaves out the pages of the titles group.
	for _, in := range []string{`>Garden log</a>`, `>Garden</a>`, `>Allotment</a>`} {
		if strings.Contains(body[words:], in) {
			t.Errorf("the pages by their words list %s, which the titles list too: %.800s", in, body[words:])
		}
	}
}

// TestASearchOfOnlyBlanksIsTheEmptySearch: a search of spaces, a tab, a no-break space (U+00A0), an em space (U+2003) or an
// ideographic space (U+3000) has no words. It draws the page of the search with no q: a whole page, and no word about
// a search that found nothing.
func TestASearchOfOnlyBlanksIsTheEmptySearch(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "create-page", map[string]string{"title": "Garden", "body": "The roses."})
	for _, q := range []string{"", " ", "  \t ", " ", " ", "　  "} {
		path := "/search?q=" + url.QueryEscape(q)
		page := pageAt(t, l.web, path)
		wantInPage(t, path, page, `<form method="GET" action="/search" class="inline" data-action="search">`, `<summary>How to search</summary>`)
		wantNotInPage(t, path, page, "No page matches.", `class="result-group"`)
	}
}

// TestSearchOrderNewestFirst: the browser links the other order beside the heading of the words group, with the same
// words, and the form has no field order. An unknown order is a 422 page.
func TestSearchOrderNewestFirst(t *testing.T) {
	l := emptyLife(t)
	for _, day := range []string{"2021-05-05", "2098-01-01", "2020-03-01"} {
		l.do(t, "/days/"+day, "capture", map[string]string{"text": "A lantern."})
	}
	l.do(t, "/", "create-page", map[string]string{"title": "Shed", "body": "A lantern."})

	if body := pageAt(t, l.web, "/search?q=lantern"); !strings.Contains(body, `href="/search?order=newest&amp;q=lantern"`) || !strings.Contains(body, ">Newest first</a>") {
		t.Errorf("the best-match page lacks the link Newest first:\n%.2500s", body)
	}
	body := pageAt(t, l.web, "/search?q=lantern&order=newest")
	if !strings.Contains(body, `href="/search?q=lantern"`) || !strings.Contains(body, ">Best match</a>") || strings.Contains(body, ">Newest first</a>") {
		t.Errorf("the newest page lacks the link Best match:\n%.2500s", body)
	}
	if strings.Contains(body, `name="order"`) {
		t.Errorf("the search form draws the field order, which is a link here:\n%.2500s", body)
	}
	for _, bad := range []string{"oldest", "NEWEST", "newest,best"} {
		path := "/search?q=lantern&order=" + url.QueryEscape(bad)
		wantInPage(t, path, pageWithStatus(t, l.web, path, http.StatusUnprocessableEntity), "order must be one of best, newest")
	}
}

// TestASearchWithNoHitOffersTheNewPage: the link makes a page titled with the words searched, unless they hold search
// syntax (a * or a quote), which no title can keep.
func TestASearchWithNoHitOffersTheNewPage(t *testing.T) {
	l := emptyLife(t)
	body := pageAt(t, l.web, "/search?q="+url.QueryEscape("Zebra crossing"))
	wantInPage(t, "the search", body, `<a href="/browse?kind=page&amp;new=create-page&amp;title=Zebra&#43;crossing">Make a page titled ‘Zebra crossing’</a>`)
	for _, q := range []string{"zebra*", `"zebra crossing"`} {
		body := pageAt(t, l.web, "/search?q="+url.QueryEscape(q))
		if !strings.Contains(body, "No page matches.") || strings.Contains(body, "Make a page titled") {
			t.Errorf("q=%s: the link is drawn for search syntax, or the empty words are not", q)
		}
	}
	wantInPage(t, "the New page form", pageAt(t, l.web, "/browse?kind=page&new=create-page&title=Zebra+crossing"), `name="title" value="Zebra crossing"`)
}

// TestTheSearchPageFoldsItsPagingFields: "Rows on a page" and "Rows skipped" are under More fields, not beside the words.
func TestTheSearchPageFoldsItsPagingFields(t *testing.T) {
	l := emptyLife(t)
	page := pageAt(t, l.web, "/search?q=ab")
	fold := regexp.MustCompile(`(?s)<details class="more-fields"><summary>[0-9]+ more fields</summary>(.*?)</details>`).FindStringSubmatch(page)
	if fold == nil || !strings.Contains(fold[1], `name="limit"`) || !strings.Contains(fold[1], `name="offset"`) {
		t.Fatalf("limit and offset are not under More fields:\n%s", page[strings.Index(page, "<main"):])
	}
}

// TestTheListsOfABrowserLinkThePagesBeforeAndAfter: the page of the people list and the page of a search link the page
// before it with rel prev and the page after it with rel next, and the first page of a search has no rel prev. Both keep
// the limit.
func TestTheListsOfABrowserLinkThePagesBeforeAndAfter(t *testing.T) {
	l := emptyLife(t)
	for i := 1; i <= 7; i++ {
		l.do(t, "/", "create-person", map[string]string{"title": fmt.Sprintf("Person %d", i)})
		l.do(t, fmt.Sprintf("/days/2031-03-%02d", i), "capture", map[string]string{"text": fmt.Sprintf("needleword [[Ghost %d]]", i)})
	}
	body := pageAt(t, l.web, "/browse?kind=person&limit=3&offset=3")
	if !strings.Contains(body, `href="/browse?kind=person&amp;limit=3&amp;offset=0" rel="prev"`) || !strings.Contains(body, `href="/browse?kind=person&amp;limit=3&amp;offset=6" rel="next"`) {
		t.Errorf("people page 2: %.600s", body)
	}
	if body := pageAt(t, l.web, "/search?q=needleword&limit=3"); !strings.Contains(body, `rel="next"`) || strings.Contains(body, `rel="prev"`) {
		t.Errorf("search page 1 pager: %.600s", body)
	}
}
