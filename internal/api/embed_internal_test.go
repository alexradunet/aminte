package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/siren"
)

// embedServer is the server of a synthetic file with the routes of a probe added, for the tests of embed. It is the
// server and not the handler of New, so the test reaches the mux, which has no guard: the probe is not a catalog
// action. The routes:
//
//	/t/{n}     an entity with content (the property n), and two references of its own with the rel sub
//	/empty     an entity with no class and no title
//	/bare      an entity with a class and a title and no content
//	/who       an entity that names the writer, the host and the context value of the request that it answers
//	/hop/{n}   a redirect to /hop/{n-1}; /hop/0 is /t/0
//	/out       a redirect to another host
//	/probe     the entity that the tests embed into: its sub-entities are the ones that a test names
func embedServer(t *testing.T, probe func() []siren.Link) *server {
	t.Helper()
	at := time.Date(2031, 5, 14, 9, 0, 0, 0, time.UTC)
	h := newServer(&core.Store{DB: dbtest.New(t), Clock: func() time.Time { return at }, Zone: time.UTC}, NewApps())
	h.mux.HandleFunc("GET /t/{n}", h.serve(func(r *http.Request) (*siren.Entity, error) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		return &siren.Entity{Class: []string{"target"}, Title: "Target " + r.PathValue("n"), Properties: map[string]any{"n": n},
			Entities: []siren.Link{link("sub", "/t/0", "inner"), link("item", "/t/0", "inner")}}, nil
	}))
	h.mux.HandleFunc("GET /empty", h.serve(func(*http.Request) (*siren.Entity, error) { return &siren.Entity{}, nil }))
	h.mux.HandleFunc("GET /bare", h.serve(func(*http.Request) (*siren.Entity, error) {
		return &siren.Entity{Class: []string{"bare"}, Title: "Bare"}, nil
	}))
	h.mux.HandleFunc("GET /who", h.serve(func(r *http.Request) (*siren.Entity, error) {
		return &siren.Entity{Class: []string{"who"}, Title: "Who", Properties: map[string]any{
			"source": source(r), "host": r.Host, "ctx": fmt.Sprint(r.Context().Value(probeKey{}))}}, nil
	}))
	h.mux.HandleFunc("GET /hop/{n}", h.serve(func(r *http.Request) (*siren.Entity, error) {
		if n, _ := strconv.Atoi(r.PathValue("n")); n > 0 {
			return nil, redirect{"/hop/" + strconv.Itoa(n-1)}
		}
		return nil, redirect{"/t/0"}
	}))
	h.mux.HandleFunc("GET /out", h.serve(func(*http.Request) (*siren.Entity, error) { return nil, redirect{"https://example.org/x"} }))
	h.mux.HandleFunc("GET /probe", h.serve(func(*http.Request) (*siren.Entity, error) {
		return &siren.Entity{Class: []string{"probe"}, Title: "Probe", Properties: map[string]any{"ok": true}, Entities: probe()}, nil
	}))
	return h
}

type probeKey struct{}

// readProbe is the entity that GET /probe?embed=... answers, read as JSON.
func readProbe(t *testing.T, h *server, query string) *siren.Entity {
	t.Helper()
	req := httptest.NewRequest("GET", "/probe"+query, nil)
	req.Host = "aminte.test"
	req.Header.Set("Accept", sirenType)
	req = req.WithContext(context.WithValue(req.Context(), probeKey{}, "from the request"))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	e := &siren.Entity{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), e) != nil {
		t.Fatalf("GET /probe%s: %d %.300s", query, rec.Code, rec.Body.String())
	}
	return e
}

func ref(rel, href string) siren.Link { return link(rel, href, "") }

