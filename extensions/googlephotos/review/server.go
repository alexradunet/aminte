// Package review is the local web page where the owner picks the cover of each day. The server keeps its state in
// memory, behind one mutex. The handlers are synchronous: the page of a pending pick refreshes itself, and no
// goroutine runs beside a request.
package review

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"aminte-googlephotos/aminte"
	"aminte-googlephotos/google"
)

//go:embed html/pages.html
var htmlFS embed.FS

var pages = template.Must(template.ParseFS(htmlFS, "html/pages.html"))

const (
	staleAfter  = 50 * time.Minute // a baseUrl is valid 60 minutes: list again before it ends
	maxForm     = 64 << 10
	previewSize = "=w1280-h1280"
	tileSize    = "=w256-h256-c"
)

// Config is what a Server needs.
type Config struct {
	Auth   *google.Auth
	Picker *google.Picker
	Aminte *aminte.Client
	Zone   *time.Location   // the zone that gives the day of a photo
	Now    func() time.Time // nil means time.Now
	Log    *log.Logger      // nil means log.Default(); it never gets a token or the bytes of a photo
}

// Server is the review page.
type Server struct {
	cfg Config
	mux *http.ServeMux

	mu       sync.Mutex
	state    string // the OAuth state of the sign in in progress
	verifier string // its PKCE verifier
	session  *google.Session
	items    map[string]google.Item
	byDay    map[string][]string // day -> ids of its items, oldest first
	pending  []string            // the days to choose, oldest first
	loaded   bool                // a pick was listed
	listedAt time.Time
	notice   string // one message for the next page
}

// New makes the server.
func New(cfg Config) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = log.Default()
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /{$}", s.index)
	s.mux.HandleFunc("GET /connect", s.connect)
	s.mux.HandleFunc("GET /oauth/callback", s.callback)
	s.mux.HandleFunc("POST /pick", s.pick)
	s.mux.HandleFunc("GET /session", s.sessionPage)
	s.mux.HandleFunc("GET /thumb/{id}", s.thumb)
	s.mux.HandleFunc("POST /choose", s.choose)
	s.mux.HandleFunc("POST /skip", s.skip)
	s.mux.HandleFunc("POST /done", s.done)
	return s
}

// Handler is the server with its guards: the Host must be a loopback name, and a POST from another site is refused.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "forbidden: this page answers on a loopback address only", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && !sameSite(r) {
			http.Error(w, "forbidden: a request from another site", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
		r.Body = http.MaxBytesReader(w, r.Body, maxForm)
		s.mux.ServeHTTP(w, r)
	})
}

// loopbackHost tells whether a Host header names this machine. It stops a DNS rebinding page from reaching the server.
func loopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// sameSite tells whether a POST comes from a page of this server. Sec-Fetch-Site decides when the browser sends it.
// Else an Origin, when there is one, must be this server. A request with neither (a script) is not from a browser.
func sameSite(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	}
	return true
}

// LoopbackAddr checks that addr is host:port with a loopback host, and gives it back.
func LoopbackAddr(addr string) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("address %q: %w", addr, err)
	}
	if !loopbackHost(host) {
		return "", fmt.Errorf("address %q: the review page listens on a loopback address only (127.0.0.1, [::1] or localhost)", addr)
	}
	return addr, nil
}

type tile struct{ ID, Name string }

type view struct {
	Connected bool
	Loaded    bool
	Notice    string
	Day       string
	Tiles     []tile
	Left      int
	Refresh   int // seconds, for a meta refresh; 0 for none
	PickerURI string
}

func (s *Server) render(w http.ResponseWriter, name string, v view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.ExecuteTemplate(w, name, v); err != nil {
		s.cfg.Log.Printf("render %s: %v", name, err)
	}
}

