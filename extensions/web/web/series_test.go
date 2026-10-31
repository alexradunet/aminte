package web_test

// The series of a metric (/metrics/NAME), the page of a reading, and the pages that lead to them.
// Web draws them from the answers of the real core.
// A test reads a form as a browser reads it (internal/formdoc), and sends it through web.

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/siren"
)

// pageOfSeries returns the entity of the page that the series with the name carries, as the core answers it.
func pageOfSeries(t *testing.T, l *life, name string) *siren.Entity {
	t.Helper()
	return must(t)(l.api.Get(t.Context(), pageOf(t, l, name)))
}

// A metric is one page in a browser.
// The form that records a reading is fixed to the metric. It comes before the readings.
// The page has every reading, newest first, in a fold for each year. A window leads back to every reading.
// A record from the page returns to the page. The new reading is the first row.
func TestAMetricIsOnePage(t *testing.T) {
	l, readings := lifeWithWeightSeries(t)
	body := pageAt(t, l.web, "/metrics/Weight")

	record := formTextPostingTo(t, body, "/measurements")
	if !strings.Contains(record, `<input type="hidden" name="metric" value="Weight">`) || strings.Contains(record, `<select name="metric"`) {
		t.Errorf("the record form does not fix its metric to the page's: %s", record)
	}
	table := strings.Index(body, `class="readings-year"`)
	if at := strings.Index(body, record); record == "" || table < 0 || at > table {
		t.Errorf("Record is not before the readings (record at %d, table at %d)", at, table)
	}
	if strings.Contains(body, ">All readings<") {
		t.Error("the page links All readings: every reading is on it")
	}
	at := func(s string) int { return strings.LastIndex(body, `href="`+s+`"`) }
	if !(at(readings[2]) < at(readings[1]) && at(readings[1]) < at(readings[0])) || at(readings[0]) < 0 {
		t.Errorf("the readings are not every one, newest first: %d %d %d", at(readings[2]), at(readings[1]), at(readings[0]))
	}
	for _, year := range []string{"2026", "2024"} {
		if !strings.Contains(body, `<details class="readings-year" open><summary><span class="year-name">`+year+`</span>`) {
			t.Errorf("no open fold of %s: a short series opens every year", year)
		}
	}
	if b := pageAt(t, l.web, "/metrics/Weight?from=2026-01-01"); !strings.Contains(b, ">All readings<") {
		t.Error("a window does not lead back to every reading")
	}

	// The browser sends the hidden fields of the form, a day and a value.
	// The page comes back with the new reading as its first row.
	action, sent := formSentWith(t, body, `action="/measurements"`, url.Values{"day": {"2026-10-13"}, "value": {"76.9"}})
	rec := postForm(l.web, action, sent, "Referer", "http://example.com/metrics/Weight")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/metrics/Weight" {
		t.Fatalf("record from the page: %d to %q, want 303 back to it", rec.Code, rec.Header().Get("Location"))
	}
	after := pageAt(t, l.web, "/metrics/Weight")
	fold := after[strings.Index(after, `class="readings-year"`):]
	if first := strings.Index(fold, "<tbody>"); first < 0 || !strings.HasPrefix(fold[first:], `<tbody><tr><td><a href="/days/2026-10-13">`) || !strings.Contains(fold[first:first+200], ">76.9</a>") {
		t.Error("the reading recorded from the page is not its first row")
	}
}

// A metric has one view in a browser.
// The page of a live metric is a redirect to its series, and the redirect keeps the query.
// The page of a deleted metric stays, with Restore.
func TestTheBrowserSendsAMetricsPageToItsSeries(t *testing.T) {
	l, _ := lifeWithWeightSeries(t)
	self := pageOf(t, l, "Weight")
	for _, tc := range []struct{ path, to string }{
		{self, "/metrics/Weight"},
		{self + "?new=edit-metric", "/metrics/Weight?new=edit-metric"},
	} {
		rec := ask(l.web, http.MethodGet, tc.path)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != tc.to {
			t.Errorf("GET %s in a browser: %d to %q, want 303 to %q", tc.path, rec.Code, rec.Header().Get("Location"), tc.to)
		}
	}

	l.do(t, self, "tombstone-pages", nil)
	body := pageAt(t, l.web, self)
	if !strings.Contains(body, "Deleted") || !strings.Contains(body, `action="`+self+`/revive"`) {
		t.Error("a deleted metric's page is not the page view with Restore")
	}
}

