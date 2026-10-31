package api

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/siren"
)

// TestEveryActionClassIsInTheContract: the first column of the table of API.md (Action classes) and siren.ActionClasses
// name the same classes.
func TestEveryActionClassIsInTheContract(t *testing.T) {
	doc, err := os.ReadFile("../../API.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(doc), "\n## Action classes")
	if !ok {
		t.Fatal("API.md has no Action classes section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var documented []string
	row := regexp.MustCompile("^\\| `([a-z]+)` \\|")
	for _, line := range strings.Split(section, "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			documented = append(documented, m[1])
		}
	}
	got := slices.Clone(siren.ActionClasses())
	slices.Sort(got)
	slices.Sort(documented)
	if !slices.Equal(got, documented) {
		t.Errorf("siren.ActionClasses() = %v, the table of API.md has %v", got, documented)
	}
}

// TestTheCatalogUsesOnlyKnownClassesAndOneToAnAction: every class of an action of the catalog (and of the task
// catalog) is in the list, and no action has two.
func TestTheCatalogUsesOnlyKnownClassesAndOneToAnAction(t *testing.T) {
	used := map[string]bool{}
	for _, s := range append(slices.Clone(catalog), taskCatalog...) {
		if len(s.Class) > 1 {
			t.Errorf("%s has %d classes %v, want at most one", s.Name, len(s.Class), s.Class)
		}
		for _, c := range s.Class {
			used[c] = true
			if !slices.Contains(siren.ActionClasses(), c) {
				t.Errorf("%s has the class %q, which is not in siren.ActionClasses()", s.Name, c)
			}
		}
		if got := s.base().Class; !slices.Equal(got, s.Class) {
			t.Errorf("%s: the action has the classes %v, the row has %v", s.Name, got, s.Class)
		}
	}
	for _, c := range siren.ActionClasses() {
		if !used[c] {
			t.Errorf("no action of the catalog has the class %q", c)
		}
	}
}

// TestTheCatalogGivesEachActionItsClass: the classes of the vocabulary (plan 159), by name, in the catalog and in the
// rows of the task service.
func TestTheCatalogGivesEachActionItsClass(t *testing.T) {
	want := map[string]string{
		"create-page": "create", "create-person": "create", "add-file": "create", "register-metric": "create",
		"record": "create", "capture": "create", "snapshot": "create",
		"rename": "edit", "edit-person": "edit", "edit-metric": "edit", "correct": "edit", "save-body": "edit",
		"set-cover": "edit", "settings": "edit", "promote": "edit",
		"revive":   "toggle",
		"add-task": "create", "edit-task": "edit", "complete-task": "toggle", "reopen-task": "toggle",
	}
	for _, s := range append(slices.Clone(catalog), taskCatalog...) {
		var got string
		if len(s.Class) > 0 {
			got = s.Class[0]
		}
		if got != want[s.Name] {
			t.Errorf("%s has the class %q, want %q", s.Name, got, want[s.Name])
		}
	}
}

// TestAnAppActionKeepsAKnownClassAndLosesAnUnknownOne: inNamespace passes the classes that this server lists, on the
// entity and on the action of an embedded link, and drops the others. The merged catalog does the same.
func TestAnAppActionKeepsAKnownClassAndLosesAnUnknownOne(t *testing.T) {
	e := &siren.Entity{
		Actions: []siren.Action{
			{Name: "add", Method: "POST", Href: "/items", Class: []string{"create", "sparkle"}},
			{Name: "weird", Method: "POST", Href: "/weird", Class: []string{"sparkle"}},
		},
		Entities: []siren.Link{{Rel: []string{"item"}, Href: "/items/1", Actions: []siren.Action{
			{Name: "done", Method: "POST", Href: "/items/1/done", Class: []string{"toggle"}},
			{Name: "odd", Method: "POST", Href: "/items/1/odd", Class: []string{"primary"}},
		}}},
	}
	got := inNamespace("app", e)
	if c := got.Actions[0].Class; !slices.Equal(c, []string{"create"}) {
		t.Errorf("the class of app-add = %v, want [create]", c)
	}
	if c := got.Actions[1].Class; len(c) != 0 {
		t.Errorf("the class of app-weird = %v, want none", c)
	}
	acts := got.Entities[0].Actions
	if c := acts[0].Class; !slices.Equal(c, []string{"toggle"}) {
		t.Errorf("the class of app-done = %v, want [toggle]", c)
	}
	if c := acts[1].Class; len(c) != 0 {
		t.Errorf("the class of app-odd = %v, want none", c)
	}
	s := specOfApp("app", siren.Action{Name: "add", Method: "POST", Href: "/items", Class: []string{"sparkle", "create"}})
	if !slices.Equal(s.Class, []string{"create"}) {
		t.Errorf("the class of the row of app-add = %v, want [create]", s.Class)
	}
}

// TestAnAppIsRefusedAnActionWithTwoClasses: API.md says an action has at most one class, so set refuses an app that
// sends two, and it names the action. One known class is accepted.
func TestAnAppIsRefusedAnActionWithTwoClasses(t *testing.T) {
	act := func(class ...string) []siren.Action {
		return []siren.Action{{Name: "classy", Class: class, Method: "POST", Href: "/classy", Title: "Classy"}}
	}
	err := NewApps().set("two", "http://127.0.0.1:1", "Two", act("create", "edit"))
	if err == nil || !strings.Contains(err.Error(), "classy") {
		t.Errorf("two classes: err = %v, want a refusal that names the action", err)
	}
	if err := NewApps().set("one", "http://127.0.0.1:1", "One", act("create")); err != nil {
		t.Errorf("one known class: err = %v, want none", err)
	}
}
