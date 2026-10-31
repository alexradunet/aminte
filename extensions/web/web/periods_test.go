package web_test

// The period pages as the browser draws them (D30): the week with its seven days and its page, the month as a calendar,
// the year as twelve months, the page of a period with its kind and its view, and the zoom between the levels. They run
// against the real core. Today is Wednesday 2031-05-14, in ISO week 2031-W20.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// mainElementOf is the part of a page between its main tags: what a failure of a test needs to show.
func mainElementOf(page string) string {
	_, rest, _ := strings.Cut(page, `<main id="main">`)
	rest, _, _ = strings.Cut(rest, "</main>")
	return rest
}

func TestAWeekListsItsSevenDaysAndItsPage(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "create-page", map[string]string{"title": "2031-W20", "body": "Finish the report"})

	page := pageAt(t, l.web, "/weeks/2031-W20")
	if strings.Contains(page, "no value") {
		t.Errorf("the week draws a missing value:\n%s", mainElementOf(page))
	}
	if !strings.Contains(page, "<h1>Week 20 of 2031</h1>") {
		t.Errorf("the title of the week:\n%s", mainElementOf(page))
	}
	for d := 12; d <= 18; d++ {
		if want := fmt.Sprintf(`href="/days/2031-05-%02d"`, d); !strings.Contains(page, want) {
			t.Errorf("the week does not list %s", want)
		}
	}
	if strings.Contains(page, `href="/days/2031-05-11"`) || strings.Contains(page, `href="/days/2031-05-19"`) {
		t.Error("the week lists a day outside it")
	}
	wantInPage(t, "the week", page, `aria-label="The page of the period"`, `<h2>The week page</h2>`, "Finish the report", `href="/weeks/2031-W19" rel="prev"`, `aria-current="page">Week</a>`)
	wantNotInPage(t, "the week", page, "has no page yet") // a week with a page does not offer to make it

	today := ask(l.web, http.MethodGet, "/weeks/today")
	if today.Code != http.StatusSeeOther && today.Code != http.StatusFound || today.Header().Get("Location") != "/weeks/2031-W20" {
		t.Errorf("/weeks/today: %d to %q", today.Code, today.Header().Get("Location"))
	}
	for _, bad := range []string{"/weeks/2031-W53", "/weeks/2031-W00", "/weeks/2031-W5", "/weeks/x"} {
		pageWithStatus(t, l.web, bad, http.StatusUnprocessableEntity)
	}
}

func TestAMonthAndAYearShowTheirPages(t *testing.T) {
	l := emptyLife(t)
	if page := pageAt(t, l.web, "/months/2031-05"); strings.Contains(page, "<h2>The month page</h2>") || !strings.Contains(page, "This month has no page yet") {
		t.Errorf("a month with no page:\n%s", mainElementOf(page))
	}
	l.do(t, "/", "create-page", map[string]string{"title": "2031-05", "body": "Plan the month"})
	l.do(t, "/", "create-page", map[string]string{"title": "2031", "body": "Run a marathon"})
	month := pageAt(t, l.web, "/months/2031-05")
	wantInPage(t, "the month", month, `aria-label="The page of the period"`, "Plan the month")
	if strings.Index(month, "Plan the month") > strings.Index(month, `<table class="calendar">`) {
		t.Error("the page of the month is below its calendar")
	}
	wantInPage(t, "the year", pageAt(t, l.web, "/years/2031"), "Run a marathon", `aria-label="Quarters"`, `href="/actions?new=create-page&amp;title=2031-Q4"`)
	// The link of a quarter with no page opens the form of create-page, with the quarter as the title.
	opened := pageAt(t, l.web, "/actions?new=create-page&title=2031-Q4")
	if !strings.Contains(opened, `<details class="action" open>`) || !strings.Contains(opened, `name="title" value="2031-Q4"`) {
		t.Errorf("?new=create-page&title=2031-Q4 does not open the form for the quarter:\n%s", mainElementOf(opened))
	}
	l.do(t, "/", "create-page", map[string]string{"title": "2031-Q4", "body": "Ship it"})
	if after := pageAt(t, l.web, "/years/2031"); !strings.Contains(after, `<a href="/pages/`) || strings.Contains(after, `title=2031-Q4"`) {
		t.Errorf("a quarter with a page still links its opener:\n%s", mainElementOf(after))
	}
}

func TestAPeriodPageNamesItsKindAndItsView(t *testing.T) {
	l := emptyLife(t)
	for key, want := range map[string][]string{
		"2031-W20": {`<span class="kind">week page</span>`, `<a href="/weeks/2031-W20">The week view</a>`},
		"2031-05":  {`<span class="kind">month page</span>`, `<a href="/months/2031-05">The month view</a>`},
		"2031":     {`<span class="kind">year page</span>`, `<a href="/years/2031">The year view</a>`},
		"2031-Q4":  {`<span class="kind">quarter page</span>`, `<a href="/months/2031-10">October</a>`, `<a href="/months/2031-12">December</a>`},
	} {
		e := l.do(t, "/", "create-page", map[string]string{"title": key, "body": "Aim " + key})
		wantInPage(t, "the page "+key, pageAt(t, l.web, href(e, "self")), append(want, "Aim "+key)...)
	}
}

