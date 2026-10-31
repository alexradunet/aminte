package view

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// TestMetricSectionsSayDaysByTheClockOfTheView: the cards of /metrics say a day, and whether a card is stale, against
// the today of the view's clock (the shell's), not against the property today of the resource. Here the property
// says 1 January 2026 and the shell 16 October 2026: a reading of 14 October is "2 days ago", and one of 1 July is
// stale (older than 60 days).
func TestMetricSectionsSayDaysByTheClockOfTheView(t *testing.T) {
	metric := func(id float64, name, day string) any {
		return map[string]any{"id": id, "name": name, "unit": "kg", "recent": []any{map[string]any{"id": id * 10, "day": day, "value": 70.0}}}
	}
	v := &view{Shell: &Shell{Today: "2026-10-16", Zone: time.UTC}, Props: map[string]any{"today": "2026-01-01",
		"groups": []any{map[string]any{"title": "Not filed", "depth": 0.0, "metrics": []any{metric(1, "weight", "2026-10-14"), metric(2, "ferritin", "2026-07-01")}}}}}
	s := v.MetricSections()
	if len(s) != 1 || len(s[0].Cards) != 2 {
		t.Fatalf("sections %+v, want one with two cards", s)
	}
	weight, ferritin := s[0].Cards[0], s[0].Cards[1]
	if weight.When != "2 days ago · Wed 14 Oct" || weight.Short != "2 days ago" || weight.Stale {
		t.Errorf("weight: when %q, short %q, stale %v; want 2 days ago · Wed 14 Oct, 2 days ago, not stale", weight.When, weight.Short, weight.Stale)
	}
	if ferritin.When != "Wed 1 Jul" || !ferritin.Stale {
		t.Errorf("ferritin: when %q, stale %v; want Wed 1 Jul, stale", ferritin.When, ferritin.Stale)
	}
	if s[0].Latest != "2 days ago · Wed 14 Oct" {
		t.Errorf("the section's newest day is %q, want 2 days ago · Wed 14 Oct", s[0].Latest)
	}
}

// TestAYearsSummary: the line of a year's fold counts every reading of the year, a retraction too, and gives the
// least and the greatest value of the readings that have a number, in full, with the unit as a tile writes it: a
// scale 1-5 is "of 5".
func TestAYearsSummary(t *testing.T) {
	rows := func(values ...any) []map[string]any {
		out := []map[string]any{}
		for _, v := range values {
			out = append(out, map[string]any{"value": v})
		}
		return out
	}
	for _, c := range []struct {
		rows []map[string]any
		unit string
		want string
	}{
		{rows(), "kg", "0 readings"},
		{rows(70.0), "kg", "1 reading · 70 kg"},
		{rows(70.0, 71.0), "kg", "2 readings · 70 to 71 kg"},
		{rows(80.0, 70.0), "kg", "2 readings · 70 to 80 kg"},
		{rows(71.0, 70.5, 72.0), "kg", "3 readings · 70.5 to 72 kg"},
		{rows(70.0, 70.0), "kg", "2 readings · 70 kg"},
		{rows(70.0, 71.0), "", "2 readings · 70 to 71"},
		{rows(nil, 70.0), "kg", "2 readings · 70 kg"},
		{rows(nil), "kg", "1 reading"},
		{rows(1000000.0, 0.00001), "C", "2 readings · 0.00001 to 1000000 C"},
		{rows(3.0, 1.0, 5.0, 2.0, 4.0, 5.0), "1-5", "6 readings · 1 to 5 of 5"},
		{rows(4.0), "1-5", "1 reading · 4 of 5"},
	} {
		if got := yearSummary(c.rows, c.unit); got != c.want {
			t.Errorf("yearSummary(%v, %q) = %q, want %q", c.rows, c.unit, got, c.want)
		}
	}
}