// /metrics/NAME draws the whole page of its metric.
// The view has the note, the editor, the categories, the pages that mention the metric, and the form of each action of the page once.
// The one open form of the mark is there.
// A save of the mark and a save of the body return to the series.
func TestAMetricsViewHoldsItsPage(t *testing.T) {
	l, _ := lifeWithWeightSeries(t)
	page := pageOfSeries(t, l, "Weight")
	day := must(t)(l.api.Get(t.Context(), "/pages?title=2026-10-13"))
	body := pageAt(t, l.web, "/metrics/Weight")
	for _, want := range []string{"Before breakfast.", `href="` + href(day, "self") + `"`, `aria-label="Backlinks"`, `aria-label="Categories"`, `<summary>Edit</summary>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the view of a metric lacks %s", want)
		}
	}
	for _, bad := range []string{">The page: ", "Not asked for on every day", "Asked for on every day:"} {
		if strings.Contains(body, bad) {
			t.Errorf("the view of a metric still says %q", bad)
		}
	}
	// The view draws the form of each action of the page once.
	// The check skips set-cover, promote and file-pages. A link draws set-cover, and a badge has its own form for file-pages.
	for _, a := range page.Actions {
		switch a.Name {
		case "set-cover", "promote", "file-pages":
			continue
		}
		if n := strings.Count(body, `action="`+a.Href+`"`); n != 1 {
			t.Errorf("the view of a metric draws the form of %s %d times, want once (%s)", a.Name, n, a.Href)
		}
	}
	if !strings.Contains(body, `action="`+act(t, page, "file-pages").Href+`"`) {
		t.Error("the view of a metric has no form of file-pages")
	}
	mark := act(t, page, "edit-metric")
	form := regexp.MustCompile(`(?s)<form[^>]*action="` + regexp.QuoteMeta(mark.Href) + `".*?</form>`).FindString(body)
	if !strings.Contains(form, `name="daily"`) || strings.Contains(form, "<details") || strings.Contains(strings.SplitN(body, form, 2)[0], `<details class="action"><summary class="button">Save`) {
		t.Errorf("the mark is not one open form with its box: %s", form)
	}

	saveHref := act(t, page, "save-body").Href
	action, save := formSentWith(t, body, `action="`+saveHref+`"`, url.Values{"body": {"Before breakfast. See [[Scales]]."}})
	rec := postForm(l.web, action, save, "Referer", "http://example.com/metrics/Weight")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/metrics/Weight") {
		t.Fatalf("the save of the body from the view: %d to %q, want 303 back to the series", rec.Code, rec.Header().Get("Location"))
	}
	if body := pageAt(t, l.web, rec.Header().Get("Location")); !strings.Contains(body, "<h2>Result</h2>") || !strings.Contains(body, `Created <a href="/pages?title=Scales">Scales</a>.`) {
		t.Error("the toast of the save is lost")
	}
	body = pageAt(t, l.web, "/metrics/Weight") // the save changed the version
	action, daily := formSentWith(t, body, `action="`+mark.Href+`"`, url.Values{"daily": {"1"}})
	rec = postForm(l.web, action, daily, "Referer", "http://example.com/metrics/Weight")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/metrics/Weight") {
		t.Fatalf("the mark from the view: %d to %q, want 303 back to the series", rec.Code, rec.Header().Get("Location"))
	}
	if after := pageAt(t, l.web, "/metrics/Weight"); !regexp.MustCompile(`name="daily" value="1" checked`).MatchString(after) {
		t.Error("the mark is not set after the save")
	}
}

// The fold of a year shows its newest ten readings.
// The other readings of the year are in a closed fold under them. That fold says how many readings it holds.
// So the other groups of the page stay near. Every reading is still on the page, newest first.
func TestAYearShowsItsNewestReadingsFirst(t *testing.T) {
	l := lifeStartingOn(t, "2026-10-13")
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	for d := 1; d <= 15; d++ {
		l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": fmt.Sprintf("2026-09-%02d", d), "value": fmt.Sprint(70 + d)})
	}
	body := pageAt(t, l.web, "/metrics/Weight")
	fold := body[strings.Index(body, `<details class="readings-year" open>`):]
	end := strings.Index(fold, `</details></details>`)
	if end < 0 {
		t.Fatal("the fold of the year holds no fold of its older readings")
	}
	fold = fold[:end]
	open, more, _ := strings.Cut(fold, `<details class="readings-more"><summary>5 more readings of 2026</summary>`)
	if more == "" {
		t.Fatal("the year does not fold its older five readings under its newest ten")
	}
	if n := strings.Count(open, `<a href="/measurements/`); n != 10 {
		t.Errorf("the open part of the year has %d readings, want 10", n)
	}
	if !strings.Contains(open, `<a href="/days/2026-09-15">`) || !strings.Contains(open, `<a href="/days/2026-09-06">`) || strings.Contains(open, `<a href="/days/2026-09-05">`) {
		t.Error("the open part is not the newest ten, 15 to 6 September")
	}
	if n := strings.Count(more, `<a href="/measurements/`); n != 5 || !strings.Contains(more, `<a href="/days/2026-09-01">`) {
		t.Errorf("the closed part has %d readings, want the five of 1 to 5 September", n)
	}
	if !strings.Contains(body, `<span class="meta">15 readings · 71 to 85 kg</span>`) {
		t.Error("the summary of the year does not count all its readings")
	}
}