// flash keeps a message for the next page and goes there. The caller holds the lock.
func (s *Server) flash(w http.ResponseWriter, r *http.Request, to, format string, args ...any) {
	s.notice = fmt.Sprintf(format, args...)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// fail logs an error and shows it to the owner. The caller holds the lock.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.cfg.Log.Printf("%s: %v", what, err)
	if errors.Is(err, google.ErrNotConnected) {
		s.flash(w, r, "/", "Google needs a new sign in: use \"Sign in again\".")
		return
	}
	var se *google.StatusError
	if errors.As(err, &se) && se.Status == http.StatusNotFound && s.session != nil {
		s.session, s.loaded = nil, false
		s.flash(w, r, "/", "The Google session has ended. Pick again.")
		return
	}
	s.flash(w, r, "/", "%s: %v", what, err)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := view{Connected: s.cfg.Auth.Connected(), Loaded: s.loaded, Notice: s.notice, Left: len(s.pending)}
	s.notice = ""
	if s.session != nil && !s.loaded {
		http.Redirect(w, r, "/session", http.StatusSeeOther)
		return
	}
	if len(s.pending) > 0 {
		v.Day = s.pending[0]
		for _, id := range s.byDay[v.Day] {
			v.Tiles = append(v.Tiles, tile{ID: id, Name: s.items[id].Filename})
		}
	}
	s.render(w, "index", v)
}

func (s *Server) redirectURI(r *http.Request) string { return "http://" + r.Host + "/oauth/callback" }

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := google.RandomString(16)
	if err == nil {
		s.state = state
		s.verifier, err = google.RandomString(32)
	}
	if err != nil {
		s.fail(w, r, "sign in", err)
		return
	}
	http.Redirect(w, r, s.cfg.Auth.AuthURL(s.redirectURI(r), s.state, s.verifier), http.StatusSeeOther)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, verifier := s.state, s.verifier
	s.state, s.verifier = "", "" // one use
	got := r.URL.Query().Get("state")
	if state == "" || subtle.ConstantTimeCompare([]byte(got), []byte(state)) != 1 {
		http.Error(w, "the sign in is refused: the state does not match", http.StatusBadRequest)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		s.flash(w, r, "/", "Google refused the sign in: %s", e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.cfg.Auth.Exchange(ctx, r.URL.Query().Get("code"), verifier, s.redirectURI(r)); err != nil {
		s.fail(w, r, "sign in", err)
		return
	}
	s.flash(w, r, "/", "Connected to Google Photos.")
}

func (s *Server) pick(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	sess, err := s.cfg.Picker.CreateSession(ctx)
	if err == nil {
		if u, perr := url.Parse(sess.PickerURI); perr != nil || u.Scheme != "https" {
			err = errors.New("the picker address from Google is not https")
		}
	}
	if err != nil {
		s.fail(w, r, "pick", err)
		return
	}
	s.reset()
	s.session = &sess
	http.Redirect(w, r, "/session", http.StatusSeeOther)
}

// reset forgets the picked items. The caller holds the lock.
func (s *Server) reset() {
	s.session, s.items, s.byDay, s.pending, s.loaded = nil, nil, nil, nil, false
}

func (s *Server) sessionPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	sess, err := s.cfg.Picker.GetSession(ctx, s.session.ID)
	if err != nil {
		s.fail(w, r, "check the pick", err)
		return
	}
	if !sess.MediaItemsSet {
		wait := int(sess.PollInterval / time.Second)
		s.render(w, "session", view{Connected: true, Refresh: max(wait, 2), PickerURI: s.session.PickerURI + "/autoclose", Notice: s.takeNotice()})
		return
	}
	if err := s.list(ctx, s.session.ID); err != nil {
		s.fail(w, r, "read the picked photos", err)
		return
	}
	s.flash(w, r, "/", "%d photos picked. %d days have no cover.", len(s.items), len(s.pending))
}

func (s *Server) takeNotice() string {
	n := s.notice
	s.notice = ""
	return n
}

