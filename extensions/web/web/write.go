package web

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"lifelog-web/view"
	"lifelog/siren"
)

// The write side of the browser. A form posts to web. Web sends the same write to the core, with the same path, as the
// writer ui. The core answers with the entity, and web sends the browser on to a page (post, then redirect), so a reload
// never posts again.
//
//	the form --> web --> the core --> an answer:
//	  2xx with a Location --> 303 to the page the form came from (back), or to the written thing, with the toast
//	  an error            --> the page of the error, in the shell, with the long text the form sent
//	  a redirect          --> the same redirect
//	  2xx with no Location, or bytes --> drawn, or streamed

// maxBack is the longest return path a browser form may send.
const maxBack = 2048

// backFragment is the id that a return path may end with: a browser never sends a fragment to a server, so it only
// names where the page opens (the row that a write came from).
var backFragment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// backField is the name of the page that a browser form asks to return to after its write. Only web reads it.
const backField = "back"

// webFields are the names that only web reads: the page that a form returns to and the receipt of the toast (feedback.go).
// The core takes them from no client, so no request to the core carries them.
var webFields = []string{backField, feedbackField}

// newField opens a form on a GET (?new=rename). The core takes it on a GET alone, so a write never sends it.
const newField = "new"

// formType is the media type of the form of a browser.
const formType = "application/x-www-form-urlencoded"

// backOf is the page a browser form asks to return to after its write (docs/plans/091-the-browser-for-daily-use.md): b
// when it is a path on this server (isPath), "" otherwise. The path must be a URL, and it is at most maxBack bytes. It may
// end with one id fragment (backFragment). Any other "#" makes the value not a path. A value that is not kept is ignored,
// not refused: the write is done, and the browser goes to the resource it changed.
func backOf(b string) string {
	path, fragment, hasFragment := strings.Cut(b, "#")
	if !isPath(b) || len(b) > maxBack || hasFragment && !backFragment.MatchString(fragment) {
		return ""
	}
	if _, err := url.Parse(path); err != nil {
		return ""
	}
	return b
}

// The time limits of a body that a browser sends. A server-wide ReadTimeout would be wrong here: it would cut an upload,
// which has no size limit as a whole. So the limits are per request. Web has no other deadline of its own: the wait for
// the core is bounded in core.go, and the core bounds its own work.
//
// They are variables so that a test can shorten them.
var (
	formWithin  = 2 * time.Minute  // the whole read of the form of a write or of /render
	smallWithin = 10 * time.Second // the whole read of the form of the login or of the lock: the body is a few bytes
	uploadStall = 2 * time.Minute  // the longest a file may send no byte
)

// readForm reads the urlencoded form of r: at most limit bytes, within d. When the form is read, the deadline is cleared,
// because a deadline that runs out later would cancel the request while the answer is made. When the read fails, the
// deadline stays: the server reads what is left of a body when it closes it, and that read must not wait for a sender
// that stopped. The limit has the ResponseWriter of the server, so the server closes the connection after it answers a
// body that is larger.
func readForm(w http.ResponseWriter, r *http.Request, limit int64, d time.Duration) error {
	rc := http.NewResponseController(w)
	if err := rc.SetReadDeadline(time.Now().Add(d)); err != nil && !errors.Is(err, http.ErrNotSupported) { // a recorder in a test is not a connection
		logFault(r, "the form has no read deadline", err)
	}
	r.Body = http.MaxBytesReader(serverWriter(w), r.Body, limit)
	err := r.ParseForm()
	if err == nil {
		rc.SetReadDeadline(time.Time{}) // supported or not, as above
	}
	return err
}

// serverWriter is the ResponseWriter of the server under the wrappers of web (noStore). http.MaxBytesReader tells the
// server to close the connection through a method that only that writer has, and it does not call Unwrap itself.
func serverWriter(w http.ResponseWriter) http.ResponseWriter {
	for {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w
		}
		w = u.Unwrap()
	}
}

// stallBody is the body of an upload. Each read moves the read deadline of the connection on by stall, so a sender that
// stops is cut after stall and a sender that goes on is not. At the end of the body the deadline is cleared. After any
// other error it stays: the client of the core closes the body, and the server then reads what is left of it, and that read
// must not wait for a sender that stopped.
type stallBody struct {
	io.ReadCloser
	rc      *http.ResponseController
	stall   time.Duration
	stopped atomic.Bool // the sender sent no byte for stall: the request to the core fails with "context canceled", and this says why
}

