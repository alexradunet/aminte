package view

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"lifelog/siren"
	"lifelog/text"
)

// The browser's words for a value (docs/plans/093-words-phone-and-today.md): a value is drawn by its type, never by
// the name of its property. A day is "Mon 5 Oct", and near today "yesterday · Sun 11 Oct"; an instant is said in the
// clock of the server's day, "today, 09:48", with the exact instant kept in its <time> element.

// clock is what words need: the server's local day and its zone. A page drawn before the handler has neither: its
// days carry their year and its times are in UTC.
type clock struct {
	today string
	zone  *time.Location
}

// sameYear reports a time in the year of today; with no today, never, so the year is always written.
func (c clock) sameYear(t time.Time) bool {
	today, ok := text.ParseDay(c.today)
	return ok && today.Year() == t.Year()
}

// date is a day as a date: "Mon 5 Oct", with the year when it is not this year: "Sun 1 Nov 2026".
func (c clock) date(t time.Time) string {
	if c.sameYear(t) {
		return t.Format(layoutDate)
	}
	return t.Format(layoutDateYear)
}

// day says a day as a date; a value that is not a day is returned as it is.
func (c clock) day(s string) string {
	t, ok := text.ParseDay(s)
	if !ok {
		return s
	}
	return c.date(t)
}

// relative says a day against today: "today", "yesterday · Sun 11 Oct", "tomorrow · Tue 13 Oct",
// "4 days ago · Thu 8 Oct", "in 3 days · Thu 15 Oct" within 7 days, else the date.
func (c clock) relative(s string) string {
	d, ok := text.ParseDay(s)
	if !ok {
		return s
	}
	today, ok := text.ParseDay(c.today)
	if !ok {
		return c.date(d)
	}
	n := int(d.Sub(today).Hours() / 24)
	date := c.date(d)
	switch {
	case n == 0:
		return "today"
	case n == 1:
		return "tomorrow · " + date
	case n == -1:
		return "yesterday · " + date
	case n > 1 && n <= 7:
		return fmt.Sprintf("in %d days · %s", n, date)
	case n < -1 && n >= -7:
		return fmt.Sprintf("%d days ago · %s", -n, date)
	}
	return date
}

