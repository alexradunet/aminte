package api_test

import (
	"fmt"
	"testing"

	"aminte/internal/api/apitest"
)

// TestPeopleListCarriesItsMentions: the rows of the people list of Browse say how many pages link to each person and
// the newest day page among them, in the order of the item links. The sheet says it in words.
func TestPeopleListCarriesItsMentions(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", "2026-10-12") // the day the words are read against: a day of this year has no year
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	for _, name := range []string{"Ana", "Sam", "Zed"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-person"), map[string]string{"title": name}))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, catalog, "capture"), map[string]string{"day": "2026-10-05", "text": "Lunch with [[Ana]]"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, catalog, "capture"), map[string]string{"day": "2026-10-09", "text": "Walk with [[Ana]] and [[Sam]]"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Trip", "body": "[[Ana]] came"}))

	list := apitest.Must(t)(c.Get(t.Context(), "/browse?kind=person"))
	rows, _ := list.Properties.(map[string]any)["rows"].([]any)
	type want struct {
		title    string
		mentions float64
		lastDay  string
	}
	wants := []want{{"Ana", 3, "2026-10-09"}, {"Sam", 1, "2026-10-09"}, {"Zed", 0, ""}}
	if len(rows) != len(wants) || len(list.Entities) != len(wants) {
		t.Fatalf("rows = %v, items = %d, want %d of each", rows, len(list.Entities), len(wants))
	}
	for i, w := range wants {
		row, _ := rows[i].(map[string]any)
		lastDay, _ := row["last_day"].(string)
		// The client may give a Go integer or a decoded JSON number: compare the printed value.
		if row["title"] != w.title || fmt.Sprint(row["mentions"]) != fmt.Sprint(w.mentions) || lastDay != w.lastDay {
			t.Errorf("row %d = %v, want %+v", i, row, w)
		}
		if list.Entities[i].Title != w.title {
			t.Errorf("item %d is %q, want %q (the order of rows)", i, list.Entities[i].Title, w.title)
		}
	}
	if _, has := rows[2].(map[string]any)["last_day"]; has {
		t.Errorf("a person with no day page has last_day: %v", rows[2])
	}
}

// TestPeopleListShowsThePictureAPersonEmbeds: in the people list of Browse, a person whose page embeds a picture has
// that picture's preview in its row, and the sheet draws it round before the name; a person with none has the icon of a person.
func TestPeopleListShowsThePictureAPersonEmbeds(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	jpg, _ := jpegFile(t, "ana.jpg", 40, 30)
	photo := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "Ana face.jpg", "original": jpg}))
	ana := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Ana"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, ana, "save-body"), map[string]string{"body": "![[Ana face.jpg]]\n\nMet at school."}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Sam"}))

	rows, _ := props(apitest.Must(t)(c.Get(ctx, "/browse?kind=person")))["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want Ana and Sam", rows)
	}
	want := apitest.Href(photo, "preview")
	if got := str(rows[0].(map[string]any)["picture"]); want == "" || got != want {
		t.Errorf("Ana's picture = %q, want %q", got, want)
	}
	if _, has := rows[1].(map[string]any)["picture"]; has {
		t.Errorf("Sam has a picture: %v", rows[1])
	}
}

// TestMentionsHaveNoDayWhenNoDayPageLinks: a page that is not a day page gives a count and no last day.
func TestMentionsHaveNoDayWhenNoDayPageLinks(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", "2026-10-12") // the day the words are read against: a day of this year has no year
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-person"), map[string]string{"title": "Lake"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Trip", "body": "At [[Lake]]"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Swim", "body": "In [[Lake]]"}))
	rows, _ := props(apitest.Must(t)(c.Get(t.Context(), "/browse?kind=person")))["rows"].([]any)
	if len(rows) != 1 || fmt.Sprint(rows[0].(map[string]any)["mentions"]) != "2" {
		t.Errorf("the people rows are %v, want Lake mentioned on 2 pages", rows)
	}
	if _, has := rows[0].(map[string]any)["last_day"]; has {
		t.Errorf("a person mentioned by no day page has last_day: %v", rows[0])
	}
}

// TestDaysHaveNoRows: the list of the days is its item links alone; the lists of Browse carry rows.
func TestDaysHaveNoRows(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	if props, _ := apitest.Must(t)(c.Get(t.Context(), "/days")).Properties.(map[string]any); props["rows"] != nil {
		t.Errorf("GET /days has rows: %v", props["rows"])
	}
}
