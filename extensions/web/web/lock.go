package web

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lifelog/siren"
)

// The lock (docs/plans/160-the-lock.md, moved from the core by plan 161). A session that is idle for longer than
// lock_after, or that the owner locked, gets the lock page and nothing of the life log, until the PIN opens it. The
// rules, with their reasons, are the rules of plan 160 and of its counter-review: a try is counted before the PIN is
// compared; a lock keeps the count of wrong PINs; an ended session compares no PIN and gets no state again; every answer
// of a session but /assets/ is no-store; the lock page keeps the path of the page and never its query.
//
// The settings are in memory (settings.go), so there is no read that can fail while the program runs. A file that
// cannot be read at the start stops the program (New).

// countsAsActivity reports a request that the owner made, and not the page: a navigation that is not a prefetch or a
// prerender (no Sec-Purpose header), and a form post. The poll of the chat script and a speculative load are not.
// A browser that sends no Sec-Fetch-Mode header (an old one) counts for a GET that asks for HTML.
func countsAsActivity(r *http.Request) bool {
	if r.Header.Get("Sec-Purpose") != "" {
		return false
	}
	mode := r.Header.Get("Sec-Fetch-Mode")
	switch r.Method {
	case http.MethodGet:
		return mode == "navigate" || (mode == "" && wantsHTML(r))
	case http.MethodPost:
		return mode == "navigate"
	}
	return false
}

// localPath is next when it is a path of this origin, else "". It must start with one "/" and no "\": a browser reads
// "//host" and "/\host" as another origin. A control character is refused too: a browser drops a tab or a line
// break from a URL, which could turn "/<tab>/host" into "//host".
func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		return ""
	}
	for _, c := range next {
		if c < 0x20 || c == 0x7f || c == '\\' {
			return ""
		}
	}
	return next
}

// serveSession serves a request of a browser with a session cookie. The caller has checked the cookie.
func (b *Browser) serveSession(w http.ResponseWriter, r *http.Request, id string) {
	now := b.now()
	s := b.settings()
	lockOn := s.lockMinutes() > 0
	idle := time.Duration(s.lockMinutes())*time.Minute + idleSlack
	locked := b.sessions.locked(id, now, lockOn, idle)
	if !strings.HasPrefix(r.URL.Path, "/assets/") { // the assets are the same for every browser: they keep their cache
		w = &noStore{ResponseWriter: w}
	}
	switch {
	case r.URL.Path == "/lock" && r.Method == http.MethodPost:
		b.lockNow(w, r, id, now, lockOn)
		return
	case r.URL.Path == "/locked" && r.Method == http.MethodGet:
		b.lockedPage(w, r, locked)
		return
	case r.URL.Path == "/unlock" && r.Method == http.MethodPost:
		b.unlock(w, r, id, now, s.LockPINHash, lockOn)
		return
	case r.URL.Path == "/awake" && r.Method == http.MethodPost:
		b.awake(w, r, id, now, locked)
		return
	}
	if locked {
		if r.Method == http.MethodGet && wantsHTML(r) {
			// next is the path alone: a query, such as the words of a search, is the owner's and would show on the lock page.
			http.Redirect(w, r, "/locked?next="+url.QueryEscape(r.URL.EscapedPath()), http.StatusSeeOther)
			return
		}
		b.failLocked(w, r)
		return
	}
	if countsAsActivity(r) {
		b.sessions.activity(id, now)
	}
	b.mux.ServeHTTP(w, r)
}

func (b *Browser) failLocked(w http.ResponseWriter, r *http.Request) {
	b.fail(w, r, http.StatusUnauthorized, siren.CodeLocked, "locked: unlock this browser with the PIN at /locked")
}

// lockNow is POST /lock: the owner locks this browser. With the lock off it is 409: there is no PIN to open it with.
func (b *Browser) lockNow(w http.ResponseWriter, r *http.Request, id string, now time.Time, lockOn bool) {
	if !lockOn {
		b.fail(w, r, http.StatusConflict, siren.CodeConflict, "the lock is off: set lock_after and a PIN in Settings, under This browser")
		return
	}
	b.sessions.lock(id, now)
	http.Redirect(w, r, "/locked", http.StatusSeeOther)
}

// lockedPage is GET /locked: the page with the PIN field and nothing of the life log. A session that is not locked goes
// on to next.
func (b *Browser) lockedPage(w http.ResponseWriter, r *http.Request, locked bool) {
	next := localPath(r.URL.Query().Get("next"))
	if !locked {
		if next == "" {
			next = "/"
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	e := &siren.Entity{Class: []string{"locked"}, Title: "Locked",
		Properties: map[string]any{"failed": r.URL.Query().Get("failed") == "1", "next": next}}
	b.bare(w, r, http.StatusOK, e)
}

// unlock is POST /unlock. The right PIN opens the session and goes to next. A wrong one goes back to the lock page; the
// fifth ends the session. The log gets one line for each wrong PIN and never the PIN.
func (b *Browser) unlock(w http.ResponseWriter, r *http.Request, id string, now time.Time, hash string, lockOn bool) {
	parseErr := readForm(w, r, 4096, smallWithin)
	next := localPath(r.PostForm.Get("next"))
	open := next // where the right PIN goes
	if open == "" {
		open = "/"
	}
	// The session can end while this request waits for its body (another request, a logout): each step below asks
	// the memory of the sessions again, under its mutex.
	ended := func() {
		b.sessions.end(id, now)
		setCookie(w, "", -1)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
	// With the lock off there is nothing to open: the session goes on, and no try is counted.
	if !lockOn {
		if !b.sessions.opened(id, now) {
			ended()
			return
		}
		http.Redirect(w, r, open, http.StatusSeeOther)
		return
	}
	if !b.sessions.tryPIN(id, now) {
		log.Printf("lifelog-web: %s %s: no tries left, the session ends", r.Method, r.URL.Path)
		ended()
		return
	}
	if hash != "" && parseErr == nil && pinMatches(hash, r.PostForm.Get("pin")) {
		if !b.sessions.opened(id, now) {
			ended()
			return
		}
		http.Redirect(w, r, open, http.StatusSeeOther)
		return
	}
	n, done := b.sessions.failed(id, now)
	log.Printf("lifelog-web: %s %s: the PIN does not match (%d of %d)", r.Method, r.URL.Path, n, wrongPINLimit)
	if done {
		ended()
		return
	}
	to := "/locked?failed=1"
	if next != "" {
		to += "&next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// awake is POST /awake: the page script says that the owner typed, clicked or scrolled. A locked session gets 401
// locked, so the script can show the lock page.
func (b *Browser) awake(w http.ResponseWriter, r *http.Request, id string, now time.Time, locked bool) {
	if locked {
		b.failLocked(w, r)
		return
	}
	b.sessions.activity(id, now)
	w.WriteHeader(http.StatusNoContent)
}

// noStore makes every answer of a session uncacheable, whatever its handler set: a picture that its route keeps for a
// year would show from the cache in a locked browser. The header is set at the first WriteHeader or Write.
type noStore struct {
	http.ResponseWriter
	wrote bool
}

func (n *noStore) WriteHeader(code int) {
	if !n.wrote {
		n.wrote = true
		n.Header().Set("Cache-Control", "no-store")
	}
	n.ResponseWriter.WriteHeader(code)
}

func (n *noStore) Write(b []byte) (int, error) {
	if !n.wrote {
		n.WriteHeader(http.StatusOK)
	}
	return n.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the connection.
func (n *noStore) Unwrap() http.ResponseWriter { return n.ResponseWriter }