// TestCreatePageMakesAPeriodPageAndRefusesAPeriodThatDoesNotExist: a create-page of a week key makes the period page that
// the week view then shows. The file refuses a week that the year does not have, by the name of its constraint, and the
// browser shows the refusal.
func TestCreatePageMakesAPeriodPageAndRefusesAPeriodThatDoesNotExist(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "create-page", map[string]string{"title": "2031-W43"})
	if page := pageAt(t, l.web, "/weeks/2031-W43"); !strings.Contains(page, `<h2>The week page</h2>`) || strings.Contains(page, "has no page yet") {
		t.Errorf("the week view does not show the page that create-page made:\n%s", mainElementOf(page))
	}
	rec := postForm(l.web, "/pages", url.Values{"title": {"2027-W53"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "entities_period_exists") {
		t.Errorf("create-page of 2027-W53: %d, want 422 naming entities_period_exists: %.300s", rec.Code, rec.Body.String())
	}
}

// TestAPeriodViewWithoutAPageOffersCreatePage: a week, a month and a year with no page draw create-page with the key as
// the title, fixed. With a page they do not.
func TestAPeriodViewWithoutAPageOffersCreatePage(t *testing.T) {
	l := emptyLife(t)
	for path, key := range map[string]string{"/weeks/2031-W20": "2031-W20", "/months/2031-05": "2031-05", "/years/2031": "2031"} {
		wantInPage(t, path, pageAt(t, l.web, path), "has no page yet", `<input type="hidden" name="title" value="`+key+`">`)
		l.do(t, "/", "create-page", map[string]string{"title": key})
		wantNotInPage(t, path+" with a page", pageAt(t, l.web, path), "has no page yet", `<input type="hidden" name="title" value="`+key+`">`)
	}
}

// TestTheYearHoldsTwelveMonths: the page of a year draws its twelve months with the days written and the readings. A
// reading of the year before is not counted.
func TestTheYearHoldsTwelveMonths(t *testing.T) {
	l := lifeStartingOn(t, "2026-10-12") // a Monday, the first day of ISO week 42 of 2026
	for _, day := range []string{"2026-10-05", "2026-10-07", "2026-10-11"} {
		l.do(t, "/days/"+day, "capture", map[string]string{"text": "A page."})
	}
	l.do(t, "/", "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	for day, value := range map[string]string{"2025-12-31": "70", "2026-10-01": "71.9", "2026-10-06": "71.6", "2026-10-09": "71.2"} {
		l.do(t, "/", "record", map[string]string{"metric": "weight", "day": day, "value": value})
	}

	body := pageAt(t, l.web, "/years/2026")
	if n := strings.Count(body, `<div class="month-box">`); n != 12 { // the sidebar draws none
		t.Errorf("%d month boxes on the year's page, want 12", n)
	}
	wantInPage(t, "the year's page", body, `<a href="/months/2026-01">January</a>`, `<a href="/months/2026-12">December</a>`, `<span class="meta">3 written</span>`,
		`<a href="/days/2026-10-12" aria-current="date"`, `<a href="/days/2026-10-05" class="written"`, "3 days written", "3 readings", `rel="prev"`, `rel="next"`)
	if !strings.Contains(pageAt(t, l.web, "/years/2027"), "0 days written") {
		t.Error("a year with no page does not say that no day is written")
	}
	if !strings.Contains(pageAt(t, l.web, "/months/2026-10"), `<a href="/years/2026">Year</a>`) {
		t.Error("a month does not link to its year")
	}
}

// TestTheMonthIsACalendar: each day of a month is a cell with its link, its first line and its mood in words; the
// buttons name the months around it; the week starts on Monday; the calendar has no form to go to a day.
func TestTheMonthIsACalendar(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/days/2031-05-01", "capture", map[string]string{"text": "May day: a walk by the lake.", "mood": "4"})
	l.do(t, "/days/2031-05-14", "capture", map[string]string{"mood": "3"})
	l.do(t, "/days/2031-05-31", "capture", map[string]string{"text": "The last day."})
	gone := l.do(t, "/days/2031-05-20", "capture", map[string]string{"text": "Scratch"})
	l.do(t, href(gone, "page"), "tombstone-pages", nil)

	body := pageAt(t, l.web, "/months/2031-05")
	wantInPage(t, "the calendar", body, `<table class="calendar">`,
		`<td class="written" aria-current="date"><a href="/days/2031-05-14"><span class="n">14</span><span class="meta">mood 3 · okay</span></a></td>`,
		`<td class="written"><a href="/days/2031-05-01"><span class="n">1</span><span class="first">(00:00) May day: a walk by the lake.</span><span class="meta">mood 4 · good</span></a></td>`,
		`<td><a href="/days/2031-05-20"><span class="n">20</span></a></td>`,
		`aria-label="Previous month, April 2031"`, `aria-label="Next month, June 2031"`)
	wantNotInPage(t, "the calendar", body, `action="/days/go"`) // the day and the stream have Go to a day
	// May 2031 starts on a Thursday: three empty cells before it.
	if !strings.Contains(body, "<tr><td></td><td></td><td></td><td") {
		t.Error("the first week does not start on Monday")
	}
}

// TestTheZoomMarksItsOwnLevel: a month links Week and Month in its zoom, and marks its own level. The page marks two
// links in all: its Month and the Calendar of the sidebar.
func TestTheZoomMarksItsOwnLevel(t *testing.T) {
	l := emptyLife(t) // 2031-05-14
	body := pageAt(t, l.web, "/months/2031-04")
	if n := strings.Count(body, `aria-current="page">`); !strings.Contains(body, `<a href="/months/2031-04" aria-current="page">Month</a>`) || !strings.Contains(body, `<a href="/weeks/2031-W14">Week</a>`) || n != 2 {
		t.Errorf("the month's zoom marks %d links (want its Month and the sidebar's Calendar): %.300s", n, body[strings.Index(body, `aria-label="Zoom"`):])
	}
}
