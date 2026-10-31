package siren_test

import (
	"testing"

	"lifelog/siren"
)

// TestAToolLineIsABadge: the name of the tool, the words after it without the arrow and the mark of a link, and
// the address after the arrow when it is one of this server.
func TestAToolLineIsABadge(t *testing.T) {
	for line, want := range map[string]siren.Tool{
		"- `read_day`": {Name: "read_day"},
		"- `open_day` → [/days/2026-10-27](/days/2026-10-27)": {Name: "open_day", Words: "/days/2026-10-27", Href: "/days/2026-10-27"},
		"- `open_day` → [the day](/days/2026-10-27)":          {Name: "open_day", Words: "the day", Href: "/days/2026-10-27"},
		"- `get` → /pages/12":                                 {Name: "get", Words: "/pages/12", Href: "/pages/12"},
		"- `get` → /pages/12 and more":                        {Name: "get", Words: "/pages/12 and more", Href: "/pages/12"},
		"- `fetch` → https://example.org/x":                   {Name: "fetch", Words: "https://example.org/x"},
		"- `fetch` → [x](https://example.org/x)":              {Name: "fetch", Words: "x"},
		"- `get` failed: forbidden":                           {Name: "get", Words: "failed: forbidden"},
		"- `search_pages` words=sleep":                        {Name: "search_pages", Words: "words=sleep"},
		"- not a tool line":                                   {Name: "not a tool line"},
	} {
		if got := siren.ToolLine(line); got != want {
			t.Errorf("ToolLine(%q) = %+v, want %+v", line, got, want)
		}
	}
}

// TestANumberIsWrittenInFull: no exponent, so an id of a million makes the address /pages/1000000.
func TestANumberIsWrittenInFull(t *testing.T) {
	for x, want := range map[float64]string{1000000: "1000000", 70.4: "70.4", 0: "0", -0.5: "-0.5", 1e21: "1000000000000000000000"} {
		if got := siren.Num(x); got != want {
			t.Errorf("Num(%v) = %q, want %q", x, got, want)
		}
	}
}

// TestASubEntityIsAReferenceUntilItCarriesARepresentation: a link with an href and nothing else is a reference.
// Embedding puts the representation of an entity in its place and keeps the rel and the href. The entity of an
// embedded link has the class, the title, the properties, the sub-entities, the actions and the links back.
func TestASubEntityIsAReferenceUntilItCarriesARepresentation(t *testing.T) {
	ref := siren.Link{Rel: []string{"ref"}, Href: "/days/2031-05-14", Title: "a day", Class: []string{"hint"}}
	if ref.Embedded() || !ref.HasRel("ref") || ref.HasRel("item") {
		t.Fatalf("a reference: Embedded %v, HasRel(ref) %v, HasRel(item) %v", ref.Embedded(), ref.HasRel("ref"), ref.HasRel("item"))
	}
	e := &siren.Entity{Class: []string{"day"}, Title: "2031-05-14", Properties: map[string]any{"day": "2031-05-14"},
		Entities: []siren.Link{{Rel: []string{"reading"}, Href: "/measurements/1"}},
		Actions:  []siren.Action{{Name: "capture"}}, Links: []siren.Link{{Rel: []string{"self"}, Href: "/days/2031-05-14"}}, Result: "not carried"}
	got := ref.Embedding(e)
	if !got.Embedded() || got.Href != ref.Href || !got.HasRel("ref") || got.Title != "2031-05-14" || got.Class[0] != "day" {
		t.Errorf("the embedded link: %+v", got)
	}
	back := got.Entity()
	if back.Title != e.Title || back.Class[0] != "day" || len(back.Entities) != 1 || len(back.Actions) != 1 || len(back.Links) != 1 || back.Result != nil {
		t.Errorf("the entity of the embedded link: %+v", back)
	}
	if untitled := ref.Embedding(&siren.Entity{Class: []string{"day"}, Properties: map[string]any{}}); untitled.Title != "a day" {
		t.Errorf("an entity with no title keeps the title of the link: %q", untitled.Title)
	}
	if bare := ref.Embedding(&siren.Entity{Class: []string{"day"}, Title: "x"}); bare.Embedded() {
		t.Error("an entity with no content makes the link a representation")
	}
}
