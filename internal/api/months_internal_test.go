package api

import (
	"slices"
	"testing"
	"time"

	"aminte/internal/core"
	"aminte/text"
)

// TestWrittenDaysMarksTheDaysWithAPage: writtenDays gives each day from the first through the last, in order, and
// marks the days that have a day page. An entry outside the days marks nothing and adds no day.
func TestWrittenDaysMarksTheDaysWithAPage(t *testing.T) {
	day := func(s string) time.Time {
		t.Helper()
		d, ok := text.ParseDay(s)
		if !ok {
			t.Fatalf("%s is not a day", s)
		}
		return d
	}
	for _, c := range []struct {
		name          string
		first, last   string
		entries       []string
		days          int
		from, through string   // the first and the last day given
		written       []string // the days marked, in order
	}{
		{"a month with no page", "2026-10-01", "2026-10-31", nil, 31, "2026-10-01", "2026-10-31", nil},
		{"the first, a middle and the last day", "2026-10-01", "2026-10-31", []string{"2026-10-01", "2026-10-16", "2026-10-31"},
			31, "2026-10-01", "2026-10-31", []string{"2026-10-01", "2026-10-16", "2026-10-31"}},
		{"entries outside the days", "2026-10-01", "2026-10-31", []string{"2026-09-30", "2026-11-01"}, 31, "2026-10-01", "2026-10-31", nil},
		{"a leap February", "2024-02-01", "2024-02-29", []string{"2024-02-28", "2024-02-29", "2024-03-01"}, 29, "2024-02-01", "2024-02-29",
			[]string{"2024-02-28", "2024-02-29"}},
		{"a February of 28 days", "2026-02-01", "2026-02-28", []string{"2026-02-28", "2026-03-01"}, 28, "2026-02-01", "2026-02-28", []string{"2026-02-28"}},
		{"a leap year", "2024-01-01", "2024-12-31", []string{"2024-01-01", "2024-12-31"}, 366, "2024-01-01", "2024-12-31", []string{"2024-01-01", "2024-12-31"}},
		{"the first month a day can name", "0000-01-01", "0000-01-31", []string{"0000-01-01"}, 31, "0000-01-01", "0000-01-31", []string{"0000-01-01"}},
		{"the last month a day can name", "9999-12-01", "9999-12-31", []string{"9999-12-31"}, 31, "9999-12-01", "9999-12-31", []string{"9999-12-31"}},
		{"one day", "2026-10-16", "2026-10-16", []string{"2026-10-16"}, 1, "2026-10-16", "2026-10-16", []string{"2026-10-16"}},
	} {
		var entries []core.DayEntry
		for _, d := range c.entries {
			entries = append(entries, core.DayEntry{Day: d})
		}
		got := writtenDays(entries, day(c.first), day(c.last))
		if len(got) != c.days {
			t.Errorf("%s: %d days, want %d", c.name, len(got), c.days)
			continue
		}
		if got[0].Day != c.from || got[len(got)-1].Day != c.through {
			t.Errorf("%s: the days are from %s through %s, want from %s through %s", c.name, got[0].Day, got[len(got)-1].Day, c.from, c.through)
		}
		var written []string
		for i, d := range got {
			if i > 0 && d.Day <= got[i-1].Day { // with the count and the two ends, an order with no repeat is every day
				t.Errorf("%s: %s follows %s", c.name, d.Day, got[i-1].Day)
			}
			if d.Page {
				written = append(written, d.Day)
			}
		}
		if !slices.Equal(written, c.written) {
			t.Errorf("%s: the written days are %v, want %v", c.name, written, c.written)
		}
	}
}

// TestNeighbourLinks: a neighbour that is a day has its link; a neighbour before 0000-01-01 or after 9999-12-31 has
// none, and the other side keeps its own.
func TestNeighbourLinks(t *testing.T) {
	day := func(s string) time.Time {
		d, ok := text.ParseDay(s)
		if !ok {
			t.Fatalf("%s is not a day", s)
		}
		return d
	}
	for _, c := range []struct {
		name       string
		prev, next time.Time
		want       []string // the rel of each link
	}{
		{"both", day("2024-05-10"), day("2024-05-12"), []string{"prev", "next"}},
		{"the first day", day("0000-01-01").AddDate(0, 0, -1), day("0000-01-02"), []string{"next"}},
		{"the last day", day("9999-12-30"), day("9999-12-31").AddDate(0, 0, 1), []string{"prev"}},
	} {
		var rels []string
		for _, l := range neighbourLinks(neighbour{c.prev, "/p", "P"}, neighbour{c.next, "/n", "N"}) {
			rels = append(rels, l.Rel[0])
		}
		if !slices.Equal(rels, c.want) {
			t.Errorf("%s: links %v, want %v", c.name, rels, c.want)
		}
	}
}
