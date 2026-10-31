package view

import (
	"net/url"
	"slices"
	"strings"

	"lifelog/siren"
)

// The folds of Browse (docs/plans/118-fold-and-find.md): the category tree folds each branch. It is drawn from the
// props of GET /browse, so the resource and the catalog do not change.

// treeNode is a branch of the category tree: the category, its link, its count, and the categories under it. Open
// says that the current branch is this one or is under it.
type treeNode struct {
	Title, Href, Count string
	NewHref            string // the new-page form of Browse, open, for pages filed in this category
	Current, Open      bool
	Kids               []*treeNode
}

// BrowseTree is the category tree of Browse as branches: each category under the one its path names, in the order
// of the paths.
func (v *view) BrowseTree() []*treeNode {
	props, _ := v.Props.(map[string]any)
	cats, _ := props["categories"].([]any)
	links := v.Embedded("category")
	current := ""
	if in, _ := props["in"].(map[string]any); in != nil {
		current = Num(in["id"])
	}
	byPath := map[string]*treeNode{}
	var top []*treeNode
	for i, x := range cats {
		c, _ := x.(map[string]any)
		path, _ := c["path"].(string)
		title, _ := c["title"].(string)
		n := &treeNode{Title: title, Count: Num(c["count"]), Current: current != "" && Num(c["id"]) == current}
		if i < len(links) {
			n.Href = links[i].Href
			n.NewHref = newPageHref(n.Href)
		}
		byPath[path] = n
		parent := ""
		if j := strings.LastIndex(path, "/"); j >= 0 {
			parent = path[:j]
		}
		if p := byPath[parent]; p != nil && parent != "" {
			p.Kids = append(p.Kids, n)
		} else {
			top = append(top, n)
		}
	}
	var open func(n *treeNode) bool
	open = func(n *treeNode) bool {
		for _, k := range n.Kids {
			if open(k) {
				n.Open = true
			}
		}
		if n.Current && len(n.Kids) > 0 {
			n.Open = true
		}
		return n.Current || n.Open
	}
	for _, n := range top {
		open(n)
	}
	return top
}

// Mentions says in words how often the log mentions a person. mentions and lastDay are the values of a
// row of the list as the template sees them: a JSON number and a day, or nothing.
func (v *view) Mentions(mentions, lastDay any) string {
	n, _ := mentions.(float64)
	if n < 1 {
		return "not mentioned yet"
	}
	words := "mentioned on " + Count(n, "page", "pages")
	if day, _ := lastDay.(string); day != "" {
		words += ", last " + v.clock().day(day)
	}
	return words
}

// The words of the files in Browse (docs/plans/117-one-browse-view.md): the media list is the pictures, the videos
// and the recordings, drawn as tiles; the documents are every other file (a PDF, a text), drawn as rows. add-file
// keeps a file of any kind.

// FileWords says what a row of a list of files is, and when: "picture · Wed 14 May", "recording · no day yet",
// "application/pdf · Mon 12 May" (a file of another kind is named by its type).
func (v *view) FileWords(kind, mime, day any) string {
	words, ok := map[string]string{"image": "picture", "video": "video", "audio": "recording"}[kind.(string)]
	if !ok {
		words, _ = mime.(string)
	}
	if d, _ := day.(string); d != "" {
		return words + " · " + v.clock().day(d)
	}
	return words + " · no day yet"
}

// MediaCount says a count of the media list in words: "3 pictures", "1 video", "no recordings".
func (v *view) MediaCount(n any, one, many string) string {
	if c, _ := n.(float64); c == 0 {
		return "no " + many
	}
	return Count(n, one, many)
}

// newPageHref is the address of the Browse list of a category with the form of create-page open, so a page made
// there is filed in the category.
func newPageHref(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	q := u.Query()
	q.Set("kind", "page")
	q.Set("new", siren.NewActionOf("page"))
	u.RawQuery = q.Encode()
	return u.String()
}

// NewCategoryHref is the address of the list of pages with its Select bar open. A page becomes a category when
// something is filed in it (file-pages), so a new category is made by that bar, not by the New page form.
func (v *view) NewCategoryHref() string {
	return siren.BrowseHref(url.Values{"kind": {"page"}, "new": {"file-pages"}})
}

