package view

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"lifelog/siren"
)

// drawForm draws the action with the template form, as a page draws it, with the options of formOf.
func drawForm(t *testing.T, a siren.Action, opts ...string) (string, *formView) {
	t.Helper()
	f, err := formOf(a, opts...)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, "form", f); err != nil {
		t.Fatal(err)
	}
	return b.String(), f
}

// The actions that the tests of the forms draw, written out in the shapes of actions of the catalog: an entity gives
// the view its actions, and the view does not know the catalog. The tests of the catalog itself are in package api.

// filePages is the action file-pages, with the value of its field move; categories are the options of its field
// category.
func filePages(move any, categories ...string) siren.Action {
	return siren.Action{Name: "file-pages", Title: "Put in a category", Method: "POST", Href: "/pages/file", Fields: []siren.Field{
		{Name: "ids", Type: "text", Title: "Page ids, separated by spaces or commas", Required: true},
		{Name: "category", Type: "text", Title: "Category (title)", Required: true, Options: categories, Pages: true},
		{Name: "move", Type: "number", Title: "Move (remove the other categories)", Value: move, Options: []string{"0", "1"}},
	}}
}

// registerMetric is the action register-metric; categories are the options of its field category.
func registerMetric(categories ...string) siren.Action {
	return siren.Action{Name: "register-metric", Owner: true, Title: "New metric", Method: "POST", Href: "/metrics", Fields: []siren.Field{
		{Name: "name", Type: "text", Title: "Name", Required: true},
		{Name: "unit", Type: "text", Title: "Unit"},
		{Name: "note", Type: "text", Title: "Note"},
		{Name: "category", Type: "text", Title: "Category (title)", Options: categories, Pages: true},
	}}
}

// tombstonePages is the action tombstone-pages.
func tombstonePages() siren.Action {
	return siren.Action{Name: "tombstone-pages", Danger: true, Title: "Delete pages", Method: "POST", Href: "/pages/tombstone", Fields: []siren.Field{
		{Name: "ids", Type: "text", Title: "Page ids, separated by spaces or commas", Required: true},
	}}
}

// record is the action record, with the values of its fields day and tz.
func record(day, tz any) siren.Action {
	return siren.Action{Name: "record", Title: "Add a reading", Method: "POST", Href: "/measurements", Fields: []siren.Field{
		{Name: "metric", Type: "text", Title: "Metric", Required: true},
		{Name: "day", Type: "date", Title: "Day", Value: day, Required: true},
		{Name: "value", Type: "number", Title: "Value", Required: true},
		{Name: "taken_at", Type: "text", Title: "Taken at (UTC, 2026-06-09T21:14:03.482Z)"},
		{Name: "tz", Type: "text", Title: "Zone (IANA)", Value: tz},
		{Name: "import_key", Type: "text", Title: "Key"},
	}}
}

// addFile is the action add-file, with the fields that the tests read: its title and its two files.
func addFile() siren.Action {
	return siren.Action{Name: "add-file", Title: "New file", Method: "POST", Href: "/files", Fields: []siren.Field{
		{Name: "title", Type: "text", Title: "Title", Required: true},
		{Name: "original", Type: "file", Title: "The file (read, never stored)"},
		{Name: "preview", Type: "file", Title: "Picture, when the original is not a JPEG, PNG or GIF"},
	}}
}