// list reads the picked items, sorts them by day, and keeps the days that show no picture. The caller holds the lock.
func (s *Server) list(ctx context.Context, sessionID string) error {
	items, err := s.cfg.Picker.ListItems(ctx, sessionID)
	if err != nil {
		return err
	}
	s.items = make(map[string]google.Item, len(items))
	s.byDay = map[string][]string{}
	slices.SortFunc(items, func(a, b google.Item) int { return a.CreateTime.Compare(b.CreateTime) })
	months := map[string]bool{}
	for _, it := range items {
		s.items[it.ID] = it
		d := it.Day(s.cfg.Zone)
		s.byDay[d] = append(s.byDay[d], it.ID)
		months[d[:7]] = true
	}
	monthList := make([]string, 0, len(months))
	for m := range months {
		monthList = append(monthList, m)
	}
	sort.Strings(monthList)
	bare, err := s.cfg.Aminte.MonthsWithoutCover(ctx, monthList)
	if err != nil {
		return err
	}
	s.pending = s.pending[:0]
	for d := range s.byDay {
		if bare[d] {
			s.pending = append(s.pending, d)
		}
	}
	sort.Strings(s.pending)
	s.loaded, s.listedAt = true, s.cfg.Now()
	return nil
}

// refresh lists the items again when their addresses are about to expire. The caller holds the lock.
func (s *Server) refresh(ctx context.Context) error {
	if s.session == nil || s.cfg.Now().Sub(s.listedAt) < staleAfter {
		return nil
	}
	items, err := s.cfg.Picker.ListItems(ctx, s.session.ID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if old, ok := s.items[it.ID]; ok {
			old.BaseURL = it.BaseURL
			s.items[it.ID] = old
		}
	}
	s.listedAt = s.cfg.Now()
	return nil
}

func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	s.mu.Lock()
	err := s.refresh(ctx)
	it, ok := s.items[r.PathValue("id")]
	s.mu.Unlock()
	switch {
	case err != nil:
		s.cfg.Log.Printf("thumb: %v", err)
		http.Error(w, "the photo is not available", http.StatusBadGateway)
		return
	case !ok:
		http.NotFound(w, r)
		return
	}
	b, typ, err := s.cfg.Picker.Fetch(ctx, it.BaseURL, tileSize)
	if err != nil {
		s.cfg.Log.Printf("thumb: %v", err)
		http.Error(w, "the photo is not available", http.StatusBadGateway)
		return
	}
	if !strings.HasPrefix(typ, "image/") {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	if _, err := w.Write(b); err != nil {
		s.cfg.Log.Printf("thumb: write: %v", err)
	}
}

func (s *Server) choose(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	it, ok := s.items[r.PostFormValue("item")]
	if !ok || len(s.pending) == 0 || it.Day(s.cfg.Zone) != s.pending[0] {
		s.flash(w, r, "/", "That photo is not for the day shown.")
		return
	}
	day := s.pending[0]
	// The owner can set a cover in aminte at the same time: read the day again before the write.
	has, err := s.cfg.Aminte.DayHasCover(ctx, day)
	if err != nil {
		s.fail(w, r, "read the day", err)
		return
	}
	if has {
		s.pending = s.pending[1:]
		s.flash(w, r, "/", "%s has a cover now. Nothing was written.", day)
		return
	}
	if err := s.refresh(ctx); err != nil {
		s.fail(w, r, "list the photos again", err)
		return
	}
	it = s.items[it.ID]
	original, typ, err := s.cfg.Picker.Fetch(ctx, it.BaseURL, "=d")
	if err != nil {
		s.fail(w, r, "download the photo", err)
		return
	}
	// The type of the answer says what the bytes are; Google can send another type than the one the list names.
	mimeType := typ
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = it.MIME
	}
	var preview []byte
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif":
	default: // aminte cannot read this type: Google scales it down to a JPEG
		if preview, _, err = s.cfg.Picker.Fetch(ctx, it.BaseURL, previewSize); err != nil {
			s.fail(w, r, "download the preview", err)
			return
		}
	}
	title, err := s.cfg.Aminte.AddCover(ctx, day, mimeType, original, preview)
	if err != nil {
		s.fail(w, r, "keep the photo", err)
		return
	}
	s.pending = s.pending[1:]
	s.flash(w, r, "/", "Cover of %s: %s.", day, title)
}

func (s *Server) skip(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) > 0 {
		s.pending = s.pending[1:]
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) done(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if err := s.cfg.Picker.DeleteSession(ctx, s.session.ID); err != nil {
			s.cfg.Log.Printf("delete session: %v", err) // the session ends by itself
		}
	}
	s.reset()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
