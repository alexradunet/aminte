package web_test

// The pages of a page, of a day and of the stream of days. Web draws them from the answers of the real core.
// Each test holds the markup that a test of the core held while the core drew the browser.
// The strings are the strings of that test, because web draws the same bytes.
// Each test makes its data through the API of the core, as the owner and an agent would make it.

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog-web/internal/formdoc"
)

// browserPageAt returns the body of GET path through h, as a browser asks for it.
// The test fails unless the answer is a whole page with the status 200.
func browserPageAt(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := ask(h, http.MethodGet, path)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("GET %s: %d, want 200 and a whole page: %.300s", path, rec.Code, body)
	}
	return body
}

// formTextPostingTo returns the HTML of the form of page that posts to action.
// The test fails when the page has no such form.
func formTextPostingTo(t *testing.T, page, action string) string {
	t.Helper()
	for _, m := range regexp.MustCompile(`(?s)<form method="POST" action="([^"]+)"[^>]*>.*?</form>`).FindAllStringSubmatch(page, -1) {
		if html.UnescapeString(m[1]) == action {
			return m[0]
		}
	}
	t.Fatalf("no form posts to %s", action)
	return ""
}

// formSentWith returns the address of a form and the fields that a browser sends with it.
// The form is the form of page whose start tag holds mark (id="pick", or the address of the form).
// The fields are the fields that the form holds (internal/formdoc), with the values of typed in place of them.
// Each name of typed must be a control of the form, or a control outside it that joins it by its id (form="pick").
// If not, the test fails: a form that draws no control of a field cannot send that field.
func formSentWith(t *testing.T, page, mark string, typed url.Values) (action string, sent url.Values) {
	t.Helper()
	m := regexp.MustCompile(`(?s)(<form\b[^>]*` + regexp.QuoteMeta(mark) + `[^>]*>)(.*?)</form>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no form holds %s", mark)
	}
	forms := formdoc.Forms(m[0])
	if len(forms) != 1 {
		t.Fatalf("%d forms hold %s, want one", len(forms), mark)
	}
	controls := map[string]bool{}
	for _, n := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(m[2], -1) {
		controls[n[1]] = true
	}
	if id := regexp.MustCompile(`\sid="([^"]+)"`).FindStringSubmatch(m[1]); id != nil {
		for _, n := range regexp.MustCompile(`name="([^"]+)"[^>]*\sform="`+regexp.QuoteMeta(id[1])+`"`).FindAllStringSubmatch(page, -1) {
			controls[n[1]] = true
		}
	}
	sent = url.Values{}
	for name, values := range forms[0].Values {
		sent[name] = append([]string(nil), values...)
	}
	for name, values := range typed {
		if !controls[name] {
			t.Fatalf("the form %s has no control %s", mark, name)
		}
		sent[name] = values
	}
	return forms[0].Action, sent
}

// dayFromToday returns the day that is n days from today of the tests.
func dayFromToday(n int) string {
	d, err := time.Parse(time.DateOnly, today)
	if err != nil {
		panic(err)
	}
	return d.AddDate(0, 0, n).Format(time.DateOnly)
}

// lifeWithAJournal is a life around today.
// The day two days ago has a line. The day before today has a mood and no line.
// Today has a line and the moods 4 and 3.
func lifeWithAJournal(t *testing.T) *life {
	t.Helper()
	l := emptyLife(t)
	l.do(t, "/days/"+dayFromToday(-2), "capture", map[string]string{"text": "Morning run\nRan with [[Sam]] by the river."})
	l.do(t, "/days/"+dayFromToday(-1), "capture", map[string]string{"mood": "2"})
	l.do(t, "/days/"+today, "capture", map[string]string{"text": "Quiet day.", "mood": "4"})
	l.do(t, "/days/"+today, "capture", map[string]string{"mood": "3"})
	return l
}

