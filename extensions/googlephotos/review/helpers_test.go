package review_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog-googlephotos/google"
	"lifelog-googlephotos/lifelog"
	"lifelog-googlephotos/review"
	"lifelog/client"
)

// jpegOf makes a small synthetic JPEG. Two seeds give two different files.
func jpegOf(t testing.TB, seed int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.Set(x, y, color.RGBA{R: uint8(seed * 37), G: uint8(x*8 + seed), B: uint8(y*8 + seed*5), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// photo is a picked item of the fake Google.
type photo struct {
	ID, Create, Type, MIME string
	Served                 string // the type of the original as Google sends it; "" is MIME
	Seed                   int
}

// fakeGoogle serves the token endpoint, the Picker API and the media bytes.
type fakeGoogle struct {
	*httptest.Server
	t *testing.T

	mu      sync.Mutex
	photos  []photo
	set     bool     // mediaItemsSet
	fetches []string // the media paths fetched, for example "/media/a=d"
	deleted bool
}

func newFakeGoogle(t *testing.T, photos ...photo) *fakeGoogle {
	g := &fakeGoogle{t: t, photos: photos, set: true}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok","refresh_token":"ref","expires_in":3600}`)
	})
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) { g.session(w) })
	mux.HandleFunc("GET /v1/sessions/s1", func(w http.ResponseWriter, r *http.Request) { g.session(w) })
	mux.HandleFunc("DELETE /v1/sessions/s1", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.deleted = true
		g.mu.Unlock()
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("GET /v1/mediaItems", g.mediaItems)
	mux.HandleFunc("GET /media/", g.media)
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGoogle) session(w http.ResponseWriter) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintf(w, `{"id":"s1","pickerUri":"https://photos.example/picker/s1","mediaItemsSet":%v,"pollingConfig":{"pollInterval":"1s"}}`, g.set)
}

// mediaItems serves two photos for each page, to follow nextPageToken.
func (g *fakeGoogle) mediaItems(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	start := 0
	if tok := r.URL.Query().Get("pageToken"); tok != "" {
		fmt.Sscanf(tok, "t%d", &start)
	}
	end := min(start+2, len(g.photos))
	type file struct {
		BaseURL  string `json:"baseUrl"`
		MIMEType string `json:"mimeType"`
		Filename string `json:"filename"`
	}
	type item struct {
		ID         string `json:"id"`
		CreateTime string `json:"createTime"`
		Type       string `json:"type"`
		MediaFile  file   `json:"mediaFile"`
	}
	out := struct {
		MediaItems    []item `json:"mediaItems"`
		NextPageToken string `json:"nextPageToken,omitempty"`
	}{}
	for _, p := range g.photos[start:end] {
		out.MediaItems = append(out.MediaItems, item{p.ID, p.Create, p.Type, file{g.URL + "/media/" + p.ID, p.MIME, p.ID + ".img"}})
	}
	if end < len(g.photos) {
		out.NextPageToken = fmt.Sprintf("t%d", end)
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		g.t.Errorf("encode items: %v", err)
	}
}

// media serves /media/<id>=<size>. The original has the type of its item: a synthetic JPEG, or, for any other type,
// synthetic bytes of that type. Every other size is a JPEG, as Google scales a photo to a JPEG.
func (g *fakeGoogle) media(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "no token", http.StatusUnauthorized)
		return
	}
	g.mu.Lock()
	g.fetches = append(g.fetches, r.URL.Path)
	g.mu.Unlock()
	id, size, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/media/"), "=")
	for _, p := range g.photos {
		if p.ID != id {
			continue
		}
		served := p.MIME
		if p.Served != "" {
			served = p.Served
		}
		b, typ := jpegOf(g.t, p.Seed), "image/jpeg"
		switch {
		case size != "d":
			b = jpegOf(g.t, p.Seed+100)
		case served != "" && served != typ:
			b, typ = []byte(fmt.Sprintf("synthetic %s %d", served, p.Seed)), served
		}
		w.Header().Set("Content-Type", typ)
		if _, err := w.Write(b); err != nil {
			g.t.Errorf("write media: %v", err)
		}
		return
	}
	http.NotFound(w, r)
}

func (g *fakeGoogle) fetched(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, f := range g.fetches {
		if f == path {
			return true
		}
	}
	return false
}

// stack is the review page wired to a fake Google and a lifelog address.
type stack struct {
	rev       *review.Server
	web       *httptest.Server
	auth      *google.Auth
	tokenPath string
	client    *http.Client // does not follow redirects
}

func newStack(t *testing.T, g *fakeGoogle, lifelogURL string, connected bool) *stack {
	t.Helper()
	tokenPath := filepath.Join(t.TempDir(), "token.json")
	if connected {
		b, err := json.Marshal(google.Token{AccessToken: "tok", RefreshToken: "ref", Expiry: time.Now().Add(24 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tokenPath, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	auth := &google.Auth{
		Config:    google.Config{ClientID: "cid", ClientSecret: "sec", AuthURL: g.URL + "/auth", TokenURL: g.URL + "/token"},
		HTTP:      hc,
		TokenPath: tokenPath,
	}
	rev := review.New(review.Config{
		Auth:    auth,
		Picker:  &google.Picker{Base: g.URL + "/v1", HTTP: hc, Tokens: auth},
		Lifelog: lifelog.New(client.Remote(lifelogURL, lifelog.Source, "")),
		Zone:    time.UTC,
		Log:     log.New(io.Discard, "", 0),
	})
	web := httptest.NewServer(rev.Handler())
	t.Cleanup(web.Close)
	return &stack{rev: rev, web: web, auth: auth, tokenPath: tokenPath, client: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// reply is an answer of the review page.
type reply struct {
	Status   int
	Location string
	Body     string
}

func (s *stack) do(t *testing.T, req *http.Request) reply {
	t.Helper()
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{resp.StatusCode, resp.Header.Get("Location"), string(b)}
}

func (s *stack) get(t *testing.T, path string) reply {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.web.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s.do(t, req)
}

func (s *stack) post(t *testing.T, path string, form url.Values, header map[string]string) reply {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.web.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return s.do(t, req)
}

// home follows the redirects of a GET until a page, like a browser.
func (s *stack) home(t *testing.T) reply {
	t.Helper()
	path := "/"
	for range 5 {
		r := s.get(t, path)
		if r.Status != http.StatusSeeOther {
			return r
		}
		path = r.Location
	}
	t.Fatal("too many redirects")
	return reply{}
}

// pickAll runs the flow of a pick: Pick, then the session page, which lists the photos.
func (s *stack) pickAll(t *testing.T) {
	t.Helper()
	if r := s.post(t, "/pick", nil, nil); r.Status != http.StatusSeeOther || r.Location != "/session" {
		t.Fatalf("POST /pick = %d %q, want 303 /session", r.Status, r.Location)
	}
	if r := s.get(t, "/session"); r.Status != http.StatusSeeOther {
		t.Fatalf("GET /session = %d, want 303 to /: %s", r.Status, r.Body)
	}
}
