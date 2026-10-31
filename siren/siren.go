// Package siren is the hypermedia of the API: the Siren types that every answer is made of (an entity, its links, its
// actions and their fields), the header that names the writer of a request, the JSON of an answer, and the addresses
// of the resources that the API serves and that its view links. The API (internal/api), its view (internal/api/view),
// the clients (internal/client, cmd/lifelog) and core (for the codes of an error) import it. It imports only the
// standard library, so a client of the types gets none of the server with them.
package siren

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"time"
)

// SourceHeader names the writer of a request's rows (lifelog_meta.source). Without it a browser form writes as
// "ui" and any other client as "api".
const SourceHeader = "Lifelog-Source"

// The prefixes of the writers that are not the owner: an agent (the MCP server) writes as AgentPrefix+name, an import
// as ImportPrefix+name. The owner-only actions are refused to both (api.requireOwner).
const (
	AgentPrefix  = "agent:"
	ImportPrefix = "import:"
)

// The codes of the error entity (property code; API.md, Errors; core.Error.Code). A client branches on a code, so a
// string never changes. They are constants so that the code that makes one (core, the API) and the code that picks
// words for it (the view) cannot drift apart: a renamed code fails to compile instead of drawing an empty alert.
// The API gives the general code of a status when a failure names none (codeOf).
const (
	CodeBadRequest         = "bad_request"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeTooLarge           = "too_large"
	CodeUnsupportedMedia   = "unsupported_media" // a multipart body sent to an action that takes no file
	CodeInvalid            = "invalid"
	CodeTimeout            = "timeout"
	CodeInternal           = "internal"
	CodeExists             = "exists"               // a title in use (core.ExistsError)
	CodeCrossOrigin        = "cross_origin"         // a browser write from an origin that the server does not trust
	CodeTokenRequired      = "token_required"       // a request with no token, where the server needs one
	CodeStaleVersion       = "stale_version"        // a page saved from a version that is not the current one
	CodeSnapshotRefused    = "snapshot_refused"     // the snapshot action refused its target
	CodeRestoreCheckFailed = "restore_check_failed" // a snapshot that did not restore
	// CodeBusy is the code of a write that met a locked file (SQLITE_BUSY): the CLI and the service write one file,
	// and a write can wait longer than busy_timeout for the other.
	CodeBusy = "busy"
	// CodeUnavailable is the code of a request for an app, connected by serve --with, that is not running (503).
	CodeUnavailable = "unavailable"
	// CodeUpstream is the code of an app that answered something that is not an entity (502).
	CodeUpstream = "upstream"
	// CodeLocked is the code of a request from a browser session that the lock holds shut (401); the PIN opens it.
	CodeLocked = "locked"
)

// Codes is the one list of the codes. The tests hold it equal to the constants above, to API.md (Errors) and to
// the tables of the API and of the view.
func Codes() []string {
	return []string{
		CodeBadRequest, CodeUnauthorized, CodeForbidden, CodeNotFound, CodeConflict, CodeTooLarge,
		CodeUnsupportedMedia, CodeInvalid, CodeTimeout, CodeInternal, CodeExists, CodeCrossOrigin,
		CodeTokenRequired, CodeStaleVersion, CodeSnapshotRefused, CodeRestoreCheckFailed, CodeBusy,
		CodeUnavailable, CodeUpstream, CodeLocked,
	}
}

// ActionClasses is the one list of the values that an action's class may have: what the action does to the resource
// it is on (API.md, Action classes), never how a client draws it. The tests hold it equal to API.md. A server
// drops a class of a connected app that is not in the list.
func ActionClasses() []string {
	return []string{"create", "edit", "toggle"}
}

// Entity is a Siren entity. Result is an extension: what a POST did (e.g. the wikilinks a save skipped).
type Entity struct {
	Class      []string `json:"class,omitempty"`
	Title      string   `json:"title,omitempty"`
	Properties any      `json:"properties,omitempty"`
	Entities   []Link   `json:"entities,omitempty"`
	Actions    []Action `json:"actions,omitempty"`
	Links      []Link   `json:"links,omitempty"`
	Result     any      `json:"result,omitempty"`
}

// Link is a Siren link, also used as an embedded link (a sub-entity that is only a reference).
// Properties and Actions are an extension of Siren, for an embedded link only: an embedded link that carries them
// is a short form of an embedded representation that keeps its href. The browser draws it as a row
// (extensions/README.md, "How the browser draws a page of an app").
// Entities and Links complete the representation (API.md, "Embedding"): a sub-entity that carries properties,
// sub-entities, actions or links is an embedded representation of the thing at its href, as the Siren specification
// has it, and Embedded says so.
type Link struct {
	Rel        []string `json:"rel"`
	Href       string   `json:"href"`
	Title      string   `json:"title,omitempty"`
	Class      []string `json:"class,omitempty"`
	Properties any      `json:"properties,omitempty"`
	Entities   []Link   `json:"entities,omitempty"`
	Actions    []Action `json:"actions,omitempty"`
	Links      []Link   `json:"links,omitempty"`
}

// HasRel reports whether the link has that rel.
func (l Link) HasRel(rel string) bool { return slices.Contains(l.Rel, rel) }