// NewPageTitledHref is the address of the New page form with the title q filled, for a search that found nothing. It is
// empty when q cannot be a title as it stands: nothing typed, or search syntax (a * or a quote).
func (v *view) NewPageTitledHref(query any) string {
	q := strings.TrimSpace(Num(query))
	if q == "" || strings.ContainsAny(q, `*"`) {
		return ""
	}
	return siren.BrowseHref(url.Values{"kind": {"page"}, "new": {siren.NewActionOf("page")}, "title": {q}})
}

// MakingCategory is true when the page asked for the Select bar to make a new category (NewCategoryHref).
func (v *view) MakingCategory() bool { return v.Asked("new") == "file-pages" }

// NewMetricHref is the address of the New metric form on the Metrics board.
func (v *view) NewMetricHref() string { return siren.NewMetricHref("") }

// crumb is a step of the way to the list: a link, or the place the list is at now (Href empty).
type crumb struct{ Title, Href string }

// Crumbs is the way to the list as steps: each category on the path of the category the list is in, then the
// list itself. The lists Not filed, Ghost pages and Deleted are one step. Everything has none.
func (v *view) Crumbs() []crumb {
	props, _ := v.Props.(map[string]any)
	if in, _ := props["in"].(map[string]any); in != nil {
		path, _ := in["path"].(string)
		cats, _ := props["categories"].([]any)
		links := v.Embedded("category")
		var out []crumb
		for _, title := range strings.Split(path, "/") {
			c := crumb{Title: title}
			for i, x := range cats {
				if m, _ := x.(map[string]any); m["title"] == title && i < len(links) {
					c.Href = links[i].Href
					break
				}
			}
			out = append(out, c)
		}
		out[len(out)-1].Href = ""
		return out
	}
	for _, key := range [...][2]string{{"deleted", "Deleted"}, {"ghosts", "Ghost pages"}, {"unfiled", "Not filed"}} {
		if on, _ := props[key[0]].(bool); on {
			return []crumb{{Title: key[1]}}
		}
	}
	return nil
}

// OtherCategories are the categories of a row of the list without the one the list is in: the badges of the row.
func (v *view) OtherCategories(cats any) []any {
	props, _ := v.Props.(map[string]any)
	in, _ := props["in"].(map[string]any)
	var out []any
	rows, _ := cats.([]any)
	for _, x := range rows {
		if m, _ := x.(map[string]any); in == nil || Num(m["id"]) != Num(in["id"]) {
			out = append(out, x)
		}
	}
	return out
}

// Picked reports a row that the page asks to have checked (?ids=5, as the Move link of a row does).
func (v *view) Picked(id any) bool {
	return slices.Contains(strings.FieldsFunc(v.query.Get("ids"), func(r rune) bool { return r == ' ' || r == ',' }), Num(id))
}

// MoveHref is the address of this list with the file form open and the row checked: the Move link of a row.
func (v *view) MoveHref(id any) string {
	return v.hereWith(url.Values{"new": {"file-pages"}, "ids": {Num(id)}})
}

// ListHere is this list without the form open and the checks: where a write returns to.
func (v *view) ListHere() string { return v.hereWith(nil, "new", "ids") }

// hereWith is the address of this page with the query values set and then these names removed.
func (v *view) hereWith(set url.Values, drop ...string) string {
	u, err := url.Parse(v.Here)
	if err != nil {
		return v.Here
	}
	q := u.Query()
	for k, vs := range set {
		q[k] = vs
	}
	for _, k := range drop {
		q.Del(k)
	}
	if len(q) == 0 {
		return u.EscapedPath()
	}
	return u.EscapedPath() + "?" + q.Encode()
}

// CategoryPaths gives each category's title its path in words ("Family › Cousins"), for the choices of a field of
// categories; a title without a parent says the same as its option, so it is left out.
func (v *view) CategoryPaths() map[string]string {
	props, _ := v.Props.(map[string]any)
	cats, _ := props["categories"].([]any)
	out := map[string]string{}
	for _, x := range cats {
		c, _ := x.(map[string]any)
		title, _ := c["title"].(string)
		if path, _ := c["path"].(string); strings.Contains(path, "/") {
			out[title] = strings.ReplaceAll(path, "/", " › ")
		}
	}
	return out
}
