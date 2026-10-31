// Package view draws an entity of the API as a page of HTML for a browser: a template per class of entity, and the
// generic template for any other class. It reads nothing: a page is a function of its entity, the facts of its
// request and the refused form that it shows (Draw). It imports no store (core or db), and a test holds this. The API
// (internal/api) calls Draw, and serves the fonts, the manifest and the icons from Files.
package view

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"lifelog/internal/text"
	"lifelog/siren"
)

// The browser view is a template per class of entity (html/*.html), and the generic one for any other class:
// properties, links, and every action as a form. A template is a view of the entity the JSON client gets: it
// reads the properties by their JSON names and places the entity's own actions, so a form is shown only where
// the API offers the action. A form posts, and the server answers with a redirect.
//
// The look is the Almanac design system (docs/plans/092-the-app-shell.md) bound in Hearthwood, the materials of the
// owner's Balaur design system (docs/plans/096-bound-in-hearthwood.md), in two stylesheets of this application's
// own; no framework is vendored and nothing is fetched from another host. base.css holds the tokens and styles
// ordinary HTML by element (its forms, tables, buttons and print sheet, light and dark); app.css holds only what
// this app's own vocabulary needs (the shell, its components, .wikilink, .chart), and a test keeps the two apart.
// The two are one cascade in four layers (tokens, base, components, utilities), declared once in base.css, and
// every rule is in logical properties (docs/plans/108-the-browser-made-current.md).
// The three faces are embedded files that /assets/fonts/ serves. There are two scripts. chat.js helps the chat
// column and returns the focus to the ⋯ button; lock.js (docs/plans/160-the-lock.md) is on the page only when the lock is on. Every form works without them (docs/plans/146-settings-and-the-chat-column.md). What a page does beyond its
// links and forms, the browser does by itself (docs/plans/108-the-browser-made-current.md).
//
// Files are the files of html/: the templates, the two stylesheets, the script, the icons (one SVG and two PNG), the manifest,
// and the fonts with their licences.
//
//go:embed html/*.html html/*.css html/*.js html/*.svg html/*.png html/*.webmanifest html/*.json html/fonts/*.woff2 html/fonts/*.txt
var Files embed.FS

// baseCSS and appCSS are the two stylesheets, in that order: the look of ordinary HTML, then this app's own.
var (
	baseCSS = template.CSS(MustFile("base.css"))
	appCSS  = template.CSS(versioned(MustFile("app.css")))
)

// assetURL is a picture of app.css: url(/assets/NAME), with NAME a file of html/.
var assetURL = regexp.MustCompile(`url\(/assets/([a-z0-9-]+\.png)\)`)

// versioned adds ?v= and the first 12 hex digits of the SHA-256 of the file to each picture of a sheet. /assets/
// serves a picture as immutable for a year at its name, so a new picture needs a new address: without one, a browser
// draws the positions of a new icon sheet on the old sheet that it keeps. The route reads the path only.
func versioned(css []byte) string {
	return assetURL.ReplaceAllStringFunc(string(css), func(u string) string {
		name := assetURL.FindStringSubmatch(u)[1]
		sum := sha256.Sum256(MustFile(name))
		return fmt.Sprintf("url(/assets/%s?v=%x)", name, sum[:6])
	})
}

// MustFile is the file of Files that has the name name in html/.
func MustFile(name string) []byte {
	b, err := Files.ReadFile("html/" + name)
	if err != nil {
		panic(err) // the files of html/ are embedded (the sheets, the manifest, the icons): one missing is a build defect
	}
	return b
}

