package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"lifelog/internal/db/dbtest"
)

var ctx = context.Background()

func fresh(t *testing.T) *Store {
	t.Helper()
	return &Store{DB: dbtest.New(t)}
}

func status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

func titles(es []Edge, kind string) string {
	var out []string
	for _, e := range es {
		if e.Kind == kind {
			out = append(out, e.Title)
		}
	}
	return strings.Join(out, ",")
}

func ptr(f float64) *float64 { return &f }

// counts are a database's rows by kind: a test compares them before and after a write that must change nothing.
type counts struct {
	Pages    int
	ByType   map[string]int
	Readings int
	Links    map[string]int
	Metrics  int
	BySource map[string]int
}

func countRows(ctx context.Context, s *Store) (*counts, error) {
	c := &counts{ByType: map[string]int{}, Links: map[string]int{}, BySource: map[string]int{}}
	q := func(sql string, dest map[string]int) error {
		rows, err := s.DB.R.QueryContext(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var n int
			if err := rows.Scan(&k, &n); err != nil {
				return err
			}
			dest[k] = n
		}
		return rows.Err()
	}
	if err := q(`SELECT entity_type, count(*) FROM entities WHERE deleted_at IS NULL GROUP BY 1`, c.ByType); err != nil {
		return nil, err
	}
	if err := q(`SELECT kind, count(*) FROM links GROUP BY 1`, c.Links); err != nil {
		return nil, err
	}
	if err := q(`SELECT source, count(*) FROM entities GROUP BY 1`, c.BySource); err != nil {
		return nil, err
	}
	err := s.DB.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM entities WHERE entity_type <> 'metric'), (SELECT count(*) FROM measurement_values),
	                                           (SELECT count(*) FROM metrics)`).
		Scan(&c.Pages, &c.Readings, &c.Metrics)
	return c, err
}

// everyRow is every row of the tables that a write of core changes, as text: a test compares it before and after a
// write that must write nothing. A tombstone, a title, a type or a revision that changes changes the text.
func everyRow(t *testing.T, s *Store) string {
	t.Helper()
	var b strings.Builder
	for _, table := range []string{"entities", "entity_names", "people", "metrics", "files", "measurements", "links"} {
		rows, err := s.DB.R.QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		values := make([]any, len(columns))
		dst := make([]any, len(columns))
		for i := range values {
			dst[i] = &values[i]
		}
		for rows.Next() {
			if err := rows.Scan(dst...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%s %v\n", table, values)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

// TestRowReadsReportEveryFault: each and list, the one loop of core over rows, return the error of the query, of a
// Scan, of the rows (Err) and of their Close, and they close the rows in every case. The fault driver of
// integrity_test.go gives each fault: its rows hold one integer, and with the fault scan a second row that is text.
func TestRowReadsReportEveryFault(t *testing.T) {
	for _, c := range []struct {
		fault    string
		rows     int  // the rows that each gives to its row function
		closed   int  // the rows the driver closed
		ofDriver bool // the driver's fault is in the error
		ofScan   bool // the Scan error of the second row is in the error
	}{
		{fault: "", rows: 0, closed: 1},
		{fault: "query", rows: 0, closed: 0, ofDriver: true},
		{fault: "scan", rows: 1, closed: 1, ofScan: true},
		{fault: "late", rows: 1, closed: 1, ofDriver: true},
		{fault: "close", rows: 1, closed: 1, ofDriver: true},
		{fault: "scan-close", rows: 1, closed: 1, ofDriver: true, ofScan: true},
	} {
		t.Run("fault="+c.fault, func(t *testing.T) {
			check := func(what string, err error, closed int) {
				t.Helper()
				if (err != nil) != (c.ofDriver || c.ofScan) {
					t.Errorf("%s: error %v, want an error: %v", what, err, c.ofDriver || c.ofScan)
				}
				if errors.Is(err, errIntegrityFault) != c.ofDriver {
					t.Errorf("%s: error %v, want the driver's fault in it: %v", what, err, c.ofDriver)
				}
				if ofScan := err != nil && strings.Contains(err.Error(), "Scan error on column index 0"); ofScan != c.ofScan {
					t.Errorf("%s: error %v, want the Scan error in it: %v", what, err, c.ofScan)
				}
				if closed != c.closed {
					t.Errorf("%s: the driver closed %d rows, want %d", what, closed, c.closed)
				}
			}

			driver := &integrityConnector{check: "orphan entities", fault: c.fault}
			s := faultIntegrityStore(t, driver)
			var v int64
			var seen []int64
			rows, err := s.DB.R.QueryContext(ctx, "SELECT value")
			err = each(rows, err, []any{&v}, func() { seen = append(seen, v) })
			check("each", err, driver.closed)
			if len(seen) != c.rows || c.rows == 1 && seen[0] != 42 {
				t.Errorf("each: the row function saw %v, want %d rows of 42", seen, c.rows)
			}

			driver = &integrityConnector{check: "orphan entities", fault: c.fault}
			s = faultIntegrityStore(t, driver)
			rows, err = s.DB.R.QueryContext(ctx, "SELECT value")
			got, err := list(rows, err, func(v *int64) []any { return []any{v} })
			check("list", err, driver.closed)
			switch {
			case err != nil && got != nil:
				t.Errorf("list: %v with the error %v, want no list", got, err)
			case err == nil && (got == nil || len(got) != 0):
				t.Errorf("list: %#v, want an empty list that is not nil", got)
			}
		})
	}
}

// TestTheStoreClockGivesLocalAndToday: with a Clock, Local is the instant of that clock in the Zone, and Today is the
// day of Local. With no Clock the clock of this device answers, with a Zone and with none.
func TestTheStoreClockGivesLocalAndToday(t *testing.T) {
	zone := time.FixedZone("+14", 14*60*60)
	for name, s := range map[string]*Store{"no zone": {}, "a zone": {Zone: zone}} {
		before := time.Now()
		local := s.Local()
		if after := time.Now(); local.Before(before) || local.After(after) {
			t.Errorf("%s: Local gives %v, not an instant between %v and %v", name, local, before, after)
		}
	}
	at := time.Date(2026, 3, 10, 11, 30, 0, 0, time.UTC) // 2026-03-11 01:30 in the zone
	s := &Store{Clock: func() time.Time { return at }, Zone: zone}
	if local := s.Local(); !local.Equal(at) || local.Location() != zone || s.Today() != "2026-03-11" {
		t.Errorf("a fixed clock: Local %v, Today %s; want %v in +14, and 2026-03-11", local, s.Today(), at)
	}
}
