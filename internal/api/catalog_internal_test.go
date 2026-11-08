package api

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"aminte/siren"
)

// TestTheCatalogMarksTheFieldOfAPageTitle: the catalog says which field takes a page's title (Pages), so a client
// can offer the names of the pages: one field of each action that names a page (file-pages names two: the category
// to put the pages in, and the one to take them out of).
func TestTheCatalogMarksTheFieldOfAPageTitle(t *testing.T) {
	for _, tc := range []struct{ action, fields string }{
		{"create-page", "category"}, {"find", "title"}, {"file-pages", "category,from"},
	} {
		var marked []string
		for _, fd := range action(tc.action, nil, nil).Fields {
			if fd.Pages {
				marked = append(marked, fd.Name)
			}
		}
		if strings.Join(marked, ",") != tc.fields {
			t.Errorf("%s marks %v as a page's title, want [%s]", tc.action, marked, tc.fields)
		}
	}
}

// TestTheCatalogHasNoPlacesAndNoFreeLinks: a place is a plain page (D16), and the only kind that link and unlink took
// was at. A wikilink comes from a body and a part-of from file-pages, so no action writes a link directly.
func TestTheCatalogHasNoPlacesAndNoFreeLinks(t *testing.T) {
	for _, a := range catalog {
		switch a.Name {
		case "create-place", "link", "unlink":
			t.Errorf("the catalog has the action %s", a.Name)
		}
		if a.Name == "promote" && slices.ContainsFunc(a.Fields, func(f siren.Field) bool { return f.Name == "to" }) {
			t.Error("promote has a field to: it makes a person alone")
		}
	}
}

// TestEveryNumericOptionIsANumber: the MCP server sends an option of a number field as a JSON number, unchecked, so
// the catalog's own literals are checked here, once.
func TestEveryNumericOptionIsANumber(t *testing.T) {
	for _, s := range catalog {
		for _, fd := range s.Fields {
			if fd.Type != "number" {
				continue
			}
			for _, o := range fd.Options {
				if _, err := strconv.ParseFloat(o, 64); err != nil {
					t.Errorf("%s.%s: the option %q is not a number", s.Name, fd.Name, o)
				}
			}
		}
	}
}

// TestEveryTextAreaOfTheCatalogIsBodyOrText: the long texts of the API have two names, body and text. A UI that previews
// a text (the browser extension, POST /render of its own) reads these two names, so each text area of the catalog has
// one of them.
func TestEveryTextAreaOfTheCatalogIsBodyOrText(t *testing.T) {
	areas := 0
	for _, s := range catalog {
		for _, fd := range s.Fields {
			if fd.Type == "textarea" {
				areas++
				if fd.Name != "body" && fd.Name != "text" {
					t.Errorf("%s: the text area %s is not body or text, which a preview reads", s.Name, fd.Name)
				}
			}
		}
	}
	if areas == 0 {
		t.Error("the catalog has no text area: the test checks nothing")
	}
}

// TestNoActionHasAFieldNamedFormOrBack: a browser form sends its id in form and the page to return to in back. The
// browser extension keeps both to itself, and the core refuses back as an unknown field, so no action of the catalog
// has a field of either name.
func TestNoActionHasAFieldNamedFormOrBack(t *testing.T) {
	for _, s := range catalog {
		for _, fd := range s.Fields {
			if fd.Name == "form" || fd.Name == "back" {
				t.Errorf("%s has a field named %s, which the browser extension sends for itself", s.Name, fd.Name)
			}
		}
	}
}

// TestDangerIsACatalogAction keeps the destructive-action set honest: it names nothing the catalog does not
// have, and it is what a browser draws from — never an action's name read by a template.
func TestDangerIsACatalogAction(t *testing.T) {
	dangerous := map[string]bool{}
	for _, s := range catalog {
		if s.Danger {
			dangerous[s.Name] = true
		}
	}
	if len(dangerous) == 0 {
		t.Error("no action is destructive: the set lost its contents")
	}
	for _, name := range []string{"tombstone-pages", "retract"} {
		if !dangerous[name] {
			t.Errorf("%s hides or retracts what is there: it is not marked dangerous", name)
		}
	}
	for _, name := range []string{"revive", "capture", "save-body"} {
		if dangerous[name] {
			t.Errorf("%s writes or restores: it must not be marked dangerous", name)
		}
	}
}