func TestBrowserFieldPresence(t *testing.T) {
	a := siren.Action{Name: "fields", Method: "POST", Href: "/fields", Fields: []siren.Field{
		{Name: "hidden", Type: "hidden", Value: 0},
		{Name: "optional", Type: "number"},
		{Name: "default", Type: "number", Value: 5},
		{Name: "textarea", Type: "textarea", Value: 0},
		{Name: "choice", Type: "number", Value: 0, Options: []string{"", "0", "1"}},
	}}
	got, _ := drawForm(t, a)
	for _, want := range []string{`name="hidden" value="0"`, `name="optional" value=""`, `name="default" value="5"`, `>0</textarea>`, `<option selected>0</option>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// TestPreviewKeysOnATextArea: a form offers Preview when it has a text area, shown or folded, whatever the area's
// name; a field of another type named body does not make it offer Preview.
func TestPreviewKeysOnATextArea(t *testing.T) {
	for _, c := range []struct {
		name   string
		fields []siren.Field
		opts   []string
		want   bool
	}{
		{"a text area named note", []siren.Field{{Name: "note", Type: "textarea"}}, nil, true},
		{"a folded text area", []siren.Field{{Name: "title", Type: "text", Required: true}, {Name: "note", Type: "textarea"}}, []string{"more", "note"}, true},
		{"an input named body", []siren.Field{{Name: "body", Type: "text"}}, nil, false},
		{"no field", nil, nil, false},
	} {
		f, err := formOf(siren.Action{Name: "x", Method: "POST", Href: "/x", Fields: c.fields}, c.opts...)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Preview(); got != c.want {
			t.Errorf("%s: Preview %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSegmentedIsForAFewShortOptions: a field is drawn as a row of radio buttons only when it has 2 to 5 options,
// none empty and each of 3 characters or fewer; any other field with options is a select. A flag (the options 0 and
// 1) is one checkbox, unless the action requires it.
func TestSegmentedIsForAFewShortOptions(t *testing.T) {
	for _, tc := range []struct {
		options []string
		want    bool
	}{
		{[]string{"1", "2", "3", "4", "5"}, true},
		{[]string{"1", "0"}, true},
		{[]string{"0", "1"}, false},
		{[]string{"yes", "no"}, true},
		{[]string{"1"}, false},
		{[]string{"1", "2", "3", "4", "5", "6"}, false},
		{[]string{"", "0", "1"}, false},
		{[]string{"open", "done", "skipped"}, false},
		{nil, false},
	} {
		if got := segmented(siren.Field{Name: "f", Type: "text", Options: tc.options}); got != tc.want {
			t.Errorf("segmented(%q) = %v, want %v", tc.options, got, tc.want)
		}
	}
	if segmented(siren.Field{Name: "f", Type: "text", Pages: true, Options: []string{"Art", "Gym"}}) {
		t.Error("a field of two short page titles is segmented: it takes a new title too")
	}
	if !segmented(siren.Field{Name: "f", Type: "number", Required: true, Options: []string{"0", "1"}}) {
		t.Error("a required flag is not segmented: a checkbox cannot send 0")
	}
}

// TestAFlagIsOneCheckbox: a field of the options 0 and 1 is one checkbox before the field's title. It sends 1 when it
// is checked and nothing when it is not, which the server reads as 0; a value of 1 draws it checked.
func TestAFlagIsOneCheckbox(t *testing.T) {
	for _, tc := range []struct {
		move string
		want string
	}{
		{"", `<label><input type="checkbox" name="move" value="1"><span>`},
		{"0", `<label><input type="checkbox" name="move" value="1"><span>`},
		{"1", `<label><input type="checkbox" name="move" value="1" checked><span>`},
	} {
		got := markup(drawForm(t, filePages(tc.move)))
		if want := tc.want + "Move (remove the other categories)</span></label>"; !strings.Contains(got, want) || strings.Contains(got, `type="radio"`) {
			t.Errorf("move %q: the form lacks %s, or has radio buttons:\n%s", tc.move, want, got)
		}
	}
}

// TestAFieldOfPageTitlesOffersThemInADatalist: a field of page titles with options is a text input with a datalist
// of the titles, so the reader picks one or types a new one. The datalist's id is the action's name and the field's,
// or the form's id and the field's. With no options it is a plain input; a field with options that does not take a
// page's title is still a select. Titles that look like a flag (0 and 1) in a field that is not required are still
// titles: an input with a datalist, not a checkbox.
func TestAFieldOfPageTitlesOffersThemInADatalist(t *testing.T) {
	file := filePages(nil, "Art", "Gym")
	flagLike := registerMetric("0", "1")
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"named by its action", markup(drawForm(t, file)),
			`<input type="text" name="category" value="" required list="file-pages-category"><datalist id="file-pages-category"><option value="Art"><option value="Gym"></datalist></label>`},
		{"named by its form", markup(drawForm(t, file, "id", "pick")),
			`<input type="text" name="category" value="" required list="pick-category"><datalist id="pick-category"><option value="Art"><option value="Gym"></datalist></label>`},
		{"with no options", markup(drawForm(t, filePages(nil))),
			`<input type="text" name="category" value="" required></label>`},
		{"not a page's title", markup(drawForm(t, siren.Action{Name: "sort", Title: "Sort", Method: "POST", Href: "/pages/7/sort", Fields: []siren.Field{
			{Name: "by", Type: "text", Title: "By", Required: true, Options: []string{"title", "day"}}}})),
			`<select name="by"><option>title</option><option>day</option></select>`},
		{"the titles 0 and 1", markup(drawForm(t, flagLike)),
			`<input type="text" name="category" value="" list="register-metric-category"><datalist id="register-metric-category"><option value="0"><option value="1"></datalist></label>`},
	} {
		if !strings.Contains(tc.got, tc.want) {
			t.Errorf("%s: the form lacks %s:\n%s", tc.name, tc.want, tc.got)
		}
	}
	if got := markup(drawForm(t, filePages(nil))); strings.Contains(got, "datalist") || strings.Contains(got, " list=") {
		t.Errorf("a field with no options has a datalist: %s", got)
	}
}

// markup is the first value of drawForm: the markup, without the formView.
func markup(s string, _ *formView) string { return s }

// TestOrSendsTheFormToASecondAction: the pick bar's form is file-pages with an id, whose ids the boxes outside it
// send, and Delete pages is its second button: it sends the same form to tombstone-pages with no check of the
// category. With no second action, the form has one button.
func TestOrSendsTheFormToASecondAction(t *testing.T) {
	draw := func(f *formView) string {
		t.Helper()
		var b bytes.Buffer
		if err := templates.ExecuteTemplate(&b, "form", f); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	form := func(a *shownAction, opts ...string) *formView {
		t.Helper()
		f, err := formOf(a, opts...)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	file := &shownAction{Action: filePages(nil, "Art")}
	del := &shownAction{Action: tombstonePages()}

	got := draw(form(file, "id", "pick", "outside", "ids", "back", "/browse").Or(form(del)))
	for _, want := range []string{`<form id="pick" method="POST" action="/pages/file" data-action="file-pages">`, `<input type="hidden" name="back" value="/browse">`,
		`<button class="primary">Put in a category</button><button class="danger" formaction="/pages/tombstone" formnovalidate data-action="tombstone-pages">Delete pages</button></form>`} {
		if !strings.Contains(got, want) {
			t.Errorf("the form lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, `name="ids"`) {
		t.Errorf("the form draws the ids that the boxes send: %s", got)
	}

	if got := draw(form(file).Or(nil)); strings.Count(got, "<button") != 1 || strings.Contains(got, "formaction") {
		t.Errorf("a form with no second action has another button: %s", got)
	}
}

// TestALabelGivesAFieldATitleOfItsForm: the option label ("field=words") gives a field a title in this form only, as
// the score form of New metric calls its unit Range. The action keeps its own title, so another form of it, and the
// JSON, still say Unit.
func TestALabelGivesAFieldATitleOfItsForm(t *testing.T) {
	a := registerMetric()
	if got := markup(drawForm(t, a, "label", "unit=Range")); !strings.Contains(got, "<label><span>Range</span>\n"+`<input type="text" name="unit" value="">`) ||
		strings.Contains(got, "Unit") {
		t.Errorf("the label option does not give the unit the title Range:\n%s", got)
	}
	if got := markup(drawForm(t, a)); !strings.Contains(got, "<label><span>Unit</span>") || a.Field("unit").Title != "Unit" {
		t.Errorf("a form with no label option, or the action, lost the action's title Unit:\n%s", got)
	}
}

// TestOptionsSetTheOptionsOfAField: the form option options gives a field its options in the order given, and a
// required field with 1 and 0 is a row of two radios with the words Done and Not done. A name that is no field of the
// action is ignored, as with hide.
func TestOptionsSetTheOptionsOfAField(t *testing.T) {
	a := registerMetric()
	got := markup(drawForm(t, a, "options", "unit=1,0", "options", "nothing=a,b"))
	for _, want := range []string{`<input type="radio" name="unit" value="1"`, `<input type="radio" name="unit" value="0"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the options option lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "nothing") {
		t.Errorf("an unknown field name reached the form:\n%s", got)
	}
}

