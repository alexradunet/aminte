package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/siren"
)

// idOfSelf is the id at the end of an entity's self link.
func idOfSelf(t *testing.T, href string) string {
	t.Helper()
	href, _, _ = strings.Cut(href, "?")
	id := href[strings.LastIndex(href, "/")+1:]
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		t.Fatalf("self %q ends in no id", href)
	}
	return id
}

// TestDeletePagesAtOnce: tombstone-pages tombstones every id named in one write, or none of them. The lists of Browse
// that offer it are TestBrowseActions'.
func TestDeletePagesAtOnce(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	add := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "add-file")
	var ids []string
	for i, title := range []string{"Memo 1.m4a", "Memo 2.m4a", "Memo 3.m4a"} {
		e := apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": title, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+1), 32), "mime": "audio/mp4"}))
		ids = append(ids, idOfSelf(t, apitest.Href(e, "self")))
	}
	del := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=media")), "tombstone-pages")
	if !del.Danger || len(del.Fields) != 1 || del.Fields[0].Name != "ids" || !del.Fields[0].Required {
		t.Fatalf("the media list's delete: %+v", del)
	}
	deleted := func(id string) bool {
		t.Helper()
		return slices.Contains(apitest.Must(t)(c.Get(ctx, "/pages/"+id)).Class, "deleted")
	}

	// Commas and spaces separate the ids; an id named twice counts once.
	done := apitest.Must(t)(c.Do(ctx, del, map[string]string{"ids": ids[0] + ", " + ids[1] + " " + ids[0]}))
	if done.Class[0] != "tombstoned" || props(done)["count"] != json.Number("2") || len(done.Entities) != 2 ||
		done.Entities[0].Href != "/pages/"+ids[0] || done.Entities[1].Href != "/pages/"+ids[1] || deletedCount(done.Result) != "2" {
		t.Fatalf("the answer: %+v %v %v", done.Class, props(done), done.Result)
	}
	if !deleted(ids[0]) || !deleted(ids[1]) || deleted(ids[2]) {
		t.Fatalf("after the delete: %v %v %v", deleted(ids[0]), deleted(ids[1]), deleted(ids[2]))
	}
	if rows := props(apitest.Must(t)(c.Get(ctx, "/browse?kind=media")))["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["title"] != "Memo 3.m4a" {
		t.Errorf("the media list after the delete: %v", rows)
	}

	// An id no page has refuses the whole write: the live page named beside it stays live.
	if _, err := c.Do(ctx, del, map[string]string{"ids": ids[2] + " 424242"}); apitest.Status(err) != 404 {
		t.Fatalf("an id no page has: %v", err)
	}
	if deleted(ids[2]) {
		t.Error("a refused delete tombstoned the live page named beside the missing id")
	}
	// A word that is not an id is a 422; which words are ids is TestIdsOf's.
	if _, err := c.Do(ctx, del, map[string]string{"ids": "abc"}); apitest.Status(err) != 422 {
		t.Errorf("ids abc: %v", err)
	}

	// A page tombstoned already is left as it is; one page is said in the singular.
	if again := apitest.Must(t)(c.Do(ctx, del, map[string]string{"ids": ids[0]})); deletedCount(again.Result) != "1" {
		t.Errorf("a second delete: %v", again.Result)
	}
	// Every writer may run it: it is a danger, not the owner's alone.
	agent := client.InProcess(h, "agent:test")
	apitest.Must(t)(agent.Do(ctx, del, map[string]string{"ids": ids[2]}))
	if !deleted(ids[2]) {
		t.Error("an agent's delete did nothing")
	}
	// A form sends the field once per checked box.
	if _, err := c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+ids[2])), "revive"), nil); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "/pages/tombstone", strings.NewReader(url.Values{"ids": {ids[1], ids[2]}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !deleted(ids[2]) {
		t.Errorf("ids sent once per box: %d %s", rec.Code, rec.Body.String())
	}
}

// deletedCount is the count of the result of a tombstone-pages, as the client decodes it.
func deletedCount(result any) string {
	m, _ := result.(map[string]any)
	return fmt.Sprint(m["count"])
}

// TestTombstonePagesTakesNoFieldOfFilePages: the pick bar of a list posts to file-pages or to tombstone-pages. The
// second takes the field ids alone: a field of the first (category) is 422 for every client and every body type, and
// nothing is deleted.
func TestTombstonePagesTakesNoFieldOfFilePages(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	e := apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "add-file"), map[string]string{"title": "Scan 1.jpg", "sha256": strings.Repeat("07", 32), "mime": "image/jpeg"}))
	id := idOfSelf(t, apitest.Href(e, "self"))
	for name, tc := range map[string]struct{ ctype, body string }{
		"a form": {"application/x-www-form-urlencoded", url.Values{"ids": {id}, "category": {"x"}}.Encode()},
		"JSON":   {"application/json", `{"ids":"` + id + `","category":"x"}`},
	} {
		req := httptest.NewRequest("POST", api.PathOf("tombstone-pages"), strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.ctype)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "unknown field category") {
			t.Errorf("%s with category: %d %.300s", name, rec.Code, rec.Body.String())
		}
	}
	if slices.Contains(apitest.Must(t)(c.Get(ctx, "/pages/"+id)).Class, "deleted") {
		t.Error("a refused delete deleted the page")
	}
}

// TestTheDeleteOfOnePageCarriesTheAddressOfItsRevive: the result of tombstone-pages for one page names the href of
// the revive action of that page, so a client posts the undo there; for two pages there is no such field.
func TestTheDeleteOfOnePageCarriesTheAddressOfItsRevive(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	var pages []*siren.Entity
	for _, title := range []string{"Bank", "Garden", "Orchard"} {
		pages = append(pages, apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": title})))
	}
	one := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, pages[0], "tombstone-pages"), nil))
	gone := apitest.Must(t)(c.Get(t.Context(), "/pages/"+fmt.Sprint(props(pages[0])["id"])))
	if revive := apitest.Action(t, gone, "revive"); one.Result.(map[string]any)["revive"] != revive.Href {
		t.Errorf("the result has revive %v, the action has %s", one.Result, revive.Href)
	}
	ids := fmt.Sprintf("%v %v", props(pages[1])["id"], props(pages[2])["id"])
	two := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, pages[1], "tombstone-pages"), map[string]string{"ids": ids}))
	if _, has := two.Result.(map[string]any)["revive"]; has {
		t.Errorf("a delete of two pages carries revive: %v", two.Result)
	}
}

// TestThePostOfTheReviveOfAPageRestoresIt: the address that the result of the delete names takes a plain POST, and the
// page is live again.
func TestThePostOfTheReviveOfAPageRestoresIt(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	e := apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "create-page"), map[string]string{"title": "Old idea"}))
	id := idOfSelf(t, apitest.Href(e, "self"))
	done := apitest.Must(t)(c.Do(ctx, apitest.Action(t, e, "tombstone-pages"), nil))
	revive, _ := done.Result.(map[string]any)["revive"].(string)
	if revive != "/pages/"+id+"/revive" {
		t.Fatalf("the result names the revive %q", revive)
	}
	rec := request(h, "POST", revive, "", map[string]string{"Host": "localhost"})
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != "/pages/"+id {
		t.Fatalf("the revive: %d, Location %q: %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if slices.Contains(apitest.Must(t)(c.Get(ctx, "/pages/"+id)).Class, "deleted") {
		t.Error("the page is still deleted after the revive")
	}
}