// TestAMetricsReadingsFoldByYear: a series' readings, given oldest first, are drawn newest first, in a fold a year,
// newest year first. A reading with no day is left out. A year shows its 10 newest readings and folds the rest (More).
// Every fold is open while the series holds at most 60 readings; with more, only the newest year's. Latest is the
// newest reading, and Times says that a reading has a time.
func TestAMetricsReadingsFoldByYear(t *testing.T) {
	reading := func(day string, value float64) map[string]any { return map[string]any{"day": day, "value": value} }
	days := func(year string, n int) []any { // n readings of a year, from 1 January, a day each
		var out []any
		for i := range n {
			out = append(out, reading(fmt.Sprintf("%s-01-%02d", year, i+1), float64(i)))
		}
		return out
	}
	timed := map[string]any{"day": "2026-10-12", "value": 71.0, "taken_at": "2026-10-12T06:00:00Z"}
	type fold struct {
		year, first, summary string
		rows, more           int
		open                 bool
	}
	for _, c := range []struct {
		name   string
		props  map[string]any
		want   []fold
		latest string
		times  bool
	}{
		{"no reading", map[string]any{"unit": "kg", "readings": []any{}}, nil, "", false},
		{"two years", map[string]any{"unit": "kg", "readings": []any{reading("2025-12-04", 72), reading("2026-10-10", 70),
			reading("not a day", 1), reading("2026-10-11", 71.5), reading("2026-10-12", 71)}},
			[]fold{{"2026", "2026-10-12", "3 readings · 70 to 71.5 kg", 3, 0, true}, {"2025", "2025-12-04", "1 reading · 72 kg", 1, 0, true}},
			"2026-10-12", false},
		{"a reading with a time", map[string]any{"unit": "kg", "readings": []any{reading("2026-10-11", 70), timed}},
			[]fold{{"2026", "2026-10-12", "2 readings · 70 to 71 kg", 2, 0, true}}, "2026-10-12", true},
		{"twelve readings in a year", map[string]any{"unit": "", "readings": days("2026", 12)},
			[]fold{{"2026", "2026-01-12", "12 readings · 0 to 11", 10, 2, true}}, "2026-01-12", false},
		{"sixty readings", map[string]any{"unit": "", "readings": append(days("2025", 30), days("2026", 30)...)},
			[]fold{{"2026", "2026-01-30", "30 readings · 0 to 29", 10, 20, true}, {"2025", "2025-01-30", "30 readings · 0 to 29", 10, 20, true}},
			"2026-01-30", false},
		{"sixty-one readings", map[string]any{"unit": "", "readings": append(days("2025", 31), days("2026", 30)...)},
			[]fold{{"2026", "2026-01-30", "30 readings · 0 to 29", 10, 20, true}, {"2025", "2025-01-31", "31 readings · 0 to 30", 10, 21, false}},
			"2026-01-30", false},
	} {
		r := (&view{Props: c.props}).MetricReadings()
		var got []fold
		for _, y := range r.Years {
			got = append(got, fold{y.Year, y.Rows[0]["day"].(string), y.Summary, len(y.Rows), len(y.More), y.Open})
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: folds %+v, want %+v", c.name, got, c.want)
		}
		latest, _ := r.Latest["day"].(string)
		if latest != c.latest || r.Times != c.times {
			t.Errorf("%s: latest %q, times %v; want %q, %v", c.name, latest, r.Times, c.latest, c.times)
		}
	}
	// The rows of a fold run newest first, and More holds the oldest of the year.
	r := (&view{Props: map[string]any{"readings": days("2026", 12)}}).MetricReadings()
	if y := r.Years[0]; y.Rows[9]["day"] != "2026-01-03" || y.More[0]["day"] != "2026-01-02" || y.More[1]["day"] != "2026-01-01" {
		t.Errorf("the fold of twelve readings: the last row %v, More %v", y.Rows[9], y.More)
	}
}

