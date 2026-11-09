package api

// The pages: a page by id or title, the search, and the writes on a page.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"aminte/internal/core"
	"aminte/siren"
)

func (h *server) find(r *http.Request, a admitted) (*siren.Entity, error) {
	title := a.v.Get("title")
	id, err := h.s.PageID(r.Context(), title)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, core.NotFound("no page titled %s", title)
	}
	return nil, redirect{siren.PageHref(id)}
}

// randomPage answers with the address of a thing of the kind picked at random (docs/plans/130-the-explore-pass.md,
// part 5); the kind is page when the request names none, and a day is the day view of a day page.
func (h *server) randomPage(r *http.Request, a admitted) (*siren.Entity, error) {
	kind := a.v.Get("kind")
	if kind == "" {
		kind = "page"
	}
	if kind == "day" {
		day, err := h.s.RandomDay(r.Context())
		if err != nil {
			return nil, err
		}
		return nil, redirect{siren.DayHref(day)}
	}
	id, err := h.s.RandomThing(r.Context(), kind)
	if err != nil {
		return nil, err
	}
	return nil, redirect{siren.PageHref(id)}
}

func (h *server) page(r *http.Request) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	e, err := h.pageEntity(r.Context(), id)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (h *server) pageEntity(ctx context.Context, id int64) (*siren.Entity, error) {
	p, err := h.s.PageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{p.Type}, Title: p.Title, Properties: p,
		Links: []siren.Link{link("self", siren.PageHref(id), p.Title), link("index", "/", "Home")}}
	if p.IsDayPage {
		e.Class = append(e.Class, "day-page")
		e.Links = append(e.Links, link("day", siren.DayHref(p.Day), "Day view"))
	}
	if p.File != nil && p.File.Preview {
		e.Links = append(e.Links, link("preview", pictureHref(id), "Picture"))
	}
	if p.Type == "metric" { // the page of a metric links its readings, and its readings link the page
		e.Links = append(e.Links, link("series", siren.MetricHref(p.Title), "Readings"))
	}
	e.Links = append(e.Links, coverLinks(p)...)
	// register-metric turns a plain page into the metric; not a period page or a category (core's promotable)
	if p.Type == "page" && !p.IsDayPage && p.Period == "" && p.DeletedAt == "" && !slices.ContainsFunc(p.In, func(l core.Edge) bool { return l.Kind == "part-of" }) {
		e.Links = append(e.Links, link("make-metric", siren.NewMetricHref(p.Title), "Make it a metric"))
	}
	for _, l := range p.Out {
		e.Entities = append(e.Entities, siren.Link{Rel: []string{l.Kind}, Href: siren.PageHref(l.ID), Title: l.Title, Class: []string{l.Type}})
	}
	// The categories, read once: the options of file-pages, and which of the pages filed here are categories
	// themselves (docs/plans/115-categories-to-browse.md).
	var cats []core.Category
	if !p.IsDayPage {
		if cats, err = h.s.Categories(ctx); err != nil {
			return nil, err
		}
	}
	isCategory := func(id int64) bool {
		return slices.ContainsFunc(cats, func(c core.Category) bool { return c.ID == id })
	}
	for _, l := range p.In {
		class := []string{l.Type}
		if l.Kind == "part-of" && isCategory(l.ID) {
			class = append(class, "category")
		}
		e.Entities = append(e.Entities, siren.Link{Rel: []string{"backlink", l.Kind}, Href: siren.PageHref(l.ID), Title: l.Title, Class: class})
	}
	var kinds []string
	if p.DeletedAt != "" {
		e.Class = append(e.Class, "deleted")
	} else if kinds, err = h.linkKinds(ctx, p.Type, p.IsDayPage); err != nil {
		return nil, err
	}
	e.Actions = pageActions(p, kinds, cats)
	return e, nil
}

// pageActions are the actions a page offers, by its type, its state and its links. kinds are the link kinds it may
// start (linkKinds), and cats are the categories that file-pages offers. A tombstoned page offers revive alone. A
// page that something is part-of is a category page: a new page is made and filed there in one write.
func pageActions(p *core.Page, kinds []string, cats []core.Category) []siren.Action {
	ids := idParam(p.ID)
	if p.DeletedAt != "" {
		return []siren.Action{action("revive", ids, nil)}
	}
	var out []siren.Action
	if slices.ContainsFunc(p.In, func(l core.Edge) bool { return l.Kind == "part-of" }) {
		out = append(out, action("create-page", nil, map[string]any{"category": p.Title}))
	}
	out = append(out, saveBodyAction(p.ID, p.Body, p.Version))
	if !p.IsDayPage {
		if p.Type == "page" {
			out = append(out, action("promote", ids, nil))
		}
		out = append(out, action("rename", ids, nil))
		if p.Person != nil {
			out = append(out, action("edit-person", ids, map[string]any{"name": p.Person.Name, "birth_day": p.Person.Birth, "death_day": p.Person.Death}))
		}
		if p.Metric != nil {
			daily := "0"
			if p.Metric.Daily {
				daily = "1"
			}
			out = append(out, action("edit-metric", ids, map[string]any{"daily": daily}))
		}
	}
	if p.Type != "file" {
		out = append(out, action("set-cover", ids, nil))
	}
	if slices.Contains(kinds, "part-of") { // its categories, as badges (docs/plans/114-one-way-to-do-a-thing.md)
		var titles []string
		for _, c := range cats {
			if c.ID != p.ID {
				titles = append(titles, c.Title)
			}
		}
		out = append(out, withOptions(action("file-pages", nil, map[string]any{"ids": strconv.FormatInt(p.ID, 10), "move": "0"}), "category", titles))
	}
	return append(out, action("tombstone-pages", nil, map[string]any{"ids": strconv.FormatInt(p.ID, 10)}))
}

