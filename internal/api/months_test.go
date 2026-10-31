package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheMonthIsACalendar: each day of a month, whether it has a live day page, its first line and its mood; the
// months around it, across a year; the current month; and a month that is not one.
func TestTheMonthIsACalendar(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	capture := func(day string, v map[string]string) {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	capture("2031-05-01", map[string]string{"text": "May day: a walk by the lake.", "mood": "4"})
	capture("2031-05-14", map[string]string{"mood": "3"})
	capture("2031-05-31", map[string]string{"text": "The last day."})
	gone := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/2031-05-20")), "capture"), map[string]string{"text": "Scratch"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(gone, "page"))), "tombstone-pages"), nil))

	for _, tc := range []struct {
		month, title, prev, next string
		days                     int
	}{
		{"2031-05", "May 2031", "/months/2031-04", "/months/2031-06", 31},
		{"2031-06", "June 2031", "/months/2031-05", "/months/2031-07", 30},
		{"2028-02", "February 2028", "/months/2028-01", "/months/2028-03", 29},
		{"2031-02", "February 2031", "/months/2031-01", "/months/2031-03", 28},
		{"2031-12", "December 2031", "/months/2031-11", "/months/2032-01", 31},
		{"2031-01", "January 2031", "/months/2030-12", "/months/2031-02", 31},
	} {
		m := apitest.Must(t)(c.Get(t.Context(), "/months/"+tc.month))
		days := props(m)["days"].([]any)
		if m.Title != tc.title || apitest.Href(m, "prev") != tc.prev || apitest.Href(m, "next") != tc.next || len(days) != tc.days {
			t.Errorf("%s: %q, prev %q, next %q, %d days", tc.month, m.Title, apitest.Href(m, "prev"), apitest.Href(m, "next"), len(days))
		}
		if first := days[0].(map[string]any)["day"]; first != tc.month+"-01" {
			t.Errorf("%s starts on %v", tc.month, first)
		}
		if len(m.Entities) != tc.days || m.Entities[0].Href != "/days/"+tc.month+"-01" {
			t.Errorf("%s embeds %d day links", tc.month, len(m.Entities))
		}
	}

	may := props(apitest.Must(t)(c.Get(t.Context(), "/months/2031-05")))
	var marked []string
	for _, d := range may["days"].([]any) {
		m := d.(map[string]any)
		if m["page"] == true || m["mood"] != nil {
			marked = append(marked, fmt.Sprintf("%v %v %v %v", m["day"], m["page"], m["mood"], m["first_line"]))
		}
	}
	want := "2031-05-01 true [4] (00:00) May day: a walk by the lake. | 2031-05-14 true [3] <nil> | 2031-05-31 true <nil> (00:00) The last day."
	if got := strings.Join(marked, " | "); got != want {
		t.Errorf("May:\n got %s\nwant %s", got, want)
	}
	if may["month"] != "2031-05" || may["today"] != fixedToday {
		t.Errorf("May's month %v, today %v", may["month"], may["today"])
	}

	for _, bad := range []string{"2031-13", "2031-5", "x", "2031-05-01"} {
		if _, err := c.Get(t.Context(), "/months/"+bad); apitest.Status(err) != 422 {
			t.Errorf("/months/%s: %v", bad, err)
		}
	}
}

// TestOpenDayGoesToTheDay: open-day goes to the day it names, and to today when it names no day or names the word
// "today", as GET /days/today does. An empty day is today.
func TestOpenDayGoesToTheDay(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	for _, tc := range []struct {
		query, to string // to: the day of the redirect, "" for a refusal
	}{
		{"?day=2031-04-30", "2031-04-30"}, {"?day=1999-12-31", "1999-12-31"},
		{"", fixedToday}, {"?day=", fixedToday},
		{"?day=today", fixedToday}, {"?day=2031-02-30", ""}, {"?day=x", ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/days/go"+tc.query, nil))
		code, to := rec.Code, rec.Header().Get("Location")
		if tc.to != "" && (code != http.StatusSeeOther || to != "/days/"+tc.to) || tc.to == "" && code != http.StatusUnprocessableEntity {
			t.Errorf("GET /days/go%s: %d %q, want %q", tc.query, code, to, tc.to)
		}
	}
	// The catalog action, as every client sends it, opens the day.
	day := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "open-day"), map[string]string{"day": "2031-04-30"}))
	if day.Title != "2031-04-30" || day.Class[0] != "day" {
		t.Errorf("open-day through the client: %s %v", day.Title, day.Class)
	}
}

// TestTheFirstMonthOfTheDaysIsACalendar: January 0000 holds the first day that a date can name. Its calendar has its
// 31 days, a day page on its first day and a reading on its third day.
func TestTheFirstMonthOfTheDaysIsACalendar(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/0000-01-01")), "capture"), map[string]string{"text": "The first day."}))
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": "0000-01-03", "value": "70"}))
	m, err := c.Get(t.Context(), "/months/0000-01")
	if err != nil {
		t.Fatalf("/months/0000-01: %v", err)
	}
	days := props(m)["days"].([]any)
	if m.Title != "January 0000" || len(days) != 31 {
		t.Fatalf("%q, %d days", m.Title, len(days))
	}
	first, third := days[0].(map[string]any), days[2].(map[string]any)
	if first["day"] != "0000-01-01" || first["page"] != true || first["first_line"] != "(00:00) The first day." {
		t.Errorf("the first day: %v", first)
	}
	if third["day"] != "0000-01-03" || third["page"] != false || said(third["readings"]) != "1" {
		t.Errorf("the third day: %v", third)
	}
}

// TestTheZoomLinksEveryLevel: a week, a month and a year link Days, Week, Month and Year: the period of today when
// today is in it, else of its first day (a week's Thursday), and the stream before the day after the period.
func TestTheZoomLinksEveryLevel(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday) // 2031-05-14
	for _, tc := range []struct {
		page, days, week, month, year string
	}{
		{"/months/2031-05", "/days", "/weeks/2031-W20", "/months/2031-05", "/years/2031"},
		{"/months/2031-04", "/days", "/weeks/2031-W14", "/months/2031-04", "/years/2031"},
		{"/years/2031", "/days", "/weeks/2031-W20", "/months/2031-05", "/years/2031"},
		{"/years/2030", "/days", "/weeks/2030-W01", "/months/2030-01", "/years/2030"},
		{"/months/9999-12", "/days", "/weeks/9999-W48", "/months/9999-12", "/years/9999"},
	} {
		e := apitest.Must(t)(c.Get(t.Context(), tc.page))
		got := strings.Join([]string{apitest.Href(e, "days"), apitest.Href(e, "week"), apitest.Href(e, "month"), apitest.Href(e, "year")}, " ")
		if want := strings.Join([]string{tc.days, tc.week, tc.month, tc.year}, " "); got != want {
			t.Errorf("%s zooms to %s, want %s", tc.page, got, want)
		}
	}
}
