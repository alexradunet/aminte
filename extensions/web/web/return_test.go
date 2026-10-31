package web_test

// Where a write of the browser returns, and the toast that tells its result. A form names the page to return to (back).
// Web keeps the value only when it is a path of this server. The result of the write comes back once, as the toast at the
// top of the page where the browser lands. These are the tests of the browser of the core
// (internal/api/view_forms_test.go, days_test.go, feedback_test.go, feedback_internal_test.go, states_test.go,
// measurements_test.go and files_test.go), ported to web with their strings, against the real core. The rules for the
// value of back are in limits_test.go (backOf).
//
// Not ported: the part of TestABrowserFormReturnsToThePageThatSentIt about a JSON client, which never follows back. Web
// has no JSON client. The core now refuses the field back as an unknown field (plan 161, decision 4). A test of the
// core holds that.

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// receiptDigits is the shape of a receipt of the toast: 32 random bytes, in hexadecimal. It says nothing of the write.
var receiptDigits = regexp.MustCompile(`^[0-9a-f]{64}$`)

// fromTheMain is the page from its main element on, for the message of a test: the sheets before it are 100 KB of style.
func fromTheMain(page string) string {
	_, main, _ := strings.Cut(page, "<main")
	return "<main" + main
}

// locationWithoutReceipt is a Location without the receipt of the toast, so that a test can compare the page that the write
// returned to. It fails the test when the Location does not carry exactly one receipt, or when its query cannot be read.
// Anything else in the Location stays. So a Location with a private word in its query is not equal to the page.
func locationWithoutReceipt(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	if receipts := q["feedback"]; len(receipts) != 1 || !receiptDigits.MatchString(receipts[0]) {
		t.Errorf("Location %q does not carry one receipt of 64 hexadecimal digits", location)
	}
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// Core test: TestABrowserFormReturnsToThePageThatSentIt. A form that names a path on this server returns there with the
// feedback. Web ignores any other value, and the browser goes to what it changed. The table of
// TestBackOfKeepsAPathOnThisServerAndDropsEveryOtherValue says which values are a path on this server.
func TestAFormReturnsToThePageThatSentIt(t *testing.T) {
	l := emptyLife(t)
	const day = "2031-05-12"
	for _, tc := range []struct{ back, want string }{
		{"/days?limit=5&offset=10", "/days?limit=5&offset=10"},
		{"/metrics?order=page#row-1-3", "/metrics?order=page#row-1-3"}, // the fragment names where the page opens
		{"//evil.example/", "/days/" + day},
	} {
		rec := postForm(l.web, "/days/"+day+"/capture", url.Values{"text": {"a line"}, "back": {tc.back}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("back %q: %d", tc.back, rec.Code)
		}
		if got := locationWithoutReceipt(t, rec.Header().Get("Location")); got != tc.want {
			t.Errorf("back %q: Location %q, want %q", tc.back, got, tc.want)
		}
	}
	// The feedback of a write that has some goes to the page returned to, with its receipt.
	rec := postForm(l.web, "/pages", url.Values{"title": {"Notes"}, "body": {"[[bad/name]]"}, "back": {"/browse?kind=person"}})
	loc := rec.Header().Get("Location")
	if got := locationWithoutReceipt(t, loc); got != "/browse?kind=person" || !strings.Contains(browserPageAt(t, l.web, loc), "bad/name") {
		t.Errorf("feedback did not follow the return path: %q", loc)
	}
	if view, _ := must(t)(l.api.Get(t.Context(), "/days/"+day)).Properties.(map[string]any)["view"].([]any); len(view) == 0 {
		t.Error("the writes were not done")
	}
}

// Core test: TestBrowserMutationFeedback. A form with no back returns to what it wrote. The Location is the page of the
// new thing, with one receipt and nothing else in its query. The page tells the result in words, once.
func TestAFormWithNoBackReturnsToTheNewPageWithOneReceiptAndTellsItsResultOnce(t *testing.T) {
	l := emptyLife(t)
	target := l.do(t, "/", "create-page", map[string]string{"title": "Revived target"})
	must(t)(l.api.Do(t.Context(), act(t, target, "tombstone-pages"), nil))
	rec := postForm(l.web, "/pages", url.Values{"title": {"Feedback page"}, "body": {"[[Revived target]] [[bad/name]]"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	body := browserPageAt(t, l.web, location)
	for _, want := range []string{"Result", `Revived <a href="/pages?title=Revived+target">Revived target</a>.`, "Not linked, not a valid title: <code>[[bad/name]]</code>."} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in the redirected feedback", want)
		}
	}
	if strings.Contains(browserPageAt(t, l.web, location), "<h2>Result</h2>") { // a reload is a whole page with the status 200
		t.Error("receipt replayed")
	}
	page := must(t)(l.api.Get(t.Context(), "/pages?title=Feedback%20page"))
	if got := locationWithoutReceipt(t, location); got != href(page, "self") {
		t.Errorf("the Location must hold only the path of the new page and an opaque receipt: %s, the page is %s", location, href(page, "self"))
	}
}

// Core test: TestAFormThatReturnsHomeLandsOnToday. The write that returns to "/" goes home, which is the page of today.
// Its one-use receipt is read there, once.
func TestAFormThatReturnsHomeLandsOnTodayWithItsToastOnce(t *testing.T) {
	l := emptyLife(t)
	rec := postForm(l.web, "/days/"+today+"/capture", url.Values{"text": {"Walked with [[Dana Example]]."}, "back": {"/"}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/?feedback=") {
		t.Fatalf("capture: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	to := rec.Header().Get("Location")
	if body := browserPageAt(t, l.web, to); !strings.Contains(body, `class="toast"`) || !strings.Contains(body, "Dana Example") || !strings.Contains(body, `<h1>Wednesday</h1>`) {
		t.Errorf("the page of today does not carry the receipt of the capture:\n%.600s", fromTheMain(body))
	}
	if strings.Contains(browserPageAt(t, l.web, to), `class="toast"`) { // a reload is a whole page with the status 200
		t.Error("the receipt came back on a reload")
	}
}

// Core test: TestAWriteEndsInAToast. The result of a form comes back once, as the toast at the top of the page. The toast
// is an open dialog in the flow. Escape (closedby) and its own Close button close it with no script.
func TestAWriteEndsInAToastWithItsCloseButton(t *testing.T) {
	l := emptyLife(t)
	rec := postForm(l.web, "/days/"+today+"/capture", url.Values{"text": {"Walked with [[Dana Example]]."}, "back": {"/days/" + today}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/days/"+today+"?feedback=") {
		t.Fatalf("capture: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	body := browserPageAt(t, l.web, rec.Header().Get("Location"))
	if !strings.Contains(body, `<main id="main">`+"\n"+`<dialog open closedby="closerequest" class="toast" aria-label="Result"><h2>Result</h2>`) || !strings.Contains(body, "Dana Example") ||
		!strings.Contains(body, `<form method="dialog" class="tiny"><button class="close">Close</button></form></dialog>`) {
		t.Errorf("the capture's result is not the toast at the top of the page, with its Close")
	}
	if strings.Contains(browserPageAt(t, l.web, rec.Header().Get("Location")), `class="toast"`) { // a reload is a whole page with the status 200
		t.Error("the toast came back on a reload")
	}
}

// Core test: TestPhotoDaySyncJSONAndBrowserReceipt (the browser case). A file with a day, sent as a form of files, is
// redirected like any other write. The toast tells in words what happened in the page of its day: a link that came back
// to life, and the link of the body.
func TestAFileWithADayTellsWhatHappenedInItsDayPageInTheToast(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "create-page", map[string]string{"title": "2031-05-10", "body": "[[Old synthetic target]]"})
	old := must(t)(l.api.Get(t.Context(), "/pages?title=Old+synthetic+target"))
	must(t)(l.api.Do(t.Context(), act(t, old, "tombstone-pages"), nil))

	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	for _, field := range [][2]string{{"title", "Next synthetic.jpg"}, {"day", "2031-05-10"}, {"body", "[[File synthetic target]]"}} {
		if err := mw.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("original", "synthetic.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(syntheticJPEG(t)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/files", &form)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	l.web.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /files: %d %s", rec.Code, rec.Body.String())
	}
	if got := locationWithoutReceipt(t, rec.Header().Get("Location")); !strings.HasPrefix(got, "/pages/") || strings.Contains(got, "?") {
		t.Errorf("the file returned to %q, want the path of its page with a receipt", rec.Header().Get("Location"))
	}
	body := ask(l.web, http.MethodGet, rec.Header().Get("Location")).Body.String()
	for _, want := range []string{"In its day page: ", "Revived", "Old synthetic target", "File synthetic target"} {
		if !strings.Contains(body, want) {
			t.Errorf("the toast lacks %s:\n%.600s", want, fromTheMain(body))
		}
	}
}

// Core test: TestBrowserMutationFeedbackEscaping. The result of a write is text of the core. It can hold a word that an
// agent wrote. The toast draws it escaped: the page at the Location has Result and the words, and no live script. A
// receipt is the only way to a toast. So a query with a message in it draws nothing, and a second visit has no toast. The
// test looks for "<script>alert" and not for "<script>", because a page of the shell has the script chat.js.
func TestAToastIsEscapedAndOnlyAReceiptDrawsOne(t *testing.T) {
	const page = `{"class":["page"],"title":"Synthetic","properties":{"body":"","entity_type":"page"},"links":[{"rel":["self"],"href":"/pages/1","title":"Synthetic"}]`
	core := http.NewServeMux()
	core.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusOK, `{"class":["actions"],"actions":[]}`)
	})
	core.HandleFunc("GET /pages/1", func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusOK, page+`}`)
	})
	core.HandleFunc("POST /pages/1", func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusOK, page+`,"result":{"skipped":["<script>alert(1)</script>"]}}`, "Location", "/pages/1")
	})
	captureLog(t) // this core has no root, so web logs that it cannot read the shell: the page is drawn without it
	h := webOver(t, core)
	post := postForm(h, "/pages/1", url.Values{})
	location := post.Header().Get("Location")
	if post.Code != http.StatusSeeOther || !strings.HasPrefix(location, "/pages/1?feedback=") {
		t.Fatalf("the write: %d to %q, want a redirect with a receipt", post.Code, location)
	}
	body := ask(h, http.MethodGet, location).Body.String()
	if !strings.Contains(body, "Result") || strings.Contains(body, "<script>alert") || !strings.Contains(body, "alert(1)") {
		t.Fatalf("unsafe or missing feedback:\n%.600s", fromTheMain(body))
	}
	if strings.Contains(ask(h, http.MethodGet, "/pages/1?feedback=<script>&message=forged-success").Body.String(), "forged-success") {
		t.Fatal("query trusted as message")
	}
	if strings.Contains(ask(h, http.MethodGet, location).Body.String(), "<h2>Result</h2>") {
		t.Fatal("refresh replayed")
	}
}

// Core test: TestDeleteFromTheViewReturnsToThePage (the first block). A form of a metric sent with no back (a form opened
// from /actions) returns to the page of the metric. The page leads to its view, the series. The toast comes along.
func TestAWriteWithNoBackLandsOnThePageThenOnTheSeriesWithItsToast(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	self := pageOf(t, l, "Weight")
	mark := act(t, must(t)(l.api.Get(t.Context(), "/metrics/Weight")), "edit-metric").Href
	rec := postForm(l.web, mark, url.Values{"daily": {"1"}}, "Referer", "http://example.com/actions")
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, self+"?feedback=") {
		t.Fatalf("the mark sent with no back: %d to %q, want 303 to %s?feedback=…", rec.Code, loc, self)
	}
	rec = ask(l.web, http.MethodGet, loc)
	loc2 := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc2, "/metrics/Weight?feedback=") {
		t.Fatalf("the page in a browser: %d to %q, want 303 to the view with the receipt", rec.Code, loc2)
	}
	if view := ask(l.web, http.MethodGet, loc2).Body.String(); !strings.Contains(view, `class="toast"`) || !strings.Contains(view, "Asked for on every day.") {
		t.Error("the toast is lost across the redirect to the view")
	}
}
