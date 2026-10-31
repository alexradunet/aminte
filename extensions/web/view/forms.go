package view

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"lifelog/siren"
)

// Action is the entity's action of that name as this page draws it, nil when it is not offered.
func (v *view) Action(name string) *shownAction {
	for _, a := range v.Actions {
		if a.Name == name {
			return v.shown(a)
		}
	}
	return nil
}

// shownAction is an action as a page draws it: a copy of the entity's action, so the entity stays as it is and the
// JSON with it, and the marks of the browser, which the wire does not carry.
type shownAction struct {
	siren.Action
	asked bool // the page's link asked for this form (?new=<name>): open, filled from the query
}

// shown is the action as this page draws it. The page's link can ask for one form (?new=<name>,
// docs/plans/114-one-way-to-do-a-thing.md): it is open, and each field that the query names, but a path field or a
// file, holds the query's value. A value that is not among the field's options is added to them, so the form sends
// it and the action refuses it with its reason (a metric that does not exist): it is never dropped in silence.
func (v *view) shown(a siren.Action) *shownAction {
	c := &shownAction{Action: a}
	if a.Name == "" || v.Asked("new") != a.Name {
		return c
	}
	c.asked = true
	c.Fields = slices.Clone(a.Fields)
	for i, f := range c.Fields {
		if _, named := v.query[f.Name]; named && f.In != "path" && f.Type != "file" && f.Name != "new" {
			c.Fields[i].Value = v.query.Get(f.Name)
			if q := v.query.Get(f.Name); q != "" && len(f.Options) > 0 && !slices.Contains(f.Options, q) {
				c.Fields[i].Options = append(slices.Clone(f.Options), q)
			}
		}
	}
	return c
}

// formView is an action as the "form" template draws it. formOf takes name/value pairs: "button" labels the
// button, "class" styles the form, "autocomplete" sets its autocomplete attribute, "id" names it (the controls outside it name it), "hide" lists fields kept with their
// value but not shown, "outside" lists fields that controls outside the form send (the boxes of a list join the form
// by its id) and that the form does not draw, "autofocus" names the field the browser starts in ("" for none),
// "more" lists optional fields folded under "More fields" (docs/plans/095-the-leaf-and-its-margin.md; "*" folds every
// field that the action does not require), "group" names the exclusive group of a boxed form (the browser keeps one form of a name open), "send" (any word) marks the text areas as the ones whose Enter key sends (enterkeyhint), "back" is the
// page the browser returns to after the write (no field of the action: the server reads it, see backOf), "label"
// ("field=words") gives a field a title in this form only (the score form calls its unit Range), "key"
// ("field=letter") gives a field its access key (the words of an empty field are its Placeholder in the catalog; Prompt
// replaces them for one form), "required" lists fields
// that the form requires though the action does not, "pattern" ("field=regexp") is what the browser accepts in a
// field (a score's range), "aria" is the aria-label of an icon-only
// button that removes something (the × of a list: the button then has the class remove), and any other name fixes a
// field to a value (hidden).
type formView struct {
	*siren.Action
	Button, Class string
	Autocomplete  string // the form's autocomplete attribute, when the form asks for one (Settings)
	ID            string // the form's id, which the controls outside it name (form="pick")
	Focus         string
	Back          string
	NoPreview     bool          // the form has a text area but offers no Preview (the chat column)
	Asked         bool          // the form the page's link asked for (?new=<name>): open
	Opens         bool          // the form names the path fields of a templated action, and opens its form (opener)
	Group         string        // boxed gives its <details> this name: of the forms with one name, one is open
	Send          bool          // the Enter key of a text area sends the form (the chat)
	Primary       bool          // the button is the primary one: a form with fields to fill
	Aria          string        // the aria-label of the button, for a button that is a glyph (the ×)
	Also          *formView     // a second action that the form sends to, by a button of its own (Or)
	Shown         []siren.Field // the fields most writes need
	More          []siren.Field // optional fields, folded; still in the form, so a browser sends them with no script
	Hidden        []siren.Field
	prompts       map[string]string            // field name to placeholder words
	keys          map[string]string            // field name to its access key
	patterns      map[string]string            // field name to the pattern of its value
	labels        map[string]map[string]string // field name to the words of each of its options (Labels)
}

