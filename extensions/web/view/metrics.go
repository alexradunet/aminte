package view

import (
	"html/template"
	"regexp"
	"strings"

	"lifelog/siren"
	"lifelog/text"
)

// The one page of a metric (docs/plans/100-one-page-a-metric.md): its readings newest first, in one fold a year.
// The series gives them oldest first; only the view orders and folds them.

// newestRows is how many readings a year's fold shows; the rest of the year are in a closed fold under them, so the
// page's other groups stay near on a long series.
const newestRows = 10

// openFolds is how many readings a series may hold for every year's fold to be open: a short series (a lab
// marker read twice a year) is read whole, and a long one opens on its newest year.
const openFolds = 60

// readingYear is one fold of a metric's readings: a year, its newest readings, the rest of them (More), the line its
// summary says of them all, and whether it is open.
type readingYear struct {
	Year    string
	Rows    []map[string]any
	More    []map[string]any
	Summary string
	Open    bool
}

// metricReadings are a series' readings as its page draws them: the folds, newest year first; the newest reading;
// and whether a column of times is needed.
type metricReadings struct {
	Years  []readingYear
	Latest map[string]any
	Times  bool
}

// MetricReadings are the series' readings by year, newest first.
func (v *view) MetricReadings() metricReadings {
	list, _ := v.Props["readings"].([]any)
	unit, _ := v.Props["unit"].(string)
	r := metricReadings{}
	for i := len(list) - 1; i >= 0; i-- {
		m, _ := list[i].(map[string]any)
		day, _ := m["day"].(string)
		if !text.IsDay(day) {
			continue
		}
		if n := len(r.Years); n == 0 || r.Years[n-1].Year != day[:4] {
			r.Years = append(r.Years, readingYear{Year: day[:4]})
		}
		y := &r.Years[len(r.Years)-1]
		y.Rows = append(y.Rows, m)
		if _, ok := m["taken_at"]; ok {
			r.Times = true
		}
	}
	for i := range r.Years {
		y := &r.Years[i]
		y.Open = i == 0 || len(list) <= openFolds
		y.Summary = yearSummary(y.Rows, unit)
		if len(y.Rows) > newestRows {
			y.Rows, y.More = y.Rows[:newestRows], y.Rows[newestRows:]
		}
	}
	if len(r.Years) > 0 {
		r.Latest = r.Years[0].Rows[0]
	}
	return r
}

// yearSummary is what a closed fold says of its year: "214 readings · 76.2 to 81 kg". The unit is in words, as a
// tile writes it: "6 readings · 1 to 5 of 5".
func yearSummary(rows []map[string]any, unit string) string {
	var lo, hi float64
	n := 0
	for _, m := range rows {
		v, ok := m["value"].(float64)
		if !ok {
			continue
		}
		if n == 0 || v < lo {
			lo = v
		}
		if n == 0 || v > hi {
			hi = v
		}
		n++
	}
	s := Count(len(rows), "reading", "readings")
	if n == 0 {
		return s
	}
	values := Num(lo)
	if hi != lo {
		values += " to " + Num(hi)
	}
	if unit != "" {
		values += " " + unitWords(unit)
	}
	return s + " · " + values
}

// The metrics as cards (docs/plans/112-metrics-as-cards.md): a section for each top-level group of /metrics, its
// subcategories inside it, and a card for each metric with its latest reading and the line of its recent ones.

// metricCard is a metric as its card draws it.
type metricCard struct {
	ID          string
	Name, Href  string
	Value, Unit string
	Measurement string // the latest reading's id, "" with none
	Day         string // the latest reading's day, "" with none
	When        string // the latest reading's day in words
	Short       string // the first words of When: "2 days ago", or the day when it is not near today
	Count       int    // how many recent readings the line holds
	Line        template.HTML
	Stale       bool // the latest reading is older than staleDays
	Note        string
}

// metricSub is a category under a section's top: its path below the top, its page, its cards and the day of its
// newest reading in words.
type metricSub struct {
	Title, Page, Latest string
	Cards               []metricCard
}

// metricSection is one top-level group: a category, or Not filed. Count is how many metrics it holds, each
// once, in its subcategories too. Latest is the day of the newest reading among them, in words: the line of the
// group's fold says it (docs/plans/118-fold-and-find.md, rule 4).
type metricSection struct {
	Title, Page, Anchor, Latest string
	Count                       int
	Cards                       []metricCard
	Subs                        []metricSub
}

// newest is the day of the newest reading of the cards, "" with none.
func newest(cards []metricCard) string {
	day := ""
	for _, k := range cards {
		day = max(day, k.Day)
	}
	return day
}

