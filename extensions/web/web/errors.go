package web

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"

	"lifelog-web/view"
	"lifelog/siren"
)

// retryAfter is the Retry-After of a 503, in seconds. It is the wait of the core (internal/api, retryAfter).
const retryAfter = "1"

// fail answers with the page of an error that web found itself: the core did not answer, or answered what is not a
// core's answer, or a guard of the door refused the request. The page has no shell, because the shell is read from the
// core.
func (b *Browser) fail(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	e := &siren.Entity{Class: []string{"error"}, Title: "Error",
		Links:      []siren.Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}},
		Properties: map[string]any{"status": status, "code": code, "message": msg}}
	b.bare(w, r, status, e)
}

// bare answers with the page of e with no shell: the page of an error that web found itself, the login and the lock. A
// refusal of a form has a link back to the page that the form came from (backOf).
func (b *Browser) bare(w http.ResponseWriter, r *http.Request, status int, e *siren.Entity) {
	if wantsSiren(r) { // a script of the page reads the error itself: the poll of chat.js reads the code locked
		w.Header().Set("Content-Type", sirenType)
		w.WriteHeader(status)
		if err := siren.WriteJSON(w, e, ""); err != nil {
			logFault(r, "the error was not sent", err)
		}
		return
	}
	f := view.Facts{URL: r.URL}
	if status >= http.StatusBadRequest {
		f.Back = refererOf(r)
	}
	body, err := view.Draw(e, f)
	if err != nil {
		logFault(r, "the page cannot be drawn", err)
		status, body = http.StatusInternalServerError, []byte(view.DrawFailed)
	}
	var wait string
	if status == http.StatusServiceUnavailable {
		wait = retryAfter
	}
	write(w, r, status, body, wait, false)
}

// refererOf is the page that a refused form came from, when it is a page of this server: the Referer of a POST that has
// the host of the request. "" for any other request, and for a Referer of another host.
func refererOf(r *http.Request) string {
	if r.Method != http.MethodPost {
		return ""
	}
	if ref, err := url.Parse(r.Referer()); err == nil && ref.Host == r.Host && strings.HasPrefix(ref.Path, "/") {
		return ref.RequestURI()
	}
	return ""
}

// unreachable answers when a read of the core did not give an answer. A browser that went away gets nothing. A core that
// is too slow is 504, and a core that is not there is 503 unavailable, the answer of the core for an app that does not
// run. The cause is in the log and not in the page: it can name the address of the core.
func (b *Browser) unreachable(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil && !errors.Is(r.Context().Err(), context.DeadlineExceeded) {
		return
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err // the address holds the path of the request, which can name a person or a metric
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
		logFault(r, "the core is too slow", err)
		b.fail(w, r, http.StatusGatewayTimeout, siren.CodeTimeout, "lifelog did not answer in time")
		return
	}
	logFault(r, "the core cannot be reached", err)
	b.fail(w, r, http.StatusServiceUnavailable, siren.CodeUnavailable, "lifelog is not running")
}

// recovered ends the request of a handler that panicked. Defer it at the top of the handler: recover sees a panic only
// when a deferred function calls it. Without it the server would close the connection, and the browser would see a
// broken connection and no page. The log gets one record in both cases: the route, the value of the panic and the stack.
// The route is the pattern, never the path, because a path can name a person or a metric.
//
//   - The answer has not started: the browser gets the page of an internal error, the same words for every fault. The
//     page is never cached, whatever header the handler had set.
//   - The answer has started: the status is sent, and a page after the first part of a body would look like a whole
//     answer. So recovered panics with http.ErrAbortHandler. The server then closes the connection, logs nothing more,
//     and the browser sees an answer that ends too early.
func (b *Browser) recovered(w *startWriter, r *http.Request) {
	v := recover()
	if v == nil {
		return
	}
	if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
		panic(v) // the handler cut the answer on purpose: the server ends the connection and logs nothing
	}
	log.Printf("%s: panic: %v\n%s", routeName(r), v, debug.Stack())
	if w.started {
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Cache-Control", "no-store") // the handler may have set a cache for the answer that it did not make
	b.fail(w, r, http.StatusInternalServerError, siren.CodeInternal, "internal error")
}

// startWriter records that the answer has started: the handler sent its status, wrote a byte or flushed. From then on
// the status cannot change (recovered). It sits at the top of ServeHTTP. Unwrap lets http.ResponseController and
// serverWriter reach the writer of the server.
type startWriter struct {
	http.ResponseWriter
	started bool
}

func (s *startWriter) WriteHeader(code int) {
	s.started = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *startWriter) Write(b []byte) (int, error) {
	s.started = true
	return s.ResponseWriter.Write(b)
}

// FlushError is the flush that http.ResponseController calls first. A flush sends the status 200 when the handler has sent
// none, so it starts the answer too.
func (s *startWriter) FlushError() error {
	s.started = true
	return http.NewResponseController(s.ResponseWriter).Flush()
}

func (s *startWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
