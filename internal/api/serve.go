package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"time"

	"aminte/internal/core"
	"aminte/siren"
)

// server is the API on one store. The store's clock is the server's one clock: today is core.Store.Today, and the words
// for a time and the name of a snapshot are in the zone of core.Store.Local.
type server struct {
	s    *core.Store
	mux  *http.ServeMux
	with *Apps // the connected apps (with.go); shared with the other handler of the same serve
}

// NewWith returns the whole API as one handler: aminte serve mounts it on a socket; the CLI and the temporary server
// of aminte NAME call it in-process. It trusts no browser origin other than its own. A test gives a store with a
// fixed clock, so today is a fixed day and a time is written in fixed words.
//
// apps is the registry of the connected apps: their actions are in the catalog, and /with/NAME leads to them. serve
// and the handler share one registry, so both see the same apps. A nil apps is a server with no connected app: the CLI
// in-process and the temporary server of `aminte NAME` have none.
//
// The guard refuses a write that a page of another origin sends through the browser of the owner. It trusts no
// origin: a server that has no token must not take a form post from a page in the owner's browser. A program sends no
// Origin and no Sec-Fetch-Site, so the guard lets it through.
func NewWith(s *core.Store, apps *Apps) http.Handler {
	if apps == nil {
		apps = NewApps()
	}
	h := newServer(s, apps)
	guard := http.NewCrossOriginProtection()
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, crossOriginRefusal())
	}))
	return guard.Handler(limitBody(withDeadlines(defaultLimits, h.mux)))
}

// The time limits of a request. A server-wide ReadTimeout is wrong here: it would cut an upload, which has no size
// limit as a whole (limitBody). So the limits are per request, and a multipart form (the upload of a file) has its own.
type limits struct {
	request time.Duration // the context of any other request: the whole of its work. Snapshot and integrity-check are the longest.
	upload  time.Duration // the context of an upload: long, because a large original on a slow link is slow, but not endless
	stall   time.Duration // the longest an upload may send no byte
}

var defaultLimits = limits{request: 2 * time.Minute, upload: time.Hour, stall: 2 * time.Minute}

