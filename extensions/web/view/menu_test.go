package view

import (
	"strings"
	"testing"

	"lifelog/siren"
)

// TestAPageOfAnAppDrawsEachActionOnceByItsClass: an app names no template, so its actions are drawn by their class: the
// first create with one required text is the quick-add, another create is a button of the head, an edit action and an
// action with no class are in the menu, and a danger action is last. Each is drawn once.
func TestAPageOfAnAppDrawsEachActionOnceByItsClass(t *testing.T) {
	act := func(name, class string, danger bool, fields ...siren.Field) siren.Action {
		a := siren.Action{Name: name, Title: name, Method: "POST", Href: "/with/app/" + name, Danger: danger, Fields: fields}
		if class != "" {
			a.Class = []string{class}
		}
		return a
	}
	e := &siren.Entity{Title: "App", Actions: []siren.Action{
		act("drop", "", true), act("quick", "create", false, textField("text", true)),
		act("tweak", "edit", false, textField("text", false)), act("plain", "", false),
		act("bulk", "create", false, textField("a", true), textField("b", true)), act("flip", "toggle", false),
	}}
	page := drawApp(t, e, "/with/app/")
	for _, a := range e.Actions {
		if n := strings.Count(page, `data-action="`+a.Name+`"`); n != 1 {
			t.Errorf("%s is drawn %d times, want once", a.Name, n)
		}
	}
	for _, menu := range []string{"tweak", "plain", "flip", "drop"} {
		if !strings.Contains(page, `commandfor="do-`+menu+`"`) {
			t.Errorf("%s is not in the menu", menu)
		}
	}
	list := page[strings.Index(page, `class="menu-list"`):]
	if !(strings.Index(list, "tweak") < strings.Index(list, "plain") && strings.Index(list, "plain") < strings.Index(list, "flip") && strings.Index(list, "flip") < strings.Index(list, ">drop<")) {
		t.Errorf("the menu is not edit, the others, danger:\n%.600s", list)
	}
	// The focus of a danger dialog starts on Cancel: showModal() would focus the danger button, and Enter would delete.
	if n := strings.Count(page, " autofocus>"); n != 1 || !strings.Contains(page, `command="close" autofocus>Cancel<`) {
		t.Errorf("%d autofocus buttons, want one: the Cancel of the danger dialog", n)
	}
	if strings.Contains(page, `commandfor="do-quick"`) || strings.Contains(page, `commandfor="do-bulk"`) || !strings.Contains(page, `popovertarget="new-bulk"`) || !strings.Contains(page, `id="new-bulk" popover`) {
		t.Error("a create action is in the menu, or the second create has no button")
	}
}
