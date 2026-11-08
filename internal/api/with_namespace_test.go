package api

import (
	"testing"

	"aminte/siren"
)

// TestTheActionsOfAnEmbeddedLinkAreInTheNamespace: an embedded link keeps its actions as the entity keeps its own: a
// path gets the prefix of the app, a name gets the name of the app, and an action that leaves the server is dropped.
func TestTheActionsOfAnEmbeddedLinkAreInTheNamespace(t *testing.T) {
	e := &siren.Entity{Entities: []siren.Link{{Rel: []string{"item"}, Href: "/items/1", Properties: map[string]any{"n": 1},
		Actions: []siren.Action{
			{Name: "done-item", Method: "POST", Href: "/items/1/done"},
			{Name: "away", Method: "POST", Href: "http://example.invalid/x"},
		}}}}
	got := inNamespace("app", e).Entities
	if len(got) != 1 || got[0].Href != "/with/app/items/1" {
		t.Fatalf("entities = %+v", got)
	}
	acts := got[0].Actions
	if len(acts) != 1 || acts[0].Name != "app-done-item" || acts[0].Href != "/with/app/items/1/done" {
		t.Errorf("actions = %+v, want only app-done-item at /with/app/items/1/done", acts)
	}
	if got[0].Properties == nil {
		t.Error("the properties of the embedded link are lost")
	}
}
