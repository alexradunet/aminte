package web

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	pathpkg "path"
	"slices"
	"strings"

	"aminte-web/view"
	"aminte/siren"
)

// embedField is the query parameter that asks the core for an embedding (API.md, "Embedding"). Web sends it for the page
// that needs it (embedFor). A page of the browser never takes it.
const embedField = "embed"

// feedbackField is the one-use receipt of the toast after a write (feedback.go). Web alone reads it, so web does not send
// it to the core (webFields).
const feedbackField = "feedback"

// refRel is the rel of the sub-entities of a conversation that name what its agent touched.
const refRel = "ref"

// page is the answer to a GET of any path that web does not serve itself. It asks the core for the same path and
// query as Siren JSON and then does one of three things with the answer:
//
//   - A redirect of the core is the redirect of the browser. A path of the core is a path of web.
//   - A Siren entity is drawn as a page, in the shell, with the status of the core.
//   - Any other answer (a picture, a file) streams to the browser as the core sent it.
func (b *Browser) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has(embedField) { // refused, not ignored: web asks for the embedding that its page needs
		b.fail(w, r, http.StatusUnprocessableEntity, siren.CodeInvalid, "embed is a parameter of the Siren answer, not of a page")
		return
	}
	if (r.URL.Path == appHome || r.URL.Path == appHome+"/") && wantsHTML(r) {
		http.Redirect(w, r, "/settings", http.StatusSeeOther) // the form of this browser is on the page of the settings
		return
	}
	uri := coreURI(r.URL)
	if rels := embedFor(r.URL.Path); rels != "" && !wantsSiren(r) { // a script that reads the entity gets it as the core sends it
		uri = withEmbed(uri, rels)
	}
	res, err := b.core.get(r.Context(), uri, pageWriter, r.Header)
	if err != nil {
		b.unreachable(w, r, err)
		return
	}
	defer res.Body.Close()
	switch {
	case isRedirect(res.StatusCode):
		b.redirect(w, r, res.Header.Get("Location"), res.StatusCode)
	case isSiren(res) && wantsSiren(r):
		// A script of the page (the poll of chat.js) reads the entity itself. The session is the owner's, and the
		// answer is no-store as every answer of a session is (serveSession).
		stream(w, r, res)
	case isSiren(res):
		e, err := b.core.readEntity(res)
		if err != nil {
			logFault(r, "the answer of the core cannot be read", err)
			b.fail(w, r, http.StatusBadGateway, siren.CodeUpstream, "the core gave an answer that is not an entity")
			return
		}
		b.draw(w, r, res.StatusCode, e, res.Header.Get("Retry-After"), view.Facts{URL: r.URL})
	default:
		stream(w, r, res)
	}
}

// embedFor is the embedding that the page at path needs, "" for none. Home is the page of today, and the root gives the
// shell too (homeOf). The conversation view is the chat at full size: the core embeds the entity at each address that
// its tool lines name. A read that fails keeps the page without its cards.
func embedFor(path string) string {
	if path == "/" {
		return "today,conversation"
	}
	if ok, _ := pathpkg.Match("/days/*/conversation", path); ok {
		return refRel
	}
	return ""
}

// draw answers with the page of e, which the core answered with status. retryAfter is the header of that answer. f holds
// the facts that the caller knows: the address, and for the error of a form the text it sent and the page it came from.
// The parts of a page that a GET adds (the day of Home, the view of a metric, the form of the settings of this browser,
// the toast) are not added to the answer of a POST.
func (b *Browser) draw(w http.ResponseWriter, r *http.Request, status int, e *siren.Entity, retryAfter string, f view.Facts) {
	var root *siren.Entity // the root of the API with the day of today and the chat embedded, when this page asked for it
	reading := r.Method != http.MethodPost
	if reading && r.URL.Path == "/" && status < http.StatusBadRequest {
		var ok bool
		if root, e, ok = homeOf(e); !ok {
			b.fail(w, r, http.StatusBadGateway, siren.CodeUpstream, "the core gave no day of today")
			return
		}
	}
	if reading && status < http.StatusBadRequest {
		if to := ownView(e); to != "" {
			// The query goes with the page, the receipt of the toast too: the toast is drawn where the browser lands.
			if token := r.URL.Query().Get(feedbackField); token != "" {
				if u, err := url.Parse(to); err == nil {
					b.feedback.move(token, r.URL.Path, u.Path)
				}
			}
			b.redirect(w, r, withQuery(to, r.URL.RawQuery), http.StatusSeeOther)
			return
		}
		if r.URL.Path == "/settings" && slices.Contains(e.Class, "settings") {
			b.offerBrowserSettings(r, e)
		}
		if r.Method == http.MethodGet {
			if text := b.feedback.take(r.URL.Path, r.URL.Query().Get(feedbackField)); text != "" {
				e.Result = text
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
			}
		}
	}
	b.shellFacts(r, status, root, e, &f)
	body, err := view.Draw(e, f)
	if err != nil {
		logFault(r, "the view "+view.TemplateOf(e)+" cannot be drawn", err)
		status, body = http.StatusInternalServerError, []byte(view.DrawFailed)
	}
	write(w, r, status, body, retryAfter, true)
}

// appName is the name of this program as a connected app: `serve --with web` runs aminte-web. browserSettingsAction is
// the name that the core gives its action settings, for the core prefixes each name of an app (extensions/README.md, "A
// catalog of its own"). The home of the app is /with/web; its one form is drawn on /settings ("This browser"), so the
// rail does not list it as an app and its home leads to /settings (one form for one thing).
const (
	appName               = "web"
	browserSettingsAction = appName + "-settings"
	appHome               = "/with/" + appName
)

