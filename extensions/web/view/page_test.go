package view

import (
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"lifelog/siren"
)

// TestNumWritesNumbersInFull keeps an id a path can carry: the JSON client reads every number as a float64.
func TestNumWritesNumbersInFull(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{1000000.0, "1000000"}, {123456789012.0, "123456789012"}, {71.1, "71.1"}, {0.0000001, "0.0000001"},
		{-2.5, "-2.5"}, {nil, ""}, {"text", "text"}, {int64(7), "7"}, {true, "true"},
	} {
		if got := Num(c.v); got != c.want {
			t.Errorf("num(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// TestPageViewIsAFunctionOfItsEntityAndFacts: the view of a page is a function of its entity and its facts, and of
// nothing else. ?new= opens its form, filled from the query. The entity does not change.
func TestPageViewIsAFunctionOfItsEntityAndFacts(t *testing.T) {
	e := &siren.Entity{Class: []string{"page"}, Title: "Diet", Properties: map[string]any{"id": 4}, Actions: []siren.Action{
		{Name: "save-body", Method: "POST", Href: "/pages/4/body", Fields: []siren.Field{{Name: "body", Type: "textarea", Value: "v1"}, {Name: "version", Type: "hidden", Value: "3"}}},
		{Name: "rename", Method: "POST", Href: "/pages/4/rename", Fields: []siren.Field{{Name: "title", Type: "text"}}},
	}}
	u, err := url.Parse("/pages/4?feedback=abc&new=rename&title=Food")
	if err != nil {
		t.Fatal(err)
	}
	sv := &Shell{Today: "2026-10-16"}
	v, err := pageView(e, Facts{URL: u, Shell: sv})
	if err != nil {
		t.Fatal(err)
	}
	if v.Path != "/pages/4" || v.Here != "/pages/4?new=rename&title=Food" || v.Shell != sv {
		t.Errorf("path %q, here %q, shell %v: want the facts given", v.Path, v.Here, v.Shell)
	}
	v, err = pageView(e, Facts{URL: u})
	if err != nil {
		t.Fatal(err)
	}
	if v.Shell != nil {
		t.Errorf("with no shell: shell %v", v.Shell)
	}
	if rename := v.Action("rename"); !rename.asked || rename.Fields[0].Value != "Food" {
		t.Errorf("the asked action: %+v", rename)
	}
	if save := v.Action("save-body"); save.asked {
		t.Errorf("an action that was not asked: %+v", save)
	}
	if e.Actions[0].Fields[0].Value != "v1" || e.Actions[1].Fields[0].Value != nil {
		t.Errorf("pageView changed the entity's actions: %+v", e.Actions)
	}
}

// TestHereDropsTheReceipt: a form returns to the page it is on, but never with the receipt that page already took.
func TestHereDropsTheReceipt(t *testing.T) {
	for raw, want := range map[string]string{
		"/days?feedback=abc&limit=5": "/days?limit=5",
		"/pages/4?feedback=abc":      "/pages/4",
		"/":                          "/",
		"/pages/Caf%C3%A9":           "/pages/Caf%C3%A9",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := hereOf(u); got != want {
			t.Errorf("hereOf(%s) = %s, want %s", raw, got, want)
		}
	}
}

// TestDayPartsSplitTheViewByPart: each row of a day's view goes to the part of the page that draws it, in the order of
// the view: the day page's body to Journal (also an empty body), a page to Pages (edited or not).
// A reading's row, and a row that is not an object, go to no part.
func TestDayPartsSplitTheViewByPart(t *testing.T) {
	row := func(what, detail string) any { return map[string]any{"what": what, "detail": detail} }
	for _, c := range []struct {
		name  string
		props any
		want  dayParts
	}{
		{"no properties", nil, dayParts{}},
		{"no view", map[string]any{}, dayParts{}},
		{"an empty view", map[string]any{"view": []any{}}, dayParts{}},
		{"a day page with an empty body", map[string]any{"view": []any{row("day page", "")}}, dayParts{Journal: []string{""}}},
		{"every part", map[string]any{"view": []any{row("day page", "Ran"), row("page", "A"),
			row("page (edited)", "B"), row("weight", "70 kg"), row("page", "C")}},
			dayParts{Journal: []string{"Ran"}, Pages: []writtenPage{{"A", false}, {"B", true}, {"C", false}}}},
		{"pages and no day page", map[string]any{"view": []any{row("page (edited)", "B")}}, dayParts{Pages: []writtenPage{{"B", true}}}},
		{"rows that are not objects", map[string]any{"view": []any{"x", 3.0, nil, row("page", "Sintra")}}, dayParts{Pages: []writtenPage{{"Sintra", false}}}},
	} {
		if got := (&view{Props: c.props}).DayParts(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestCategoriesAreThePartOfLinks: the categories of a page or a metric are its part-of links, in their order; a link
// of another kind is not a category.
func TestCategoriesAreThePartOfLinks(t *testing.T) {
	health := map[string]any{"kind": "part-of", "title": "Health"}
	sport := map[string]any{"kind": "part-of", "title": "Sport"}
	lisbon := map[string]any{"kind": "follows", "title": "Lisbon"}
	for _, c := range []struct {
		name  string
		props any
		want  []any
	}{
		{"no properties", nil, nil},
		{"no links", map[string]any{"links": []any{}}, nil},
		{"no part-of link", map[string]any{"links": []any{lisbon}}, nil},
		{"two categories", map[string]any{"links": []any{health, lisbon, sport}}, []any{health, sport}},
	} {
		if got := (&view{Props: c.props}).Categories(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestBacklinksLeaveOutPartOf: the backlinks of a page are grouped by kind, in the order of their first appearance,
// and the part-of links are not among them: Filed here lists those.
func TestBacklinksLeaveOutPartOf(t *testing.T) {
	link := func(kind, title string) any { return map[string]any{"kind": kind, "title": title} }
	a, b, c, d := link("wikilink", "A"), link("part-of", "B"), link("follows", "C"), link("wikilink", "D")
	for _, tc := range []struct {
		name  string
		props any
		want  []bucket
	}{
		{"no properties", nil, nil},
		{"only part-of", map[string]any{"backlinks": []any{b}}, nil},
		{"every kind", map[string]any{"backlinks": []any{a, b, c, d}}, []bucket{{"wikilink", []any{a, d}}, {"follows", []any{c}}}},
	} {
		if got := (&view{Props: tc.props}).Backlinks(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAnErrorCodePicksItsWords: the first words and the tone of each error code of API.md.
func TestAnErrorCodePicksItsWords(t *testing.T) {
	for code, want := range map[string]alertView{
		"stale_version":        {"Not saved. The page changed since you opened it.", false},
		"exists":               {"Not created.", false},
		"forbidden":            {"Refused.", true},
		"cross_origin":         {"Refused.", true},
		"token_required":       {"Refused.", true},
		"unauthorized":         {"Refused.", true},
		"too_large":            {"Not done.", true},
		"timeout":              {"Not done.", true},
		"internal":             {"Not done.", true},
		"snapshot_refused":     {"Not taken.", false},
		"restore_check_failed": {"Taken, but the restore check failed.", true},
		"unavailable":          {"Not done. The app is not running.", false},
		"locked":               {"Locked. Unlock this browser with the PIN, then try again.", false},
		"upstream":             {"Not done.", true},
		"invalid":              {},
		"bad_request":          {},
		"not_found":            {},
		"conflict":             {},
	} {
		if got := alertOf(code); got != want {
			t.Errorf("alertOf(%q) = %+v, want %+v", code, got, want)
		}
	}
	for _, code := range siren.Codes() {
		hasWords := alertOf(code) != alertView{}
		if inList := slices.Contains(messageIsTheAlert, code); hasWords == inList {
			t.Errorf("code %q: has words %v, is in messageIsTheAlert %v; it must be exactly one", code, hasWords, inList)
		}
	}
	for _, words := range []string{alertOf("stale_version").Words, alertOf("forbidden").Words} {
		if !strings.HasSuffix(words, ".") {
			t.Errorf("%q is not a sentence", words)
		}
	}
}