// TestAFormOfOneFieldDoesNotSayRequired: the word "required" is a mark for the field that a form needs among others.
// In a form of one field, the field is the whole form, so the mark says nothing. The browser still refuses an empty
// box (the attribute required stays). A form of two fields, or of one shown field and one folded, keeps the mark.
func TestAFormOfOneFieldDoesNotSayRequired(t *testing.T) {
	search := siren.Action{Name: "search", Title: "Search", Method: "GET", Href: "/search", Fields: []siren.Field{
		{Name: "q", Type: "search", Title: "Search", Required: true},
	}}
	got := markup(drawForm(t, search, "label", "q=Words"))
	if strings.Contains(got, "required</small>") || !strings.Contains(got, `<span>Words</span>`) || !strings.Contains(got, `name="q" value="" required`) {
		t.Errorf("the form of one field says required, lacks its label, or lost the attribute:\n%s", got)
	}
	if got := markup(drawForm(t, filePages(nil))); strings.Count(got, "<small>required</small>") != 2 {
		t.Errorf("a form of three fields lost the mark on its two required fields:\n%s", got)
	}
	if got := markup(drawForm(t, registerMetric(), "more", "unit")); !strings.Contains(got, "<small>required</small>") {
		t.Errorf("a form of one shown field and one folded field lost the mark:\n%s", got)
	}
	mood := siren.Action{Name: "mood", Title: "Mood", Method: "POST", Href: "/mood", Fields: []siren.Field{
		{Name: "mood", Type: "number", Title: "Mood", Required: true, Options: []string{"1", "2", "3"}},
	}}
	if got := markup(drawForm(t, mood)); strings.Contains(got, "required</small>") || !strings.Contains(got, " required>") {
		t.Errorf("a segmented field alone says required, or lost the attribute:\n%s", got)
	}
}