// TestEmbedReplacesTheReferencesOfTheAskedRels: a sub-entity that is a reference and has an asked rel takes the
// entity that its href answers, and keeps its rel and its href. A sub-entity of another rel, a request with no embed
// and a rel that no sub-entity has leave the answer as it is.
func TestEmbedReplacesTheReferencesOfTheAskedRels(t *testing.T) {
	h := embedServer(t, func() []siren.Link {
		return []siren.Link{ref("item", "/t/1"), ref("other", "/t/2"), ref("item", "/t/3#row-3"), ref("item", "/t/4")}
	})
	for _, query := range []string{"", "?embed=nothing", "?embed=", "?embed=,,"} {
		for i, l := range readProbe(t, h, query).Entities {
			if l.Embedded() {
				t.Errorf("%q: sub-entity %d is embedded; nothing was asked for", query, i)
			}
		}
	}
	got := readProbe(t, h, "?embed=item").Entities
	for i, want := range []struct {
		href     string
		embedded bool
		n        float64
	}{{"/t/1", true, 1}, {"/t/2", false, 0}, {"/t/3#row-3", true, 3}, {"/t/4", true, 4}} {
		l := got[i]
		if l.Href != want.href || l.Embedded() != want.embedded {
			t.Errorf("sub-entity %d: href %q, embedded %v; want %q, %v", i, l.Href, l.Embedded(), want.href, want.embedded)
			continue
		}
		if props, _ := l.Properties.(map[string]any); want.embedded && (props["n"] != want.n || l.Class[0] != "target" || l.Title != "Target "+strconv.Itoa(int(want.n))) {
			t.Errorf("sub-entity %d: %v %v %q, want the target %v", i, props, l.Class, l.Title, want.n)
		}
	}
	if !got[0].HasRel("item") || !got[2].HasRel("item") {
		t.Errorf("the rel is lost: %v %v", got[0].Rel, got[2].Rel)
	}
	both := readProbe(t, h, "?embed=other&embed=item") // two parameters, and the rels of a list with blanks
	if !both.Entities[0].Embedded() || !both.Entities[1].Embedded() {
		t.Error("embed with two parameters did not embed both rels")
	}
	if spaced := readProbe(t, h, "?embed=%20other%20,%20item"); !spaced.Entities[0].Embedded() || !spaced.Entities[1].Embedded() {
		t.Error("embed with blanks around the rels did not embed both rels")
	}
}

// TestEmbedReadsAtMostThirtySixAndGoesOneLevelDeep: one answer makes 36 reads, in the order of the sub-entities, and
// the others stay references. The embedded entities are not embedded into, though they ask for the same rel.
func TestEmbedReadsAtMostThirtySixAndGoesOneLevelDeep(t *testing.T) {
	h := embedServer(t, func() []siren.Link {
		var links []siren.Link
		for i := range 40 {
			links = append(links, ref("item", "/t/"+strconv.Itoa(i)))
		}
		return links
	})
	got := readProbe(t, h, "?embed=item,sub").Entities
	for i, l := range got {
		if want := i < maxEmbedded; l.Embedded() != want {
			t.Errorf("sub-entity %d: embedded %v, want %v", i, l.Embedded(), want)
		}
	}
	if maxEmbedded != 36 {
		t.Errorf("the bound is %d, the API says 36", maxEmbedded)
	}
	for _, inner := range got[0].Entities {
		if inner.Embedded() {
			t.Errorf("the embedded entity has an embedded sub-entity %s: depth is 1", inner.Href)
		}
	}
}

// TestEmbedKeepsTheReferenceThatCannotBeRead: an address that is not a path of this server, an answer that is not
// 2xx, an answer that is not a Siren entity, an entity with no content, an address that embeds on its own and a
// redirect that leaves the server leave the sub-entity as it is. A read that fails still counts for the bound.
func TestEmbedKeepsTheReferenceThatCannotBeRead(t *testing.T) {
	failing := []string{"https://example.org/x", "//example.org/x", "t/1", "", "/pages/999999", "/nothing/here", "/snapshots/life.db",
		"/empty", "/bare", "/t/1?embed=sub", "/out", "/hop/2", "/%zz"}
	h := embedServer(t, func() []siren.Link {
		links := []siren.Link{ref("item", "/t/1")}
		for _, href := range failing {
			links = append(links, ref("item", href))
		}
		return links
	})
	got := readProbe(t, h, "?embed=item").Entities
	if !got[0].Embedded() {
		t.Error("the good address is not embedded")
	}
	for i, href := range failing {
		if l := got[i+1]; l.Embedded() || l.Href != href || !l.HasRel("item") || l.Title != "" || len(l.Class) != 0 {
			t.Errorf("%q: embedded %v, href %q, rel %v, title %q, class %v; want the reference as it was", href, l.Embedded(), l.Href, l.Rel, l.Title, l.Class)
		}
	}
	// The failures count as reads: the 36 reads go on the 36 first references, good or not.
	many := embedServer(t, func() []siren.Link {
		var links []siren.Link
		for range 36 {
			links = append(links, ref("item", "/nothing/here"))
		}
		return append(links, ref("item", "/t/1"))
	})
	if readProbe(t, many, "?embed=item").Entities[36].Embedded() {
		t.Error("the 37th read was made after 36 that failed")
	}
}