// withDeadlines gives each request a context deadline: l.request, or l.upload for a multipart form. The deadline reaches
// a query of the store and ends it with context.DeadlineExceeded, which errorEntity answers as 504. A wait for the
// file's lock ends by busy_timeout (5 s) and not by this deadline: the answer is then busy (503).
// An upload also gets a read deadline that moves on with each read of its body (stallBody), so a sender that stops
// is cut after l.stall, and a sender that goes on is not.
func withDeadlines(l limits, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := l.request
		if isMultipart(r) { // as limitBody
			limit = l.upload
			if r.Body != nil {
				rc := http.NewResponseController(w)
				if err := rc.SetReadDeadline(time.Now().Add(l.stall)); err == nil {
					r.Body = stallBody{r.Body, rc, l.stall}
				} else if !errors.Is(err, http.ErrNotSupported) { // a recorder in a test is not a connection
					logFault(r, "the upload has no read deadline", err)
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), limit)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// stallBody is the body of an upload. Each read moves the read deadline of the connection on by stall. At the end of
// the body the deadline is cleared: after that the server watches the connection for a client that went away, and a
// deadline that ran out then would cancel the request while the answer is still being made.
type stallBody struct {
	io.ReadCloser
	rc    *http.ResponseController
	stall time.Duration
}

func (b stallBody) Read(p []byte) (int, error) {
	b.rc.SetReadDeadline(time.Now().Add(b.stall)) // supported: withDeadlines set it once already
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.rc.SetReadDeadline(time.Time{})
	}
	return n, err
}

// newServer is the server with its routes: mount gives each action of the catalog its route, then the routes that are
// not actions follow.
func newServer(s *core.Store, apps *Apps) *server {
	h := &server{s: s, with: apps}
	m := http.NewServeMux()
	h.mux = m
	h.mount(m, catalog, map[string]actionFunc{
		"capture": h.capture, "create-page": h.createPage, "save-body": h.saveBody, "create-person": h.createPerson,
		"add-file": h.addFile, "set-cover": h.setCover, "promote": h.promote, "edit-person": h.editPerson, "edit-metric": h.editMetric,
		"revive": h.revive, "tombstone-pages": h.tombstoneMany, "file-pages": h.filePages, "record": h.record, "correct": h.correct,
		"retract": h.retract, "rename": h.rename, "register-metric": h.registerMetric, "integrity-check": h.integrity,
		"search": h.search, "browse": h.browse, "find": h.find, "open-day": h.openDay,
		"random-page": h.randomPage, "snapshot": h.snapshot,
		"rebuild-search": h.rebuildSearch, "resync-links": h.resyncLinks,
	})
	// The routes that are not actions: the resources and the files (a picture, a snapshot).
	m.HandleFunc("GET /{$}", h.serve(h.root))
	m.HandleFunc("GET /actions", h.serve(h.actions))
	m.HandleFunc("GET /days", h.serve(h.days))
	m.HandleFunc("GET /days/today", h.serve(h.today))
	m.HandleFunc("GET /days/{day}", h.serve(h.day))
	m.HandleFunc("GET /snapshots/{name}", h.snapshotFile)
	m.HandleFunc("GET /months/{month}", h.serve(h.month))
	m.HandleFunc("GET /weeks/today", h.serve(h.currentWeek))
	m.HandleFunc("GET /weeks/{week}", h.serve(h.week))
	m.HandleFunc("GET /years/{year}", h.serve(h.year))
	m.HandleFunc("GET /pages/{id}", h.serve(h.page))
	m.HandleFunc("GET /pages/{id}/cover", h.serve(h.coverChoice))
	m.HandleFunc("GET /pages/{id}/preview", picture(func(r *http.Request) ([]byte, error) {
		id, err := idOf(r)
		if err != nil {
			return nil, err
		}
		return h.s.Preview(r.Context(), id)
	}, "private, max-age=31536000, immutable")) // a preview by id never changes
	m.HandleFunc("GET /previews", picture(func(r *http.Request) ([]byte, error) {
		return h.s.PreviewByTitle(r.Context(), r.URL.Query().Get("title"))
	}, ""))
	m.HandleFunc("GET /metrics", h.serve(h.metrics))
	m.HandleFunc("GET /metrics/{name}", h.serve(h.series))
	m.HandleFunc("GET /measurements/{id}", h.serve(h.measurement))
	h.mountWith(m)
	return h
}

// crossOriginRefusal is the refusal of a browser write from an origin that the server does not trust: the guard of
// the API writes it. Each call gives a new error, so no caller can change the refusal of another.
func crossOriginRefusal() error {
	return &core.Error{Status: http.StatusForbidden, Msg: "cross-origin browser write refused", Code: siren.CodeCrossOrigin}
}

// authorityRefusal is the refusal of a request whose authority the listener does not take (NetworkAuthority). Each call
// gives a new error, as crossOriginRefusal does.
func authorityRefusal() error {
	return &core.Error{Status: http.StatusForbidden, Msg: "HTTP authority refused"}
}

// redirect is returned by a handler that answers with another resource's address.
type redirect struct{ to string }

func (r redirect) Error() string { return "see " + r.to }

// admitted is the request of an action after admit. The handler reads the writer, the fields and the id from it, and
// reads nothing of the request that the catalog does not list.
type admitted struct {
	act spec       // the action's row of the catalog
	src string     // the writer (aminte_meta.source); "" for a read that is not the owner's alone
	v   url.Values // the fields, checked against act; nil for an action with a file field (parseFileForm)
	id  int64      // the {id} of the path, when the row has a numeric one; else 0
}

// admit is the one opening of every action: mount and the forward to a connected app both go through it, and a
// handler runs only after it. So a handler cannot skip a step, and a new action has none to forget. In this order, admit
//   - reads the writer of a write, and of an owner read, and refuses a writer that is not the owner for an Owner row
//     (an agent or an import). Any other read reads no writer, so its source header cannot refuse it;
//   - parses the {id} of the path when the row has one that is a number (404 when it is not a number);
//   - reads the fields with fieldsOf and refuses a request that breaks the shape of the row (422, or 415 for a multipart
//     body). An action with a file field is the one exception: parseFileForm reads its fields, and the handler calls it.
func admit(r *http.Request, act spec) (admitted, error) {
	a := admitted{act: act}
	if act.Method != http.MethodGet || act.Owner {
		a.src = source(r)
	}
	if act.Owner {
		if err := requireOwner(a.src); err != nil {
			return a, err
		}
	}
	// A route without {id} has none to parse: the forward to a connected app has the path of the app, whatever its fields say.
	if r.PathValue("id") != "" && slices.ContainsFunc(act.Fields, func(f siren.Field) bool { return f.In == "path" && f.Name == "id" && f.Type == "number" }) {
		id, err := idOf(r)
		if err != nil {
			return a, err
		}
		a.id = id
	}
	if !slices.ContainsFunc(act.Fields, func(f siren.Field) bool { return f.Type == "file" }) {
		v, err := fieldsOf(r, act)
		if err != nil {
			return a, err
		}
		a.v = v
	}
	return a, nil
}

// sent are the fields of the action that the request names, as the values to send to a service that this server
// forwards the action to: a field that the request leaves out stays out, which is what an edit needs.
func (a admitted) sent() map[string]string {
	values := map[string]string{}
	for _, f := range fieldNames(a.act) {
		if a.v.Has(f) {
			values[f] = a.v.Get(f)
		}
	}
	return values
}

// actionFunc runs one action of the catalog on the request that admit let through.
type actionFunc func(r *http.Request, a admitted) (*siren.Entity, error)

// mount gives each action of cat a route to its handler, with the method and the path of the action, and runs admit
// before the handler. mount panics when an action has no handler or a handler has no action: the server does not
// start when the catalog and the routes disagree.
func (h *server) mount(m *http.ServeMux, cat []spec, handlers map[string]actionFunc) {
	named := map[string]bool{}
	for _, s := range cat {
		named[s.Name] = true
		if handlers[s.Name] == nil {
			panic("api: the action " + s.Name + " has no handler")
		}
	}
	for name := range handlers {
		if !named[name] {
			panic("api: the handler " + name + " has no action in the catalog")
		}
	}
	for _, s := range cat {
		f := handlers[s.Name]
		m.HandleFunc(s.Method+" "+s.Path, h.serve(func(r *http.Request) (*siren.Entity, error) {
			a, err := admit(r, s)
			if err != nil {
				return nil, err
			}
			return f(r, a)
		}))
	}
}

// serve is the route of one resource: it runs f and answers its entity as Siren JSON. A redirect of f is a 303. A POST
// that made a thing answers it, with its address in Location. A read that asks for embed gets the sub-entities in the
// answer (embed.go).
func (h *server) serve(f func(*http.Request) (*siren.Entity, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e, err := guarded(f, r)
		status := http.StatusOK
		var rd redirect
		switch {
		case errors.As(err, &rd):
			http.Redirect(w, r, rd.to, http.StatusSeeOther)
			return
		case err != nil:
			e, status = errorEntity(r, err)
		case r.Method == "POST":
			if self := selfOf(e); self != "" {
				w.Header().Set("Location", self)
			}
		}
		if r.Method == "GET" && status < 400 {
			h.embed(r, e)
		}
		answer(w, r, status, e)
	}
}

// guarded runs the handler f of a route and turns a panic of it into an error. serve writes nothing before f returns,
// so the answer of that error, the 500 entity, can still be sent whole, and errorEntity logs the cause with the route
// (logFault). Without this, net/http would drop the connection and the owner would see no line for the route.
// http.ErrAbortHandler is the standard way to end a response on purpose: it goes on up.
func guarded(f func(*http.Request) (*siren.Entity, error), r *http.Request) (e *siren.Entity, err error) {
	defer func() {
		if p := recover(); p != nil {
			if p == http.ErrAbortHandler {
				panic(p)
			}
			e, err = nil, fmt.Errorf("panic: %v\n%s", p, debug.Stack())
		}
	}()
	return f(r)
}

// errorEntity is the one error representation: status, a code a client branches on, and the message. An error that
// core did not type (not a core.Error, not an ExistsError) is a fault of the server: a defect, or the disk or the
// driver failed. Its text can name a file or show the driver, so the client gets the message "internal error", and
// the server's log gets the cause (logFault). A cancelled request is not logged: its client went away, or the server
// stopped it, and no fault caused it. A timeout keeps its text.
func errorEntity(r *http.Request, err error) (*siren.Entity, int) {
	status, code, msg := http.StatusInternalServerError, "", err.Error()
	e := &siren.Entity{Class: []string{"error"}, Title: "Error", Links: []siren.Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}}}
	var ce *core.Error
	var ex *core.ExistsError
	var offer *existsOffer
	switch {
	case errors.Is(err, context.DeadlineExceeded): // before the typed errors: a refusal that wraps a timeout is a timeout
		status = http.StatusGatewayTimeout
	case errors.As(err, &ex):
		status, code = http.StatusConflict, siren.CodeExists
		e.Links = append(e.Links, siren.Link{Rel: []string{"existing"}, Href: siren.PageHref(ex.ID), Title: ex.Title})
		if errors.As(err, &offer) {
			e.Links = append(e.Links, siren.Link{Rel: []string{"promote"}, Href: offer.promote, Title: offer.title})
		}
	case errors.As(err, &ce):
		status, code = ce.Status, ce.Code
		if ce.Metric != "" {
			e.Links = append(e.Links, siren.Link{Rel: []string{"register"}, Href: siren.NewMetricHref(ce.Metric), Title: "Add the metric " + ce.Metric})
		}
	default:
		msg = "internal error"
		if !errors.Is(err, context.Canceled) {
			logFault(r, "internal error", err)
		}
	}
	if code == "" {
		code = codeOf(status)
	}
	e.Properties = map[string]any{"status": status, "code": code, "message": msg}
	return e, status
}

