package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"aminte/text"
)

// RegisterMetric makes a metric (D27): a page titled m.Name, whose body is m.Note, and its metrics row with the
// unit and the daily mark (D31), one id. A plain page of that title becomes the metric, as a person is promoted (D20): a ghost, or a note
// the owner wrote about it, whose text is kept — the note is written only to an empty body. The schema refuses a
// person or a file of that title (entities_promote_from_page). Registering one that exists with the same unit is a no-op, and so is one with no unit where the existing metric is a scale (isScale): a
// source that names Mood does not write its range, so the request takes the metric as it is. Another unit is
// refused: a unit never changes (metrics_unit_fixed). A metric that exists keeps its mark, and a request that sends a
// different mark is refused: EditMetric changes it (nothing is dropped in silence).
func (t *Tx) RegisterMetric(m NewMetric) (added bool, err error) {
	name, unit, note := m.Name, m.Unit, m.Note
	daily := m.Daily != nil && *m.Daily
	if err := checkTitle("metric name", name); err != nil {
		return false, err
	}
	p, err := t.lookupWithBody(name)
	if err != nil {
		return false, err
	}
	switch {
	case p == nil:
		// the note is written by the insert: creation is one write, so the metric starts at revision 1
		id, err := t.insertRow("metric", name, text.TitleKey(name), nil, note)
		if err != nil {
			return false, err
		}
		if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO metrics(id, unit, daily) VALUES (?, ?, ?)`, id, unit, daily); err != nil {
			return false, err
		}
		_, err = t.syncWikilinks(id, note)
		return err == nil, err
	case p.Type == "metric":
		var have string
		var haveDaily bool
		if err := t.tx.QueryRowContext(t.ctx, `SELECT unit, daily FROM metrics WHERE id = ?`, p.ID).Scan(&have, &haveDaily); err != nil {
			return false, err
		}
		if have != unit && !(isScale(have) && unit == "") {
			return false, Conflict("metric %s exists with unit %q, not %q: a unit never changes; add a new metric (New metric, register-metric)", p.Title, have, unit)
		}
		if m.Daily != nil && *m.Daily != haveDaily {
			return false, Conflict("metric %s exists with daily %d: change the mark on its page (Ask for it every day, edit-metric)", p.Title, b2i(haveDaily))
		}
		return false, nil
	}
	// a day page is never a metric (entities_day_page_plain), and a person or a file never becomes one
	// (entities_promote_from_page): the schema refuses the update
	if err := t.retype(p, "metric"); err != nil {
		return false, err
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO metrics(id, unit, daily) VALUES (?, ?, ?)`, p.ID, unit, daily); err != nil {
		return false, err
	}
	if note != "" && p.Body == "" { // the owner's text is never overwritten
		if _, err := t.SetBody(p.ID, note); err != nil {
			return false, err
		}
	}
	return true, nil
}