// Core test: TestAnEmptyPageSaysSoWithItsEditorClosed.
// A page with no text says so in words, as every empty view does.
// Its editor is closed under that sentence, so the page does not open as a large empty box with the focus in it.
func TestAnEmptyPageSaysSoWithItsEditorClosed(t *testing.T) {
	l := emptyLife(t)
	sam := l.do(t, "/", "create-person", map[string]string{"title": "Sam"})
	body := browserPageAt(t, l.web, href(sam, "self"))
	if !strings.Contains(body, `<p class="empty">Nothing written here yet.</p>`) {
		t.Error("an empty page does not say that nothing is written")
	}
	if !strings.Contains(body, `<details class="body-editor"><summary>Edit</summary>`) || strings.Contains(body, `<details class="body-editor" open><summary>Edit</summary>`) {
		t.Error("an empty page opens its editor")
	}
	// One autofocus is the title field of the popover of the pencil. A browser puts the focus there when the popover opens.
	// The other autofocus is the Cancel button of the danger dialog.
	if strings.Count(body, "autofocus") != 2 || !strings.Contains(body, `name="title" value="Sam" required autofocus`) {
		t.Error("an empty page puts the focus in its editor")
	}
}

// Core test: TestTheEditorIsTallAsItsBodyAndFirst.
// The page draws its editor before its body.
func TestAPageDrawsItsEditorBeforeItsBody(t *testing.T) {
	l := emptyLife(t)
	p := l.do(t, "/", "create-page", map[string]string{"title": "Long", "body": "words"})
	body := browserPageAt(t, l.web, href(p, "self"))
	if e, b := strings.Index(body, "<summary>Edit</summary>"), strings.Index(body, `<div class="prose">`); e < 0 || b < 0 || e > b {
		t.Errorf("the editor is not before the body: %d, %d", e, b)
	}
}

// Core test: TestTheBrowserMarksTheBodyOfASaveRequired.
// The catalog marks the field body of save-body as required, because the core refuses an empty body.
// So the browser marks the text area required, and it does not send an empty body.
func TestTheBrowserMarksTheBodyOfASaveRequired(t *testing.T) {
	l := emptyLife(t)
	p := l.do(t, "/", "create-page", map[string]string{"title": "Notes", "body": "words"})
	area := regexp.MustCompile(`<textarea name="body"[^>]*>`).FindString(browserPageAt(t, l.web, href(p, "self")))
	if area != `<textarea name="body" rows="12" required>` {
		t.Errorf("the text area of save-body: %q, want it required", area)
	}
}

// Core test: TestADestructiveActionDrawsADangerButton.
// The warning comes from the catalog, not from a template that knows the name of an action.
// A tombstone is drawn as a danger. The revive that undoes it is not.
func TestADestructiveActionDrawsADangerButton(t *testing.T) {
	l := emptyLife(t)
	p := l.do(t, "/", "create-page", map[string]string{"title": "Draft"})
	if body := browserPageAt(t, l.web, href(p, "self")); !strings.Contains(body, `<button class="danger">Delete</button>`) {
		t.Errorf("a page that offers a tombstone draws no danger button: %.400s", body)
	}
	l.do(t, href(p, "self"), "tombstone-pages", nil)
	body := browserPageAt(t, l.web, href(p, "self"))
	if !strings.Contains(body, "<button>Restore</button>") {
		t.Errorf("a deleted page offers no plain Restore: %.400s", body)
	}
	if strings.Contains(body, `<button class="danger">`) {
		t.Error("a deleted page draws a danger button for the revive that restores it")
	}
}

// Core test: TestTheDayCarriesItsMood.
// The day shows its moods in one span and offers the edit of its page.
// The table of its readings has no column for the scope.
func TestTheDayShowsItsMoodsAndTheEditOfItsPage(t *testing.T) {
	l := lifeWithAJournal(t)
	body := browserPageAt(t, l.web, "/days/"+today)
	for _, want := range []string{`<span class="mood" title="Mood">mood 4, 3</span>`, "<summary>Edit the day page</summary>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the day view lacks %q", want)
		}
	}
	if strings.Contains(body, `<th scope="col">scope</th>`) {
		t.Error("the readings show a scope column")
	}
}

// Core test: TestTheRootIsTodayAtAGlance.
// Home is the page of today: GET / answers with the day of today.
// A browser asks with either of its two usual Accept headers. Its capture form posts to the day.
func TestHomeIsThePageOfToday(t *testing.T) {
	l := lifeWithAJournal(t)
	for _, accept := range []string{"text/html,application/xhtml+xml", "text/html"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		l.web.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `<span class="leaf-day">14</span>`) || !strings.Contains(body, `<section class="capture"`) {
			t.Errorf("GET / as %q: %d, want the page of today: %.300s", accept, rec.Code, body)
		}
		if _, ok := formdoc.Find(formdoc.Forms(body), func(f formdoc.Form) bool { return f.Action == "/days/"+today+"/capture" }); !ok {
			t.Errorf("GET / as %q: no form captures on the day of today", accept)
		}
	}
}