// offerBrowserSettings adds the form of this browser to the settings page, under the form of the core: the action
// web-settings of the catalog of the core, with the values that this program keeps. Without `serve --with web` the core
// has no such action, and the page is the page of the core.
func (b *Browser) offerBrowserSettings(r *http.Request, e *siren.Entity) {
	actions, err := b.core.catalog(r.Context())
	if err != nil {
		logFault(r, "the catalog of the core cannot be read", err)
		return
	}
	if i := slices.IndexFunc(actions, func(a siren.Action) bool { return a.Name == browserSettingsAction }); i >= 0 {
		e.Actions = append(e.Actions, withValues(actions[i], b.settings()))
	}
}

// homeOf is the page of Home, from the root of the API read with ?embed=today,conversation: the day of today. The
// root is the shell too. ok is false when the root has no embedded day of today.
func homeOf(root *siren.Entity) (_, home *siren.Entity, ok bool) {
	i := slices.IndexFunc(root.Entities, func(l siren.Link) bool { return l.HasRel("today") && l.Embedded() })
	if i < 0 {
		return nil, nil, false
	}
	return root, root.Entities[i].Entity(), true
}

// ownView is the address of the view that carries the page e, "" when e has none. A live metric and a live conversation
// page each have one view in a browser: the series of the metric and the conversation of the day. The page links to
// that view (API.md, "page, person, file"), and the view carries the page. The page of a deleted metric or conversation
// stays a page.
func ownView(e *siren.Entity) string {
	if slices.Contains(e.Class, "deleted") {
		return ""
	}
	for _, v := range []struct{ class, rel string }{{"metric", "series"}, {"page", "conversation"}} {
		if l := e.LinkByRel(v.rel); l != nil && slices.Contains(e.Class, v.class) && isPath(l.Href) {
			return l.Href
		}
	}
	return ""
}

// redirect sends the browser to another address of web. A Location that is not a path would send the browser to
// another host, so it is a fault of the core.
func (b *Browser) redirect(w http.ResponseWriter, r *http.Request, to string, status int) {
	if !isPath(to) {
		b.fail(w, r, http.StatusBadGateway, siren.CodeUpstream, "the core sent a redirect that is not a path")
		return
	}
	http.Redirect(w, r, to, status)
}

// stream sends the answer of the core to the browser as it is: the status, the headers that describe the content, and
// the body, which is copied and never held whole. A HEAD gets the headers alone.
func stream(w http.ResponseWriter, r *http.Request, res *http.Response) {
	for _, name := range relayed {
		if v := res.Header.Values(name); len(v) > 0 {
			w.Header()[http.CanonicalHeaderKey(name)] = v
		}
	}
	w.WriteHeader(res.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, res.Body); err != nil && !errors.Is(err, context.Canceled) {
		logFault(r, "the answer of the core was not sent", err)
	}
}

// relayed are the headers of a streamed answer that web passes to the browser: the ones that describe the content, how
// long a browser keeps it, what a conditional or a range request needs, and the nosniff and the Allow of the plain text of
// a route that does not exist or does not take the method.
var relayed = []string{"Content-Type", "Content-Length", "Content-Disposition", "Cache-Control", "ETag", "Last-Modified",
	"Accept-Ranges", "Content-Range", "Retry-After", "X-Content-Type-Options", "Allow"}

// write sends the page: its status, its body and its headers. A page of the shell asks the browser to prerender the
// links of the rules in speculation.json. A 503 of the core keeps the Retry-After that the core sent.
func write(w http.ResponseWriter, r *http.Request, status int, body []byte, retryAfter string, shell bool) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	if shell {
		h.Set("Speculation-Rules", `"/assets/speculation.json"`) // a quoted URL string
	}
	if status == http.StatusServiceUnavailable && retryAfter != "" {
		h.Set("Retry-After", retryAfter)
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(body); err != nil {
		logFault(r, "the page was not sent", err)
	}
}

// coreURI is the path and the query of u as a read of the core takes them: the query without the names that only web
// reads.
func coreURI(u *url.URL) string { return withQuery(u.EscapedPath(), coreQuery(u)) }

// coreQuery is the raw query of u without webFields: the receipt of the toast (feedback) and the page to return to (back).
// The core refuses a name that no action lists, and these two are names of web alone. new stays: the core takes it on a
// GET, to open a form. The other pairs stay as the browser sent them.
func coreQuery(u *url.URL) string {
	var kept []string
	for pair := range strings.SplitSeq(u.RawQuery, "&") {
		name, _, _ := strings.Cut(pair, "=")
		if name, err := url.QueryUnescape(name); pair != "" && (err != nil || !slices.Contains(webFields, name)) {
			kept = append(kept, pair)
		}
	}
	return strings.Join(kept, "&")
}

// withQuery is path with the raw query, when there is one.
func withQuery(path, rawQuery string) string {
	if rawQuery == "" {
		return path
	}
	if strings.Contains(path, "?") {
		return path + "&" + rawQuery
	}
	return path + "?" + rawQuery
}

// withEmbed is uri with the parameter embed.
func withEmbed(uri, rels string) string { return withQuery(uri, embedField+"="+rels) }

// logFault writes a fault of this program to the standard log: the route, what failed and the cause. The browser never
// gets the cause. A request that the browser cancelled is no fault.
func logFault(r *http.Request, what string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	log.Printf("%s: %s: %v", routeName(r), what, err)
}

// routeName names the route of r in the log: the pattern that the request matched, and not its path, because a path can name
// a person or a metric. A request that no route matched (the login, a refusal of the door) has its method.
func routeName(r *http.Request) string {
	if r.Pattern == "" {
		return r.Method
	}
	return r.Pattern
}
