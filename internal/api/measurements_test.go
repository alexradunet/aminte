package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
	"lifelog/siren"
)

// The one page of a metric (docs/plans/100-one-page-a-metric.md): Record, every reading and the metric's own page.

// metricFixture is Weight (kg) with a note, a reading of 2024 and two of 2026, and today's day page, which names it.
// The server's day is 2026-10-13. It returns the three readings' addresses, oldest first.
func metricFixture(t *testing.T) (*client.Client, http.Handler, []string) {
	t.Helper()
	c, h, _ := apitest.FreshOn(t, "cli", "2026-10-13")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg", "note": "Before breakfast."}))
	var readings []string
	for _, r := range [][2]string{{"2024-03-01", "81.5"}, {"2026-10-01", "77.4"}, {"2026-10-12", "77.1"}} {
		m := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": r[0], "value": r[1]}))
		readings = append(readings, apitest.Href(m, "self"))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "capture"), map[string]string{"text": "Weighed in: [[Weight]] is down."}))
	return c, h, readings
}

// TestAMetricIsOnePage: the series of a metric carries its page and every reading, oldest first; it offers record and
// save-body; a window of it links back to every reading and the whole series does not.
func TestAMetricIsOnePage(t *testing.T) {
	c, _, readings := metricFixture(t)
	all := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight"))
	if pageOf(all) == "" || apitest.Href(all, "page") != "" || !hasAction(all, "record") || !hasAction(all, "save-body") {
		t.Errorf("the series does not carry its page (and no link to it), or offers no record: %s", names(all))
	}
	var ids []string
	for _, r := range all.Properties.(map[string]any)["readings"].([]any) {
		ids = append(ids, fmt.Sprint("/measurements/", r.(map[string]any)["id"]))
	}
	if !slices.Equal(ids, readings) {
		t.Errorf("the readings of the series with no from are %v, want every one, oldest first: %v", ids, readings)
	}
	if apitest.Href(all, "self") != "/metrics/Weight" {
		t.Errorf("the series asked with no query names %s as itself", apitest.Href(all, "self"))
	}
	if apitest.Href(all, "all-readings") != "" {
		t.Errorf("the whole series links %s: every reading is in it", apitest.Href(all, "all-readings"))
	}
	window := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight?from=2026-01-01"))
	if n := len(window.Properties.(map[string]any)["readings"].([]any)); n != 2 {
		t.Errorf("from=2026-01-01 reads %d readings, want 2", n)
	}
	if apitest.Href(window, "all-readings") == "" {
		t.Error("a window does not lead back to every reading")
	}

	// Record from the page: a plain POST of the action the series offers, and the new reading is the first row.
	record := apitest.Action(t, all, "record")
	if f := record.Field("metric"); f == nil || f.Value != "Weight" {
		t.Errorf("the record action of the series does not fix its metric: %+v", f)
	}
	apitest.Must(t)(c.Do(t.Context(), record, map[string]string{"day": "2026-10-13", "value": "76.9"}))
	rows := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight")).Properties.(map[string]any)["readings"].([]any)
	if last := rows[len(rows)-1].(map[string]any); last["day"] != "2026-10-13" || fmt.Sprint(last["value"]) != "76.9" {
		t.Errorf("the reading recorded from the page is not the newest row: %v", last)
	}
}

// pageOf is the address of the page that a series carries (plan 144): its id is the series' own property. Empty for an
// entity with no page.
func pageOf(e *siren.Entity) string {
	n, ok := e.Properties.(map[string]any)["id"].(json.Number)
	if !ok {
		return ""
	}
	return "/pages/" + n.String()
}

