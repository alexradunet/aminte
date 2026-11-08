package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"aminte/client"
	"aminte/internal/api/apitest"
)

// TestAnActionCarriesTheHintOfItsFormAndTheActionsListTheDescription: an action on a resource says what it does in the
// words of its hint. /actions lists every action with the text of the API and no hint.
func TestAnActionCarriesTheHintOfItsFormAndTheActionsListTheDescription(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	hint := "Make a person. The title is the short name that links use. The name is the full name."
	if got := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=person")), "create-person").Hint; got != hint {
		t.Errorf("/browse?kind=person offers the hint %q for create-person, want %q", got, hint)
	}
	listed := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/actions")), "create-person")
	if listed.Hint != "" || !strings.Contains(listed.Description, "Create a person: title is the permanent handle") {
		t.Errorf("/actions lists create-person with the hint %q and the description %q", listed.Hint, listed.Description)
	}
}

// TestARefusedReadingOfAnUnknownMetricLinksToTheFormOfTheMetric: the answer says the metric does not exist and links
// (rel register) to the address that opens the form that adds it, with the name filled.
func TestARefusedReadingOfAnUnknownMetricLinksToTheFormOfTheMetric(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	record := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "record")
	_, err := c.Do(ctx, record, map[string]string{"metric": "Ghost", "day": "2026-10-12", "value": "3"})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusNotFound {
		t.Fatalf("a reading of an unknown metric: %v, want 404", err)
	}
	register := ce.Entity.LinkByRel("register")
	if register == nil || register.Href != "/metrics?name=Ghost&new=register-metric" || register.Title != "Add the metric Ghost" {
		t.Fatalf("the link register is %+v, want /metrics?name=Ghost&new=register-metric", register)
	}
	// The address is served, and the page there offers the action. The browser fills the fields from the query.
	filled := apitest.Must(t)(c.Get(ctx, register.Href))
	apitest.Action(t, filled, "register-metric")
}

// TestFilingNamesFewPagesAndCountsMany: the sentence of file-pages names up to three pages, else counts them.
func TestFilingNamesFewPagesAndCountsMany(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	var ids []string
	for _, title := range []string{"One", "Two", "Three", "Four"} {
		p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": title}))
		ids = append(ids, fmt.Sprint(p.Properties.(map[string]any)["id"]))
	}
	file := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/browse")), "file-pages")
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{ids[:1], "Put One in Box (a new category)."}, {ids[:3], "Put One, Two and Three in Box."}, {ids, "Put 4 pages in Box."},
	} {
		done := apitest.Must(t)(c.Do(t.Context(), file, map[string]string{"ids": strings.Join(tc.ids, ","), "category": "Box"}))
		if done.Result != tc.want {
			t.Errorf("%d pages: %q, want %q", len(tc.ids), done.Result, tc.want)
		}
	}
}
