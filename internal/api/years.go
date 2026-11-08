package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"aminte/internal/core"
	"aminte/siren"
)

// A year looks back over the calendar (docs/plans/097-the-almanac-reflects.md): its twelve months, with their written
// days.

// yearMonth is a month of a year: its days, and how many of them have a day page.
type yearMonth struct {
	Month   string       `json:"month"`
	Written int          `json:"written"`
	Days    []writtenDay `json:"days"`
}

// yearQuarter is a quarter of a year: its key, and the id of its page, 0 when it has none.
type yearQuarter struct {
	Key    string `json:"key"`
	PageID int64  `json:"page_id,omitempty"`
}

var yearPattern = regexp.MustCompile(`^\d{4}$`)

func (h *server) year(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	s := r.PathValue("year")
	year, _ := strconv.Atoi(s)
	if !yearPattern.MatchString(s) || !siren.IsYear(year) {
		return nil, core.Invalid("a year is YYYY")
	}
	first, last := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
	from, through := first.Format(time.DateOnly), last.Format(time.DateOnly)
	entries, err := h.s.DayEntries(ctx, from, through, false, 366)
	if err != nil {
		return nil, err
	}
	byDay, err := h.s.ReadingsByDay(ctx, from, through)
	if err != nil {
		return nil, err
	}
	readings := 0
	for _, n := range byDay {
		readings += n
	}
	e := &siren.Entity{Class: []string{"year"}, Title: s}
	months := []yearMonth{}
	days := writtenDays(entries, first, last) // the days of the year, built once: each month takes its own
	for m := first; m.Year() == year; m = m.AddDate(0, 1, 0) {
		n := m.AddDate(0, 1, -1).Day() // how many days the month has
		ym := yearMonth{Month: m.Format("2006-01"), Days: days[:n:n]}
		days = days[n:]
		for _, d := range ym.Days {
			if d.Page {
				ym.Written++
			}
		}
		months = append(months, ym)
		e.Entities = append(e.Entities, link("month", siren.MonthHref(m), m.Format(layoutMonth)))
	}
	e.Links = []siren.Link{link("self", siren.YearHref(year), s)}
	if siren.IsYear(year - 1) {
		e.Links = append(e.Links, link("prev", siren.YearHref(year-1), fmt.Sprintf("%04d", year-1)))
	}
	if siren.IsYear(year + 1) {
		e.Links = append(e.Links, link("next", siren.YearHref(year+1), fmt.Sprintf("%04d", year+1)))
	}
	e.Links = append(e.Links, zoomLinks(anchorIn(h.s.Today(), first, last))...)
	e.Links = append(e.Links, link("index", "/", "Home"))
	props := map[string]any{"year": s, "today": h.s.Today(), "written": len(entries), "readings": readings, "months": months}
	if err := h.withPeriodPage(ctx, e, props, s); err != nil {
		return nil, err
	}
	// The four quarters have no view of their own: each is a link to its page, or to the form that makes it.
	quarters := make([]yearQuarter, 4)
	for i := range quarters {
		key := fmt.Sprintf("%s-Q%d", s, i+1)
		quarters[i] = yearQuarter{Key: key}
		if quarters[i].PageID, err = h.s.PageID(ctx, key); err != nil {
			return nil, err
		}
	}
	props["quarters"] = quarters
	e.Properties = props
	return e, nil
}