// getAs is one request as a browser (html) or the JSON client sends it; the query rides in path.
func getAs(h http.Handler, method, path string, html bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if html {
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestTheCoreSendsAMetricsPageNowhere: the page of a metric is a page for every client, whatever it accepts, with the
// query kept: the core redirects nothing. It links to its series (rel series), where the metric has one view, and a
// deleted metric stays a page, with revive.
func TestTheCoreSendsAMetricsPageNowhere(t *testing.T) {
	c, h, _ := metricFixture(t)
	self := pageOf(apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight")))
	for _, path := range []string{self, self + "?new=edit-metric"} {
		rec := getAs(h, "GET", path, true)
		if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" || rec.Header().Get("Content-Type") != "application/vnd.siren+json" {
			t.Errorf("GET %s in a browser: %d %q %q, want 200 and the entity", path, rec.Code, rec.Header().Get("Location"), rec.Header().Get("Content-Type"))
		}
	}
	page := apitest.Must(t)(c.Get(t.Context(), self))
	if page.Class[0] != "metric" || page.Properties.(map[string]any)["body"] != "Before breakfast." || apitest.Href(page, "series") != "/metrics/Weight" {
		t.Errorf("the metric's page for JSON: %v %v", page.Class, page.Properties)
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "tombstone-pages"), nil))
	gone := apitest.Must(t)(c.Get(t.Context(), self))
	apitest.Action(t, gone, "revive")
	if !slices.Contains(gone.Class, "deleted") {
		t.Errorf("a deleted metric's page is %v, want a deleted page", gone.Class)
	}
}

// TestTheActionsOfAMetricsPageWork: the page of a metric offers save-body and edit-metric; a save of the body that
// links a new title makes that page and says so, and the mark sets the daily flag of the series.
func TestTheActionsOfAMetricsPageWork(t *testing.T) {
	c, _, _ := metricFixture(t)
	series := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight"))
	page := apitest.Must(t)(c.Get(t.Context(), pageOf(series)))
	saved := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "save-body"), map[string]string{"body": "Before breakfast. See [[Scales]]."}))
	if _, err := c.Get(t.Context(), "/pages?title=Scales"); err != nil {
		t.Errorf("the save did not make the page Scales: %v", err)
	}
	if saved.Result == nil {
		t.Error("the save says nothing of the page that it made")
	}
	page = apitest.Must(t)(c.Get(t.Context(), pageOf(series))) // the save changed the version
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "edit-metric"), map[string]string{"daily": "1"}))
	if daily := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight")).Properties.(map[string]any)["daily"]; daily != true {
		t.Errorf("the mark is not set after the save: daily = %v", daily)
	}
}
func TestTheSeriesCarriesItsPage(t *testing.T) {
	c, _, _ := metricFixture(t)
	series := apitest.Must(t)(c.Get(t.Context(), "/metrics/weight"))
	p := series.Properties.(map[string]any)
	if series.Title != "Weight" || p["unit"] != "kg" || pageOf(series) == "" || p["body"] != "Before breakfast." {
		t.Errorf("the series: title %q, %v", series.Title, p)
	}
	page := apitest.Must(t)(c.Get(t.Context(), pageOf(series)))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "rename"), map[string]string{"title": "Body weight"}))
	for _, path := range []string{"/metrics/Body%20weight", "/metrics/Weight"} {
		s := apitest.Must(t)(c.Get(t.Context(), path))
		if s.Title != "Body weight" || len(s.Properties.(map[string]any)["readings"].([]any)) != 3 {
			t.Errorf("%s after the rename: %q with %v", path, s.Title, s.Properties.(map[string]any)["readings"])
		}
	}
}

