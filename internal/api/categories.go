package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"lifelog/siren"
)

// filePages is file-pages: every page named is filed in the category page, in one write (D26). The answer is the
// class filed: the category, whether it was made, the ids, and how many links were added and removed (with from, how many were removed from it), with an item
// link to each page. The result is the sentence that a UI shows after the write.
func (h *server) filePages(r *http.Request, a admitted) (*siren.Entity, error) {
	ids, err := idsOf(a.v["ids"])
	if err != nil {
		return nil, err
	}
	from := a.v.Get("from")
	move := flag(a.v, "move")
	f, err := h.s.FilePages(r.Context(), a.src, ids, a.v.Get("category"), from, move)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"filed"}, Title: "Filed",
		Properties: map[string]any{"category": f.Category, "created": f.Created, "ids": ids, "count": len(ids),
			"added": f.Added, "moved": f.Moved, "removed": f.Removed},
		Links: []siren.Link{link("self", siren.PageHref(f.Category.ID), f.Category.Title), link("category", siren.PageHref(f.Category.ID), f.Category.Title),
			link("index", "/", "Home")}}
	for _, id := range ids {
		e.Entities = append(e.Entities, link("item", siren.PageHref(id), strconv.FormatInt(id, 10)))
	}
	if from != "" {
		e.Result = "Removed " + h.pagesWords(r.Context(), ids, f.Removed) + " from " + f.Category.Title + "."
		return e, nil
	}
	verb := "Put"
	if move {
		verb = "Moved"
	}
	result := verb + " " + h.pagesWords(r.Context(), ids, len(ids)) + " in " + f.Category.Title
	if f.Created {
		result += " (a new category)"
	}
	e.Result = result + "."
	return e, nil
}

// fewPages is how many pages a sentence names by their titles. More pages are counted.
const fewPages = 3

// pagesWords names the pages of ids for a sentence: their titles when there are few, else n of them ("5 pages"). A
// page that cannot be read leaves the count: the sentence is a help, and the write has been done.
func (h *server) pagesWords(ctx context.Context, ids []int64, n int) string {
	if len(ids) > 0 && len(ids) <= fewPages && n == len(ids) {
		titles, _ := h.s.Titles(ctx, ids) // with an error, no titles: the count says it
		switch len(titles) {
		case 0:
		case 1:
			return titles[0]
		default:
			return strings.Join(titles[:len(titles)-1], ", ") + " and " + titles[len(titles)-1]
		}
	}
	if n == 1 {
		return "1 page"
	}
	return strconv.Itoa(n) + " pages"
}
