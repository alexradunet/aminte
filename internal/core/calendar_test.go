package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"lifelog/text"
)

// daysFrom are the days from from through through, as the stream gives the reads its days.
func daysFrom(t *testing.T, from, through string) []string {
	t.Helper()
	first, ok1 := text.ParseDay(from)
	last, ok2 := text.ParseDay(through)
	if !ok1 || !ok2 {
		t.Fatalf("%s to %s is not a window", from, through)
	}
	var out []string
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(time.DateOnly))
	}
	return out
}

// The calendar's reads (docs/plans/101-the-calendar-holds-every-dated-thing.md). Every expected value is worked out by
// hand from the rows the test writes.

func birthdayLines(bs []birthday) string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Day+" "+b.Title)
	}
	return strings.Join(out, " | ")
}

func TestBirthdaysComeBackEachYear(t *testing.T) {
	s := fresh(t)
	person := func(title, birth, death string) int64 {
		t.Helper()
		id, err := s.CreatePerson(ctx, "cli", title, title, birth, death)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	person("Ana", "1990-05-14", "")
	person("Ion", "1930-03-01", "2020-05-14")
	person("Leap", "2000-02-29", "")
	person("Later", "2040-05-14", "")
	person("Year end", "1985-12-31", "")
	person("New year", "1985-01-02", "")
	person("No days", "", "")
	gone := person("Gone", "1990-05-14", "")
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ from, through, want string }{
		// a death day is not a birthday; a person not yet born and a tombstoned one are not there
		{"2031-05-01", "2031-05-31", "2031-05-14 Ana"},
		{"2020-05-14", "2020-05-14", "2020-05-14 Ana"},
		// 29 February is on 28 February in a common year, and on its own day in a leap year
		{"2031-02-01", "2031-02-28", "2031-02-28 Leap"},
		{"2032-02-27", "2032-03-01", "2032-03-01 Ion | 2032-02-29 Leap"},
		// the birth day itself, and a day before any birth
		{"2000-02-29", "2000-03-01", "2000-03-01 Ion | 2000-02-29 Leap"},
		{"1929-05-14", "1929-05-14", ""},
		// a week across the new year
		{"2031-12-29", "2032-01-04", "2032-01-02 New year | 2031-12-31 Year end"},
	} {
		as, err := s.birthdaysOn(ctx, daysFrom(t, tc.from, tc.through))
		if err != nil {
			t.Fatalf("%s to %s: %v", tc.from, tc.through, err)
		}
		if got := birthdayLines(as); got != tc.want {
			t.Errorf("%s to %s:\n got %s\nwant %s", tc.from, tc.through, got, tc.want)
		}
	}
}

func TestDatedRowsOfAWindow(t *testing.T) {
	s := fresh(t)
	_, _, err := s.Capture(ctx, "cli", "2031-05-14", "A walk by the lake.", nil)
	if err != nil {
		t.Fatal(err)
	}
	page := func(title, day string) int64 {
		t.Helper()
		var id int64
		if err := s.Do(ctx, "cli", func(tx *Tx) error {
			var err error
			id, _, err = tx.CreatePage(title, "", day)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	page("Notes on rivers", "2031-05-14")
	page("June plan", "2031-06-01")
	if err := s.Tombstone(ctx, "cli", page("Scratch", "2031-05-14")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 21; i++ {
		page(fmt.Sprintf("Import %02d", i), "2031-05-20")
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Lake.jpg", SHA256: strings.Repeat("a", 64), MIME: "image/jpeg", Day: "2031-05-14"}); err != nil {
		t.Fatal(err)
	}

	set, err := json.Marshal(daysFrom(t, "2031-05-01", "2031-05-31"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.dated(ctx, `IN (SELECT value FROM json_each(:days))`, 20, sql.Named("days", string(set)))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s %s %s %d", r.Day, r.Kind, r.Title, r.Pages))
	}
	want := []string{
		"2031-05-14 file Lake.jpg 2",
		"2031-05-14 page Notes on rivers 2",
	}
	for i := 1; i <= 20; i++ {
		want = append(want, fmt.Sprintf("2031-05-20 page Import %02d 21", i))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("dated rows:\n got %s\nwant %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
