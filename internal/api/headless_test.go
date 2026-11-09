package api_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"aminte/internal/api/apitest"
	"aminte/siren"
)

// The core is headless (docs/plans/161-the-browser-as-an-extension.md): it answers Siren JSON and bytes, to every
// client, whatever the client accepts. The browser is the extension aminte-web. The tests of this file hold what the
// core no longer does.

// TestEveryAcceptGetsTheSirenAnswer: a read and a write answer Siren JSON for a client that accepts HTML, as for any
// other client. A write answers the entity with 200 and its address in Location, not a redirect. The answer has no Vary
// and no rules of speculation: it has one representation.
func TestEveryAcceptGetsTheSirenAnswer(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	browser := map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8", "Sec-Fetch-Site": "same-origin", "Host": "localhost"}
	for _, path := range []string{"/", "/days/" + fixedToday, "/actions", "/browse", "/metrics", "/pages/999999"} {
		rec := request(h, "GET", path, "", browser)
		if rec.Header().Get("Content-Type") != "application/vnd.siren+json" || !strings.HasPrefix(strings.TrimSpace(rec.Body.String()), "{") {
			t.Errorf("GET %s: %d %q %.80s, want Siren JSON", path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		for _, name := range []string{"Vary", "Speculation-Rules"} {
			if v := rec.Header().Values(name); len(v) != 0 {
				t.Errorf("GET %s has the header %s %v", path, name, v)
			}
		}
	}
	rec := request(h, "POST", "/days/"+fixedToday+"/capture", url.Values{"text": {"A line."}}.Encode(), browser)
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != "/days/"+fixedToday || rec.Header().Get("Content-Type") != "application/vnd.siren+json" {
		t.Errorf("a write from a browser: %d, Location %q, %q; want 200, the day and Siren JSON", rec.Code, rec.Header().Get("Location"), rec.Header().Get("Content-Type"))
	}
}

// TestTheWriterIsApiUnlessTheClientNamesIt: a request with no header Aminte-Source writes as api, whatever it accepts.
// There is no default ui: the browser extension sends ui.
func TestTheWriterIsApiUnlessTheClientNamesIt(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	for day, header := range map[string]map[string]string{
		"2026-09-01": {},
		"2026-09-02": {"Accept": "text/html"},
		"2026-09-03": {siren.SourceHeader: "ui"},
		"2026-09-04": {"Accept": "text/html", siren.SourceHeader: "ui"},
	} {
		header["Host"] = "localhost"
		if rec := request(h, "POST", "/days/"+day+"/capture", "text=hello", header); rec.Code != http.StatusOK {
			t.Fatalf("capture on %s: %d %.200s", day, rec.Code, rec.Body.String())
		}
	}
	for day, want := range map[string]string{"2026-09-01": "api", "2026-09-02": "api", "2026-09-03": "ui", "2026-09-04": "ui"} {
		if got := scalar(t, s, "SELECT source FROM entities WHERE id = (SELECT entity_id FROM entity_names WHERE title = '"+day+"')"); got != want {
			t.Errorf("the page %s was written by %q, want %q", day, got, want)
		}
	}
}

// TestTheRoutesOfTheBrowserAreGone: the login, the lock, the files of the browser and the preview of a body are routes of
// the browser extension. The core answers none of them, and a bearer token reaches none of them either.
func TestTheRoutesOfTheBrowserAreGone(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	for _, tc := range []struct{ method, path string }{
		{"GET", "/login"}, {"POST", "/login"}, {"POST", "/logout"}, {"GET", "/locked"}, {"POST", "/lock"}, {"POST", "/unlock"}, {"POST", "/awake"},
		{"POST", "/render"}, {"GET", "/manifest.webmanifest"}, {"GET", "/assets/chat.js"}, {"GET", "/assets/lock.js"},
		{"GET", "/assets/icon.svg"}, {"GET", "/assets/wood.png"}, {"GET", "/assets/speculation.json"}, {"GET", "/assets/fonts/a.woff2"},
	} {
		rec := request(h, tc.method, tc.path, "x=1", map[string]string{"Host": "localhost"})
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: %d, want 404 or 405", tc.method, tc.path, rec.Code)
		}
	}
}

// TestACookieOpensNothingInTokenMode: the bearer token is the one credential. No cookie, whatever its name or value,
// takes the place of the header. (The login that set such a cookie is the browser extension's.)
func TestACookieOpensNothingInTokenMode(t *testing.T) {
	h, _ := tokenHandler(t)
	for _, name := range []string{"aminte_session", "aminte_token", "session"} {
		rec := request(h, "GET", "/", "", map[string]string{"Host": "127.0.0.1:7777", "Cookie": name + "=" + testToken})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("the cookie %s: %d, want 401", name, rec.Code)
		}
	}
}