// isScale says whether the unit of a stored metric names a scale's range: two numbers of ASCII digits around one
// hyphen (`1-5`, `0-10`). The schema keeps the lower number below the upper one (metrics_range_order), so the shape is
// enough, and it holds the values of a scale to its range (measurements_scale_range, D6). A count or a 0/1 habit has
// no unit (D7, D24).
func isScale(unit string) bool {
	a, b, found := strings.Cut(unit, "-")
	return found && asciiDigits(a) && asciiDigits(b)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func asciiDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// metricLookup is the id of the live metric that a name_key names. TestNewDatabaseReadingLookupUsesIndexes plans it.
const metricLookup = `SELECT m.id FROM entities m
	WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND m.entity_type = 'metric' AND m.deleted_at IS NULL`

// metricID finds a live metric by its name, in any case (its title_key).
func (t *Tx) metricID(name string) (int64, error) {
	var id int64
	err := t.tx.QueryRowContext(t.ctx, metricLookup, text.TitleKey(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, &Error{Status: http.StatusNotFound, Metric: name,
			Msg: fmt.Sprintf("no metric %q: add it first (New metric, register-metric)", name)}
	}
	return id, err
}

// NewMetric is what CreateMetric makes in one write: the metric, its unit, its note and its daily mark (D31). Daily is
// nil when the request did not send it: a new metric is then not daily, and a metric that exists keeps its mark.
type NewMetric struct {
	Name, Unit, Note string
	Daily            *bool
}

// EditMetric sets the daily mark of a live metric (D31). The unit, the page and the readings stay. A missing page is
// a 404, a tombstoned page or a page that is not a metric a 409, as EditPerson says for a person.
func (s *Store) EditMetric(ctx context.Context, source string, id int64, daily bool) error {
	return s.Do(ctx, source, func(t *Tx) error {
		p, err := t.livePage(id)
		if err != nil {
			return err
		}
		if p.Type != "metric" {
			return Conflict("page %d is a %s, not a metric", id, p.Type)
		}
		_, err = t.tx.ExecContext(t.ctx, `UPDATE metrics SET daily = ? WHERE id = ?`, daily, id)
		return err
	})
}

// CreateMetric registers a metric in one write. It reports whether the metric is new, as RegisterMetric does.
// file-pages files it in a category (docs/plans/131-the-pareto-pass.md, part 2).
func (s *Store) CreateMetric(ctx context.Context, source string, m NewMetric) (bool, error) {
	return alone(s, ctx, source, func(t *Tx) (bool, error) { return t.RegisterMetric(m) })
}

// Reading is one measurement to record (D7).
type Reading struct {
	ID           int64   `json:"id,omitempty"`
	Metric       string  `json:"metric"`
	Day          string  `json:"day"`
	TakenAt      string  `json:"taken_at,omitempty"`
	TZ           string  `json:"tz,omitempty"`
	ImportKey    string  `json:"import_key,omitempty"`
	Value        float64 `json:"value"`
	CapturedWith int64   `json:"captured_with_id,omitempty"`
}

// Record appends a measurement. An ImportKey makes a re-send a no-op (ON CONFLICT ... DO NOTHING, docs/contract/imports.md);
// the id is 0 when the row was recorded before.
func (t *Tx) Record(m Reading) (id int64, err error) {
	metric, err := t.prepareReading(m)
	if err != nil {
		return 0, err
	}
	var with any
	if m.CapturedWith != 0 {
		with = m.CapturedWith
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, import_key, captured_with_id, created_at)
	                     VALUES (?, ?, ?, ?, ?, ?, ?, ?, `+nowSQL+`)
	                     ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING
	                     RETURNING id`,
		metric, m.Day, nullIfEmpty(m.TakenAt), nullIfEmpty(m.TZ), m.Value, t.source, nullIfEmpty(m.ImportKey), with).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // sent before
	}
	return id, err
}

// prepareReading is the live metric the reading names; the day, instant, zone and value are the DDL's to refuse.
func (t *Tx) prepareReading(m Reading) (metric int64, err error) {
	if err := checkValue(&m.Value); err != nil {
		return 0, err
	}
	return t.metricID(m.Metric)
}

// Correct supersedes a reading with a new value, or retracts it when value is nil (cookbook/correct-a-measurement.md).
func (t *Tx) Correct(wrong int64, value *float64) (id int64, err error) {
	if err := checkValue(value); err != nil {
		return 0, err
	}
	var v any
	if value != nil {
		v = *value
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, captured_with_id, supersedes_id, created_at)
	                     SELECT metric_id, day, taken_at, tz, ?, ?, captured_with_id, id, `+nowSQL+` FROM measurements WHERE id = ?
	                     RETURNING id`, v, t.source, wrong).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, NotFound("no measurement %d", wrong)
	}
	return id, err
}

// checkValue refuses a NaN: SQLite binds it as NULL, which a correction cannot tell from a retraction (D7). Every
// other rule of a value is the DDL's: finite (measurements_value_finite), inside a scale's range
// (measurements_scale_range).
func checkValue(value *float64) error {
	if value != nil && math.IsNaN(*value) {
		return Invalid("value must be a number")
	}
	return nil
}

func (s *Store) Record(ctx context.Context, source string, m Reading) (id int64, err error) {
	return alone(s, ctx, source, func(t *Tx) (int64, error) { return t.Record(m) })
}

// RecordedDay is the day of the reading that the source recorded with the import key for the metric that the name
// names: the reading that stands when Record answers that it was sent before. A name never leaves its owner and a
// reading never changes, so the read after the write finds the same row.
func (s *Store) RecordedDay(ctx context.Context, source, importKey, metric string) (string, error) {
	var day string
	err := s.DB.R.QueryRowContext(ctx, `SELECT day FROM measurements
	        WHERE source = ? AND import_key = ? AND metric_id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`,
		source, importKey, text.TitleKey(metric)).Scan(&day)
	if err != nil {
		return "", fmt.Errorf("the reading %s sent before: %w", importKey, err)
	}
	return day, nil
}

func (s *Store) Correct(ctx context.Context, source string, wrong int64, value *float64) (id int64, err error) {
	return alone(s, ctx, source, func(t *Tx) (int64, error) { return t.Correct(wrong, value) })
}

// readingColumns are a reading's columns as Reading scans them, from measurements me joined to its metric m with
// its name m_name.
const readingColumns = `me.id, m_name.title, me.day, coalesce(me.taken_at, ''), coalesce(me.tz, ''), me.value, coalesce(me.captured_with_id, 0)`