// MetricSections are the groups of /metrics as sections of cards, in the order of the groups. The words of a day
// and whether a card is stale are said against the today of the view's clock.
func (v *view) MetricSections() []metricSection {
	groups, _ := v.Props["groups"].([]any)
	rows := map[string]map[string]any{}
	if ms, _ := v.Props["metrics"].([]any); ms != nil {
		for _, x := range ms {
			if m, _ := x.(map[string]any); m != nil {
				rows[Num(m["id"])] = m
			}
		}
	}
	c := v.clock()
	limit, _ := text.ParseDay(c.today)
	limit = limit.AddDate(0, 0, -staleDays)
	card := func(x any) metricCard {
		g, _ := x.(map[string]any)
		id := Num(g["id"])
		m := rows[id]
		if m == nil {
			m = g
		}
		name, _ := m["name"].(string)
		unit, _ := m["unit"].(string)
		note, _ := m["note"].(string)
		k := metricCard{ID: id, Name: name, Href: siren.MetricHref(name), Unit: unitWords(unit), Note: note}
		recent, _ := m["recent"].([]any)
		if n := len(recent); n > 0 {
			last, _ := recent[n-1].(map[string]any)
			day, _ := last["day"].(string)
			k.Value, k.Measurement, k.Day, k.When, k.Count = Num(last["value"]), Num(last["id"]), day, c.relative(day), n
			k.Short, _, _ = strings.Cut(k.When, " · ")
			if t, ok := text.ParseDay(day); ok && !limit.IsZero() && t.Before(limit) {
				k.Stale = true
			}
			k.Line = sparkLine(c, name, unit, recent)
		}
		return k
	}
	var out []metricSection
	var seen map[string]bool
	for _, x := range groups {
		g, _ := x.(map[string]any)
		title, _ := g["title"].(string)
		depth := Num(g["depth"])
		ms, _ := g["metrics"].([]any)
		if depth == "0" || len(out) == 0 {
			s := metricSection{Title: title, Page: Num(g["page"])}
			switch {
			case s.Page != "":
				s.Anchor = "c-" + s.Page
			default:
				s.Anchor = "not-filed"
			}
			out = append(out, s)
			seen = map[string]bool{}
		}
		s := &out[len(out)-1]
		var cards []metricCard
		for _, m := range ms {
			k := card(m)
			cards = append(cards, k)
			if !seen[k.ID] {
				seen[k.ID] = true
				s.Count++
			}
		}
		if depth == "0" {
			s.Cards = cards
			continue
		}
		path, _ := g["path"].(string)
		sub := path
		if i := strings.Index(path, "/"); i >= 0 {
			sub = path[i+1:]
		}
		k := metricSub{Title: sub, Page: Num(g["page"]), Cards: cards}
		if d := newest(cards); d != "" {
			k.Latest = c.relative(d)
		}
		s.Subs = append(s.Subs, k)
	}
	for i := range out {
		day := newest(out[i].Cards)
		for _, k := range out[i].Subs {
			day = max(day, newest(k.Cards))
		}
		if day != "" {
			out[i].Latest = c.relative(day)
		}
	}
	return out
}

// readingTile is a metric of the day: its last reading of the day (the one to correct, with an ID), and how many more
// readings of the day it stands for. A tile with no reading (an empty ID) is a daily metric asked for: it draws the form.
type readingTile struct {
	Metric, Value, Unit string
	ID                  string
	More                int
}

// ReadingTiles are the tiles of the day: first the daily metrics (property daily), in the order given, then the other
// metrics read that day, in the order of their first reading. A metric has one tile.
func (v *view) ReadingTiles() []readingTile {
	daily, _ := v.Props["daily"].([]any)
	readings, _ := v.Props["readings"].([]any)
	units, _ := v.Props["units"].(map[string]any)
	var out []readingTile
	at := map[string]int{}
	add := func(metric string) int {
		i, seen := at[metric]
		if !seen {
			i, at[metric] = len(out), len(out)
			unit, _ := units[metric].(string)
			out = append(out, readingTile{Metric: metric, Unit: unit})
		}
		return i
	}
	for _, d := range daily {
		name, _ := d.(string)
		add(name)
	}
	read := map[string]bool{}
	for _, r := range readings {
		m, _ := r.(map[string]any)
		metric, _ := m["metric"].(string)
		i := add(metric)
		if read[metric] {
			out[i].More++
		}
		read[metric] = true
		out[i].ID, out[i].Value = Num(m["id"]), Num(m["value"])
	}
	return out
}

var scaleUnit = regexp.MustCompile(`^\d+-(\d+)$`)

// unitWords is a unit as it follows a value: a scale "1-5" is "of 5"; anything else is itself.
func unitWords(unit string) string {
	if m := scaleUnit.FindStringSubmatch(unit); m != nil {
		return "of " + m[1]
	}
	return unit
}

// staleDays is the age of the latest reading after which a metric's card is muted: a series read at least monthly
// stays plain, a lab marker read twice a year is muted.
const staleDays = 60
