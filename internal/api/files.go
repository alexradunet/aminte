package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/photo"
	"lifelog/internal/preview"
	"lifelog/siren"
)

const (
	maxPicture       = 64 << 20 // the largest original a picture is made from, and the largest picture sent
	maxParts         = 64
	maxMultipartText = 16 << 20
	maxField         = 16 << 20 // a text field: a long transcript
	maxMeta          = 1 << 20  // the head of an original read for its metadata
)

// errNoFile is the refusal of a form that sends neither the original nor its sha256 and mime.
const errNoFile = "send the original, or its sha256 and mime"

// fileForm is a request of add-file or set-cover, read.
type fileForm struct {
	In   core.FileIn
	Dry  bool   // dry_run: write nothing, say what keeping the file would do
	Sent bool   // a file was sent: the original, or its sha256 and mime. A form with no file is not refused: the caller decides
	Name string // the original's file name; "" when no original was sent
}

// addFile keeps a file (docs/cookbook/keep-a-file.md, D9) and answers with its page; Result says what was written.
func (h *server) addFile(r *http.Request, a admitted) (*siren.Entity, error) {
	f, err := parseFileForm(r, a.act)
	if err != nil {
		return nil, err
	}
	if !f.Sent {
		return nil, core.Invalid(errNoFile)
	}
	in := f.In
	if in.Title == "" {
		in.Title = f.Name
	}
	if in.Title == "" { // a sha256 has no name
		return nil, core.Invalid("missing field title: name the file, or send its original")
	}
	in.Day = h.dayNamed(in.Day)
	if f.Dry {
		k, err := h.s.TryAddFile(r.Context(), a.src, in)
		if err != nil {
			return nil, err
		}
		return &siren.Entity{Class: []string{"result", "dry-run"}, Title: "Dry run: nothing written",
			Properties: map[string]any{"title": in.Title, "sha256": in.SHA256, "mime": in.MIME, "picture": in.Preview != nil},
			Result:     k, Links: []siren.Link{link("index", "/", "Home")}}, nil
	}
	k, err := h.s.AddFile(r.Context(), a.src, in)
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), k.ID))(k)
}

// parseFileForm reads a request of the action act (add-file or set-cover): its fields, refused as fieldsOf refuses
// them. Multipart (a browser, the CLI): the original is streamed through SHA-256 and never stored; a picture the
// writer reads is held, up to maxPicture, to make the preview from. A multipart form with the file input left empty
// and no sha256 and no mime sends no file (Sent is false), and is not refused here. Url-encoded or JSON (an agent
// that hashed the file itself): sha256 and mime, and no picture.
func parseFileForm(r *http.Request, act spec) (fileForm, error) {
	if !isMultipart(r) {
		return encodedFileForm(r, act)
	}
	m, err := readMultipart(r, act)
	if err != nil {
		return fileForm{}, err
	}
	if err := checked(m.text, act, r); err != nil {
		return fileForm{}, err
	}
	in, sent, err := verifyOriginal(m)
	if err != nil || !sent {
		return fileForm{In: in}, err
	}
	if in.Preview, err = previewOf(m); err != nil {
		return fileForm{}, err
	}
	return fileForm{In: in, Dry: flag(m.text, "dry_run"), Sent: true, Name: m.name}, nil
}

// encodedFileForm reads a file form that is not multipart: there is no original, so sha256 and mime say which file.
func encodedFileForm(r *http.Request, act spec) (fileForm, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return fileForm{}, err
	}
	switch {
	case v.Get("sha256") == "" && v.Get("mime") == "": // no file sent, as verifyOriginal says it
		return fileForm{In: core.FileIn{Title: v.Get("title")}}, nil
	case v.Get("sha256") == "" || v.Get("mime") == "":
		return fileForm{}, core.Invalid(errNoFile)
	}
	in := core.FileIn{Title: v.Get("title"), SHA256: v.Get("sha256"), MIME: v.Get("mime"), Body: v.Get("body"), Day: v.Get("day")}
	return fileForm{In: in, Dry: flag(v, "dry_run"), Sent: true}, nil
}

// multipartForm is what readMultipart takes from a multipart request.
type multipartForm struct {
	text      url.Values // the text fields of the action, within the budget
	textBytes int        // the budget used: every text field counts, kept or not
	original  bool       // an original with a file name was sent
	name      string     // the original's file name
	sum, typ  string     // SHA-256 and type of the original
	picture   []byte     // the original, held, when the writer can read it as a picture and it is not too large
	given     []byte     // the preview part, when one was sent
	meta      photo.Meta // the metadata read from the head of the original
}