// Prompt gives a field the words that this form shows in its empty box, in place of the catalog's: the unit
// of a metric is known to the page only.
func (f *formView) Prompt(field, words string) *formView {
	if f.prompts == nil {
		f.prompts = map[string]string{}
	}
	f.prompts[field] = words
	return f
}

// Labels gives the options of a field words to show beside their value: a category is offered as its path. The
// value that the form sends stays the option.
func (f *formView) Labels(field string, words map[string]string) *formView {
	if f.labels == nil {
		f.labels = map[string]map[string]string{}
	}
	f.labels[field] = words
	return f
}

// Or is the form f with a second button, after its own, that sends the same form to the action of g (formaction),
// with no check of the fields that f requires: the Delete of a list, beside its Put in a category. g's action takes
// the method of f's and no field that f does not send. With no g, f is as it was.
func (f *formView) Or(g *formView) *formView {
	f.Also = g
	return f
}

// Preview reports a form with a text area: it offers Preview, a second button that sends the form to POST /render
// in a new tab, where the body shows as its page will show it, and nothing is saved.
func (f *formView) Preview() bool {
	if f.NoPreview {
		return false
	}
	for _, fd := range append(f.Shown[:len(f.Shown):len(f.Shown)], f.More...) {
		if fd.Type == "textarea" {
			return true
		}
	}
	return false
}

// Multipart reports a form with a file field, shown or folded: a browser sends it as multipart/form-data.
func (f *formView) Multipart() bool {
	for _, fd := range append(f.Shown[:len(f.Shown):len(f.Shown)], f.More...) {
		if fd.Type == "file" {
			return true
		}
	}
	return false
}

// fieldView is a field as the "field" template draws it: the field, and whether the browser starts in it.
type fieldView struct {
	siren.Field
	Focus   bool
	Key     string            // the field's access key, which the browser's own modifier reaches
	List    string            // the id of the datalist of a field of page titles with options (Pages): see Fields
	Pattern string            // the browser sends the form only when the value matches it
	Send    bool              // the Enter key sends the form (enterkeyhint of a text area)
	Sole    bool              // the only field the form draws: it needs no mark "required"
	Labels  map[string]string // the words shown beside each option of the datalist (formView.Labels)
}

// Fields are the fields the form shows (folded false) or folds under "More fields" (folded true). A field of page
// titles with options offers them in a datalist, whose id is the form's id, or else its action's name, and the
// field's name: a page draws an action in one form, or gives each of its forms an id.
func (f *formView) Fields(folded bool) []fieldView {
	list := f.Shown
	if folded {
		list = f.More
	}
	out := make([]fieldView, len(list))
	sole := len(f.Shown)+len(f.More) == 1
	for i, fd := range list {
		if w, ok := f.prompts[fd.Name]; ok {
			fd.Placeholder = w
		}
		out[i] = fieldView{fd, fd.Name == f.Focus, f.keys[fd.Name], "", f.patterns[fd.Name], f.Send, sole, f.labels[fd.Name]}
		if fd.Pages && len(fd.Options) > 0 {
			out[i].List = cmp.Or(f.ID, f.Name) + "-" + fd.Name
		}
	}
	return out
}

// Spoken reports a field of the two words Done and Not done: the value of a daily metric that has no unit, with the
// options 1 and 0 (the form option "options"). The radios then show the words and not the numbers.
func (f fieldView) Spoken() bool {
	return f.Name == "value" && (slices.Equal(f.Options, []string{"1", "0"}) || slices.Equal(f.Options, []string{"0", "1"}))
}