func (b *stallBody) Read(p []byte) (int, error) {
	b.rc.SetReadDeadline(time.Now().Add(b.stall)) // supported: outgoing set it once already
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.rc.SetReadDeadline(time.Time{})
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		b.stopped.Store(true)
	}
	return n, err
}

// outgoing is a write as web sends it to the core.
type outgoing struct {
	uri         string    // the path and the query, without the names that only web reads (webFields) and new
	body        io.Reader // the fields of a form, or the stream of a file
	size        int64     // the length of body, -1 when it is not known
	contentType string
	back        string     // the page to return to, checked by backOf; "" for none
	submitted   string     // the long text that the form sent, for the page of a refusal
	upload      *stallBody // the stream of a file; nil for a form of text
}

// write sends a POST of the browser to the core. A form of text (urlencoded) is read and bounded, because web takes out
// the names that only it reads (webFields), new and the fields of the other button of a pick bar. A form of a file
// (multipart) streams to the core unchanged and is never held, so web reads nothing of it: the page to return to is in
// the query of its address (the view puts it there).
func (b *Browser) write(w http.ResponseWriter, r *http.Request) {
	out, ok := b.outgoing(w, r)
	if !ok {
		return
	}
	res, err := b.core.post(r.Context(), out.uri, pageWriter, out.body, out.size, out.contentType)
	if err != nil {
		if out.upload != nil && out.upload.stopped.Load() { // not a fault of the core
			b.fail(w, r, http.StatusBadRequest, siren.CodeBadRequest, "the file stopped coming before its end")
			return
		}
		b.unreachable(w, r, err)
		return
	}
	defer res.Body.Close()
	switch {
	case isRedirect(res.StatusCode):
		b.redirect(w, r, res.Header.Get("Location"), res.StatusCode)
	case isSiren(res):
		b.answerWrite(w, r, res, out)
	default:
		stream(w, r, res)
	}
}

// outgoing makes the write for the core from the request, or answers a refusal and returns false.
func (b *Browser) outgoing(w http.ResponseWriter, r *http.Request) (out outgoing, ok bool) {
	query, err := url.ParseQuery(r.URL.RawQuery) // a pair that cannot be read is refused, not dropped
	if err != nil {
		b.fail(w, r, http.StatusBadRequest, siren.CodeBadRequest, "query: "+err.Error())
		return out, false
	}
	back := query.Get(backField)
	for _, name := range webFields {
		query.Del(name)
	}
	query.Del(newField)
	out.uri = withQuery(r.URL.EscapedPath(), query.Encode())

	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	emptyForm := ct == "" && r.ContentLength == 0 // a POST with no field: a browser sends the type, a program may not
	switch {
	case ct == "multipart/form-data":
		out.body, out.size, out.contentType, out.back = r.Body, r.ContentLength, r.Header.Get("Content-Type"), backOf(back)
		rc := http.NewResponseController(w)
		if err := rc.SetReadDeadline(time.Now().Add(uploadStall)); err == nil {
			out.upload = &stallBody{ReadCloser: r.Body, rc: rc, stall: uploadStall}
			out.body = out.upload
		} else if !errors.Is(err, http.ErrNotSupported) { // a recorder in a test is not a connection
			logFault(r, "the upload has no read deadline", err)
		}
		return out, true
	case ct == formType || emptyForm:
	default: // refused, not ignored: a browser form sends one of the two types
		b.fail(w, r, http.StatusUnsupportedMediaType, siren.CodeUnsupportedMedia, "send application/x-www-form-urlencoded or multipart/form-data")
		return out, false
	}

	if err := readForm(w, r, maxRequestBody, formWithin); err != nil {
		b.refuseBody(w, r, err)
		return out, false
	}
	form := r.PostForm
	out.back = backOf(cmp.Or(form.Get(backField), back))
	for _, name := range webFields {
		form.Del(name)
	}
	form.Del(newField)
	if len(form) > 0 { // the catalog says which fields the action takes, and which fields are long text
		actions, err := b.core.catalog(r.Context())
		switch {
		case errors.Is(err, errAnswer):
			logFault(r, "the catalog of the core cannot be read", err)
			b.fail(w, r, http.StatusBadGateway, siren.CodeUpstream, "the core gave an answer that web cannot use")
			return out, false
		case err != nil:
			b.unreachable(w, r, err)
			return out, false
		}
		out.submitted = longText(form, actions)
		if act, found := actionAt(actions, r.URL.Path); found {
			dropSharedFields(form, act, actions)
		}
	}
	encoded := form.Encode()
	out.body, out.size, out.contentType = strings.NewReader(encoded), int64(len(encoded)), formType
	return out, true
}

