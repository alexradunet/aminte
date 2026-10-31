package view

import (
	"strings"
	"testing"

	"lifelog/siren"
)

// TestTheFormHelperHasNoLeadOption: the first create action of a head is its primary button, so the option lead is
// refused, as an option of no meaning, and not read as a field fixed to a value.
func TestTheFormHelperHasNoLeadOption(t *testing.T) {
	if _, err := formOf(registerMetric(), "lead", "1"); err == nil {
		t.Error("the option lead was accepted")
	}
}

// TestTheGenericViewDrawsItsScalarPropertiesAsTheMetaLine: the strings, numbers and true booleans of an entity are one
// quiet line under its title, in the order of their names; an object or a list stays a panel, and the panel does not
// repeat the line.
func TestTheGenericViewDrawsItsScalarPropertiesAsTheMetaLine(t *testing.T) {
	e := &siren.Entity{Class: []string{"thing"}, Title: "A thing", Properties: map[string]any{
		"count": 3, "colour": "red", "done": true, "late": false, "note": "", "nested": map[string]any{"depth": 2}, "tags": []any{"a", "b"}}}
	page := drawApp(t, e, "/with/app/")
	if want := `<p class="meta-line">colour: red · count: 3 · done: yes</p>`; !strings.Contains(page, want) {
		t.Errorf("the meta line is not %s:\n%.1500s", want, page)
	}
	panel := page[strings.Index(page, `aria-label="Properties"`):]
	panel = panel[:strings.Index(panel, "</section>")]
	if !strings.Contains(panel, "nested") || !strings.Contains(panel, "tags") || strings.Contains(panel, "colour") || strings.Contains(panel, "count") {
		t.Errorf("the panel is not the object and the list alone:\n%s", panel)
	}
	only := drawApp(t, &siren.Entity{Class: []string{"thing"}, Title: "Plain", Properties: map[string]any{"count": 1}}, "/with/app/")
	if strings.Contains(only, `aria-label="Properties"`) {
		t.Error("an entity with scalar properties alone draws a panel of properties")
	}
}