// TestTheTilesOfADay: a tile a metric of the day's readings, in the order of its first reading. A tile shows the last
// reading of the day of its metric (the one to correct) and counts the others (More).
func TestTheTilesOfADay(t *testing.T) {
	r := func(id float64, metric string, value float64) any {
		return map[string]any{"id": id, "metric": metric, "value": value}
	}
	v := &view{Props: map[string]any{"day": "2026-10-16",
		"readings": []any{r(5, "weight", 70), r(6, "mood", 3), r(7, "weight", 71), r(8, "weight", 1000000)}}}
	want := []readingTile{
		{Metric: "weight", Value: "1000000", ID: "8", More: 2},
		{Metric: "mood", Value: "3", ID: "6"},
	}
	if got := v.ReadingTiles(); !reflect.DeepEqual(got, want) {
		t.Errorf("the tiles:\n %+v\nwant\n %+v", got, want)
	}
	if got := (&view{Props: map[string]any{"day": "2026-10-16"}}).ReadingTiles(); got != nil {
		t.Errorf("a day with no reading has tiles: %+v", got)
	}
}

// TestTheDailyTilesComeFirst: the daily metrics are the first tiles, in the order given, with the reading of the day
// when there is one; the metrics read that day follow. A daily metric read twice is one tile with More 1.
func TestTheDailyTilesComeFirst(t *testing.T) {
	r := func(id float64, metric string, value float64) any {
		return map[string]any{"id": id, "metric": metric, "value": value}
	}
	v := &view{Props: map[string]any{"day": "2026-10-16", "daily": []any{"Meditate", "Sleep"},
		"units":    map[string]any{"Sleep": "h"},
		"readings": []any{r(5, "weight", 70), r(6, "Sleep", 7), r(7, "Sleep", 8)}}}
	want := []readingTile{
		{Metric: "Meditate"},
		{Metric: "Sleep", Unit: "h", Value: "8", ID: "7", More: 1},
		{Metric: "weight", Value: "70", ID: "5"},
	}
	if got := v.ReadingTiles(); !reflect.DeepEqual(got, want) {
		t.Errorf("the tiles:\n %+v\nwant\n %+v", got, want)
	}
}