func TestAllReadingsIncludesEntireAdmittedCalendar(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	actions := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	metric := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, actions, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}))
	days := []string{"0000-01-01", "0000-02-29", "0001-01-01", "0001-01-02"}
	var plain []string
	values := map[string]string{}
	for i, day := range days {
		fact := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, actions, "record"), map[string]string{"metric": "Steps", "day": day, "value": fmt.Sprint(10 + i)}))
		id := apitest.Href(fact, "self")
		values[id] = fmt.Sprint(10 + i)
		plain = append(plain, id)
	}
	metricPage := apitest.Must(t)(c.Get(t.Context(), pageOf(metric)))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, metricPage, "rename"), map[string]string{"title": "Stride count"}))
	assertRows := func(t *testing.T, e *siren.Entity, want []string) {
		t.Helper()
		rows := e.Properties.(map[string]any)["readings"].([]any)
		got := []string{}
		for _, row := range rows {
			r := row.(map[string]any)
			got = append(got, fmt.Sprint("/measurements/", r["id"]))
			id := got[len(got)-1]
			if fmt.Sprint(r["value"]) != values[id] {
				t.Fatalf("row %s value=%v want=%v", id, r["value"], values[id])
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ordered identities=%v want=%v", got, want)
		}
	}
	// The all-readings link drops the lower bound and keeps the upper one; following it again changes nothing.
	q := url.Values{"from": {"0000-12-31"}, "to": {"0001-01-01"}}
	bounded := apitest.Must(t)(c.Get(t.Context(), "/metrics/Steps?"+q.Encode())) // retained alias
	link := apitest.Href(bounded, "all-readings")
	expanded := apitest.Must(t)(c.Get(t.Context(), link))
	assertRows(t, expanded, plain[:3])
	u, err := url.Parse(apitest.Href(expanded, "self"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Has("from") || u.Query().Get("to") != "0001-01-01" {
		t.Fatalf("history navigation=%s", u)
	}
	assertRows(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(expanded, "self"))), plain[:3])
	if apitest.Href(expanded, "all-readings") != "" {
		t.Errorf("all history links all-readings again: %s", apitest.Href(expanded, "all-readings"))
	}
	// Explicit ranges retain their exclusive lower and inclusive upper limits.
	bounded = apitest.Must(t)(c.Get(t.Context(), "/metrics/Stride%20count?from=0000-01-01&to=0000-02-29"))
	rows := bounded.Properties.(map[string]any)["readings"].([]any)
	if len(rows) != 1 || fmt.Sprint("/measurements/", rows[0].(map[string]any)["id"]) != plain[1] || fmt.Sprint(rows[0].(map[string]any)["value"]) != "11" {
		t.Fatal("bounded range semantics changed")
	}
	// A default window near the minimum must not manufacture negative-year input.
	assertRows(t, apitest.Must(t)(c.Get(t.Context(), "/metrics/Steps?to=0000-01-01")), plain[:1])
	if _, err := c.Get(t.Context(), "/metrics/Steps?to=-001-01-01"); err == nil {
		t.Fatal("accepted a to that is not a day")
	}
}

// New metric on /metrics (docs/plans/113-new-metric-on-metrics.md): one form of register-metric, a name, a unit and a
// note (docs/plans/131-the-pareto-pass.md, part 3). file-pages files a metric in a category.

func TestRegisterMetricTakesANameAUnitANoteAndTheDailyMark(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	register := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "register-metric")
	if !register.Owner || register.Href != "/metrics" {
		t.Fatalf("/metrics offers register-metric as %+v", register)
	}
	var fields []string
	for _, f := range register.Fields {
		fields = append(fields, f.Name)
	}
	if !slices.Equal(fields, []string{"name", "unit", "note", "daily"}) {
		t.Errorf("register-metric fields: %v", fields)
	}
	walk := apitest.Must(t)(c.Do(ctx, register, map[string]string{"name": "Walk"}))
	if walk.Result != "New metric Walk." {
		t.Errorf("the answer's sentence: %v", walk.Result)
	}
	if again := apitest.Must(t)(c.Do(ctx, register, map[string]string{"name": "walk"})); again.Result != "Walk exists." {
		t.Errorf("the answer's sentence of a metric that exists: %v", again.Result)
	}
	if got, want := groupsOf(t, c), "Not filed:Mood,Walk"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}
}

// TestTheMetricsPageMakesAMetric: New metric is the register-metric action of /metrics, the first of its actions; the
// address ?new=register-metric (with the fields of the form in the query) is served; the action makes the metric with
// its unit and its note. The day offers no register-metric (docs/plans/131-the-pareto-pass.md, part 2).
func TestTheMetricsPageMakesAMetric(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	metrics := apitest.Must(t)(c.Get(ctx, "/metrics"))
	if metrics.Actions[0].Name != "register-metric" {
		t.Errorf("the first action of /metrics is %s, want register-metric", metrics.Actions[0].Name)
	}
	if hasAction(apitest.Must(t)(c.Get(ctx, "/days/"+fixedToday)), "register-metric") {
		t.Error("the day offers register-metric")
	}
	asked := apitest.Must(t)(c.Get(ctx, "/metrics?name=Steps&unit=steps&new=register-metric"))
	apitest.Action(t, asked, "register-metric")
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, metrics, "register-metric"), map[string]string{"name": "Weight", "unit": "kg", "note": "Before breakfast"}))
	made := map[string]string{}
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		made[m["name"].(string)] = fmt.Sprintf("unit %q, note %v", m["unit"], m["note"])
	}
	if want := `unit "kg", note Before breakfast`; made["Weight"] != want {
		t.Errorf("the action made Weight with %s, want %s", made["Weight"], want)
	}
}

