package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/internal/api/apitest"
	"lifelog/siren"
)

// journalFixture is a small life around fixedToday: a mood; two earlier day pages, one written as a mood alone.
func journalFixture(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	capture := func(day string, v map[string]string) {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	capture(dayOf(-2), map[string]string{"text": "Morning run\nRan with [[Sam]] by the river."})
	capture(dayOf(-1), map[string]string{"mood": "2"})
	capture(fixedToday, map[string]string{"text": "Quiet day.", "mood": "4"})
	capture(fixedToday, map[string]string{"mood": "3"})
	return c, h
}

func TestTheDayCarriesItsMood(t *testing.T) {
	c, _ := journalFixture(t)

	today := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	p := props(today)
	if fmt.Sprint(p["mood"]) != "[4 3]" || p["today"] != fixedToday {
		t.Errorf("today's mood %v, today %v", p["mood"], p["today"])
	}
	save := apitest.Action(t, today, "save-body")
	page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(today, "page")))
	if save.Href != apitest.Href(today, "page")+"/body" || fieldOf(save, "version") != fmt.Sprint(props(page)["version"]) ||
		fieldOf(save, "body") != props(page)["body"] {
		t.Errorf("the day's save-body: %+v, page %v", save, props(page))
	}

	// A past day with nothing on it.
	past := props(apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-20))))
	if fmt.Sprint(past["mood"]) != "[]" {
		t.Errorf("a day with no mood: %v", past["mood"])
	}
	quiet := apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-5)))
	if hasAction(quiet, "save-body") {
		t.Errorf("a quiet day offers save-body: %s", names(quiet))
	}
}

