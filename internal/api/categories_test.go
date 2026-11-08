package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"aminte/client"
	"aminte/internal/api/apitest"
	"aminte/siren"
)

// The categories (D26). A category page lists what is filed in it and opens in Browse, and a new page is filed where
// it is made (docs/plans/115-categories-to-browse.md). file-pages files several pages in one write
// (docs/plans/112-metrics-as-cards.md), and never a day page.

// filedFixture files Alps 2030, Japan 2031 and the person Sam in Travel, and Travel in Life. It returns the page ids
// by title.
func filedFixture(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	ids := map[string]string{}
	for _, title := range []string{"Japan 2031", "Alps 2030"} {
		ids[title] = fmt.Sprint(props(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": title})))["id"])
	}
	ids["Sam"] = fmt.Sprint(props(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Sam"})))["id"])
	for _, title := range []string{"Japan 2031", "Alps 2030", "Sam"} {
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+ids[title])), "file-pages"), map[string]string{"category": "Travel"}))
	}
	ids["Travel"] = fmt.Sprint(props(apitest.Must(t)(c.Get(ctx, "/pages?title=Travel")))["id"])
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+ids["Travel"])), "file-pages"), map[string]string{"category": "Life"}))
	ids["Life"] = fmt.Sprint(props(apitest.Must(t)(c.Get(ctx, "/pages?title=Life")))["id"])
	return ids
}

func TestACategoryPageListsWhatIsFiledInIt(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	travel, life := ids["Travel"], ids["Life"]

	// The JSON: a sub-category's backlink carries the class category; the rest their type.
	e := apitest.Must(t)(c.Get(ctx, "/pages/"+life))
	var filed []string
	for _, l := range e.Entities {
		if l.Rel[0] == "backlink" && l.Rel[1] == "part-of" {
			filed = append(filed, l.Title+":"+strings.Join(l.Class, "+"))
		}
	}
	if want := "Travel:page+category"; strings.Join(filed, " ") != want {
		t.Errorf("Life's part-of backlinks: %v, want %s", filed, want)
	}
	filed = nil
	for _, l := range apitest.Must(t)(c.Get(ctx, "/pages/"+travel)).Entities {
		if l.Rel[0] == "backlink" && l.Rel[1] == "part-of" {
			filed = append(filed, l.Title+":"+strings.Join(l.Class, "+"))
		}
	}
	if want := "Alps 2030:page Japan 2031:page Sam:person"; strings.Join(filed, " ") != want {
		t.Errorf("Travel's part-of backlinks: %v, want %s", filed, want)
	}

	// A page in no category is filed under nobody.
	for _, l := range apitest.Must(t)(c.Get(ctx, "/pages/"+ids["Japan 2031"])).Entities {
		if l.Rel[0] == "backlink" && l.Rel[1] == "part-of" {
			t.Errorf("a page that is no category has the part-of backlink %s", l.Title)
		}
	}
}

func TestANewPageIsFiledWhereItIsMade(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	travel := ids["Travel"]

	// The catalog: category is an optional title field of create-page, so every surface has it.
	create := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "create-page")
	var fields []string
	for _, f := range create.Fields {
		fields = append(fields, f.Name)
	}
	if strings.Join(fields, ",") != "title,body,category" || create.Fields[2].Required || !create.Fields[2].Pages {
		t.Errorf("create-page fields: %+v", create.Fields)
	}

	// A category page offers create-page with its own title filled, and draws it as New page here.
	here := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+travel)), "create-page")
	if here.Fields[2].Value != "Travel" {
		t.Fatalf("Travel offers create-page as %+v", here)
	}
	if hasAction(apitest.Must(t)(c.Get(ctx, "/pages/"+ids["Japan 2031"])), "create-page") {
		t.Error("a page that is no category offers create-page")
	}

	// The write: the page and its part-of link in one; a missing category is made.
	made := apitest.Must(t)(c.Do(ctx, here, map[string]string{"title": "Kyoto", "body": "Temples."}))
	if l := apitest.Must(t)(c.Get(ctx, "/pages/"+travel)).Entities; !titled(l, "Kyoto") {
		t.Error("Kyoto is not filed in Travel")
	}
	var kinds []string
	for _, l := range made.Entities {
		kinds = append(kinds, l.Rel[0]+":"+l.Title)
	}
	if want := "part-of:Travel"; strings.Join(kinds, " ") != want {
		t.Errorf("Kyoto's links: %v, want %s", kinds, want)
	}
	apitest.Must(t)(c.Do(ctx, create, map[string]string{"title": "Lisbon", "category": "Cities"}))
	if cs := props(apitest.Must(t)(c.Get(ctx, "/browse")))["categories"].([]any); len(cs) != 3 || cs[0].(map[string]any)["path"] != "Cities" {
		t.Errorf("categories after Lisbon: %v", cs)
	}

	// A day cannot be a category (links_endpoint_types): refused, and no page is made.
	if _, err := c.Do(ctx, create, map[string]string{"title": "Nothing", "category": fixedToday}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Fatalf("a day as the category: %v", err)
	}
	if _, err := c.Get(ctx, "/pages?title=Nothing"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("the page was made although its filing was refused: %v", err)
	}
}

