package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"lifelog/client"
	"lifelog/internal/core"
	"lifelog/siren"
)

// A connected app (docs/plans/154-connected-apps-through-the-catalog.md) is a program that `lifelog serve --with NAME`
// runs. It serves its own Siren catalog on a loopback address. This file puts its actions in the catalog of the
// server, under the prefix NAME-, and forwards the requests to /with/NAME to it. The server names no app and no
// domain: the catalog of the app is the whole interface.

// forwardTimeout is the longest wait for a connected app to answer one forwarded request. It is less than the limit
// of the request itself (defaultLimits), so an app that is slow gives the request's 504 timeout and not a cut
// connection. It is a variable so that a test can shorten it.
var forwardTimeout = 30 * time.Second

// appNameRE is the grammar of the name of an action of an app. With the prefix NAME- it is the name of an action of
// the merged catalog.
var appNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// Apps is the registry of the connected apps of one server. The handler of the API and serve (which starts the children)
// share one value, so both see the same apps. It is safe for use by many goroutines. Make it with NewApps.
type Apps struct {
	mu   sync.RWMutex
	apps map[string]liveApp // by name; a name that is here is known
	// tasks is the task service, the one child whose catalog has the class tasks (tasks.go). It is not an app: it has
	// no name in apps, no /with/NAME and no action with a prefix. Nil while none runs.
	tasks *taskService
}

// liveApp is a connected app that the server knows. c is nil while the app does not run.
type liveApp struct {
	c       *client.Client
	title   string
	actions []siren.Action
}

// NewApps returns a registry with no app.
func NewApps() *Apps { return &Apps{apps: map[string]liveApp{}} }

// Expect says that the app NAME exists, before it runs. Until Register, and after Remove, a request to it is 503
// unavailable.
func (a *Apps) Expect(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.apps[name]; !ok {
		a.apps[name] = liveApp{}
	}
}

// registerTimeout is the longest wait for one answer of a child in the handshake. A child that holds the connection
// open must not stop the server. It is a variable so that a test can shorten it.
var registerTimeout = 5 * time.Second

// Register is the one handshake of a child of `serve --with`, and of the child of a test (lifelogtest): it makes the
// child at base (http://127.0.0.1:PORT) known as what its catalog says (extensions/README.md, "A catalog of its own"
// and "A task service").
//
//	GET /actions    the entity must have the class actions. Else the child has no catalog.
//	  class tasks   the child is the task service: GET /lists gives its lists (setTasks).
//	  no tasks      the child is an app: GET / gives its title, and its name is the title when the home gives none (set).
//
// The registry refuses a catalog for the reasons of set and setTasks, and stays as it was. The answer says what the
// child became, for the line of a log. When the child does not answer GET /actions, the error wraps the cause of the
// client, so the caller can tell a child that is not up yet (a dial that fails, or a late answer) from a child that has
// no catalog; no other error wraps a cause.
func (a *Apps) Register(ctx context.Context, name, base string) (Registered, error) {
	c := client.Remote(base, "", "")
	get := func(href string) (*siren.Entity, error) {
		ctx, cancel := context.WithTimeout(ctx, registerTimeout)
		defer cancel()
		return c.Get(ctx, href)
	}
	e, err := get("/actions")
	var ce *client.Error
	switch {
	case errors.As(err, &ce):
		return Registered{}, fmt.Errorf("no catalog (status %d)", ce.Status)
	case err != nil:
		return Registered{}, fmt.Errorf("no catalog (%w)", err)
	case !slices.Contains(e.Class, "actions"):
		return Registered{}, errors.New("no catalog (the answer is not an actions entity)")
	case slices.Contains(e.Class, "tasks"):
		listed, err := get("/lists")
		var lists []taskList
		if err == nil {
			lists, err = taskListsOf(listed)
		}
		if err != nil {
			return Registered{}, fmt.Errorf("catalog refused: the task service gives no lists: %v", err)
		}
		if err := a.setTasks(name, base, e.Actions, lists); err != nil {
			return Registered{}, fmt.Errorf("catalog refused: %v", err)
		}
		return Registered{Tasks: true, Actions: len(e.Actions), Lists: len(lists)}, nil
	}
	title := name
	if home, err := get("/"); err == nil && home.Title != "" {
		title = home.Title
	}
	if err := a.set(name, base, title, e.Actions); err != nil {
		return Registered{}, fmt.Errorf("catalog refused: %v", err)
	}
	return Registered{Actions: len(e.Actions)}, nil
}

