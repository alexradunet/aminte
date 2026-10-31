package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lifelog/siren"
)

// The connection to the core. Web asks the core for Siren JSON and for bytes, and it draws the HTML itself. The
// package client is not used for a read: it holds the whole answer and decodes numbers as json.Number, and the view
// reads a number as a float64 and streams a picture or a file without holding it.

const (
	// maxEntity is the largest Siren answer that web reads. An answer is one entity, and a list is paged, so the
	// limit stops a wrong server from filling memory. It is the limit of package client.
	maxEntity = 16 << 20
	// headerWait is the longest wait for the headers of an answer from the core. The core ends a request at 2
	// minutes, so a slow answer is the 504 of the core and not a timeout on this side.
	headerWait  = 150 * time.Second
	dialTimeout = 10 * time.Second
	// entityWait is the longest wait for the body of a Siren answer once its headers came. An entity is small, so a
	// body that stalls is a core that hung; a stream of a file has no such bound.
	entityWait = 30 * time.Second
	// pageWriter is the writer of the reads for a page of the browser.
	pageWriter = "ui"
)

// core is the connection to one lifelog server. It is safe for concurrent use.
type core struct {
	base       string // scheme://host[:port], without a path
	token      string // sent as a bearer credential when it is not empty
	hc         *http.Client
	entityWait time.Duration // the longest wait for the body of a Siren answer (entityWait)
}

// newCore checks the address of the core and makes the connection. It dials nothing yet.
func newCore(rawURL, token string) (*core, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("the address of the core %q: use http://host:port", rawURL)
	}
	if strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("the token must be one line")
	}
	transport := &http.Transport{
		Proxy:                 nil, // no proxy of the environment: the requests carry the owner's token, and the core is on the loopback
		DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: headerWait,
		ExpectContinueTimeout: time.Second,
		MaxIdleConnsPerHost:   8,    // a page of pictures asks for several at once
		DisableCompression:    true, // the bytes of a picture or a file reach the browser as the core sent them
	}
	hc := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // a redirect is an answer: the browser follows it
	}}
	return &core{base: u.Scheme + "://" + u.Host, token: token, hc: hc, entityWait: entityWait}, nil
}

// get asks the core for uri (a path and a query) as writer, and returns the answer with its body not yet read. The
// caller closes the body. header holds the headers of the browser's request that make sense for a read of the core
// (conditional and range requests): nil for none.
func (c *core) get(ctx context.Context, uri, writer string, header http.Header) (*http.Response, error) {
	req, err := c.request(ctx, http.MethodGet, uri, writer, nil)
	if err != nil {
		return nil, err
	}
	for _, name := range forwarded {
		if v := header.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	return c.hc.Do(req)
}

// post sends a write of the browser to the core: the body as it is, with its type and its size (-1 when it is not
// known). A body that streams (a file) is never held whole. The core is asked once: a write that was sent may be done,
// and the browser decides what to do (the same rule as for a connected app, extensions/README.md).
func (c *core) post(ctx context.Context, uri, writer string, body io.Reader, size int64, contentType string) (*http.Response, error) {
	if size == 0 {
		body = http.NoBody // a zero length with a body means "unknown" to the client
	}
	req, err := c.request(ctx, http.MethodPost, uri, writer, body)
	if err != nil {
		return nil, err
	}
	if size >= 0 {
		req.ContentLength = size
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.hc.Do(req)
}

// request is a request to the core that asks for Siren JSON, as writer, with the bearer token.
func (c *core) request(ctx context.Context, method, uri, writer string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+uri, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", sirenType)
	req.Header.Set(siren.SourceHeader, writer)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// sirenType is the media type of the JSON of an entity.
const sirenType = "application/vnd.siren+json"

// forwarded are the headers of the browser's request that web passes to the core, so that a file or a picture answers
// a conditional or a range request as the core does.
var forwarded = []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"}

// isSiren reports an answer that holds a Siren entity.
func isSiren(res *http.Response) bool {
	return strings.HasPrefix(res.Header.Get("Content-Type"), sirenType)
}

// readBody reads the body of an answer of the core with the limit maxEntity. An answer that is larger is an error, never
// a half answer. A body that stalls is ended after entityWait.
func (c *core) readBody(res *http.Response) ([]byte, error) {
	stall := time.AfterFunc(c.entityWait, func() { res.Body.Close() }) // ends a Read that waits for the core
	defer stall.Stop()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxEntity+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxEntity {
		return nil, fmt.Errorf("the answer is larger than %d bytes", maxEntity)
	}
	return body, nil
}

// readEntity reads the Siren entity that res holds. Numbers are float64, as the view reads them. An answer that is not
// one entity is an error, never a half entity.
func (c *core) readEntity(res *http.Response) (*siren.Entity, error) {
	body, err := c.readBody(res)
	if err != nil {
		return nil, err
	}
	return entityOf(res, body)
}

// entityOf decodes body, the body of res, as one Siren entity.
func entityOf(res *http.Response, body []byte) (*siren.Entity, error) {
	e := &siren.Entity{}
	if err := json.Unmarshal(body, e); err != nil {
		return nil, fmt.Errorf("HTTP %d, not a Siren entity (%s)", res.StatusCode, res.Header.Get("Content-Type")) // never the body: it may be a picture or a page
	}
	return e, nil
}

// read asks the core for uri, which web built, and returns the Siren entity that a 2xx answer holds. It returns nil for
// any other answer: a failure, another status, an answer that is not an entity, or an entity with no class and no
// title.
func (c *core) read(ctx context.Context, uri, writer string) *siren.Entity {
	res, err := c.get(ctx, uri, writer, nil)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 || !isSiren(res) {
		return nil
	}
	e, err := c.readEntity(res)
	if err != nil || e.Title == "" && len(e.Class) == 0 {
		return nil
	}
	return e
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// isPath reports a Location that is a path of one server: it starts with one slash and has no backslash, so a browser
// does not read it as the address of another host ("//host" and "/\host").
func isPath(to string) bool {
	return strings.HasPrefix(to, "/") && !strings.HasPrefix(to, "//") && !strings.Contains(to, `\`) &&
		strings.IndexFunc(to, func(c rune) bool { return c < 0x20 || c == 0x7f }) < 0
}

// errAnswer marks a core that answered, but not with what web asked for.
var errAnswer = errors.New("the core gave an answer that web cannot use")

// catalog is the actions of the core, from GET /actions. The catalog is the one list of the fields of an action, and it
// holds the actions of the connected apps too.
func (c *core) catalog(ctx context.Context) ([]siren.Action, error) {
	res, err := c.get(ctx, "/actions", pageWriter, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !isSiren(res) {
		return nil, fmt.Errorf("GET /actions: HTTP %d: %w", res.StatusCode, errAnswer)
	}
	e, err := c.readEntity(res)
	if err != nil {
		return nil, fmt.Errorf("GET /actions: %v: %w", err, errAnswer)
	}
	return e.Actions, nil
}
