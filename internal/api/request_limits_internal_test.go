package api

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"lifelog/internal/core"
)

// TestMultipartUnknownNameProjection: parseFileForm keeps the value of a name the action lists, and refuses any other
// name (a name of 64 KiB too, which is a modest header and no stress fixture).
func TestMultipartUnknownNameProjection(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	field := func(name, value string) {
		w, err := mw.CreateFormField(name)
		if err == nil {
			_, err = io.WriteString(w, value)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	sum := strings.Repeat("ab", 32)
	for _, f := range [][2]string{{"title", "Scan"}, {"sha256", sum}, {"mime", "application/pdf"}, {"body", "the text"},
		{"day", "2031-05-14"}, {"dry_run", "1"}} {
		field(f[0], f[1])
	}
	// "original" and "preview" with no file name are file inputs left empty.
	for _, name := range []string{"original", "preview"} {
		field(name, "NOT KEPT")
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/files", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())

	f, err := parseFileForm(r, specOf("add-file"))
	want := core.FileIn{Title: "Scan", SHA256: sum, MIME: "application/pdf", Body: "the text", Day: "2031-05-14"}
	if err != nil || !f.Sent || !f.Dry || !reflect.DeepEqual(f.In, want) {
		t.Errorf("parseFileForm = %+v, %v; want %+v, dry and sent", f, err, want)
	}
	// The names of a browser (back, feedback, new) are no names of a file form: each is refused.
	for _, name := range []string{"back", "feedback", "new"} {
		var other bytes.Buffer
		mw := multipart.NewWriter(&other)
		if w, err := mw.CreateFormField(name); err != nil {
			t.Fatal(err)
		} else if _, err := io.WriteString(w, "/browse"); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/files", &other)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		if _, err := parseFileForm(r, specOf("add-file")); err == nil || !strings.Contains(err.Error(), "unknown field "+name) {
			t.Errorf("a part named %s: %v, want the refusal that names it", name, err)
		}
	}
}

func TestMultipartRefusesAnyNameTheActionDoesNotList(t *testing.T) {
	for _, name := range []string{"", "unknown", "Title", "title-extra", "category", "unknown" + strings.Repeat("x", 64<<10)} {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		if w, err := mw.CreateFormField(name); err != nil {
			t.Fatal(err)
		} else if _, err := io.WriteString(w, "NOT KEPT"); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/files", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		_, err := parseFileForm(r, specOf("add-file"))
		var ce *core.Error
		if !errors.As(err, &ce) || ce.Status != 422 {
			t.Errorf("a part named %.20q: %v, want 422", name, err)
		}
	}
}
