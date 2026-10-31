package view

import (
	"slices"
	"testing"
)

// TestADeleteOffersItsUndoOrTheList: the result of tombstone-pages for one page carries one form that restores that
// page and returns to the page the toast is on; for several pages it links to the list of the deleted instead.
func TestADeleteOffersItsUndoOrTheList(t *testing.T) {
	one := resultSentences(map[string]any{"tombstoned": []any{7.0}, "count": 1.0, "revive": "/undo/7"}, clock{}, "/browse?kind=media")
	want := resultForm{Href: "/undo/7", Label: "Undo", Hidden: [][2]string{{"back", "/browse?kind=media"}}}
	if len(one) != 1 || one[0].HTML != "Deleted 1 page." || one[0].Form == nil || one[0].Form.Href != want.Href ||
		one[0].Form.Label != want.Label || !slices.Equal(one[0].Form.Hidden, want.Hidden) {
		t.Errorf("one page: %+v", one)
	}
	many := resultSentences(map[string]any{"tombstoned": []any{7.0, 8.0, 9.0}, "count": 3.0}, clock{}, "/")
	if len(many) != 2 || many[0].HTML != "Deleted 3 pages." || many[0].Form != nil || many[1].Form != nil ||
		many[1].HTML != `<a href="/browse?deleted=1">See the deleted pages</a>.` {
		t.Errorf("three pages: %+v", many)
	}
}
