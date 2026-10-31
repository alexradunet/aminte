package api_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/siren"
)

// TestActionsWithPathFieldsAreFilledByNew: GET /actions?new=NAME&FIELD=VALUE answers the action NAME with its href
// filled in (docs/plans/131-the-pareto-pass.md, part 1): a UI cannot post to a templated href, so it asks the catalog
// for the action it opens. The filled action runs, and the write is done as from the resource's own page. The others
// stay templated.
func TestActionsWithPathFieldsAreFilledByNew(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	values := map[string]string{"id": "7", "day": "2031-05-03"}
	listed := apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions
	filledSome := false
	for _, a := range listed {
		q := url.Values{"new": {a.Name}}
		want := a.Href
		for _, f := range a.Fields {
			if f.In == "path" {
				q.Set(f.Name, values[f.Name])
				want = strings.ReplaceAll(want, "{"+f.Name+"}", values[f.Name])
			}
		}
		if want == a.Href {
			continue
		}
		filledSome = true
		got := apitest.Must(t)(c.Get(t.Context(), "/actions?"+q.Encode()))
		if filled := apitest.Action(t, got, a.Name); filled.Href != want {
			t.Errorf("/actions?%s: the action %s has the href %s, want %s", q.Encode(), a.Name, filled.Href, want)
		}
		for _, other := range got.Actions {
			if other.Name != a.Name && other.Href != apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), other.Name).Href {
				t.Errorf("/actions?%s: the action %s is filled too: %s", q.Encode(), other.Name, other.Href)
			}
		}
	}
	if !filledSome {
		t.Fatal("no action of the catalog has a path field")
	}
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Old name"}))
	id := fmt.Sprint(p.Properties.(map[string]any)["id"])
	rename := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions?new=rename&id="+id)), "rename")
	if rename.Href != "/pages/"+id+"/rename" {
		t.Fatalf("the filled rename posts to %s", rename.Href)
	}
	apitest.Must(t)(c.Do(t.Context(), rename, map[string]string{"title": "New name"}))
	if got := apitest.Must(t)(c.Get(t.Context(), "/pages/"+id)).Title; got != "New name" {
		t.Errorf("the page is titled %q after the rename, want New name", got)
	}
}

// TestTheCatalogCarriesThePlaceholderOfAField: GET /actions says the example of a field as the member placeholder,
// for the CLI, the MCP tools and a browser alike.
func TestTheCatalogCarriesThePlaceholderOfAField(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		if a.Name != "register-metric" {
			continue
		}
		if f := a.Field("name"); f == nil || f.Placeholder != "Weight, Mood, Walk" {
			t.Errorf("register-metric name: %+v", f)
		}
		if f := a.Field("daily"); f == nil || f.Placeholder != "" {
			t.Errorf("register-metric daily: %+v", f)
		}
		return
	}
	t.Error("register-metric is not in the catalog")
}

// TestEveryActionOfARootOrADayIsInTheCatalog: an action that the root or a day offers is an action of GET /actions with
// the same name and method, and the day offers capture and record at their own addresses. A UI that draws a form from
// an entity posts where the catalog says.
func TestEveryActionOfARootOrADayIsInTheCatalog(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "walk"}))
	catalog := apitest.Must(t)(c.Get(ctx, "/actions"))
	day := apitest.Must(t)(c.Get(ctx, "/days/"+fixedToday))
	for _, e := range []*siren.Entity{root, day} {
		for _, offered := range e.Actions {
			if listed := apitest.Action(t, catalog, offered.Name); listed.Method != offered.Method {
				t.Errorf("%s is %s on %s and %s in the catalog", offered.Name, offered.Method, e.Title, listed.Method)
			}
		}
	}
	for name, href := range map[string]string{"capture": "/days/" + fixedToday + "/capture", "record": "/measurements"} {
		if a := apitest.Action(t, day, name); a.Method != "POST" || a.Href != href {
			t.Errorf("the day offers %s as %s %s, want POST %s", name, a.Method, a.Href, href)
		}
	}
}