// logFault writes a fault of the server to the standard log, which the owner reads on the server's stderr: the route,
// what failed and the cause. A client never gets the cause. The route is the pattern that the request matched, not
// its path, because a path can name a metric or a title. A request that a guard answers before the routes has no
// pattern: its method stands for the route.
func logFault(r *http.Request, what string, err error) {
	route := r.Pattern
	if route == "" {
		route = r.Method
	}
	log.Printf("aminte: %s: %s: %v", route, what, err)
}

// codeOf is the general code of a status, for an error that names no more specific one.
func codeOf(status int) string {
	switch status {
	case http.StatusBadRequest:
		return siren.CodeBadRequest
	case http.StatusUnauthorized:
		return siren.CodeUnauthorized
	case http.StatusForbidden:
		return siren.CodeForbidden
	case http.StatusNotFound:
		return siren.CodeNotFound
	case http.StatusConflict:
		return siren.CodeConflict
	case http.StatusRequestEntityTooLarge:
		return siren.CodeTooLarge
	case http.StatusUnsupportedMediaType:
		return siren.CodeUnsupportedMedia
	case http.StatusUnprocessableEntity:
		return siren.CodeInvalid
	case http.StatusServiceUnavailable:
		return siren.CodeBusy
	case http.StatusBadGateway:
		return siren.CodeUpstream
	case http.StatusGatewayTimeout:
		return siren.CodeTimeout
	}
	return siren.CodeInternal
}

