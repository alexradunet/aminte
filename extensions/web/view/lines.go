package view

import (
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"aminte/text"
)

// The lines of the readings (docs/plans/093-words-phone-and-today.md): the line of a metric's recent readings on its
// card (sparkLine), and the chart of the readings of a metric (chart). The card is in metrics.go. A line draws only
// the readings that have a day and a number: a retraction has no number.
//
// One drawer, plot, places the readings of each line: across by the line's own placement of a day, and up between
// the least and the greatest value. sparkLine is a placement and one call of smallLine; chart is a placement, one
// call of plot, and its axes.

// lineReading is a reading that a line draws: its day, as a time and as written, and its value.
type lineReading struct {
	t     time.Time
	day   string
	value float64
}

// lineReadings are the readings of list that a line draws, in their order: those with a day and a number.
func lineReadings(list []any) []lineReading {
	var rs []lineReading
	for _, it := range list {
		m, _ := it.(map[string]any)
		day, _ := m["day"].(string)
		value, isNumber := m["value"].(float64)
		if t, ok := text.ParseDay(day); ok && isNumber { // no day, or no value (a retraction): nothing to draw
			rs = append(rs, lineReading{t, day, value})
		}
	}
	return rs
}

// plot places rs, which is not empty: x places a reading across, and y places a value up, given the least and the
// greatest value of rs. It gives the points of the SVG polyline, at (where a value stands up in this line), and the
// least and the greatest value.
func plot(rs []lineReading, x func(lineReading) float64, y func(v, lo, hi float64) float64) (points string, at func(float64) float64, lo, hi float64) {
	lo, hi = rs[0].value, rs[0].value
	for _, r := range rs[1:] {
		if r.value < lo {
			lo = r.value
		}
		if r.value > hi {
			hi = r.value
		}
	}
	at = func(v float64) float64 { return y(v, lo, hi) }
	var sb strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&sb, "%.1f,%.1f ", x(r), at(r.value))
	}
	return sb.String(), at, lo, hi
}

// share is where v stands between lo and hi: 0 at lo, 1 at hi, and 0 when hi is lo. A reading can be as large as a
// float64 holds (measurements_value_finite), so hi-lo can overflow (1e308 and -1e308): only then does share take
// the difference of the halves, which cannot overflow. The halves are not used for other values: the half of a value
// near zero is not exact, and the halves of 0 and 5e-324 are the same, so their difference is 0.
func share(v, lo, hi float64) float64 {
	if hi == lo {
		return 0
	}
	if d := hi - lo; !math.IsInf(d, 0) {
		return (v - lo) / d
	}
	return (v/2 - lo/2) / (hi/2 - lo/2)
}

// readingWords say a reading of a line for a screen reader: its value, its unit and its day against today on the
// clock c, "70 kg 2 days ago · Wed 14 Oct".
func readingWords(c clock, r lineReading, unit string) string {
	return Num(r.value) + " " + unitWords(unit) + " " + c.relative(r.day)
}

// smallLine draws rs as an inline SVG, 120 by 28, each reading placed across by x, with the values between their
// least and their greatest and the last reading marked; "" with no reading. label is what it says to a screen
// reader.
func smallLine(rs []lineReading, label string, x func(lineReading) float64) template.HTML {
	if len(rs) == 0 {
		return ""
	}
	points, y, _, _ := plot(rs, x, func(v, lo, hi float64) float64 {
		if hi == lo {
			return 14
		}
		return 24 - share(v, lo, hi)*20
	})
	last := rs[len(rs)-1]
	return template.HTML(fmt.Sprintf(`<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="%s"><polyline points="%s"/><circle cx="%.1f" cy="%.1f" r="2.5"/></svg>`,
		template.HTMLEscapeString(strings.Join(strings.Fields(label), " ")), points, x(last), y(last.value)))
}

// sparkLine draws a metric's recent readings as a small line: each point placed by its day between the first
// reading and the last, so a marker read twice a year is drawn as it was read. Its label says how many readings,
// and the first and the last value in words, with their days against today on the clock c.
func sparkLine(c clock, metric, unit string, points []any) template.HTML {
	rs := lineReadings(points)
	if len(rs) == 0 {
		return ""
	}
	first := rs[0]
	label := fmt.Sprintf("%s: %d readings, from %s to %s", metric, len(rs), readingWords(c, first, unit), readingWords(c, rs[len(rs)-1], unit))
	if len(rs) == 1 {
		label = metric + ": one reading, " + readingWords(c, first, unit)
	}
	span := rs[len(rs)-1].t.Sub(first.t).Hours()
	return smallLine(rs, label, func(r lineReading) float64 {
		if span > 0 {
			return 4 + r.t.Sub(first.t).Hours()/span*112
		}
		return 116 // one day only: the points stand at the end, where the latest is
	})
}

// chartPoints is how many readings the chart draws a point for: more points than this overlap on its 640 units and
// only add weight, so the line alone draws a longer series (docs/plans/100-one-page-a-metric.md).
const chartPoints = 200

// chart draws a series as an inline SVG: a line through its readings, each placed by its day between from and to,
// and a point a reading while there are at most chartPoints; "" with no reading to draw. The least and the greatest
// value of the axis are those of the readings that it draws. A value is written as Num writes it, as on the tile; a
// position in the SVG is a coordinate, not a value.
func chart(from, to string, readings any) template.HTML {
	rs := lineReadings(anyList(readings))
	// An absent lower bound is explicit all-history mode. Plot from the earliest
	// returned observation, without giving an ordinary calendar date sentinel meaning.
	if from == "" {
		for _, r := range rs {
			if from == "" || r.day < from {
				from = r.day
			}
		}
	}
	f, ok1 := text.ParseDay(from)
	t, ok2 := text.ParseDay(to)
	if len(rs) == 0 || !ok1 || !ok2 || t.Before(f) {
		return ""
	}
	const w, h, pad, left, bottom = 640.0, 200.0, 14.0, 48.0, 22.0
	x := func(r lineReading) float64 {
		// Unix seconds cover the full supported date range without duration saturation.
		across := 0.5
		if t.After(f) {
			across = float64(r.t.Unix()-f.Unix()) / float64(t.Unix()-f.Unix())
		}
		return left + across*(w-left-pad)
	}
	points, y, lo, hi := plot(rs, x, func(v, lo, hi float64) float64 {
		return pad + (1-share(v, lo, hi))*(h-2*pad-bottom)
	})
	var sb strings.Builder
	first, last := f.Format(layoutDateYear), t.Format(layoutDateYear) // the ends in words, with the year: a range can span years
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %g %g" role="img" aria-label="%s from %s to %s">`,
		w, h, Count(len(rs), "reading", "readings"), first, last)
	fmt.Fprintf(&sb, `<line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/><line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/>`,
		left, y(hi), w-pad, y(hi), left, y(lo), w-pad, y(lo))
	fmt.Fprintf(&sb, `<text x="%g" y="%g">%s</text><text x="%g" y="%g">%s</text>`, left-6, y(hi)+4, Num(hi), left-6, y(lo)+4, Num(lo))
	fmt.Fprintf(&sb, `<text class="x" x="%g" y="%g">%s</text><text class="x" x="%g" y="%g">%s</text>`,
		left+40, h-4, first, w-pad-40, h-4, last)
	sb.WriteString(`<polyline points="` + points + `"/>`)
	if len(rs) <= chartPoints {
		for _, r := range rs {
			fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3"><title>%s: %s</title></circle>`, x(r), y(r.value),
				r.t.Format(layoutDateYear), Num(r.value))
		}
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
