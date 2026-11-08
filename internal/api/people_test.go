package api_test

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"aminte/client"
	"aminte/internal/api/apitest"
	"aminte/siren"
)

// TestANewPersonWithTheTitleOfAPlainPageOffersToPromoteIt: the answer 409 links to the page that holds the title (rel
// existing) and, when that page is a plain page, to the promote form of that page with the fields typed (rel promote;
// docs/plans/141-the-access-pass.md, part 2).
func TestANewPersonWithTheTitleOfAPlainPageOffersToPromoteIt(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	create := apitest.Action(t, root, "create-person")
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Sam"}))
	samHref := apitest.Href(sam, "self")
	ana := apitest.Must(t)(c.Do(ctx, create, map[string]string{"title": "Ana"}))

	conflict := func(vals map[string]string) *siren.Entity {
		t.Helper()
		_, err := c.Do(ctx, create, vals)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != http.StatusConflict || ce.Code() != siren.CodeExists {
			t.Fatalf("create-person %v: %v, want 409 %s", vals, err, siren.CodeExists)
		}
		return ce.Entity
	}
	e := conflict(map[string]string{"title": "Sam", "name": "Samuel Ortiz", "birth_day": "1980-02-03"})
	if l := e.LinkByRel("existing"); l == nil || l.Href != samHref || l.Title != "Sam" {
		t.Errorf("the link existing is %+v, want the page %s titled Sam", l, samHref)
	}
	wantPromote := samHref + "?birth_day=1980-02-03&name=Samuel+Ortiz&new=promote"
	if l := e.LinkByRel("promote"); l == nil || l.Href != wantPromote || l.Title != "make it a person, with what you typed" {
		t.Errorf("the link promote is %+v, want %s", l, wantPromote)
	}
	// The address of the link is served, and the page there offers promote. The browser fills the fields from the query.
	apitest.Action(t, apitest.Must(t)(c.Get(ctx, wantPromote)), "promote")
	// A page that is a person already has nothing to promote: only the link to it.
	e = conflict(map[string]string{"title": "Ana"})
	if l := e.LinkByRel("existing"); l == nil || l.Href != apitest.Href(ana, "self") {
		t.Errorf("the link existing of a taken person is %+v, want %s", l, apitest.Href(ana, "self"))
	}
	if l := e.LinkByRel("promote"); l != nil {
		t.Errorf("the 409 of a taken person offers a promotion: %+v", l)
	}
}

// TestPromoteMakesAPersonAndTakesItsDates: through the API, as a client of any surface sends them. A plain page with
// no field becomes a person named as its title. The catalog has no field to, and the writer refuses it.
func TestPromoteMakesAPersonAndTakesItsDates(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Sam"}))
	pier := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Pier"}))
	promote := apitest.Action(t, pier, "promote")
	if promote.Title != "Make it a person" || promote.Field("to") != nil {
		t.Errorf("promote is %q with the field to: %v", promote.Title, promote.Field("to"))
	}
	if _, err := c.Do(ctx, promote, map[string]string{"to": "person"}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("a field the action does not list: %v, want 422", err)
	}
	plain := apitest.Must(t)(c.Do(ctx, promote, nil))
	if person, _ := props(plain)["person"].(map[string]any); plain.Class[0] != "person" && person["name"] != "Pier" {
		t.Errorf("a plain page with no field: class %v, person %v, want a person named Pier", plain.Class, person)
	}
	got := apitest.Must(t)(c.Do(ctx, apitest.Action(t, sam, "promote"), map[string]string{"name": "Samuel Ortiz", "birth_day": "1980-02-03"}))
	if person, _ := props(got)["person"].(map[string]any); person["name"] != "Samuel Ortiz" || person["birth_day"] != "1980-02-03" || person["death_day"] != nil {
		t.Errorf("the person is %v", person)
	}
}

