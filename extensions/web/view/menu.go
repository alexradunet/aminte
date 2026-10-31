package view

import (
	"fmt"
	"slices"

	"lifelog/siren"
)

// The head of a page draws the actions that no template places by name (docs/plans/159-the-class-of-an-action.md). An
// action that a template places (placing.places) is drawn there; a `create` action is a button of the head (Creates);
// a title edit is the pencil beside the title (Pencil); any other action of the entity is in the ⋯ menu. A new action
// of the catalog is on a page with no change of a template.

// placing is the way this view's entity is drawn. A page of a connected app or of the task service (drawnByShape) is
// always drawn by generic: the class is the app's word, and it may be the name of a class of this server.
func (v *view) placing() placing {
	if drawnByShape(v.Path) {
		return placing{template: "generic"}
	}
	return placingOf(v.Entity)
}

// placed reports an action that the template of this view draws in a place of its own.
func (v *view) placed(name string) bool { return slices.Contains(v.placing().places, name) }

// formItem is one action that the head draws apart from its place in the domain: an item of the ⋯ menu (a button in
// the list and a dialog with the form), the pencil or a create button (a button and a popover with the form).
type formItem struct {
	ID      string // the id of the dialog or the popover, which the button names
	Title   string
	Owner   bool
	Note    string // what the action does, above its form: its hint, else its description
	Open    bool   // the page's link asked for this form (?new=<name>): its dialog is open
	Close   string // the words of the button that closes the dialog
	Danger  bool   // the dialog is the confirm step of a danger action: its Cancel button has the focus, not the danger button
	Primary bool   // the first create button of a head: the ember one
	Form    *formView
}

// itemOf is the item of an action whose form is f.
func itemOf(a siren.Action, id string, f *formView) formItem {
	it := formItem{ID: id, Title: a.Title, Owner: a.Owner, Note: a.Hint, Open: f.Asked, Close: "Close", Form: f}
	if it.Note == "" {
		it.Note = a.Description
	}
	if it.Title == "" {
		it.Title = a.Name
	}
	return it
}

// menu is the ⋯ menu in three groups: the actions that edit the resource, the others, and the danger actions, last
// and apart. The order of the entity is kept inside a group.
type menu struct {
	Edit, Other, Danger []formItem
}

// Empty reports a menu with no item: the page draws no ⋯ button.
func (m menu) Empty() bool { return len(m.Edit)+len(m.Other)+len(m.Danger) == 0 }

// Items are the items of the menu in the order it draws them.
func (m menu) Items() []formItem {
	return slices.Concat(m.Edit, m.Other, m.Danger)
}

// Menu is the actions of the entity that the ⋯ menu draws: those that no template places and that are not `create`
// (Creates draws these) or the title edit (Pencil draws it). A `toggle` that no template places is in it too.
func (v *view) Menu() (menu, error) {
	var m menu
	for _, a := range v.Actions {
		if v.placed(a.Name) || slices.Contains(a.Class, "create") || isTitleEdit(a) {
			continue
		}
		f, err := formOf(v.shown(a), v.menuOptions(a)...)
		if err != nil {
			return menu{}, err
		}
		it := itemOf(a, "do-"+a.Name, f)
		switch {
		case a.Danger:
			it.Close, it.Danger = "Cancel", true
			m.Danger = append(m.Danger, it)
		case slices.Contains(a.Class, "edit"):
			m.Edit = append(m.Edit, it)
		default:
			m.Other = append(m.Other, it)
		}
	}
	return m, nil
}

// menuOptions are the form options of an action in the menu. The ids of a delete are filled in already, so the form
// hides them, and a delete returns to the page: a deleted page, and a deleted metric, stay as pages.
func (v *view) menuOptions(a siren.Action) []string {
	switch a.Name {
	case "tombstone-pages":
		return []string{"hide", "ids", "back", v.pageHere()}
	case "edit-metric":
		return []string{"button", "Save"}
	}
	return nil
}

// pageHere is the address of the page of this view's entity: the view of a metric (a series) returns to its page,
// which a deleted metric keeps, and any other page returns to itself.
func (v *view) pageHere() string {
	if props, ok := v.Props.(map[string]any); ok && v.IsSeries() {
		return fmt.Sprintf("/pages/%s", Num(props["id"]))
	}
	return v.ReturnHere()
}

// isTitleEdit reports an `edit` action whose one field apart from the path fields is the title: the rename of a page.
// The head draws it as the pencil beside the title.
func isTitleEdit(a siren.Action) bool {
	if !slices.Contains(a.Class, "edit") {
		return false
	}
	var names []string
	for _, f := range a.Fields {
		if f.In != "path" {
			names = append(names, f.Name)
		}
	}
	return slices.Equal(names, []string{"title"})
}

// Pencil is the title edit of the entity as the head draws it: a button beside the title and a popover under it, with
// the form filled with the current title. Nil when the entity offers none, or when a template places it.
func (v *view) Pencil() (*formItem, error) {
	for _, a := range v.Actions {
		if !isTitleEdit(a) || v.placed(a.Name) {
			continue
		}
		c := v.shown(a)
		c.Fields = slices.Clone(c.Fields)
		for i, f := range c.Fields {
			if f.Name == "title" && Num(f.Value) == "" {
				c.Fields[i].Value = v.Title
			}
		}
		f, err := formOf(c, "autofocus", "title")
		if err != nil {
			return nil, err
		}
		it := itemOf(a, "pencil", f)
		return &it, nil
	}
	return nil, nil
}

// Creates are the `create` actions that no template places, each a button of the head with its form in a popover under
// it. The first is the primary button. The quick-add line of a page drawn by shape is not among them.
func (v *view) Creates() ([]formItem, error) {
	quick := v.QuickAdd()
	var out []formItem
	for _, a := range v.Actions {
		if !slices.Contains(a.Class, "create") || v.placed(a.Name) || (quick != nil && quick.Name == a.Name) {
			continue
		}
		f, err := formOf(v.shown(a), createOptions(a)...)
		if err != nil {
			return nil, err
		}
		it := itemOf(a, "new-"+a.Name, f)
		it.Primary = len(out) == 0
		out = append(out, it)
	}
	return out, nil
}

// createOptions are the form options of a `create` action in the head. The form of a file keeps the fields that a
// browser cannot fill folded, and the form of a reading folds its time and sends a key that makes a form sent twice
// one reading.
func createOptions(a siren.Action) []string {
	switch a.Name {
	case "add-file":
		return []string{"hide", "sha256,mime", "more", "preview,dry_run"}
	case "record":
		return []string{"more", "taken_at,tz", "import_key", newKey()}
	}
	return nil
}