// TestTheRootIsTodayAtAGlance: GET / is the entry point of the API for every client: the newest day pages and every
// action that starts something. A client that accepts HTML gets the same entity, not the page of today: the core draws
// no page.
func TestTheRootIsTodayAtAGlance(t *testing.T) {
	c, h := journalFixture(t)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := props(root)
	var recent []string
	for _, r := range p["recent"].([]any) {
		m := r.(map[string]any)
		recent = append(recent, fmt.Sprintf("%v %v %v", m["day"], m["mood"], m["first_line"]))
	}
	if got, want := strings.Join(recent, " | "), fixedToday+" [4 3] (00:00) Quiet day. | "+dayOf(-1)+" [2] <nil> | "+dayOf(-2)+" <nil> (00:00) Morning run"; got != want {
		t.Errorf("recent:\n got %s\nwant %s", got, want)
	}
	if p["today"] != fixedToday || !hasAction(root, "snapshot") {
		t.Errorf("the JSON root: today %v, actions %s", p["today"], names(root))
	}

	for _, accept := range []string{"application/vnd.siren+json", "application/json", "text/html,application/json", "text/html,application/xhtml+xml", "text/html", ""} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"class": [`+"\n"+`    "root"`) || rec.Header().Get("Content-Type") != "application/vnd.siren+json" {
			t.Errorf("GET / as %q: %d %q, want the JSON root: %.200s", accept, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
}

// TestTheDayOfTodayCarriesTheLastEntries: the day of today carries "recent", the list the root has, and no other day
// does (docs/plans/129-the-ux-pass.md, part 10).
func TestTheDayOfTodayCarriesTheLastEntries(t *testing.T) {
	c, _ := journalFixture(t)
	today := props(apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday)))
	var recent []string
	list, _ := today["recent"].([]any)
	for _, r := range list {
		m := r.(map[string]any)
		recent = append(recent, fmt.Sprintf("%v %v %v", m["day"], m["mood"], m["first_line"]))
	}
	if got, want := strings.Join(recent, " | "), fixedToday+" [4 3] (00:00) Quiet day. | "+dayOf(-1)+" [2] <nil> | "+dayOf(-2)+" <nil> (00:00) Morning run"; got != want {
		t.Errorf("recent of today:\n got %s\nwant %s", got, want)
	}
	if _, ok := props(apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-1))))["recent"]; ok {
		t.Error("a day that is not today carries recent")
	}
}

// TestTheMoodIsAChoiceOfFive: capture's mood offers the options 1 to 5 (docs/plans/092-the-app-shell.md), on the
// day's action and in the catalog; the values the API takes do not change.
func TestTheMoodIsAChoiceOfFive(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	day := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	for _, a := range []string{"the day's", "the catalog's"} {
		capture := apitest.Action(t, day, "capture")
		if a == "the catalog's" {
			capture = apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "capture")
		}
		var mood string
		for _, f := range capture.Fields {
			if f.Name == "mood" {
				mood = fmt.Sprintf("%s %v %q", f.Type, f.Required, f.Options)
			}
		}
		if mood != `number false ["1" "2" "3" "4" "5"]` {
			t.Errorf("%s capture's mood: %s", a, mood)
		}
	}

	capture := apitest.Action(t, day, "capture")
	apitest.Must(t)(c.Do(t.Context(), capture, map[string]string{"text": "No mood today."}))
	apitest.Must(t)(c.Do(t.Context(), capture, map[string]string{"mood": "4"}))
	for _, bad := range []string{"6", "0", "2.5"} {
		if _, err := c.Do(t.Context(), capture, map[string]string{"mood": bad}); apitest.Status(err) != 422 {
			t.Errorf("mood %s: %v, want 422", bad, err)
		}
	}
	if got := fmt.Sprint(props(apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday)))["mood"]); got != "[4]" {
		t.Errorf("the day's mood after the captures: %s", got)
	}
	past := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-1))), "capture")
	apitest.Must(t)(c.Do(t.Context(), past, map[string]string{"mood": "2"}))
	if got := fmt.Sprint(props(apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-1))))["mood"]); got != "[2]" {
		t.Errorf("the mood of a day with one mood: %s", got)
	}
}

// TestADayLinksItsNeighboursAndItsMonth (docs/plans/095-the-leaf-and-its-margin.md): a day links the day before and
// the day after, its month and its week, and says which day is today. 14 May 2031 is a Wednesday in ISO week 20.
func TestADayLinksItsNeighboursAndItsMonth(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	today := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	for rel, want := range map[string]string{"prev": "/days/2031-05-13", "next": "/days/2031-05-15", "month": "/months/2031-05", "week": "/weeks/2031-W20"} {
		if got := apitest.Href(today, rel); got != want {
			t.Errorf("the link %s of today is %q, want %q", rel, got, want)
		}
	}
	past := apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-3)))
	if props(today)["today"] != fixedToday || props(past)["today"] != fixedToday || props(past)["day"] != "2031-05-11" {
		t.Errorf("today %v, past: today %v day %v", props(today)["today"], props(past)["today"], props(past)["day"])
	}
}
func TestLiveDayView(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Walk"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Walk", "day": "2026-09-01", "value": "1"}))
	page := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Walk"))
	for i, hidden := range []bool{false, true, false} {
		if hidden {
			apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "tombstone-pages"), nil))
			page = apitest.Must(t)(c.Get(t.Context(), apitest.Href(page, "self")))
		} else if i > 0 {
			page = apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "revive"), nil))
		}
		day := apitest.Must(t)(c.Get(t.Context(), "/days/2026-09-01"))
		if hasOption(apitest.Action(t, day, "record"), "metric", "Walk") == hidden {
			t.Errorf("record options hidden=%v: %+v", hidden, day.Actions)
		}
		props := day.Properties.(map[string]any)
		listed := false
		for _, v := range props["view"].([]any) {
			row := v.(map[string]any)
			listed = listed || row["what"] == "Walk" && strings.HasPrefix(fmt.Sprint(row["detail"]), "1")
		}
		if listed == hidden {
			t.Errorf("reading visibility hidden=%v: %v", hidden, props["view"])
		}
		if len(props["readings"].([]any)) != 1 {
			t.Error("historical reading lost")
		}
	}
}

// TestACaptureSentAgainIsAddedOnce: the same capture sent twice, with the same import_key, adds one paragraph and one
// mood. The second answer is "Already added" with the link to the day, as a reading sent again gets "Already recorded".
func TestACaptureSentAgainIsAddedOnce(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	toDay := "/days/" + fixedToday
	day := apitest.Must(t)(c.Get(t.Context(), toDay))
	capture := apitest.Action(t, day, "capture")
	if capture.Field("import_key") == nil {
		t.Fatal("the capture action has no import_key field")
	}
	form := map[string]string{"text": "Walked the dog", "mood": "4", "import_key": "form-key-1"}
	if first := apitest.Must(t)(c.Do(t.Context(), capture, form)); first.Title == "Already added" {
		t.Errorf("the first send says Already added")
	}
	again := apitest.Must(t)(c.Do(t.Context(), capture, form))
	if again.Title != "Already added" || apitest.Href(again, "day") != toDay {
		t.Errorf("sent again: %q links the day %q, want Already added with %s", again.Title, apitest.Href(again, "day"), toDay)
	}
	day = apitest.Must(t)(c.Get(t.Context(), toDay))
	if got := fmt.Sprint(props(day)["mood"]); got != "[4]" {
		t.Errorf("the moods of the day after a send again: %s, want [4]", got)
	}
	if page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(day, "page"))); props(page)["body"] != "(00:00) Walked the dog" {
		t.Errorf("the page after a send again: %q", props(page)["body"])
	}
	fields := map[string]string{"text": "By JSON", "import_key": "json-key-1"}
	apitest.Must(t)(c.Do(t.Context(), capture, fields))
	answer := apitest.Must(t)(c.Do(t.Context(), capture, fields))
	if answer.Title != "Already added" || apitest.Href(answer, "day") != toDay {
		t.Errorf("sent again by JSON: %q links the day %q, want Already added with %s", answer.Title, apitest.Href(answer, "day"), toDay)
	}
	if page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(day, "page"))); props(page)["body"] != "(00:00) Walked the dog\n\n(00:00) By JSON" {
		t.Errorf("the page after the JSON sends: %q", props(page)["body"])
	}
}

// TestTheStreamPagesByDay: /days is the days with something, the newest first (docs/plans/130-the-explore-pass.md),
// paged by limit and offset as every list: next is the older days, prev the newer days or the top. A day after today
// is not in it.
func TestTheStreamPagesByDay(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday) // 2031-05-14
	for day, text := range map[string]string{fixedToday: "Today.", dayOf(-1): "Yesterday.", dayOf(-2): "Two days ago.", "2030-05-14": "A year ago.", "2031-05-20": "Not yet."} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": text}))
	}
	cards := func(e *siren.Entity) string {
		var out []string
		for _, x := range props(e)["cards"].([]any) {
			m := x.(map[string]any)
			out = append(out, fmt.Sprintf("%v:%v", m["day"], m["words"]))
		}
		return strings.Join(out, " ")
	}
	top := apitest.Must(t)(c.Get(t.Context(), "/days?limit=2"))
	if got, want := cards(top), "2031-05-14:(00:00) Today. 2031-05-13:(00:00) Yesterday."; got != want {
		t.Errorf("the top page: %s, want %s", got, want)
	}
	if apitest.Href(top, "next") != "/days?limit=2&offset=2" || apitest.Href(top, "prev") != "" {
		t.Errorf("the top page links next %q, prev %q", apitest.Href(top, "next"), apitest.Href(top, "prev"))
	}
	for rel, want := range map[string]string{"days": "/days", "week": "/weeks/2031-W20", "month": "/months/2031-05", "year": "/years/2031"} {
		if got := apitest.Href(top, rel); got != want {
			t.Errorf("the zoom link %s is %q, want %q", rel, got, want)
		}
	}
	older := apitest.Must(t)(c.Get(t.Context(), apitest.Href(top, "next")))
	if got, want := cards(older), "2031-05-12:(00:00) Two days ago. 2030-05-14:(00:00) A year ago."; got != want {
		t.Errorf("the older page: %s, want %s", got, want)
	}
	if apitest.Href(older, "next") != "" || apitest.Href(older, "prev") != "/days?limit=2" {
		t.Errorf("the older page links next %q, prev %q", apitest.Href(older, "next"), apitest.Href(older, "prev"))
	}
	if _, err := c.Get(t.Context(), "/days?limit=0"); apitest.Status(err) != 422 {
		t.Errorf("/days?limit=0: %v, want a 422", err)
	}
}

// TestOnThisDayListsTheSameDayOfEarlierYears: a day carries, as "earlier", its day pages of the years before, the nearest
// year first, while a page exists, at most five. A day with no earlier year carries none, and a past day has its own.
func TestOnThisDayListsTheSameDayOfEarlierYears(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	earlier := func(day string) string {
		t.Helper()
		var out []string
		list, _ := props(apitest.Must(t)(c.Get(t.Context(), "/days/"+day)))["earlier"].([]any)
		for _, x := range list {
			m := x.(map[string]any)
			out = append(out, fmt.Sprintf("%v %v %v", m["day"], m["mood"], m["first_line"]))
		}
		return strings.Join(out, " | ")
	}
	capture := func(day string, v map[string]string) {
		t.Helper()
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	if got := earlier(fixedToday); got != "" {
		t.Errorf("a day with no earlier year has %q", got)
	}
	capture("2030-05-14", map[string]string{"text": "A year ago.", "mood": "5"})
	capture("2029-05-14", map[string]string{"text": "Two years ago."})
	capture("2027-05-14", map[string]string{"text": "Four years ago, after a gap."})
	if got, want := earlier(fixedToday), "2030-05-14 [5] (00:00) A year ago. | 2029-05-14 <nil> (00:00) Two years ago."; got != want {
		t.Errorf("the earlier years of today:\n got %s\nwant %s", got, want)
	}
	if got, want := earlier("2030-05-14"), "2029-05-14 <nil> (00:00) Two years ago."; got != want {
		t.Errorf("the earlier years of 2030-05-14:\n got %s\nwant %s", got, want)
	}
	for y := 2025; y <= 2026; y++ {
		capture(fmt.Sprintf("%d-05-14", y), map[string]string{"text": "Synthetic words."})
	}
	capture("2028-05-14", map[string]string{"text": "Three years ago."})
	if n := strings.Count(earlier(fixedToday), "|") + 1; n != 5 || strings.Contains(earlier(fixedToday), "2025-05-14") {
		t.Errorf("the earlier years of today are %d, want the five nearest (2030 to 2026): %s", n, earlier(fixedToday))
	}
}
