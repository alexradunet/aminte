package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"aminte/internal/core"
	"aminte/siren"
)

// TestPaged: paged trims a read of limit+1 rows to the page, gives back limit and offset as properties, and adds the
// link prev when offset > 0 and next when the read had one more row. A link keeps the query of the list and gives it
// its own limit and offset.
func TestPaged(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rows          []int
		path, query   string
		limit, offset int
		want          string // the rows, the properties (limit and offset), then each link as rel=href
	}{
		{"the first page of more", []int{1, 2, 3, 4}, "/browse", "kind=person", 3, 0,
			"[1 2 3] {3 0} self=/x next=/browse?kind=person&limit=3&offset=3"},
		{"a middle page", []int{4, 5, 6, 7}, "/browse", "kind=person", 3, 3,
			"[4 5 6] {3 3} self=/x prev=/browse?kind=person&limit=3&offset=0 next=/browse?kind=person&limit=3&offset=6"},
		{"the last page", []int{7}, "/browse", "kind=person", 3, 6,
			"[7] {3 6} self=/x prev=/browse?kind=person&limit=3&offset=3"},
		{"a page past the end", nil, "/browse", "kind=person", 3, 9,
			"[] {3 9} self=/x prev=/browse?kind=person&limit=3&offset=6"},
		{"an offset that is not a step of limit", []int{1, 2}, "/days", "", 3, 2,
			"[1 2] {3 2} self=/x prev=/days?limit=3&offset=0"},
		{"one page, all the rows", []int{1, 2, 3}, "/days", "", 3, 0,
			"[1 2 3] {3 0} self=/x"},
		{"a query with its own limit and offset", []int{1, 2, 3, 4}, "/search", "q=needleword&limit=3&offset=3", 3, 3,
			"[1 2 3] {3 3} self=/x prev=/search?limit=3&offset=0&q=needleword next=/search?limit=3&offset=6&q=needleword"},
	} {
		q, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		before := q.Encode()
		e := &siren.Entity{Links: []siren.Link{link("self", "/x", "This")}}
		rows, props := paged(e, tc.rows, tc.path, q, tc.limit, tc.offset)
		got := fmt.Sprint(rows, " ", props)
		for _, l := range e.Links {
			got += fmt.Sprintf(" %s=%s", strings.Join(l.Rel, ","), l.Href)
		}
		if got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
		if q.Encode() != before {
			t.Errorf("%s: paged changed the query of the list to %s", tc.name, q.Encode())
		}
	}
}

// TestReadPage: readPage asks the read for one row more than the page, so the page is trimmed and the next link
// exists; a failed read gives its error and adds no link.
func TestReadPage(t *testing.T) {
	var asked [2]int
	read := func(limit, offset int) ([]int, error) {
		asked = [2]int{limit, offset}
		return []int{1, 2, 3}, nil
	}
	e := &siren.Entity{}
	rows, props, err := readPage(e, "/days", nil, 2, 4, read)
	if err != nil {
		t.Fatal(err)
	}
	if asked != [2]int{3, 4} {
		t.Errorf("the read was asked for limit %d offset %d, want 3 and 4", asked[0], asked[1])
	}
	if fmt.Sprint(rows, props) != "[1 2] {2 4}" || len(e.Links) != 2 {
		t.Errorf("page %v props %v links %v", rows, props, e.Links)
	}
	e = &siren.Entity{}
	failed := func(int, int) ([]int, error) { return nil, fmt.Errorf("no read") }
	if _, _, err := readPage(e, "/days", nil, 2, 4, failed); err == nil || len(e.Links) != 0 {
		t.Errorf("a failed read: err %v, links %v", err, e.Links)
	}
}

// TestNumberReadsADecimalNumberOnly: a field is a number in decimal: a sign, digits with one point, an exponent. The
// float syntax of Go also takes hexadecimal numbers, underscores, NaN and Inf. A client never means these.
func TestNumberReadsADecimalNumberOnly(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
	}{{"70", 70}, {"-0.5", -0.5}, {"+2", 2}, {".5", 0.5}, {"4.", 4}, {"1e3", 1000}, {"1.5E-2", 0.015}, {"0", 0}} {
		got, err := number(url.Values{"v": {tc.in}}, "v")
		if err != nil || got == nil || *got != tc.want {
			t.Errorf("number(%q)=%v,%v want %v", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"NaN", "nan", "Inf", "-Inf", "+Inf", "Infinity", "0x1p-2", "0x10", "1_000", " 4", "4 ", "4,5", "\u0664\u0665", "1.8e308", ".", "e3", "1e", "1e+", "--1", "1.2.3", "-", "+"} {
		got, err := number(url.Values{"v": {in}}, "v")
		if err == nil || got != nil || err.Error() != fmt.Sprintf("v %q is not a number", in) {
			t.Errorf("number(%q)=%v,%v want the refusal", in, got, err)
		}
	}
	if got, err := number(url.Values{}, "v"); got != nil || err != nil {
		t.Errorf("an empty field: %v,%v want nil,nil", got, err)
	}
}

// TestFormReadsAJSONBodyAsFormFields: a JSON body becomes the fields of a form. A string is itself, a number, a
// boolean, an array and an object are their JSON text, null is absent, the last value of a duplicate key is kept,
// and only one object is a body.
func TestFormReadsAJSONBodyAsFormFields(t *testing.T) {
	read := func(body string) (url.Values, error) {
		r := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return form(r)
	}
	for _, tc := range []struct{ body, key, want string }{
		{`{"a":1}`, "a", "1"}, {`{"a":1e21}`, "a", "1e+21"}, {`{"a":true}`, "a", "true"}, {`{"a":[1,2]}`, "a", "[1,2]"},
		{`{"a":{"b":1}}`, "a", `{"b":1}`}, {`{"a":"x","a":"y"}`, "a", "y"},
	} {
		v, err := read(tc.body)
		if err != nil || v.Get(tc.key) != tc.want {
			t.Errorf("%s: %q,%v want %q", tc.body, v.Get(tc.key), err, tc.want)
		}
	}
	if v, err := read(`{"a":null}`); err != nil || v.Has("a") {
		t.Errorf("null: %v,%v want no field", v, err)
	}
	v, _ := read(`{"a":true}`)
	if flag(v, "a") {
		t.Errorf("flag of true: true, want false: only 1 is yes (the options check refuses the rest)")
	}
	if _, err := read(`{"a":"x"}{"b":"y"}`); err == nil || !strings.Contains(err.Error(), "expected one JSON object") {
		t.Errorf("two objects: %v", err)
	}
	for _, body := range []string{`[1]`, `"s"`, `null`} {
		_, err := read(body)
		var e *core.Error
		if !errors.As(err, &e) || e.Status != http.StatusBadRequest {
			t.Errorf("%s: %v want status 400", body, err)
		}
	}
}
