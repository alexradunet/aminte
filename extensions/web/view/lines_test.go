package view

import (
	"regexp"
	"strings"
	"testing"
)

// chartAxis finds the two value labels of a chart's axis: the greatest value, then the least.
var chartAxis = regexp.MustCompile(`<text x="42" y="[^"]*">([^<]*)</text><text x="42" y="[^"]*">([^<]*)</text>`)

// TestALineDrawsOnlyTheReadingsWithADayAndANumber: a reading with no day, or with no number (a retraction is null
// in the JSON), is not drawn. The least and the greatest value of a line come from the readings that are drawn,
// also when the first reading of the list is not drawn. 12 October 2026 is a Monday, 14 October a Wednesday.
func TestALineDrawsOnlyTheReadingsWithADayAndANumber(t *testing.T) {
	c := clock{today: "2026-10-16"}
	for name, first := range map[string]map[string]any{
		"no day":   {"day": "not a day", "value": 5.0},
		"null":     {"day": "2026-10-12", "value": nil},
		"no value": {"day": "2026-10-12"},
		"a text":   {"day": "2026-10-12", "value": "75"},
	} {
		readings := []any{first, map[string]any{"day": "2026-10-14", "value": 70.0}, map[string]any{"day": "2026-10-16", "value": 80.0}}

		got := string(chart("2026-10-12", "2026-10-16", readings))
		if m := chartAxis.FindStringSubmatch(got); m == nil || m[1] != "80" || m[2] != "70" {
			t.Errorf("%s first: the chart's axis is %q, want 80 and 70:\n%s", name, m, got)
		}
		if !strings.Contains(got, `aria-label="2 readings from Mon 12 Oct 2026 to Fri 16 Oct 2026"`) {
			t.Errorf("%s first: the chart does not count 2 readings:\n%s", name, got)
		}

		got = string(sparkLine(c, "weight", "kg", readings))
		if want := `<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight: 2 readings, from 70 kg 2 days ago · Wed 14 Oct to 80 kg today"><polyline points="4.0,24.0 116.0,4.0 "/><circle cx="116.0" cy="4.0" r="2.5"/></svg>`; got != want {
			t.Errorf("%s first: the spark line is\n%s\nwant\n%s", name, got, want)
		}
	}

	none := []any{map[string]any{"day": "not a day", "value": 1.0}, map[string]any{"day": "2026-10-14", "value": nil}}
	if got := chart("2026-10-12", "2026-10-16", none); got != "" {
		t.Errorf("a chart with no reading to draw is drawn:\n%s", got)
	}
	if got := sparkLine(c, "weight", "kg", none); got != "" {
		t.Errorf("a spark line with no reading to draw is drawn:\n%s", got)
	}
}

// TestAChartSaysHowManyReadingsItDraws: the label of a chart says "1 reading" for one reading and "2 readings" for two.
func TestAChartSaysHowManyReadingsItDraws(t *testing.T) {
	one := map[string]any{"day": "2026-10-12", "value": 70.0}
	two := map[string]any{"day": "2026-10-14", "value": 71.0}
	for _, c := range []struct {
		readings []any
		want     string
	}{
		{[]any{one}, `aria-label="1 reading from Mon 12 Oct 2026 to Fri 16 Oct 2026"`},
		{[]any{one, two}, `aria-label="2 readings from Mon 12 Oct 2026 to Fri 16 Oct 2026"`},
	} {
		if got := string(chart("2026-10-12", "2026-10-16", c.readings)); !strings.Contains(got, c.want) {
			t.Errorf("the chart of %v does not say %s:\n%s", c.readings, c.want, got)
		}
	}
}

