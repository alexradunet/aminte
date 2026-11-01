package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestAFormWithAReturnPathIsRefused: a write answers the entity it made, with its address in Location, and goes
// nowhere else. The fields back and feedback were the browser's: they are unknown fields now, for every client and
// every body type, and the refused write changes nothing. (Return paths are the business of a UI: the browser
// extension keeps them.)
func TestAFormWithAReturnPathIsRefused(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	rec := request(h, "POST", "/days/2026-09-29/capture", url.Values{"text": {"a line"}}.Encode(), map[string]string{"Host": "localhost"})
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != "/days/2026-09-29" {
		t.Fatalf("a capture: %d, Location %q, want 200 and the day", rec.Code, rec.Header().Get("Location"))
	}
	if n := len(apitest.Must(t)(c.Get(t.Context(), "/days/2026-09-29")).Properties.(map[string]any)["view"].([]any)); n == 0 {
		t.Error("the write was not done")
	}
	for _, field := range []string{"back", "feedback"} {
		rec := request(h, "POST", "/days/2026-09-30/capture", url.Values{"text": {"refused"}, field: {"/browse"}}.Encode(), map[string]string{"Host": "localhost"})
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "unknown field "+field) {
			t.Errorf("a capture with %s: %d %.200s, want 422 that names the field", field, rec.Code, rec.Body.String())
		}
	}
	if _, err := c.Get(t.Context(), "/days/2026-09-30"); err != nil {
		t.Fatal(err)
	}
	if page := apitest.Must(t)(c.Get(t.Context(), "/days/2026-09-30")).Properties.(map[string]any)["page"]; page != nil && page != float64(0) {
		t.Errorf("a refused capture wrote the page %v", page)
	}
}

