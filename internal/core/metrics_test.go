package core

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"aminte/text"
)

func TestMeasurementsAreAppendOnly(t *testing.T) {
	s := fresh(t)
	if _, err := s.Record(ctx, "cli", Reading{Metric: "weight", Day: "2026-09-29", Value: 70}); status(err) != 404 {
		t.Errorf("an unregistered metric: %v", err)
	}
	m := Reading{Metric: "mood", Day: "2026-09-29", Value: 3, ImportKey: "k1"}
	id, err := s.Record(ctx, "agent:x", m)
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	if again, err := s.Record(ctx, "agent:x", m); err != nil || again != 0 {
		t.Errorf("a re-send with the same key: %d, %v", again, err)
	}
	fix, err := s.Correct(ctx, "cli", id, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", id, ptr(5)); status(err) != 422 {
		t.Errorf("a second correction of one row: %v", err)
	}
	if _, err := s.Correct(ctx, "cli", fix, nil); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Day(ctx, "2026-09-29")
	if len(d.Readings) != 0 {
		t.Errorf("a retracted reading is still current: %+v", d.Readings)
	}
}

// A correction of an imported, keyed reading is the corrector's row: its writer's source and no import key.
func TestACorrectionKeepsItsWritersProvenance(t *testing.T) {
	s := fresh(t)
	root, err := s.Record(ctx, "import:notebook", Reading{Metric: "mood", Day: "2026-10-07", Value: 3, ImportKey: "Medical/Mood.md|reading|mood|2026-10-07|1"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Correct(ctx, "cli", root, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	newest, err := s.Correct(ctx, "cli", first, ptr(5))
	if err != nil {
		t.Fatal(err)
	}
	retracted, err := s.Correct(ctx, "agent:owner", newest, nil)
	if err != nil {
		t.Fatalf("retraction of newest row: %v", err)
	}
	var source string
	var rowKey sql.NullString
	var value sql.NullFloat64
	if err := s.DB.R.QueryRowContext(ctx, `SELECT source, import_key, value FROM measurements WHERE id = ?`, retracted).Scan(&source, &rowKey, &value); err != nil {
		t.Fatal(err)
	}
	if source != "agent:owner" || rowKey.Valid || value.Valid {
		t.Fatalf("retraction provenance = source %q key %q value %v, want the writer's source, no key and no value", source, rowKey.String, value)
	}
	if _, err := s.Correct(ctx, "cli", 987654321, ptr(4)); status(err) != 404 {
		t.Fatalf("unknown measurement: %v, want 404", err)
	}
}

func TestCorrectionDomains(t *testing.T) {
	s := fresh(t)
	captured, _, err := s.Capture(ctx, "cli", "2026-10-01", "domain test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "focus"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "evening_walk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "winter_vitamin"}); err != nil {
		t.Fatal(err)
	}

	assertRejectedCorrection := func(name string, r Reading, value float64) {
		t.Helper()
		id, err := s.Record(ctx, "cli", r)
		if err != nil {
			t.Fatalf("%s record: %v", name, err)
		}
		before := measurementRows(t, s)
		if _, err := s.Correct(ctx, "cli", id, ptr(value)); status(err) != 422 {
			t.Fatalf("%s correct to %g: %v, want 422", name, value, err)
		}
		if got := measurementRows(t, s); got != before {
			t.Fatalf("%s wrote %d rows, want %d", name, got, before)
		}
		m, err := s.Measurement(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !m.Current || m.Value != r.Value {
			t.Fatalf("%s current reading after rejection: current=%v value=%g, want %g", name, m.Current, m.Value, r.Value)
		}
	}

	assertRejectedCorrection("mood above range", Reading{Metric: "mood", Day: "2026-10-01", Value: 3}, 6)
	assertRejectedCorrection("fractional mood", Reading{Metric: "mood", Day: "2026-10-02", Value: 3}, 2.5)

	unknownRows := measurementRows(t, s)
	if _, err := s.Correct(ctx, "cli", 987654321, ptr(1)); status(err) != 404 {
		t.Fatalf("unknown measurement: %v, want 404", err)
	}
	if got := measurementRows(t, s); got != unknownRows {
		t.Fatalf("unknown correction wrote %d rows, want %d", got, unknownRows)
	}

	lo, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-03", TakenAt: "2026-10-03T07:00:00.000Z", TZ: "Europe/Bucharest", Value: 3, CapturedWith: captured})
	if err != nil {
		t.Fatal(err)
	}
	loFix, err := s.Correct(ctx, "cli", lo, ptr(1))
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Measurement(ctx, loFix)
	if err != nil {
		t.Fatal(err)
	}
	if m.Metric != "Mood" || m.Day != "2026-10-03" || m.TakenAt != "2026-10-03T07:00:00.000Z" || m.TZ != "Europe/Bucharest" || m.CapturedWith != captured || m.Supersedes != lo || !m.Current || m.Value != 1 {
		t.Errorf("corrected row did not copy identity/provenance: %+v", m)
	}

	hi, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-04", Value: 4})
	if err != nil {
		t.Fatal(err)
	}
	hiFix, err := s.Correct(ctx, "cli", hi, ptr(5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", hiFix, nil); err != nil {
		t.Fatalf("nil retraction: %v", err)
	}
	d, err := s.Day(ctx, "2026-10-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Readings) != 0 {
		t.Fatalf("retracted boundary correction remains current: %+v", d.Readings)
	}

	start, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-05", Value: 3})
	if err != nil {
		t.Fatal(err)
	}
	middle, err := s.Correct(ctx, "cli", start, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	end, err := s.Correct(ctx, "cli", middle, ptr(5))
	if err != nil {
		t.Fatalf("correction of a correction: %v", err)
	}
	if m, err := s.Measurement(ctx, end); err != nil || !m.Current || m.Supersedes != middle || m.Value != 5 {
		t.Fatalf("second correction: %+v, %v", m, err)
	}

	focus, err := s.Record(ctx, "cli", Reading{Metric: "focus", Day: "2026-10-06", Value: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", focus, ptr(2.5)); err != nil {
		t.Fatalf("finite non-habit unitless correction: %v", err)
	}
}

func measurementRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM measurements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A scale names its range as its unit, and the writer holds a '1-5' scale to whole numbers from 1 to 5 whichever
// metric it is (D6, issue 0015).
func TestScalesAreRangeChecked(t *testing.T) {
	s := fresh(t)
	for _, m := range []struct{ name, unit string }{{"Energy", "1-5"}, {"Pain", "0-10"}} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: m.name, Unit: m.unit}); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []float64{0, 6, 2.5} {
		if _, err := s.Record(ctx, "cli", Reading{Metric: "Energy", Day: "2026-09-01", Value: v}); status(err) != 422 {
			t.Errorf("Energy 1-5 reading %g: %v, want 422", v, err)
		}
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Energy", Day: "2026-09-01", Value: 4}); err != nil {
		t.Errorf("Energy 1-5 reading 4: %v", err)
	}
}

// A scale names its range as its unit (metrics.unit), so every metric whose unit is a range is held to whole
// numbers inside it, on a first reading and on a correction alike, not only the seeded Mood (D6, D7).
func TestAnyRangeUnitHoldsItsValuesToTheRange(t *testing.T) {
	s := fresh(t)
	for _, m := range []struct{ name, unit string }{{"Pain", "0-10"}, {"Grade", "1-100"}} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: m.name, Unit: m.unit}); err != nil {
			t.Fatal(err)
		}
	}
	record := func(metric string, v float64) (int64, error) {
		return s.Record(ctx, "cli", Reading{Metric: metric, Day: "2026-09-01", Value: v})
	}
	for _, c := range []struct {
		metric string
		value  float64
		ok     bool
	}{
		{"Pain", 0, true}, {"Pain", 10, true}, {"Pain", 11, false}, {"Pain", -1, false}, {"Pain", 5.5, false},
		{"Grade", 1, true}, {"Grade", 100, true}, {"Grade", 0, false}, {"Grade", 101, false},
		{"Mood", 5, true}, {"Mood", 6, false},
	} {
		_, err := record(c.metric, c.value)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s %g: %v, want it recorded", c.metric, c.value, err)
		case !c.ok && status(err) != 422:
			t.Errorf("%s %g: %v, want a 422", c.metric, c.value, err)
		}
	}
	// a correction is held to the range too, and a retraction (no value) is not a value
	id, err := record("Pain", 3)
	if err != nil {
		t.Fatal(err)
	}
	eleven := 11.0
	if _, err := s.Correct(ctx, "cli", id, &eleven); status(err) != 422 {
		t.Errorf("a correction of Pain to 11: %v, want a 422", err)
	}
	four := 4.0
	if _, err := s.Correct(ctx, "cli", id, &four); err != nil {
		t.Errorf("a correction of Pain to 4: %v", err)
	}
	// a metric without a range unit takes any finite number
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Weight", Unit: "kg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := record("Weight", 70.5); err != nil {
		t.Errorf("Weight 70.5 kg: %v", err)
	}
}

