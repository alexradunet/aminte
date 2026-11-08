package api_test

import (
	"fmt"
	"strings"
	"testing"

	"aminte/internal/api/apitest"
)

// weekToday is the local day of the year tests: a Monday, the first day of ISO week 42 of 2026.
const weekToday = "2026-10-12"

// objects is a list property as a list of objects.
func objects(p map[string]any, key string) []map[string]any {
	var out []map[string]any
	for _, x := range p[key].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// TestTheYearHoldsTwelveMonths: the day pages and the current readings of one year, month by month. A reading of the
// year before is not counted.
func TestTheYearHoldsTwelveMonths(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", weekToday)
	for _, day := range []string{"2026-10-05", "2026-10-07", "2026-10-11"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": "A page."}))
	}
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for day, value := range map[string]string{"2025-12-31": "70", "2026-10-01": "71.9", "2026-10-06": "71.6", "2026-10-09": "71.2"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": day, "value": value}))
	}
	y := apitest.Must(t)(c.Get(t.Context(), "/years/2026"))
	p := props(y)
	if y.Class[0] != "year" || y.Title != "2026" || p["year"] != "2026" || p["today"] != weekToday || said(p["written"]) != "3" || said(p["readings"]) != "3" {
		t.Errorf("the year: %v %q %v %v %v %v", y.Class, y.Title, p["year"], p["today"], p["written"], p["readings"])
	}
	months := objects(p, "months")
	if len(months) != 12 || len(y.Entities) != 12 || y.Entities[9].Href != "/months/2026-10" {
		t.Fatalf("%d months, %d links", len(months), len(y.Entities))
	}
	if months[9]["month"] != "2026-10" || said(months[9]["written"]) != "3" || said(months[0]["written"]) != "0" || len(months[1]["days"].([]any)) != 28 || len(months[9]["days"].([]any)) != 31 {
		t.Errorf("months: %v %v %v", months[9]["written"], months[0]["written"], len(months))
	}
	if d := months[9]["days"].([]any)[4].(map[string]any); d["day"] != "2026-10-05" || d["page"] != true {
		t.Errorf("5 October: %v", d)
	}
	for rel, want := range map[string]string{"self": "/years/2026", "prev": "/years/2025", "next": "/years/2027"} {
		if got := apitest.Href(y, rel); got != want {
			t.Errorf("link %s = %q, want %q", rel, got, want)
		}
	}
	if names(y) != "create-page" {
		t.Errorf("actions: %s, want create-page for the year page it does not have: the day and the stream go to a day", names(y))
	}
	if len(objects(props(apitest.Must(t)(c.Get(t.Context(), "/years/2028"))), "months")[1]["days"].([]any)) != 29 {
		t.Error("February 2028 has not 29 days")
	}
	for _, bad := range []string{"20", "abcd", "0000", "20260", "2026-10"} {
		if _, err := c.Get(t.Context(), "/years/"+bad); apitest.Status(err) != 422 {
			t.Errorf("/years/%s: %v", bad, err)
		}
	}
}

// TestHomeLinksThisMonthAndThisYear: Home links this month and this year by their own addresses.
func TestHomeLinksThisMonthAndThisYear(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", weekToday)
	home := apitest.Must(t)(c.Get(t.Context(), "/"))
	if apitest.Href(home, "calendar") != "/months/2026-10" || apitest.Href(home, "year") != "/years/2026" || apitest.Href(home, "week") != "" {
		t.Errorf("Home links: calendar %q, year %q, week %q", apitest.Href(home, "calendar"), apitest.Href(home, "year"), apitest.Href(home, "week"))
	}
}

// said is a property as the client decodes it, written out: a number is a json.Number.
func said(v any) string { return fmt.Sprint(v) }

// TestTheEdgesOfTheCalendarLinkOnlyToPages: the days run from 0000-01-01 to 9999-12-31 (aminte_meta.days), and a
// year has a page from 0001 to 9999. The first and the last day, month and year answer, and each of their links leads
// to a page that answers: a link to a period that has no page is absent.
func TestTheEdgesOfTheCalendarLinkOnlyToPages(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", weekToday)
	for _, tc := range []struct{ page, rels string }{
		{"/days/0000-01-01", "next,month"},
		{"/days/0000-01-02", "prev,next,month"},
		{"/days/9999-12-31", "prev,month"},
		{"/months/0000-01", "next,month"},
		{"/months/9999-12", "prev,month,year"},
		{"/years/0001", "next,month,year"},
		{"/years/9999", "prev,month,year"},
	} {
		e, err := c.Get(t.Context(), tc.page)
		if err != nil {
			t.Errorf("%s: %v", tc.page, err)
			continue
		}
		var rels []string
		for _, rel := range []string{"prev", "next", "up", "month", "year"} {
			href := apitest.Href(e, rel)
			if href == "" {
				continue
			}
			rels = append(rels, rel)
			if _, err := c.Get(t.Context(), href); err != nil {
				t.Errorf("%s: the %s link %s does not answer: %v", tc.page, rel, href, err)
			}
		}
		if got := strings.Join(rels, ","); got != tc.rels {
			t.Errorf("%s links %s, want %s", tc.page, got, tc.rels)
		}
	}
}