func (s *Store) readings(ctx context.Context, where string, args ...any) ([]Reading, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT `+readingColumns+`
		  FROM measurement_values me JOIN entities m ON m.id = me.metric_id  JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key `+where, args...)
	return list(rows, err, func(r *Reading) []any {
		return []any{&r.ID, &r.Metric, &r.Day, &r.TakenAt, &r.TZ, &r.Value, &r.CapturedWith}
	})
}

// Metric is a live metric (D27): its page's id, title (its name) and body (its note), its unit; Categories are the
// paths of the categories it is filed in (D26).
type Metric struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Unit       string   `json:"unit"`
	Note       string   `json:"note,omitempty"`
	Daily      bool     `json:"daily"`
	Categories []string `json:"categories,omitempty"`
}

func (s *Store) Metrics(ctx context.Context) ([]Metric, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT m.id, p_name.title, m.unit, p.body, m.daily
		  FROM metrics m JOIN entities p ON p.id = m.id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
		 ORDER BY p.preferred_name_key`)
	out, err := list(rows, err, func(m *Metric) []any { return []any{&m.ID, &m.Name, &m.Unit, &m.Note, &m.Daily} })
	if err != nil {
		return nil, err
	}
	cats, filed, err := s.categories(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		for _, c := range filed[out[i].ID] {
			out[i].Categories = append(out[i].Categories, cats[c].Path)
		}
		sort.Strings(out[i].Categories)
	}
	return out, nil
}

// Recent is one of a metric's recent readings: its measurement, its day and its value.
type Recent struct {
	ID    int64   `json:"id"`
	Day   string  `json:"day"`
	Value float64 `json:"value"`
}

// RecentReadings are the newest n current unassociated readings of each live metric, oldest first, in the series'
// order, so the last is the reading a metric's page calls its latest.
// Each metric's readings are read backwards on measurements_series and stop at n, so the work is n a metric, not
// the whole history.
func (s *Store) RecentReadings(ctx context.Context, n int) (map[int64][]Recent, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT v.metric_id, v.id, v.day, v.value
		  FROM metrics m CROSS JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL -- the metrics drive, not every page
		  JOIN measurement_values v ON v.id IN (
		        SELECT x.id FROM measurement_values x
		         WHERE x.metric_id = m.id
		         ORDER BY x.day DESC, x.taken_at DESC, x.id DESC LIMIT ?)
		 ORDER BY v.metric_id, v.day, v.taken_at, v.id`, n)
	out := map[int64][]Recent{}
	var metric int64
	var r Recent
	if err := each(rows, err, []any{&metric, &r.ID, &r.Day, &r.Value}, func() { out[metric] = append(out[metric], r) }); err != nil {
		return nil, err
	}
	return out, nil
}

// Series is cookbook/metric-series.md: the current readings of one metric in (from, to]. An empty from is no lower
// bound: every reading through to.
func (s *Store) Series(ctx context.Context, metric, from, to string) ([]Reading, error) {
	if from != "" {
		if err := checkDay("from", from); err != nil {
			return nil, err
		}
	}
	if err := checkDay("to", to); err != nil {
		return nil, err
	}
	predicate := `WHERE m.id=(SELECT entity_id FROM entity_names WHERE name_key=?) AND me.day<=?`
	args := []any{text.TitleKey(metric), to}
	if from != "" {
		predicate += ` AND me.day>?`
		args = append(args, from)
	}
	return s.readings(ctx, predicate+` ORDER BY me.day,me.taken_at,me.id`, args...)
}

// ReadingsByDay counts the current readings of each day from from through through, both included: the readings
// that Series gives, for all metrics. A corrected reading counts once, and a retracted one not at all. The readings
// of a tombstoned metric do not count, as they do not in Moods. A day without one is absent.
func (s *Store) ReadingsByDay(ctx context.Context, from, through string) (map[string]int, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	return s.readingsBy(ctx, `me.day BETWEEN ? AND ?`, from, through)
}

// readingsBy is ReadingsByDay for the days that a condition on me.day selects.
func (s *Store) readingsBy(ctx context.Context, cond string, args ...any) (map[string]int, error) {
	return groupCounts(s.DB.R.QueryContext(ctx, `
		SELECT me.day, count(*)
		  FROM measurement_values me
		  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL
		 WHERE `+cond+`
		 GROUP BY me.day`, args...))
}

// Measurement reads one row of the append-only table, current or not, and what corrected it.
type Measurement struct {
	Reading
	Current      bool  `json:"current"`
	Supersedes   int64 `json:"supersedes_id,omitempty"`
	SupersededBy int64 `json:"superseded_by,omitempty"`
	Retraction   bool  `json:"retraction"`
}

func (s *Store) Measurement(ctx context.Context, id int64) (*Measurement, error) {
	m := &Measurement{}
	var value sql.NullFloat64
	err := s.DB.R.QueryRowContext(ctx, `
		SELECT `+readingColumns+`, coalesce(me.supersedes_id, 0),
		       coalesce((SELECT x.id FROM measurements x WHERE x.supersedes_id = me.id), 0),
		       EXISTS (SELECT 1 FROM measurement_values v WHERE v.id = me.id)
		  FROM measurements me JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key  WHERE me.id = ?`, id).
		Scan(&m.ID, &m.Metric, &m.Day, &m.TakenAt, &m.TZ, &value, &m.CapturedWith, &m.Supersedes, &m.SupersededBy, &m.Current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no measurement %d", id)
	}
	m.Value, m.Retraction = value.Float64, !value.Valid
	return m, err
}
