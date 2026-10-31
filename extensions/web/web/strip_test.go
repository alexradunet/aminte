package web_test

// The Today strip and the group On this day, from the data of a real file. The view tests hold the strip drawn from a
// hand-made shell. These hold what the real core gives to the shell: the count, the streak, the chips of the habits, and
// the way a chip leads to the form of the reading and back to the page it was on.

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var (
	// stripBoxRE is a Today strip.
	stripBoxRE = regexp.MustCompile(`(?s)<section class="rail-today".*?</section>`)
	// onThisDayRE is the group On this day of the margin of a day.
	onThisDayRE = regexp.MustCompile(`(?s)<section class="group" aria-label="On this day">.*?</section>`)
	// stripRecordFormRE is the form of a reading on Home: from its box up to the end of the form.
	stripRecordFormRE = regexp.MustCompile(`(?s)<div id="record">.*?</form>`)
	// stripFieldRE is an input of a form that a browser sends: a hidden, text, number or date input.
	stripFieldRE = regexp.MustCompile(`<input type="(?:hidden|text|number|date)" name="([^"]+)" value="([^"]*)"`)
	// stripSelectRE is a select of a form: group 1 is its name, group 2 is its options.
	stripSelectRE = regexp.MustCompile(`(?s)<select name="([^"]+)"[^>]*>(.*?)</select>`)
	// stripOptionRE is an option: group 1 is "selected" when it is chosen, group 3 is its value, group 4 is its words.
	stripOptionRE = regexp.MustCompile(`<option( selected)?( value="([^"]*)")?>([^<]*)</option>`)
)

// sentByTheBrowser is what a browser sends with a form: the path it posts to, and its inputs. A select sends the option
// that the owner has chosen (the one marked selected, else the first), as a browser does with no script. The argument
// is the HTML of the form, from its first tag up to its end.
func sentByTheBrowser(t *testing.T, form string) (string, url.Values) {
	t.Helper()
	action := regexp.MustCompile(`<form method="POST" action="([^"]+)"`).FindStringSubmatch(form)
	if action == nil {
		t.Fatalf("no form with a path to post to:\n%.800s", form)
	}
	sent := url.Values{}
	for _, in := range stripFieldRE.FindAllStringSubmatch(form, -1) {
		sent.Set(in[1], html.UnescapeString(in[2]))
	}
	for _, s := range stripSelectRE.FindAllStringSubmatch(form, -1) {
		options := stripOptionRE.FindAllStringSubmatch(s[2], -1)
		if len(options) == 0 {
			t.Fatalf("the select %s has no option", s[1])
		}
		chosen := options[0]
		for _, o := range options {
			if o[1] != "" {
				chosen = o
			}
		}
		value := chosen[3]
		if chosen[2] == "" { // no value attribute: the words are the value
			value = html.UnescapeString(chosen[4])
		}
		sent.Set(s[1], value)
	}
	return html.UnescapeString(action[1]), sent
}

