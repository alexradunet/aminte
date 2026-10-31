package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"

	"lifelog/siren"
)

// Embedding (API.md, "Embedding"): GET ?embed=rel1,rel2 replaces each sub-entity that is a reference with one of those
// rels by the entity that its href answers. The server asks its own handler, in the process, so a client gets in one
// answer what it would read in many. The page of a browser reads the same way (pageAnswer).

// embedField is the name of the query parameter that asks for the embedding.
const embedField = "embed"

// embedSource is the writer of an embedded read. It is an agent's, so requireOwner refuses it: an owner-only read
// (the settings, the integrity check) keeps its link. The reason is that an embedded href can come from text that an
// agent wrote (a tool line of a conversation), and such text must never make the server run an owner-only read.
const embedSource = siren.AgentPrefix + "embed"

// maxEmbedded is the most reads that one answer makes. The sub-entities after them stay references.
const maxEmbedded = 36

// sirenType is the media type of the JSON of an entity.
const sirenType = "application/vnd.siren+json"

// embedRels are the rels that the query asks to embed: the values of every embed parameter, cut at the commas.
func embedRels(q url.Values) []string {
	var rels []string
	for _, v := range q[embedField] {
		for rel := range strings.SplitSeq(v, ",") {
			if rel = strings.TrimSpace(rel); rel != "" {
				rels = append(rels, rel)
			}
		}
	}
	return rels
}

// embed embeds the sub-entities of e that the query of r asks for.
func (h *server) embed(r *http.Request, e *siren.Entity) {
	if rels := embedRels(r.URL.Query()); len(rels) > 0 {
		h.embedLinks(r, e.Entities, maxEmbedded, rels...)
	}
}

// embedLinks replaces, in place, each reference in links that has one of rels by the entity that its href answers
// (siren.Link.Embedding). The depth is 1: an embedded read asks for no embedding of its own. At most limit reads are
// made, in the order of the links; the API's limit is maxEmbedded, and a page that draws fewer asks for fewer. A
// reference whose read fails stays as it is.
func (h *server) embedLinks(r *http.Request, links []siren.Link, limit int, rels ...string) {
	reads := 0
	for i, l := range links {
		if reads == limit {
			return
		}
		if l.Href == "" || l.Embedded() || !slices.ContainsFunc(rels, l.HasRel) {
			continue
		}
		reads++
		if sub := h.entityAt(r, l.Href, 1); sub != nil {
			if embedded := l.Embedding(sub); embedded.Embedded() { // an entity with no content is no representation
				links[i] = embedded
			}
		}
	}
}

// entityAt is the entity that the server's own handler answers for ref, and nil when there is none: a ref that is not
// a path of this server, that asks for an embedding itself, that answers a status other than 2xx or that answers no
// Siren entity. The fragment of ref stays with the reader. A redirect to a path of this server (/days/today to the day,
// a title to its page) is followed hops times at most.
func (h *server) entityAt(r *http.Request, ref string, hops int) *siren.Entity {
	path, _, _ := strings.Cut(ref, "#")
	u, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || u.Query().Has(embedField) {
		return nil
	}
	rec := h.inside(r, u)
	if rec.Code/100 == 3 {
		if to := rec.Header().Get("Location"); hops > 0 && strings.HasPrefix(to, "/") {
			return h.entityAt(r, to, hops-1)
		}
		return nil
	}
	return answeredEntity(rec)
}

// inside asks the server's own handler for a GET of u (a path and a query), as the writer embedSource. The page's own
// guard has passed already, so the request goes straight to the routes. It carries the context and the host of r, so
// the deadline of r ends it, and the authority of r is the one it was let in with. The request is made from the URL and
// not from a request line: an address from a tool line can hold a space or a line break, which a request line cannot.
func (h *server) inside(r *http.Request, u *url.URL) *httptest.ResponseRecorder {
	req := (&http.Request{Method: http.MethodGet, URL: &url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery},
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{}, Body: http.NoBody,
		Host: r.Host, RemoteAddr: r.RemoteAddr, RequestURI: u.RequestURI()}).WithContext(r.Context())
	req.Header.Set("Accept", sirenType)
	req.Header.Set(siren.SourceHeader, embedSource)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

// answeredEntity is the entity that a 2xx Siren answer holds, nil for any other answer and for an entity with no
// class and no title.
func answeredEntity(rec *httptest.ResponseRecorder) *siren.Entity {
	if rec.Code/100 != 2 || !strings.HasPrefix(rec.Header().Get("Content-Type"), sirenType) {
		return nil
	}
	e := &siren.Entity{}
	if err := json.Unmarshal(rec.Body.Bytes(), e); err != nil || e.Title == "" && len(e.Class) == 0 {
		return nil
	}
	return e
}
