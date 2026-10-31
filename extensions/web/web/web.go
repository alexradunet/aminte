// Package web is the browser of lifelog: an HTTP handler that draws the answers of the core as pages of HTML. It is a
// client of the API. It asks the core for Siren JSON with a bearer token and with the writer ui, and it draws the entity
// with the view, in the shell. It opens no file of the life log and holds no state of the data. A picture or a file
// that the core serves streams through it unchanged.
//
// It is also the door of the browser. A browser logs in with the owner's token and gets a session cookie. Each page and
// each form is behind that cookie, and behind the lock (door.go, lock.go). A form posts to web, and web sends the same
// write to the core and sends the browser to the page it came from (write.go). The choices of the browser, the ground
// and the lock, are its own settings, in a file of its own state folder (settings.go), and it serves them as a
// catalog of its own (catalog.go).
package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"lifelog/siren"
)

// Options are what a Browser needs to start.
type Options struct {
	// CoreURL is the address of the core, for example http://127.0.0.1:7777.
	CoreURL string
	// Token is the owner's token. It is the login of the browser and the bearer token of every request to the core, so it
	// is the token of the core. At least 32 characters, one line.
	Token string
	// Origins are the browser origins (scheme://host[:port]) that the guard of the writes trusts besides its own: the
	// address that the proxy of the owner shows to the browser.
	Origins []string
	// StateDir is the state folder: settings.json is in it. It is made when it is missing.
	StateDir string
	// Now is the clock of the sessions and of the receipts of the toast. Nil is time.Now.
	Now func() time.Time
}

// Browser is the handler of the browser. Its fields are the whole of its state: the connection to the core, the guard
// of the browser's writes, the sessions, the settings and the receipts of the toast.
type Browser struct {
	core     *core
	guard    *http.CrossOriginProtection // refuses a write from an origin that it does not trust (door.go)
	mux      *http.ServeMux
	now      func() time.Time
	token    []byte
	sessions *sessions
	store    *settingsStore
	feedback feedbackStore
}

// New makes the handler. It reads the settings file and dials nothing: the first request does. Every error is an error of
// the configuration, which another start would repeat.
func New(o Options) (*Browser, error) {
	if err := checkToken(o.Token); err != nil {
		return nil, err
	}
	c, err := newCore(o.CoreURL, o.Token)
	if err != nil {
		return nil, err
	}
	guard := http.NewCrossOriginProtection()
	for _, origin := range o.Origins {
		if err := checkOrigin(origin); err != nil {
			return nil, err
		}
		if err := guard.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
	}
	store, err := openSettings(o.StateDir)
	if err != nil {
		return nil, err
	}
	b := &Browser{core: c, guard: guard, mux: http.NewServeMux(), now: o.Now, token: []byte(o.Token), sessions: newSessions([]byte(o.Token)), store: store}
	if b.now == nil {
		b.now = time.Now
	}
	b.feedback.now = b.now
	b.routes()
	return b, nil
}

// tokenMinLength is the shortest token that web accepts. The core demands the same length of its own token, so one token
// is the login of the browser and the bearer token of the core.
const tokenMinLength = 32

// checkToken reports whether t may be the owner's token: one line of at least tokenMinLength characters.
func checkToken(t string) error {
	if len(t) < tokenMinLength {
		return fmt.Errorf("the token must be at least %d characters", tokenMinLength)
	}
	if strings.ContainsAny(t, "\r\n") {
		return errors.New("the token must be one line")
	}
	return nil
}

// settings are the choices of the owner for this browser. It is the one place where they are read.
func (b *Browser) settings() browserSettings { return b.store.get() }

// routes registers the routes that web serves itself, then the catch-all: a GET or HEAD asks the core for a page, and a
// POST sends a write to the core. The login and the lock are routes of the door (door.go), before these.
func (b *Browser) routes() {
	b.ownRoutes(b.mux)
	b.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			b.page(w, r)
		case http.MethodPost:
			b.write(w, r)
		default: // refused, not ignored
			w.Header().Set("Allow", "GET, HEAD, POST")
			b.fail(w, r, http.StatusMethodNotAllowed, siren.CodeBadRequest, "this address takes GET, HEAD and POST")
		}
	})
}
