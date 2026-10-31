package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"lifelog/internal/core"
	"lifelog/siren"
	"lifelog/text"
)

// A month is the calendar of the browser (docs/plans/091-the-browser-for-daily-use.md): each day of a month, whether
// it has a day page, the first line of that page, its cover and the day's mood. The day view holds the rest.

// monthDay is one day of a month as the calendar shows it.
type monthDay struct {
	Day       string    `json:"day"`
	Page      bool      `json:"page"`
	FirstLine string    `json:"first_line,omitempty"`
	Mood      []float64 `json:"mood,omitempty"`
	Readings  int       `json:"readings,omitempty"`
	Picture   string    `json:"picture,omitempty"` // the address of the day page's cover
}

// layoutMonth is the layout of a month in words.
const layoutMonth = "January 2006"

func (h *server) month(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	first, ok := siren.MonthOf(r.PathValue("month"))
	if !ok {
		return nil, core.Invalid("a month is YYYY-MM")
	}
	last := first.AddDate(0, 1, -1)
	from, through, today := first.Format(time.DateOnly), last.Format(time.DateOnly), h.s.Today()
	days, err := h.calendarOf(ctx, from, through)
	if err != nil {
		return nil, err
	}
	name := first.Format(layoutMonth)
	prev, next := first.AddDate(0, -1, 0), first.AddDate(0, 1, 0)
	e := &siren.Entity{Class: []string{"month"}, Title: name, Links: []siren.Link{link("self", siren.MonthHref(first), name)}}
	e.Links = append(e.Links, neighbourLinks(
		neighbour{prev, siren.MonthHref(prev), prev.Format(layoutMonth)},
		neighbour{next, siren.MonthHref(next), next.Format(layoutMonth)})...)
	e.Links = append(e.Links, zoomLinks(anchorIn(today, first, last))...)
	e.Links = append(e.Links, link("index", "/", "Home"))
	for _, d := range days {
		e.Entities = append(e.Entities, link("day", siren.DayHref(d.Day), d.Day))
	}
	props := map[string]any{"month": first.Format("2006-01"), "today": today, "days": days}
	if err := h.withPeriodPage(ctx, e, props, first.Format("2006-01")); err != nil {
		return nil, err
	}
	e.Properties = props
	return e, nil
}

// zoomLinks are the levels of the Calendar's zoom (docs/plans/130-the-explore-pass.md, part 3): the stream (days), and
// the month and the year of the anchor. A year that the anchor's year cannot have is left out.
func zoomLinks(anchor time.Time) []siren.Link {
	out := []siren.Link{link("days", "/days", "Days")}
	out = append(out, link("month", siren.MonthHref(anchor), anchor.Format(layoutMonth)))
	out = append(out, link("week", weekHref(mondayOf(anchor)), weekName(mondayOf(anchor))))
	if siren.IsYear(anchor.Year()) {
		out = append(out, link("year", siren.YearHref(anchor.Year()), strconv.Itoa(anchor.Year())))
	}
	return out
}

// mondayOf is the Monday of the ISO week that holds t.
func mondayOf(t time.Time) time.Time { return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)) }

// anchorIn is the day a period's zoom is anchored on: today when it is inside the period, else its first day.
func anchorIn(today string, first, last time.Time) time.Time {
	if t, ok := text.ParseDay(today); ok && !t.Before(first) && !t.After(last) {
		return t
	}
	return first
}

// openDay answers with the address of the day the form names, or of today when it names none or the word "today".
func (h *server) openDay(r *http.Request, act spec, _ string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	day := h.dayNamed(v.Get("day"))
	if day == "" { // the field is optional: no day is today
		day = h.s.Today()
	}
	if !text.IsDay(day) {
		return nil, core.Invalid("day must be a day, YYYY-MM-DD")
	}
	return nil, redirect{siren.DayHref(day)}
}

// calendarOf reads the days from from through through: whether each has a day page, its first line, its mood and its
// readings other than the Mood's (the mood says those). A month reads it; a window is at most 31 days.
func (h *server) calendarOf(ctx context.Context, from, through string) ([]monthDay, error) {
	entries, err := h.s.DayEntries(ctx, from, through, false, 31)
	if err != nil {
		return nil, err
	}
	moods, err := h.s.Moods(ctx, from, through)
	if err != nil {
		return nil, err
	}
	readings, err := h.s.ReadingsByDay(ctx, from, through)
	if err != nil {
		return nil, err
	}
	firstLines, pictures := map[string]string{}, map[string]string{}
	for _, d := range entries {
		firstLines[d.Day], pictures[d.Day] = d.FirstLine, pictureHref(d.Picture)
	}
	days := []monthDay{}
	first, _ := text.ParseDay(from)
	last, _ := text.ParseDay(through)
	for _, w := range writtenDays(entries, first, last) {
		day := w.Day
		days = append(days, monthDay{Day: day, Page: w.Page, FirstLine: firstLines[day], Mood: moods[day],
			Readings: readings[day] - len(moods[day]), Picture: pictures[day]})
	}
	return days, nil
}

// writtenDay is a day and whether it has a day page: the day is written.
type writtenDay struct {
	Day  string `json:"day"`
	Page bool   `json:"page"`
}

// writtenDays are the days from first through last, each with whether one of entries is its day page. The
// calendar and the months of a year draw them.
func writtenDays(entries []core.DayEntry, first, last time.Time) []writtenDay {
	written := make(map[string]bool, len(entries))
	for _, d := range entries {
		written[d.Day] = true
	}
	days := []writtenDay{}
	for t := first; !t.After(last); t = t.AddDate(0, 0, 1) {
		day := t.Format(time.DateOnly)
		days = append(days, writtenDay{Day: day, Page: written[day]})
	}
	return days
}