// retryAfter is the Retry-After of a 503, in seconds. A 503 is a busy file (siren.CodeBusy) or an app that is not
// running (siren.CodeUnavailable). The writer has waited on a busy file for busy_timeout already, so a short wait
// before the next try is enough for both.
const retryAfter = "1"

// writeError answers with the error entity of err. The guards and the pictures use it. A route that serve wraps answers
// its error in serve.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e, status := errorEntity(r, err)
	answer(w, r, status, e)
}

// answer is the one way an entity leaves the API: the answer of every route, guard and error, as Siren JSON. The
// answer is held whole before its first byte, so a fault found while it is made can still answer 500. A failed write
// is logged: the client went away, and nothing else can be done.
func answer(w http.ResponseWriter, r *http.Request, status int, e *siren.Entity) {
	status, b := sirenAnswer(r, status, e)
	w.Header().Set("Content-Type", sirenType)
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.WriteHeader(status)
	if _, err := w.Write(b); err != nil {
		logFault(r, "the answer was not sent", err)
	}
}

// sirenAnswer is the status and the Siren JSON of e. A value that JSON cannot carry (a NaN, an infinity) is a fault
// of this program: the answer is then the error entity of the fault, with 500.
func sirenAnswer(r *http.Request, status int, e *siren.Entity) (int, []byte) {
	b, err := sirenJSON(e)
	if err == nil {
		return status, b
	}
	e, status = errorEntity(r, fmt.Errorf("the answer cannot be encoded as JSON: %w", err))
	if b, err = sirenJSON(e); err != nil { // the error entity holds a number and strings only: a defect
		logFault(r, "the error cannot be encoded as JSON", err)
		return status, nil
	}
	return status, b
}

