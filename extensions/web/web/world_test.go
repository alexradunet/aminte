package web_test

// The synthetic life that the tests of web read: a small file made through the API, as an owner and an agent would
// make it. The clock stands at the start of today, so every page is the same at every run.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog-web/web"
	"lifelog/client"
	"lifelog/lifelogtest"
	"lifelog/siren"
)

const today = "2031-05-14"

// life is a lifelog server with a synthetic file, and web in front of it.
type life struct {
	core    *lifelogtest.Server
	api     *client.Client
	web     *web.Browser
	hrefs   map[string]string // the addresses of the things that the file holds, by a short name
	picture string            // the title of the file page that has a picture
}

// must returns the entity of a call of the client, or fails the test.
func must(t *testing.T) func(*siren.Entity, error) *siren.Entity {
	return func(e *siren.Entity, err error) *siren.Entity {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
}

// act is the action of e with the name, or it fails the test.
func act(t *testing.T, e *siren.Entity, name string) siren.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action %s in %v", name, e.Actions)
	return siren.Action{}
}

func href(e *siren.Entity, rel string) string {
	if l := e.LinkByRel(rel); l != nil {
		return l.Href
	}
	return ""
}

// emptyLife is a fresh file with web in front of it and nothing written.
func emptyLife(t *testing.T) *life {
	t.Helper()
	core := lifelogtest.New(t, today)
	browser, err := web.New(web.Options{CoreURL: core.URL})
	if err != nil {
		t.Fatal(err)
	}
	return &life{core: core, api: client.Remote(core.URL, "cli", ""), web: browser, hrefs: map[string]string{}}
}

// do runs the action of the resource at from.
func (l *life) do(t *testing.T, from, action string, values map[string]string) *siren.Entity {
	t.Helper()
	e := must(t)(l.api.Get(t.Context(), from))
	return must(t)(l.api.Do(t.Context(), act(t, e, action), values))
}

// toolLines are the tool lines that name each address, as an agent writes them.
func toolLines(refs ...string) string {
	var b strings.Builder
	for i, ref := range refs {
		fmt.Fprintf(&b, "- `tool%d` → [%s](%s)\n", i+1, ref, ref)
	}
	return b.String()
}

// richLife is a file with every kind of thing a page of the shell shows: days with words, a mood, wikilinks and tags;
// a page, a person, a file with a picture; two metrics, one of them a habit, with readings; the conversation of today
// with a tool line that names things, and the conversations of two earlier days.
func richLife(t *testing.T) *life {
	t.Helper()
	l := emptyLife(t)
	sam := l.do(t, "/", "create-person", map[string]string{"title": "Sam", "name": "Samuel Ortiz"})
	river := l.do(t, "/", "create-page", map[string]string{"title": "River walk", "body": "A long path along the water.\nA heron lives there. #birds Walked with [[Sam]]."})
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Walk", "daily": "1"})
	for _, d := range []string{"2031-05-10", "2031-05-11", "2031-05-12", "2031-05-13", today} {
		l.do(t, "/days/"+d, "capture", map[string]string{"text": "Synthetic words of " + d + ". Met [[Sam]] at the [[River walk]]. #birds"})
	}
	l.do(t, "/days/"+today, "capture", map[string]string{"mood": "4"})
	l.do(t, "/days/2030-05-14", "capture", map[string]string{"text": "A year ago.", "mood": "5"})
	l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": "2031-05-12", "value": "70.4"})
	weight := l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": "2031-05-13", "value": "71.2"})
	l.do(t, "/", "record", map[string]string{"metric": "Walk", "day": today, "value": "1"})
	l.do(t, "/", "record", map[string]string{"metric": "Walk", "day": "2031-05-13", "value": "1"})

	l.picture = "Lake.jpg"
	photo := filepath.Join(t.TempDir(), "IMG_0001.JPG")
	if err := os.WriteFile(photo, syntheticJPEG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	lake := l.do(t, "/", "add-file", map[string]string{"title": l.picture, "body": "Dawn at the lake.", "day": today, "original": photo})

	for _, day := range []string{"2031-05-10", "2031-05-13"} { // the days of the day bar
		l.do(t, "/", "create-page", map[string]string{"title": "Conversation " + day, "body": "[[" + day + "]]\n"})
	}
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "How did I **sleep**? See [[River walk]]."})
	conv := must(t)(l.api.Get(t.Context(), "/days/"+today+"/conversation"))
	page := must(t)(l.api.Get(t.Context(), href(conv, "page")))
	body, _ := page.Properties.(map[string]any)["body"].(string)
	refs := []string{"/days/" + today, href(river, "self"), href(sam, "self"), "/metrics/Weight", "/search?q=heron", "/pages/999999", "/days/today"}
	more := "\n\n" + toolLines(refs...) + "\n**lifelog** (09:30): You slept *well*.\n"
	must(t)(l.api.Do(t.Context(), act(t, page, "save-body"), map[string]string{"body": body + more}))

	l.hrefs["river"], l.hrefs["sam"] = href(river, "self"), href(sam, "self")
	l.hrefs["lake"] = href(lake, "self")
	l.hrefs["weightPage"] = pageOf(t, l, "Weight")
	l.hrefs["walkPage"] = pageOf(t, l, "Walk")
	l.hrefs["convPage"] = href(conv, "page")
	l.hrefs["reading"] = href(weight, "self")
	return l
}

// pageOf is the address of the page of the metric with the name: a series carries the properties of its page.
func pageOf(t *testing.T, l *life, name string) string {
	t.Helper()
	series := must(t)(l.api.Get(t.Context(), siren.MetricHref(name)))
	props, _ := series.Properties.(map[string]any)
	id, ok := props["id"].(json.Number)
	if !ok {
		t.Fatalf("the series %s has no page id: %v", name, props)
	}
	return "/pages/" + id.String()
}

// syntheticJPEG is a small JPEG with a gradient.
func syntheticJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 128, A: 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// ask is a GET of uri as a browser sends it, answered by h in the process.
func ask(h http.Handler, method, uri string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, uri, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
