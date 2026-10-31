package view

import (
	"slices"
	"sort"
	"strings"

	"lifelog/siren"
)

// The generic view reads the shape of an entity and the class of its actions, and never the name of an app. The three
// rules are in extensions/README.md, "How the browser draws a page of an app". The code below holds them.

// drawnByShape says that the page at path is drawn by the generic view, by the shape of its entity and not by its
// class: a page of a connected app (/with/NAME) and a page of the task service (/tasks). The class of such a page is
// the word of the service, and it may be the name of a class of this server.
func drawnByShape(path string) bool {
	return strings.HasPrefix(path, "/with/") || path == "/tasks" || strings.HasPrefix(path, "/tasks/")
}

// QuickAdd is the first action of the entity that a browser draws as one line at the top of the page: its class is
// create, it writes (it is not a GET), it has no path field, and exactly one of its fields is required and has the
// type text. Nil when
// no action has that shape. Only a page drawn by shape (drawnByShape) has one: the catalog page of this server lists every
// action once, in its own order.
func (v *view) QuickAdd() *siren.Action {
	if !drawnByShape(v.Path) {
		return nil
	}
	for i, a := range v.Actions {
		if quickAdd(a) {
			return &v.Actions[i]
		}
	}
	return nil
}

func quickAdd(a siren.Action) bool {
	if a.Method == "GET" || !slices.Contains(a.Class, "create") {
		return false
	}
	required := 0
	for _, f := range a.Fields {
		switch {
		case f.In == "path":
			return false // a browser cannot fill a path in one line: opener draws it
		case f.Type == "hidden" || !f.Required:
		case f.Type == "text":
			required++
		default:
			return false
		}
	}
	return required == 1
}

// SideLinks are the links of the entity that a wide screen draws as a side column: all but self.
func (v *view) SideLinks() []siren.Link {
	var out []siren.Link
	for _, l := range v.Links {
		if !slices.Contains(l.Rel, "self") {
			out = append(out, l)
		}
	}
	return out
}

// IsHere reports a link to the page the browser is on (its query does not count).
func (v *view) IsHere(href string) bool {
	path, _, _ := strings.Cut(href, "?")
	return path == v.Path
}

// roundAction reports an action of a row that a browser draws as a small round button: its class is toggle, and it
// has no field to fill. Any other action of a row is boxed.
func roundAction(a siren.Action) bool {
	return slices.Contains(a.Class, "toggle") && len(a.Fields) == 0
}

// rowChips are the scalar properties of an embedded link as "name: value" (scalarItems).
func rowChips(l siren.Link) []string {
	shape, err := jsonShape(l.Properties)
	props, _ := shape.(map[string]any)
	if err != nil {
		return nil
	}
	return scalarItems(props)
}

// scalarItems are the scalar properties as "name: value", in the order of the names: a string that is not empty, a
// number, a true boolean. A list, an object, a null, an empty string and false make no item. A row draws them as chips
// and the head of a page as its meta line.
func scalarItems(props map[string]any) []string {
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		switch x := props[name].(type) {
		case string:
			if x != "" {
				out = append(out, name+": "+x)
			}
		case float64:
			out = append(out, name+": "+Num(x))
		case bool:
			if x {
				out = append(out, name+": yes")
			}
		}
	}
	return out
}