// Registered says what Register made of a child: the task service (with its lists), or an app. Actions are the
// actions of its catalog.
type Registered struct {
	Tasks   bool
	Actions int
	Lists   int
}

// set says that the app NAME runs at base (http://127.0.0.1:PORT), with its home title and its catalog. It refuses
// the whole catalog, and the app stays as it was, when: the name of an action is not a-z 0-9 and -; the href of an
// action is not a path (it does not start with one slash, or it has a scheme, a host, a query or a fragment); the
// name of an action with the prefix NAME- is the name of an action of the static catalog or of another running app;
// or an action has a field of type file. This version of the server does not forward a multipart body.
func (a *Apps) set(name, base, title string, actions []siren.Action) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, act := range actions {
		if err := checkAppAction(act); err != nil {
			return fmt.Errorf("the app %s: %w", name, err)
		}
		taken := name + "-" + act.Name
		if _, ok := lookup(taken); ok || isTaskAction(taken) {
			return fmt.Errorf("the app %s: the action %s is in the catalog already", name, taken)
		}
		for other, o := range a.apps {
			if other == name {
				continue
			}
			for _, x := range o.actions {
				if other+"-"+x.Name == taken {
					return fmt.Errorf("the app %s: the action %s is an action of the app %s", name, taken, other)
				}
			}
		}
	}
	a.apps[name] = liveApp{c: client.Remote(base, "", ""), title: title, actions: slices.Clone(actions)}
	return nil
}

// setTasks says that the child NAME is the task service, at base (http://127.0.0.1:PORT), with its catalog and its
// lists (docs/plans/158-one-task-service-behind-tasks.md). It refuses the child, and the registry stays as it was,
// when another task service runs, or when the catalog lacks one of the six writes of the protocol (taskWrites). The
// name stops being an app: Expect made it known, and setTasks takes it back, so /with/NAME is 404.
func (a *Apps) setTasks(name, base string, actions []siren.Action, lists []taskList) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tasks != nil && a.tasks.name != name {
		return fmt.Errorf("a task service runs already: %s", a.tasks.name)
	}
	for _, w := range taskWrites {
		if !slices.ContainsFunc(actions, func(x siren.Action) bool { return x.Method == "POST" && x.Href == w.Href }) {
			return fmt.Errorf("the task service has no action POST %s", w.Href)
		}
	}
	delete(a.apps, name)
	a.tasks = &taskService{name: name, c: client.Remote(base, "", ""), lists: slices.Clone(lists)}
	return nil
}

// taskService is the task service that runs, and false when none does. The value is a copy that the registry never
// changes, so a request reads one consistent service.
func (a *Apps) taskService() (taskService, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tasks == nil {
		return taskService{}, false
	}
	return *a.tasks, true
}

// checkAppAction refuses an action that set does not take, with the reason.
func checkAppAction(act siren.Action) error {
	if !appNameRE.MatchString(act.Name) {
		return fmt.Errorf("the action name %q: use a-z, 0-9 and -", act.Name)
	}
	u, err := url.Parse(act.Href)
	if err != nil || !isPath(act.Href) || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("the action %s: the href %q is not a path", act.Name, act.Href)
	}
	if len(act.Class) > 1 {
		return fmt.Errorf("the action %s: %d classes %v, an action has at most one", act.Name, len(act.Class), act.Class)
	}
	for _, f := range act.Fields {
		if f.Type == "file" {
			return fmt.Errorf("the action %s: the field %s is a file, and a file is not forwarded", act.Name, f.Name)
		}
	}
	return nil
}

// Remove says that the app NAME does not run. Its name stays known. If NAME is the task service, the role is free.
func (a *Apps) Remove(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tasks != nil && a.tasks.name == name {
		a.tasks = nil
	}
	if _, ok := a.apps[name]; ok {
		a.apps[name] = liveApp{}
	}
}

