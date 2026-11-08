package core

import (
	"context"
	"database/sql"

	"aminte/text"
)

// The calendar's reads (docs/plans/101-the-calendar-holds-every-dated-thing.md): what names a day, over a set of
// days. The stream reads them for its cards. Each read is of rows the schema holds; the anniversaries are the
// application's reading of a person's days.

// datedRow is a row the calendar shows on its day: a page that is not a day page and has the day (kind page, or file
// for a file). Pages is the count of the day's pages and files, on a page or a file.
type datedRow struct {
	Day   string
	Kind  string
	ID    int64
	Title string
	Pages int
}

// dated are the dated rows of the days that days selects, by day: the pages in the order of their titles, at most pagesPerDay
// pages and files a day. days is the rest of a condition on a day column, after the column ("IN (SELECT value FROM json_each(:days))"), with its named arguments.
func (s *Store) dated(ctx context.Context, days string, pagesPerDay int, args ...any) ([]datedRow, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT day, kind, id, title, n FROM (
		  SELECT p.day AS day, CASE p.entity_type WHEN 'file' THEN 'file' ELSE 'page' END AS kind, p.id AS id,
		         p_name.title AS title, 2 AS rank, p.preferred_name_key AS sortkey,
		         row_number() OVER (PARTITION BY p.day ORDER BY p.preferred_name_key, p.id) AS r,
		         count(*) OVER (PARTITION BY p.day) AS n
		    FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		   WHERE p.day `+days+` AND NOT p.is_journal AND p.deleted_at IS NULL
		) WHERE r <= :cap
		ORDER BY day, rank, sortkey, id`, append(args, sql.Named("cap", pagesPerDay))...)
	return list(rows, err, func(d *datedRow) []any { return []any{&d.Day, &d.Kind, &d.ID, &d.Title, &d.Pages} })
}

// birthday is a live person's birth day, or its day in a later year, on Day inside a window.
type birthday struct {
	Day   string
	ID    int64
	Title string
}

// birthdaysOn are the birthdays on a set of days: for each person, in the order of the titles, the days in the order
// of days. The days need not be one window: the stream's days can span decades. Each day is an exact day (YYYY-MM-DD).
// A day of 29 February comes back on 28 February in a common year.
func (s *Store) birthdaysOn(ctx context.Context, days []string) ([]birthday, error) {
	// the days by their month and day ("05-14"), with 29 February on 28 February of a common year
	on := map[string][]string{}
	for _, day := range days {
		t, ok := text.ParseDay(day)
		if !ok {
			continue
		}
		on[day[5:]] = append(on[day[5:]], day)
		if day[5:] == "02-28" && t.AddDate(0, 0, 1).Day() == 1 {
			on["02-29"] = append(on["02-29"], day)
		}
	}
	keys := make([]string, 0, len(on))
	for k := range on {
		keys = append(keys, k)
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT e.id, n.title, p.birth_day
		  FROM people p
		  JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
		  JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
		 WHERE substr(p.birth_day, 6) IN (SELECT value FROM json_each(:set))
		 ORDER BY e.preferred_name_key, e.id`, sql.Named("set", jsonArray(keys)))
	out := []birthday{}
	var id int64
	var title, birth string
	if err := each(rows, err, []any{&id, &title, &birth}, func() {
		for _, day := range on[birth[5:]] {
			if day >= birth { // before the person's day: not yet an anniversary
				out = append(out, birthday{Day: day, ID: id, Title: title})
			}
		}
	}); err != nil {
		return nil, err
	}
	return out, nil
}
