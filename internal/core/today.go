package core

import (
	"context"
	"database/sql"
)

// The reads of the Today strip of the rail (docs/plans/137-pixel-wood-and-the-focus-pass.md, part 4): how many days
// in a row the owner wrote, and the chips with what today says of each.

// streakQuery is cookbook/streaks.md. The tests/ suite `streaks` runs the recipe; TestStreakIsTheCookbookRecipe
// keeps this copy equal to it.
const streakQuery = `
WITH RECURSIVE
  start(day) AS (
    SELECT CASE WHEN EXISTS (SELECT 1 FROM entities p WHERE p.day = :today AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> '')
                THEN :today ELSE date(:today, '-1 day') END),
  run(day, n) AS (
    SELECT s.day, 1 FROM start s
     WHERE EXISTS (SELECT 1 FROM entities p WHERE p.day = s.day AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> '')
    UNION ALL
    SELECT date(r.day, '-1 day'), r.n + 1 FROM run r
     WHERE r.n < 3660
       AND EXISTS (SELECT 1 FROM entities p WHERE p.day = date(r.day, '-1 day') AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> ''))
SELECT coalesce(max(n), 0) AS streak,
       EXISTS (SELECT 1 FROM entities p WHERE p.day = :today AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> '') AS written_today
  FROM run`

// Streak is the count of days written in a row that ends today, or yesterday when today has no words yet
// (cookbook/streaks.md), and whether today has words.
func (s *Store) Streak(ctx context.Context, today string) (streak int, writtenToday bool, err error) {
	if err := checkDay("today", today); err != nil {
		return 0, false, err
	}
	err = s.DB.R.QueryRowContext(ctx, streakQuery, sql.Named("today", today)).Scan(&streak, &writtenToday)
	return streak, writtenToday, err
}

// Habit is a daily metric with no unit (D31, D24). Done is nil when it has no reading today, else whether the newest
// reading of today is not 0.
type Habit struct {
	Name string `json:"name"`
	Done *bool  `json:"done"`
}

// Habits are the chips of the day today, by name: the live metrics that are daily and have no unit (D31, D24). A
// reading of 0 is not done; any other value of today is. A daily metric with a unit is not a chip.
func (s *Store) Habits(ctx context.Context, today string) ([]Habit, error) {
	if err := checkDay("today", today); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT m_name.title,
		       (SELECT v.value FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :today
		         ORDER BY v.taken_at DESC, v.id DESC LIMIT 1)
		  FROM metrics m JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL
		  JOIN entity_names m_name ON m_name.entity_id = e.id AND m_name.name_key = e.preferred_name_key
		 WHERE m.daily = 1 AND m.unit = ''
		 ORDER BY e.preferred_name_key`, sql.Named("today", today))
	var h Habit
	var value sql.NullFloat64
	out := []Habit{}
	if err := each(rows, err, []any{&h.Name, &value}, func() {
		h.Done = nil
		if value.Valid {
			done := value.Float64 != 0
			h.Done = &done
		}
		out = append(out, h)
	}); err != nil {
		return nil, err
	}
	return out, nil
}