// TestAStableNameResolvesAfterARename: a rename keeps the id. The answer is the page, a link by the old title leads to
// the same id, and the page that links the old title shows the new one.
func TestAStableNameResolvesAfterARename(t *testing.T) {
	c, h, s := apitest.Fresh(t, "cli")
	id, err := s.CreatePerson(context.Background(), "cli", "Old person", "Synthetic Person", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := s.CreatePage(context.Background(), "cli", "Old reference", "See [[Old person]].", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := request(h, "POST", fmt.Sprintf("/pages/%d/rename", id), url.Values{"title": {"New person"}}.Encode(), map[string]string{"Host": "localhost"})
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != fmt.Sprintf("/pages/%d", id) {
		t.Fatalf("rename: %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := apitest.Must(t)(c.Get(t.Context(), fmt.Sprintf("/pages/%d", id))).Title; got != "New person" {
		t.Errorf("the preferred title is %q, want New person", got)
	}
	links, _ := apitest.Must(t)(c.Get(t.Context(), fmt.Sprintf("/pages/%d", ref))).Properties.(map[string]any)["links"].([]any)
	found := false
	for _, l := range links {
		l := l.(map[string]any)
		found = found || (l["title"] == "New person" && fmt.Sprint(l["id"]) == fmt.Sprint(id))
	}
	if !found {
		t.Errorf("the old reference does not link the person by the new title and the same id: %v", links)
	}
}

// TestASaveCarriesTheRevisionAsAString: the revision of a page is a number above 2^53 in this file. The entity gives it
// as a string, so no client rounds it; a save with it is applied, and the old token and the clock token are refused as
// stale versions, whatever the body says.
func TestASaveCarriesTheRevisionAsAString(t *testing.T) {
	c, h, store := apitest.Fresh(t, "cli")
	d := store.DB
	id, _, err := store.CreatePage(context.Background(), "cli", "Browser version", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE entities SET revision=9007199254740993 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	page := apitest.Must(t)(c.Get(t.Context(), fmt.Sprintf("/pages/%d", id)))
	if v := props(page)["version"]; v != "9007199254740993" {
		t.Fatalf("the version of the entity is %#v, want the string 9007199254740993", v)
	}
	save := apitest.Action(t, page, "save-body")
	if f := save.Field("version"); f == nil || f.Value != "9007199254740993" {
		t.Fatalf("the field version of save-body is %+v", f)
	}
	var clock string
	if err := d.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, id).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	post := func(token, body string) *http.Response {
		t.Helper()
		rec := request(h, "POST", fmt.Sprintf("/pages/%d/body", id), url.Values{"version": {token}, "body": {body}}.Encode(), map[string]string{"Host": "localhost"})
		return rec.Result()
	}
	if got := post("9007199254740993", "committed").StatusCode; got != http.StatusOK {
		t.Fatalf("a save with the version: %d", got)
	}
	for _, token := range []string{"9007199254740993", clock} {
		res := post(token, "stale [[Forbidden]]")
		if res.StatusCode != http.StatusConflict {
			t.Fatalf("a stale token %q: %d", token, res.StatusCode)
		}
	}
	p, err := store.PageByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "committed" || p.Version != "9007199254740994" {
		t.Fatalf("the page persisted %+v", p)
	}
	if target, err := store.PageID(context.Background(), "Forbidden"); err != nil || target != 0 {
		t.Fatalf("a refused save made the page %d %v", target, err)
	}
}

// TestAPagesCategoriesAreLinks: file-pages puts a page in a category (a new name makes it), and with from it takes the
// page out of that one only. The page lists its categories as part-of links, offers the categories by title (not
// itself) in the options of file-pages, and a day page is never filed.
func TestAPagesCategoriesAreLinks(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Seeds"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages?title=Seeds")), "file-pages"), map[string]string{"category": "Projects"}))
	garden := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden", "body": "See [[Seeds]]."}))
	self := apitest.Href(garden, "self")

	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, self)), "file-pages")
	if !hasOption(file, "category", "Projects") || hasOption(file, "category", "Garden") {
		t.Errorf("file-pages offers %+v: the categories by title, and not the page itself", file.Field("category"))
	}
	for _, cat := range []string{"Projects", "Outdoors"} {
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, self)), "file-pages"), map[string]string{"category": cat}))
	}
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, self)), "file-pages"), map[string]string{"from": "Outdoors"}))
	links := map[string]string{}
	for _, l := range props(apitest.Must(t)(c.Get(ctx, self)))["links"].([]any) {
		l := l.(map[string]any)
		links[l["title"].(string)] = l["kind"].(string)
	}
	if len(links) != 2 || links["Projects"] != "part-of" || links["Seeds"] != "wikilink" {
		t.Errorf("after x the links are %v, want Projects part-of and Seeds wikilink", links)
	}

	day := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "Dug the beds."}))
	if hasAction(apitest.Must(t)(c.Get(ctx, apitest.Href(day, "page"))), "file-pages") {
		t.Error("a day page offers file-pages: a day page is never filed")
	}
}

// TestEveryListOffersTheSamePickBar: the lists of Browse and the Metrics board offer file-pages, and Delete pages
// (tombstone-pages) only on the lists of what can be deleted. A list of metrics or of people has no Delete.
func TestEveryListOffersTheSamePickBar(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Ana"}))
	for title, mime := range map[string]string{"Scan.jpg": "image/jpeg", "Notes.pdf": "application/pdf"} {
		sum := map[string]string{"Scan.jpg": "ab", "Notes.pdf": "cd"}[title]
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": title, "sha256": strings.Repeat(sum, 32), "mime": mime}))
	}
	for _, list := range []struct {
		path   string
		delete bool
	}{
		{"/browse?kind=media", true}, {"/browse?kind=document", true}, {"/browse?kind=page", true},
		{"/browse?kind=metric", false}, {"/browse?kind=person", false}, {"/metrics", false},
	} {
		e := apitest.Must(t)(c.Get(ctx, list.path))
		if !hasAction(e, "file-pages") || hasAction(e, "tombstone-pages") != list.delete {
			t.Errorf("%s offers %s: file-pages always, and tombstone-pages %v", list.path, names(e), list.delete)
		}
	}
}