// names are the names of the apps that run, sorted.
func (a *Apps) names() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var names []string
	for name, x := range a.apps {
		if x.c != nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// find is the app with the name, and whether the name is known. c of the app is nil when it does not run.
func (a *Apps) find(name string) (liveApp, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	x, ok := a.apps[name]
	return x, ok
}

// links are the apps that run, as the links app of the root: each with its home title, else its name.
func (a *Apps) links() []siren.Link {
	var out []siren.Link
	for _, name := range a.names() {
		x, _ := a.find(name)
		title := x.title
		if title == "" {
			title = name
		}
		out = append(out, link("app", "/with/"+name, title))
	}
	return out
}

// specs are the actions of the apps that run, as rows of the merged catalog: by app, then in the order of the app.
// The six actions of the task service follow while it runs.
func (a *Apps) specs() []spec {
	var out []spec
	for _, name := range a.names() {
		x, _ := a.find(name)
		for _, act := range x.actions {
			out = append(out, specOfApp(name, act))
		}
	}
	if svc, ok := a.taskService(); ok {
		out = append(out, svc.specs()...)
	}
	return out
}

// specOfApp is the row of the merged catalog for an action of the app: the name and the path have the prefix of the
// app. The short text of a form (Hint) is not the app's, so it is empty. admit and the forms read
// the row as they read a row of the static catalog.
func specOfApp(app string, act siren.Action) spec {
	return spec{Name: app + "-" + act.Name, Title: act.Title, Description: act.Description, Method: act.Method,
		Path: "/with/" + app + act.Href, Fields: slices.Clone(act.Fields), Owner: act.Owner, Danger: act.Danger, Class: knownClasses(act.Class)}
}

// knownClasses keeps the classes that this server lists (siren.ActionClasses) and drops the others, so an app cannot
// make a UI draw something it does not know. It is nil when none is left.
func knownClasses(classes []string) []string {
	var out []string
	for _, c := range classes {
		if slices.Contains(siren.ActionClasses(), c) {
			out = append(out, c)
		}
	}
	return out
}

// mountWith gives m the routes of the apps: /with/NAME and everything under it, for every method. They go through
// serve, so an error is an error entity.
func (h *server) mountWith(m *http.ServeMux) {
	m.HandleFunc("/with/{app}", h.serve(h.withApp))
	m.HandleFunc("/with/{app}/{rest...}", h.serve(h.withApp))
}

// withApp forwards a request for /with/NAME/REST to the app NAME, in the name of the writer of the request, and gives
// back the answer of the app in the namespace of the server. There is no retry: a write that was sent may have been
// done, and the caller decides.
func (h *server) withApp(r *http.Request) (*siren.Entity, error) {
	name := r.PathValue("app")
	app, known := h.with.find(name)
	switch {
	case !known:
		return nil, core.NotFound("no such app %s", name)
	case app.c == nil:
		return nil, notRunning("the app " + name)
	}
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/with/"+name) // escaped: the app decodes it, not this server
	if rest == "" {
		rest = "/"
	}
	ctx, cancel := context.WithTimeout(r.Context(), forwardTimeout)
	defer cancel()
	var e *siren.Entity
	var err error
	if act, ok := matchAction(app.actions, r.Method, rest); ok {
		a, err := admit(r, specOfApp(name, act))
		if err != nil { // the refusal is this server's own, so it does not go through forwardError
			return nil, err
		}
		concrete := siren.Action{Name: act.Name, Method: act.Method, Href: rest} // nothing is left to fill in the path
		e, err = app.c.As(source(r)).Do(ctx, concrete, a.sent())
		if err != nil {
			return nil, forwardError(r, "the app "+name, err)
		}
		return inNamespace(name, e), nil
	} else if r.Method == http.MethodGet {
		q := r.URL.Query()
		q.Del(newField)   // the form that a link asks for is this server's: the app never sees it
		q.Del(embedField) // this server embeds in the answer (serve)
		href := rest
		if len(q) > 0 {
			href += "?" + q.Encode()
		}
		e, err = app.c.As(source(r)).Get(ctx, href)
	} else {
		return nil, core.NotFound("the app %s has no action %s %s", name, r.Method, rest)
	}
	if err != nil {
		return nil, forwardError(r, "the app "+name, err)
	}
	return inNamespace(name, e), nil
}

// matchAction is the action of the app with the method whose href fits the path, segment by segment: {x} fits one
// segment that is not empty, and any other segment fits itself.
func matchAction(actions []siren.Action, method, path string) (siren.Action, bool) {
	parts := strings.Split(path, "/")
	for _, act := range actions {
		want := strings.Split(act.Href, "/")
		if act.Method != method || len(want) != len(parts) {
			continue
		}
		fits := true
		for i, w := range want {
			if isPlaceholder(w) {
				fits = fits && parts[i] != ""
			} else {
				fits = fits && w == parts[i]
			}
		}
		if fits {
			return act, true
		}
	}
	return siren.Action{}, false
}

func isPlaceholder(segment string) bool {
	return len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}'
}