// A year of eleven readings folds one reading under its newest ten.
// The fold says "1 more reading", not "1 more readings".
func TestTheFoldOfOneOlderReadingSaysReading(t *testing.T) {
	l := lifeStartingOn(t, "2026-10-13")
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	for d := 1; d <= 11; d++ {
		l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": fmt.Sprintf("2026-09-%02d", d), "value": fmt.Sprint(70 + d)})
	}
	body := pageAt(t, l.web, "/metrics/Weight")
	if want := `<details class="readings-more"><summary>1 more reading of 2026</summary>`; !strings.Contains(body, want) {
		t.Errorf("the fold of one older reading is not %s", want)
	}
}

// The page of a window links All readings.
// The page that the link leads to draws a chart. The chart has a circle for each reading, and each circle has its own place.
// The chart of a window of 90 days ends at 626.
// A window of one day and a window with no reading draw no chart.
func TestAllReadingsOfAWindowDrawTheChartAtItsPlaces(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "synthetic"})
	for _, day := range []string{"2026-01-01", "2026-09-01"} {
		l.do(t, "/", "record", map[string]string{"metric": "synthetic", "day": day, "value": "1"})
	}
	body := pageAt(t, l.web, "/metrics/synthetic?from=2026-06-03&to=2026-09-01")
	link := regexp.MustCompile(`href="([^"]+)"\>All readings`).FindStringSubmatch(body)
	if len(link) != 2 {
		t.Fatal("missing all readings link")
	}
	all := pageAt(t, l.web, strings.ReplaceAll(link[1], "&amp;", "&"))
	chartSVG := regexp.MustCompile(`(?s)<svg class="chart".*?</svg>`).FindString(all) // the icons of the sidebar have circles too
	xs := regexp.MustCompile(`<circle cx="([^"]+)"`).FindAllStringSubmatch(chartSVG, -1)
	if len(xs) != 2 || xs[0][1] == xs[1][1] {
		t.Fatalf("collapsed coordinates: %v", xs)
	}
	if !strings.Contains(body, `cx="626.0"`) {
		t.Error("90 day endpoint")
	}
	if strings.Contains(pageAt(t, l.web, "/metrics/synthetic?from=2026-09-01&to=2026-09-01"), `<svg class="chart"`) {
		t.Error("same day exclusive range should be empty")
	}
	if strings.Contains(pageAt(t, l.web, "/metrics/synthetic?from=2025-01-01&to=2025-02-01"), `<svg class="chart"`) {
		t.Error("empty chart")
	}
}

// The page that the link All readings leads to says All history.
// This holds also when the readings are on the first days of the calendar,
// and when the owner asks with the old name of a metric that has a new name.
func TestTheLinkToAllReadingsLeadsToAllHistory(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Steps", "unit": "steps"})
	for i, day := range []string{"0000-01-01", "0000-02-29", "0001-01-01", "0001-01-02"} {
		l.do(t, "/", "record", map[string]string{"metric": "Steps", "day": day, "value": fmt.Sprint(10 + i)})
	}
	l.do(t, pageOf(t, l, "Steps"), "rename", map[string]string{"title": "Stride count"})
	q := url.Values{"from": {"0000-12-31"}, "to": {"0001-01-01"}}
	bounded := must(t)(l.api.Get(t.Context(), "/metrics/Steps?"+q.Encode())) // the old name stays readable
	link := href(bounded, "all-readings")
	if link == "" {
		t.Fatal("the series of a window has no link to all readings")
	}
	if body := pageAt(t, l.web, link); !strings.Contains(body, "All history") {
		t.Fatal("missing explicit history label")
	}
}

