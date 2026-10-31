package web_test

// The files that web serves itself: the fonts, the manifest, the icons, the sheets' pictures and the two scripts. They
// hold nothing of the life log, so web does not ask the core for them. The expected types, caches and bytes are
// constants of this test (and the embedded files of the view for the bytes), never the answer of another program.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog-web/view"
)

// The Cache-Control of a file: a name that never changes (a font, a picture; app.css names a picture with ?v= and its
// hash), the manifest and the rules of speculation, and a script.
const (
	cacheForever = "public, max-age=31536000, immutable"
	cacheHour    = "public, max-age=3600"
	cacheScript  = "public, max-age=300"
)

// ownFiles are the files of the browser, each with its path, its embedded file, its type and its Cache-Control. The fonts
// have their own test.
var ownFiles = []struct{ path, file, typ, cache string }{
	{"/manifest.webmanifest", "manifest.webmanifest", "application/manifest+json", cacheHour},
	{"/assets/speculation.json", "speculation.json", "application/speculationrules+json", cacheHour},
	{"/assets/chat.js", "chat.js", "text/javascript; charset=utf-8", cacheScript},
	{"/assets/lock.js", "lock.js", "text/javascript; charset=utf-8", cacheScript},
	{"/assets/icon.svg", "icon.svg", "image/svg+xml", cacheForever},
	{"/assets/icon-192.png", "icon-192.png", "image/png", cacheForever},
	{"/assets/icon-512.png", "icon-512.png", "image/png", cacheForever},
	{"/assets/wood.png", "wood.png", "image/png", cacheForever},
	{"/assets/ground.png", "ground.png", "image/png", cacheForever},
	{"/assets/embers.png", "embers.png", "image/png", cacheForever},
	{"/assets/sprites.png", "sprites.png", "image/png", cacheForever},
	{"/assets/icons.png", "icons.png", "image/png", cacheForever},
}

// installFiles are the nine files that a browser fetches without a session to install the app: the manifest, the icon and
// the pictures of the sheet. They hold nothing of the life log.
var installFiles = []string{"/manifest.webmanifest", "/assets/icon.svg", "/assets/icon-192.png", "/assets/icon-512.png",
	"/assets/wood.png", "/assets/ground.png", "/assets/embers.png", "/assets/sprites.png", "/assets/icons.png"}

// fontURLs are the font files that the style sheet of a page names, as the page gives them.
func fontURLs(t *testing.T, h http.Handler) []string {
	t.Helper()
	page := ask(h, http.MethodGet, "/actions")
	if page.Code != http.StatusOK {
		t.Fatalf("GET /actions: %d", page.Code)
	}
	var urls []string
	for _, m := range regexp.MustCompile(`url\((/assets/fonts/[^)]+)\)`).FindAllStringSubmatch(page.Body.String(), -1) {
		urls = append(urls, m[1])
	}
	return urls
}

// TestTheOwnFilesHaveTheirTypeTheirCacheAndTheirBytes: each file is 200 with the documented type and Cache-Control, the
// nosniff header, the bytes of the embedded file and their length. A HEAD gets the same headers and no body.
func TestTheOwnFilesHaveTheirTypeTheirCacheAndTheirBytes(t *testing.T) {
	l := emptyLife(t)
	for _, f := range ownFiles {
		want := view.MustFile(f.file)
		if len(want) == 0 {
			t.Fatalf("the embedded file %s is empty", f.file)
		}
		got := ask(l.web, http.MethodGet, f.path)
		if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), want) {
			t.Errorf("GET %s: %d, %d bytes; want 200 and the %d bytes of %s", f.path, got.Code, got.Body.Len(), len(want), f.file)
		}
		for name, value := range map[string]string{"Content-Type": f.typ, "Cache-Control": f.cache, "X-Content-Type-Options": "nosniff",
			"Content-Length": fmt.Sprint(len(want))} {
			if got.Header().Get(name) != value {
				t.Errorf("GET %s: %s is %q, want %q", f.path, name, got.Header().Get(name), value)
			}
		}
		head := ask(l.web, http.MethodHead, f.path)
		if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != f.typ || head.Header().Get("Cache-Control") != f.cache {
			t.Errorf("HEAD %s: %d, %d bytes, %q, %q; want the headers of the GET and no body", f.path, head.Code, head.Body.Len(),
				head.Header().Get("Content-Type"), head.Header().Get("Cache-Control"))
		}
		// A file takes GET and HEAD. Web answers any other method itself.
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			if got := ask(l.web, method, f.path); got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "GET, HEAD" {
				t.Errorf("%s %s: %d, Allow %q, want 405 with GET, HEAD", method, f.path, got.Code, got.Header().Get("Allow"))
			}
		}
	}
}

