package api

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"aminte/internal/core"
	"aminte/siren"
)

// The parsers of a request: its fields, its ids, its numbers and flags, and the page of a list. Every handler reads
// its input through them, so one refusal has one message everywhere.

const maxRequestBody = 20 << 20

func requestBodyError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return requestLimit("request body is larger than 20 MiB")
	}
	return &core.Error{Status: http.StatusBadRequest, Msg: "body: " + err.Error()}
}

func requestLimit(msg string) error {
	return &core.Error{Status: http.StatusRequestEntityTooLarge, Msg: msg}
}

// isMultipart says whether the body of a request is a multipart form: an upload, with limits of its own.
func isMultipart(r *http.Request) bool {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return ct == "multipart/form-data"
}

// limitBody gives the body of a request that is not a multipart form the limit maxRequestBody. The limit has the
// ResponseWriter of the server, so the server closes the connection after it answers a body that is larger, and
// reads no other request from it. A multipart form has the limits of parseFileForm and no limit as a whole, because
// an original has none.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMultipart(r) && r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

// form reads an action's fields: urlencoded (what an HTML form and the CLI send) or a JSON object. The body has the limit
// of limitBody.
func form(r *http.Request) (url.Values, error) {
	defer r.Body.Close()
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "application/json" {
		var m map[string]any
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&m); err != nil {
			return nil, requestBodyError(err)
		}
		if m == nil {
			return nil, requestBodyError(errors.New("expected a JSON object"))
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				err = errors.New("expected one JSON object")
			}
			return nil, requestBodyError(err)
		}
		v := url.Values{}
		for k, x := range m {
			switch x := x.(type) {
			case string:
				v.Set(k, x)
			case nil:
			default:
				b, _ := json.Marshal(x)
				v.Set(k, string(b))
			}
		}
		return v, nil
	}
	if ct == "multipart/form-data" {
		return nil, multipartRefusal()
	}
	if err := r.ParseForm(); err != nil {
		return nil, requestBodyError(err)
	}
	return r.Form, nil
}

// multipartRefusal is the 415 for a multipart body sent to an action that has no file field. Nothing reads the parts, so
// every field would be empty and the sender would not know. Only the file forms (parseFileForm) take such a body.
func multipartRefusal() error {
	return &core.Error{Status: http.StatusUnsupportedMediaType, Msg: "this action does not take multipart/form-data: send application/x-www-form-urlencoded or JSON"}
}

// fieldsOf reads the fields of the catalog action act: the query of a GET, the form of a POST. It refuses a request
// that breaks the shape the catalog gives the action (checked), and a multipart body (415) whatever the method: a GET
// reads its query alone, so a body that it would ignore is refused too.
func fieldsOf(r *http.Request, act spec) (url.Values, error) {
	if act.Method == "GET" {
		if isMultipart(r) {
			return nil, multipartRefusal()
		}
		v := r.URL.Query()
		return v, checked(v, act, r)
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	return v, checked(v, act, r)
}

// newField is the one name that a GET may carry beside the fields of its action: it names the action whose form a link
// asks for (known). A POST that carries it is refused as an unknown field.
const newField = "new"

// checked refuses values that break the shape of the action act: a required field left empty (required), a name the
// action does not list (known), and a value outside the options of a field (options). The shape of a request is the
// API's rule, not the data's, so it has one home for every surface.
func checked(v url.Values, act spec, r *http.Request) error {
	if err := required(v, act); err != nil {
		return err
	}
	if err := known(v, act, r); err != nil {
		return err
	}
	return options(v, act)
}

// fieldNames are the names of the fields of the action s that a request sends: a path field is in the route.
func fieldNames(s spec) []string {
	var names []string
	for _, f := range s.Fields {
		if f.In != "path" {
			names = append(names, f.Name)
		}
	}
	return names
}

// unknownField is the refusal of a name that the action does not take.
func unknownField(name string, s spec) error {
	return core.Invalid("unknown field %s: %s takes %s", name, s.Name, strings.Join(fieldNames(s), ", "))
}

// known refuses a name that is not a field of the action. A GET may carry embed, which serve reads (embed.go), and
// new, which names an action whose form a link of a UI asks for; the GET may then carry the fields of that action too.
// A HEAD is a GET without a body, so it takes the same names and gets the same status.
// A new that names no action is refused. Every other name, back and feedback too, is refused on every method.
func known(v url.Values, act spec, r *http.Request) error {
	allowed := fieldNames(act)
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		allowed = append(allowed, newField, embedField)
		if asked := v.Get(newField); asked != "" {
			s, ok := lookup(asked)
			if !ok {
				return core.Invalid("unknown form %s: new names an action of GET /actions", asked)
			}
			allowed = append(allowed, fieldNames(s)...)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(v)) {
		if !slices.Contains(allowed, name) {
			return unknownField(name, act)
		}
	}
	return nil
}

// options refuses a non-empty value outside the static options of a field. The options of a field that the catalog
// leaves empty are known at run time (withOptions), and the handler checks them.
func options(v url.Values, s spec) error {
	for _, f := range s.Fields {
		if x := v.Get(f.Name); f.In != "path" && x != "" && len(f.Options) > 0 && !slices.Contains(f.Options, x) {
			return core.Invalid("%s must be one of %s", f.Name, strings.Join(f.Options, ", "))
		}
	}
	return nil
}

// required refuses values that leave a required field of the action s empty. The catalog is the one list of the
// fields an action requires. A path field is not in the values: the route gives it.
func required(v url.Values, s spec) error {
	for _, f := range s.Fields {
		if f.Required && f.In != "path" && v.Get(f.Name) == "" {
			return core.Invalid("missing field %s", f.Name)
		}
	}
	return nil
}

// idOf is the {id} of the path; a value that is not an id is a 404, like an id no row has.
func idOf(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, core.NotFound("no such id %s", r.PathValue("id"))
	}
	return id, nil
}