// templates is the set of the templates in html/*.html, with the functions that they call.
var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"basecss":       func() template.CSS { return baseCSS },
	"appcss":        func() template.CSS { return appCSS },
	"rel":           func(r []string) string { return strings.Join(r, " ") },
	"markdown":      markdown,
	"markdownCover": markdownCover,
	"periodWords":   periodWords,
	"mark":          mark,
	"rejected":      rejected,
	"chart":         chart,
	"titleHref":     titleHref,
	"weekday":       weekday,
	"unitWords":     unitWords,
	"form":          formOf,
	"newKey":        newKey,
	"group":         group,
	"weeks":         weeks,
	"dayOfMonth":    dayOfMonth,
	"dict":          dict,
	"monthBand":     monthBand,
	"num":           Num,
	"moodWord":      moodWord,
	"count":         Count,
	"pathesc":       url.PathEscape,
	"icon":          icon,
	"shortDay":      shortDay,
	"addDays":       addDays,
	"nameWords":     nameWords,
	"monthName":     monthName,
	"crumbs":        func(path string) string { return strings.ReplaceAll(path, "/", " › ") }, // "Family/Cousins" to "Family › Cousins"
	"add":           func(a, b int) int { return a + b },
	"segmented":     segmented,
	"checkbox":      checkbox,
	"alert":         alertOf,
	"beyond":        beyond,
	"kindIcon":      kindIcon,
	"waiting":       waiting,
	"toolChip":      siren.ToolLine,
	"roundAction":   roundAction,
	"rowChips":      rowChips,
	"slot":          slot,
}).ParseFS(Files, "html/*.html"))

// newKey is a new key for one draw of a capture form (D28): 26 characters of crypto/rand, so a form sent twice is one
// capture and the next draw is another.
func newKey() string { return rand.Text() }

// dict is the arguments of a sub-template that needs more than one value: name/value pairs, each name a string.
func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("dict: %d arguments, want name/value pairs", len(kv))
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: name %v is not a string", kv[i])
		}
		m[k] = kv[i+1]
	}
	return m, nil
}

// Num shows a property's value. A number is written in full, never with an exponent: the JSON client reads every
// number as a float64, and an id of a million must still make the link /pages/1000000, not /pages/1e+06.
func Num(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return siren.Num(x)
	}
	return fmt.Sprint(v)
}

// placing says how a class of entity is drawn: the template, and the names of the actions that this template draws
// itself, each in its place in the domain (the editor of a body, the capture box, a tile, the pick bar). The ⋯ menu
// draws the other actions of the entity (docs/plans/159-the-class-of-an-action.md). The test
// TestEveryActionOfAnEntityIsDrawnOnce holds each list equal to what its template draws.
type placing struct {
	template string
	places   []string
}

// pagePlaces are the actions that the template page (and the sheet and margin of a series) draws in their own places:
// the editor of the body, Restore in the alert of a deleted page, the category badges, New page here in Filed here,
// and the link to the picker of the cover.
var pagePlaces = []string{"save-body", "revive", "file-pages", "create-page", "set-cover"}

// viewOf names, for each class of entity, the template that shows it and the actions that template places; a class
// without one is shown by "generic", which places none.
var viewOf = map[string]placing{
	"day":    {"day", []string{"capture", "save-body", "record", "set-cover"}},
	"page":   {"page", pagePlaces},
	"person": {"page", pagePlaces},
	"file":   {"page", pagePlaces},
	"metric": {"page", pagePlaces},
	"series": {"metric", append(slices.Clone(pagePlaces), "record", "edit-metric")},

	"days":         {"stream", []string{"open-day", "random-page"}},
	"search":       {"search", []string{"search"}},
	"browse":       {"browse", []string{"file-pages", "tombstone-pages", "random-page"}},
	"metrics":      {"metrics", []string{"file-pages"}},
	"conversation": {"conversation", []string{"save-body", "say", "file-pages"}},
	"settings":     {"settings", []string{"settings"}},
	"measurement":  {"measurement", []string{"correct"}},
	"month":        {"month", []string{"create-page", "save-body"}},
	"year":         {"year", []string{"create-page", "save-body"}},
	"week":         {"week", []string{"create-page", "save-body"}},
	"cover-choice": {"cover-choice", []string{"set-cover"}},
	"actions":      {"catalog", nil}, // the catalog page draws every action boxed: nothing is placed, and it has no menu
	"error":        {"error", nil}, "login": {"login", nil}, "locked": {"locked", nil}, "preview": {"preview", nil},
}

