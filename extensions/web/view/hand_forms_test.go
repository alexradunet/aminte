package view

import (
	"regexp"
	"strings"
	"testing"

	"lifelog/siren"
)

// TestAFixedValueIsAHiddenInput: a form whose fields are all fixed or hidden is hidden inputs and one button, the
// glyph with its aria-label, and nothing else (the × of a category).
func TestAFixedValueIsAHiddenInput(t *testing.T) {
	file := filePages(0, "Sport")
	file.Fields = append(file.Fields, siren.Field{Name: "from", Type: "text", Pages: true})
	html, _ := drawForm(t, file, "ids", "5", "from", "Sport", "outside", "category,move", "class", "tiny", "button", "×", "aria", "Remove from Sport")
	if n := regexp.MustCompile(`name="(\w+)"`).FindAllStringSubmatch(html, -1); len(n) != 2 || n[0][1] != "ids" || n[1][1] != "from" {
		t.Errorf("file-pages sends %v, want ids and from: %s", n, html)
	}
	for _, want := range []string{`class="tiny"`, `<input type="hidden" name="ids" value="5">`, `<input type="hidden" name="from" value="Sport">`,
		`<button class="remove" aria-label="Remove from Sport">×</button>`} {
		if !strings.Contains(html, want) {
			t.Errorf("file-pages lacks %s in %s", want, html)
		}
	}
	if strings.Contains(html, "<label") || strings.Contains(html, "<details") || strings.Contains(html, "<datalist") {
		t.Errorf("file-pages draws a field: %s", html)
	}
}

// TestMoreStarFoldsEveryOptionalField: "*" folds every field that the action does not require, and keeps the
// required ones and the fixed ones out of the fold.
func TestMoreStarFoldsEveryOptionalField(t *testing.T) {
	act := siren.Action{Name: "note", Method: "POST", Href: "/notes", Fields: []siren.Field{
		{Name: "page", Type: "text", Required: true}, {Name: "text", Type: "text", Required: true},
		{Name: "on", Type: "date"}, {Name: "due", Type: "date"}, {Name: "every", Type: "text"}, {Name: "urgent", Type: "number", Options: []string{"0", "1"}}}}
	_, f := drawForm(t, act, "page", "Bank", "more", "*")
	var shown, more []string
	for _, fd := range f.Shown {
		shown = append(shown, fd.Name)
	}
	for _, fd := range f.More {
		more = append(more, fd.Name)
	}
	if strings.Join(shown, ",") != "text" || strings.Join(more, ",") != "on,due,every,urgent" {
		t.Errorf("shown %v, folded %v", shown, more)
	}
}