// readMultipart streams the parts of a multipart request and keeps only what the budgets allow: bounded text of known
// names, and the original and the preview. It returns what it has read, also when it returns an error.
func readMultipart(r *http.Request, act spec) (*multipartForm, error) {
	m := &multipartForm{text: url.Values{}}
	defer r.Body.Close()
	mr, err := r.MultipartReader()
	if err != nil {
		return m, invalidForm(err)
	}
	parts := 0
	seen := map[string]bool{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return m, nil
		}
		if err != nil {
			return m, invalidForm(err)
		}
		parts++
		if parts > maxParts {
			return m, requestLimit("multipart request has more than 64 parts")
		}
		name := part.FormName()
		if name == "original" || name == "preview" {
			if seen[name] {
				return m, requestLimit("duplicate " + name + " part")
			}
			seen[name] = true
		}
		if err := m.take(part, name, act); err != nil {
			return m, err
		}
		part.Close()
	}
}

// invalidForm is the refusal of a request that is not a form that can be read.
func invalidForm(err error) error { return core.Invalid("the form: %v", err) }

// take reads one part into m.
func (m *multipartForm) take(part *multipart.Part, name string, act spec) error {
	var err error
	switch {
	case (name == "original" || name == "preview") && part.FileName() == "": // a file input left empty
		_, err = io.Copy(io.Discard, part)
	case name == "original":
		m.original, m.name = true, part.FileName()
		m.sum, m.typ, m.picture, m.meta, err = readOriginal(part)
	case name == "preview":
		m.given, err = io.ReadAll(io.LimitReader(part, maxPicture+1))
		if err == nil && len(m.given) > maxPicture {
			return requestLimit("the picture sent is larger than 64 MiB")
		}
	default:
		return m.takeText(part, name, act)
	}
	if err != nil {
		return invalidForm(err)
	}
	return nil
}

// takeText reads a text part. A name that is not a text field of the action act is refused before its bytes are
// read.
func (m *multipartForm) takeText(part *multipart.Part, name string, act spec) error {
	if !slices.Contains(textFields(act), name) {
		return unknownField(name, act)
	}
	limited := io.LimitReader(part, int64(min(maxField, maxMultipartText-m.textBytes)+1))
	b, err := io.ReadAll(limited)
	n := int64(len(b))
	if err == nil && n <= maxField && m.textBytes+len(b) <= maxMultipartText {
		m.text.Set(name, string(b))
	}
	m.textBytes += int(n)
	if err != nil {
		return invalidForm(err)
	}
	if n > maxField || m.textBytes > maxMultipartText {
		return requestLimit("multipart text is larger than 16 MiB")
	}
	return nil
}

// verifyOriginal makes the file of the form from its text and its original: an original gives the sha256 (a sha256
// sent with it must be the same) and the type if none was sent. Without an original, the form must send sha256 and
// mime, or neither: sent is then false, and the file is the one of the form's text only.
func verifyOriginal(m *multipartForm) (in core.FileIn, sent bool, err error) {
	in = core.FileIn{Title: m.text.Get("title"), SHA256: m.text.Get("sha256"), MIME: m.text.Get("mime"),
		Body: m.text.Get("body"), Day: m.text.Get("day"), Taken: m.meta.Day()}
	switch {
	case m.original && in.SHA256 != "" && !strings.EqualFold(strings.TrimSpace(in.SHA256), m.sum):
		return core.FileIn{}, false, core.Invalid("the sha256 sent is not the original's (%s)", m.sum)
	case m.original:
		in.SHA256 = m.sum
		if in.MIME == "" {
			in.MIME = m.typ
		}

	case in.SHA256 == "" && in.MIME == "": // no file sent: the dry_run field is not read
		return in, false, nil
	case in.SHA256 == "" || in.MIME == "":
		return core.FileIn{}, false, core.Invalid(errNoFile)
	}
	return in, true, nil
}

// previewOf makes the picture of the file: from the preview that was sent, else from the original when the writer can
// read it. Nil when there is neither.
func previewOf(m *multipartForm) ([]byte, error) {
	source := m.given
	if source == nil {
		source = m.picture
	}
	if source == nil {
		return nil, nil
	}
	p, err := preview.Make(source)
	if err != nil {
		return nil, core.Invalid("the picture: %v", err)
	}
	return p, nil
}

// textFields are the names of the text parts that a multipart request to the action act may carry: its fields that are
// not files.
func textFields(act spec) []string {
	var names []string
	for _, f := range act.Fields {
		if f.Type != "file" && f.In != "path" {
			names = append(names, f.Name)
		}
	}
	return names
}

// readOriginal hashes the original as it streams, and keeps its first megabyte for its metadata (photo.Read: a
// JPEG's EXIF is in its first 64 KB; a HEIC has no metadata for lifelog); a picture the writer can read is held whole,
// unless it passes maxPicture (it is then kept without a picture, as a HEIC is).
func readOriginal(part *multipart.Part) (sum, typ string, picture []byte, meta photo.Meta, err error) {
	h := sha256.New()
	head := make([]byte, 512)
	n, err := io.ReadFull(part, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", "", nil, meta, err
	}
	head = head[:n]
	h.Write(head)
	typ = mimeOf(part.FileName(), head)
	held := &capped{max: maxPicture}
	first := &prefix{max: maxMeta}
	first.Write(head)
	w := io.MultiWriter(h, first)
	if preview.Readable(typ) {
		held.b.Write(head)
		w = io.MultiWriter(h, first, held)
	}
	if _, err := io.Copy(w, part); err != nil {
		return "", "", nil, meta, err
	}
	if preview.Readable(typ) && !held.over {
		picture = held.b.Bytes()
	}
	return hex.EncodeToString(h.Sum(nil)), typ, picture, photo.Read(first.b), nil
}