// coverLinks are the links of a page's cover: cover, its picture, when it has one; choose-cover, the pictures it may
// take, when it may take one (a live page that is not a file).
func coverLinks(p *core.Page) []siren.Link {
	var out []siren.Link
	if p.Cover != nil {
		out = append(out, link("cover", pictureHref(p.Cover.ID), p.Cover.Title))
	}
	if p.Type != "file" && p.DeletedAt == "" {
		out = append(out, link("choose-cover", siren.CoverHref(p.ID), "Set cover photo"))
	}
	return out
}

// linkKinds are the kinds a page of this type may start (link_kinds.from_types), minus `part-of` from a day page
// (D26). links_endpoint_types holds that rule: this is only the offer of file-pages.
func (h *server) linkKinds(ctx context.Context, typ string, dayPage bool) ([]string, error) {
	kinds, err := h.s.LinkKindsFrom(ctx, typ)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range kinds {
		if k == "part-of" && dayPage {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

// searchProps are the properties of the search answer: the words, their order and kind, the pages whose title starts
// with them, the hits of this page and the count of the hits of each kind.
type searchProps struct {
	pageProps
	Q      string            `json:"q"`
	Order  string            `json:"order"`
	Kind   string            `json:"kind"`
	Titles []core.TitleMatch `json:"titles"`
	Hits   []searchHit       `json:"hits"`
}

// searchHit is a hit with the address of its picture: its cover, or a file's own; absent when it has none.
type searchHit struct {
	core.Hit
	Picture string `json:"picture,omitempty"`
}

// searchKindTitles are the words of the chips of a search, by kind.
var searchKindTitles = map[string]string{"all": "All", "day": "Days", "page": "Pages", "person": "People", "file": "Files"}

// searchHref is the address of the search for these words in this order, of this kind. The best match and every kind
// are the defaults, so their address names neither.
func searchHref(q, order, kind string) string {
	v := url.Values{"q": {q}}
	if order != core.SearchBest {
		v.Set("order", order)
	}
	if kind != "" {
		v.Set("kind", kind)
	}
	return "/search?" + v.Encode()
}

func (h *server) search(r *http.Request, a admitted) (*siren.Entity, error) {
	q, kind := a.v.Get("q"), a.v.Get("kind")
	order := core.SearchOrderOf(a.v.Get("order"))
	e := &siren.Entity{Class: []string{"search"}, Title: "Search",
		Links:   []siren.Link{link("self", searchHref(q, order, kind), "Search"), link("index", "/", "Home")},
		Actions: []siren.Action{action("search", nil, map[string]any{"q": q, "order": order, "kind": kind})}}
	if strings.TrimSpace(q) == "" { // a q of only blanks has no words: it answers as the empty search does
		return e, nil
	}
	limit, offset, err := pageOf(r, 50)
	if err != nil {
		return nil, err
	}
	// The pages whose title starts with the words come first, as the names after [[ once did: the search dialog of
	// the browser is the one way to a page by its title (docs/plans/108-the-browser-made-current.md). A page is in one
	// group only, so the hits leave these out.
	titles, err := h.s.TitlesByPrefix(r.Context(), q, 8)
	if err != nil {
		return nil, err
	}
	omit := make([]int64, len(titles))
	for i, m := range titles {
		omit[i] = m.PageID
	}
	pageQuery := url.Values{"q": {q}}
	other, otherTitle := core.SearchNewest, "Newest first"
	if order != core.SearchBest {
		pageQuery.Set("order", order)
		other, otherTitle = core.SearchBest, "Best match"
	}
	if kind != "" {
		pageQuery.Set("kind", kind)
	}
	opts := core.SearchOptions{Order: order, Omit: omit, Kind: kind}
	hits, props, err := readPage(e, "/search", pageQuery, limit, offset, func(limit, offset int) ([]core.Hit, error) {
		return h.s.SearchWith(r.Context(), q, opts, limit, offset)
	})
	if err != nil {
		return nil, err
	}
	e.Links = append(e.Links, link("order", searchHref(q, other, kind), otherTitle))
	// The chips of the kinds (docs/plans/130-the-explore-pass.md, part 6), as Browse has them: All, then each kind.
	for _, k := range append([]string{"all"}, core.SearchKinds()...) {
		chip := k
		if k == "all" {
			chip = ""
		}
		e.Links = append(e.Links, siren.Link{Rel: []string{"kind"}, Href: searchHref(q, order, chip), Title: searchKindTitles[k], Class: []string{k}})
	}
	shown := make([]searchHit, len(hits))
	for i, x := range hits {
		shown[i] = searchHit{Hit: x, Picture: pictureHref(x.Picture)}
		e.Entities = append(e.Entities, link("item", siren.PageHref(x.ID), x.Title))
	}
	e.Properties = searchProps{pageProps: props, Q: q, Order: order, Kind: kind, Titles: titles, Hits: shown}
	return e, nil
}

func (h *server) createPage(r *http.Request, a admitted) (*siren.Entity, error) {
	id, sync, err := h.s.CreatePage(r.Context(), a.src, a.v.Get("title"), a.v.Get("body"), a.v.Get("category"))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(sync)
}

func (h *server) saveBody(r *http.Request, a admitted) (*siren.Entity, error) {
	sync, err := h.s.SaveBody(r.Context(), a.src, a.id, a.v.Get("body"), a.v.Get("version"))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), a.id))(sync)
}