// The page of a reading puts its facts on a card.
func TestAReadingsFactsAreOnACard(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	r := l.do(t, "/", "record", map[string]string{"metric": "weight", "day": today, "value": "71.4"})
	if body := pageAt(t, l.web, href(r, "self")); !strings.Contains(body, `<div class="sheet list"><dl class="facts">`) {
		t.Error("the facts of a reading are not on a card")
	}
}

// A metric with no unit says what a reading is, and its value box shows what to type.
// A metric with a unit says neither.
// The mark of a daily metric is one open form on its view. The box of the form is ticked or not.
func TestHabitsAndUnitsAreSaidOnTheViewOfAMetric(t *testing.T) {
	l := lifeWithHabitsAndUnits(t)
	habit := pageAt(t, l.web, "/metrics/Stretch")
	for _, want := range []string{"With no unit, a reading is a count, or 1 for done and 0 for not done.", `placeholder="a count, or 1 done and 0 not done"`} {
		if !strings.Contains(habit, want) {
			t.Errorf("the page of a habit lacks %q", want)
		}
	}
	if strings.Contains(habit, "Not asked for on every day") || strings.Contains(habit, "days after its last") {
		t.Error("the view of a metric says the old sentences of the mark, or speaks of 14 days")
	}
	// The mark is one open form on the view of the metric, in both states. The box is ticked for a daily metric.
	mark := regexp.MustCompile(`(?s)<section class="daily-mark".*?</section>`)
	if form := mark.FindString(habit); !strings.Contains(form, `name="daily"`) || strings.Contains(form, "checked") || strings.Contains(form, "<details") {
		t.Errorf("the view of a metric that is not daily lacks the open form with an unticked box:\n%s", form)
	}
	if form := mark.FindString(pageAt(t, l.web, "/metrics/Meditate")); !strings.Contains(form, `name="daily" value="1" checked`) || strings.Contains(form, "<details") {
		t.Errorf("the view of a daily metric lacks the open form with a ticked box:\n%s", form)
	}
	if weight := pageAt(t, l.web, "/metrics/Weight"); strings.Contains(weight, "1 done and 0 not done") || !mark.MatchString(weight) {
		t.Error("the view of a metric with a unit speaks of habits, or lacks the form of the mark")
	}
}

// The view of a metric offers edit-metric with the current mark. It offers the action once, and not under More.
// The form of the browser sets the mark. An unticked box sends nothing, and that means 0.
// The page of a person has no such form.
func TestTheViewOfAMetricDrawsTheDailyForm(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Meditate"})
	l.do(t, "/", "create-person", map[string]string{"title": "Sam (barber)", "name": "Samuel Ortiz"})
	view := "/metrics/Meditate"
	referer := "http://example.com" + view
	page := regexp.MustCompile(`action="(/pages/\d+)/metric"`).FindStringSubmatch(pageAt(t, l.web, view))[1]

	body := pageAt(t, l.web, view)
	if n := strings.Count(body, `action="`+page+`/metric"`); n != 1 || strings.Contains(body, `name="daily" value="1" checked`) {
		t.Errorf("the view of a metric that is not daily has %d forms of the mark, or a ticked box", n)
	}
	if rec := postForm(l.web, page+"/metric", url.Values{"daily": {"1"}, "back": {view}}, "Referer", referer); rec.Code != http.StatusSeeOther || placeWithoutReceipt(t, rec.Header().Get("Location")) != view {
		t.Fatalf("the form answered %d to %q, want 303 to the view", rec.Code, rec.Header().Get("Location"))
	}
	if !strings.Contains(pageAt(t, l.web, view), `name="daily" value="1" checked`) {
		t.Error("after the form, the box is not ticked")
	}
	if rec := postForm(l.web, page+"/metric", url.Values{"back": {view}}, "Referer", referer); rec.Code != http.StatusSeeOther {
		t.Fatalf("the form with an empty box answered %d", rec.Code)
	}
	if strings.Contains(pageAt(t, l.web, view), `name="daily" value="1" checked`) {
		t.Error("after an empty box, the mark is still set")
	}
	person := regexp.MustCompile(`href="(/pages/\d+)"`).FindStringSubmatch(pageAt(t, l.web, "/browse?kind=person"))[1]
	if strings.Contains(pageAt(t, l.web, person), "Ask for it every day") {
		t.Error("the page of a person draws the daily form")
	}
}

