// Package formdoc reads the forms of a page of HTML as a browser reads them. It is for the tests of lifelog-web only:
// they must see the forms that the view draws, not the actions that the view was given, and they must send a form as a
// browser sends it. No program imports it.
//
// It reads the HTML that the view writes (html/template output): attributes in double quotes, every option closed. It is
// not an HTML parser, and it does not read a control that sits outside its form and names it with the attribute form.
package formdoc

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

// Form is one form of a page.
type Form struct {
	// Method is the method of the form in upper case; GET when the form names none.
	Method string
	// Action is the address that the form sends to, as the page wrote it.
	Action string
	// Attr holds every attribute of the form tag, for example data-action and enctype.
	Attr map[string]string
	// Values are the fields that a browser sends when the reader changes nothing: a hidden or text field with its value,
	// a text area with its text, a select with its chosen option (the first when none is chosen), and a checkbox or a
	// radio button only when it is checked.
	Values url.Values
	// Files are the names of the file fields. A browser sends them in a form of the type multipart/form-data.
	Files []string
}

// Multipart reports a form that a browser sends as multipart/form-data.
func (f Form) Multipart() bool { return f.Attr["enctype"] == "multipart/form-data" }

var (
	formTag  = regexp.MustCompile(`(?s)<form\b([^>]*)>(.*?)</form>`)
	control  = regexp.MustCompile(`(?s)<textarea\b([^>]*)>(.*?)</textarea>|<select\b([^>]*)>(.*?)</select>|<input\b([^>]*)>`)
	optionOf = regexp.MustCompile(`(?s)<option\b([^>]*)>(.*?)</option>`)
	attrOf   = regexp.MustCompile(`([A-Za-z_:][-A-Za-z0-9_:.]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?`)
)

// Forms are the forms of page, in the order of the page.
func Forms(page string) []Form {
	var forms []Form
	for _, m := range formTag.FindAllStringSubmatch(page, -1) {
		attr := attributes(m[1])
		f := Form{Method: strings.ToUpper(attr["method"]), Action: attr["action"], Attr: attr, Values: url.Values{}}
		if f.Method == "" {
			f.Method = "GET"
		}
		for _, c := range control.FindAllStringSubmatch(m[2], -1) {
			switch {
			case c[5] != "":
				f.addInput(attributes(c[5]))
			case c[3] != "" || c[4] != "":
				f.addSelect(attributes(c[3]), c[4])
			default:
				f.addTextarea(attributes(c[1]), c[2])
			}
		}
		forms = append(forms, f)
	}
	return forms
}

// Find is the first form of forms that match accepts.
func Find(forms []Form, match func(Form) bool) (Form, bool) {
	for _, f := range forms {
		if match(f) {
			return f, true
		}
	}
	return Form{}, false
}

// attributes reads the attributes of a tag. A name with no value has the value "". The names are in lower case.
func attributes(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrOf.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = html.UnescapeString(m[2] + m[3] + m[4])
	}
	return out
}

func has(attr map[string]string, name string) bool { _, ok := attr[name]; return ok }

func (f *Form) addInput(a map[string]string) {
	name := a["name"]
	if name == "" || has(a, "disabled") {
		return
	}
	switch strings.ToLower(a["type"]) {
	case "file":
		f.Files = append(f.Files, name)
	case "submit", "button", "image", "reset":
	case "checkbox", "radio":
		if has(a, "checked") {
			if !has(a, "value") {
				a["value"] = "on"
			}
			f.Values.Add(name, a["value"])
		}
	default:
		f.Values.Add(name, a["value"])
	}
}

func (f *Form) addTextarea(a map[string]string, text string) {
	if a["name"] == "" || has(a, "disabled") {
		return
	}
	f.Values.Add(a["name"], strings.TrimPrefix(html.UnescapeString(text), "\n")) // HTML drops the first line break
}

func (f *Form) addSelect(a map[string]string, options string) {
	if a["name"] == "" || has(a, "disabled") {
		return
	}
	chosen, found := "", false
	for _, o := range optionOf.FindAllStringSubmatch(options, -1) {
		oa := attributes(o[1])
		value := oa["value"]
		if !has(oa, "value") {
			value = strings.TrimSpace(html.UnescapeString(o[2]))
		}
		if !found || has(oa, "selected") {
			chosen, found = value, true
		}
		if has(oa, "selected") {
			break
		}
	}
	if found {
		f.Values.Add(a["name"], chosen)
	}
}
