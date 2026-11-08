package api

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"aminte/internal/core"
	"aminte/siren"
)

// TestPageActionsByTypeAndState: the actions that a page offers, in order, by its type, its state and its links.
// The kinds are the ones that linkKinds gives for the type. The categories hold the plain page itself, and
// file-pages does not offer a page as its own category. Each list is written by hand.
func TestPageActionsByTypeAndState(t *testing.T) {
	cats := []core.Category{{ID: 3, Title: "Health"}, {ID: 9, Title: "Writing"}}
	filedHere := []core.Edge{{Kind: "wikilink", ID: 4, Title: "Notes", Type: "page"}, {Kind: "part-of", ID: 12, Title: "Essay", Type: "page"}}
	for _, tc := range []struct {
		name  string
		p     core.Page
		kinds []string
		want  []string
	}{
		{"a plain page", core.Page{ID: 9, Type: "page", Title: "Writing", Version: "4"}, []string{"part-of"}, []string{
			"save-body /pages/9/body version:4",
			"promote /pages/9/promote",
			"rename /pages/9/rename",
			"set-cover /pages/9/cover",
			"file-pages /pages/file ids:9 category=Health move=0|1 move:0",
			"tombstone-pages /pages/tombstone ids:9",
		}},
		{"a category page", core.Page{ID: 9, Type: "page", Title: "Writing", Version: "4", In: filedHere}, []string{"part-of"}, []string{
			"create-page /pages category:Writing",
			"save-body /pages/9/body version:4",
			"promote /pages/9/promote",
			"rename /pages/9/rename",
			"set-cover /pages/9/cover",
			"file-pages /pages/file ids:9 category=Health move=0|1 move:0",
			"tombstone-pages /pages/tombstone ids:9",
		}},
		{"a day page", core.Page{ID: 2, Type: "page", Title: "2026-10-16", Day: "2026-10-16", IsDayPage: true, Version: "1"}, nil, []string{
			"save-body /pages/2/body version:1",
			"set-cover /pages/2/cover",
			"tombstone-pages /pages/tombstone ids:2",
		}},
		{"a person", core.Page{ID: 5, Type: "person", Title: "Ana", Version: "2", Person: &core.Person{Name: "Ana Popescu", Birth: "1980-02-03"}}, []string{"part-of"}, []string{
			"save-body /pages/5/body version:2",
			"rename /pages/5/rename",
			"edit-person /pages/5/person name:Ana Popescu birth_day:1980-02-03 death_day:",
			"set-cover /pages/5/cover",
			"file-pages /pages/file ids:5 category=Health|Writing move=0|1 move:0",
			"tombstone-pages /pages/tombstone ids:5",
		}},
		{"a metric", core.Page{ID: 7, Type: "metric", Title: "Sleep", Version: "1", Metric: &core.MetricInfo{Unit: "h", Daily: true}}, []string{"part-of"}, []string{
			"save-body /pages/7/body version:1",
			"rename /pages/7/rename",
			"edit-metric /pages/7/metric daily=0|1 daily:1",
			"set-cover /pages/7/cover",
			"file-pages /pages/file ids:7 category=Health|Writing move=0|1 move:0",
			"tombstone-pages /pages/tombstone ids:7",
		}},
		{"a file", core.Page{ID: 15, Type: "file", Title: "Recipe.txt", Version: "1", File: &core.File{MIME: "text/plain"}}, []string{"part-of"}, []string{
			"save-body /pages/15/body version:1",
			"rename /pages/15/rename",
			"file-pages /pages/file ids:15 category=Health|Writing move=0|1 move:0",
			"tombstone-pages /pages/tombstone ids:15",
		}},
		{"a metric", core.Page{ID: 7, Type: "metric", Title: "weight", Version: "3"}, []string{"part-of"}, []string{
			"save-body /pages/7/body version:3",
			"rename /pages/7/rename",
			"set-cover /pages/7/cover",
			"file-pages /pages/file ids:7 category=Health|Writing move=0|1 move:0",
			"tombstone-pages /pages/tombstone ids:7",
		}},
		{"a tombstoned page", core.Page{ID: 10, Type: "page", Title: "Scratch", Version: "2", DeletedAt: "2026-10-16T09:00:00.000Z", In: filedHere}, nil, []string{
			"revive /pages/10/revive",
		}},
	} {
		var got []string
		for _, a := range pageActions(&tc.p, tc.kinds, cats) {
			got = append(got, actionWords(a))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, strings.Join(got, "\n     "), strings.Join(tc.want, "\n     "))
		}
	}
}

// actionWords is an action as one line: its name, its href, then each field that has options (name=a|b) or a value
// other than a body (name:value), in the order of the catalog.
func actionWords(a siren.Action) string {
	words := []string{a.Name, a.Href}
	for _, f := range a.Fields {
		if len(f.Options) > 0 {
			words = append(words, f.Name+"="+strings.Join(f.Options, "|"))
		}
		if f.Value != nil && f.Name != "body" {
			words = append(words, fmt.Sprintf("%s:%v", f.Name, f.Value))
		}
	}
	return strings.Join(words, " ")
}