// Correct holds the current value. A retraction offers no Correct.
// The page says how to fix a wrong day or a wrong metric, with a link to the record form that is already filled.
func TestACorrectionStartsFromTheValueAndARetractionHasNone(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	l.do(t, "/days/"+today, "record", map[string]string{"metric": "Weight", "value": "71.5"})
	reading := regexp.MustCompile(`href="(/measurements/\d+)"`).FindStringSubmatch(pageAt(t, l.web, "/metrics/Weight"))[1]

	body := pageAt(t, l.web, reading)
	if !regexp.MustCompile(`(?s)aria-label="Correct".*name="value" value="71.5"`).MatchString(body) {
		t.Errorf("Correct is not filled with 71.5:\n%s", body)
	}
	for _, want := range []string{"Wrong day or wrong metric? Retract it, then",
		`href="/?day=` + today + `&amp;metric=Weight&amp;new=record&amp;value=71.5#record">add it again</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the reading lacks %s:\n%s", want, body)
		}
	}

	rec := postForm(l.web, reading+"/retract", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("retract: %d %.300s", rec.Code, rec.Body.String())
	}
	retracted := pageAt(t, l.web, placeWithoutReceipt(t, rec.Header().Get("Location")))
	if strings.Contains(retracted, `aria-label="Correct"`) || strings.Contains(retracted, "Wrong day") || strings.Contains(retracted, "Retract</summary>") {
		t.Errorf("a retraction offers an action:\n%s", retracted)
	}
	if !strings.Contains(retracted, "This row retracts the reading before it") {
		t.Errorf("the retraction is not explained:\n%s", retracted)
	}
}

// Delete on the view of a metric lands on the page of the metric, with Restore. A deleted metric keeps its page.
// The view of a name that is not a live metric draws no part of a page.
// A toast follows the redirect of a page to its view. So a form that is sent with no back shows its result on the view.
func TestDeleteFromTheViewReturnsToThePage(t *testing.T) {
	l, _ := lifeWithWeightSeries(t)
	series := must(t)(l.api.Get(t.Context(), "/metrics/Weight"))
	self := pageOf(t, l, "Weight")
	body := pageAt(t, l.web, "/metrics/Weight")

	// A form of the metric that is sent with no back, for example a form opened from /actions.
	// The POST returns to the page. The page redirects to the view. The toast comes along.
	mark := act(t, series, "edit-metric").Href
	action, form := formSentWith(t, body, `action="`+mark+`"`, url.Values{"daily": {"1"}})
	form.Del("back")
	rec := postForm(l.web, action, form, "Referer", "http://example.com/actions")
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, self+"?feedback=") {
		t.Fatalf("the mark sent with no back: %d to %q, want 303 to %s?feedback=…", rec.Code, loc, self)
	}
	hop := ask(l.web, http.MethodGet, loc)
	loc2 := hop.Header().Get("Location")
	if hop.Code != http.StatusSeeOther || !strings.HasPrefix(loc2, "/metrics/Weight?feedback=") {
		t.Fatalf("the page in a browser: %d to %q, want 303 to the view with the receipt", hop.Code, loc2)
	}
	if view := pageAt(t, l.web, loc2); !strings.Contains(view, `class="toast"`) || !strings.Contains(view, "Asked for on every day.") {
		t.Error("the toast is lost across the redirect to the view")
	}

	// Delete from the view: the browser lands on the page, with Restore.
	del := act(t, series, "tombstone-pages").Href
	action, deleteForm := formSentWith(t, body, `action="`+del+`"`, nil)
	rec = postForm(l.web, action, deleteForm, "Referer", "http://example.com/metrics/Weight")
	loc = rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, self+"?feedback=") {
		t.Fatalf("Delete from the view: %d to %q, want 303 to %s?feedback=…", rec.Code, loc, self)
	}
	if page := pageAt(t, l.web, loc); !strings.Contains(page, `action="`+self+`/revive"`) || !strings.Contains(page, `class="toast"`) {
		t.Error("after Delete from the view the browser is not on the page with Restore and the result")
	}

	// A name that is not a live metric: the readings alone, no part of a page.
	for _, path := range []string{"/metrics/Weight", "/metrics/Nope"} {
		view := pageAt(t, l.web, path)
		for _, bad := range []string{"Nothing written here yet.", `aria-label="Facts"`, `<summary>Edit</summary>`, `aria-label="Every day"`} {
			if strings.Contains(view, bad) {
				t.Errorf("%s carries no page but draws %q", path, bad)
			}
		}
	}
}

// The owner can make a page where a metric was wanted. That page has the link "Make it a metric".
// The link opens the one New metric form, with the name filled.
// The registration turns the page into the metric. The metric keeps the text.
// The list of the metrics in Browse links to the same form. The list of another kind does not.
func TestAPlainPageLeadsToTheNewMetricForm(t *testing.T) {
	l := emptyLife(t)
	pg := l.do(t, "/", "create-page", map[string]string{"title": "Steps", "body": "Counted by the watch."})
	pgHref := href(pg, "self")
	const want = "/metrics?name=Steps&new=register-metric"
	page := pageAt(t, l.web, pgHref)
	if !strings.Contains(page, `<a class="button" href="`+strings.ReplaceAll(want, "&", "&amp;")+`">Make it a metric</a>`) {
		t.Errorf("the page of a plain page has no Make it a metric link")
	}

	form := pageAt(t, l.web, want)
	if !regexp.MustCompile(`<input type="text" name="name" value="Steps" required`).MatchString(form) {
		t.Errorf("the link does not open New metric with the name Steps")
	}
	action, sent := formSentWith(t, form, `action="/metrics"`, url.Values{"unit": {"steps"}})
	rec := postForm(l.web, action, sent)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("New metric over a plain page: %d %.300s", rec.Code, rec.Body.String())
	}
	after := must(t)(l.api.Get(t.Context(), pgHref))
	if got := fmt.Sprint(after.Class); !strings.Contains(got, "metric") {
		t.Errorf("the page is not a metric: class %v", after.Class)
	}
	if body := after.Properties.(map[string]any)["body"]; body != "Counted by the watch." {
		t.Errorf("the metric keeps the text? body %q", body)
	}
	if got := pageAt(t, l.web, "/metrics/Steps"); strings.Contains(got, "Make it a metric") {
		t.Error("the view of a metric leads to the form that makes it one")
	}

	if p := pageAt(t, l.web, "/browse?kind=metric"); !strings.Contains(p, `<a class="button" href="/metrics?new=register-metric">New metric</a>`) {
		t.Error("Browse of the metrics has no New metric link")
	}
	if p := pageAt(t, l.web, "/browse?kind=page"); strings.Contains(p, ">New metric</a>") && strings.Contains(p, `class="button" href="/metrics`) {
		t.Error("Browse of the pages links to New metric")
	}
}