// TestAPictureWithAVersionIsTheSamePicture: the style sheet names a picture with ?v= and the hash of its bytes (view
// versioned). The route reads the path alone.
func TestAPictureWithAVersionIsTheSamePicture(t *testing.T) {
	l := emptyLife(t)
	plain := ask(l.web, http.MethodGet, "/assets/icons.png")
	got := ask(l.web, http.MethodGet, "/assets/icons.png?v=0123456789ab")
	if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/png" || got.Header().Get("Cache-Control") != cacheForever ||
		!bytes.Equal(got.Body.Bytes(), plain.Body.Bytes()) {
		t.Errorf("GET /assets/icons.png?v=...: %d %q %q, %d bytes", got.Code, got.Header().Get("Content-Type"), got.Header().Get("Cache-Control"), got.Body.Len())
	}
}

// TestTheScriptsNameWhatTheyDo: chat.js polls the conversation of the day of its element, as Siren. lock.js sends /awake,
// asks for /lock, keeps the time in localStorage and waits while a page prerenders. The type, the cache and the size of
// a script are held by TestTheOwnFilesHaveTheirTypeTheirCacheAndTheirBytes and TestTheScriptsAreSmallAndAlone.
func TestTheScriptsNameWhatTheyDo(t *testing.T) {
	l := emptyLife(t)
	for path, words := range map[string][]string{
		"/assets/chat.js": {"application/vnd.siren+json", "/conversation", "dataset.day"},
		"/assets/lock.js": {"/awake", "/lock", "localStorage", "prerendering"},
	} {
		body := ask(l.web, http.MethodGet, path).Body.String()
		for _, want := range words {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
}

// TestTheFontsAreServedByExactName: every face that the style sheet names is served as a font that a browser keeps; the
// licences are text; any other name is 404, and a font takes GET and HEAD and nothing else. A font needs a session, as
// the page that names it does.
func TestTheFontsAreServedByExactName(t *testing.T) {
	l := emptyLife(t)
	urls := fontURLs(t, l.web)
	if len(urls) != 12 {
		t.Fatalf("the style sheet names %d font files, want 12 (6 faces, latin and latin-ext)", len(urls))
	}
	embedded, err := fs.Glob(view.Files, "html/fonts/*.woff2")
	if err != nil {
		t.Fatal(err)
	}
	var named []string
	for _, u := range urls {
		named = append(named, "html/fonts/"+strings.TrimPrefix(u, "/assets/fonts/"))
	}
	if slices.Sort(named); !slices.Equal(named, embedded) {
		t.Errorf("the sheet names %v, the embedded faces are %v: a face is unused or missing", named, embedded)
	}
	for _, u := range urls {
		want, err := view.Files.ReadFile("html/" + strings.TrimPrefix(u, "/assets/"))
		if err != nil {
			t.Fatal(err)
		}
		got := ask(l.web, http.MethodGet, u)
		if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "font/woff2" || !strings.HasPrefix(got.Body.String(), "wOF2") ||
			got.Header().Get("Cache-Control") != cacheForever || got.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(got.Body.Bytes(), want) {
			t.Errorf("GET %s: %d %q %q, %d bytes", u, got.Code, got.Header().Get("Content-Type"), got.Header().Get("Cache-Control"), got.Body.Len())
		}
	}
	for _, licence := range []string{"OFL-atkinson-hyperlegible.txt", "OFL-ibm-plex-mono.txt", "OFL-jersey-15.txt"} {
		got := ask(l.web, http.MethodGet, "/assets/fonts/"+licence)
		if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" || got.Header().Get("Cache-Control") != cacheForever ||
			!strings.Contains(got.Body.String(), "SIL Open Font License, Version 1.1") {
			t.Errorf("the licence %s: %d %q", licence, got.Code, got.Header().Get("Content-Type"))
		}
	}
	for _, path := range []string{"/assets/fonts/unknown.woff2", "/assets/fonts/README.md", "/assets/fonts/..%2fbase.css",
		"/assets/fonts/%2e%2e%2fbase.css", "/assets/fonts/", "/assets/fonts/a%2Fb.woff2", "/assets/fonts/woff2", "/assets/base.css"} {
		if got := ask(l.web, http.MethodGet, path); got.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, got.Code)
		}
	}
	if got := ask(l.web, http.MethodHead, urls[0]); got.Code != http.StatusOK || got.Body.Len() != 0 || got.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("HEAD %s: %d, %d bytes", urls[0], got.Code, got.Body.Len())
	}
	if got := ask(l.web, http.MethodPost, urls[0]); got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST %s: %d, Allow %q, want 405 with GET, HEAD", urls[0], got.Code, got.Header().Get("Allow"))
	}
	// Without a session a browser goes to the login, and a program gets 401.
	if got := ask(l.browser, http.MethodGet, urls[0]); got.Code != http.StatusSeeOther || got.Header().Get("Location") != "/login" {
		t.Errorf("a font without a session, for a browser: %d to %q, want 303 to /login", got.Code, got.Header().Get("Location"))
	}
	if got := request(l.browser, http.MethodGet, urls[0], "", nil); got.Code != http.StatusUnauthorized {
		t.Errorf("a font without a session, for a program: %d, want 401", got.Code)
	}
}