// placingOf is the way an entity is drawn: that of the first of its classes that has one, else the generic view.
func placingOf(e *siren.Entity) placing {
	for _, c := range e.Class {
		if p, ok := viewOf[c]; ok {
			return p
		}
	}
	return placing{template: "generic"}
}

// view is what a template gets: the entity, its properties as the JSON client reads them, the path the browser
// asked for (which navigation link is current), the address of this page (where a form on it asks to return,
// docs/plans/091-the-browser-for-daily-use.md) and, for an error answering a form, the text that form sent (so a
// refused save loses nothing) and the page it came from.
type view struct {
	*siren.Entity
	Props     any
	Path      string
	Here      string
	Submitted string
	Back      string
	Shell     *Shell     // the sidebar's read; nil when the page is drawn without it
	query     url.Values // the page's own query: ?new=<action> and the fields it fills
	cards     map[string]*siren.Entity
	chatDays  []string
}

// IsSeries reports the view of a metric (class series): it carries the metric's page (docs/plans/144-one-view-for-a-metric.md),
// draws the mark as its own form, and its Delete returns to the page, which a deleted metric keeps.
func (v *view) IsSeries() bool { return slices.Contains(v.Class, "series") }

// Asked is a parameter of the page's own query (docs/plans/114-one-way-to-do-a-thing.md): ?new=<action> opens that
// action's form, and ?category= fills a field.
func (v *view) Asked(name string) string { return v.query.Get(name) }

// ReturnHere is this page as a write returns to it: without the form that a ?new= link opened, the values that
// filled it and its back. The answer then does not open the form again, and a back never holds another back.
func (v *view) ReturnHere() string {
	drop := []string{"new", "back"}
	if a := v.Action(v.Asked("new")); a != nil {
		for _, f := range a.Fields {
			drop = append(drop, f.Name)
		}
	}
	return v.hereWith(nil, drop...)
}

// hereOf is the address of the page the browser is on, without the one-use feedback receipt: a form that returns
// here must not carry a receipt that was already taken.
func hereOf(u *url.URL) string {
	q := u.Query()
	q.Del("feedback")
	if len(q) == 0 {
		return u.EscapedPath()
	}
	return u.EscapedPath() + "?" + q.Encode()
}