// prefix keeps the first max bytes written to it and ignores the rest.
type prefix struct {
	b   []byte
	max int
}

func (p *prefix) Write(b []byte) (int, error) {
	if room := p.max - len(p.b); room > 0 {
		p.b = append(p.b, b[:min(room, len(b))]...)
	}
	return len(b), nil
}

// capped holds what is written to it until it passes max, then drops it and holds nothing more.
type capped struct {
	b    bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if !c.over {
		if c.b.Len()+len(p) > c.max {
			c.over, c.b = true, bytes.Buffer{}
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

// picture serves the JPEG a file page keeps, 404 when there is none.
// A cache that is not empty is sent as Cache-Control. A failed write is logged, as answer does.
func picture(get func(*http.Request) ([]byte, error), cache string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := get(r)
		if err == nil && b == nil {
			err = core.NotFound("this page keeps no picture")
		}
		if err != nil { // the error entity, as every other route answers
			writeError(w, r, err)
			return
		}
		if cache != "" {
			w.Header().Set("Cache-Control", cache)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		if _, err := w.Write(b); err != nil {
			logFault(r, "the picture was not sent", err)
		}
	}
}

// setCover makes a photo a page's cover (core.SetCover): a new photo sent as original, or a kept file named by
// title. It answers with the page.
func (h *server) setCover(r *http.Request, a admitted) (*siren.Entity, error) {
	f, err := parseFileForm(r, a.act)
	if err != nil {
		return nil, err
	}
	if !f.Sent {
		return h.setCoverOf(r, a.src, a.id, f.In.Title)
	}
	k, err := h.s.SetCover(r.Context(), a.src, a.id, h.s.Today(), f.In)
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), a.id))(k)
}

func (h *server) setCoverOf(r *http.Request, src string, id int64, title string) (*siren.Entity, error) {
	if title == "" {
		return nil, core.Invalid("title: a kept picture's title, or send a new photo as original")
	}
	sync, err := h.s.SetCoverOf(r.Context(), src, id, title)
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(sync)
}

// coverChoice is GET /pages/{id}/cover: what a page may take as its cover (docs/plans/127-a-cover-for-every-page.md).
// It carries the page, its cover, a page of the kept pictures (core.Pictures) with the cover marked, and set-cover,
// which takes a new photo or one of those pictures by its title. A file page has none, and a deleted page is revived
// first, as set-cover refuses them.
func (h *server) coverChoice(r *http.Request) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	p, err := h.s.PageByID(r.Context(), id)
	switch {
	case err != nil:
		return nil, err
	case p.Type == "file":
		return nil, core.NotFound("%q is a file: its own picture is its cover", p.Title)
	case p.DeletedAt != "":
		return nil, core.Conflict("page %d %q is deleted: restore it first (Restore, revive)", id, p.Title)
	}
	limit, offset, err := pageOf(r, 48)
	if err != nil {
		return nil, err
	}
	self := siren.CoverHref(id)
	e := &siren.Entity{Class: []string{"cover-choice"}, Title: "Cover of " + p.Title,
		Links:   []siren.Link{link("self", self, "Set cover photo"), link("page", siren.PageHref(id), p.Title), link("index", "/", "Home")},
		Actions: []siren.Action{action("set-cover", idParam(id), nil)}}
	found, props, err := readPage(e, self, nil, limit, offset, func(limit, offset int) ([]core.FileRow, error) {
		return h.s.Pictures(r.Context(), limit, offset)
	})
	if err != nil {
		return nil, err
	}
	rows := []coverRow{}
	for _, f := range found {
		rows = append(rows, coverRow{ID: f.ID, Title: f.Title, Picture: pictureHref(f.ID), Day: f.Day,
			Current: p.Cover != nil && p.Cover.ID == f.ID})
	}
	e.Properties = coverChoiceProps{pageProps: props, Page: pageRef{ID: id, Title: p.Title, DayPage: p.IsDayPage},
		Cover: p.Cover, Rows: rows}
	return e, nil
}

// coverChoiceProps are the properties of GET /pages/{id}/cover.
type coverChoiceProps struct {
	pageProps
	Page  pageRef     `json:"page"`
	Cover *core.Cover `json:"cover,omitempty"` // absent when the page has none
	Rows  []coverRow  `json:"rows"`
}

// pageRef names the page whose cover is chosen.
type pageRef struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	DayPage bool   `json:"is_day_page"`
}

// coverRow is a kept picture a page may take as its cover; Current marks the page's cover.
type coverRow struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Picture string `json:"picture"`
	Day     string `json:"day,omitempty"`
	Current bool   `json:"current,omitempty"`
}