// refuseBody answers an error of the body of a form: a body larger than the limit is 413, any other is 400. The words are
// the words of the core.
func (b *Browser) refuseBody(w http.ResponseWriter, r *http.Request, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		b.fail(w, r, http.StatusRequestEntityTooLarge, siren.CodeTooLarge, "request body is larger than 20 MiB")
		return
	}
	b.fail(w, r, http.StatusBadRequest, siren.CodeBadRequest, "body: "+err.Error())
}

// answerWrite answers the browser with the Siren answer of the core to its write.
func (b *Browser) answerWrite(w http.ResponseWriter, r *http.Request, res *http.Response, out outgoing) {
	body, err := b.core.readBody(res)
	var e *siren.Entity
	if err == nil {
		e, err = entityOf(res, body)
	}
	if err != nil {
		logFault(r, "the answer of the core cannot be read", err)
		b.fail(w, r, http.StatusBadGateway, siren.CodeUpstream, "the core gave an answer that is not an entity")
		return
	}
	if location := res.Header.Get("Location"); res.StatusCode/100 == 2 && location != "" {
		if !isPath(location) {
			b.fail(w, r, http.StatusBadGateway, siren.CodeUpstream, "the core sent a redirect that is not a path")
			return
		}
		http.Redirect(w, r, b.feedback.put(cmp.Or(out.back, location), resultOf(body)), http.StatusSeeOther)
		return
	}
	f := view.Facts{URL: r.URL}
	if res.StatusCode >= http.StatusBadRequest {
		f.Submitted, f.Back = out.submitted, refererOf(r)
	}
	b.draw(w, r, res.StatusCode, e, res.Header.Get("Retry-After"), f)
}

// resultOf is what the write did, from the body of the answer: the raw JSON of the member result, for the toast. The raw
// text keeps the numbers as the core wrote them. nil when the answer has none.
func resultOf(body []byte) any {
	var m struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(body, &m) != nil || len(m.Result) == 0 || string(m.Result) == "null" {
		return nil
	}
	return m.Result
}

// longText is the long text that a form sent, for the page of a refusal to show it again: the value of the first
// long-text field of the catalog that has one. The names are sorted, so the first with a value does not depend on the
// order of the catalog.
func longText(form url.Values, actions []siren.Action) string {
	var names []string
	for _, a := range actions {
		for _, f := range a.Fields {
			if f.Type == "textarea" && !slices.Contains(names, f.Name) {
				names = append(names, f.Name)
			}
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if v := form.Get(name); v != "" {
			return v
		}
	}
	return ""
}

// actionAt is the POST action of the catalog whose href fits path, segment by segment: {x} fits one segment that is not
// empty, and any other segment fits itself.
func actionAt(actions []siren.Action, path string) (siren.Action, bool) {
	parts := strings.Split(path, "/")
	for _, a := range actions {
		want := strings.Split(a.Href, "/")
		if a.Method != http.MethodPost || len(want) != len(parts) {
			continue
		}
		fits := true
		for i, w := range want {
			if len(w) > 2 && w[0] == '{' && w[len(w)-1] == '}' {
				fits = fits && parts[i] != ""
			} else {
				fits = fits && w == parts[i]
			}
		}
		if fits {
			return a, true
		}
	}
	return siren.Action{}, false
}

// dropSharedFields takes out of form the fields of the shared form that act does not list. The pick bar of a list posts
// the form of file-pages to tombstone-pages by a second button, because HTML cannot join one checkbox to two forms
// (view.SharedForms). The core takes the fields of one action from every client, so web sends the action the fields that
// it lists. A field that neither form lists stays, and the core refuses it.
func dropSharedFields(form url.Values, act siren.Action, actions []siren.Action) {
	shared, ok := view.SharedForms[act.Name]
	if !ok {
		return
	}
	i := slices.IndexFunc(actions, func(a siren.Action) bool { return a.Name == shared })
	if i < 0 {
		return
	}
	for _, f := range actions[i].Fields {
		if f.In != "path" && act.Field(f.Name) == nil {
			form.Del(f.Name)
		}
	}
}