// DrawFailed is the page a browser gets when its view cannot be drawn: a 500 that names no internal cause. The
// server's log has the cause.
const DrawFailed = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Error · Lifelog</title></head>
<body><p role="alert">This page cannot be drawn. The server's log has the cause.</p><p><a href="/">Home</a></p></body></html>
`

// Facts are what a page shows beyond its entity. The one who draws the page reads them from the request and the
// API: the address the browser asked for, the shell's read (ShellOf) and, for an error that answers a form, the text
// that form sent and the page it came from.
type Facts struct {
	URL       *url.URL // the address the browser asked for
	Shell     *Shell   // the sidebar's read; nil when the page is drawn without it
	Submitted string   // the body (or the text) that a refused form sent
	Back      string   // the page that a refused form came from, when it is a page of this server
	// Cards are the entities that the addresses of a conversation's tool lines answer, by address (CardsOf,
	// docs/plans/148-the-chat-screen.md). An address with no entity is drawn as a plain link.
	Cards map[string]*siren.Entity
	// ChatDays are the days before today that have a conversation page, oldest first: the chips of the chat screen.
	ChatDays []string
}

// pageView is the view of a page: the entity and the facts of the page. It reads nothing, so the same arguments give
// the same view. Properties that JSON cannot carry (a
// NaN, an infinity) are an error, so a page is never drawn with its properties left out.
func pageView(e *siren.Entity, f Facts) (*view, error) {
	shape, err := jsonShape(e.Properties)
	if err != nil {
		return nil, err
	}
	v := &view{Entity: e, Props: shape, Path: f.URL.Path, Here: hereOf(f.URL), Submitted: f.Submitted, Back: f.Back,
		Shell: f.Shell, query: f.URL.Query(), cards: f.Cards, chatDays: f.ChatDays}
	return v, nil
}

// Draw is the page of e as HTML: the view of e (pageView), drawn by the template of its class (htmlOf). It is the one
// call that draws a page: f are the facts of the page.
func Draw(e *siren.Entity, f Facts) ([]byte, error) {
	v, err := pageView(e, f)
	if err != nil {
		return nil, err
	}
	return htmlOf(v)
}

// TemplateOf names the template that draws an entity: the first of its classes that has one, else "generic".
func TemplateOf(e *siren.Entity) string { return placingOf(e).template }

// htmlOf is the page of a view, drawn by the template of its entity's class. A page of a connected app or of the task
// service (drawnByShape) is always drawn by generic: the class is the app's word, and it may be the name of a class of this
// server.
func htmlOf(v *view) ([]byte, error) {
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, v.placing().template, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// LinkTitle is the title of the entity's link with that rel, "" when it has none.
func (v *view) LinkTitle(rel string) string {
	if l := v.LinkByRel(rel); l != nil {
		return l.Title
	}
	return ""
}

// Link is the href of the entity's link with that rel, "" when it has none.
func (v *view) Link(rel string) string {
	if l := v.LinkByRel(rel); l != nil {
		return l.Href
	}
	return ""
}

// LinksOf are the entity's links with that rel, in order.
func (v *view) LinksOf(rel string) []siren.Link { return withRel(v.Links, rel) }

// Embedded are the entity's embedded links with that rel.
func (v *view) Embedded(rel string) []siren.Link { return withRel(v.Entities, rel) }

func withRel(links []siren.Link, rel string) []siren.Link {
	var out []siren.Link
	for _, l := range links {
		if slices.Contains(l.Rel, rel) {
			out = append(out, l)
		}
	}
	return out
}

// filedHere is what a category page lists under Filed here: the sub-categories first, then the rest, each in the
// order of the backlinks (by title).
type filedHere struct {
	Subs, Items []siren.Link
}

// Filed is the Filed here of a page: its part-of backlinks, the categories first; nil when nothing is filed in it.
func (v *view) Filed() *filedHere {
	var f filedHere
	for _, l := range v.Embedded("backlink") {
		if !slices.Contains(l.Rel, "part-of") {
			continue
		}
		if slices.Contains(l.Class, "category") {
			f.Subs = append(f.Subs, l)
		} else {
			f.Items = append(f.Items, l)
		}
	}
	if len(f.Subs)+len(f.Items) == 0 {
		return nil
	}
	return &f
}

// Categories are the categories of a page or a metric: the part-of links of its links, in their order.
func (v *view) Categories() []any {
	props, _ := v.Props.(map[string]any)
	var out []any
	for _, l := range anyList(props["links"]) {
		if m, _ := l.(map[string]any); m["kind"] == "part-of" {
			out = append(out, l)
		}
	}
	return out
}

// Backlinks are the backlinks of a page by kind, in the order of their first appearance, without part-of: Filed
// here lists those.
func (v *view) Backlinks() []bucket {
	props, _ := v.Props.(map[string]any)
	var out []bucket
	for _, b := range group(props["backlinks"], "kind") {
		if b.Key != "part-of" {
			out = append(out, b)
		}
	}
	return out
}

// dayParts are the rows of a day's view (cookbook/day-view.md) by the part of the day's page that draws them, each
// in the order of the view. Journal is the body of the day page: one body when the day has a live day page (also an
// empty body), none when it has no day page. A row of a reading is in no part: the margin draws the readings from
// the day's readings.
type dayParts struct {
	Journal []string
	Pages   []writtenPage // the other pages written on the day
}

// writtenPage is a page written on a day, as the day lists it: its title, and whether it changed after it was made.
type writtenPage struct {
	Title  string
	Edited bool
}

// DayParts are the rows of the day's view by part, from one pass over the view.
func (v *view) DayParts() dayParts {
	props, _ := v.Props.(map[string]any)
	rows, _ := props["view"].([]any)
	var p dayParts
	for _, r := range rows {
		m, _ := r.(map[string]any)
		detail, _ := m["detail"].(string)
		switch what, _ := m["what"].(string); what {
		case "day page":
			p.Journal = append(p.Journal, detail)
		case "page", "page (edited)":
			p.Pages = append(p.Pages, writtenPage{Title: detail, Edited: what != "page"})
		}
	}
	return p
}

// jsonShape is v as a JSON client reads it: maps by JSON name, lists, strings, float64s. A value that JSON cannot
// carry (a NaN, an infinity) is an error, so a page is never drawn with its properties left out.
func jsonShape(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var x any
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	return x, nil
}

// rejected are the [[…]] names in a body that make no link, because they are not valid titles.
func rejected(body string) []string {
	_, _, out := text.Targets(body, "")
	return out
}

// alertView is how an error entity is drawn (docs/plans/092-the-app-shell.md): the first words, picked by the
// error's code, and the tone: danger when the request was refused or failed, warn when the owner can fix it here.
type alertView struct {
	Words  string
	Danger bool
}

// messageIsTheAlert lists the codes whose message is the whole alert: the message already says what to do, so these
// codes add no first words.
var messageIsTheAlert = []string{siren.CodeBadRequest, siren.CodeNotFound, siren.CodeConflict, siren.CodeInvalid}

// alertOf picks an error's first words and tone by its code (API.md, Errors); the message follows them.
func alertOf(code any) alertView {
	if c, ok := code.(string); ok && slices.Contains(messageIsTheAlert, c) {
		return alertView{}
	}
	switch code {
	case siren.CodeForbidden, siren.CodeCrossOrigin, siren.CodeTokenRequired, siren.CodeUnauthorized:
		return alertView{"Refused.", true}
	case siren.CodeTooLarge, siren.CodeUnsupportedMedia, siren.CodeTimeout, siren.CodeInternal:
		return alertView{"Not done.", true}
	case siren.CodeStaleVersion:
		return alertView{"Not saved. The page changed since you opened it.", false}
	case siren.CodeExists:
		return alertView{"Not created.", false}
	case siren.CodeSnapshotRefused:
		return alertView{"Not taken.", false}
	case siren.CodeRestoreCheckFailed:
		return alertView{"Taken, but the restore check failed.", true}
	case siren.CodeBusy:
		return alertView{"Not done. The file is busy. Nothing was written, and a retry is safe.", false}
	case siren.CodeUnavailable:
		return alertView{"Not done. The app is not running.", false}
	case siren.CodeUpstream:
		return alertView{"Not done.", true}
	case siren.CodeLocked:
		return alertView{"Locked. Unlock this browser with the PIN, then try again.", false}
	}
	return alertView{}
}

// group splits a list of objects by the value of key, keeping the order of first appearance. The objects without
// the key are in the bucket whose key is "".
type bucket struct {
	Key   string
	Items []any
}

func group(list any, key string) []bucket {
	var out []bucket
	at := map[string]int{}
	items, _ := list.([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		k := Num(m[key])
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, bucket{Key: k})
		}
		out[i].Items = append(out[i].Items, it)
	}
	return out
}

// monthName is a month in words, "2026-10" to "October".
func monthName(month string) string {
	if t, ok := siren.MonthOf(month); ok {
		return t.Format("January")
	}
	return month
}