// TestAChartWritesItsNumbersInFull: the axis and the title of each point write a value as Num does, as the tile and
// every other place write it: 1000000, never 1e+06, and 0.00001, never 1e-05.
func TestAChartWritesItsNumbersInFull(t *testing.T) {
	readings := []any{map[string]any{"day": "2026-10-12", "value": 0.00001}, map[string]any{"day": "2026-10-14", "value": 1000000.0}}
	got := string(chart("2026-10-12", "2026-10-16", readings))
	if m := chartAxis.FindStringSubmatch(got); m == nil || m[1] != "1000000" || m[2] != "0.00001" {
		t.Errorf("the chart's axis is %q, want 1000000 and 0.00001:\n%s", m, got)
	}
	for _, want := range []string{"<title>Mon 12 Oct 2026: 0.00001</title>", "<title>Wed 14 Oct 2026: 1000000</title>"} {
		if !strings.Contains(got, want) {
			t.Errorf("the chart has no %s:\n%s", want, got)
		}
	}
}

// TestTheWordsOfASmallLine: the label of a spark line, with one reading and with more, says each value with its unit
// in words and its day against today on the clock: here today is 16 October.
func TestTheWordsOfASmallLine(t *testing.T) {
	c := clock{today: "2026-10-16"}
	r := func(day string, value float64) any { return map[string]any{"day": day, "value": value} }
	for _, tc := range []struct {
		name, got, want string
	}{
		{"a spark of one reading", string(sparkLine(c, "ferritin", "ug/L", []any{r("2025-04-12", 85)})),
			`<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="ferritin: one reading, 85 ug/L Sat 12 Apr 2025"><polyline points="116.0,14.0 "/><circle cx="116.0" cy="14.0" r="2.5"/></svg>`},
		{"a spark of three readings", string(sparkLine(c, "weight", "kg", []any{r("2026-10-06", 72), r("2026-10-11", 70), r("2026-10-16", 71)})),
			`<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight: 3 readings, from 72 kg Tue 6 Oct to 71 kg today"><polyline points="4.0,4.0 60.0,24.0 116.0,14.0 "/><circle cx="116.0" cy="14.0" r="2.5"/></svg>`},
		{"a spark of one day", string(sparkLine(c, "weight", "kg", []any{r("2026-10-16", 70), r("2026-10-16", 71)})),
			`<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight: 2 readings, from 70 kg today to 71 kg today"><polyline points="116.0,24.0 116.0,4.0 "/><circle cx="116.0" cy="4.0" r="2.5"/></svg>`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s:\n%s\nwant\n%s", tc.name, tc.got, tc.want)
		}
	}
}

// TestAChartTakesItsRangeFromTheReadingsItDraws: the axis of a chart is the greatest and the least value of the
// readings it draws, in whatever order they come; a reading that is not drawn is not in it. With no lower bound
// (all history), the chart starts at the earliest reading it draws; with one day only, the point stands in the middle.
func TestAChartTakesItsRangeFromTheReadingsItDraws(t *testing.T) {
	r := func(day string, value any) any { return map[string]any{"day": day, "value": value} }
	gone := r("2026-10-12", nil) // a retraction: not drawn
	for _, c := range []struct {
		name, from string
		readings   []any
		hi, lo     string
		want       string // a part of the chart that only these ends give
	}{
		{"the greatest first", "2026-10-12", []any{gone, r("2026-10-14", 80.0), r("2026-10-16", 70.0)}, "80", "70",
			`<polyline points="337.0,14.0 626.0,164.0 "/>`},
		{"the least first", "2026-10-12", []any{gone, r("2026-10-14", 70.0), r("2026-10-16", 80.0)}, "80", "70",
			`<polyline points="337.0,164.0 626.0,14.0 "/>`},
		{"the ends in the middle", "2026-10-12", []any{r("2026-10-12", 75.0), r("2026-10-14", 80.0), r("2026-10-15", 70.0)}, "80", "70",
			`<polyline points="48.0,89.0 337.0,14.0 481.5,164.0 "/>`},
		{"one value", "2026-10-12", []any{r("2026-10-14", 70.0)}, "70", "70", `<polyline points="337.0,164.0 "/>`},
		{"all history", "", []any{r("2026-10-14", 80.0), r("2026-10-12", 70.0)}, "80", "70",
			`aria-label="2 readings from Mon 12 Oct 2026 to Fri 16 Oct 2026"><line class="axis" x1="48" y1="14" x2="626" y2="14"/>`},
		{"all history, one day", "", []any{r("2026-10-16", 70.0)}, "70", "70", `<polyline points="337.0,164.0 "/>`},
	} {
		got := string(chart(c.from, "2026-10-16", c.readings))
		if m := chartAxis.FindStringSubmatch(got); m == nil || m[1] != c.hi || m[2] != c.lo {
			t.Errorf("%s: the axis is %q, want %s and %s:\n%s", c.name, m, c.hi, c.lo, got)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the chart has no %s:\n%s", c.name, c.want, got)
		}
	}
	for _, c := range []struct{ name, from, to string }{
		{"to before from", "2026-10-16", "2026-10-12"}, {"from not a day", "x", "2026-10-16"}, {"to not a day", "2026-10-12", ""},
	} {
		if got := chart(c.from, c.to, []any{r("2026-10-14", 70.0)}); got != "" {
			t.Errorf("%s: a chart is drawn:\n%s", c.name, got)
		}
	}
	if got := chart("", "2026-10-16", []any{gone}); got != "" {
		t.Errorf("all history with no reading to draw: a chart is drawn:\n%s", got)
	}
}