// titled says whether a link of the list has the title.
func titled(links []siren.Link, title string) bool {
	for _, l := range links {
		if l.Title == title {
			return true
		}
	}
	return false
}

// TestBrowseOpensTheCategoryOfAnyTitle: the list of a category is the address /browse?in=TITLE, with the title escaped
// in the query, whatever the title says.
func TestBrowseOpensTheCategoryOfAnyTitle(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	filedFixture(t, c)
	note := apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "create-page"), map[string]string{"title": "A note"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, note, "file-pages"), map[string]string{"category": "Rock & roll 2031"}))
	list := apitest.Must(t)(c.Get(ctx, "/browse?"+url.Values{"in": {"Rock & roll 2031"}}.Encode()))
	if got := browseRows(t, list); got != "A note:" {
		t.Errorf("Browse in the category: %s", got)
	}
}

// A new page is filed where it is made; a person, a file and a metric are filed by file-pages
// (docs/plans/131-the-pareto-pass.md, part 2).

// partOfLinks are the titles the entity links part-of, in order.
func partOfLinks(e *siren.Entity) []string {
	var out []string
	for _, l := range e.Entities {
		if len(l.Rel) == 1 && l.Rel[0] == "part-of" {
			out = append(out, l.Title)
		}
	}
	return out
}

func TestOnlyANewPageTakesACategory(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, name := range []string{"create-page", "create-person", "add-file", "register-metric"} {
		a := apitest.Action(t, root, name)
		f := a.Field("category")
		if want := name == "create-page"; (f != nil) != want {
			t.Errorf("%s has a field category: %v, want %v", name, f != nil, want)
		}
		if f != nil && (f.Required || !f.Pages || f.Title != "Category (title)") {
			t.Errorf("%s: category is %+v, want an optional page title", name, f)
		}
	}
}

// A day is never a category: 422, and neither the page nor its link is written. A day with no page yet is refused by
// entities_day_page, and no day page is written; a day page that exists is refused by links_endpoint_types (D26).
func TestADayAsTheCategoryWritesNothingOverHTTP(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint("day page exists ", exists), func(t *testing.T) {
			c, _, _ := apitest.Fresh(t, "cli")
			ctx := t.Context()
			root := apitest.Must(t)(c.Get(ctx, "/"))
			if exists {
				apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/days/2031-05-02")), "capture"), map[string]string{"text": "a synthetic entry"}))
			}
			for _, tc := range []struct {
				action string
				values map[string]string
			}{
				{"create-page", map[string]string{"title": "Nobody"}},
			} {
				tc.values["category"] = "2031-05-02"
				if _, err := c.Do(ctx, apitest.Action(t, root, tc.action), tc.values); apitest.Status(err) != http.StatusUnprocessableEntity {
					t.Errorf("%s with a day as the category: %v, want 422", tc.action, err)
				}
				if _, err := c.Get(ctx, "/pages?title=Nobody"); apitest.Status(err) != http.StatusNotFound {
					t.Errorf("%s: Nobody was written although the write was refused: %v", tc.action, err)
				}
				_, err := c.Get(ctx, "/pages?title=2031-05-02")
				if !exists && apitest.Status(err) != http.StatusNotFound {
					t.Errorf("%s: the day page was written although the write was refused: %v", tc.action, err)
				}
				if exists && err != nil {
					t.Errorf("%s: the day page that exists: %v", tc.action, err)
				}
			}
		})
	}
}

// Browse inside a category fills category in its New page; the New of another kind has no category.
func TestBrowseNewFormsInACategoryCarryIt(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	ctx := t.Context()
	made := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?in=family&kind=page")), "create-page")
	if v := fieldOf(made, "category"); v != "Family" {
		t.Errorf("pages in Family: create-page has category %q, want Family", v)
	}
	if v := fieldOf(apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=page")), "create-page"), "category"); v != "" {
		t.Errorf("pages everywhere: create-page has category %q, want none", v)
	}
	if person := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?in=family&kind=person")), "create-person"); person.Field("category") != nil {
		t.Errorf("people in Family: create-person has a category field: %+v", person.Fields)
	}

	// The form, sent as it is filled, files the new page in Family.
	cy := apitest.Must(t)(c.Do(ctx, made, map[string]string{"title": "Cy"}))
	if got := partOfLinks(cy); strings.Join(got, ",") != "Family" {
		t.Errorf("Cy made from Browse in Family is part-of %v", got)
	}
}

