package view

import (
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"

	"lifelog/siren"
)

// TestADayIsADate: a day in words against today, with the year only when it is not this year.
func TestADayIsADate(t *testing.T) {
	c := clock{today: "2026-10-12"}
	for in, want := range map[string]string{
		"2026-10-05": "Mon 5 Oct",
		"2025-12-31": "Wed 31 Dec 2025",
		"2027-01-01": "Fri 1 Jan 2027",
		"not a day":  "not a day",
		"2026-02-30": "2026-02-30",
	} {
		if got := c.day(in); got != want {
			t.Errorf("day(%q) = %q, want %q", in, got, want)
		}
	}
	if got := (clock{}).day("2026-10-05"); got != "Mon 5 Oct 2026" {
		t.Errorf("with no today: %q", got)
	}
}

// TestAnInstantIsATimeInTheServersZone: an instant in the clock of the server's day; with no zone, in UTC.
func TestAnInstantIsATimeInTheServersZone(t *testing.T) {
	utc := clock{today: "2026-10-12"}
	bucharest := clock{today: "2026-10-12", zone: time.FixedZone("EEST", 3*3600)}
	for _, c := range []struct {
		c        clock
		in, want string
	}{
		{utc, "2026-10-12T06:48:45.484Z", "today, 06:48 UTC"},
		{utc, "2026-10-11T21:14:03.000Z", "yesterday, 21:14 UTC"},
		{utc, "2026-10-01T06:00:00.000Z", "Thu 1 Oct, 06:00 UTC"},
		{utc, "2025-06-09T21:14:03.482Z", "Mon 9 Jun 2025, 21:14 UTC"},
		{bucharest, "2026-10-12T06:48:45.484Z", "today, 09:48"},
		{bucharest, "2026-10-11T21:14:03.000Z", "today, 00:14"}, // the server's day, not UTC's
		{bucharest, "2026-10-10T22:00:00.000Z", "yesterday, 01:00"},
		{utc, "2020-01-01T23:00:00.000", "2020-01-01T23:00:00.000"}, // a local time with no zone is not an instant
		{utc, "soon", "soon"},
	} {
		if got := c.c.instant(c.in); got != c.want {
			t.Errorf("instant(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestATimeKeepsItsExactValue: the words are inside a <time> element that carries the value as stored.
func TestATimeKeepsItsExactValue(t *testing.T) {
	c := clock{today: "2026-10-12"}
	for in, want := range map[string]template.HTML{
		"2026-10-12T06:48:45.484Z": `<time datetime="2026-10-12T06:48:45.484Z" title="2026-10-12T06:48:45.484Z">today, 06:48 UTC</time>`,
		"2026-10-11":               `<time datetime="2026-10-11">yesterday · Sun 11 Oct</time>`,
		"<b>bold</b>":              `&lt;b&gt;bold&lt;/b&gt;`,
	} {
		if got := c.timeHTML(in); got != want {
			t.Errorf("timeHTML(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestAValueIsDrawnByItsType: the generic view's object as facts with its names in words, a list of objects as a
// table, and every value by its type, never by its name.
func TestAValueIsDrawnByItsType(t *testing.T) {
	c := clock{today: "2026-10-12"}
	var sb strings.Builder
	c.writeValue(&sb, map[string]any{
		"start_utc": "2026-10-12T06:00:00.000Z", "day": "2026-10-05", "open": true, "closed": false, "end_utc": nil,
		"edit_version": 1000000.0, "members": []any{map[string]any{"page_id": 3.0, "kind_deleted_at": "2026-10-11T21:14:03.000Z"}},
		"tags": []any{"a", "b"}, "note": "<script>",
	})
	got := sb.String()
	for _, want := range []string{
		`<dl class="facts"><dt>closed</dt><dd>no</dd><dt>day</dt><dd><time datetime="2026-10-05">7 days ago · Mon 5 Oct</time></dd>`,
		`<dt>end utc</dt><dd><small>—</small></dd>`,
		`<th scope="col">kind deleted at</th><th scope="col">page id</th>`,
		`<td><time datetime="2026-10-11T21:14:03.000Z" title="2026-10-11T21:14:03.000Z">yesterday, 21:14 UTC</time></td><td>3</td>`,
		`<dt>note</dt><dd>&lt;script&gt;</dd>`, `<dt>open</dt><dd>yes</dd>`,
		`<dt>start utc</dt><dd><time datetime="2026-10-12T06:00:00.000Z" title="2026-10-12T06:00:00.000Z">today, 06:00 UTC</time></dd>`,
		`<dt>tags</dt><dd><ul><li>a</li><li>b</li></ul></dd>`, `<dt>edit version</dt><dd>1000000</dd>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the value lacks %s:\n%s", want, got)
		}
	}
}

// TestCountSaysTheNumberAndTheFormOfItsNoun: Count writes the number in full and the noun in the form for that
// number, for a JSON number and for a length, with the words of each site that calls it.
func TestCountSaysTheNumberAndTheFormOfItsNoun(t *testing.T) {
	for _, c := range []struct {
		n         any
		one, many string
		want      string
	}{
		{0.0, "reading", "readings", "0 readings"},
		{1.0, "reading", "readings", "1 reading"},
		{2.0, "reading", "readings", "2 readings"},
		{1234567.0, "reading", "readings", "1234567 readings"},
		{1e6, "day", "days", "1000000 days"},
		{0.5, "day", "days", "0.5 days"},
		{0, "more field", "more fields", "0 more fields"},
		{1, "more field", "more fields", "1 more field"},
		{2, "more field", "more fields", "2 more fields"},
		{1.0, "more page", "more pages", "1 more page"},
		{2.0, "more page", "more pages", "2 more pages"},
		{1.0, "day", "days", "1 day"},
		{1.0, "year", "years", "1 year"},
		{36.0, "year", "years", "36 years"},
		{1.0, "page", "pages", "1 page"},
		{0, "page", "pages", "0 pages"}, // the words of tombstone-pages
		{1, "page", "pages", "1 page"},
		{2, "page", "pages", "2 pages"},
		{1.0, "picture", "pictures", "1 picture"},
		{nil, "reading", "readings", "0 readings"},
		{"1", "reading", "readings", "0 readings"},
	} {
		if got := Count(c.n, c.one, c.many); got != c.want {
			t.Errorf("Count(%#v, %q, %q) = %q, want %q", c.n, c.one, c.many, got, c.want)
		}
	}
}

// TestAPageComparesADayWithTheTodayOfItsWords: Today is the shell's today, the one that the view's words use, and ""
// for a page with no shell. A page compares a day with it, not with the today of its entity: here the entity says
// that today is 15 October and the shell says 16 October, so the page of 16 October is today's page.
func TestAPageComparesADayWithTheTodayOfItsWords(t *testing.T) {
	shell := &Shell{Today: "2026-10-16", Zone: time.UTC}
	for _, c := range []struct {
		shell *Shell
		want  string
	}{{nil, ""}, {shell, "2026-10-16"}} {
		if got := (&view{Shell: c.shell}).Today(); got != c.want {
			t.Errorf("Today with the shell %+v: %q, want %q", c.shell, got, c.want)
		}
	}
	u, err := url.Parse("/days/2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	e := &siren.Entity{Class: []string{"day"}, Title: "2026-10-16", Properties: map[string]any{"day": "2026-10-16", "today": "2026-10-15", "view": []any{}}}
	v, err := pageView(e, Facts{URL: u, Shell: shell})
	if err != nil {
		t.Fatal(err)
	}
	b, err := htmlOf(v)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, want := range []string{`<span class="tag open">today</span>`, "Nothing written today yet.", `<span class="date">Fri 16 Oct</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page of today lacks %s", want)
		}
	}
}

// TestAMoodHasAWord: the view's word for each mood from 1 to 5, as a number or as the text of an option; any other
// value has no word. The words are a view list, not a rule of the data.
func TestAMoodHasAWord(t *testing.T) {
	for in, want := range map[any]string{
		float64(1): "awful", float64(2): "bad", float64(3): "okay", float64(4): "good", float64(5): "great",
		"1": "awful", "5": "great", 3: "okay",
		float64(0): "", float64(6): "", float64(2.5): "", "": "", "good": "", nil: "",
	} {
		if got := moodWord(in); got != want {
			t.Errorf("moodWord(%v) = %q, want %q", in, got, want)
		}
	}
}