// instant says an instant in the server's zone: "today, 09:48", "yesterday, 21:14", "Mon 12 Oct, 09:48"; with no
// zone, in UTC: "Mon 12 Oct 2026, 06:48 UTC". A value that is not an instant is returned as it is.
func (c clock) instant(s string) string {
	if !text.IsInstant(s) {
		return s
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	clockTime, zone := "15:04", ""
	if c.zone == nil {
		t, zone = t.UTC(), " UTC"
	} else {
		t = t.In(c.zone)
	}
	at := t.Format(clockTime) + zone
	switch rel := c.relative(t.Format(time.DateOnly)); {
	case rel == "today":
		return "today, " + at
	case strings.HasPrefix(rel, "yesterday"):
		return "yesterday, " + at
	}
	return c.date(t) + ", " + at
}

// timeHTML is a day or an instant in words inside its <time> element, which keeps the exact value; anything else is
// its text.
func (c clock) timeHTML(s string) template.HTML {
	esc := template.HTMLEscapeString
	switch {
	case text.IsInstant(s):
		return template.HTML(`<time datetime="` + esc(s) + `" title="` + esc(s) + `">` + esc(c.instant(s)) + `</time>`)
	case text.IsDay(s):
		return template.HTML(`<time datetime="` + esc(s) + `">` + esc(c.relative(s)) + `</time>`)
	}
	return template.HTML(esc(s))
}

// nameWords is a property's name in words: "created_at" is "created at".
func nameWords(name string) string { return strings.ReplaceAll(name, "_", " ") }

// moodWord is the word for a mood from 1 to 5, or "" for any other value. The value is a number or the text of an
// option. The words are a list for the view, not a rule of the data: the mood is a number from 1 to 5 (D6).
func moodWord(v any) string {
	switch Num(v) {
	case "1":
		return "awful"
	case "2":
		return "bad"
	case "3":
		return "okay"
	case "4":
		return "good"
	case "5":
		return "great"
	}
	return ""
}

// Count says a number and its noun, in the form for that number: "0 readings", "1 reading", "3 more pages". The
// number is a JSON number (a float64) or a length (an int), written in full (Num); any other value is 0. A site that
// has its own words for 0 ("no recordings") says them before it calls Count.
func Count(n any, one, many string) string {
	f, _ := n.(float64)
	if i, ok := n.(int); ok {
		f = float64(i)
	}
	if f == 1 {
		return "1 " + one
	}
	return Num(f) + " " + many
}

// writeValue draws any value by its type (the diagram of plan 093): an object as facts, a list of objects as a table,
// a list as a list, a day or an instant in words, a boolean as yes or no, a long text as preformatted text.
func (c clock) writeValue(sb *strings.Builder, x any) {
	esc := template.HTMLEscapeString
	switch x := x.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteString(`<dl class="facts">`)
		for _, k := range keys {
			sb.WriteString("<dt>" + esc(nameWords(k)) + "</dt><dd>")
			c.writeValue(sb, x[k])
			sb.WriteString("</dd>")
		}
		sb.WriteString("</dl>")
	case []any:
		if len(x) == 0 {
			sb.WriteString(`<small>none</small>`)
			return
		}
		if _, ok := x[0].(map[string]any); ok {
			var cols []string
			seen := map[string]bool{}
			for _, row := range x {
				if m, ok := row.(map[string]any); ok {
					for k := range m {
						if !seen[k] {
							cols = append(cols, k)
							seen[k] = true
						}
					}
				}
			}
			sort.Strings(cols)
			sb.WriteString("<table><thead><tr>")
			for _, col := range cols {
				sb.WriteString(`<th scope="col">` + esc(nameWords(col)) + "</th>")
			}
			sb.WriteString("</tr></thead><tbody>")
			for _, row := range x {
				m, _ := row.(map[string]any)
				sb.WriteString("<tr>")
				for _, col := range cols {
					sb.WriteString("<td>")
					c.writeValue(sb, m[col])
					sb.WriteString("</td>")
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</tbody></table>")
			return
		}
		sb.WriteString("<ul>")
		for _, it := range x {
			sb.WriteString("<li>")
			c.writeValue(sb, it)
			sb.WriteString("</li>")
		}
		sb.WriteString("</ul>")
	case string:
		switch {
		case text.IsInstant(x) || text.IsDay(x):
			sb.WriteString(string(c.timeHTML(x)))
		case strings.Contains(x, "\n") || len(x) > 80:
			sb.WriteString("<pre>" + esc(x) + "</pre>")
		default:
			sb.WriteString(esc(x))
		}
	case bool:
		if x {
			sb.WriteString("yes")
		} else {
			sb.WriteString("no")
		}
	case float64:
		sb.WriteString(siren.Num(x))
	case nil:
		sb.WriteString(`<small>—</small>`)
	default:
		sb.WriteString(esc(fmt.Sprint(x)))
	}
}

// clock is the clock of the view's words: the shell's today and zone.
func (v *view) clock() clock {
	if v.Shell == nil {
		return clock{}
	}
	return clock{today: v.Shell.Today, zone: v.Shell.Zone}
}

// Today is the day that the server reads as today for this page: the today of the view's words and of the sidebar,
// "" for a page with no shell. A template compares a day with today through it. The property today of a resource is
// for a JSON client; a template reads it only as data (a link, the day of a form).
func (v *view) Today() string { return v.clock().today }

// When is a day or an instant of the entity in words, in its <time> element.
func (v *view) When(x any) template.HTML {
	s, _ := x.(string)
	return v.clock().timeHTML(s)
}

// Date is a day as a date, "Mon 5 Oct".
func (v *view) Date(x any) string {
	s, _ := x.(string)
	return v.clock().day(s)
}

// LongDate is a day in full words, "Wednesday 28 October", with its year when it is not this year's: the date
// above the first turn of the chat screen.
func (v *view) LongDate(x any) string {
	s, _ := x.(string)
	t, ok := text.ParseDay(s)
	if !ok {
		return s
	}
	if v.clock().sameYear(t) {
		return t.Format("Monday 2 January")
	}
	return t.Format("Monday 2 January 2006")
}

// Value draws any value of the entity by its type.
func (v *view) Value(x any) template.HTML {
	var sb strings.Builder
	v.clock().writeValue(&sb, x)
	return template.HTML(sb.String())
}

// The layouts of a day in words.
const (
	layoutDate     = "Mon 2 Jan"
	layoutDateYear = "Mon 2 Jan 2006"
)
