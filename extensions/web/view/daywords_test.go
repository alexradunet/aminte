package view

import (
	"strings"
	"testing"
	"time"
)

// TestWeeksStartOnMonday lays months out by hand-checked first weekdays: a month that starts on a Monday, one that
// starts on a Sunday, and a February of four whole weeks.
func TestWeeksStartOnMonday(t *testing.T) {
	month := func(first string, n int) []any {
		t0, _ := time.Parse(time.DateOnly, first)
		var out []any
		for i := range n {
			out = append(out, map[string]any{"day": t0.AddDate(0, 0, i).Format(time.DateOnly)})
		}
		return out
	}
	show := func(ws [][]any) string {
		var rows []string
		for _, w := range ws {
			var cells []string
			for _, d := range w {
				if d == nil {
					cells = append(cells, "--")
				} else {
					cells = append(cells, d.(map[string]any)["day"].(string)[8:])
				}
			}
			rows = append(rows, strings.Join(cells, " "))
		}
		return strings.Join(rows, " / ")
	}
	for _, c := range []struct {
		first string
		n     int
		want  string
	}{
		{"2026-06-01", 30, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28 / 29 30 -- -- -- -- --"},
		{"2026-11-01", 30, "-- -- -- -- -- -- 01 / 02 03 04 05 06 07 08 / 09 10 11 12 13 14 15 / 16 17 18 19 20 21 22 / 23 24 25 26 27 28 29 / 30 -- -- -- -- -- --"},
		{"2027-02-01", 28, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28"},
	} {
		if got := show(weeks(month(c.first, c.n))); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.first, got, c.want)
		}
	}
	if weeks([]any{}) != nil {
		t.Error("no day makes weeks")
	}
}