// TestEditPersonThroughTheAPI: the person's page offers the action with the current values, a field sent empty
// removes a date, a field left out stays, and a page that is not a person is refused.
func TestEditPersonThroughTheAPI(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Sam (barber)", "name": "Samuel Ortiz", "birth_day": "1980-02-03"}))
	self := apitest.Href(sam, "self")

	act := apitest.Action(t, sam, "edit-person")
	if act.Href != self+"/person" || act.Title != "Edit name and dates" {
		t.Errorf("the action is %s %q", act.Href, act.Title)
	}
	for _, f := range act.Fields {
		if want := map[string]any{"name": "Samuel Ortiz", "birth_day": "1980-02-03", "death_day": ""}[f.Name]; f.Value != want {
			t.Errorf("the field %s offers %v, want %v", f.Name, f.Value, want)
		}
	}
	// A form sends every field; an empty date removes it.
	apitest.Must(t)(c.Do(ctx, act, map[string]string{"name": "Samuel A. Ortiz", "birth_day": "", "death_day": "2020-01-01"}))
	person := func() map[string]any {
		t.Helper()
		p, _ := props(apitest.Must(t)(c.Get(ctx, self)))["person"].(map[string]any)
		return p
	}
	if p := person(); p["name"] != "Samuel A. Ortiz" || p["birth_day"] != nil || p["death_day"] != "2020-01-01" {
		t.Errorf("the person is %v, want a new name, no birth day and a death day", p)
	}
	// A request that names the name alone leaves the dates. The templated action of /actions has no values filled in.
	catalog := apitest.Must(t)(c.Get(ctx, "/actions"))
	edit := apitest.Action(t, catalog, "edit-person")
	id := strings.TrimPrefix(self, "/pages/")
	apitest.Must(t)(c.Do(ctx, edit, map[string]string{"id": id, "name": "Sam O."}))
	if p := person(); p["name"] != "Sam O." || p["death_day"] != "2020-01-01" {
		t.Errorf("the person is %v, want only the name changed", p)
	}
	if _, err := c.Do(ctx, edit, map[string]string{"id": id, "birth_day": "2021-01-01"}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("a birth after the death: %v, want 422", err)
	}
	if _, err := c.Do(ctx, edit, map[string]string{"id": id, "name": ""}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("an empty name: %v, want 422", err)
	}
	// A page that is not a person offers no such action, and the route refuses it.
	plain := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Plain"}))
	for _, a := range plain.Actions {
		if a.Name == "edit-person" {
			t.Error("a plain page offers edit-person")
		}
	}
	if rec := request(h, "POST", apitest.Href(plain, "self")+"/person", url.Values{"name": {"X"}}.Encode(), map[string]string{"Host": "localhost"}); rec.Code != http.StatusConflict {
		t.Errorf("edit-person on a plain page: %d, want 409", rec.Code)
	}
}

// TestAPeriodPageOrACategoryIsNotOfferedAsAPersonOrAMetric: the schema refuses to change the type of a period page
// (entities_period_page) and of a category, whose part-of links need a plain page (entities_endpoint_types). So the
// 409 of New person offers no promotion of them, and their pages offer no Make it a metric.
func TestAPeriodPageOrACategoryIsNotOfferedAsAPersonOrAMetric(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "2026-W43"}))
	week := apitest.Must(t)(c.Get(ctx, "/pages?title=2026-W43"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Run", "category": "Body"}))
	body := apitest.Must(t)(c.Get(ctx, "/pages?title=Body"))
	for title, e := range map[string]*siren.Entity{"2026-W43": week, "Body": body} {
		rec := request(h, "POST", "/people", url.Values{"title": {title}}.Encode(), map[string]string{"Host": "localhost"})
		if rec.Code != http.StatusConflict || strings.Contains(rec.Body.String(), `"promote"`) {
			t.Errorf("New person %s: %d, and the answer offers a promotion the schema refuses:\n%.600s", title, rec.Code, rec.Body.String())
		}
		if got := apitest.Href(apitest.Must(t)(c.Get(ctx, apitest.Href(e, "self"))), "make-metric"); got != "" {
			t.Errorf("the page %s offers make-metric %q", title, got)
		}
	}
}
