package aminte

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aminte/client"
)

const addFileAction = `{"actions":[{"name":"add-file","method":"POST","href":"/files","fields":[
{"name":"day","type":"date"},{"name":"title","type":"text"},{"name":"mime","type":"text"},
{"name":"original","type":"file"},{"name":"preview","type":"file"}]}]}`

// titlesServer answers 409 exists for the first taken titles, then 200. It records the titles it was sent and checks
// the header, the text fields and the file parts of each upload.
func titlesServer(t *testing.T, taken int) (*Client, *[]string, *int) {
	var titles []string
	var catalogReads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Aminte-Source") != "import:google-photos" ||
			r.Header.Get("Accept") != "application/vnd.siren+json" {
			t.Errorf("headers: %v", r.Header)
		}
		if r.URL.Path == "/actions" {
			catalogReads++
			fmt.Fprint(w, addFileAction)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/files" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse: %v", err)
			return
		}
		title := r.FormValue("title")
		titles = append(titles, title)
		if r.FormValue("day") != "2031-05-04" || r.FormValue("mime") != "image/png" {
			t.Errorf("fields: %v", r.MultipartForm.Value)
		}
		if _, h, err := r.FormFile("original"); err != nil || h.Filename != title {
			t.Errorf("original: %v, %v", h, err)
		}
		if _, h, err := r.FormFile("preview"); err != nil || h.Filename != "preview.jpg" {
			t.Errorf("preview: %v, %v", h, err)
		}
		if len(titles) <= taken {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"class":["error"],"properties":{"status":409,"code":"exists","message":"title in use"}}`)
			return
		}
		fmt.Fprint(w, `{"class":["file"]}`)
	}))
	t.Cleanup(srv.Close)
	return New(client.Remote(srv.URL+"/", Source, "secret")), &titles, &catalogReads
}

func TestAddCoverTriesNumberedTitlesAfterExists(t *testing.T) {
	c, titles, reads := titlesServer(t, 2)
	got, err := c.AddCover(context.Background(), "2031-05-04", "image/png", []byte("x"), []byte("p"))
	if err != nil || got != "2031-05-04 3.png" {
		t.Fatalf("AddCover = %q, %v; want 2031-05-04 3.png", got, err)
	}
	if want := "2031-05-04.png|2031-05-04 2.png|2031-05-04 3.png"; strings.Join(*titles, "|") != want {
		t.Errorf("titles tried = %v, want %s", *titles, want)
	}
	if _, err := c.AddCover(context.Background(), "2031-05-04", "image/png", []byte("x"), []byte("p")); err != nil {
		t.Fatal(err)
	}
	if *reads != 1 {
		t.Errorf("GET /actions was read %d times for two AddCover calls, want 1", *reads)
	}
}

func TestAddCoverStopsAfterTheBound(t *testing.T) {
	c, titles, _ := titlesServer(t, 100)
	if _, err := c.AddCover(context.Background(), "2031-05-04", "image/png", []byte("x"), []byte("p")); err == nil {
		t.Fatal("no error after every title was in use")
	}
	if len(*titles) != maxTitles {
		t.Errorf("tried %d titles, want %d", len(*titles), maxTitles)
	}
}

func TestAnErrorEntityGivesItsCodeAndMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"class":["error"],"properties":{"status":422,"code":"invalid","message":"missing field day"}}`)
	}))
	defer srv.Close()
	_, err := New(client.Remote(srv.URL, Source, "")).DayHasCover(context.Background(), "2031-05-04")
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 422 || ce.Code() != "invalid" || !strings.Contains(err.Error(), "missing field day") {
		t.Errorf("error = %v, want the code and message of the entity", err)
	}
}

func TestExtOfMediaTypes(t *testing.T) {
	for mime, want := range map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif", "image/heic": "heic", "image/webp": "webp"} {
		if got := Ext(mime); got != want {
			t.Errorf("Ext(%s) = %s, want %s", mime, got, want)
		}
	}
}
