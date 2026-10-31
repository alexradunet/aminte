package view

import "slices"

// The groups of a search (docs/plans/130-the-explore-pass.md, part 6): with no kind, the hits of the page by kind, in
// the order of the chips that the entity links (rel kind, class the kind; All first).

// hitGroup is the hits of one kind on the page: the words of its chip, the address of its chip and its hits.
type hitGroup struct {
	Title, Href string
	Hits        []any
}

// HitGroups are the hits of the page by kind, in the order of the kind links; a kind with no hit on the page has no
// group.
func (v *view) HitGroups() []hitGroup {
	props, _ := v.Props.(map[string]any)
	byKind := group(props["hits"], "kind")
	var out []hitGroup
	for _, l := range v.LinksOf("kind") {
		if len(l.Class) == 0 {
			continue
		}
		k := l.Class[0]
		if i := slices.IndexFunc(byKind, func(b bucket) bool { return b.Key == k }); i >= 0 {
			out = append(out, hitGroup{Title: l.Title, Href: l.Href, Hits: byKind[i].Items})
		}
	}
	return out
}

// kindIcons are the icons of the kinds of a hit that has no picture: the icons of the sidebar that exist.
var kindIcons = map[string]string{"day": "calendar", "page": "files", "person": "people", "file": "image"}

// kindIcon is the icon of a hit's kind, "" for a kind that has none.
func kindIcon(kind any) string {
	k, _ := kind.(string)
	return kindIcons[k]
}