// returnedPathOf is the address that a write returned the browser to: the Location without the receipt of the toast.
func returnedPathOf(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	q := u.Query()
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// TestTheTodayStripReadsTheLife: three days written in a row that end today, a mood today, and three chips (daily
// metrics with no unit): Walk done today, Stretch with a reading ten days ago and none today, and Smoke recorded 0
// today. A metric with a unit is no habit. The strip counts the entry, the mood and Walk: 3 of 5. The head of today
// shows the same streak.
func TestTheTodayStripReadsTheLife(t *testing.T) {
	l := emptyLife(t)
	for _, name := range []string{"Walk", "Stretch", "Smoke"} {
		l.do(t, "/", "register-metric", map[string]string{"name": name, "daily": "1"})
	}
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	for _, day := range []string{"2031-05-12", "2031-05-13", today} {
		l.do(t, "/days/"+day, "capture", map[string]string{"text": "Synthetic words."})
	}
	l.do(t, "/days/"+today, "capture", map[string]string{"mood": "4"})
	for _, r := range []struct{ metric, day, value string }{
		{"Walk", today, "1"}, {"Stretch", "2031-05-04", "1"}, {"Smoke", today, "0"}, {"Weight", today, "70"},
	} {
		l.do(t, "/days/"+r.day, "record", map[string]string{"metric": r.metric, "value": r.value})
	}

	body := pageAt(t, l.web, "/days/"+today)
	strip := stripBoxRE.FindString(body)
	for _, want := range []string{
		"3 of 5 done", "3 days in a row",
		`<a class="chip done" href="/days/today#capture">✓ entry</a>`, `<a class="chip done" href="/days/today#capture">✓ mood</a>`,
		`<span class="chip done">✓ Walk</span>`, `<a class="chip" href="/?back=%2Fdays%2F2031-05-14&amp;metric=Stretch&amp;new=record&amp;value=1#record">Stretch</a>`,
		`<span class="chip">Smoke · no</span>`,
	} {
		if !strings.Contains(strip, want) {
			t.Errorf("the strip lacks %s:\n%s", want, strip)
		}
	}
	if strings.Contains(strip, "Weight") {
		t.Errorf("a metric with a unit is in the strip:\n%s", strip)
	}
	if !strings.Contains(body, `<span class="tag open">today</span> <span class="tag">3 days in a row</span>`) {
		t.Errorf("the head of today does not show the streak:\n%.1500s", body)
	}
	// A day that is not today has the strip in its rail, and no streak tag in its head.
	if past := pageAt(t, l.web, "/days/2031-05-13"); !strings.Contains(past, "3 days in a row</div>") || strings.Contains(past, `<span class="tag">3 days in a row</span>`) {
		t.Errorf("the page of a past day: the rail must show the streak and the head must not")
	}
}

// TestTheStripIsAtTheHeadOfTodayOnAPhone: the strip is drawn in the More sheet and also in the page of today, in a block
// that the style sheet shows on a narrow screen only. Another day does not draw it in its page, and the entry has no
// autofocus.
func TestTheStripIsAtTheHeadOfTodayOnAPhone(t *testing.T) {
	l := emptyLife(t)
	todayPage := pageAt(t, l.web, "/days/"+today)
	if n := strings.Count(todayPage, `<section class="rail-today"`); n != 2 || !strings.Contains(todayPage, `<div class="today-inline"><section class="rail-today"`) {
		t.Errorf("the strip is drawn %d times, or not in the page of today", n)
	}
	if other := pageAt(t, l.web, "/days/2031-05-10"); strings.Count(other, `<section class="rail-today"`) != 1 || strings.Contains(other, `<div class="today-inline">`) {
		t.Error("another day draws the strip in its page")
	}
	if strings.Contains(todayPage, `accesskey="c" autofocus`) {
		t.Error("the entry of today takes the focus: a phone opens scrolled past the date")
	}
}

// TestTheLinkOfAHabitChipFillsTheRecordForm: the chip leads to Home with the form of the reading open, the habit chosen
// and the value 1 filled in, from the query. No other form is involved.
func TestTheLinkOfAHabitChipFillsTheRecordForm(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Walk"})
	l.do(t, "/days/2031-05-13", "record", map[string]string{"metric": "Walk", "value": "1"})
	form := stripRecordFormRE.FindString(pageAt(t, l.web, "/?new=record&metric=Walk&value=1"))
	if !strings.Contains(form, `<option selected>Walk</option>`) || !regexp.MustCompile(`name="value" value="1"`).MatchString(form) || !strings.Contains(form, "<details class=\"action\" open>") {
		t.Errorf("the form is not open and filled with Walk and 1:\n%s", form)
	}
}

// TestAHabitChipOnAPageThatAChipOpenedReturnsWithoutTheForm: a chip on a page that a chip opened (the form of the reading
// open and filled) goes back to that page without the form, so that the answer of the tick does not open the form
// again, and a back never holds another back.
func TestAHabitChipOnAPageThatAChipOpenedReturnsWithoutTheForm(t *testing.T) {
	l := emptyLife(t)
	for _, name := range []string{"Walk", "Run"} {
		l.do(t, "/", "register-metric", map[string]string{"name": name, "daily": "1"})
		l.do(t, "/days/2031-05-13", "record", map[string]string{"metric": name, "value": "1"})
	}
	strip := stripBoxRE.FindString(pageAt(t, l.web, "/?back=%2F&metric=Walk&new=record&value=1"))
	if want := `<a class="chip" href="/?back=%2F&amp;metric=Run&amp;new=record&amp;value=1#record">Run</a>`; !strings.Contains(strip, want) {
		t.Errorf("the chip of Run does not go back to Home without the form, want %s:\n%s", want, strip)
	}
}

// TestTheChipOfAHabitReturnsToThePageItWasOn: the owner follows the chip, sends the form that it fills, and the write
// returns to the page whose rail drew the chip. The reading is there afterwards: the strip of the page counts the habit
// as done.
func TestTheChipOfAHabitReturnsToThePageItWasOn(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Stretch", "daily": "1"})
	l.do(t, "/days/2031-05-11", "record", map[string]string{"metric": "Stretch", "value": "1"})
	page := "/days/2031-05-13"
	chip := regexp.MustCompile(`<a class="chip" href="([^"]+)">Stretch</a>`).FindStringSubmatch(pageAt(t, l.web, page))
	if chip == nil {
		t.Fatal("no chip for Stretch")
	}
	link, err := url.Parse(html.UnescapeString(chip[1]))
	if err != nil {
		t.Fatal(err)
	}
	form := stripRecordFormRE.FindString(pageAt(t, l.web, link.RequestURI()))
	if form == "" {
		t.Fatal("the page of the chip has no form of a reading")
	}
	action, sent := sentByTheBrowser(t, form)
	if sent.Get("metric") != "Stretch" || sent.Get("value") != "1" {
		t.Fatalf("the form sends %v", sent)
	}
	rec := postForm(l.web, action, sent)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d, want 303", action, rec.Code)
	}
	if got := returnedPathOf(t, rec.Header().Get("Location")); got != page {
		t.Errorf("returned to %q, want %q", got, page)
	}
	if after := stripBoxRE.FindString(pageAt(t, l.web, page)); !strings.Contains(after, `<span class="chip done">✓ Stretch</span>`) {
		t.Errorf("the strip does not count Stretch as done after the write:\n%s", after)
	}
}