// A GET of a live conversation page by a browser is a 303 to the conversation of its day. The redirect keeps the query.
// A page that is not a conversation is not redirected.
// A deleted conversation page stays a page.
// The conversation view is a whole page for a day with no talk and for a day with talk.
func TestAConversationPageHasOneViewInABrowser(t *testing.T) {
	l := emptyLife(t)
	conversation := "/days/" + today + "/conversation"
	pageAt(t, l.web, "/days/today/conversation")
	l.do(t, conversation, "say", map[string]string{"text": "Hello"})
	pageAt(t, l.web, "/days/today/conversation")
	self := href(must(t)(l.api.Get(t.Context(), conversation)), "page")
	if self == "" {
		t.Fatal("the conversation links no page")
	}
	for query, want := range map[string]string{"": conversation, "?new=rename": conversation + "?new=rename"} {
		if rec := ask(l.web, http.MethodGet, self+query); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("GET %s%s in a browser: %d %q, want 303 to %s", self, query, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	// A page with a title that is not of a day is not a conversation. The redirect does not apply to it.
	other := l.do(t, "/", "create-page", map[string]string{"title": "Conversation notes"})
	if rec := ask(l.web, http.MethodGet, href(other, "self")); rec.Code != http.StatusOK {
		t.Errorf("a page titled Conversation notes: %d, want 200", rec.Code)
	}
	l.do(t, self, "tombstone-pages", nil)
	rec := ask(l.web, http.MethodGet, self)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="`+self+`/revive"`) {
		t.Errorf("a deleted conversation page: %d, want 200 and the page with Restore: %.300s", rec.Code, rec.Body.String())
	}
}
