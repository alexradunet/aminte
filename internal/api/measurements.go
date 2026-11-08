package api

// The readings: the metrics, a series, a reading, and the writes of readings.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"aminte/internal/core"
	"aminte/siren"
)

// recordAction offers every live metric of ms by name, and the metric the view is of (and, when given, selects it).
// The action itself takes any registered metric, so the list is a help and not a rule. ms is the registry
// (Store.Metrics), which the caller reads once for the whole view.
func recordAction(ms []core.Metric, metric, day string) siren.Action {
	a := action("record", nil, map[string]any{"day": day})
	if metric != "" {
		a.Field("metric").Value = metric
	}
	names := make([]string, len(ms)) // Metrics are in the order of their names
	for i, m := range ms {
		names[i] = m.Name
	}
	if metric != "" && !slices.Contains(names, metric) {
		names = append(names, metric)
		slices.Sort(names)
	}
	return withOptions(a, "metric", names)
}

// dailyOf are the names of the daily metrics of ms, in name order; never nil, so the property is an array.
func dailyOf(ms []core.Metric) []string {
	names := []string{}
	for _, m := range ms {
		if m.Daily {
			names = append(names, m.Name)
		}
	}
	return names
}

// unitsOf is the unit of each metric of ms that readings name or that is in daily, by metric name; a metric with no
// unit is left out.
func unitsOf(ms []core.Metric, readings []core.Reading, daily []string) map[string]string {
	read := map[string]bool{}
	for _, r := range readings {
		read[r.Metric] = true
	}
	units := map[string]string{}
	for _, m := range ms {
		if (read[m.Name] || slices.Contains(daily, m.Name)) && m.Unit != "" {
			units[m.Name] = m.Unit
		}
	}
	return units
}

// recentReadings is how many of a metric's newest readings /metrics reads for its card: the latest, and the line of
// the ones before it.
const recentReadings = 30

// metricRow is a metric as /metrics lists it: the metric, and its newest current unassociated readings, oldest first
// (the last is its latest).
type metricRow struct {
	core.Metric
	Recent []core.Recent `json:"recent"`
}

