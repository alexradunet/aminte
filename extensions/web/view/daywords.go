package view

import (
	"strconv"
	"time"

	"aminte/text"
)

// The calendar in words and numbers: days stepped, weekdays, and the cells of a month.

// addDays is the day n days after day ("" for a day that is not one).
func addDays(day string, n int) string {
	t, ok := text.ParseDay(day)
	if !ok {
		return ""
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// weekday is the name of a day's weekday, "" for a day that is not one.
func weekday(day string) string {
	t, ok := text.ParseDay(day)
	if !ok {
		return ""
	}
	return t.Weekday().String()
}

// shortDay is a day without its year, "Mon 12 Oct": the sidebar's today, a day's own heading and the step to the
// day beside it, where the year is the page's. Everywhere else a day is written by Date, with its year when it is
// not this year's.
func shortDay(day string) string {
	t, ok := text.ParseDay(day)
	if !ok {
		return day
	}
	return t.Format(layoutDate)
}

// dayOfMonth is the number of a day in its month, for the calendar's cells.
func dayOfMonth(day string) string {
	if t, ok := text.ParseDay(day); ok {
		return strconv.Itoa(t.Day())
	}
	return day
}

// monthBand is the band of the calendar leaf: "Oct 2026".
func monthBand(day string) string {
	if t, ok := text.ParseDay(day); ok {
		return t.Format("Jan 2006")
	}
	return ""
}

// beyond counts what a card leaves out: the items after the first n, and the pages the day's items already left out.
func beyond(list any, n int, morePages any) int {
	items := anyList(list)
	extra, _ := morePages.(float64)
	return max(len(items)-n, 0) + int(extra)
}

// anyList is a list property as a template gets it; nil for anything else.
func anyList(list any) []any {
	items, _ := list.([]any)
	return items
}

// weeks puts the days of a month into the rows of a calendar, Monday first: nil fills a cell before the first day and
// after the last.
func weeks(list any) [][]any {
	days, _ := list.([]any)
	if len(days) == 0 {
		return nil
	}
	m, _ := days[0].(map[string]any)
	first, _ := m["day"].(string)
	t, ok := text.ParseDay(first)
	if !ok {
		return nil
	}
	var out [][]any
	row := make([]any, (int(t.Weekday())+6)%7)
	for _, d := range days {
		if row = append(row, d); len(row) == 7 {
			out, row = append(out, row), nil
		}
	}
	if len(row) > 0 {
		out = append(out, append(row, make([]any, 7-len(row))...))
	}
	return out
}
