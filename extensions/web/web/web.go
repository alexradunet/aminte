// Package web is the browser of lifelog: an HTTP handler that draws the answers of the core as pages of HTML. It is a
// client of the API. It asks the core for Siren JSON with a bearer token and with the writer ui, and it draws the entity
// with the view, in the shell. It opens no file and holds no state of the data. A picture or a file that the core serves
// streams through it unchanged.
//
// The handler answers GET and HEAD, and the one POST that writes nothing, /render. The routes that write (the
// forms), the login, the lock and the settings of the browser are the next part of the extension. Their place
// is marked in this file: Options, the guard and the settings.
package web

import (
	"net/http"

	"lifelog/siren"
)

// Options are what a Browser needs to start.
type Options struct {
	// CoreURL is the address of the core, for example http://127.0.0.1:7777.
	CoreURL string
	// Token is the bearer token of the core. Empty means that the core checks none.
	Token string
}

// Browser is the handler of the browser. Its fields are the whole of its state: the connection to the core, the guard
// of the browser's writes and the routes.
type Browser struct {
	core  *core
	guard *http.CrossOriginProtection // the guard of the writes of a browser; the next part adds the allowed origins to it
	mux   *http.ServeMux
}

// New makes the handler. It dials nothing: the first request does.
func New(o Options) (*Browser, error) {
	c, err := newCore(o.CoreURL, o.Token)
	if err != nil {
		return nil, err
	}
	b := &Browser{core: c, guard: http.NewCrossOriginProtection(), mux: http.NewServeMux()}
	b.routes()
	return b, nil
}

// ServeHTTP makes Browser an http.Handler.
func (b *Browser) ServeHTTP(w http.ResponseWriter, r *http.Request) { b.mux.ServeHTTP(w, r) }

// routes registers the routes that web serves itself, then the page, which asks the core.
func (b *Browser) routes() {
	b.ownRoutes(b.mux)
	b.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			b.page(w, r)
		default: // refused, not ignored
			w.Header().Set("Allow", "GET, HEAD")
			b.fail(w, r, http.StatusMethodNotAllowed, siren.CodeBadRequest, "this address takes GET and HEAD")
		}
	})
}
