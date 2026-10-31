package web_test

// The links of the pages lead to pages that answer. A link to a period that has no page is absent, and no link sends a
// query that its page does not read.

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestHomeAndADayLinkOnlyToServedPaths: every link of the page of every action and of a day page opens a page, and no link
// sends a query that its page does not read. A link may lead to a file of web, to a page of the core or to a page of an app:
// web answers the whole set, and none is 404.
func TestHomeAndADayLinkOnlyToServedPaths(t *testing.T) {
	l := lifeStartingOn(t, today)
	l.do(t, "/days/2031-05-03", "capture", map[string]string{"text": "Synthetic entry"})
	for _, page := range []string{"/actions", "/days/2031-05-03", "/days/" + today} {
		body := ask(l.web, http.MethodGet, page).Body.String()
		links := regexp.MustCompile(`href="(/[^"]*)"`).FindAllStringSubmatch(body, -1)
		if len(links) < 10 {
			t.Errorf("%s holds %d links, want many: the test reads the wrong page", page, len(links))
		}
		for _, m := range links {
			to, _, _ := strings.Cut(html.UnescapeString(m[1]), "#") // a browser sends no fragment
			if strings.HasPrefix(to, "/metrics?show=") {
				t.Errorf("%s links %s: /metrics reads no show", page, to)
			}
			if rec := ask(l.web, http.MethodGet, to); rec.Code == http.StatusNotFound {
				t.Errorf("%s links %s, which answers 404", page, to)
			}
		}
	}
}

// TestTheEdgesOfTheCalendarLinkOnlyToPages: the days run from 0000-01-01 to 9999-12-31, and a year has a page from 0001 to
// 9999. The first and the last day, month and year answer, and each of their links leads to a page that answers. Where the
// entity of the core has no prev or next link, the page has no step with that rel, and no page has an empty href. The links
// of the entity itself are held by the tests of the core.
func TestTheEdgesOfTheCalendarLinkOnlyToPages(t *testing.T) {
	l := lifeStartingOn(t, "2026-10-12")
	calendarHref := regexp.MustCompile(`href="(/(?:days|months|years)/[^"]+)"`)
	checked := map[string]bool{} // the links already fetched: the rail repeats the days of this month
	for _, page := range []string{"/days/0000-01-01", "/days/0000-01-02", "/days/9999-12-31", "/months/0000-01", "/months/9999-12", "/years/0001", "/years/9999"} {
		e := must(t)(l.api.Get(t.Context(), page))
		body := wholePageAt(t, l, page, http.StatusOK)
		for _, m := range calendarHref.FindAllStringSubmatch(body, -1) {
			if checked[m[1]] {
				continue
			}
			checked[m[1]] = true
			// a browser keeps the fragment (#capture of the Today strip) to itself; the test would send it in the path
			if code := ask(l.web, http.MethodGet, strings.SplitN(m[1], "#", 2)[0]).Code; code != http.StatusOK && code != http.StatusSeeOther {
				t.Errorf("%s: it links %s, which answers %d", page, m[1], code)
			}
		}
		for _, rel := range []string{"prev", "next"} {
			if drawn := strings.Contains(body, `rel="`+rel+`"`); href(e, rel) == "" && drawn {
				t.Errorf("%s has no %s link, and its page draws a step with rel=%q", page, rel, rel)
			} else if href(e, rel) != "" && !drawn {
				t.Errorf("%s links %s, and its page draws no step with rel=%q", page, rel, rel)
			}
		}
		if strings.Contains(body, `href=""`) {
			t.Errorf("%s: its page has an empty href", page)
		}
	}
	if len(checked) < 20 {
		t.Errorf("the seven pages hold %d calendar links, want many: the test reads the wrong pages", len(checked))
	}
}

// TestTheFirstMonthOfTheDaysIsACalendar: January 0000 holds the first day that a date can name. Its page answers with its 31
// days, a written day on its first day with the first line of the entry, and the plain day after it. The days of the entity
// are held by the tests of the core.
func TestTheFirstMonthOfTheDaysIsACalendar(t *testing.T) {
	l := lifeStartingOn(t, today)
	l.do(t, "/days/0000-01-01", "capture", map[string]string{"text": "The first day."})
	l.do(t, "/", "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	l.do(t, "/", "record", map[string]string{"metric": "weight", "day": "0000-01-03", "value": "70"})
	body := wholePageAt(t, l, "/months/0000-01", http.StatusOK)
	if !strings.Contains(body, "January 0000") {
		t.Errorf("the page of /months/0000-01 does not name its month: %.300s", mainPartOf(body))
	}
	if n := strings.Count(body, `<td><a href="/days/0000-01-`) + strings.Count(body, `<td class="written"><a href="/days/0000-01-`); n != 31 {
		t.Errorf("the calendar of January 0000 holds %d days, want 31", n)
	}
	for _, want := range []string{`<td class="written"><a href="/days/0000-01-01"><span class="n">1</span><span class="first">(00:00) The first day.</span></a></td>`,
		`<td><a href="/days/0000-01-02"><span class="n">2</span></a></td>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the calendar of January 0000 lacks %s", want)
		}
	}
}