// idParam is the path parameter of an action on the resource with this id.
func idParam(id int64) map[string]string { return map[string]string{"id": strconv.FormatInt(id, 10)} }

// decimal is a number in decimal: a sign, digits with at most one point, an exponent. The float syntax of Go also
// takes hexadecimal numbers (0x1p-2), underscores (1_000), NaN and Inf. A client never means these, so they are refused.
var decimal = regexp.MustCompile(`^[+-]?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?$`)

// number is a field's value as a decimal number, nil when the field is empty.
func number(v url.Values, name string) (*float64, error) {
	s := v.Get(name)
	if s == "" {
		return nil, nil
	}
	f, err := parseDecimal(s)
	if err != nil {
		return nil, core.Invalid("%s %q is not a number", name, s)
	}
	return &f, nil
}

// clock is a field's value as a time of the day, HH:MM in 24 hours, "" when the field is empty. The value must be the
// text that Go prints for it, because time.Parse alone also reads 7:05.
func clock(v url.Values, name string) (string, error) {
	s := v.Get(name)
	if s == "" {
		return "", nil
	}
	if t, err := time.Parse("15:04", s); err != nil || t.Format("15:04") != s {
		return "", core.Invalid("%s %q is not a time HH:MM", name, s)
	}
	return s, nil
}

// parseDecimal reads s as a float64. It refuses what is not a decimal number, and a value that does not fit.
func parseDecimal(s string) (float64, error) {
	if !decimal.MatchString(s) {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseFloat(s, 64)
}

// flag is a 0/1 field: false when it is empty. fieldsOf has refused any other value (the options of flagField), so
// the reader does not.
func flag(v url.Values, name string) bool { return v.Get(name) == "1" }

// The lists page (docs/plans/079-client-contract-hygiene.md): limit and offset select a page, and the entity
// links the next one when a further row exists, the previous one when offset > 0.
const maxPageLimit = 500

// pageOf reads limit (1–maxPageLimit, def when absent) and offset (≥ 0) from the query.
func pageOf(r *http.Request, def int) (limit, offset int, err error) {
	q := r.URL.Query()
	limit = def
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > maxPageLimit {
			return 0, 0, core.Invalid("limit must be 1 to %d", maxPageLimit)
		}
	}
	if s := q.Get("offset"); s != "" {
		if offset, err = strconv.Atoi(s); err != nil || offset < 0 {
			return 0, 0, core.Invalid("offset must be 0 or more")
		}
	}
	return limit, offset, nil
}

// pageProps are the properties that every paged list carries: the limit and the offset of the page it shows.
type pageProps struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// readPage reads one page of a list and links its next and previous pages (paged). read is the list's read: it
// gets the rows to ask for and the offset. The rows to ask for are limit+1: the one more row says that a next page
// exists. Only readPage knows this, so a list cannot lose its next link by asking for limit rows.
func readPage[T any](e *siren.Entity, path string, q url.Values, limit, offset int, read func(limit, offset int) ([]T, error)) ([]T, pageProps, error) {
	rows, err := read(limit+1, offset)
	if err != nil {
		return nil, pageProps{}, err
	}
	rows, props := paged(e, rows, path, q, limit, offset)
	return rows, props, nil
}

// paged trims a read of limit+1 rows to the page and adds the next and prev links to e; path and q are the list's
// own address. It gives back the page and its properties, which the caller puts in its own.
func paged[T any](e *siren.Entity, rows []T, path string, q url.Values, limit, offset int) ([]T, pageProps) {
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	return rows, pageLinks(e, more, path, q, limit, offset)
}

// pageLinks adds the next link (when more says a further row exists) and the prev link (when offset > 0) to e. A list
// that knows its total, and so reads no extra row, calls it directly.
func pageLinks(e *siren.Entity, more bool, path string, q url.Values, limit, offset int) pageProps {
	href := func(offset int) string {
		v := url.Values{}
		for k, vs := range q {
			v[k] = vs
		}
		v.Set("limit", strconv.Itoa(limit))
		v.Set("offset", strconv.Itoa(offset))
		return path + "?" + v.Encode()
	}
	if offset > 0 {
		e.Links = append(e.Links, link("prev", href(max(offset-limit, 0)), "Previous page"))
	}
	if more {
		e.Links = append(e.Links, link("next", href(offset+limit), "Next page"))
	}
	return pageProps{Limit: limit, Offset: offset}
}