// TestTheChartSaysItsEndsInWords: the ends of a series' chart are dates in words with their year, on the axis and
// for a reader, not ISO days (docs/plans/095-the-leaf-and-its-margin.md). 14 July 2026 is a Tuesday: 12 October 2026
// is a Monday, 90 days later.
func TestTheChartSaysItsEndsInWords(t *testing.T) {
	got := string(chart("2026-07-14", "2026-10-12", []any{
		map[string]any{"day": "2026-09-29", "value": 78.4}, map[string]any{"day": "2026-10-12", "value": 77.1}}))
	for _, want := range []string{`aria-label="2 readings from Tue 14 Jul 2026 to Mon 12 Oct 2026"`, `>Tue 14 Jul 2026</text>`, `>Mon 12 Oct 2026</text>`} {
		if !strings.Contains(got, want) {
			t.Errorf("the chart lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "2026-07-14") || strings.Contains(got, "2026-10-12") {
		t.Errorf("the chart shows an ISO day:\n%s", got)
	}
}

// TestALineOfReadingsNearTheLimitHasItsPlaces: a reading can be as large as a float64 holds
// (measurements_value_finite), so the difference of the greatest and the least value of a line can overflow. The
// readings 1e308 and -1e308 stand at the top and at the bottom of each line, as two readings of 80 and 70 do: no
// place is NaN. A reading can also be as small as a float64 holds: the readings 0 and 5e-324, the least float64 more
// than 0, stand at the bottom and at the top of each line, also with no NaN. The top of a small line is 4 and its
// bottom 24; the top of a chart is 14 and its bottom 164.
func TestALineOfReadingsNearTheLimitHasItsPlaces(t *testing.T) {
	c := clock{today: "2026-10-16"}
	for _, line := range []struct {
		first, second      float64
		spark, chartPoints string
	}{
		{1e308, -1e308, "4.0,4.0 116.0,24.0", "48.0,14.0 337.0,164.0"},
		{0, 5e-324, "4.0,24.0 116.0,4.0", "48.0,164.0 337.0,14.0"},
	} {
		rs := []any{map[string]any{"day": "2026-10-12", "value": line.first}, map[string]any{"day": "2026-10-14", "value": line.second}}
		for _, tc := range []struct{ name, got, want string }{
			{"a spark", string(sparkLine(c, "x", "m", rs)), `<polyline points="` + line.spark + ` "/>`},
			{"a chart", string(chart("2026-10-12", "2026-10-16", rs)), `<polyline points="` + line.chartPoints + ` "/>`},
		} {
			if !strings.Contains(tc.got, tc.want) || strings.Contains(tc.got, "NaN") {
				t.Errorf("%s of %g and %g: want %s and no NaN:\n%s", tc.name, line.first, line.second, tc.want, tc.got)
			}
		}
	}
}
