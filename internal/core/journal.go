package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"lifelog/text"
)

// CaptureWithKey appends an entry to a day page, creating or reviving it, with an optional mood (cookbook/capture.md).
// A sender that may send the capture twice gives a key (D28). The key is the sender's, made once for
// one capture. A key that the file holds for this source means the capture was added before: nothing is written, and
// the answer is the day page of that capture and already = true. The key, and the Mood reading of the capture, are
// written in the same transaction as the entry. A key of "" is no key: a second send appends again.
func (t *Tx) CaptureWithKey(key, day, entry string, mood *float64) (id int64, r Sync, already bool, err error) {
	if entry == "" && mood == nil {
		return 0, r, false, Invalid("nothing to capture: give text, a mood or both")
	}
	if key != "" {
		err = t.tx.QueryRowContext(t.ctx, `SELECT page_id FROM capture_keys WHERE source = ? AND import_key = ?`, t.source, key).Scan(&id)
		if err == nil {
			return id, r, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, r, false, err
		}
	}
	if id, err = t.dayPage(day); err != nil {
		return 0, r, false, err
	}
	if key != "" { // the shape of the key is capture_keys_import_key's to refuse
		if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO capture_keys(source, import_key, page_id, recorded_at) VALUES (?, ?, ?, `+nowSQL+`)`,
			t.source, key, id); err != nil {
			return 0, r, false, err
		}
	}
	if entry != "" {
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET body = body || CASE WHEN body = '' THEN '' ELSE char(10, 10) END || ?
		                        WHERE id = ?`, entry, id); err != nil {
			return 0, r, false, err
		}
	}
	var body string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ?`, id).Scan(&body); err != nil {
		return 0, r, false, err
	}
	if r, err = t.syncWikilinks(id, body); err != nil {
		return 0, r, false, err
	}
	if mood != nil {
		var reading int64
		reading, err = t.Record(Reading{Metric: "Mood", Day: day, Value: *mood, CapturedWith: id, ImportKey: key})
		if err == nil && reading == 0 { // the index of measurements holds the key: a Mood recorded under it, not by this capture
			err = Conflict("the key %q was used for a reading of Mood before: give a capture a key of its own", key)
		}
	}
	return id, r, false, err
}

// dayPage is the id of a local day's page: found (revived when tombstoned), or created on its first write
// (cookbook/capture.md).
func (t *Tx) dayPage(day string) (int64, error) {
	id, _, _, err := t.ensurePage(day, day, day)
	return id, err
}

// Capture is CaptureWithKey with no key.
func (s *Store) Capture(ctx context.Context, source, day, entry string, mood *float64) (int64, Sync, error) {
	id, r, _, err := s.CaptureWithKey(ctx, source, "", day, entry, mood)
	return id, r, err
}

// CaptureWithKey is Tx.CaptureWithKey as one write: already says that the key was held, and the capture changed nothing.
func (s *Store) CaptureWithKey(ctx context.Context, source, key, day, entry string, mood *float64) (id int64, r Sync, already bool, err error) {
	type added struct {
		made
		already bool
	}
	m, err := alone(s, ctx, source, func(t *Tx) (added, error) {
		id, r, already, err := t.CaptureWithKey(key, day, entry, mood)
		return added{made{id, r}, already}, err
	})
	return m.id, m.sync, m.already, err
}

// DayRow is one row of cookbook/day-view.md.
type DayRow struct {
	What   string `json:"what"`
	At     string `json:"at,omitempty"`
	Detail string `json:"detail"`
}

// Day is cookbook/day-view.md for one local day, plus the day page's id (0 when the day has none yet) and the
// day's readings with their ids, so a client can correct one.
type Day struct {
	Day      string    `json:"day"`
	PageID   int64     `json:"page_id,omitempty"`
	Rows     []DayRow  `json:"view"`
	Readings []Reading `json:"readings"`
}

func (s *Store) Day(ctx context.Context, day string) (*Day, error) {
	if err := checkDay("day", day); err != nil {
		return nil, err
	}
	d := &Day{Day: day, Rows: []DayRow{}}
	err := s.DB.R.QueryRowContext(ctx, `SELECT id FROM entities WHERE id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND deleted_at IS NULL`, day).Scan(&d.PageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var r DayRow
	var at sql.NullString
	rows, err := s.DB.R.QueryContext(ctx, dayView, sql.Named("day", day))
	if err := each(rows, err, []any{&r.What, &at, &r.Detail}, func() {
		r.At = at.String
		d.Rows = append(d.Rows, r)
	}); err != nil {
		return nil, err
	}
	d.Readings, err = s.readings(ctx, `WHERE me.day = ? ORDER BY me.taken_at, me.id`, day)
	return d, err
}

// dayView implements cookbook/day-view.md with registry lookup and preferred display.
const dayView = `
SELECT what, at, detail FROM (
  SELECT 'day page' AS what, NULL AS at, p.body AS detail
    FROM entities p
   WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = :day) AND p.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN p.revision > 1 THEN ' (edited)' ELSE '' END,
         p.updated_at, p_name.title
    FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
   WHERE p.day = :day AND NOT p.is_journal AND p.deleted_at IS NULL
  UNION ALL
  SELECT p_name.title, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id JOIN entities p ON p.id = m.id AND p.deleted_at IS NULL JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
   WHERE me.day = :day
)
ORDER BY (at IS NOT NULL), at;`

// Moods are the values of the current Mood readings, by day, from from through through, each day's
// in id order: cookbook/mood-over-time.md read for a window of days. The metric is found by its name key, so a
// renamed Mood is still the mood. A day without a reading is absent.
func (s *Store) Moods(ctx context.Context, from, through string) (map[string][]float64, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	return s.moods(ctx, `me.day BETWEEN ? AND ?`, from, through)
}

// moods is Moods for the days that a condition on me.day selects.
func (s *Store) moods(ctx context.Context, cond string, args ...any) (map[string][]float64, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT me.day, me.value
		  FROM measurement_values me
		  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL
		  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'
		 WHERE `+cond+`
		 ORDER BY me.day, me.id`, args...)
	out := map[string][]float64{}
	var day string
	var v float64
	if err := each(rows, err, []any{&day, &v}, func() { out[day] = append(out[day], v) }); err != nil {
		return nil, err
	}
	return out, nil
}

