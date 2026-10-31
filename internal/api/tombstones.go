package api

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"lifelog/internal/core"
	"lifelog/siren"
)

// maxTombstoneIDs bounds one tombstone-pages: a list page shows 200 rows at most.
const maxTombstoneIDs = 500

// idsOf reads the field ids of a bulk action: every value of the field (a browser sends one per checked row),
// each split on commas and spaces (a CLI or an agent sends one value), each an id once. Nothing is a 422, as is a
// word that is not an id or more than maxTombstoneIDs of them.
func idsOf(values []string) ([]int64, error) {
	var ids []int64
	seen := map[int64]bool{}
	for _, v := range values {
		for _, w := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
			id, err := strconv.ParseInt(w, 10, 64)
			if err != nil || id < 1 {
				return nil, core.Invalid("ids: %q is not an id", w)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, core.Invalid("missing field ids")
	}
	if len(ids) > maxTombstoneIDs {
		return nil, core.Invalid("ids: at most %d in one action", maxTombstoneIDs)
	}
	return ids, nil
}

// tombstoneMany is tombstone-pages, the one delete: every id named is tombstoned in one write, or none is. The
// answer is the class tombstoned: the ids, their count, and an item link to each. The result says the count, for a UI
// that shows it and offers the Undo.
func (h *server) tombstoneMany(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	ids, err := idsOf(v["ids"])
	if err != nil {
		return nil, err
	}
	if err := h.s.Tombstone(r.Context(), src, ids...); err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"tombstoned"}, Title: "Deleted",
		Properties: map[string]any{"ids": ids, "count": len(ids)},
		Links:      []siren.Link{link("self", "/", "Home"), link("index", "/", "Home")}}
	for _, id := range ids {
		e.Entities = append(e.Entities, link("item", siren.PageHref(id), strconv.FormatInt(id, 10)))
	}
	result := map[string]any{"tombstoned": ids, "count": len(ids)}
	if len(ids) == 1 { // the address of the revive that undoes it
		result["revive"] = action("revive", idParam(ids[0]), nil).Href
	}
	e.Result = result
	return e, nil
}