// TestTheManifestAndTheIconAreServed: a browser can install the app. The manifest says what the app is, its icons are the
// PNG files of the sizes that it declares, and the icon is a small SVG with no text and no script. The share target posts
// text to the capture of the local day.
func TestTheManifestAndTheIconAreServed(t *testing.T) {
	l := emptyLife(t)
	got := ask(l.web, http.MethodGet, "/manifest.webmanifest")
	var m struct {
		Name        string `json:"name"`
		StartURL    string `json:"start_url"`
		Display     string `json:"display"`
		ThemeColor  string `json:"theme_color"`
		Icons       []struct{ Src, Sizes, Purpose string }
		Shortcuts   []struct{ Name, URL string }
		ShareTarget struct {
			Action, Method, Enctype string
			Params                  struct {
				Text  string
				Files []struct {
					Name   string
					Accept []string
				}
			}
		} `json:"share_target"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &m); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	if m.Name != "Lifelog" || m.StartURL != "/" || m.Display != "standalone" || m.ThemeColor != "#3a2210" || len(m.Shortcuts) != 2 || len(m.Icons) != 3 ||
		m.ShareTarget.Action != "/days/today/capture" || m.ShareTarget.Method != "POST" || m.ShareTarget.Enctype != "application/x-www-form-urlencoded" ||
		m.ShareTarget.Params.Text != "text" || len(m.ShareTarget.Params.Files) != 0 { // a phone's share sheet sends text to capture
		t.Errorf("the manifest says %+v", m)
	}
	wantIcons := [][3]string{{"/assets/icon-192.png", "192x192", "any"}, {"/assets/icon-512.png", "512x512", "any"}, {"/assets/icon-512.png", "512x512", "maskable"}}
	for n, i := range m.Icons {
		if n < len(wantIcons) && [3]string{i.Src, i.Sizes, i.Purpose} != wantIcons[n] {
			t.Errorf("the manifest icon %d is %+v, want %v", n, i, wantIcons[n])
		}
	}
	for _, i := range m.Icons {
		icon := ask(l.web, http.MethodGet, i.Src)
		cfg, err := png.DecodeConfig(bytes.NewReader(icon.Body.Bytes()))
		if err == nil {
			_, err = png.Decode(bytes.NewReader(icon.Body.Bytes()))
		}
		if err != nil || fmt.Sprintf("%dx%d", cfg.Width, cfg.Height) != i.Sizes {
			t.Errorf("the icon %s decodes as %+v (%v), the manifest says %s", i.Src, cfg, err, i.Sizes)
		}
	}
	if svg := ask(l.web, http.MethodGet, "/assets/icon.svg").Body.String(); !strings.HasPrefix(svg, "<svg ") || strings.Contains(svg, "<script") ||
		strings.Contains(svg, "<text") || len(svg) > 1024 {
		t.Errorf("the icon is not a small SVG with no text and no script: %d bytes", len(svg))
	}

	// The share target: a POST of text to /days/today/capture writes on the local day. It posts with the enctype that the
	// manifest declares, as a phone does, and with the session of the installed app.
	shared := sharedText(t, l, m.ShareTarget.Action, m.ShareTarget.Enctype, "Shared from a phone")
	if shared.Code != http.StatusSeeOther || !strings.HasPrefix(shared.Header().Get("Location"), "/days/"+today) {
		t.Fatalf("POST %s: %d to %q, want 303 to the local day %s", m.ShareTarget.Action, shared.Code, shared.Header().Get("Location"), today)
	}
	day := must(t)(l.api.Get(t.Context(), "/days/"+today))
	page := must(t)(l.api.Get(t.Context(), href(day, "page")))
	if body, _ := page.Properties.(map[string]any)["body"].(string); !strings.Contains(body, "Shared from a phone") {
		t.Errorf("the day page of the core does not hold the shared text: %q", body)
	}
	if !strings.Contains(ask(l.web, http.MethodGet, "/days/"+today).Body.String(), "Shared from a phone") {
		t.Errorf("the page of %s does not show the shared text", today)
	}
}

// sharedText posts text to path as the share sheet of a phone does: with the enctype, and with none of the headers of a
// page that is in the browser (no Origin, no fetch metadata).
func sharedText(t *testing.T, l *life, path, enctype, text string) *httptest.ResponseRecorder {
	t.Helper()
	return request(l.web, http.MethodPost, path, url.Values{"text": {text}}.Encode(), map[string]string{
		"Content-Type": enctype, "Accept": "text/html,application/xhtml+xml", "Host": "example.com"})
}

// TestTheInstallFilesNeedNoSession: the files that a browser fetches to install the app hold nothing of the life log, so
// a GET or a HEAD of them needs no session. A write to them is 401, and every other file needs the session: a browser goes
// to the login and a program gets 401.
func TestTheInstallFilesNeedNoSession(t *testing.T) {
	l := emptyLife(t)
	open := 0
	for _, f := range ownFiles {
		if slices.Contains(installFiles, f.path) {
			open++
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				if got := ask(l.browser, method, f.path); got.Code != http.StatusOK {
					t.Errorf("%s %s with no session: %d, want 200", method, f.path, got.Code)
				}
			}
		} else {
			if got := ask(l.browser, http.MethodGet, f.path); got.Code != http.StatusSeeOther || got.Header().Get("Location") != "/login" {
				t.Errorf("GET %s with no session, for a browser: %d to %q, want 303 to /login", f.path, got.Code, got.Header().Get("Location"))
			}
			if got := request(l.browser, http.MethodGet, f.path, "", nil); got.Code != http.StatusUnauthorized {
				t.Errorf("GET %s with no session, for a program: %d, want 401", f.path, got.Code)
			}
		}
		if got := request(l.browser, http.MethodPost, f.path, "", nil); got.Code != http.StatusUnauthorized {
			t.Errorf("POST %s with no session: %d, want 401", f.path, got.Code)
		}
		if got := ask(l.web, http.MethodGet, f.path); got.Code != http.StatusOK {
			t.Errorf("GET %s with a session: %d, want 200", f.path, got.Code)
		}
	}
	if open != len(installFiles) {
		t.Errorf("%d of the install files are files of web, want all %d", open, len(installFiles))
	}
}
