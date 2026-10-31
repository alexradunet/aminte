package web_test

// Every view of web renders as a whole page. The life is small and synthetic, made through the API of the real core.
// The rules of the pages that all views share are here: both sheets, the way to skip to the main part, the sections of the
// rail, the footer, the search dialog, no script but chat.js, no generated id, no id twice, and a datalist for each list.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/lifelogtest"
)

// chatScriptTag is the one script element that a page may carry.
const chatScriptTag = `<script src="/assets/chat.js" defer></script>`

// lifeStartingOn is a fresh file whose clock stands at the start of day, with web in front of it and a browser that has
// logged in. emptyLife is the same for the day of the other tests of web.
func lifeStartingOn(t *testing.T, day string) *life {
	t.Helper()
	core := lifelogtest.New(t, day)
	clk := newClock()
	state := t.TempDir()
	browser := newBrowser(t, core.URL, clk, state)
	cookie := login(t, browser)
	return &life{core: core, api: client.Remote(core.URL, "cli", ""), web: signedIn{browser, cookie}, browser: browser, cookie: cookie, clock: clk, state: state, hrefs: map[string]string{}}
}

// wholePageAt asks web for uri and fails the test unless the answer has the status and is a whole page.
func wholePageAt(t *testing.T, l *life, uri string, status int) string {
	t.Helper()
	rec := ask(l.web, http.MethodGet, uri)
	body := rec.Body.String()
	if rec.Code != status || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("GET %s: %d, want %d and a whole page: %.300s", uri, rec.Code, status, body)
	}
	return body
}