// notRunning is the 503 of a child that does not run. who names it for the client: "the app NAME", or "the task
// service", whose name the client never sees.
func notRunning(who string) error {
	return &core.Error{Status: http.StatusServiceUnavailable, Code: siren.CodeUnavailable, Msg: who + " is not running"}
}

// forwardError is the error of this server for a failed call of an app. The refusal of the app passes with its status
// and its code. An app that is not there is 503, a timeout stays a timeout (504), and an answer that is not an entity
// is 502. The cause of a 502 goes to the log and not to the client. who names the child, as for notRunning.
func forwardError(r *http.Request, who string, err error) error {
	var ce *client.Error
	var op *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.As(err, &ce):
		msg := ce.Error()
		if p, ok := ce.Entity.Properties.(map[string]any); ok {
			if m, ok := p["message"].(string); ok {
				msg = m
			}
		}
		code := ce.Code()
		if code == "" {
			code = codeOf(ce.Status)
		}
		return &core.Error{Status: ce.Status, Code: code, Msg: msg}
	case errors.As(err, &op) && op.Op == "dial":
		return notRunning(who)
	}
	logFault(r, who+" gave no entity", err)
	return &core.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: who + " answered something that is not an entity"}
}

// inNamespace changes the answer of an app to the namespace of this server: each address that starts with a slash
// gets the prefix /with/NAME, and each action name gets the prefix NAME-. An action keeps its classes that are in
// siren.ActionClasses. A link, an embedded link or an action whose
// address is not a path (it has a scheme or a host) is dropped: a connected app is on a loopback address, and
// nothing it says leads elsewhere.
func inNamespace(name string, e *siren.Entity) *siren.Entity {
	prefix := "/with/" + name
	e.Links = relinked(prefix, name, e.Links)
	e.Entities = relinked(prefix, name, e.Entities)
	e.Actions = prefixed(prefix, name, e.Actions)
	return e
}

// relinked keeps the links with a path. It also rewrites the actions of an embedded link, as the actions of the entity.
// A link of an app carries no sub-entities and no links of its own: a hostile app cannot hide an address in them.
func relinked(prefix, name string, links []siren.Link) []siren.Link {
	var out []siren.Link
	for _, l := range links {
		if isPath(l.Href) {
			l.Href = prefix + l.Href
			l.Actions = prefixed(prefix, name, l.Actions)
			l.Entities, l.Links = nil, nil // an embedded link of an app is the short form: its own sub-entities are not forwarded
			out = append(out, l)
		}
	}
	return out
}

// prefixed gives each action with a path the prefix of its address and of its name, and drops the others.
func prefixed(prefix, name string, actions []siren.Action) []siren.Action {
	var out []siren.Action
	for _, a := range actions {
		if isPath(a.Href) {
			a.Href, a.Name, a.Class = prefix+a.Href, name+"-"+a.Name, knownClasses(a.Class)
			out = append(out, a)
		}
	}
	return out
}

// isPath says that href is a path on the same server: one slash first, no scheme, no host.
func isPath(href string) bool {
	u, err := url.Parse(href)
	return err == nil && strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") && u.Scheme == "" && u.Host == ""
}