// TestAnEmptyAutofocusKeepsTheFocusOut: a form the page's link asks for starts in its first field, unless the view
// names the field, or none ("autofocus" ""): the New metric fold draws one action in three forms, and only the first
// takes the focus.
func TestAnEmptyAutofocusKeepsTheFocusOut(t *testing.T) {
	asked := &shownAction{Action: registerMetric(), asked: true}
	for _, tc := range []struct {
		opts []string
		want string
	}{
		{nil, "name"},
		{[]string{"autofocus", "note"}, "note"},
		{[]string{"autofocus", ""}, ""},
	} {
		f, err := formOf(asked, tc.opts...)
		if err != nil {
			t.Fatal(err)
		}
		if f.Focus != tc.want {
			t.Errorf("options %q: the focus is in %q, want %q", tc.opts, f.Focus, tc.want)
		}
	}
}

// TestDangerAndRowsReachTheForm says the button of a destructive action is drawn as one, and that a textarea is
// as tall as its field says: both from the action (Danger, Rows), so no template knows an action or a field by name.
// The catalog marks them (TestDangerIsACatalogAction, in package api).
func TestDangerAndRowsReachTheForm(t *testing.T) {
	tombstone := siren.Action{Name: "tombstone", Danger: true, Title: "Delete", Method: "POST", Href: "/pages/7/tombstone"}
	revive := siren.Action{Name: "revive", Title: "Restore", Method: "POST", Href: "/pages/7/revive"}
	save := siren.Action{Name: "save-body", Title: "Save", Method: "POST", Href: "/pages/7/body", Fields: []siren.Field{
		{Name: "body", Type: "textarea", Title: "Body", Value: "words", Required: true, Rows: 18},
		{Name: "version", Type: "hidden", Title: "Version", Value: "3", Required: true}}}
	capture := siren.Action{Name: "capture", Title: "Add to the day", Method: "POST", Href: "/days/2026-01-01/capture", Fields: []siren.Field{
		{Name: "text", Type: "textarea", Title: "Entry (CommonMark)", Rows: 4},
		{Name: "mood", Type: "number", Title: "Mood", Options: []string{"1", "2", "3", "4", "5"}}}}
	if got, _ := drawForm(t, tombstone); !strings.Contains(got, `<button class="danger">`) {
		t.Errorf("a tombstone draws no danger button: %s", got)
	}
	if got, _ := drawForm(t, revive); strings.Contains(got, "danger") {
		t.Errorf("a revive draws a danger button: %s", got)
	}
	if got, _ := drawForm(t, save); !strings.Contains(got, `name="body" rows="18"`) {
		t.Errorf("a body is not drawn as tall as its field says: %s", got)
	}
	if got, _ := drawForm(t, capture, "autofocus", "text"); strings.Count(got, "autofocus") != 1 ||
		!strings.Contains(got, `name="text" rows="4" autofocus`) {
		t.Errorf("the browser does not start in the field the view named: %s", got)
	}
}

