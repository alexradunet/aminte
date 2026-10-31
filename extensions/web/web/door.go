package web

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"lifelog/siren"
)

// The door of the browser. A browser logs in once with the owner's token and gets the session cookie (sessions.go). Every
// other request needs that cookie, and a session that the lock holds shut gets the lock page (lock.go). This program
// takes no bearer header: a program uses the API of the core.
//
// /login, /logout, /lock, /unlock, /locked and /awake are about one browser, not about the data, so they are not actions
// of the catalog.

// installFiles are the files that a browser fetches without its cookie to install the app. The wood tile is one: it is an
// image of the sheet, fetched like an icon. They are embedded and static, and they hold nothing of the life log. A GET or
// a HEAD of them needs no cookie. The fonts are not here: the page fetches them with its cookie.
var installFiles = map[string]bool{
	"/manifest.webmanifest": true,
	"/assets/icon.svg":      true,
	"/assets/icon-192.png":  true,
	"/assets/icon-512.png":  true,
	"/assets/wood.png":      true,
	"/assets/ground.png":    true,
	"/assets/embers.png":    true,
	"/assets/sprites.png":   true,
	"/assets/icons.png":     true,
}

// checkOrigin admits an exact browser origin: a scheme, a host, an optional port and nothing else.
func checkOrigin(origin string) error {
	// A browser sends its origin in lower case, and the guard compares the text as it is: an origin with an upper-case
	// letter in its scheme or its host would never match, so it is refused first, with the reason.
	if origin != strings.ToLower(origin) {
		return fmt.Errorf("origin %q: a browser sends its origin in lower case, so write the scheme and the host in lower case", origin)
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || origin != u.Scheme+"://"+u.Host {
		return fmt.Errorf("origin %q: use scheme://host[:port] exactly", origin)
	}
	return nil
}

// absoluteForm reports a request target in absolute form (GET http://host/path). Go then replaces Host with the authority
// of the target. No proxy deployment is supported, so web refuses it instead of trusting it.
func absoluteForm(r *http.Request) bool {
	return r.URL == nil || r.URL.IsAbs() || r.URL.Host != ""
}

// ServeHTTP makes Browser an http.Handler. It is the door: the checks that every request passes, in this order.
//
//  0. A panic of any step below ends in the page of an internal error, or in a cut answer when the answer had started
//     (recovered).
//  1. An absolute-form target is refused.
//  2. The guard refuses a write from an origin that it does not trust, before anything else is done for the request.
//  3. The login, the logout and the install files need no session.
//  4. Any other request needs a valid session cookie: a browser gets /login, any other client gets 401.
//  5. A session goes on to serveSession, which holds the lock.
func (b *Browser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := &startWriter{ResponseWriter: w}
	w = started
	defer b.recovered(started, r)
	if absoluteForm(r) {
		b.fail(w, r, http.StatusForbidden, siren.CodeForbidden, "HTTP authority refused")
		return
	}
	if err := b.guard.Check(r); err != nil {
		b.fail(w, r, http.StatusForbidden, siren.CodeCrossOrigin, "cross-origin browser write refused")
		return
	}
	switch {
	case r.URL.Path == "/login" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		b.login(w, r)
	case r.URL.Path == "/logout" && r.Method == http.MethodPost:
		b.logout(w, r)
	case installFiles[b.routeOf(r)] && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		b.mux.ServeHTTP(w, r)
	default:
		id, ok := b.sessions.idOf(r)
		switch {
		case ok:
			b.serveSession(w, r, id)
		case r.Method == http.MethodGet && wantsHTML(r):
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		default:
			b.fail(w, r, http.StatusUnauthorized, siren.CodeTokenRequired, "token required")
		}
	}
}

// routeOf is the path of the pattern of the route that answers r: "/assets/icon.svg" for GET /assets/icon.svg, "/" for a
// page of the core. The door decides by it, and not by r.URL.Path, because the routes match the escaped path:
// /assets%2Ficon.svg decodes to the path of the icon, but it is a page of the core to the routes.
func (b *Browser) routeOf(r *http.Request) string {
	_, pattern := b.mux.Handler(r)
	if _, path, ok := strings.Cut(pattern, " "); ok { // "GET /assets/icon.svg"
		return path
	}
	return pattern
}

// wantsHTML reports a request of a browser for a page: it accepts HTML and does not ask for JSON.
func wantsHTML(r *http.Request) bool {
	a := r.Header.Get("Accept")
	return strings.Contains(a, "text/html") && !strings.Contains(a, "json")
}

// wantsSiren reports a request of a script of the page that reads an entity: it asks for Siren JSON and not for HTML.
// Web answers it with the entity, not with a page.
func wantsSiren(r *http.Request) bool {
	a := r.Header.Get("Accept")
	return strings.Contains(a, sirenType) && !strings.Contains(a, "text/html")
}

// matches reports whether credential is the owner's token. The compare takes the same time for every value.
func (b *Browser) matches(credential string) bool {
	return subtle.ConstantTimeCompare([]byte(credential), b.token) == 1
}

// login is the browser's way in: GET shows the form, POST with the token sets the cookie. The page is the only one served
// without a credential, and it shows nothing of the life log.
func (b *Browser) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_, loggedIn := b.sessions.idOf(r)
		e := &siren.Entity{Class: []string{"login"}, Title: "Log in",
			Properties: map[string]any{"failed": r.URL.Query().Get("failed") == "1", "logged_in": loggedIn}}
		b.bare(w, r, http.StatusOK, e)
		return
	}
	if err := readForm(w, r, 4096, smallWithin); err != nil || !b.matches(r.PostForm.Get("token")) {
		// One line for the owner, who reads the log: a guess shows there. Never the token that was sent, or the body.
		log.Printf("%s %s: the token does not match", r.Method, r.URL.Path)
		http.Redirect(w, r, "/login?failed=1", http.StatusSeeOther)
		return
	}
	value, id, err := b.sessions.issue()
	if err != nil {
		logFault(r, "make a session", err)
		b.fail(w, r, http.StatusInternalServerError, siren.CodeInternal, "internal error")
		return
	}
	b.sessions.open(id, b.now())
	setCookie(w, value, loginMaxAge)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout ends the session of the request, if it has one, and clears the cookie.
func (b *Browser) logout(w http.ResponseWriter, r *http.Request) {
	if id, ok := b.sessions.idOf(r); ok {
		b.sessions.end(id, b.now())
	}
	setCookie(w, "", -1)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