// TestAScoreRangeOutOfOrderIsRefused: a score's range is lo-hi with lo less than hi (metrics_range_order, D6). A
// client can send 5-1: the server answers the schema's 422 with the sentence of the trigger, and nothing is written.
func TestAScoreRangeOutOfOrderIsRefused(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	register := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/metrics")), "register-metric")
	_, err := c.Do(t.Context(), register, map[string]string{"name": "Pain", "unit": "5-1"})
	const trigger = "a scale names its range as its unit, lo-hi with lo less than hi, as in 1-5 (D6)"
	if apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(err.Error(), trigger) {
		t.Errorf("a score with the range 5-1: %v, want 422 with the sentence of the trigger", err)
	}
	if e, err := c.Get(t.Context(), "/pages?title=Pain"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("the page Pain after the refusal: %v %v, want none", e, err)
	}
}

// The metrics as cards (docs/plans/112-metrics-as-cards.md): the cards that /metrics draws. Several metrics filed in a
// category in one write are TestFileMetricsAtOnce's.

// cardsFixture registers Ferritin (read on two days long ago), Weight (read on the two days before fixedToday), Walk
// (a habit) and Steps (never read), and returns the client, the handler and the metric ids by name.
func cardsFixture(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	for _, m := range []map[string]string{{"name": "Ferritin", "unit": "ug/L", "note": "iron stores"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), m))
	}
	record := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics/Ferritin")), "record")
	for _, r := range []map[string]string{{"metric": "Ferritin", "day": "2030-03-01", "value": "40"}, {"metric": "Ferritin", "day": "2030-09-01", "value": "55"},
		{"metric": "Weight", "day": "2031-05-12", "value": "80"}, {"metric": "Weight", "day": "2031-05-13", "value": "79.5"}} {
		apitest.Must(t)(c.Do(ctx, record, r))
	}
	ids := map[string]string{}
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		ids[m["name"].(string)] = fmt.Sprint(m["id"])
	}
	return ids
}

// groupsOf says the groups of /metrics: "title:names" for each, in order.
func groupsOf(t *testing.T, c *client.Client) string {
	t.Helper()
	var got []string
	for _, g := range props(apitest.Must(t)(c.Get(t.Context(), "/metrics")))["groups"].([]any) {
		g := g.(map[string]any)
		var ms []string
		for _, m := range g["metrics"].([]any) {
			ms = append(ms, m.(map[string]any)["name"].(string))
		}
		got = append(got, g["title"].(string)+":"+strings.Join(ms, ","))
	}
	return strings.Join(got, " ")
}

func TestTheMetricsPageDrawsCards(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"], "category": "Iron"}))
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Weight"] + "," + ids["Walk"], "category": "Body"}))
	iron := apitest.Must(t)(c.Get(ctx, "/pages?title=Iron"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, iron, "file-pages"), map[string]string{"category": "Body"}))

	// The JSON carries each metric's recent readings, oldest first.
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		if m["name"] != "Weight" {
			continue
		}
		r := m["recent"].([]any)
		if len(r) != 2 || r[0].(map[string]any)["day"] != "2031-05-12" || r[1].(map[string]any)["value"] != json.Number("79.5") {
			t.Errorf("Weight's recent readings: %v", r)
		}
	}

	// The pick bar: one filing of several metrics at once, as a client sends it (a box per card): the answer says it in
	// words, and the board has no Not filed group when everything is filed.
	done := apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Mood"] + "," + ids["Steps"], "category": "Daily"}))
	if done.Result != "Put Mood and Steps in Daily (a new category)." {
		t.Errorf("the filing says %v", done.Result)
	}
	if got := groupsOf(t, c); strings.Contains(got, "Not filed") {
		t.Errorf("groups %q after the filing, want no Not filed group", got)
	}
	// A filing with no id is refused and writes nothing.
	if _, err := c.Do(ctx, file, map[string]string{"category": "Daily", "move": "1"}); apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(err.Error(), "missing field ids") {
		t.Errorf("no box checked: %v, want 422 missing field ids", err)
	}
}

