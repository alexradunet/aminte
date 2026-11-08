package api_test

import (
	"fmt"
	"strings"
	"testing"

	"aminte/client"
	"aminte/internal/api/apitest"
)

// walk follows a list's next links from first, returning every item title and the page sizes seen.
func walk(t *testing.T, c *client.Client, first string) (titles []string, sizes []int) {
	t.Helper()
	for href, n := first, 0; href != ""; n++ {
		if n > 20 {
			t.Fatal("more than 20 pages")
		}
		e := apitest.Must(t)(c.Get(t.Context(), href))
		items := 0
		for _, l := range e.Entities {
			if l.Rel[0] == "item" {
				titles = append(titles, l.Title)
				items++
			}
		}
		sizes = append(sizes, items)
		prev, next := apitest.Href(e, "prev"), apitest.Href(e, "next")
		if (n == 0) != (prev == "") {
			t.Errorf("page %d of %s: prev=%q", n, first, prev)
		}
		href = next
	}
	return titles, sizes
}

func TestListsPage(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", "2031-03-08") // the stream holds no day after today
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	for i := 1; i <= 7; i++ {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-person"), map[string]string{"title": fmt.Sprintf("Person %d", i)}))
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, catalog, "capture"), map[string]string{"day": fmt.Sprintf("2031-03-%02d", i), "text": fmt.Sprintf("needleword [[Ghost %d]]", i)}))
	}
	for _, tc := range []struct {
		list   string
		titles []string
	}{
		{"/browse?kind=person&limit=3", []string{"Person 1", "Person 2", "Person 3", "Person 4", "Person 5", "Person 6", "Person 7"}},
		{"/days?limit=3", []string{"2031-03-07", "2031-03-06", "2031-03-05", "2031-03-04", "2031-03-03", "2031-03-02", "2031-03-01"}},
	} {
		titles, sizes := walk(t, c, tc.list)
		if strings.Join(titles, ",") != strings.Join(tc.titles, ",") || fmt.Sprint(sizes) != "[3 3 1]" {
			t.Errorf("%s: titles=%v sizes=%v", tc.list, titles, sizes)
		}
	}
	// Ghost pages (empty, unlinked, older than 30 days) are paged by the same reader; the core test builds them.
	// Search pages by rank then id, every day page once; the q survives in the links.
	titles, sizes := walk(t, c, "/search?q=needleword&limit=3")
	if len(titles) != 7 || fmt.Sprint(sizes) != "[3 3 1]" {
		t.Errorf("search: titles=%v sizes=%v", titles, sizes)
	}
	seen := map[string]bool{}
	for _, x := range titles {
		if seen[x] {
			t.Errorf("search repeated %s", x)
		}
		seen[x] = true
	}
	// Defaults are the counts of before; the properties say what page this is.
	for _, tc := range []struct {
		list  string
		limit int
	}{{"/days", 30}, {"/browse", 200}, {"/browse?kind=person", 200}, {"/browse?kind=media", 200}, {"/browse?ghosts=1", 200}, {"/search?q=needleword", 50}} {
		e := apitest.Must(t)(c.Get(t.Context(), tc.list))
		p, _ := e.Properties.(map[string]any)
		if fmt.Sprint(p["limit"]) != fmt.Sprint(tc.limit) || fmt.Sprint(p["offset"]) != "0" || apitest.Href(e, "next") != "" || apitest.Href(e, "prev") != "" {
			t.Errorf("%s: limit=%v offset=%v next=%q prev=%q", tc.list, p["limit"], p["offset"], apitest.Href(e, "next"), apitest.Href(e, "prev"))
		}
	}
	// The page past the end is empty (its links are TestPaged's); bad values are refused with the code invalid, on
	// each list that reads them.
	if last := apitest.Must(t)(c.Get(t.Context(), "/browse?kind=person&limit=3&offset=9")); len(last.Entities) != 0 {
		t.Errorf("past the end: %d items", len(last.Entities))
	}
	for _, bad := range []string{"/browse?limit=0", "/browse?limit=501", "/browse?limit=x", "/browse?offset=-1", "/browse?offset=x", "/days?limit=0", "/search?q=a&limit=0"} {
		_, err := c.Get(t.Context(), bad)
		if apitest.Status(err) != 422 {
			t.Errorf("%s: %v, want a 422", bad, err)
			continue
		}
		if code, _ := codeOf(t, err); code != "invalid" {
			t.Errorf("%s: code=%q, want invalid", bad, code)
		}
	}
	// The links of a page past the first.
	people := apitest.Must(t)(c.Get(t.Context(), "/browse?kind=person&limit=3&offset=3"))
	if apitest.Href(people, "prev") != "/browse?kind=person&limit=3&offset=0" || apitest.Href(people, "next") != "/browse?kind=person&limit=3&offset=6" {
		t.Errorf("people page 2: prev %q next %q", apitest.Href(people, "prev"), apitest.Href(people, "next"))
	}
	if first := apitest.Must(t)(c.Get(t.Context(), "/search?q=needleword&limit=3")); apitest.Href(first, "next") == "" || apitest.Href(first, "prev") != "" {
		t.Errorf("search page 1 pager: next %q prev %q", apitest.Href(first, "next"), apitest.Href(first, "prev"))
	}
}
