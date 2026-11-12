package web_test

// The offline mode (README.md, "Offline"). The service worker runs in a browser, so these tests hold the part of the
// server that the worker relies on: the page that it keeps, and each answer that it reads from a capture. The core is the
// real one.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"aminte-web/internal/formdoc"
	"aminte-web/view"
)

// workerHeaders are the headers of a request that the service worker sends itself: a fetch of a script of this origin,
// and not a navigation.
var workerHeaders = map[string]string{"Accept": "*/*", "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors"}

// TestTheOfflinePageNeedsNoSessionAndHoldsNoKey: the worker gets /offline in a browser that is logged out or locked, and
// it keeps the answer, so the page is not an answer of a session: it is the same with and without one, and a browser
// asks for it again each time. The page has no script and no import_key (one kept page would send one key for every
// entry). Its form is a capture form: each of its fields is a field of the action capture of the real catalog. The mark
// for the list of the worker is in the page one time, and sw.js names the same text.
func TestTheOfflinePageNeedsNoSessionAndHoldsNoKey(t *testing.T) {
	f := newLockFixture(t, "5")
	open := ask(f.h, http.MethodGet, "/offline")
	if open.Code != http.StatusOK || open.Header().Get("Cache-Control") != "no-cache" || open.Header().Get("Speculation-Rules") != "" {
		t.Fatalf("GET /offline with no session: %d, Cache-Control %q, Speculation-Rules %q; want 200, no-cache and no rules",
			open.Code, open.Header().Get("Cache-Control"), open.Header().Get("Speculation-Rules"))
	}
	page := open.Body.String()
	for _, banned := range []string{"<script", "import_key", secret} {
		if strings.Contains(page, banned) {
			t.Errorf("the offline page holds %q", banned)
		}
	}
	const mark = `<div id="outbox"></div>`
	if strings.Count(page, mark) != 1 || !strings.Contains(string(view.MustFile("sw.js")), mark) {
		t.Errorf("the mark %s is %d times in the page, want 1, and sw.js must name it", mark, strings.Count(page, mark))
	}
	if !strings.Contains(page, `<a href="/">Try again</a>`) {
		t.Errorf("the offline page has no link to try again")
	}

	form, ok := formdoc.Find(formdoc.Forms(page), func(form formdoc.Form) bool { return form.Method == http.MethodPost })
	if !ok || form.Action != "/days/today/capture" {
		t.Fatalf("the form of the offline page posts to %q, want /days/today/capture", form.Action)
	}
	capture := act(t, must(t)(f.l.api.Get(t.Context(), "/days/"+today)), "capture")
	if _, has := form.Values["text"]; !has {
		t.Errorf("the form of the offline page has no field text: %v", form.Values)
	}
	for name := range form.Values {
		if capture.Field(name) == nil {
			t.Errorf("the form of the offline page sends %q, which is not a field of capture", name)
		}
	}

	f.post("/lock", "", nil)
	if !f.isLocked() {
		t.Fatal("the session is not locked")
	}
	locked := f.browse("/offline", nil)
	if locked.Code != http.StatusOK || locked.Body.String() != page || locked.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("GET /offline in a locked browser: %d, Cache-Control %q; want the same page as with no session", locked.Code, locked.Header().Get("Cache-Control"))
	}
	if got := ask(f.h, http.MethodHead, "/offline"); got.Code != http.StatusOK || got.Body.Len() != 0 {
		t.Errorf("HEAD /offline: %d with %d bytes, want 200 and no body", got.Code, got.Body.Len())
	}

	// The Remove button of the worker posts to /offline, and the worker answers it. The server takes no POST there.
	if got := request(f.h, http.MethodPost, "/offline", "drop=k", workerHeaders); got.Code != http.StatusUnauthorized {
		t.Errorf("POST /offline with no session: %d, want 401", got.Code)
	}
	l := emptyLife(t)
	if got := postForm(l.web, "/offline", url.Values{"drop": {"k"}}); got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /offline with a session: %d, Allow %q; want 405 with GET, HEAD", got.Code, got.Header().Get("Allow"))
	}
}