func (h *server) createPerson(r *http.Request, a admitted) (*siren.Entity, error) {
	id, err := h.s.CreatePerson(r.Context(), a.src, a.v.Get("title"), a.v.Get("name"), h.dayNamed(a.v.Get("birth_day")), h.dayNamed(a.v.Get("death_day")))
	if err != nil {
		return nil, offerPromote(err, a.v)
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) promote(r *http.Request, a admitted) (*siren.Entity, error) {
	if err := h.s.Promote(r.Context(), a.src, a.id, a.v.Get("name"), h.dayNamed(a.v.Get("birth_day")), h.dayNamed(a.v.Get("death_day"))); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), a.id)
}

// editPerson sets the fields that the request names. A field left out stays; core.PersonEdit says what an empty one does.
func (h *server) editPerson(r *http.Request, a admitted) (*siren.Entity, error) {
	named := func(field string, read func(string) string) *string {
		if !a.v.Has(field) {
			return nil
		}
		s := read(a.v.Get(field))
		return &s
	}
	same := func(s string) string { return s }
	e := core.PersonEdit{Name: named("name", same), Birth: named("birth_day", h.dayNamed), Death: named("death_day", h.dayNamed)}
	if err := h.s.EditPerson(r.Context(), a.src, a.id, e); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), a.id)
}

// editMetric sets the daily mark of a metric. An unticked checkbox sends nothing, so a missing daily is 0.
func (h *server) editMetric(r *http.Request, a admitted) (*siren.Entity, error) {
	daily := flag(a.v, "daily")
	if err := h.s.EditMetric(r.Context(), a.src, a.id, daily); err != nil {
		return nil, err
	}
	e, err := h.pageEntity(r.Context(), a.id)
	if err != nil {
		return nil, err
	}
	e.Result = "No longer asked for every day."
	if daily {
		e.Result = "Asked for on every day."
	}
	return e, nil
}

// existsOffer is an ExistsError of create-person that the owner can mend in one step: the page that holds the title
// is a plain page, and promote makes it the person, with the fields typed.
type existsOffer struct {
	*core.ExistsError
	promote string // the address of the promote form
	title   string // the words of the link to it
}

func (o *existsOffer) Unwrap() error { return o.ExistsError }

// offerPromote wraps err as an existsOffer when err says that a plain page holds the title of the new person. The
// fields are the ones of the request; a value that is empty is left out.
func offerPromote(err error, v url.Values) error {
	var ex *core.ExistsError
	if !errors.As(err, &ex) || !ex.Plain {
		return err
	}
	q := url.Values{"new": {"promote"}}
	for _, field := range []string{"name", "birth_day", "death_day"} {
		if x := v.Get(field); x != "" {
			q.Set(field, x)
		}
	}
	return &existsOffer{ex, siren.PageHref(ex.ID) + "?" + q.Encode(), "make it a person, with what you typed"}
}

func (h *server) revive(r *http.Request, a admitted) (*siren.Entity, error) {
	if err := h.s.Revive(r.Context(), a.src, a.id); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), a.id)
}

func (h *server) rename(r *http.Request, a admitted) (*siren.Entity, error) {
	to, err := h.s.Rename(r.Context(), a.src, a.id, a.v.Get("title"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), to)
}
