package web

// The routes that web serves itself: the files of the browser (sheets, scripts, pictures, fonts, the manifest) and
// /render. They hold nothing of the database, so they ask the core for nothing.

import (
	"bytes"
	"mime"
	"net/http"
	"strings"
	"time"

	"lifelog-web/view"
	"lifelog/siren"
)

// maxRequestBody is the largest body that web reads: the text of a form (write.go) and /render. It is the limit of the
// core for a body that is not a file.
const maxRequestBody = 20 << 20

// immutable and the other values are the Cache-Control of a file. A picture never changes under its name: app.css
// names it with ?v= and the hash of its bytes (view.versioned).
const (
	immutable = "public, max-age=31536000, immutable"
	hour      = "public, max-age=3600"
	script    = "public, max-age=300"
)

// fontTypes are the files the font route serves, by their extension: the faces and their licences.
var fontTypes = map[string]string{"woff2": "font/woff2", "txt": "text/plain; charset=utf-8"}

// ownRoutes registers the routes of web's own files and /render on m.
func (b *Browser) ownRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /assets/fonts/{file}", fonts)
	m.HandleFunc("GET /manifest.webmanifest", file("manifest.webmanifest", "application/manifest+json", hour))
	// The rules of speculation. JSON has no comments, so the reasons are here. No address with a query string: a
	// ?feedback= receipt is read once, and ?new= opens a form. The pattern is "/*\?*=*", with the "=" every query of
	// this app has: a URLPattern "/*\?*" alone has the search "*", which an empty search matches too, so it would
	// exclude every address. The others are the routes that redirect (today, go, random, find), the pictures, the
	// downloads of a snapshot, the install file and the integrity check, which a hover must not run. A browser
	// without speculation rules ignores the header.
	m.HandleFunc("GET /assets/speculation.json", file("speculation.json", "application/speculationrules+json", hour))
	m.HandleFunc("GET /assets/chat.js", file("chat.js", "text/javascript; charset=utf-8", script))
	m.HandleFunc("GET /assets/lock.js", file("lock.js", "text/javascript; charset=utf-8", script))
	m.HandleFunc("GET /assets/icon.svg", file("icon.svg", "image/svg+xml", immutable))
	for _, name := range []string{"icon-192.png", "icon-512.png", "wood.png", "ground.png", "embers.png", "sprites.png", "icons.png"} {
		m.HandleFunc("GET /assets/"+name, file(name, "image/png", immutable))
	}
	m.HandleFunc("POST /render", b.render)
}

// fonts is GET /assets/fonts/{file}: an embedded face or licence, by its exact name (view/html/fonts/README.md). A file
// never changes under its name, so a browser keeps it for a year.
func fonts(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	typ := fontTypes[name[strings.LastIndex(name, ".")+1:]]
	if typ == "" || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	b, err := view.Files.ReadFile("html/fonts/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, name, b, typ, immutable)
}

// file serves an embedded file of view/html/. The file is the same for every request.
func file(name, typ, cache string) http.HandlerFunc {
	b := view.MustFile(name)
	return func(w http.ResponseWriter, r *http.Request) { serveFile(w, r, name, b, typ, cache) }
}

func serveFile(w http.ResponseWriter, r *http.Request, name string, b []byte, typ, cache string) {
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// render is POST /render: a page that shows the field body (or text, a capture's) as its page will show it saved, the
// Preview of a form that writes a body, opened in a new tab so the form keeps its words. It writes nothing and asks the
// core for nothing: the page has no shell. The guard of the origin and the session of the door come before it.
func (b *Browser) render(w http.ResponseWriter, r *http.Request) {
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/x-www-form-urlencoded" {
		b.fail(w, r, http.StatusUnsupportedMediaType, siren.CodeUnsupportedMedia, "send application/x-www-form-urlencoded")
		return
	}
	if err := readForm(w, r, maxRequestBody, formWithin); err != nil {
		b.refuseBody(w, r, err)
		return
	}
	body := r.Form.Get("body")
	if body == "" {
		body = r.Form.Get("text")
	}
	e := &siren.Entity{Class: []string{"preview"}, Title: "Preview", Properties: map[string]any{"body": body}}
	page, err := view.Draw(e, view.Facts{URL: r.URL})
	if err != nil {
		logFault(r, "the preview cannot be drawn", err)
		b.fail(w, r, http.StatusInternalServerError, siren.CodeInternal, "internal error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, r, http.StatusOK, page, "", false)
}