func TestFileMetricsAtOnce(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	if file.Owner || file.Danger || file.Method != "POST" || file.Href != "/pages/file" {
		t.Fatalf("/metrics offers file-pages as %+v", file)
	}
	var fields []string
	for _, f := range file.Fields {
		fields = append(fields, f.Name)
	}
	if !slices.Equal(fields, []string{"ids", "category", "from", "move"}) || !file.Fields[0].Required || file.Fields[1].Required || file.Fields[2].Required {
		t.Errorf("file-pages fields: %+v", file.Fields)
	}

	// Commas separate the ids; a missing category is made, and said so.
	done := apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"] + "," + ids["Steps"], "category": "Bloodwork"}))
	p := props(done)
	cat := p["category"].(map[string]any)
	if done.Class[0] != "filed" || cat["title"] != "Bloodwork" || p["created"] != true || p["added"] != json.Number("2") || p["count"] != json.Number("2") ||
		len(done.Entities) != 2 || done.Entities[0].Href != "/pages/"+ids["Ferritin"] || done.Result != "Put Ferritin and Steps in Bloodwork (a new category)." {
		t.Fatalf("the answer: %v %v %v", done.Class, p, done.Result)
	}
	if got, want := apitest.Href(done, "self"), "/pages/"+fmt.Sprint(cat["id"]); got != want {
		t.Errorf("the answer's self is %s, want the category %s", got, want)
	}
	if got, want := groupsOf(t, c), "Bloodwork:Ferritin,Steps Not filed:Mood,Walk,Weight"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}

	// An agent may file: a part-of link is open to it, as link is.
	agent := client.InProcess(h, "agent:test")
	if again := apitest.Must(t)(agent.Do(ctx, file, map[string]string{"ids": ids["Weight"], "category": "Body"})); again.Result != "Put Weight in Body (a new category)." {
		t.Errorf("an agent's filing: %v", again.Result)
	}
	// Move takes Steps out of Bloodwork, and Ferritin stays in it.
	moved := apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Steps"], "category": "Body", "move": "1"}))
	if props(moved)["moved"] != json.Number("1") || moved.Result != "Moved Steps in Body." {
		t.Errorf("the move: %v %v", props(moved), moved.Result)
	}
	if got, want := groupsOf(t, c), "Bloodwork:Ferritin Body:Steps,Weight Not filed:Mood,Walk"; got != want {
		t.Errorf("after the move, groups %q, want %q", got, want)
	}

	// From takes Steps out of Body: only that link is removed, and the pages stay.
	out := apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Steps"], "from": "Body"}))
	if props(out)["removed"] != json.Number("1") || out.Result != "Removed Steps from Body." {
		t.Errorf("the removal: %v %v", props(out), out.Result)
	}
	if got, want := groupsOf(t, c), "Bloodwork:Ferritin Body:Weight Not filed:Mood,Steps,Walk"; got != want {
		t.Errorf("after the removal, groups %q, want %q", got, want)
	}

	// Refusals write nothing: an id no page has, no category, no id, a move that is not 0 or 1.
	for _, bad := range []struct {
		form map[string]string
		want int
	}{
		{map[string]string{"ids": ids["Mood"] + " 424242", "category": "Mood board"}, 404},
		{map[string]string{"ids": ids["Mood"]}, 422},
		{map[string]string{"ids": ids["Mood"], "category": "X", "from": "Y"}, 422},
		{map[string]string{"ids": ids["Mood"], "from": "Nowhere"}, 404},
		{map[string]string{"category": "Mood board"}, 422},
		{map[string]string{"ids": ids["Mood"], "category": "Mood board", "move": "yes"}, 422},
	} {
		if _, err := c.Do(ctx, file, bad.form); apitest.Status(err) != bad.want {
			t.Errorf("%v: %v, want %d", bad.form, err, bad.want)
		}
	}
	if _, err := c.Get(ctx, "/pages?title=Mood+board"); apitest.Status(err) != 404 {
		t.Errorf("a refused filing made its category page: %v", err)
	}
	if got := groupsOf(t, c); !strings.HasSuffix(got, "Not filed:Mood,Steps,Walk") {
		t.Errorf("a refused filing filed Mood: %q", got)
	}
}

// A day page is never filed (D26): file-pages that names a day page is a 422 with SQLite's sentence and writes
// nothing, not even for the other ids or the category it would make; a part-of link from a day page is a 422; and a
// day page's link form does not offer part-of.
func TestADayPageIsNeverFiled(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	plain := fmt.Sprint(props(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Plans"})))["id"])
	day := apitest.Must(t)(c.Get(ctx, apitest.Href(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "a synthetic entry"})), "page")))
	dayID := fmt.Sprint(props(day)["id"])
	const sentence = "a day page is never filed in a category (D26)"

	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+plain)), "file-pages")
	for _, ids := range []string{dayID, plain + " " + dayID, dayID + "," + plain} {
		_, err := c.Do(ctx, file, map[string]string{"ids": ids, "category": "Garden"})
		if apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(err), sentence) {
			t.Errorf("file-pages %s: %v, want a 422 that says %q", ids, err, sentence)
		}
	}
	if _, err := c.Get(ctx, "/pages?title=Garden"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("a refused filing made its category page: %v", err)
	}
	if got := partOfLinks(apitest.Must(t)(c.Get(ctx, "/pages/"+plain))); len(got) != 0 {
		t.Errorf("a refused filing filed Plans in %v", got)
	}

	if hasAction(day, "file-pages") {
		t.Error("a day page offers file-pages: a day page is never filed (D26)")
	}
	if got := partOfLinks(apitest.Must(t)(c.Get(ctx, "/pages/"+dayID))); len(got) != 0 {
		t.Errorf("the day page is part-of %v", got)
	}
}
