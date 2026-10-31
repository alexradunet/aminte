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
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"lifelog/internal/api/view"
	"lifelog/internal/core"
	"lifelog/siren"
)

// server is the API on one store. The store's clock is the server's one clock: today is core.Store.Today, the words
// for a time and the name of a snapshot are in the zone of core.Store.Local, and the life of a feedback receipt is
// measured with core.Store.Now.
type server struct {
	s        *core.Store
	feedback feedbackStore
	mux      *http.ServeMux
	with     *Apps // the connected apps (with.go); shared with the other handler of the same serve
}

// New returns the whole API as one handler: lifelog serve mounts it on a socket, the CLI and the MCP server
// call it in-process. It trusts no browser origin other than its own. A public listener uses Public, which builds the
// API with the origins that the owner allows (docs/plans/078-network-server.md). A test gives a store with a fixed
// clock, so today is a fixed day and a time is written in fixed words.
func New(s *core.Store) http.Handler {
	return NewWith(s, NewApps())
}

// NewWith is New with the connected apps of apps: their actions are in the catalog, and /with/NAME leads to them. A
// serve with a public listener gives the same apps to this handler and to Public (PublicOptions.Apps).
func NewWith(s *core.Store, apps *Apps) http.Handler {
	return newOn(s, http.NewCrossOriginProtection(), apps)
}

