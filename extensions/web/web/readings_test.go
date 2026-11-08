package web_test

// The readings of a day. Web draws them from the answers of the real core.
// They are the form that records a reading, the tiles of the day, and the tile that asks for a daily metric.
// A test reads a form as a browser reads it (internal/formdoc), and sends it through web.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"aminte-web/internal/formdoc"
)

// recordFormSends returns what a browser sends with the record form of the page body, when the owner changes nothing.
// These are the hidden fields, the text fields, the date and number boxes, and for each select the chosen option.
// The chosen option is the option that is marked selected, or else the first option.
// The test fails when the page has no record form.
func recordFormSends(t *testing.T, body string) url.Values {
	t.Helper()
	section := regexp.MustCompile(`(?s)<div id="record">.*?</form>`).FindString(body)
	if section == "" {
		t.Fatalf("no record form:\n%.800s", body)
	}
	form, ok := formdoc.Find(formdoc.Forms(section), func(f formdoc.Form) bool { return f.Action == "/measurements" })
	if !ok {
		t.Fatalf("the record form does not post to /measurements:\n%s", section)
	}
	return form.Values
}

// placeWithoutReceipt returns a Location without the receipt of the toast, when the Location has one.
func placeWithoutReceipt(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	q := u.Query()
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// When no metric is named, the first option is an empty prompt and the select is required. So the browser makes the owner choose.
// The form offers every live metric, read lately or not.
// A link that names a metric selects it.
// A link can name a metric that does not exist. The form keeps and selects that name. The browser sends it, and the core refuses it with 404.
// The name is never dropped in silence.
func TestTheRecordFormAsksForAMetricWhenNoneIsNamed(t *testing.T) {
	l := emptyLife(t)
	for _, n := range []string{"Evening walk", "Meditate"} {
		l.do(t, "/", "register-metric", map[string]string{"name": n})
	}
	body := pageAt(t, l.web, "/days/"+today)
	want := `<select name="metric" required><option value="">Pick a metric</option><option>Evening walk</option><option>Meditate</option>`
	if !strings.Contains(body, want) {
		t.Errorf("the record form lacks %s:\n%s", want, regexp.MustCompile(`(?s)<div id="record">.*?</form>`).FindString(body))
	}
	if sent := recordFormSends(t, body); sent.Get("metric") != "" {
		t.Errorf("the browser sends metric %q before the owner chose", sent.Get("metric"))
	}
	if got := recordFormSends(t, pageAt(t, l.web, "/?new=record&metric=Nothing&value=1")).Get("metric"); got != "Nothing" {
		t.Errorf("an unknown metric in the link is sent as %q, want Nothing", got)
	}
	rec := postForm(l.web, "/measurements", url.Values{"metric": {"Nothing"}, "day": {today}, "value": {"1"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("record of an unknown metric: %d, want 404", rec.Code)
	}
}

// A metric that was not read for years is not among the metrics read lately.
// The link ?new=record&metric=X must still record X. It must never record the first metric of the list.
func TestALinkToTheRecordFormOfAMetricRecordsThatMetric(t *testing.T) {
	l := emptyLife(t)
	for _, n := range []string{"Evening walk", "Meditate"} {
		l.do(t, "/", "register-metric", map[string]string{"name": n})
	}
	l.do(t, "/days/"+dayFromToday(-1), "record", map[string]string{"metric": "Evening walk", "value": "1"}) // the one read; Meditate is not

	sent := recordFormSends(t, pageAt(t, l.web, "/?new=record&metric=Meditate&value=1"))
	if sent.Get("metric") != "Meditate" {
		t.Fatalf("the browser sends metric %q, want Meditate", sent.Get("metric"))
	}
	if rec := postForm(l.web, "/measurements", sent); rec.Code != http.StatusSeeOther {
		t.Fatalf("record: %d %.300s", rec.Code, rec.Body.String())
	}
	if got := pageAt(t, l.web, "/metrics/Meditate"); !strings.Contains(got, `/days/`+today) {
		t.Errorf("the reading is not on Meditate:\n%.1500s", got)
	}
	if got := pageAt(t, l.web, "/metrics/Evening%20walk"); strings.Contains(got, `/days/`+today) {
		t.Errorf("the reading is on Evening walk")
	}
}

// The folded fields have plain titles with an example.
func TestTheRecordFieldsSayWhatToType(t *testing.T) {
	l := emptyLife(t)
	body := pageAt(t, l.web, "/days/"+today)
	for _, want := range []string{"Time taken, in UTC (2026-06-09T21:14:03.482Z)", "Time zone (Europe/Bucharest)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the record form lacks %q", want)
		}
	}
}

// lifeWithHabitsAndUnits is a life with four metrics.
// Stretch has no unit. Meditate is daily and has no unit. Weight has the unit kg. Mood score is a score of 1 to 5.
// Today has the readings Weight 70, Weight 71, Mood score 4 and Stretch 1.
func lifeWithHabitsAndUnits(t *testing.T) *life {
	t.Helper()
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Stretch"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Meditate", "daily": "1"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Mood score", "unit": "1-5"})
	for _, v := range []string{"70", "71"} {
		l.do(t, "/days/"+today, "record", map[string]string{"metric": "Weight", "value": v})
	}
	l.do(t, "/days/"+today, "record", map[string]string{"metric": "Mood score", "value": "4"})
	l.do(t, "/days/"+today, "record", map[string]string{"metric": "Stretch", "value": "1"})
	return l
}

// The tiles of a day show the unit. A scale shows in words.
// A metric that is read again on the day links to its page. A reading of a habit has no unit.
func TestTheTilesOfADayShowTheirUnits(t *testing.T) {
	l := lifeWithHabitsAndUnits(t)
	day := pageAt(t, l.web, "/days/"+today)
	for _, want := range []string{`71 <span class="unit">kg</span>`, `4 <span class="unit">of 5</span>`,
		`<a class="meta" href="/metrics/Weight">and 1 more this day</a>`} {
		if !strings.Contains(day, want) {
			t.Errorf("the day lacks %s", want)
		}
	}
	if strings.Contains(day, `>1 <span class="unit">`) {
		t.Error("a habit reading has a unit")
	}
}

// A day draws one tile for each daily metric in name order. Then it draws the other metrics that were read that day.
// A daily metric with no reading draws the record form fixed to that metric.
// A save returns to the day. Then the tile shows the reading once.
// A past day asks in the same way.
func TestADailyMetricAsksInItsTileOnEveryDay(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Sleep", "unit": "h", "daily": "1"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Meditate", "daily": "1"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	page := "/days/" + today
	l.do(t, page, "record", map[string]string{"metric": "Weight", "value": "70", "day": today})

	body := pageAt(t, l.web, page)
	names := regexp.MustCompile(`<a class="name" href="/metrics/[^"]*">([^<]+)</a>`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, n := range names {
		got = append(got, n[1])
	}
	if want := "Meditate Sleep Weight"; strings.Join(got, " ") != want {
		t.Errorf("tiles %v, want %s", got, want)
	}
	sleep := regexp.MustCompile(`(?s)<form\b[^>]*id="tile-Sleep".*?</form>`).FindString(body)
	for _, want := range []string{`name="metric" value="Sleep"`, `name="day" value="` + today + `"`, `type="number"`, `placeholder="h"`, `>Save</button>`} {
		if !strings.Contains(sleep, want) {
			t.Errorf("the Sleep form lacks %s:\n%s", want, sleep)
		}
	}
	meditate := regexp.MustCompile(`(?s)<form\b[^>]*id="tile-Meditate".*?</form>`).FindString(body)
	if strings.Contains(meditate, `type="number"`) {
		t.Errorf("a metric with no unit has a number box:\n%s", meditate)
	}
	for _, want := range []string{`type="radio" name="value" value="1"`, `type="radio" name="value" value="0"`, `Done`, `Not done`} {
		if !strings.Contains(meditate, want) {
			t.Errorf("the Meditate form lacks %s:\n%s", want, meditate)
		}
	}
	if strings.Index(meditate, `value="1"`) > strings.Index(meditate, `value="0"`) {
		t.Errorf("Done is not the first radio:\n%s", meditate)
	}
	if strings.Contains(body, "No readings on this day") || strings.Contains(body, `id="tile-Weight"`) {
		t.Errorf("a day with tiles shows the empty state, or the read Weight asks again")
	}

	action, sent := formSentWith(t, body, `id="tile-Sleep"`, url.Values{"value": {"7.5"}})
	rec := postForm(l.web, action, sent)
	if rec.Code != http.StatusSeeOther || placeWithoutReceipt(t, rec.Header().Get("Location")) != page {
		t.Fatalf("save: %d, returned to %q, want %q", rec.Code, rec.Header().Get("Location"), page)
	}
	after := pageAt(t, l.web, page)
	if strings.Contains(after, `id="tile-Sleep"`) || !strings.Contains(after, "7.5") {
		t.Errorf("after the save the Sleep tile still asks, or lacks 7.5")
	}
	if n := strings.Count(after, `class="name" href="/metrics/Sleep"`); n != 1 {
		t.Errorf("Sleep has %d tiles, want 1", n)
	}

	past := pageAt(t, l.web, "/days/"+dayFromToday(-1))
	if !strings.Contains(past, `id="tile-Sleep"`) || !strings.Contains(past, `id="tile-Meditate"`) {
		t.Errorf("a past day lacks the daily tiles")
	}
}

// A day with no daily metric and no reading says so.
// A day with a daily metric does not say so, because the tile asks.
func TestTheEmptyStateOfReadingsNeedsNoTile(t *testing.T) {
	l := emptyLife(t)
	page := "/days/" + today
	if !strings.Contains(pageAt(t, l.web, page), "No readings on this day") {
		t.Error("an empty day lacks the empty state")
	}
	l.do(t, "/", "register-metric", map[string]string{"name": "Meditate", "daily": "1"})
	if strings.Contains(pageAt(t, l.web, page), "No readings on this day") {
		t.Error("a day with a daily metric shows the empty state")
	}
}