// TestOptionalFieldsFoldUnderMoreFields: the optional fields a view names are drawn inside the form under "More
// fields", with their values, so a browser sends them with no script; a required field is never folded; folded
// fields with a value are drawn open; a folded file field still makes the form multipart
// (docs/plans/095-the-leaf-and-its-margin.md).
func TestOptionalFieldsFoldUnderMoreFields(t *testing.T) {
	more := regexp.MustCompile(`(?s)<details class="more-fields"( open)?><summary>([^<]*)</summary>(.*?)</details>`)
	got, f := drawForm(t, record("2026-10-12", nil), "more", "metric,taken_at,tz,import_key")
	m := more.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no More fields in the form: %s", got)
	}
	if m[1] != "" || m[2] != "3 more fields" {
		t.Errorf("More fields: open %q, summary %q; want closed, %q", m[1], m[2], "3 more fields")
	}
	for _, name := range []string{"taken_at", "tz", "import_key"} {
		if !strings.Contains(m[3], `name="`+name+`"`) {
			t.Errorf("%s is not folded: %s", name, m[3])
		}
	}
	if strings.Contains(m[3], `name="metric"`) || !strings.Contains(got, `name="metric"`) {
		t.Errorf("the required metric is folded or gone: %s", got)
	}
	if !strings.Contains(got, `<input type="date" name="day" value="2026-10-12" required>`) || !f.Primary ||
		!strings.Contains(got, `</details>
<button class="primary">`) || strings.Index(got, "</details>") > strings.Index(got, "<button") {
		t.Errorf("the shown fields, or the button after the folded ones, are not drawn: %s", got)
	}
	got, _ = drawForm(t, record(nil, "Europe/Bucharest"), "more", "taken_at,tz")
	if m := more.FindStringSubmatch(got); m == nil || m[1] != " open" || m[2] != "2 more fields" ||
		!strings.Contains(m[3], `name="tz" value="Europe/Bucharest"`) {
		t.Errorf("a folded field with a value is out of sight: %s", got)
	}
	if got, _ = drawForm(t, addFile(), "more", "preview"); !strings.Contains(got, `enctype="multipart/form-data"`) ||
		!more.MatchString(got) || !strings.Contains(more.FindStringSubmatch(got)[3], `type="file" name="preview"`) {
		t.Errorf("a form with a folded file is not multipart: %s", got)
	}
	if got, _ = drawForm(t, record(nil, nil)); strings.Contains(got, "more-fields") {
		t.Errorf("a form whose view folds nothing has More fields: %s", got)
	}
}

// TestAGroupNamesTheDetailsOfABoxedForm: boxed gives its <details> the name of the option "group", so a browser keeps
// one form of the group open. A form with no group has no name attribute.
func TestAGroupNamesTheDetailsOfABoxedForm(t *testing.T) {
	draw := func(opts ...string) string {
		t.Helper()
		f, err := formOf(tombstonePages(), opts...)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := templates.ExecuteTemplate(&b, "boxed", f); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if got := draw("group", "more"); !strings.HasPrefix(got, `<details class="action" name="more">`) {
		t.Errorf("a grouped form is drawn %.80s, want a details named more", got)
	}
	if got := draw(); !strings.HasPrefix(got, `<details class="action">`) || strings.Contains(got, ` name="more"`) {
		t.Errorf("a form with no group is drawn %.80s, want a details with no name", got)
	}
}

// TestAFieldDrawsItsPlaceholderFromTheCatalog: the example text of a field is the Placeholder of the catalog's field.
// The form helper has no option for it: a template that passes one is refused, so the text has one home. A Prompt
// replaces it in one form (the unit of a metric, known to the page only).
func TestAFieldDrawsItsPlaceholderFromTheCatalog(t *testing.T) {
	a := registerMetric()
	a.Fields[0].Placeholder = "Weight, Mood, Walk"
	if got := markup(drawForm(t, a)); !strings.Contains(got, `placeholder="Weight, Mood, Walk"`) {
		t.Errorf("the catalog's placeholder is not drawn:\n%s", got)
	}
	f, err := formOf(a)
	if err != nil {
		t.Fatal(err)
	}
	if got := markup(drawForm(t, a)); strings.Count(got, "placeholder=") != 1 {
		t.Errorf("a field with no placeholder drew one:\n%s", got)
	}
	if _, err := formOf(a, "placeholder", "name=Weight"); err == nil {
		t.Error("the option placeholder was accepted: it would be a field fixed to a value, in silence")
	}
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, "form", f.Prompt("name", "kg")); err != nil || !strings.Contains(b.String(), `placeholder="kg"`) {
		t.Errorf("Prompt does not replace the placeholder: %v\n%s", err, b.String())
	}
}