// newOn builds the API behind guard, which refuses a browser write from an origin that it does not trust. Public
// gives the guard that trusts the allowed origins; New gives one that trusts no other origin.
func newOn(s *core.Store, guard *http.CrossOriginProtection, apps *Apps) http.Handler {
	h := newServer(s)
	h.with = apps
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
func newServer(s *core.Store) *server {
	h := &server{s: s, feedback: feedbackStore{now: s.Now}, with: NewApps()}
	m := http.NewServeMux()
	h.mux = m
	h.mount(m, catalog, map[string]actionFunc{
		"capture": h.capture, "create-page": h.createPage, "save-body": h.saveBody, "create-person": h.createPerson,
		"add-file": h.addFile, "set-cover": h.setCover, "promote": h.promote, "edit-person": h.editPerson, "edit-metric": h.editMetric,
		"revive": h.revive, "tombstone-pages": h.tombstoneMany, "file-pages": h.filePages, "record": h.record, "correct": h.correct,
		"retract": h.retract, "rename": h.rename, "register-metric": h.registerMetric, "integrity-check": h.integrity,
		"search": h.search, "browse": h.browse, "find": h.find, "open-day": h.openDay,
		"random-page": h.randomPage, "snapshot": h.snapshot,
		"rebuild-search": h.rebuildSearch, "resync-links": h.resyncLinks, "say": h.say, "settings": h.saveSettings,
	})
	// The routes that are not actions: the resources, the editor's helpers and the files of the browser.
	m.HandleFunc("GET /{$}", h.serve(h.root))
	m.HandleFunc("GET /actions", h.serve(h.actions))
	m.HandleFunc("GET /days", h.serve(h.days))
	m.HandleFunc("GET /days/today", h.serve(h.today))
	m.HandleFunc("GET /days/{day}", h.serve(h.day))
	m.HandleFunc("GET /days/{day}/conversation", h.serve(h.conversation))
	m.HandleFunc("GET /settings", h.serve(h.settings))
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
	m.HandleFunc("POST /render", h.render)
	h.mountWith(m)
	h.mountTasks(m)
	m.HandleFunc("GET /assets/fonts/{file}", fonts)
	m.HandleFunc("GET /manifest.webmanifest", install("manifest.webmanifest", "application/manifest+json", "public, max-age=3600"))
	// The rules of speculation. JSON has no comments, so the reasons are here. No address with a query string: a
	// ?feedback= receipt is read once, and ?new= opens a form. The pattern is "/*\?*=*", with the "=" every query of
	// this app has: a URLPattern "/*\?*" alone has the search "*", which an empty search matches too, so it would
	// exclude every address (probed with a URLPattern implementation on 2026-10-28; no Go test can run one). The
	// others are the routes that redirect (today, go, random, find), the pictures, the downloads of a snapshot, the install file and the integrity
	// check, which a hover must not run. A browser without speculation rules ignores the header.
	m.HandleFunc("GET /assets/speculation.json", install("speculation.json", "application/speculationrules+json", "public, max-age=3600"))
	m.HandleFunc("GET /assets/chat.js", install("chat.js", "text/javascript; charset=utf-8", "public, max-age=300"))
	m.HandleFunc("GET /assets/lock.js", install("lock.js", "text/javascript; charset=utf-8", "public, max-age=300"))
	m.HandleFunc("GET /assets/icon.svg", install("icon.svg", "image/svg+xml", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/icon-192.png", install("icon-192.png", "image/png", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/icon-512.png", install("icon-512.png", "image/png", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/wood.png", install("wood.png", "image/png", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/ground.png", install("ground.png", "image/png", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/embers.png", install("embers.png", "image/png", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/sprites.png", install("sprites.png", "image/png", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/icons.png", install("icons.png", "image/png", "public, max-age=31536000, immutable"))
	return h
}

// crossOriginRefusal is the refusal of a browser write from an origin that the server does not trust: the guard of
// the API and the login forms of a public listener write it. Each call gives a new error, so no caller can change the
// refusal of another.
func crossOriginRefusal() error {
	return &core.Error{Status: http.StatusForbidden, Msg: "cross-origin browser write refused", Code: siren.CodeCrossOrigin}
}

// authorityRefusal is the refusal of a request whose authority the listener does not take: the local listener and the
// public one write it. Each call gives a new error, as crossOriginRefusal does.
func authorityRefusal() error {
	return &core.Error{Status: http.StatusForbidden, Msg: "HTTP authority refused"}
}

// redirect is returned by a handler that answers with another resource's address.
type redirect struct{ to string }

func (r redirect) Error() string { return "see " + r.to }

// actionFunc runs one action of the catalog. act is the action's row of the catalog, which mount gives: the handler
// reads its fields with it (fieldsOf), so a handler does not write its own name. src is the writer of the request
// (lifelog_meta.source); a read that is not the owner's alone gets "".
type actionFunc func(r *http.Request, act spec, src string) (*siren.Entity, error)

// mount gives each action of cat a route to its handler, with the method and the path of the action, and gives the
// handler the action's row. An action that writes reads its writer. An Owner action reads its writer and refuses a
// writer that is not the owner (an agent or an import). Any other read reads no writer, so its source header cannot
// refuse it. mount panics when an action has no handler or a handler has no action: the server does not start when
// the catalog and the routes disagree.
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
			var src string
			if s.Method != "GET" || s.Owner {
				src = source(r)
			}
			if s.Owner {
				if err := requireOwner(src); err != nil {
					return nil, err
				}
			}
			return f(r, s, src)
		}))
	}
}

func (h *server) serve(f func(*http.Request) (*siren.Entity, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var e *siren.Entity
		var err error
		if r.Method == "GET" && wantsHTML(r) && r.URL.Query().Has(embedField) { // refused, not ignored: a page reads its sub-entities itself
			err = core.Invalid("embed is for the Siren answer: send Accept: application/vnd.siren+json")
		} else {
			e, err = guarded(f, r)
		}
		status := http.StatusOK
		var rd redirect
		switch {
		case errors.As(err, &rd):
			if token := r.URL.Query().Get("feedback"); token != "" { // the toast follows the redirect
				if to, perr := url.Parse(rd.to); perr == nil {
					h.feedback.move(token, r.URL.Path, to.Path)
				}
			}
			http.Redirect(w, r, rd.to, http.StatusSeeOther)
			return
		case err != nil:
			e, status = errorEntity(r, err)
		case r.Method == "POST":
			if self := selfOf(e); self != "" {
				if wantsHTML(r) { // a browser form: post, then redirect, so a reload never posts again
					to := self
					if back := backOf(r); back != "" {
						to = back
					}
					http.Redirect(w, r, h.feedback.put(to, e.Result), http.StatusSeeOther)
					return
				}
				w.Header().Set("Location", self)
			}
		}
		if r.Method == "GET" && status < 400 && !wantsHTML(r) {
			h.embed(r, e) // the Siren answer only: a page reads its sub-entities itself (shellFacts)
		}
		if r.Method == "GET" && wantsHTML(r) && status < 400 {
			if feedback := h.feedback.take(r.URL.Path, r.URL.Query().Get("feedback")); feedback != "" {
				e.Result = feedback
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
			}
		}
		answer(w, r, status, e, frame{srv: h, submitted: submittedText(r)})
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

// textareas are the names of the long-text fields of the catalog (a body, an entry), sorted, so the first with a value
// does not depend on the order of the catalog.
var textareas = func() []string {
	var names []string
	for _, s := range catalog {
		for _, f := range s.Fields {
			if f.Type == "textarea" && !slices.Contains(names, f.Name) {
				names = append(names, f.Name)
			}
		}
	}
	slices.Sort(names)
	return names
}()

// submittedText is the long text that a POST sent, for the page of a refusal to show it again: the value of the first
// long-text field of the catalog that has one. A request that is not a POST sent none.
func submittedText(r *http.Request) string {
	if r.Method != "POST" {
		return ""
	}
	for _, name := range textareas {
		if v := r.PostForm.Get(name); v != "" {
			return v
		}
	}
	return ""
}

// maxBack is the longest return path a browser form may send.
const maxBack = 2048

// backFragment is the id that a return path may end with: a browser never sends a fragment to a server, so it only
// names where the page opens (the row that a write came from).
var backFragment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// backOf is the page a browser form asks to return to after its write (docs/plans/091-the-browser-for-daily-use.md):
// the form field back, when it is a path on this server, "" otherwise. Only a path that starts with one slash is
// kept: "//host" and "/\host" name another host to a browser, and a scheme, a host or a control character makes the
// value something other than a path here. The path may end with one id fragment (backFragment). Any other "#" makes
// the value not a path. A value that is not kept is ignored, not refused: the write is done, and the browser goes to
// the resource it changed.
func backOf(r *http.Request) string {
	b := r.PostForm.Get("back")
	path, fragment, hasFragment := strings.Cut(b, "#")
	if b == "" || len(b) > maxBack || !strings.HasPrefix(b, "/") || strings.HasPrefix(b, "//") ||
		strings.Contains(b, "\\") || hasFragment && !backFragment.MatchString(fragment) ||
		strings.IndexFunc(b, func(c rune) bool { return c < 0x20 || c == 0x7f }) >= 0 {
		return ""
	}
	u, err := url.Parse(path)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") {
		return ""
	}
	return b
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
	if r.Context().Value(quietKey{}) != nil {
		return
	}
	route := r.Pattern
	if route == "" {
		route = r.Method
	}
	log.Printf("lifelog: %s: %s: %v", route, what, err)
}

// quietKey marks the context of a read whose faults the log does not repeat: the reads that the page of an error makes
// for its shell (shellFacts). The error has logged its own cause already, and a fault that has the same cause again
// would be a second line for one failure.
type quietKey struct{}

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

// writeError answers with the error entity of err, as JSON or as a page with no shell. The guards, the pictures and
// /render use it. A route that serve wraps answers its error in serve: there a browser gets the page in the shell.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e, status := errorEntity(r, err)
	answer(w, r, status, e, frame{submitted: submittedText(r)})
}

// frame says how answer draws an entity as a page, and for whom. The zero frame gives a program JSON and a browser a
// page with no sidebar: the answer of a guard, of a picture's error and of a refusal before the routes.
type frame struct {
	// srv is the server whose own handler gives the page its shell (shellFacts); nil: no sidebar, so the page shows
	// nothing of the database.
	srv      *server
	pageOnly bool // a page for every client, a program too: the answer is presentation and has no JSON form
	// submitted is the long text that a refused browser form sent (submittedText), which the page of the refusal shows
	// again. It is used only when the answer is the error of a POST.
	submitted string
}

// hasShell reports a page that has the sidebar. A server with no store has no database to show.
func (fr frame) hasShell() bool { return fr.srv != nil && fr.srv.s != nil }

// answer is the one way an entity leaves the API: the answer of every route, guard and error. A program gets Siren
// JSON; a browser, and every client of a pageOnly frame, gets a page, and only then is the shell read. The answer is
// held whole before its first byte, so a fault found while it is made can still answer 500. A failed write is
// logged: the client went away, and nothing else can be done.
func answer(w http.ResponseWriter, r *http.Request, status int, e *siren.Entity, fr frame) {
	typ, b := sirenType, []byte(nil)
	if fr.pageOnly || wantsHTML(r) {
		typ = "text/html; charset=utf-8"
		status, b = pageAnswer(r, status, e, fr)
	} else {
		status, b = sirenAnswer(r, status, e)
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Add("Vary", "Accept") // one URL, two representations
	if fr.hasShell() && typ == "text/html; charset=utf-8" {
		w.Header().Set("Speculation-Rules", `"/assets/speculation.json"`) // a quoted URL string
	}
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.WriteHeader(status)
	if _, err := w.Write(b); err != nil {
		logFault(r, "the answer was not sent", err)
	}
}

// pageAnswer is the status and the HTML of the page of e. When the page cannot be drawn, a defect of this program,
// the owner reads the cause in the log and a client never does: the page is DrawFailed, with 500.
func pageAnswer(r *http.Request, status int, e *siren.Entity, fr frame) (int, []byte) {
	f := view.Facts{URL: r.URL}
	if fr.hasShell() {
		fr.srv.shellFacts(r, status, e, &f)
	}
	if status >= 400 && r.Method == "POST" {
		f.Submitted = fr.submitted
		if ref, err := url.Parse(r.Referer()); err == nil && ref.Host == r.Host && strings.HasPrefix(ref.Path, "/") {
			f.Back = ref.RequestURI()
		}
	}
	b, err := view.Draw(e, f)
	if err != nil {
		logFault(r, "the view "+view.TemplateOf(e)+" cannot be drawn", err)
		return http.StatusInternalServerError, []byte(view.DrawFailed)
	}
	return status, b
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

func wantsHTML(r *http.Request) bool {
	a := r.Header.Get("Accept")
	return strings.Contains(a, "text/html") && !strings.Contains(a, "json")
}

// source is the writer of a request: its header, else "ui" for a browser and "api". The name is not checked here.
func source(r *http.Request) string {
	s := r.Header.Get(siren.SourceHeader)
	switch {
	case s != "":
	case wantsHTML(r):
		s = "ui"
	default:
		s = "api"
	}
	return s
}

// writerRE is the grammar of a writer name: entities_source, measurements_source and links_source.
var writerRE = regexp.MustCompile(`^[a-z0-9_:.-]{1,64}$`)

// requireOwner refuses a writer that is not the owner: an agent (agent:*) or an import (import:*). The owner-only
// actions (the catalog's Owner mark) are refused to any other writer, on every surface.
// It checks the grammar of the name too, which repeats the CHECKs on purpose. An owner-only action can read and meet
// no CHECK, and the prefix tests below would pass "Agent:x" as the owner. Any other write leaves the name to the
// schema.
func requireOwner(source string) error {
	if !writerRE.MatchString(source) {
		return core.Invalid("source %q: 1-64 characters of a-z 0-9 _ : . -", source)
	}
	if strings.HasPrefix(source, siren.AgentPrefix) || strings.HasPrefix(source, siren.ImportPrefix) {
		return &core.Error{Status: http.StatusForbidden, Msg: "this action is the owner's: an agent or an import may not run it"}
	}
	return nil
}

// actions is the catalog: every action with its templated href. The action that new names is filled in instead,
// when the query names a value for each of its path fields (GET /actions?new=rename&id=12): its href is then an
// address that a form can post to. A browser cannot fill a path from a form, so this is how /actions offers every
// action to a browser (docs/plans/131-the-pareto-pass.md).
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