// Word is the small word of an option of a radio: the words of a mood, or of a Spoken field; else "".
func (f fieldView) Word(option string) string {
	switch {
	case f.Name == "mood":
		return moodWord(option)
	case f.Spoken():
		if option == "1" {
			return "Done"
		}
		return "Not done"
	}
	return ""
}

// MoreOpen reports folded fields of which one has a value: they are drawn open, so no value is out of sight.
func (f *formView) MoreOpen() bool {
	for _, fd := range f.More {
		if Num(fd.Value) != "" {
			return true
		}
	}
	return false
}

// formOptions are the options of a form call, read from the name/value pairs of the template (formView lists them). A
// template cannot build a struct, so the pairs stay its syntax; this is the one place that knows the names.
type formOptions struct {
	button, class, id, back string
	autocomplete, aria      string
	plain                   bool // "plain" was given: the form offers no Preview
	group                   string
	send                    bool // "send" was given
	hasButton               bool // "button" was given, also as ""
	autofocus               string
	hasAutofocus            bool // "autofocus" was given, also as "": the options name the field of the focus, or none
	hide, outside           nameSet
	more, required          nameSet
	labels                  map[string]string   // field name to words
	keys, patterns          map[string]string   // field name to letter, field name to regexp
	options                 map[string][]string // field name to the options that replace the action's own
	fixed                   map[string]string   // any other option: field name to its fixed value
}

// nameSet is a set of field names.
type nameSet map[string]bool

// parseFormOptions reads the pairs of a form call. A list option ("hide": "ids,move") and a "field=words" option
// ("label": "text=Entry") add to the earlier ones of the same name when it is repeated. A "field=words"
// option with no "=" is ignored. The option "options" is of that shape: "value=1,0" gives the field value the options
// 1 and 0, in that order.
func parseFormOptions(pairs []string) (formOptions, error) {
	if len(pairs)%2 != 0 {
		return formOptions{}, fmt.Errorf("options come in name/value pairs")
	}
	o := formOptions{hide: nameSet{}, outside: nameSet{}, more: nameSet{}, required: nameSet{},
		labels: map[string]string{}, keys: map[string]string{}, patterns: map[string]string{},
		options: map[string][]string{}, fixed: map[string]string{}}
	lists := map[string]nameSet{"hide": o.hide, "outside": o.outside, "more": o.more, "required": o.required}
	perField := map[string]map[string]string{"label": o.labels, "key": o.keys, "pattern": o.patterns}
	for i := 0; i < len(pairs); i += 2 {
		name, v := pairs[i], pairs[i+1]
		switch name {
		case "button":
			o.button, o.hasButton = v, true
		case "class":
			o.class = v
		case "autocomplete":
			o.autocomplete = v
		case "aria":
			o.aria = v
		case "group":
			o.group = v
		case "send":
			o.send = v != ""
		case "plain":
			o.plain = v != ""
		case "id":
			o.id = v
		case "back":
			o.back = v
		case "autofocus":
			o.autofocus, o.hasAutofocus = v, true
		case "hide", "outside", "more", "required":
			for _, field := range strings.Split(v, ",") {
				lists[name][field] = true
			}
		case "label", "key", "pattern":
			if field, words, ok := strings.Cut(v, "="); ok {
				perField[name][field] = words
			}
		case "lead":
			return formOptions{}, fmt.Errorf("lead is no option: the first create action of a head is its primary button")
		case "placeholder":
			return formOptions{}, fmt.Errorf("placeholder is no option: the words of an empty field are its Placeholder in the catalog, or Prompt for one form")
		case "options":
			if field, list, ok := strings.Cut(v, "="); ok {
				o.options[field] = strings.Split(list, ",")
			}
		default:
			o.fixed[name] = v
		}
	}
	return o, nil
}