// sirenJSON is the Siren JSON of e, indented, with no HTML escapes. The answer is held whole before its first byte,
// as a page is. An entity is small: a list is paged, and this writer takes a body in a request of at most
// maxRequestBody bytes.
func sirenJSON(e *siren.Entity) ([]byte, error) {
	var b bytes.Buffer
	if err := siren.WriteJSON(&b, e, "  "); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// source is the writer of a request: its header, else "api". The name is not checked here. A client that is a browser
// sends its own name (the extension aminte-web sends "ui").
func source(r *http.Request) string {
	if s := r.Header.Get(siren.SourceHeader); s != "" {
		return s
	}
	return "api"
}

// requireOwner refuses a writer that is not the owner: an agent (agent:*) or an import (import:*). The owner-only
// actions (the catalog's Owner mark) are refused to any other writer, on every surface.
// It checks the grammar of the name too, which repeats the CHECKs on purpose. An owner-only action can read and meet
// no CHECK, and the prefix tests of core.AgentWriter would pass "Agent:x" as the owner. Any other write leaves the name to the
// schema.
func requireOwner(source string) error {
	if !core.WriterName(source) {
		return core.Invalid("source %q: 1-64 characters of a-z 0-9 _ : . -", source)
	}
	if core.AgentWriter(source) {
		return &core.Error{Status: http.StatusForbidden, Msg: "this action is the owner's: an agent or an import may not run it"}
	}
	return nil
}

// actions is the catalog: every action with its templated href. The action that new names is filled in instead,
// when the query names a value for each of its path fields (GET /actions?new=rename&id=12): its href is then an
// address that a form can post to. A form cannot fill a path, so this is how /actions offers every action to a UI
// (docs/plans/131-the-pareto-pass.md).
func (h *server) actions(r *http.Request) (*siren.Entity, error) {
	q := r.URL.Query()
	e := &siren.Entity{Class: []string{"actions"}, Title: "Actions", Links: []siren.Link{link("self", "/actions", "Actions"), link("index", "/", "Home")}}
	for _, s := range append(slices.Clone(catalog), h.with.specs()...) { // the actions of the connected apps follow
		if params, ok := pathValues(s, q); ok && s.Name == q.Get("new") {
			e.Actions = append(e.Actions, filled(s, params, nil))
			continue
		}
		e.Actions = append(e.Actions, templated(s))
	}
	for i := range e.Actions { // this page lists every action with the API text, not the short text of a form
		e.Actions[i].Hint = ""
	}
	return e, nil
}

// pathValues are the values that q names for the path fields of s, and whether it names a value for each one.
func pathValues(s spec, q url.Values) (map[string]string, bool) {
	params := map[string]string{}
	for _, f := range s.Fields {
		if f.In != "path" {
			continue
		}
		if q.Get(f.Name) == "" {
			return nil, false
		}
		params[f.Name] = q.Get(f.Name)
	}
	return params, true
}