// Embedded reports a sub-entity that carries the representation of the thing at its href: properties, sub-entities,
// actions or links. A sub-entity that carries none is a reference, and a reference is what embed replaces.
func (l Link) Embedded() bool {
	return l.Properties != nil || len(l.Entities) > 0 || len(l.Actions) > 0 || len(l.Links) > 0
}

// Embedding is the link l with the representation of e in place of the reference: the class, the title (when e has
// one), the properties, the sub-entities, the actions and the links of e. The rel and the href stay, so a reader finds
// the sub-entity by the address that named it. e.Result is not part of a representation.
func (l Link) Embedding(e *Entity) Link {
	l.Class, l.Properties, l.Entities, l.Actions, l.Links = e.Class, e.Properties, e.Entities, e.Actions, e.Links
	if e.Title != "" {
		l.Title = e.Title
	}
	return l
}

// Entity is the representation that the link carries, as an entity of its own.
func (l Link) Entity() *Entity {
	return &Entity{Class: l.Class, Title: l.Title, Properties: l.Properties, Entities: l.Entities, Actions: l.Actions, Links: l.Links}
}

// Action is a Siren action. Class (standard Siren) is at most one of ActionClasses: what the action does to the
// resource. Description (what the action does, for an agent), Owner (the owner's alone) and Danger (it hides or
// retracts what is there) are extensions. Hint is the browser's short text for the form: it
// is not on the wire, so a client, the CLI and the MCP tools keep reading Description.
type Action struct {
	Name        string   `json:"name"`
	Class       []string `json:"class,omitempty"`
	Owner       bool     `json:"owner,omitempty"`
	Danger      bool     `json:"danger,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Hint        string   `json:"-"`
	Method      string   `json:"method"`
	Href        string   `json:"href"`
	Type        string   `json:"type,omitempty"`
	Fields      []Field  `json:"fields,omitempty"`
}

// Field is a Siren field. In "path" (an extension) marks a field of the catalog's templated href; Rows (an
// extension) is how many lines a browser draws of a textarea; Pages (an extension) says the value is a page's
// title, so a browser can offer the names of the pages as it does after [[; Accept (an extension) is what a file
// field takes, as the accept attribute of a browser's file input ("audio/*");
// Placeholder (an extension, the name of HAL-FORMS and Ion) is the words that a client may show in an empty field:
// an example of a value or a short prompt. A client may ignore it.
type Field struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Title       string   `json:"title,omitempty"`
	Value       any      `json:"value,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Options     []string `json:"options,omitempty"`
	In          string   `json:"in,omitempty"`
	Rows        int      `json:"rows,omitempty"`
	Pages       bool     `json:"pages,omitempty"`
	Accept      string   `json:"accept,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

// LinkByRel is the entity's link with that rel, nil when it has none.
func (e *Entity) LinkByRel(rel string) *Link {
	for i := range e.Links {
		for _, r := range e.Links[i].Rel {
			if r == rel {
				return &e.Links[i]
			}
		}
	}
	return nil
}

// Field is the action's field with that name, nil when it has none.
func (a *Action) Field(name string) *Field {
	for i := range a.Fields {
		if a.Fields[i].Name == name {
			return &a.Fields[i]
		}
	}
	return nil
}

// WriteJSON writes v to w as the API's JSON: with no HTML escapes, so a body reads as it was written (< > & stay as
// they are); indented by indent ("" for none); with a newline at the end. The API, the CLI and the MCP server write
// their JSON with it.
func WriteJSON(w io.Writer, v any, indent string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	return enc.Encode(v)
}

// Num is a number as a page shows it: written in full, never with an exponent. The JSON client reads every number as a
// float64, and an id of a million must still make the link /pages/1000000, not /pages/1e+06.
func Num(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }

// PageHref is the address of a page.
func PageHref(id int64) string { return "/pages/" + strconv.FormatInt(id, 10) }

// CoverHref is the address of the choice of a page's cover.
func CoverHref(id int64) string { return PageHref(id) + "/cover" }

// DayHref is the address of a day.
func DayHref(day string) string { return "/days/" + day }

// MeasurementHref is the address of a reading.
func MeasurementHref(id int64) string { return "/measurements/" + strconv.FormatInt(id, 10) }

// MetricHref is the address of a metric, by its name.
func MetricHref(name string) string { return "/metrics/" + url.PathEscape(name) }

// MonthHref is the address of the month of t.
func MonthHref(t time.Time) string { return "/months/" + t.Format("2006-01") }

// MonthOf parses a month, YYYY-MM; ok is false for anything else.
func MonthOf(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01", s)
	return t, err == nil && t.Format("2006-01") == s
}

// YearHref is the address of a year.
func YearHref(year int) string { return fmt.Sprintf("/years/%04d", year) }

// IsYear reports a year that has a page: 0001 to 9999. The year 0000 holds days, but it has no page (API.md).
func IsYear(year int) bool { return 1 <= year && year <= 9999 }

// BrowseHref is the address of Browse with a query.
func BrowseHref(v url.Values) string {
	if len(v) == 0 {
		return "/browse"
	}
	return "/browse?" + v.Encode()
}