func formOf(x any, opts ...string) (*formView, error) {
	var c shownAction // an action of the entity's list (range .Actions) has no marks
	switch x := x.(type) {
	case *shownAction:
		if x == nil { // the page does not offer the action
			return nil, nil
		}
		c = *x
	case siren.Action:
		c.Action = x
	default:
		return nil, nil
	}
	a := &c.Action
	opens := false
	if o := opener(*a); o != nil {
		a, opens = o, true
	}
	o, err := parseFormOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("form %s: %w", a.Name, err)
	}
	f := &formView{Action: a, Button: a.Title, Class: o.class, Autocomplete: o.autocomplete, Aria: o.aria, ID: o.id, Back: o.back, NoPreview: o.plain, Group: o.group, Send: o.send, Focus: o.autofocus,
		keys: o.keys, patterns: o.patterns}
	if o.hasButton {
		f.Button = o.button
	} else if f.Button == "" {
		f.Button = a.Name
	}
	for _, fd := range a.Fields {
		if v, ok := o.fixed[fd.Name]; ok { // a fixed field keeps its value (the habit's empty unit)
			fd.Value, fd.Type = v, "hidden"
		}
		if list, ok := o.options[fd.Name]; ok {
			fd.Options = list
		}
		if o.required[fd.Name] {
			fd.Required = true
		}
		if l, ok := o.labels[fd.Name]; ok {
			fd.Title = l
		}
		switch {
		case o.outside[fd.Name]:
		case fd.Type == "hidden" || o.hide[fd.Name]:
			f.Hidden = append(f.Hidden, fd)
		case (o.more[fd.Name] || o.more["*"]) && !fd.Required:
			f.More = append(f.More, fd)
		default:
			f.Shown = append(f.Shown, fd)
		}
	}
	f.Primary = len(f.Shown)+len(f.More) > 0
	f.Opens = opens
	f.Asked = c.asked
	if c.asked && !o.hasAutofocus && len(f.Shown) > 0 { // a form the reader asked for starts in its first field
		f.Focus = f.Shown[0].Name
	}
	return f, nil
}

// opener is the form of a templated action that has path fields (GET /actions): a browser cannot fill a path, so
// the form is a GET of /actions that names the action in new and sends its path fields. /actions then answers with
// the action's href filled in, and its form open. Nil when a has no path field.
func opener(a siren.Action) *siren.Action {
	var path []siren.Field
	for _, f := range a.Fields {
		if f.In == "path" {
			f.In = ""
			path = append(path, f)
		}
	}
	if path == nil {
		return nil
	}
	o := a
	o.Method, o.Href, o.Type, o.Danger = "GET", "/actions", "", false
	o.Fields = append([]siren.Field{{Name: "new", Type: "hidden", Value: a.Name}}, path...)
	return &o
}

// segmented reports a field the browser draws as a Segmented choice of the design system, a row of radio buttons:
// 2 to 5 options, none empty and each of 3 characters or fewer (a mood 1 to 5). A field of page titles is never one:
// its options are the titles it offers, and it takes a new title too (a datalist). A flag is never one either: it is
// a checkbox. Any other field with options is a select.
func segmented(f siren.Field) bool {
	if f.Pages || checkbox(f) || len(f.Options) < 2 || len(f.Options) > 5 {
		return false
	}
	for _, o := range f.Options {
		if o == "" || utf8.RuneCountInString(o) > 3 {
			return false
		}
	}
	return true
}

// checkbox reports a flag that the browser draws as one checkbox before the field's title: the options 0 and 1, in
// a field that the action does not require. A checked box sends 1, and a box that is not checked sends nothing,
// which the server reads as 0 (flag). A required flag stays a row of radio buttons, as a checkbox cannot send 0. A
// field of page titles is never one: its options are titles (two categories titled 0 and 1), which a datalist offers.
func checkbox(f siren.Field) bool {
	return !f.Pages && !f.Required && slices.Equal(f.Options, []string{"0", "1"})
}

// SharedForms names, by action, the action whose form a browser also sends to it. The pick bar of a list (pick-bar in
// html/layout.html, formView.Or) sends the form of file-pages to tombstone-pages by a second button, because HTML
// cannot join one checkbox to two forms. The API allows these fields to a browser form only.
var SharedForms = map[string]string{"tombstone-pages": "file-pages"}