// TestEveryViewRenders draws every view of a small life as a browser gets it: one table of paths, each a whole page with
// its status. Every page carries what semantic HTML needs: both sheets under their markers, one main with a way to skip to
// it, the sections of the rail, at most one link of the whole rail marked, a footer, the search dialog, no script but
// chat.js, and no id generated for a template to find. The one exception is the id of a datalist, which the list of its
// input names: each list has its datalist on the page, and no id is repeated on a page. Lisbon is in a category, so the
// pages draw datalists. A row also names the words that its page shows.
func TestEveryViewRenders(t *testing.T) {
	ctx := t.Context()
	l := lifeStartingOn(t, "2026-09-30")
	day := l.do(t, "/", "capture", map[string]string{"text": "Ran with [[Sam]] #running, `[[not a link]]`", "mood": "4"})
	l.do(t, href(day, "page"), "save-body", map[string]string{"body": "Ran with [[Sam]] and [[Ana]] #running"})
	l.do(t, "/pages?title=Ana", "promote", map[string]string{"name": "Ana Popescu"})
	sam := must(t)(l.api.Get(ctx, "/pages?title=Sam"))
	lisbon := l.do(t, "/", "create-page", map[string]string{"title": "Lisbon"})
	l.do(t, href(lisbon, "self"), "file-pages", map[string]string{"category": "Cities"})
	l.do(t, "/", "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	l.do(t, "/", "register-metric", map[string]string{"name": "walk"})
	var reading string
	for _, d := range []string{"2026-09-01", "2026-09-15", "2026-09-28"} {
		reading = href(l.do(t, "/", "record", map[string]string{"metric": "weight", "day": d, "value": "71.2"}), "self")
	}
	draft := l.do(t, "/", "create-page", map[string]string{"title": "Draft", "body": "old words"})
	l.do(t, href(draft, "self"), "rename", map[string]string{"title": "Essay"})
	gone := l.do(t, "/", "create-page", map[string]string{"title": "Scratch"})
	l.do(t, href(gone, "self"), "tombstone-pages", nil)

	opens := func(title string) string { // the address that GET /pages?title= opens for the title: web relays the 303 of the core
		t.Helper()
		rec := ask(l.web, http.MethodGet, "/pages?title="+url.QueryEscape(title))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("/pages?title=%s: %d", title, rec.Code)
		}
		return rec.Header().Get("Location")
	}
	dayPage := href(day, "page")
	bodies := map[string]string{}
	ids, lists := regexp.MustCompile(` id="([^"]*)"`), regexp.MustCompile(` list="([^"]*)"`)
	datalists := 0
	for _, row := range []struct {
		path   string
		status int
		want   []string
	}{
		{"/actions", http.StatusOK, nil},
		{"/actions?new=snapshot", http.StatusOK, []string{`<details class="action" open><summary class="button">Take a snapshot`}}, // the rail's Snapshot link
		{"/days", http.StatusOK, []string{`<article class="day-card" aria-label="`, `<article class="day-card slim" aria-label="2026-09-28">`,
			`<a href="/days" aria-current="page">Days</a>`, `</span>Ana</a>`,
			`<form class="tiny" method="GET" action="/pages/random" data-action="random-page"><input type="hidden" name="kind" value="day"><button class="quiet">A random day</button></form>`}},
		{href(day, "self"), http.StatusOK, nil},
		{"/weeks/2026-W40", http.StatusOK, nil}, // a week draws its days and the form that makes its page: no id repeats
		{dayPage, http.StatusOK, []string{`name="version" value="`, `<a href="/pages?title=Sam" class="wikilink">Sam</a>`,
			`<a href="/pages?title=running" class="wikilink">#running</a>`}},
		{href(sam, "self"), http.StatusOK, nil},
		{opens("Ana"), http.StatusOK, []string{"Ana Popescu", "Backlinks", "mentioned in", `<div class="snippet">Ran with Sam and Ana #running</div>`}},
		{"/browse", http.StatusOK, []string{`placeholder="A page's title, or words of a body">`, `<span class="first">Essay</span>`}},
		{"/browse?kind=person", http.StatusOK, []string{`<form class="tiny" method="GET" action="/pages/random" data-action="random-page"><input type="hidden" name="kind" value="person"><button class="quiet">A random one</button></form>`}},
		{"/browse?deleted=1", http.StatusOK, []string{`<span class="first">Scratch</span>`}},
		{"/browse?ghosts=1", http.StatusOK, nil},
		{"/metrics", http.StatusOK, nil},
		{"/metrics/walk", http.StatusOK, nil},
		{"/metrics/weight", http.StatusOK, []string{`<svg class="chart"`, "71.2"}},
		{"/measurements/1", http.StatusOK, nil},
		{reading, http.StatusOK, []string{`action="/measurements/`, "Correct"}},
		{"/search", http.StatusOK, nil},
		{"/search?q=ran", http.StatusOK, []string{`<nav class="kinds chips" aria-label="Kind">`, `<a href="/search?q=ran" aria-current="page">All</a>`,
			`<section class="result-group" aria-label="Days"><div class="panel-head"><h2>Days</h2><a class="meta" href="/search?kind=day&amp;q=ran">Show only these</a>`,
			`<span class="hit-icon" aria-hidden="true">`}},
		{"/search?q=ran&kind=day", http.StatusOK, []string{`aria-current="page">Days</a>`, `aria-label="Pages by their words"`, `<input type="hidden" name="kind" value="day">`}},
		{"/integrity", http.StatusOK, []string{`action="/search/rebuild"`, `action="/links/resync"`}},
		{"/settings", http.StatusOK, []string{"<h1>Settings</h1>", `type="password"`}},
		{"/days/today/conversation", http.StatusOK, []string{"<h1>Chat</h1>", `/say"`, "Nothing is said on this day yet"}},
		{href(gone, "self"), http.StatusOK, []string{"Deleted", `action="` + href(gone, "self") + `/revive"`}},
		{"/years/2026", http.StatusOK, []string{`<a href="/years/2026" aria-current="page">Year</a>`}},
		{"/months/2026-09", http.StatusOK, []string{`<a href="/months/2026-09" aria-current="page">Month</a>`, `<a href="/days">Days</a>`}},
		{href(draft, "self"), http.StatusOK, nil},
		{opens("Essay"), http.StatusOK, nil},
		{"/pages/999999", http.StatusNotFound, nil},
	} {
		body := wholePageAt(t, l, row.path, row.status)
		bodies[row.path] = body
		for _, want := range append([]string{
			`<style data-sheet="base">`, `<style data-sheet="app">`, // the two sheets, the base one first
			`<a class="skip" href="#main">`, `<main id="main">`, `<nav aria-label="Sections">`, "<footer>",
			`<dialog class="palette" id="palette" closedby="any" aria-label="Search">`,
		}, row.want...) {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not carry %s", row.path, want)
			}
		}
		if strings.Contains(strings.ReplaceAll(body, chatScriptTag, ""), "<script") {
			t.Errorf("%s carries a script besides chat.js", row.path)
		}
		if strings.Contains(body, `id="f-`) {
			t.Errorf("%s carries a generated id: a page is semantic HTML", row.path)
		}
		seen := map[string]bool{}
		for _, m := range ids.FindAllStringSubmatch(body, -1) {
			if seen[m[1]] {
				t.Errorf("%s carries the id %q twice", row.path, m[1])
			}
			seen[m[1]] = true
		}
		for _, m := range lists.FindAllStringSubmatch(body, -1) {
			if !strings.Contains(body, `<datalist id="`+m[1]+`">`) {
				t.Errorf("%s has an input with list=%q and no datalist of that id", row.path, m[1])
			}
			datalists++
		}
		rail, _, _ := strings.Cut(body[strings.Index(body, `<aside class="rail">`)+1:], "</aside>")
		if n := strings.Count(rail, `aria-current="page"`); n > 1 {
			t.Errorf("%s marks %d links of the rail current, want one at most", row.path, n)
		}
	}
	// Plain labels: the screen has no technical word where a plain one serves.
	for _, c := range []struct{ path, want, not string }{
		{href(day, "self"), "<span>Entry</span>", "(CommonMark)"},
		{dayPage, "", "<dt>version</dt>"},
		{"/search", "<span>Words</span>", "required</small>"},
		{"/search", "How to search", "FTS5"},
		{"/search?q=ran", "<span>Words</span>", "required</small>"},
		{"/search?q=ran", "How to search", "FTS5"},
		{"/months/2026-09", "", "A day with a page is tinted"},
	} {
		if c.want != "" && !strings.Contains(bodies[c.path], c.want) {
			t.Errorf("%s lacks %q", c.path, c.want)
		}
		if strings.Contains(bodies[c.path], c.not) {
			t.Errorf("%s still carries %q", c.path, c.not)
		}
	}
	if strings.Contains(bodies[dayPage], `/promote"`) {
		t.Error("a day page shows a promote form the API does not offer")
	}
	if datalists == 0 {
		t.Error("no page draws a datalist: the check of list and datalist checks nothing")
	}
}

// TestNoPageShowsARawInstant: a page and a reading say their times in words. The exact instant stays only in an attribute
// (the datetime of a time element, a title, or the value of a form).
func TestNoPageShowsARawInstant(t *testing.T) {
	l := lifeStartingOn(t, today)
	garden := l.do(t, "/", "create-page", map[string]string{"title": "Garden project", "body": "Seeds."})
	l.do(t, "/", "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	reading := l.do(t, "/", "record", map[string]string{"metric": "weight", "day": today, "value": "71.1", "taken_at": today + "T06:00:00.000Z"})

	attributes := regexp.MustCompile(`(datetime|title|value)="[^"]*"`)
	instant := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)
	for _, path := range []string{href(garden, "self"), href(reading, "self")} {
		body := wholePageAt(t, l, path, http.StatusOK)
		if !instant.MatchString(body) {
			t.Errorf("%s holds no instant at all: the test proves nothing", path)
		}
		if raw := instant.FindString(attributes.ReplaceAllString(body, "")); raw != "" {
			t.Errorf("%s shows the raw instant %s", path, raw)
		}
		if !strings.Contains(body, "<time datetime=") {
			t.Errorf("%s says no time in words", path)
		}
	}
}
