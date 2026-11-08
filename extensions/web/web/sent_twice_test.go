package web_test

// A form that a browser sends twice (a double click, a reload that posts again) is one write. The form carries a key
// (import_key, D28) that web draws, the core stores the first write under it, and the second send is an answer "Already
// added" that lands on the day. The core is the real one. Its clock stands still, so a reading that is sent again "on the
// next day" is not here.

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"aminte-web/internal/formdoc"
)

// drawnFormPostingTo is the form of the page at uri, drawn by h, that posts to action. It fails the test when the page has none.
func drawnFormPostingTo(t *testing.T, h http.Handler, uri, action string) formdoc.Form {
	t.Helper()
	page := ask(h, http.MethodGet, uri)
	if page.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", uri, page.Code)
	}
	form, ok := formdoc.Find(formdoc.Forms(page.Body.String()), func(f formdoc.Form) bool { return f.Method == http.MethodPost && f.Action == action })
	if !ok {
		t.Fatalf("GET %s: no form posts to %s", uri, action)
	}
	return form
}

// locationWithoutFeedback is a Location without the receipt of the toast (feedback), which only web reads.
func locationWithoutFeedback(t *testing.T, location string) string {
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

// readingHrefsOfDay are the addresses of the readings that the day at path holds, from the API.
func readingHrefsOfDay(t *testing.T, l *life, path string) []string {
	t.Helper()
	var out []string
	for _, e := range must(t)(l.api.Get(t.Context(), path)).Entities {
		if slices.Contains(e.Rel, "reading") {
			out = append(out, e.Href)
		}
	}
	return out
}

// TestTheCaptureFormCarriesAFreshKeyEachTimeItIsDrawn (issue 0085, D28): the browser sends the key that the form was drawn
// with, so a form sent twice is one capture. The form of the next draw has another key, so the next entry is a new
// capture.
func TestTheCaptureFormCarriesAFreshKeyEachTimeItIsDrawn(t *testing.T) {
	l := emptyLife(t)
	action := "/days/" + today + "/capture"
	first := drawnFormPostingTo(t, l.web, "/days/"+today, action).Values.Get("import_key")
	second := drawnFormPostingTo(t, l.web, "/days/"+today, action).Values.Get("import_key")
	if len(first) < 16 || first == second {
		t.Errorf("the keys of two draws are %q and %q: want two different keys of 16 characters or more", first, second)
	}
}

// TestACaptureFormSentAgainIsAddedOnce: the same form sent twice answers 303 to the day both times, the second time with
// "Already added" and no second paragraph.
func TestACaptureFormSentAgainIsAddedOnce(t *testing.T) {
	l := emptyLife(t)
	form := url.Values{"text": {"Walked the dog"}, "mood": {"4"}, "import_key": {"form-key-1"}}
	toDay := "/days/" + today
	first := postForm(l.web, toDay+"/capture", form)
	if first.Code != http.StatusSeeOther || locationWithoutFeedback(t, first.Header().Get("Location")) != toDay {
		t.Fatalf("first send: %d %q", first.Code, first.Header().Get("Location"))
	}
	if body := pageAt(t, l.web, first.Header().Get("Location")); strings.Contains(body, "Already added") {
		t.Errorf("the first send says Already added:\n%.400s", body)
	}
	again := postForm(l.web, toDay+"/capture", form)
	if again.Code != http.StatusSeeOther || locationWithoutFeedback(t, again.Header().Get("Location")) != toDay {
		t.Fatalf("send again: %d %q, want 303 to the day", again.Code, again.Header().Get("Location"))
	}
	if body := pageAt(t, l.web, again.Header().Get("Location")); !strings.Contains(body, "Already added") {
		t.Errorf("the second send does not say Already added:\n%.400s", body)
	}
	day := must(t)(l.api.Get(t.Context(), toDay))
	props, _ := day.Properties.(map[string]any)
	if got := fmt.Sprint(props["mood"]); got != "[4]" {
		t.Errorf("the moods of the day after a send again: %s, want [4]", got)
	}
	page := must(t)(l.api.Get(t.Context(), href(day, "page")))
	if body := page.Properties.(map[string]any)["body"]; body != "(00:00) Walked the dog" {
		t.Errorf("the page after a send again: %q", body)
	}
}

// TestAReadingSentAgainLandsOnTheDayOfTheStoredReading: a form of a reading sent again gets the redirect to the day of the
// reading that stands, with the toast "Already added. Nothing changed.", as a capture does, and the day keeps one reading.
func TestAReadingSentAgainLandsOnTheDayOfTheStoredReading(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	form := url.Values{"metric": {"Weight"}, "day": {"2026-10-14"}, "value": {"72"}, "import_key": {"scale/browser"}}
	if first := postForm(l.web, "/measurements", form); first.Code != http.StatusSeeOther {
		t.Fatalf("the first form: %d, want 303: %.300s", first.Code, first.Body.String())
	}
	again := postForm(l.web, "/measurements", form)
	if again.Code != http.StatusSeeOther || locationWithoutFeedback(t, again.Header().Get("Location")) != "/days/2026-10-14" {
		t.Fatalf("the form sent again: %d %q, want 303 to /days/2026-10-14", again.Code, again.Header().Get("Location"))
	}
	if body := ask(l.web, http.MethodGet, again.Header().Get("Location")).Body.String(); !strings.Contains(body, "Already added. Nothing changed.") {
		t.Errorf("the second send does not say Already added:\n%.400s", body)
	}
	if got := readingHrefsOfDay(t, l, "/days/2026-10-14"); len(got) != 1 {
		t.Errorf("the day holds the readings %v after the same form was sent twice, want one", got)
	}
}

// TestARecordGoesBackToTheDayItWasMadeOn: the form of Home and of a day, filled by a link, returns to that page with the
// answer, and its key is filled, so a form sent twice is one reading.
func TestARecordGoesBackToTheDayItWasMadeOn(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Walk"})
	pages := map[string]string{"/": today, "/days/2031-05-12": "2031-05-12"} // the page that the form is on, and the day that its reading is for
	for page, day := range pages {
		form := drawnFormPostingTo(t, l.web, page+"?new=record&metric=Walk&value=1", "/measurements")
		sent := form.Values
		if sent.Get("import_key") == "" || sent.Get("metric") != "Walk" || sent.Get("day") != day {
			t.Fatalf("%s: the browser sends %v, want the key, the metric Walk and the day %s", page, sent, day)
		}
		got := postForm(l.web, "/measurements", sent)
		if got.Code != http.StatusSeeOther || locationWithoutFeedback(t, got.Header().Get("Location")) != page {
			t.Errorf("%s: %d, returned to %q, want %q", page, got.Code, got.Header().Get("Location"), page)
		}
		if again := postForm(l.web, "/measurements", sent); again.Code != http.StatusSeeOther {
			t.Errorf("%s: the same form sent twice: %d", page, again.Code)
		}
	}
	// two forms, each sent twice: one reading on each day
	for _, day := range pages {
		if got := readingHrefsOfDay(t, l, "/days/"+day); len(got) != 1 {
			t.Errorf("%s: a form sent twice made the readings %v, want one", day, got)
		}
		if strings.Contains(ask(l.web, http.MethodGet, "/days/"+day).Body.String(), "more this day") {
			t.Errorf("%s: the page says there is more than one reading", day)
		}
	}
}