// TestAReadingLinkWritesItsNumberAsNumDoes: the title of a reading's link, in a day and in a series, writes the value
// as every other place writes it: 1000000, never 1e+06, and 0.00001, never 1e-05.
func TestAReadingLinkWritesItsNumberAsNumDoes(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", "2026-10-13")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "steps"}))
	for _, v := range []string{"1000000", "0.00001"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "steps", "day": "2026-10-12", "value": v}))
	}
	for path, want := range map[string][]string{
		"/days/2026-10-12": {"steps 0.00001", "steps 1000000"},
		"/metrics/steps":   {"2026-10-12 0.00001", "2026-10-12 1000000"},
	} {
		var got []string
		for _, l := range apitest.Must(t)(c.Get(t.Context(), path)).Entities {
			if slices.Contains(l.Rel, "reading") {
				got = append(got, l.Title)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: the reading links are %q, want %q", path, got, want)
		}
	}
}

// TestAReadingSentAgainLinksTheDayOfTheStoredReading: a reading sent again with the same import_key answers "Already
// recorded", and its day link is the day of the reading that stands, not the day of the request. The first send says
// day=today on 2026-10-13. The next ones come on the next day: one says today again, and one names another day.
func TestAReadingSentAgainLinksTheDayOfTheStoredReading(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	c := client.InProcess(api.NewWith(&core.Store{DB: dbtest.New(t), Clock: func() time.Time { return now }, Zone: time.UTC}, nil), "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	record := func(day string) *siren.Entity {
		t.Helper()
		return apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": day, "value": "71.2", "import_key": "scale/2026-10-13"}))
	}
	if first := record("today"); apitest.Href(first, "day") != "/days/2026-10-13" {
		t.Fatalf("the first send links the day %q, want /days/2026-10-13", apitest.Href(first, "day"))
	}
	now = now.AddDate(0, 0, 1)
	for _, day := range []string{"today", "2026-09-01"} {
		again := record(day)
		if again.Title != "Already recorded" || apitest.Href(again, "day") != "/days/2026-10-13" {
			t.Errorf("sent again with day=%s: %q links the day %q, want Already recorded with /days/2026-10-13", day, again.Title, apitest.Href(again, "day"))
		}
	}
}

// TestAPlainPageLeadsToTheNewMetricForm: the owner who made a page where a metric was wanted finds the link
// make-metric on it, and the address of the link is served and offers register-metric. Registering turns the page into
// the metric with the same id and the same text. A day page and a person have no such link, and a metric no more.
func TestAPlainPageLeadsToTheNewMetricForm(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	pg := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Steps", "body": "Counted by the watch."}))
	pgHref := apitest.Href(pg, "self")
	const want = "/metrics?name=Steps&new=register-metric"
	if got := apitest.Href(apitest.Must(t)(c.Get(ctx, pgHref)), "make-metric"); got != want {
		t.Errorf("make-metric of a plain page: %q, want %q", got, want)
	}

	person := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Ada"}))
	day := apitest.Must(t)(c.Get(ctx, "/days/"+fixedToday))
	for name, e := range map[string]*siren.Entity{"person": person, "day page": apitest.Must(t)(c.Get(ctx, apitest.Href(day, "page")))} {
		if got := apitest.Href(apitest.Must(t)(c.Get(ctx, apitest.Href(e, "self"))), "make-metric"); got != "" {
			t.Errorf("a %s has make-metric %q", name, got)
		}
	}

	form := apitest.Must(t)(c.Get(ctx, want))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, form, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}))
	after := apitest.Must(t)(c.Get(ctx, pgHref))
	if !slices.Contains(after.Class, "metric") {
		t.Errorf("the page is not a metric: class %v", after.Class)
	}
	if body := after.Properties.(map[string]any)["body"]; body != "Counted by the watch." {
		t.Errorf("the metric keeps the text? body %q", body)
	}
	if got := apitest.Href(after, "make-metric"); got != "" {
		t.Errorf("a metric has make-metric %q", got)
	}
}

// TestARecordOfAnUnknownMetricSaysHowToRegisterIt: the refusal names the action that makes the metric.
func TestARecordOfAnUnknownMetricSaysHowToRegisterIt(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	_, err := c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Nothing", "day": fixedToday, "value": "1"})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusNotFound || !strings.Contains(ce.Error(), "register-metric") {
		t.Errorf("record of an unknown metric: %v, want 404 naming register-metric", err)
	}
}
