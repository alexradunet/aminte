package web_test

// The Metrics board. Web draws it from the answers of the real core.
// It has the cards, the folds of the groups, the groups by category, the form that makes a metric and the form that files metrics.
// A test reads a form as a browser reads it (internal/formdoc), and sends it through web.

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// lifeWithWeightSeries is a life whose today is 2026-10-13.
// Weight has the unit kg and the note "Before breakfast.". It has readings on 2024-03-01, 2026-10-01 and 2026-10-12.
// The day page of today names Weight.
// The function returns the addresses of the three readings, oldest first.
func lifeWithWeightSeries(t *testing.T) (*life, []string) {
	t.Helper()
	l := lifeStartingOn(t, "2026-10-13")
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg", "note": "Before breakfast."})
	var readings []string
	for _, r := range [][2]string{{"2024-03-01", "81.5"}, {"2026-10-01", "77.4"}, {"2026-10-12", "77.1"}} {
		readings = append(readings, href(l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": r[0], "value": r[1]}), "self"))
	}
	l.do(t, "/", "capture", map[string]string{"text": "Weighed in: [[Weight]] is down."})
	return l, readings
}

// metricIds returns the ids of the live metrics by name, as the core lists them on /metrics.
func metricIds(t *testing.T, l *life) map[string]string {
	t.Helper()
	ids := map[string]string{}
	board := must(t)(l.api.Get(t.Context(), "/metrics"))
	list, _ := board.Properties.(map[string]any)["metrics"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		ids[fmt.Sprint(m["name"])] = fmt.Sprint(m["id"])
	}
	return ids
}

// lifeWithCards is a life with four metrics, and it returns the ids of the metrics by name.
// Ferritin has readings on two days long ago. Weight has readings on the two days before today.
// Walk is a habit. Steps has no reading. Mood is a metric of every file and has no reading.
func lifeWithCards(t *testing.T) (*life, map[string]string) {
	t.Helper()
	l := emptyLife(t)
	for _, m := range []map[string]string{{"name": "Ferritin", "unit": "ug/L", "note": "iron stores"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		l.do(t, "/", "register-metric", m)
	}
	for _, r := range []map[string]string{{"metric": "Ferritin", "day": "2030-03-01", "value": "40"}, {"metric": "Ferritin", "day": "2030-09-01", "value": "55"},
		{"metric": "Weight", "day": "2031-05-12", "value": "80"}, {"metric": "Weight", "day": "2031-05-13", "value": "79.5"}} {
		l.do(t, "/metrics/Ferritin", "record", r)
	}
	return l, metricIds(t, l)
}

// fileMetrics files the metrics with the ids in the category, with the action file-pages of /metrics.
func fileMetrics(t *testing.T, l *life, category string, ids ...string) {
	t.Helper()
	l.do(t, "/metrics", "file-pages", map[string]string{"ids": strings.Join(ids, ","), "category": category})
}

// landsOnWithReceipt reports whether location is the page back with one receipt of the toast.
// The page back has its path, and its query when it has one.
// The receipt is 64 hexadecimal characters in the query field feedback.
func landsOnWithReceipt(location, back string) bool {
	want, err := url.Parse(back)
	if err != nil {
		return false
	}
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != want.Path || u.Fragment != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	receipts := q["feedback"]
	q.Del("feedback")
	if q.Encode() != want.Query().Encode() || len(receipts) != 1 || len(receipts[0]) != 64 {
		return false
	}
	_, err = hex.DecodeString(receipts[0])
	return err == nil
}

// The Latest tile of a metric writes the unit of a scale as every tile does: "of 5" for 1-5.
// It writes any other unit as it is.
func TestTheLatestTileWritesAScaleInWords(t *testing.T) {
	l, _ := lifeWithWeightSeries(t)
	l.do(t, "/", "record", map[string]string{"metric": "Mood", "day": "2026-10-12", "value": "4"})
	for path, value := range map[string]string{"/metrics/Mood": `4 <span class="unit">of 5</span>`, "/metrics/Weight": `77.1 <span class="unit">kg</span>`} {
		latest := regexp.MustCompile(`<span class="name">Latest, Mon 12 Oct</span>\n<a class="reading" href="/measurements/\d+">` + regexp.QuoteMeta(value) + `</a>`)
		if body := pageAt(t, l.web, path); !latest.MatchString(body) {
			t.Errorf("the Latest tile of %s does not write %s", path, value)
		}
	}
}

// /metrics draws a card for each metric in the fold of its category.
// A card has the name, the last reading with its unit, the day of that reading and the trend.
// The card of a metric with no reading says so.
// The page also has the jump bar, the fold of a category in a category, and the form that files the metrics that the owner picks.
// A post of that form files the metrics and says so. A post with no pick is refused with a link back.
func TestTheMetricsPageDrawsCards(t *testing.T) {
	l, ids := lifeWithCards(t)
	fileMetrics(t, l, "Iron", ids["Ferritin"])
	fileMetrics(t, l, "Body", ids["Weight"], ids["Walk"])
	iron := must(t)(l.api.Get(t.Context(), "/pages?title=Iron"))
	l.do(t, href(iron, "self"), "file-pages", map[string]string{"category": "Body"})

	page := pageAt(t, l.web, "/metrics")
	for _, want := range []string{
		`<nav class="kinds jump" aria-label="Groups"><a href="#c-`, `>Body <small>3</small></a>`, `<a href="#not-filed">Not filed <small>2</small></a>`,
		`id="not-filed"`, `<details class="metric-sub" open><summary><h3>Iron <small>1</small></h3><span class="meta">newest `, `<a href="` + href(iron, "self") + `">Open Iron</a>`,
		`<a class="name" href="/metrics/Weight">Weight</a>`, `79.5 <span class="unit">kg</span>`, `<time class="meta" datetime="2031-05-13" title="yesterday · Tue 13 May · 2 readings">yesterday</time>`, `<svg class="trend"`,
		`<div class="reading-tile metric-card stale">`, `title="iron stores">Ferritin</a>`, `<span class="reading none"><span aria-hidden="true">–</span> <span class="unit">n</span></span><span class="line-none" aria-hidden="true"></span>`, `<div class="card-foot"><span class="meta">No reading</span>`,
		`<form id="pick" method="POST" action="/pages/file" class="inline" data-action="file-pages">` + "\n" + `<input type="hidden" name="back" value="/metrics">`,
		`<input type="checkbox" name="ids" value="` + ids["Weight"] + `" form="pick" aria-label="Select Weight">`,
		`<input type="text" name="category" value="" required list="pick-category" placeholder="Bloodwork, Sport, Financial"><datalist id="pick-category"><option value="Body"><option value="Iron"></datalist>`,
		`<label><input type="checkbox" name="move" value="1"><span>Move (remove the other categories)</span></label>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if main := page[strings.Index(page, "<main"):]; strings.Contains(main, "<table") { // the rail's month is a table
		t.Error("the category lens draws a table")
	}
	if strings.Contains(page, `class="pick-on" checked`) {
		t.Error("Select is on with nothing refused")
	}

	// A box for each card is outside the form and joins it. The form returns to /metrics with the sentence.
	// The box Move is not checked, so the form sends no move.
	action, sent := formSentWith(t, page, `id="pick"`, url.Values{"ids": {ids["Mood"], ids["Steps"]}, "category": {"Daily"}})
	rec := postForm(l.web, action, sent)
	if rec.Code != http.StatusSeeOther || !landsOnWithReceipt(rec.Header().Get("Location"), "/metrics") {
		t.Fatalf("the form: %d %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if after := pageAt(t, l.web, rec.Header().Get("Location")); !strings.Contains(after, "Put Mood and Steps in Daily (a new category).") || !strings.Contains(after, `<a href="#c-`) || strings.Contains(after, `id="not-filed"`) {
		t.Errorf("/metrics after the filing: %.600s", after)
	}
	// A refused form answers with the error page, which leads back to the board.
	action, refused := formSentWith(t, page, `id="pick"`, url.Values{"category": {"Daily"}, "move": {"1"}})
	rec = postForm(l.web, action, refused, "Referer", "http://example.com/metrics")
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "missing field ids") || !strings.Contains(body, `<a href="/metrics">Back</a>`) {
		t.Errorf("no box checked: %d %.600s", rec.Code, body)
	}
}

// A category on the board is a fold. It holds its metrics and its categories, and it says the newest day of its readings.
// The tools of a group are its foot. The board has no list view.
func TestTheMetricsBoardFolds(t *testing.T) {
	l, ids := lifeWithCards(t)
	fileMetrics(t, l, "Iron", ids["Ferritin"])
	fileMetrics(t, l, "Body", ids["Weight"], ids["Walk"])
	l.do(t, "/pages?title=Iron", "file-pages", map[string]string{"category": "Body"})

	cards := pageAt(t, l.web, "/metrics")
	for _, want := range []string{
		// Body holds Weight and Walk, and Ferritin under Iron: its newest reading is Weight's, yesterday.
		`<details class="group metric-group" id="c-`, ` open><summary><h2>Body <small>3</small></h2><span class="meta">newest yesterday · Tue 13 May</span></summary>`,
		`<details class="metric-sub" open><summary><h3>Iron <small>1</small></h3><span class="meta">newest `,
		// Mood and Steps have no reading: the line of Not filed says no day.
		`<details class="group metric-group" id="not-filed" open><summary><h2>Not filed <small>2</small></h2></summary>`,
	} {
		if !strings.Contains(cards, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if strings.Contains(cards, "view=list") || strings.Contains(cards, "metric-rows") {
		t.Error("/metrics draws a list view")
	}
	// The tools of a group are its foot: after its cards, and in Iron after Ferritin.
	iron := cards[strings.Index(cards, `<h3>Iron`):]
	if ferritin, tools := strings.Index(iron, `>Ferritin</a>`), strings.Index(iron, `<div class="group-tools">`); ferritin < 0 || tools < ferritin {
		t.Errorf("the tools of Iron come before its cards: Ferritin at %d, the tools at %d", ferritin, tools)
	}
}

// The board draws each category in tree order, with its metrics under it. The metrics with no category come last.
// The datalist of the categories follows the tree.
// The view of a metric holds its note and its categories. Each category is a badge with an x.
func TestMetricsAreGroupedByCategory(t *testing.T) {
	l := emptyLife(t)
	for _, m := range []map[string]string{{"name": "TSH", "unit": "µUI/mL", "note": "thyroid-stimulating hormone"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		l.do(t, "/", "register-metric", m)
	}
	for _, p := range []string{"Biomarkers", "Thyroid", "Body"} {
		l.do(t, "/", "create-page", map[string]string{"title": p})
	}
	file := func(page, category string) {
		t.Helper()
		l.do(t, page, "file-pages", map[string]string{"category": category})
	}
	file("/pages?title=Thyroid", "Biomarkers")
	file(pageOf(t, l, "TSH"), "Thyroid")
	file("/pages?title=Weight", "Body")
	file("/pages?title=Walk", "Body")

	page := pageAt(t, l.web, "/metrics")
	for _, s := range []string{"<h3>Thyroid <small>1</small></h3>", `<datalist id="pick-category"><option value="Biomarkers"><option value="Thyroid"><option value="Body"></datalist>`,
		"<h2>Not filed", `title="thyroid-stimulating hormone"`} {
		if !strings.Contains(page, s) {
			t.Errorf("/metrics lacks %q", s)
		}
	}
	// The metric's view holds its note and its categories, each a badge with its x.
	s := pageAt(t, l.web, "/metrics/TSH")
	if !regexp.MustCompile(`<span class="badge category"><a href="/pages/\d+">Thyroid</a><form method="POST" action="/pages/file" class="tiny">\s*<input type="hidden" name="ids" value="\d+"><input type="hidden" name="from" value="Thyroid">`).MatchString(s) {
		t.Error("a metric's page does not show its category as a badge with its x")
	}
	if !strings.Contains(s, "thyroid-stimulating hormone") {
		t.Error("a metric's view does not hold its note")
	}
}

// New metric is one boxed form of register-metric. ?new=register-metric opens it with the focus in its name.
// A card has no + Add, and the page has no New category.
// The form of the day and the page of the metric record a reading. Select files the metrics.
// The post of the form makes the metric.
func TestTheMetricsPageMakesAMetric(t *testing.T) {
	l := emptyLife(t)
	page := pageAt(t, l.web, "/metrics")
	if !strings.Contains(page, `<form method="POST" action="/metrics"`) || strings.Contains(page, "new-kind") || strings.Contains(page, "New category") {
		t.Errorf("/metrics does not draw one New metric form, or draws the kinds or New category")
	}
	if !regexp.MustCompile(`class="button primary" popovertarget="new-register-metric">[^/]*</span>New metric <span class="owner">owner</span></button>`).MatchString(page) || !strings.Contains(page, `id="new-register-metric" popover`) {
		t.Error("/metrics does not draw New metric as the primary button with its popover")
	}
	if home := pageAt(t, l.web, "/days/"+today); strings.Contains(home, `method="POST" action="/metrics"`) {
		t.Error("Home, the page of today, draws the New metric form")
	}
	if p := pageAt(t, l.web, "/metrics?new=register-metric"); !strings.Contains(p, `<dialog class="menu-dialog" id="new-register-metric"`) ||
		!regexp.MustCompile(`<input type="text" name="name" value="" required placeholder="[^"]*" autofocus>`).MatchString(p) {
		t.Error("?new=register-metric does not open New metric with the focus in its name")
	}

	action, sent := formSentWith(t, page, `action="/metrics"`, url.Values{"name": {"Weight"}, "unit": {"kg"}, "note": {"Before breakfast"}})
	rec := postForm(l.web, action, sent)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("New metric: %d %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	made := map[string]string{}
	list, _ := must(t)(l.api.Get(t.Context(), "/metrics")).Properties.(map[string]any)["metrics"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		made[fmt.Sprint(m["name"])] = fmt.Sprintf("unit %q, note %v", m["unit"], m["note"])
	}
	if want := `unit "kg", note Before breakfast`; made["Weight"] != want {
		t.Errorf("the form made Weight with %s, want %s", made["Weight"], want)
	}
	if page = pageAt(t, l.web, "/metrics"); strings.Contains(page, `aria-label="Add a reading of`) || strings.Contains(page, "+ New metric here") {
		t.Error("/metrics draws + Add on a card or + New metric here")
	}
}