// TestEmbedFollowsOneRedirect: /hop/1 is a redirect to /hop/0, which is a redirect again; /hop/0 is one redirect from
// an entity. The sub-entity that is one redirect away is embedded, with its own href; the one that is two away is not.
func TestEmbedFollowsOneRedirect(t *testing.T) {
	h := embedServer(t, func() []siren.Link { return []siren.Link{ref("item", "/hop/0"), ref("item", "/hop/1")} })
	got := readProbe(t, h, "?embed=item").Entities
	if !got[0].Embedded() || got[0].Href != "/hop/0" {
		t.Errorf("one redirect: embedded %v, href %q", got[0].Embedded(), got[0].Href)
	}
	if got[1].Embedded() {
		t.Error("two redirects: the second was followed")
	}
}

// TestEmbedReadsAsAnAgentInTheContextOfTheRequest: an embedded read carries the writer agent:embed, the host and the
// context of the request. An owner-only read refuses that writer, so its reference stays and its answer is not in the
// result: the settings never reach a client through an address that an agent wrote.
func TestEmbedReadsAsAnAgentInTheContextOfTheRequest(t *testing.T) {
	h := embedServer(t, func() []siren.Link {
		return []siren.Link{ref("item", "/who"), ref("item", "/settings"), ref("item", "/integrity"), ref("item", "/settings?x=1")}
	})
	got := readProbe(t, h, "?embed=item").Entities
	props, _ := got[0].Properties.(map[string]any)
	if props["source"] != "agent:embed" || props["host"] != "aminte.test" || props["ctx"] != "from the request" {
		t.Errorf("the embedded read: %v", props)
	}
	for _, l := range got[1:] {
		if l.Embedded() {
			t.Errorf("the owner-only address %s is embedded", l.Href)
		}
	}
}

// TestAReadTakesEmbedButAWriteDoesNot: embed is a name that every GET takes, an action's read included, whatever the
// client accepts. A POST that names it is refused: only a read embeds.
func TestAReadTakesEmbedButAWriteDoesNot(t *testing.T) {
	h := embedServer(t, func() []siren.Link { return nil })
	for _, target := range []string{"/search?q=a&embed=item", "/browse?embed=item", "/days?embed=item", "/?embed=today"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", target, nil)
		req.Header.Set("Accept", sirenType)
		h.mux.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("GET %s: %d %.200s, want 200", target, rec.Code, rec.Body.String())
		}
	}
	page := httptest.NewRecorder()
	asked := httptest.NewRequest("GET", "/probe?embed=item", nil)
	asked.Header.Set("Accept", "text/html")
	h.mux.ServeHTTP(page, asked)
	if page.Code != 200 || page.Header().Get("Content-Type") != sirenType {
		t.Errorf("a client that accepts HTML and asks for embed: %d %q, want 200 and Siren JSON", page.Code, page.Header().Get("Content-Type"))
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/days/2031-05-14/capture", strings.NewReader(url.Values{"text": {"x"}, "embed": {"item"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", sirenType)
	h.mux.ServeHTTP(rec, req)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), "embed") {
		t.Errorf("POST capture with embed: %d %.200s, want 422 naming embed", rec.Code, rec.Body.String())
	}
}