// TestOnThisDayListsTheSameDayOfEarlierYears: the margin of a day has a group with its day pages of the years before,
// the nearest year first, while a page exists. The group is on every day, not only on today, and it is absent when no
// page exists.
func TestOnThisDayListsTheSameDayOfEarlierYears(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/days/"+today, "capture", map[string]string{"text": "Today, with no earlier year."})
	if g := onThisDayRE.FindString(pageAt(t, l.web, "/days/"+today)); g != "" { // the page of the day itself is no earlier year
		t.Errorf("a day with no earlier year has a group:\n%s", g)
	}
	l.do(t, "/days/2030-05-14", "capture", map[string]string{"text": "A year ago.", "mood": "5"})
	l.do(t, "/days/2029-05-14", "capture", map[string]string{"text": "Two years ago."})
	l.do(t, "/days/2027-05-14", "capture", map[string]string{"text": "Four years ago, after a gap."})
	g := onThisDayRE.FindString(pageAt(t, l.web, "/days/"+today))
	if g == "" {
		t.Fatal("no group on today")
	}
	for _, want := range []string{`href="/days/2030-05-14"`, `A year ago.`, `mood 5`, `href="/days/2029-05-14"`, `Two years ago.`} {
		if !strings.Contains(g, want) {
			t.Errorf("the group lacks %s:\n%s", want, g)
		}
	}
	if strings.Contains(g, "2027-05-14") || strings.Index(g, "2030-05-14") > strings.Index(g, "2029-05-14") {
		t.Errorf("the list goes past a missing year, or the nearest year is not first:\n%s", g)
	}
	if strings.Contains(g, "2031-05-14") {
		t.Errorf("the group of today lists today:\n%s", g)
	}
	// A past day has its own earlier years, and its own day is left out.
	if g := onThisDayRE.FindString(pageAt(t, l.web, "/days/2030-05-14")); !strings.Contains(g, `href="/days/2029-05-14"`) || strings.Contains(g, "2030-05-14") {
		t.Errorf("the group of 2030-05-14:\n%s", g)
	}
}

// TestOnThisDayStopsAtFiveYears: at most five earlier years.
func TestOnThisDayStopsAtFiveYears(t *testing.T) {
	l := emptyLife(t)
	for year := 2025; year <= 2030; year++ {
		l.do(t, fmt.Sprintf("/days/%d-05-14", year), "capture", map[string]string{"text": "Synthetic words."})
	}
	g := onThisDayRE.FindString(pageAt(t, l.web, "/days/"+today))
	if n := strings.Count(g, `class="entry`); n != 5 || strings.Contains(g, "2025-05-14") {
		t.Errorf("%d entries, want the five nearest years (2026 to 2030):\n%s", n, g)
	}
}
