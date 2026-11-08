package api

// A week is an ISO 8601 week (docs/decisions/D30-period-pages.md): it starts on Monday, and week 1 holds 4 January. Its page
// is the period page titled 2026-W43. The view lists the seven days and the page of the week.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"aminte/internal/core"
	"aminte/siren"
	"aminte/text"
)

var weekPattern = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

// weekOf is the Monday of a week, YYYY-Www. ok is false for any other text, and for a week that its year does not
// have (week 53 of a year with 52 weeks): the file refuses such a page too (entities_period_exists).
func weekOf(s string) (monday time.Time, ok bool) {
	m := weekPattern.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[1])
	week, _ := strconv.Atoi(m[2])
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC) // 4 January is always in week 1
	monday = jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+7*(week-1))
	y, w := monday.ISOWeek()
	return monday, week >= 1 && y == year && w == week && text.IsDay(monday.Format(time.DateOnly))
}

// weekKey is the key of the week that starts on monday: its title as a period page.
func weekKey(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

func weekHref(monday time.Time) string { return "/weeks/" + weekKey(monday) }

func weekName(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("Week %d of %d", w, y)
}

// currentWeek redirects to the week of today.
func (h *server) currentWeek(*http.Request) (*siren.Entity, error) {
	t, _ := text.ParseDay(h.s.Today())
	return nil, redirect{weekHref(t)}
}

func (h *server) week(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	monday, ok := weekOf(r.PathValue("week"))
	if !ok {
		return nil, core.Invalid("a week is YYYY-Www")
	}
	sunday := monday.AddDate(0, 0, 6)
	from, through, today := monday.Format(time.DateOnly), sunday.Format(time.DateOnly), h.s.Today()
	days, err := h.calendarOf(ctx, from, through)
	if err != nil {
		return nil, err
	}
	key, name := weekKey(monday), weekName(monday)
	e := &siren.Entity{Class: []string{"week"}, Title: name, Links: []siren.Link{link("self", weekHref(monday), name)}}
	prev, next := monday.AddDate(0, 0, -7), monday.AddDate(0, 0, 7)
	e.Links = append(e.Links, neighbourLinks(
		neighbour{prev, weekHref(prev), weekName(prev)},
		neighbour{next, weekHref(next), weekName(next)})...)
	e.Links = append(e.Links, zoomLinks(anchorIn(today, monday, sunday))...)
	e.Links = append(e.Links, link("index", "/", "Home"))
	for _, d := range days {
		e.Entities = append(e.Entities, link("day", siren.DayHref(d.Day), d.Day))
	}
	props := map[string]any{"week": key, "from": from, "through": through, "today": today, "days": days}
	if err := h.withPeriodPage(ctx, e, props, key); err != nil {
		return nil, err
	}
	e.Properties = props
	return e, nil
}

// withPeriodPage adds the page of a period to the entity of its view, when the page exists: its id, body, version and
// cover as properties, a link to the page and the action that saves its body. A period with no page offers create-page
// with its key as the title: that is the one way to make the page.
func (h *server) withPeriodPage(ctx context.Context, e *siren.Entity, props map[string]any, key string) error {
	id, err := h.s.PageID(ctx, key)
	if err != nil {
		return err
	}
	if id == 0 {
		e.Actions = append(e.Actions, action("create-page", nil, map[string]any{"title": key}))
		return nil
	}
	p, err := h.s.PageByID(ctx, id)
	if err != nil || p.DeletedAt != "" {
		return err
	}
	props["page_id"], props["body"], props["version"] = p.ID, p.Body, p.Version
	if p.Cover != nil {
		props["cover"] = p.Cover
	}
	e.Links = append(e.Links, link("page", siren.PageHref(id), "The "+p.Period+" page"))
	e.Actions = append(e.Actions, saveBodyAction(id, p.Body, p.Version))
	return nil
}
