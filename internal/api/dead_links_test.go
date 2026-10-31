package api_test

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestHomeAndADayLinkOnlyToServedPaths: every link and every sub-entity of Home, of the catalog and of a day leads to
// a path that the server serves, and no link sends a query that its page does not read
// (docs/plans/117-one-browse-view.md, step 0).
func TestHomeAndADayLinkOnlyToServedPaths(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/2031-05-03")), "capture"), map[string]string{"text": "Synthetic entry"}))
	for _, page := range []string{"/", "/actions", "/days/2031-05-03", "/days/" + fixedToday} {
		e := apitest.Must(t)(c.Get(t.Context(), page))
		hrefs := []string{}
		for _, l := range append(slices.Clone(e.Links), e.Entities...) {
			hrefs = append(hrefs, l.Href)
		}
		for _, href := range hrefs {
			href, _, _ = strings.Cut(href, "#") // a client sends no fragment
			if !strings.HasPrefix(href, "/") {
				continue
			}
			if strings.HasPrefix(href, "/metrics?show=") {
				t.Errorf("%s links %s: /metrics reads no show", page, href)
			}
			if _, err := c.Get(t.Context(), href); apitest.Status(err) == http.StatusNotFound {
				t.Errorf("%s links %s, which answers 404", page, href)
			}
		}
	}
}

// TestActionDescriptionsNameOnlyTheirFields: a field name in an action's description (a word with an underscore) is a
// field of that action, so a client is never told to send a field the action does not take.
func TestActionDescriptionsNameOnlyTheirFields(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	cat := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	for _, a := range cat.Actions {
		var fields []string
		for _, f := range a.Fields {
			fields = append(fields, f.Name)
		}
		for _, w := range regexp.MustCompile(`\b[a-z]+_[a-z_]+\b`).FindAllString(a.Description, -1) {
			if !slices.Contains(fields, w) {
				t.Errorf("%s: the description names %s, which is not one of its fields %v", a.Name, w, fields)
			}
		}
	}
}

// TestTheCatalogNamesNoRemovedFeature: no action's description or field title tells a client of a feature the
// docs removed: a photo's position (D21), or the tables of tasks, deadlines and occurrences (D23). An MCP agent reads
// these texts as the truth. The words "task" and "due" are free, so this test guards the removed features by their
// routes (/tasks, /deadlines, /occurrences) and by the words occurrence and reminder, not by those words. Each
// pattern names a removed feature, not a bare word, so a true text such as "due to" or the position of a row stays
// free.
func TestTheCatalogNamesNoRemovedFeature(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	removed := regexp.MustCompile(`(?i)(/(tasks|deadlines|occurrences)\b|\b(` + strings.Join([]string{
		`occurrences?`, `reminders?`, // D23
		`(a|the) photo's position`, `positions? of (a|the) photos?`, `and position`, `positions? near`,
		`position as its point`, `the place it was taken`, // D21
	}, "|") + `)\b)`)
	// The texts of the removed features, and two true texts.
	for s, want := range map[string]bool{
		"GET /tasks lists the open tasks.":                                          true,
		"Add a deadline: POST /deadlines.":                                          true,
		"An occurrence repeats on its day.":                                         true,
		"A reminder is sent on the day.":                                            true,
		"A photo's own day (its EXIF date) and position are read from the original": true,
		"its day page gets an at link to the place it was taken in, and shows it":   true,
		"a position near no place is reported":                                      true,
		"at names the place, which takes the position as its point":                 true,
		"Add a note: the day it must be done by is due, and a task is a note.":      false,
		"Refused due to a stale version.":                                           false,
		"The position of a row in the list.":                                        false,
	} {
		if got := removed.MatchString(s); got != want {
			t.Fatalf("the pattern finds a removed feature in %q: %v, want %v", s, got, want)
		}
	}
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		texts := []string{a.Title, a.Description}
		for _, f := range a.Fields {
			texts = append(texts, f.Title)
		}
		for _, s := range texts {
			if w := removed.FindString(s); w != "" {
				t.Errorf("%s says %q: %s", a.Name, w, s)
			}
		}
	}
}
