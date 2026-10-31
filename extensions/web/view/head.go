package view

import (
	"bytes"
	"html/template"
	"slices"
)

// The head of a page (the template page-head, docs/plans/159-the-class-of-an-action.md) has one quiet line under the
// title, the meta line. These are its items.

// Meta are the items of the meta line, each as HTML: for the generic view the scalar properties as "name: value"; for
// a page, a person, a file and a metric the facts of the page (a person's full name and dates, a file's type and short
// SHA-256) and when it was created. Any other view has none.
func (v *view) Meta() []template.HTML {
	props, _ := v.Props.(map[string]any)
	if v.placing().template == "generic" {
		var out []template.HTML
		for _, it := range scalarItems(props) {
			out = append(out, template.HTML(template.HTMLEscapeString(it)))
		}
		return out
	}
	if !slices.Contains([]string{"page", "metric"}, v.placing().template) {
		return nil
	}
	var out []template.HTML
	esc := template.HTMLEscapeString
	if person, ok := props["person"].(map[string]any); ok {
		if name, _ := person["name"].(string); name != "" {
			out = append(out, template.HTML(esc(name)))
		}
		if day, _ := person["birth_day"].(string); day != "" {
			out = append(out, template.HTML("born "+esc(v.Date(day))))
		}
		if day, _ := person["death_day"].(string); day != "" {
			out = append(out, template.HTML("died "+esc(v.Date(day))))
		}
	}
	if file, ok := props["file"].(map[string]any); ok {
		if mime, _ := file["mime"].(string); mime != "" {
			out = append(out, template.HTML(esc(mime)))
		}
		if sha, _ := file["sha256"].(string); len(sha) >= 12 {
			out = append(out, template.HTML(`<span title="SHA-256 of the original: `+esc(sha)+`">`+esc(sha[:12])+`…</span>`))
		}
	}
	if at, _ := props["created_at"].(string); at != "" {
		out = append(out, "created "+v.When(at))
	}
	return out
}

// Structured are the properties of the generic view that the meta line does not carry: the objects and the lists, a
// panel of their own. Nil when there is none.
func (v *view) Structured() any {
	props, ok := v.Props.(map[string]any)
	if !ok {
		return v.Props
	}
	rest := map[string]any{}
	for name, x := range props {
		switch x.(type) {
		case string, float64, bool:
		default:
			rest[name] = x
		}
	}
	if len(rest) == 0 {
		return nil
	}
	return rest
}

// slotTemplates is the set of the templates, which slot executes from. It is set by init: templates names the function
// slot, so slot cannot name templates in an initializer.
var slotTemplates *template.Template

func init() { slotTemplates = templates }

// slot is the HTML of the template of that name, drawn for data. A template cannot pass a part of its own markup to
// the template that it calls, so the head takes the name of a template for each part that a view adds to it (above the
// title, under it, among the tools).
func slot(name string, data any) (template.HTML, error) {
	var b bytes.Buffer
	if err := slotTemplates.ExecuteTemplate(&b, name, data); err != nil {
		return "", err
	}
	return template.HTML(b.String()), nil
}