// Registering a metric whose title is already a scale, with the scale's own range or with no unit, takes the
// existing metric: a source that names Mood does not write its range, so a unitless request cannot be a conflict.
// A different unit is one, since a unit never changes (metrics_unit_fixed).
func TestRegisteringAnExistingScaleAdoptsIt(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Pain", Unit: "0-10", Note: "how much it hurts"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, unit string
		want       int // 0: adopted
	}{
		{"Mood", "", 0}, {"mood", "1-5", 0}, {"MOOD", "", 0}, {"Mood", "kg", 409}, {"Mood", "0-10", 409}, {"Mood", "1-10", 409},
		{"pain", "", 0}, {"Pain", "0-10", 0}, {"Pain", "1-5", 409}, {"Pain", "mg", 409},
	} {
		added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: c.name, Unit: c.unit, Note: "ignored note"})
		if status(err) != c.want || added {
			t.Errorf("register %q with unit %q: added %v, %v; want added false and status %d", c.name, c.unit, added, err, c.want)
		}
	}
	// the adopted metric is untouched: same unit, same page text
	var unit, body string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT m.unit, e.body FROM metrics m JOIN entities e ON e.id = m.id WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = 'pain')`).Scan(&unit, &body); err != nil {
		t.Fatal(err)
	}
	if unit != "0-10" || body != "how much it hurts" {
		t.Errorf("Pain after the adoptions: unit %q, body %q", unit, body)
	}
	// only a range is adopted: a physical unit, or none, stays what it was
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Weight", Unit: "kg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Weight"}); status(err) != 409 {
		t.Errorf("Weight (kg) registered with no unit: %v, want a 409", err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Steps"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Steps", Unit: "0-10"}); status(err) != 409 {
		t.Errorf("a unitless metric registered as a scale: %v, want a 409", err)
	}
}

// TestIsScale: the shape of a scale's range. A unit with its bounds out of order (5-1, 3-3) is never stored: the schema
// refuses it (metrics_range_order), as TestCreateMetricRefusalsWriteNothing holds.
func TestIsScale(t *testing.T) {
	for _, c := range []struct {
		unit string
		ok   bool
	}{
		{"1-5", true}, {"0-10", true}, {"1-100", true}, {"01-05", true}, {"0-1", true},
		{"0-100000000000000000000", true}, {"100000000000000000000-100000000000000000001", true},
		{"", false}, {"1-", false},
		{"-5", false}, {"-", false}, {"1-5-7", false}, {"1--5", false}, {"1 -5", false},
		{"1-5 ", false}, {"1–5", false}, {"+1-5", false}, {"1.5-5", false}, {"a-b", false},
		{"١-٥", false}, {"kg", false}, {"mg/dL", false}, {"1/5", false}, {"2024-2025", true},
	} {
		if ok := isScale(c.unit); ok != c.ok {
			t.Errorf("isScale(%q) = %v, want %v", c.unit, ok, c.ok)
		}
	}
}

func TestMeasurementZoneValidationBeforeWrites(t *testing.T) {
	for _, zone := range []string{"UTC\x00", "UTC\x00hidden", "\x00UTC", "Europe Berlin", strings.Repeat("x", 65)} {
		t.Run(zone, func(t *testing.T) {
			s := fresh(t)
			input := Reading{Metric: "Mood", Day: "2020-01-01", Value: 3, TZ: zone}
			id, err := s.Record(ctx, "cli", input)
			if status(err) != 422 || id != 0 {
				t.Errorf("Record invalid zone: id=%d err=%v", id, err)
			}
			var count int
			if err := s.DB.R.QueryRow("SELECT count(*) FROM measurements").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("invalid Record persisted %d measurements", count)
			}
		})
	}
}

func TestMeasurementZoneValidControls(t *testing.T) {
	s := fresh(t)
	for _, zone := range []string{"", "UTC", "Europe/Bucharest", "Etc/GMT+5", "America/Port-au-Prince", strings.Repeat("x", 64)} {
		id, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2020-01-01", Value: 3, TZ: zone})
		if err != nil {
			t.Fatalf("Record(%q): %v", zone, err)
		}
		row, err := s.Measurement(ctx, id)
		if err != nil || row.TZ != zone {
			t.Errorf("stored zone got %+v, %v; want %q", row, err, zone)
		}
	}
}

func metricID(t *testing.T, s *Store, name, unit string) int64 {
	t.Helper()
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: name, Unit: unit}); err != nil {
		t.Fatal(err)
	}
	return pageByTitle(t, s, name).ID
}

func TestRecentReadings(t *testing.T) {
	s := fresh(t)
	weight := metricID(t, s, "Weight", "kg")
	gone := metricID(t, s, "Gone", "kg")
	record := func(metric, day string, v float64) int64 {
		t.Helper()
		id, err := s.Record(ctx, "cli", Reading{Metric: metric, Day: day, Value: v})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	record("Weight", "2026-10-01", 80)
	wrong := record("Weight", "2026-10-02", 90)
	mistake := record("Weight", "2026-10-03", 70)
	record("Weight", "2026-10-04", 79)
	last := record("Weight", "2026-10-04", 78.5) // a later reading of the same day comes last
	if _, err := s.Correct(ctx, "cli", wrong, ptr(81.0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", mistake, nil); err != nil {
		t.Fatal(err)
	}
	record("Gone", "2026-10-05", 1)
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "gone", Day: "2026-10-06", Value: 2}); status(err) != 404 {
		t.Errorf("a reading of a tombstoned metric: %v, want a 404", err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Lake", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Lake", Day: "2026-10-06", Value: 2}); status(err) != 404 {
		t.Errorf("a reading of a page that is not a metric: %v, want a 404", err)
	}

	got, err := s.RecentReadings(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		day   string
		value float64
	}{{"2026-10-01", 80}, {"2026-10-02", 81}, {"2026-10-04", 79}, {"2026-10-04", 78.5}}
	w := got[weight]
	if len(w) != len(want) {
		t.Fatalf("Weight recent = %+v, want %v", w, want)
	}
	for i, r := range w {
		if r.Day != want[i].day || r.Value != want[i].value {
			t.Errorf("Weight recent[%d] = %s %v, want %s %v", i, r.Day, r.Value, want[i].day, want[i].value)
		}
	}
	if w[len(w)-1].ID != last {
		t.Errorf("the latest is measurement %d, want %d", w[len(w)-1].ID, last)
	}
	if _, ok := got[gone]; ok {
		t.Errorf("a deleted metric has recent readings: %+v", got[gone])
	}

	two, err := s.RecentReadings(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if w := two[weight]; len(w) != 2 || w[0].Value != 79 || w[1].Value != 78.5 {
		t.Errorf("the newest 2 = %+v, want 79 then 78.5", w)
	}
}

func TestCreateMetricRefusalsWriteNothing(t *testing.T) {
	s := fresh(t)
	gone, _, err := s.CreatePage(ctx, "cli", "Old Group", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Capture(ctx, "cli", "2031-05-02", "a day", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePerson(ctx, "cli", "Ana Example", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Grade", "what the teacher gives", ""); err != nil {
		t.Fatal(err)
	}
	const promoteOnly = "only a plain page is promoted: a person, a metric or a file keeps its type (D20)"
	const outOfOrder = "a scale names its range as its unit, lo-hi with lo less than hi, as in 1-5 (D6)"
	for _, c := range []struct {
		name string
		m    NewMetric
		want int
		says string // a part of the message; "" for any
	}{
		// a day page is never a metric: the schema refuses the promotion
		{"a day page's title", NewMetric{Name: "2031-05-02", Unit: "km"}, 422, "entities_day_page_plain"},
		// a person or a file is never a metric: the schema refuses the promotion (entities_promote_from_page)
		{"a person's title", NewMetric{Name: "Ana Example", Unit: "km"}, 422, promoteOnly},
		{"a file's title", NewMetric{Name: "Scan.pdf", Unit: "km"}, 422, promoteOnly},
		// a unit of the shape lo-hi with lo not less than hi is not a range: the schema refuses it (metrics_range_order),
		// for a new metric and for a plain page that becomes the metric
		{"a range with its bounds out of order", NewMetric{Name: "Pain", Unit: "5-1"}, 422, outOfOrder},
		{"a range with equal bounds", NewMetric{Name: "Pain", Unit: "3-3"}, 422, outOfOrder},
		{"a plain page with a range out of order", NewMetric{Name: "Grade", Unit: "10-1"}, 422, outOfOrder},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := everyRow(t, s)
			_, err := s.CreateMetric(ctx, "cli", c.m)
			if status(err) != c.want || !strings.Contains(fmt.Sprint(err), c.says) {
				t.Fatalf("status %d (%v), want %d with %q", status(err), err, c.want, c.says)
			}
			if after := everyRow(t, s); after != before {
				t.Errorf("a refused CreateMetric wrote:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// TestReadingsByDayCountsTheCurrentReadingsOfAWindow: the readings of all metrics together (the mood of a capture, a
// weight, steps). A correction replaces its reading, so its day counts it once, and a retracted reading does not
// count. A tombstoned metric has no reading in the count, as it has none in Moods. The bounds are both included, and a
// day with no reading is absent. A sum of the counts is the count of the window that the year page shows.
func TestReadingsByDayCountsTheCurrentReadingsOfAWindow(t *testing.T) {
	s := fresh(t)
	mood := 4.0
	if _, _, err := s.Capture(ctx, "cli", "2031-05-14", "", &mood); err != nil {
		t.Fatal(err)
	}
	for name, unit := range map[string]string{"weight": "kg", "steps": "n"} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: name, Unit: unit}); err != nil {
			t.Fatal(err)
		}
	}
	record := func(metric, day string, v float64) int64 {
		t.Helper()
		id, err := s.Record(ctx, "cli", Reading{Metric: metric, Day: day, Value: v})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	record("weight", "2031-05-01", 70)
	wrong := record("weight", "2031-05-14", 80)
	if _, err := s.Correct(ctx, "cli", wrong, func() *float64 { v := 79.5; return &v }()); err != nil {
		t.Fatal(err)
	}
	record("weight", "2031-05-14", 79)
	record("weight", "2031-05-15", 78)
	if _, err := s.Correct(ctx, "cli", record("weight", "2031-05-15", 70), nil); err != nil { // a retraction
		t.Fatal(err)
	}
	record("steps", "2031-05-16", 900)
	record("weight", "2031-05-31", 72)
	record("weight", "2031-06-01", 77)
	// A metric that is tombstoned: its readings stay in the file but not in the count (14 May would count 4, and
	// 20 May would appear).
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "scrap", Unit: "n"}); err != nil {
		t.Fatal(err)
	}
	record("scrap", "2031-05-14", 1)
	record("scrap", "2031-05-20", 2)
	var scrap int64
	if err := s.DB.R.QueryRowContext(ctx, `SELECT entity_id FROM entity_names WHERE name_key = 'scrap'`).Scan(&scrap); err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", scrap); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		from, through string
		want          string
	}{
		// 14 May: the mood, the correction (not the reading it corrects) and 79. 15 May: 78, not the retracted
		// reading. 1 June is after the window.
		{"2031-05-01", "2031-05-31", "map[2031-05-01:1 2031-05-14:3 2031-05-15:1 2031-05-16:1 2031-05-31:1]"},
		{"2031-05-15", "2031-05-16", "map[2031-05-15:1 2031-05-16:1]"}, {"2031-05-01", "2031-05-01", "map[2031-05-01:1]"},
		{"2031-05-31", "2031-06-01", "map[2031-05-31:1 2031-06-01:1]"},
		{"2031-05-02", "2031-05-13", "map[]"}, {"2031-04-01", "2031-04-30", "map[]"},
	} {
		got, err := s.ReadingsByDay(ctx, tc.from, tc.through)
		if err != nil || fmt.Sprint(got) != tc.want {
			t.Errorf("ReadingsByDay(%s, %s) = %v, %v; want %s", tc.from, tc.through, got, err, tc.want)
		}
	}
	if _, err := s.ReadingsByDay(ctx, "2031-05-16", "2031-05-01"); status(err) != 422 {
		t.Errorf("a reversed window: %v", err)
	}
}

// TestSeriesWithNoFromHasNoLowerBound: a series is (from, to], and an empty from reads every reading through to, the
// first day that a date can name too. A from or a to that is not a day is refused.
func TestSeriesWithNoFromHasNoLowerBound(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "weight", Unit: "kg"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Reading{{Metric: "weight", Day: "0000-01-01", Value: 70}, {Metric: "weight", Day: "2026-10-05", Value: 71},
		{Metric: "weight", Day: "2026-10-09", Value: 72}} {
		if _, err := s.Record(ctx, "cli", r); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ from, to, want string }{
		{"", "9999-12-31", "0000-01-01=70 2026-10-05=71 2026-10-09=72"},
		{"", "2026-10-05", "0000-01-01=70 2026-10-05=71"},
		{"0000-01-01", "9999-12-31", "2026-10-05=71 2026-10-09=72"},
		{"2026-10-05", "2026-10-09", "2026-10-09=72"},
	} {
		rows, err := s.Series(ctx, "Weight", tc.from, tc.to)
		var got []string
		for _, r := range rows {
			got = append(got, fmt.Sprintf("%s=%g", r.Day, r.Value))
		}
		if err != nil || strings.Join(got, " ") != tc.want {
			t.Errorf("Series(%q, %q) = %v, %v; want %s", tc.from, tc.to, got, err, tc.want)
		}
	}
	for _, bad := range [][2]string{{"2026-1-5", "2026-10-09"}, {"", ""}, {"", "2026-10-32"}} {
		if _, err := s.Series(ctx, "weight", bad[0], bad[1]); status(err) != 422 {
			t.Errorf("Series(%q, %q): %v, want a 422", bad[0], bad[1], err)
		}
	}
}

func TestNewDatabaseReadingLookupUsesIndexes(t *testing.T) {
	s := fresh(t)
	if err := s.Do(t.Context(), "cli", func(tx *Tx) error {
		for i := range 200 {
			if _, _, err := tx.CreatePage(fmt.Sprintf("Synthetic lookup page %03d", i), "", "2031-02-03"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Keep the writer open: closing it optimizes the grown database and would
	// conceal initialization statistics that still describe only the seeded Mood.
	// This is prepareReading's lookup, planned on that same writer connection.
	rows, err := s.DB.W.QueryContext(t.Context(), `EXPLAIN QUERY PLAN `+metricLookup, "mood")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	searched := map[string]bool{"m": false, "entity_names": false}
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
		fields := strings.Fields(detail)
		if len(fields) >= 2 && fields[0] == "SEARCH" {
			if _, relevant := searched[fields[1]]; relevant {
				searched[fields[1]] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"m", "entity_names"} {
		if !searched[alias] {
			t.Errorf("reading lookup does not search %s by an index: %v", alias, plan)
		}
	}
}

// TestTheDailyMarkIsWrittenWithTheMetricAndEdited: a new metric and a promoted plain page get the mark that the call
// names. An existing metric keeps its mark when it is registered again. EditMetric flips the mark and touches
// neither the unit nor the readings. A plain page is a 409 and a missing page a 404.
func TestTheDailyMarkIsWrittenWithTheMetricAndEdited(t *testing.T) {
	s := fresh(t)
	daily := func(name string) (unit string, daily bool) {
		t.Helper()
		if err := s.DB.R.QueryRowContext(ctx, `SELECT m.unit, m.daily FROM metrics m JOIN entity_names n ON n.entity_id = m.id WHERE n.name_key = ?`, text.TitleKey(name)).Scan(&unit, &daily); err != nil {
			t.Fatal(err)
		}
		return unit, daily
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Meditate", Daily: new(true)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Stretch", "my own words", ""); err != nil {
		t.Fatal(err)
	}
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Stretch", Daily: new(true)}); err != nil || !added {
		t.Fatalf("a plain page becomes a metric: %v %v", added, err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Sleep", Unit: "h"}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"Meditate": true, "Stretch": true, "Sleep": false} {
		if _, got := daily(name); got != want {
			t.Errorf("%s: daily %v, want %v", name, got, want)
		}
	}
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Meditate"}); err != nil || added {
		t.Fatalf("registering Meditate again: %v %v", added, err)
	}
	if _, got := daily("Meditate"); !got {
		t.Error("registering a metric again cleared its mark")
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Meditate", Daily: new(false)}); err == nil || !strings.Contains(err.Error(), "edit-metric") {
		t.Errorf("registering Meditate again with another mark: %v, want a refusal that names edit-metric", err)
	}
	if _, got := daily("Meditate"); !got {
		t.Error("the refused registration changed the mark")
	}

	if _, err := s.Record(ctx, "cli", Reading{Metric: "Sleep", Day: "2026-10-01", Value: 7}); err != nil {
		t.Fatal(err)
	}
	id, err := s.PageID(ctx, "Sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EditMetric(ctx, "cli", id, true); err != nil {
		t.Fatal(err)
	}
	if unit, got := daily("Sleep"); !got || unit != "h" {
		t.Errorf("after EditMetric true: unit %q daily %v, want h and true", unit, got)
	}
	if err := s.EditMetric(ctx, "cli", id, false); err != nil {
		t.Fatal(err)
	}
	if _, got := daily("Sleep"); got {
		t.Error("after EditMetric false: daily is still set")
	}
	if rs, err := s.Series(ctx, "Sleep", "2026-01-01", "2026-12-31"); err != nil || len(rs) != 1 {
		t.Errorf("the readings of Sleep: %v %v, want 1", rs, err)
	}
	plain, _, err := s.CreatePage(ctx, "cli", "Plain", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EditMetric(ctx, "cli", plain, true); status(err) != 409 {
		t.Errorf("EditMetric on a plain page: %v, want 409", err)
	}
	if err := s.EditMetric(ctx, "cli", 9999, true); status(err) != 404 {
		t.Errorf("EditMetric on a missing page: %v, want 404", err)
	}
}