// Core test: TestTheDayOfTodayCarriesTheLastEntries.
// The list of the last entries is in the margin of today, and of no other day.
func TestTheDayOfTodayCarriesTheLastEntries(t *testing.T) {
	l := lifeWithAJournal(t)
	body := browserPageAt(t, l.web, "/days/"+today)
	for _, want := range []string{`aria-label="Last entries"`, "<h2>Last entries</h2>", `<a href="/days">Every day</a>`,
		`<a class="entry" href="/days/` + dayFromToday(-2) + `">`, "Morning"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page of today lacks %q", want)
		}
	}
	if strings.Contains(browserPageAt(t, l.web, "/days/"+dayFromToday(-1)), "Last entries") {
		t.Error("a day that is not today draws Last entries")
	}
}

// Core test: TestTheMoodIsAChoiceOfFive.
// The browser draws the mood of the capture form as a row of radio buttons. It works with no script.
// The mood is not a list and is not required. A day with one mood says its word.
// A capture that a browser sends with a mood reaches the day.
func TestTheMoodIsARowOfFaces(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/days/"+today, "capture", map[string]string{"text": "No mood today."})
	l.do(t, "/days/"+today, "capture", map[string]string{"mood": "4"})
	l.do(t, "/days/"+dayFromToday(-1), "capture", map[string]string{"mood": "2"})

	body := browserPageAt(t, l.web, "/days/"+today)
	form := formTextPostingTo(t, body, "/days/"+today+"/capture")
	for _, want := range []string{`<fieldset class="segmented faces"><legend>Mood</legend>`,
		`<label title="awful"><input type="radio" name="mood" value="1"><span>1 <small>awful</small></span></label>`,
		`<label title="okay"><input type="radio" name="mood" value="3"><span>3 <small>okay</small></span></label>`,
		`<label title="great"><input type="radio" name="mood" value="5"><span>5 <small>great</small></span></label>`, `<input type="hidden" name="back" value="/days/` + today + `">`} {
		if !strings.Contains(form, want) {
			t.Errorf("the day's Capture lacks %s:\n%s", want, form)
		}
	}
	if strings.Contains(form, `<select name="mood"`) || strings.Contains(form, " required") {
		t.Errorf("the mood is a list, or is required:\n%s", form)
	}
	if one := browserPageAt(t, l.web, "/days/"+dayFromToday(-1)); !strings.Contains(one, `<span class="mood" title="Mood">mood 2 · bad</span>`) {
		t.Errorf("a day with one mood does not say its word:\n%s", one)
	}
	rec := postForm(l.web, "/days/"+today+"/capture", url.Values{"text": {"From the browser."}, "mood": {"3"}, "back": {"/days/" + today}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/days/"+today) {
		t.Fatalf("Capture from the browser: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	day := must(t)(l.api.Get(t.Context(), "/days/"+today))
	if got := fmt.Sprint(day.Properties.(map[string]any)["mood"]); got != "[4 3]" {
		t.Errorf("the moods of the day after the capture of the browser: %s, want [4 3]", got)
	}
}

// Core test: TestCaptureReturnsToThePageThatSentIt.
// The capture form of a day returns to that day. This holds for today too.
// Home is the page of today, so no form returns to "/".
// The test reads the forms as a browser reads them.
func TestTheCaptureOfADayReturnsToTheDay(t *testing.T) {
	l := emptyLife(t)
	for _, day := range []string{today, dayFromToday(-3)} {
		page := "/days/" + day
		capture, ok := formdoc.Find(formdoc.Forms(browserPageAt(t, l.web, page)), func(f formdoc.Form) bool { return f.Action == page+"/capture" })
		if !ok {
			t.Errorf("%s has no form that captures on the day", page)
			continue
		}
		if got := capture.Values.Get("back"); got != page {
			t.Errorf("Capture on %s returns to %q, want the day", page, got)
		}
	}
}

// Core test: TestTheDayIsALeafAndItsMargin.
// The head of a day is a calendar leaf that opens its month. The weekday, the week and "today" are beside it.
// The links to the previous and the next day keep their whole name for a reader.
// In the source, the sheet (the journal, then Capture) comes before the margin. So a narrow screen shows the writing first.
// Record a reading is in the Readings of the margin, with its optional fields folded.
// 14 May 2031 is a Wednesday in ISO week 20.
func TestTheDayIsALeafAndItsMargin(t *testing.T) {
	l := emptyLife(t)
	body := browserPageAt(t, l.web, "/days/"+today)
	for _, want := range []string{
		`<a class="leaf" href="/months/2031-05" aria-label="May 2031, the calendar of the month"><span class="leaf-month">May 2031</span><span class="leaf-day">14</span></a>`,
		`<h1>Wednesday</h1>`,
		`<p class="sub"><span class="tag open">today</span>`,
		`href="/days/2031-05-13" rel="prev" aria-label="Previous day, Tue 13 May"><span aria-hidden="true">‹</span><span class="step-day">Tue 13 May</span></a>`,
		`href="/days/2031-05-15" rel="next" aria-label="Next day, Thu 15 May"><span class="step-day">Thu 15 May</span><span aria-hidden="true">›</span></a>`,
		`<p class="empty">Nothing written today yet. <a href="#capture">Add to today</a> below.</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the day lacks %s", want)
		}
	}
	order := []string{`<div class="sheet">`, `<section class="journal"`, `<section class="capture"`, `<div class="margin">`,
		`<section class="group" aria-label="Readings">`}
	at, prev := -1, "the start"
	for _, mark := range order {
		i := strings.Index(body, mark)
		if i <= at {
			t.Fatalf("%s is not after %s in the source (at %d, after %d)", mark, prev, i, at)
		}
		at, prev = i, mark
	}
	readings := body[strings.Index(body, `aria-label="Readings"`):]
	readings = readings[:strings.Index(readings, "</section>")]
	more := regexp.MustCompile(`(?s)<details class="more-fields"><summary>2 more fields</summary>(.*?)</details>`).FindStringSubmatch(readings)
	if !strings.Contains(readings, `<summary class="button">Add a reading</summary>`) || more == nil ||
		strings.Count(more[1], "<label>") != 2 || !strings.Contains(more[1], `name="taken_at"`) {
		t.Errorf("Add a reading is not in the Readings, with 2 folded fields:\n%s", readings)
	}
	if !strings.Contains(readings, `<p class="empty">No readings on this day. <a href="#record">Add one</a> below.</p>`) {
		t.Errorf("a day with no reading does not say so:\n%s", readings)
	}

	past := browserPageAt(t, l.web, "/days/"+dayFromToday(-3))
	for _, want := range []string{`<span class="leaf-day">11</span>`, `<h1>Sunday</h1>`, `<a href="/days/today">Go to today</a>`,
		`<p class="empty">Nothing written on this day. <a href="#capture">Add to it</a> below.</p>`, `<h2>Add to Sun 11 May</h2>`} {
		if !strings.Contains(past, want) {
			t.Errorf("a past day lacks %s", want)
		}
	}
	if strings.Contains(past, `<span class="tag open">today</span>`) {
		t.Error("a past day says today")
	}
}

// Core test: TestTheStreamPagesByDay.
// /days lists the days that have something, the newest first. Each day is a card with the words of the day.
// A day after today is not in the list.
func TestTheStreamDrawsOneCardForEachDayWithSomething(t *testing.T) {
	l := emptyLife(t)
	for day, text := range map[string]string{today: "Today.", dayFromToday(-1): "Yesterday.", dayFromToday(-2): "Two days ago.", "2030-05-14": "A year ago.", "2031-05-20": "Not yet."} {
		l.do(t, "/days/"+day, "capture", map[string]string{"text": text})
	}
	body := browserPageAt(t, l.web, "/days")
	for _, want := range []string{fmt.Sprintf(`<article class="day-card" aria-label="%s">`, today), `<p class="card-words">(00:00) Today.</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("/days lacks %s", want)
		}
	}
	if strings.Contains(body, "Not yet.") {
		t.Error("/days lists a day after today")
	}
}