// TestAConnectedAppDoesNotSeeEmbed: the server embeds in the answer of an app; the app is asked without the parameter.
// The reference in its answer is in the namespace of the app, so the embedded read goes to the app too.
func TestAConnectedAppDoesNotSeeEmbed(t *testing.T) {
	var asked []string
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		w.Header().Set("Content-Type", sirenType)
		fmt.Fprint(w, `{"class":["items"],"title":"Items","properties":{"n":1},"entities":[{"rel":["item"],"href":"/things/1"}]}`)
	}))
	t.Cleanup(app.Close)
	h := embedServer(t, func() []siren.Link { return nil })
	if err := h.with.set("probe", app.URL, "Probe", nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/with/probe/items?q=1&embed=item", nil)
	req.Header.Set("Accept", sirenType)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	e := &siren.Entity{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), e) != nil || len(e.Entities) != 1 {
		t.Fatalf("GET /with/probe/items: %d %.300s", rec.Code, rec.Body.String())
	}
	if want := []string{"/items?q=1", "/things/1"}; strings.Join(asked, " ") != strings.Join(want, " ") {
		t.Errorf("the app was asked %q, want %q: the request of the client, with no embed, then the embedded read", asked, want)
	}
	if got := e.Entities[0]; got.Href != "/with/probe/things/1" || !got.Embedded() {
		t.Errorf("the sub-entity of the app: %q, embedded %v; want the address in the namespace of the app, embedded", got.Href, got.Embedded())
	}
}

// TestAnEmbeddedLinkOfAnAppKeepsNoSubEntities: the answer of an app is moved into the namespace of the server link by
// link. A sub-entity of an app that carries entities or links of its own would carry addresses that this move does not
// reach, so it keeps none of them.
func TestAnEmbeddedLinkOfAnAppKeepsNoSubEntities(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sirenType)
		fmt.Fprint(w, `{"class":["items"],"title":"Items","entities":[{"rel":["item"],"href":"/things/1","properties":{"n":1},
			"entities":[{"rel":["x"],"href":"https://evil.example/x"}],"links":[{"rel":["y"],"href":"https://evil.example/y"}]}]}`)
	}))
	t.Cleanup(app.Close)
	h := embedServer(t, func() []siren.Link { return nil })
	if err := h.with.set("probe", app.URL, "Probe", nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/with/probe/items", nil)
	req.Header.Set("Accept", sirenType)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	e := &siren.Entity{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), e) != nil || len(e.Entities) != 1 {
		t.Fatalf("GET /with/probe/items: %d %.300s", rec.Code, rec.Body.String())
	}
	if got := e.Entities[0]; got.Href != "/with/probe/things/1" || len(got.Entities) != 0 || len(got.Links) != 0 || strings.Contains(rec.Body.String(), "evil.example") {
		t.Errorf("the sub-entity of the app: %+v", got)
	}
}

// TestEmbedOfAnAddressWithASpaceAnswers: a tool line is text that an agent wrote, and its address may hold a space or
// a tab ("/search?q=grey heron"). The read of such an address must not end the answer: it is embedded when it answers,
// else it stays a reference, and every other sub-entity is embedded as before.
func TestEmbedOfAnAddressWithASpaceAnswers(t *testing.T) {
	odd := []string{"/t/1?q=grey heron", "/t/2 x", "/t/3\tz", "/t/4\r\nHost: evil"}
	h := embedServer(t, func() []siren.Link {
		links := []siren.Link{}
		for _, href := range odd {
			links = append(links, ref("item", href))
		}
		return append(links, ref("item", "/t/5"))
	})
	got := readProbe(t, h, "?embed=item").Entities // a panic of the read ends the answer, and readProbe fails
	if len(got) != len(odd)+1 || !got[len(odd)].Embedded() {
		t.Fatalf("the sub-entities after the odd addresses: %+v", got)
	}
	if !got[0].Embedded() || got[0].Href != odd[0] {
		t.Errorf("%q is an address with words in its query: embedded %v, href %q", odd[0], got[0].Embedded(), got[0].Href)
	}
	for i, href := range odd {
		if got[i].Href != href || !got[i].HasRel("item") {
			t.Errorf("%q: href %q, rel %v; the sub-entity keeps its href and its rel", href, got[i].Href, got[i].Rel)
		}
	}
}
