package api

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"aminte/siren"
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

// TestTheCatalogUsesOnlyKnownClassesAndOneToAnAction: every class of an action of the catalog is in the list, and no action has two.
func TestTheCatalogUsesOnlyKnownClassesAndOneToAnAction(t *testing.T) {
	used := map[string]bool{}
	for _, s := range catalog {
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

// TestTheClassOfARowIsWhatTheRowDoes: API.md says what each class does, and a row shows it: its method, its title, whether
// it acts on one resource by {id}, and what it asks a person for (the fields that are not path fields). The test reads
// these from every row of the catalog, so a new action needs no edit here, and the message
// names the row that breaks a rule. The rules:
//
//   - A row with a class is a POST row: a GET reads and changes nothing.
//   - A title that starts with New or Add is a create (one verb for one meaning), and a create has such a title. The
//     one create that asks for nothing, the snapshot of the file, has a plain verb.
//   - A POST on {id} that asks for nothing and is no danger is a toggle. A toggle asks for nothing, or for one field
//     with two options.
//   - A POST on {id} that asks for a field is an edit, unless it has the shape of a toggle. An edit asks for a field.
//   - A danger row has no class: its danger mark draws it.
//   - A POST row that acts on no {id} and asks for nothing has no class (rebuild-search, resync-links),
//     except the rows that are pinned below.
//
// Two rows have the shape of another class, so no rule can tell their class from the shape. Each is pinned by name
// in pinnedClass, and the test fails when a pinned row is gone, so a rename does not drop a pin silently.
//
// The rules leave one thing open, because a row cannot show it: the class edit given to a row that has fields and acts
// on no {id}, such as file-pages. No row of the catalog has that shape with a class.
func TestTheClassOfARowIsWhatTheRowDoes(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range catalog {
		var class string
		if len(s.Class) > 0 {
			class = s.Class[0]
		}
		var onID bool
		var asked []siren.Field
		for _, f := range s.Fields {
			switch {
			case f.In != "path":
				asked = append(asked, f)
			case f.Name == "id":
				onID = true
			}
		}
		aToggle := len(asked) == 0 || len(asked) == 1 && len(asked[0].Options) == 2
		newOrAdd := strings.HasPrefix(s.Title, "New ") || strings.HasPrefix(s.Title, "Add ")
		want, pinned := pinnedClass[s.Name]
		seen[s.Name] = true
		switch {
		case pinned && class != want:
			t.Errorf("%s has the class %q, want %q: the class is pinned in pinnedClass", s.Name, class, want)
		case !pinned && s.Method == "POST" && !onID && len(asked) == 0 && class != "":
			t.Errorf("%s acts on no {id} and asks for nothing: the class is %q, want none", s.Name, class)
		case class != "" && s.Method != "POST":
			t.Errorf("%s has the class %q and the method %s, want POST", s.Name, class, s.Method)
		case newOrAdd && class != "create":
			t.Errorf("%s is titled %q, which starts a create, and has the class %q", s.Name, s.Title, class)
		case class == "create" && !newOrAdd && len(asked) > 0:
			t.Errorf("%s is a create titled %q: want a title that starts with New or Add", s.Name, s.Title)
		case s.Danger && class != "":
			t.Errorf("%s is a danger and has the class %q, want none", s.Name, class)
		case s.Method == "POST" && onID && !s.Danger && len(asked) == 0 && class != "toggle":
			t.Errorf("%s acts on {id} and asks for nothing: the class is %q, want toggle", s.Name, class)
		case s.Method == "POST" && onID && !s.Danger && len(asked) > 0 && !aToggle && class != "edit":
			t.Errorf("%s acts on {id} and asks for %d fields: the class is %q, want edit", s.Name, len(asked), class)
		case class == "edit" && len(asked) == 0:
			t.Errorf("%s is an edit and asks for no field", s.Name)
		case class == "toggle" && !aToggle:
			t.Errorf("%s is a toggle and asks for %d fields, want none or one with two options", s.Name, len(asked))
		}
	}
	for name := range pinnedClass {
		if !seen[name] {
			t.Errorf("pinnedClass names %s, which is in no catalog", name)
		}
	}
}

// pinnedClass are the classes that the shape of a row cannot show. A client places an action by its class (plan 159),
// so a lost or a changed class moves a control on a page.
//
//   - edit-metric asks for one field with two options (daily), which is the shape of a toggle. It is an edit, because
//     it sets a value of the metric.
//   - snapshot is the one row that acts on no {id}, asks for nothing, and has a class: it makes a file, so it is a
//     create. Its title has a plain verb, not New or Add, because no field of it names a new thing.
var pinnedClass = map[string]string{
	"edit-metric": "edit",
	"snapshot":    "create",
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