// TestTheSectionsOfMetrics: a section a top-level group, in the order of the groups, with the categories under it as
// its subs. A section counts each of its metrics one time, in a sub too; its newest day is the newest of its cards and
// of its subs' cards. A card takes the metric's row of the property metrics (its note and its recent readings) by
// its id. A scale's unit is said "of 5". Today is 16 October 2026.
func TestTheSectionsOfMetrics(t *testing.T) {
	recent := func(id float64, day string, value float64) any {
		return map[string]any{"id": id, "day": day, "value": value}
	}
	metric := func(id float64, name string) any { return map[string]any{"id": id, "name": name} }
	group := func(title, path string, page, depth float64, ms ...any) any {
		g := map[string]any{"title": title, "depth": depth, "metrics": ms}
		if path != "" {
			g["path"], g["page"] = path, page
		}
		return g
	}
	props := map[string]any{
		"metrics": []any{
			map[string]any{"id": 1.0, "name": "weight", "unit": "kg", "note": "In the morning", "recent": []any{recent(11, "2026-10-01", 71), recent(12, "2026-10-14", 70.5)}},
			map[string]any{"id": 2.0, "name": "ferritin", "unit": "ug/L", "recent": []any{recent(21, "2025-04-12", 85)}},
			map[string]any{"id": 3.0, "name": "Blood sugar", "unit": "mmol/L", "recent": []any{}},
			map[string]any{"id": 4.0, "name": "mood", "unit": "1-5", "recent": []any{recent(41, "2026-10-15", 4)}},
		},
		"groups": []any{
			group("Health", "Health", 7, 0, metric(1, "weight")),
			group("Blood", "Health/Blood", 8, 1, metric(2, "ferritin"), metric(1, "weight"), metric(3, "Blood sugar")),
			group("Sport", "Sport", 9, 0),
			group("Not filed", "", 0, 0, metric(4, "mood"), metric(5, "steps")),
		},
	}
	v := &view{Shell: &Shell{Today: "2026-10-16", Zone: time.UTC}, Props: props}
	type card struct {
		ID, Name, Href, Value, Unit, Measurement, Day, When, Short, Note string
		Count                                                            int
		Stale, Line                                                      bool
	}
	cards := func(ks []metricCard) []card {
		var out []card
		for _, k := range ks {
			out = append(out, card{k.ID, k.Name, k.Href, k.Value, k.Unit, k.Measurement, k.Day, k.When, k.Short, k.Note, k.Count, k.Stale, k.Line != ""})
		}
		return out
	}
	weight := card{"1", "weight", "/metrics/weight", "70.5", "kg", "12", "2026-10-14", "2 days ago · Wed 14 Oct", "2 days ago", "In the morning", 2, false, true}
	ferritin := card{"2", "ferritin", "/metrics/ferritin", "85", "ug/L", "21", "2025-04-12", "Sat 12 Apr 2025", "Sat 12 Apr 2025", "", 1, true, true}
	sugar := card{ID: "3", Name: "Blood sugar", Href: "/metrics/Blood%20sugar", Unit: "mmol/L"}
	mood := card{"4", "mood", "/metrics/mood", "4", "of 5", "41", "2026-10-15", "yesterday · Thu 15 Oct", "yesterday", "", 1, false, true}
	steps := card{ID: "5", Name: "steps", Href: "/metrics/steps"} // no row in metrics: the card is the group's entry

	s := v.MetricSections()
	if len(s) != 3 {
		t.Fatalf("%d sections, want 3 (Health, Sport, Not filed): %+v", len(s), s)
	}
	for i, want := range []struct {
		title, page, anchor, latest string
		count                       int
		cards                       []card
		subs                        int
	}{
		{"Health", "7", "c-7", "2 days ago · Wed 14 Oct", 3, []card{weight}, 1},
		{"Sport", "9", "c-9", "", 0, nil, 0},
		{"Not filed", "", "not-filed", "yesterday · Thu 15 Oct", 2, []card{mood, steps}, 0},
	} {
		got := s[i]
		if got.Title != want.title || got.Page != want.page || got.Anchor != want.anchor || got.Latest != want.latest || got.Count != want.count || len(got.Subs) != want.subs {
			t.Errorf("section %d: %q page %q anchor %q newest %q count %d, %d subs; want %q %q %q %q %d, %d subs", i,
				got.Title, got.Page, got.Anchor, got.Latest, got.Count, len(got.Subs), want.title, want.page, want.anchor, want.latest, want.count, want.subs)
		}
		if c := cards(got.Cards); !reflect.DeepEqual(c, want.cards) {
			t.Errorf("section %s: cards\n %+v\nwant\n %+v", want.title, c, want.cards)
		}
	}
	sub := s[0].Subs[0]
	if sub.Title != "Blood" || sub.Page != "8" || sub.Latest != "2 days ago · Wed 14 Oct" || !reflect.DeepEqual(cards(sub.Cards), []card{ferritin, weight, sugar}) {
		t.Errorf("the sub of Health: %q page %q newest %q, cards %+v", sub.Title, sub.Page, sub.Latest, cards(sub.Cards))
	}
	want := `<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight: 2 readings, from 71 kg Thu 1 Oct to 70.5 kg 2 days ago · Wed 14 Oct"><polyline points="4.0,4.0 116.0,24.0 "/><circle cx="116.0" cy="24.0" r="2.5"/></svg>`
	if got := string(s[0].Cards[0].Line); got != want {
		t.Errorf("the line of weight:\n%s\nwant\n%s", got, want)
	}
	if got := (&view{Props: map[string]any{"groups": []any{}}}).MetricSections(); got != nil {
		t.Errorf("no group makes sections: %+v", got)
	}
}