func (h *server) metrics(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	recent, err := h.s.RecentReadings(ctx, recentReadings)
	if err != nil {
		return nil, err
	}
	var names []string
	rows := []metricRow{}
	for _, m := range ms {
		names = append(names, m.Name)
		rows = append(rows, metricRow{Metric: m, Recent: append([]core.Recent{}, recent[m.ID]...)})
	}
	cs, err := h.s.Categories(ctx)
	if err != nil {
		return nil, err
	}
	var titles []string // the category of file-pages and of register-metric offers them, as Browse and a page do
	for _, c := range cs {
		titles = append(titles, c.Title)
	}
	today := h.s.Today()
	e := &siren.Entity{Class: []string{"metrics"}, Title: "Metrics",
		Properties: map[string]any{"metrics": rows, "groups": groupMetrics(ms, cs), "categories": cs, "today": today},
		Links:      []siren.Link{link("self", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []siren.Action{action("register-metric", nil, nil), // first: the head draws the first create action as its primary button
			withOptions(action("record", nil, map[string]any{"day": today}), "metric", names),
			withOptions(action("file-pages", nil, nil), "category", titles)}}
	for _, m := range ms {
		e.Entities = append(e.Entities, link("item", siren.MetricHref(m.Name), m.Name))
	}
	return e, nil
}

// metricGroup is one section of /metrics: a category page with the metrics filed in it (D26), or the metrics
// filed nowhere. Depth is the category's depth in the drawn tree.
type metricGroup struct {
	Title   string        `json:"title"`
	Path    string        `json:"path,omitempty"`
	Page    int64         `json:"page,omitempty"`
	Depth   int           `json:"depth"`
	Metrics []core.Metric `json:"metrics"`
}

// groupMetrics lists a metric under each category it is filed in, in the order of the tree; the rest last. A category is shown when it or a category under it holds a metric.
func groupMetrics(ms []core.Metric, cs []core.Category) []metricGroup {
	filed := map[string][]core.Metric{}
	var none []core.Metric
	for _, m := range ms {
		switch {
		case len(m.Categories) > 0:
			for _, p := range m.Categories {
				filed[p] = append(filed[p], m)
			}
		default:
			none = append(none, m)
		}
	}
	var out []metricGroup
	for _, c := range cs { // by path: a category comes before the categories under it
		held := false
		for p := range filed {
			if p == c.Path || strings.HasPrefix(p, c.Path+"/") {
				held = true
				break
			}
		}
		if !held {
			continue
		}
		out = append(out, metricGroup{Title: c.Title, Path: c.Path, Page: c.ID, Depth: strings.Count(c.Path, "/"),
			Metrics: append([]core.Metric{}, filed[c.Path]...)})
	}
	if len(none) > 0 {
		out = append(out, metricGroup{Title: "Not filed", Metrics: none})
	}
	return out
}

// series is cookbook/metric-series.md; ?to= defaults to today, and with no ?from= every reading through it is read.
func (h *server) series(r *http.Request) (*siren.Entity, error) {
	return h.seriesEntity(r.Context(), r.PathValue("name"), r.URL.Query())
}

// seriesEntity is the resource /metrics/{name} serves: a metric's readings and the write of a reading. The metric
// is a page (D27), and the series carries the page: its properties, links, embedded links and actions (plan 144),
// so one view draws both. q is its query (from, to), nil for the defaults.
func (h *server) seriesEntity(ctx context.Context, name string, q url.Values) (*siren.Entity, error) {
	today := h.s.Today()
	to := q.Get("to")
	if to == "" {
		to = today
	}
	from := q.Get("from") // no lower bound: every admitted day through to
	m, err := h.metricOf(ctx, name)
	if err != nil {
		return nil, err
	}
	if m != nil { // an alias names the metric by its name
		name = m.Name
	}
	rows, err := h.s.Series(ctx, name, from, to) // an empty from is all history
	if err != nil {
		return nil, err
	}
	// The series' own address, and the address of all its history: the same query with no lower bound. A query of
	// the defaults is no query, so a browser that registers a metric lands on its bare address.
	href := func(from string) string {
		v := url.Values{}
		if from != "" {
			v.Set("from", from)
		}
		if to != today {
			v.Set("to", to)
		}
		if len(v) == 0 {
			return siren.MetricHref(name)
		}
		return siren.MetricHref(name) + "?" + v.Encode()
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	record := recordAction(ms, name, today)
	props := map[string]any{"metric": name, "from": from, "to": to, "readings": rows}
	e := &siren.Entity{Class: []string{"series"}, Title: name, Properties: props,
		Links:   []siren.Link{link("self", href(from), name), link("up", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []siren.Action{record}}
	if from != "" {
		e.Links = append(e.Links, link("all-readings", href(""), "All readings"))
	}
	if m != nil {
		props["unit"] = m.Unit
		props["daily"] = m.Daily
	}
	for _, m := range rows {
		e.Entities = append(e.Entities, link("reading", siren.MeasurementHref(m.ID), m.Day+" "+siren.Num(m.Value)))
	}
	if m != nil {
		if err := h.carryPage(ctx, e, m.ID); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// viewLinkRels are the rels of the links of a page that a view carrying the page leaves out: the view has its own self
// and index, and the page's link to the view (series, conversation) would lead to the view itself.
var viewLinkRels = []string{"self", "series", "conversation", "index"}

// carryPage adds the page of a metric to its series e, or the page of a day's talk to its conversation. The view keeps
// what it sets (its class, its title, its properties, its self and index links); the page's `metric` object is left out,
// since the series holds the unit and the mark. The links viewLinkRels names are left out too. Every other property,
// link, embedded link and action of the page is added once.
func (h *server) carryPage(ctx context.Context, e *siren.Entity, id int64) error {
	pe, err := h.pageEntity(ctx, id)
	if err != nil {
		return err
	}
	b, err := json.Marshal(pe.Properties)
	if err != nil {
		return err
	}
	var page map[string]any
	if err := json.Unmarshal(b, &page); err != nil {
		return err
	}
	props := e.Properties.(map[string]any)
	for k, v := range page {
		if _, set := props[k]; !set && k != "metric" {
			props[k] = v
		}
	}
	for _, l := range pe.Links {
		if !slices.ContainsFunc(l.Rel, func(rel string) bool { return slices.Contains(viewLinkRels, rel) }) {
			e.Links = append(e.Links, l)
		}
	}
	e.Entities = append(e.Entities, pe.Entities...)
	e.Actions = append(e.Actions, pe.Actions...)
	return nil
}

// metricOf is the live metric that name names, by its name or by an alias its renames kept (D27, as a series is
// read); nil when it names none.
func (h *server) metricOf(ctx context.Context, name string) (*core.Metric, error) {
	id, err := h.s.PageID(ctx, name)
	if err != nil || id == 0 {
		return nil, err
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i], nil
		}
	}
	return nil, nil
}

func (h *server) measurement(r *http.Request) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(r.Context(), id)
}

func (h *server) measurementEntity(ctx context.Context, id int64) (*siren.Entity, error) {
	m, err := h.s.Measurement(ctx, id)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"measurement"}, Title: fmt.Sprintf("%s on %s", m.Metric, m.Day), Properties: m,
		Links: []siren.Link{link("self", siren.MeasurementHref(id), "This reading"), link("day", siren.DayHref(m.Day), m.Day),
			link("series", siren.MetricHref(m.Metric), m.Metric), link("index", "/", "Home")}}
	if m.Supersedes != 0 {
		e.Links = append(e.Links, link("supersedes", siren.MeasurementHref(m.Supersedes), "Corrects"))
	}
	if m.SupersededBy != 0 {
		e.Links = append(e.Links, link("superseded-by", siren.MeasurementHref(m.SupersededBy), "Corrected by"))
	} else {
		if !m.Retraction { // a retraction has no value to correct, and nothing left to retract
			ids := idParam(id)
			e.Actions = []siren.Action{action("correct", ids, map[string]any{"value": m.Value}), action("retract", ids, nil)}
			e.Links = append(e.Links, link("record", siren.NewReadingHref(m.Metric, m.Day, m.Value, ""), "Add it again"))
		}
	}
	if m.CapturedWith != 0 {
		e.Links = append(e.Links, link("captured-with", siren.PageHref(m.CapturedWith), "Captured with"))
	}
	return e, nil
}

func (h *server) record(r *http.Request, a admitted) (*siren.Entity, error) {
	value, err := number(a.v, "value")
	if err != nil {
		return nil, err
	}
	day := h.dayNamed(a.v.Get("day"))
	id, err := h.s.Record(r.Context(), a.src, core.Reading{Metric: a.v.Get("metric"), Day: day, Value: *value,
		TakenAt: a.v.Get("taken_at"), TZ: a.v.Get("tz"), ImportKey: a.v.Get("import_key")})
	if err != nil {
		return nil, err
	}
	if id == 0 { // a re-send: the first one stands (docs/contract/imports.md), and the answer links its day
		if day, err = h.s.RecordedDay(r.Context(), a.src, a.v.Get("import_key"), a.v.Get("metric")); err != nil {
			return nil, err
		}
		return &siren.Entity{Class: []string{"result"}, Title: "Already recorded",
			Properties: map[string]any{"message": "a reading with this import_key was recorded before; nothing changed"},
			Result:     map[string]any{"already": true},
			Links:      []siren.Link{link("self", siren.DayHref(day), day), link("day", siren.DayHref(day), day), link("index", "/", "Home")}}, nil
	}
	return h.measurementEntity(r.Context(), id)
}

func (h *server) correct(r *http.Request, a admitted) (*siren.Entity, error) {
	value, err := number(a.v, "value") // never nil: the catalog requires value, and a nil value retracts
	if err != nil {
		return nil, err
	}
	return h.correctTo(r.Context(), a.src, a.id, value)
}

func (h *server) retract(r *http.Request, a admitted) (*siren.Entity, error) {
	return h.correctTo(r.Context(), a.src, a.id, nil)
}

// correctTo corrects or retracts a reading.
func (h *server) correctTo(ctx context.Context, src string, id int64, value *float64) (*siren.Entity, error) {
	fix, err := h.s.Correct(ctx, src, id, value)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(ctx, fix)
}

func (h *server) registerMetric(r *http.Request, a admitted) (*siren.Entity, error) {
	m := core.NewMetric{Name: a.v.Get("name"), Unit: a.v.Get("unit"), Note: a.v.Get("note")}
	if a.v.Has("daily") { // an unticked checkbox sends nothing: the mark is then not asked about
		daily := flag(a.v, "daily")
		m.Daily = &daily
	}
	added, err := h.s.CreateMetric(r.Context(), a.src, m)
	if err != nil {
		return nil, err
	}
	e, err := h.seriesEntity(r.Context(), m.Name, nil)
	if err != nil {
		return nil, err
	}
	result := "New metric " + e.Title
	if !added {
		result = e.Title + " exists"
	}
	e.Result = result + "."
	return e, nil
}
