package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"

	"lifelog-web/view"
	"lifelog/siren"
)

// retryAfter is the Retry-After of a 503, in seconds. It is the wait of the core (internal/api, retryAfter).
const retryAfter = "1"

// fail answers with the page of an error that web found itself: the core did not answer, or answered what is not a
// core's answer. The page has no shell, because the shell is read from the core.
func (b *Browser) fail(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	e := &siren.Entity{Class: []string{"error"}, Title: "Error",
		Links:      []siren.Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}},
		Properties: map[string]any{"status": status, "code": code, "message": msg}}
	body, err := view.Draw(e, view.Facts{URL: r.URL})
	if err != nil {
		logFault(r, "the error page cannot be drawn", err)
		status, body = http.StatusInternalServerError, []byte(view.DrawFailed)
	}
	var wait string
	if status == http.StatusServiceUnavailable {
		wait = retryAfter
	}
	write(w, r, status, body, wait, false)
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