// DayEntry is a live day page as a list of days shows it: its day, its id, and the first line of its body as plain
// text.
type DayEntry struct {
	Day       string `json:"day"`
	PageID    int64  `json:"page_id"`
	FirstLine string `json:"first_line,omitempty"`
	// Picture is the day page's cover (0 when it has none); the calendar draws it behind the day.
	Picture int64  `json:"-"`
	body    string // read for Picture
}

// DayEntries are the live day pages from from through through: in day order, or the newest first, and at most limit
// of them.
func (s *Store) DayEntries(ctx context.Context, from, through string, newestFirst bool, limit int) ([]DayEntry, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	if limit < 1 {
		return nil, Invalid("day entries need a limit of 1 or more")
	}
	order := "ASC"
	if newestFirst {
		order = "DESC"
	}
	return s.dayEntries(ctx, `p.day BETWEEN ? AND ?`, order, limit, from, through)
}

// dayEntries reads the live day pages that a condition on p.day selects. order is ASC or DESC, never user text.
func (s *Store) dayEntries(ctx context.Context, cond, order string, limit int, args ...any) ([]DayEntry, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.day, p.id, p.body FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		 WHERE `+cond+` AND p.is_journal AND p.deleted_at IS NULL
		 ORDER BY p.day `+order+` LIMIT ?`, append(args, limit)...)
	out := []DayEntry{}
	var e DayEntry
	if err := each(rows, err, []any{&e.Day, &e.PageID, &e.body}, func() {
		e.FirstLine = plainLines(e.body, 1, firstLineRunes)
		out = append(out, e)
	}); err != nil {
		return nil, err
	}
	return out, setPictures(ctx, s.DB.R, out, func(e *DayEntry) (string, *int64) { return e.body, &e.Picture })
}

// orderedMark is a number with a dot or a parenthesis at the start of a line. In a first line it is words ("1986.
// Born"), so it is escaped before text.Plain reads the line, and CommonMark does not read it as an ordered list.
var orderedMark = regexp.MustCompile(`^(\d{1,9})([.)])`)

// firstLineRunes is how long a line of a list of days may be before it is cut. A day's first line is plainLines(body, 1,
// firstLineRunes): it is for a list of days, not a rendering.
const firstLineRunes = 160

// plainLines are the first n lines of a body that have words, as plain text (text.Plain), joined by a newline: a
// heading's or a list item's marker cut, the marks of emphasis, code and links cut, an ![[embed]] or an image left out
// (a picture is not words, and a cover goes first in the body), the brackets of a [[wikilink]] cut, and each line cut
// with "…" after runes characters. A line with no words is passed over.
func plainLines(body string, n, runes int) string {
	var out []string
	for line := range strings.SplitSeq(body, "\n") {
		if len(out) == n {
			break
		}
		line = text.BlockMark.ReplaceAllString(strings.TrimSpace(line), "")
		line = text.Plain(orderedMark.ReplaceAllString(line, `$1\$2`))
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > runes {
			line = strings.TrimSpace(string(r[:runes])) + "…"
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// CapturedDay is the local day of the page that the source's capture with the key was added to: the day that stands
// when CaptureWithKey answers that it was added before. A day page never changes its day, and a capture key never
// changes, so the read after the write finds the same row.
func (s *Store) CapturedDay(ctx context.Context, source, key string) (string, error) {
	var day string
	err := s.DB.R.QueryRowContext(ctx, `SELECT p.day FROM capture_keys k JOIN entities p ON p.id = k.page_id
	        WHERE k.source = ? AND k.import_key = ?`, source, key).Scan(&day)
	if err != nil {
		return "", fmt.Errorf("the capture %s added before: %w", key, err)
	}
	return day, nil
}