// TestWhatTheWorkerReadsFromACapture: the worker decides by the status of a capture that it sends. A 303 is a write
// that the server did, the first time and each time after it, so the worker removes the entry. A 401 is a browser with
// no session or a locked one: the entry waits, nothing is written, and it passes after the PIN. A 422 is a refusal that
// will not pass. An entry that is sent late lands on its day with the time it was written, one time.
func TestWhatTheWorkerReadsFromACapture(t *testing.T) {
	f := newLockFixture(t, "5")
	const yesterday = "2031-05-13"
	to := "/days/" + yesterday + "/capture"
	entry := url.Values{"text": {"Written while the server slept"}, "time": {"21:40"}, "import_key": {"outbox-key-1"}}.Encode()
	bodyOfDay := func() string {
		t.Helper()
		day := must(t)(f.l.api.Get(t.Context(), "/days/"+yesterday))
		if href(day, "page") == "" {
			return ""
		}
		body, _ := must(t)(f.l.api.Get(t.Context(), href(day, "page"))).Properties.(map[string]any)["body"].(string)
		return body
	}

	for _, accept := range []string{"*/*", "text/html"} { // a POST with no session is 401 for a browser too, never a 303 to the login
		headers := map[string]string{"Accept": accept, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors"}
		if got := request(f.h, http.MethodPost, to, entry, headers); got.Code != http.StatusUnauthorized {
			t.Errorf("a capture with no session, Accept %s: %d, want 401", accept, got.Code)
		}
	}
	f.post("/lock", "", nil)
	if got := f.post(to, entry, workerHeaders); got.Code != http.StatusUnauthorized {
		t.Errorf("a capture in a locked session: %d, want 401", got.Code)
	}
	if body := bodyOfDay(); body != "" {
		t.Fatalf("the day after the refused sends: %q, want no page", body)
	}
	if got := f.post("/unlock", "pin="+testPIN, nil); got.Code != http.StatusSeeOther {
		t.Fatalf("unlock: %d", got.Code)
	}

	for n := range 2 { // the first send, and the same entry sent again
		if got := f.post(to, entry, workerHeaders); got.Code != http.StatusSeeOther {
			t.Fatalf("send %d of the entry: %d %.200s, want 303", n+1, got.Code, got.Body.String())
		}
	}
	if body, want := bodyOfDay(), "(21:40) Written while the server slept"; body != want {
		t.Errorf("the day page after two sends: %q, want %q", body, want)
	}

	bad := url.Values{"text": {"Never written"}, "time": {"25:00"}, "import_key": {"outbox-key-2"}}.Encode()
	if got := f.post(to, bad, workerHeaders); got.Code != http.StatusUnprocessableEntity {
		t.Errorf("a capture with a time that is not a clock: %d, want 422", got.Code)
	}
	if body := bodyOfDay(); strings.Contains(body, "Never written") {
		t.Errorf("the day page holds the refused entry: %q", body)
	}
}

// TestTheCaptureFormOfTheDayHidesTheTime: the field time of capture is for a sender that sends late. The form of the
// day sends it empty, so the server gives the entry its own time, and the owner sees no second field.
func TestTheCaptureFormOfTheDayHidesTheTime(t *testing.T) {
	l := emptyLife(t)
	page := pageAt(t, l.web, "/days/"+today)
	if !strings.Contains(page, `<input type="hidden" name="time" value="">`) || strings.Contains(page, `<input type="time"`) {
		t.Errorf("the capture form of the day does not hide an empty time")
	}
	form := drawnFormPostingTo(t, l.web, "/days/"+today, "/days/"+today+"/capture")
	form.Values.Set("text", "Written at the clock of the server")
	if got := postForm(l.web, form.Action, form.Values); got.Code != http.StatusSeeOther {
		t.Fatalf("the form of the day with an empty time: %d %.200s, want 303", got.Code, got.Body.String())
	}
	day := must(t)(l.api.Get(t.Context(), "/days/"+today))
	if body, _ := must(t)(l.api.Get(t.Context(), href(day, "page"))).Properties.(map[string]any)["body"].(string); body != "(00:00) Written at the clock of the server" {
		t.Errorf("the day page: %q, want the entry with the time of the server", body)
	}
}
