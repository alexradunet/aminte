package web_test

// The pages of a form that the core refused, and the pages of a status that web draws for it. The core is the real one
// (amintetest). Three answers cannot come from it, so a fake core gives them: a busy file, a refusal of an owner-only
// action (web always writes as the owner), and a version of more than 2^53. A POST of a form gets a page, whatever the
// core accepts.

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"aminte/client"
	"aminte/siren"
)

// refererOnWeb is the Referer of a form that a browser sends from the page at path on the host of the tests.
func refererOnWeb(path string) string { return "http://example.com" + path }

// staleSaveOf saves the page at pageHref once. It returns the address of the action save-body of that page and the version
// that the page had before: that version is stale now.
func staleSaveOf(t *testing.T, l *life, pageHref string) (action, version string) {
	t.Helper()
	save := act(t, must(t)(l.api.Get(t.Context(), pageHref)), "save-body")
	field := save.Field("version")
	if field == nil {
		t.Fatalf("save-body of %s has no field version", pageHref)
	}
	version, _ = field.Value.(string)
	must(t)(l.api.Do(t.Context(), save, map[string]string{"body": "A second version."}))
	return save.Href, version
}

// TestAStaleSaveIsTheErrorPageWithTheTextThatTheFormSent: the core refuses a save of an old version with 409. The browser
// gets the page of the error in the shell: the status, an alert whose first words and tone the code picks, the text that
// the form sent (which nothing has saved, escaped), and a link Back to the page the form came from. The field back changes
// none of it and is not sent to the core.
func TestAStaleSaveIsTheErrorPageWithTheTextThatTheFormSent(t *testing.T) {
	l := richLife(t)
	self := l.hrefs["river"]
	action, version := staleSaveOf(t, l, self)
	rec := &bodies{next: l.core.Handler}
	h := webOver(t, rec)
	wants := []string{`<span class="date">409 · stale version</span>`, "<h1>Not saved</h1>", `class="rail"`,
		`<div class="alert" role="alert" tabindex="-1" autofocus><p><strong>Not saved. The page changed since you opened it.</strong> `,
		`aria-label="What you sent"`, "my &lt;long&gt; draft</textarea>", `<a href="` + self + `">Back</a>`}
	forms := map[string]url.Values{
		"no back":                   {"body": {"my <long> draft"}, "version": {version}},
		"back is the page":          {"body": {"my <long> draft"}, "version": {version}, "back": {self}},
		"back is a missing page":    {"body": {"my <long> draft"}, "version": {version}, "back": {"/pages/999999"}},
		"back is a redirect":        {"body": {"my <long> draft"}, "version": {version}, "back": {"/days/today"}},
		"back is another host":      {"body": {"my <long> draft"}, "version": {version}, "back": {"//evil.example/"}},
		"back is not a page at all": {"body": {"my <long> draft"}, "version": {version}, "back": {"not a path"}},
	}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			got := postForm(h, action, form, "Referer", refererOnWeb(self))
			body := got.Body.String()
			if got.Code != http.StatusConflict {
				t.Fatalf("status %d, want 409: %.400s", got.Code, body)
			}
			for _, want := range wants {
				if !strings.Contains(body, want) {
					t.Errorf("the page of the refusal lacks %s", want)
				}
			}
			if strings.Contains(body, `id="refused"`) { // no template draws this id now: the check keeps the old notice from coming back
				t.Error("the page of the refusal is the page the form came from, with a notice: the error page is the answer")
			}
		})
	}
	posts := 0
	for _, r := range rec.requests() {
		if r.method != http.MethodPost {
			continue
		}
		posts++
		sent, err := url.ParseQuery(r.body)
		if err != nil || sent.Has("back") || strings.Contains(r.uri, "back") {
			t.Errorf("the core got the field back, or a body that is no form: %s %s %s (%v)", r.method, r.uri, r.body, err)
		}
	}
	if posts != len(forms) {
		t.Errorf("%d writes reached the core, want %d, one for each form", posts, len(forms))
	}
	page := must(t)(l.api.Get(t.Context(), self))
	if props, _ := page.Properties.(map[string]any); props["body"] != "A second version." {
		t.Errorf("a refused save changed the page: body %q", props["body"])
	}
}

// TestAnErrorIsAnAlertWhoseTonePicksTheCode: a refusal of the owner's rules is a danger alert that starts with Refused., and
// a page that is not there is the message of the core alone, and the only thing on the page that takes the focus.
// A refusal of an owner-only action cannot come from the real core: web writes as the owner, so a fake core gives the
// 403.
func TestAnErrorIsAnAlertWhoseTonePicksTheCode(t *testing.T) {
	forbidden := fakeCore(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusForbidden, `{"class":["error"],"title":"Error","properties":{"status":403,"code":"forbidden","message":"snapshot is for the owner"}}`)
	})
	got := postForm(forbidden, "/thing", url.Values{"a": {"b"}})
	if want := `<div class="alert danger" role="alert" tabindex="-1" autofocus><p><strong>Refused.</strong> `; got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), want) {
		t.Errorf("a refusal: %d, no %s in\n%.600s", got.Code, want, got.Body.String())
	}

	l := emptyLife(t)
	missing := ask(l.web, http.MethodGet, "/pages/999999")
	body := missing.Body.String()
	if want := `<div class="alert" role="alert" tabindex="-1" autofocus><p>no page 999999</p></div>`; missing.Code != http.StatusNotFound || !strings.Contains(body, want) {
		t.Errorf("a missing page: %d, not an alert of its message alone, which takes the focus: %.400s", missing.Code, body)
	}
	if n := strings.Count(body, "autofocus"); n != 1 {
		t.Errorf("the error page has %d autofocus, want the alert's only", n)
	}
}

// TestAnEmptySaveIsRefusedAndThePageKeepsItsBody: the form marks the body required, but a client that does not check can send
// an empty one. The save is 422 with the alert and the words of the core, and the page keeps its body and its version.
func TestAnEmptySaveIsRefusedAndThePageKeepsItsBody(t *testing.T) {
	l := richLife(t)
	self := l.hrefs["river"]
	before := must(t)(l.api.Get(t.Context(), self))
	save := act(t, before, "save-body")
	version, _ := save.Field("version").Value.(string)
	got := postForm(l.web, save.Href, url.Values{"body": {""}, "version": {version}, "back": {self}}, "Referer", refererOnWeb(self))
	body := got.Body.String()
	if got.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "missing field body") {
		t.Errorf("an empty browser save: %d %.400s", got.Code, body)
	}
	was, now := before.Properties.(map[string]any), must(t)(l.api.Get(t.Context(), self)).Properties.(map[string]any)
	if now["body"] != was["body"] || now["version"] != was["version"] {
		t.Errorf("after an empty save the page has body %q and version %q, want %q and %q", now["body"], now["version"], was["body"], was["version"])
	}
}

// TestARefusedCaptureKeepsItsText: a capture that the core refuses (a mood out of range) is answered with the error page,
// which keeps the text so that nothing is lost, and links Back to the page it came from.
func TestARefusedCaptureKeepsItsText(t *testing.T) {
	l := emptyLife(t)
	got := postForm(l.web, "/days/"+today+"/capture", url.Values{"text": {"A note that must not be lost."}, "mood": {"9"}, "back": {"/"}},
		"Referer", refererOnWeb("/"))
	body := got.Body.String()
	if got.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `aria-label="What you sent"`) ||
		!strings.Contains(body, "A note that must not be lost.") || !strings.Contains(body, `<a href="/">Back</a>`) {
		t.Errorf("a refused capture: %d, want the error view that keeps the text: %.400s", got.Code, body)
	}
	if day := must(t)(l.api.Get(t.Context(), "/days/"+today)); href(day, "page") != "" {
		t.Error("a refused capture made the page of the day")
	}
}

// TestABusyCoreIsAPageThatSaysARetryIsSafe: a core whose file is busy answers 503 with the code busy and Retry-After. The
// browser gets a page with the same status and header, and the words of the view say that nothing was written and a retry
// is safe. The words do not come from the message of the core (the fake core sends other words). A busy file is not a fault
// of web, so the log has no line. The real core cannot be made busy from outside, so a fake core gives the answer.
func TestABusyCoreIsAPageThatSaysARetryIsSafe(t *testing.T) {
	logs := captureLog(t)
	busy := fakeCore(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		sirenAnswer(w, http.StatusServiceUnavailable, `{"class":["error"],"title":"Error","properties":{"status":503,"code":"busy","message":"the file is held by another writer"}}`, "Retry-After", "1")
	})
	got := postForm(busy, "/thing", url.Values{"title": {"Busy"}})
	body := got.Body.String()
	if got.Code != http.StatusServiceUnavailable || got.Header().Get("Retry-After") != "1" {
		t.Fatalf("a busy core: %d, Retry-After %q, want 503 and 1: %.300s", got.Code, got.Header().Get("Retry-After"), body)
	}
	for _, want := range []string{"retry is safe", "Nothing was written", "the file is held by another writer"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page of a busy core lacks %q: %.600s", want, body)
		}
	}
	if logs.Len() != 0 {
		t.Errorf("a busy file was logged as a fault: %q", logs.String())
	}
}

// TestAMultipartFormToAnActionWithoutFilesIsRefusedAndNothingIsStored: a multipart body to an action that takes no file is 415
// from the core, never read as an empty form (the fields would be lost in silence). Web streams the body, so the page of
// the refusal is the core's, with the first words of its code.
func TestAMultipartFormToAnActionWithoutFilesIsRefusedAndNothingIsStored(t *testing.T) {
	l := emptyLife(t)
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	if err := mw.WriteField("text", "Lost words"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/days/"+today+"/capture", &form)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	got := httptest.NewRecorder()
	l.web.ServeHTTP(got, req)
	if got.Code != http.StatusUnsupportedMediaType {
		t.Errorf("multipart POST to capture: %d, want 415: %.200s", got.Code, got.Body.String())
	}
	if !strings.Contains(got.Body.String(), "Not done.") {
		t.Errorf("the 415 page has no first words for its code: %.300s", got.Body.String())
	}
	if day := must(t)(l.api.Get(t.Context(), "/days/"+today)); href(day, "page") != "" {
		t.Error("the refused request made the page of the day")
	}
	if hits := must(t)(l.api.Get(t.Context(), "/search?q=Lost")); len(hits.Entities) != 0 {
		t.Errorf("the refused request wrote a page that has the words: %d hits", len(hits.Entities))
	}
}

// TestAScoreRangeOutOfOrderIsRefusedWithTheSentenceOfTheTrigger: a browser can send the range 5-1. The core answers 422 with the
// sentence of the trigger metrics_range_order (D6), the page shows it with a link Back, and nothing is written.
func TestAScoreRangeOutOfOrderIsRefusedWithTheSentenceOfTheTrigger(t *testing.T) {
	l := emptyLife(t)
	got := postForm(l.web, "/metrics", url.Values{"name": {"Pain"}, "unit": {"5-1"}}, "Referer", refererOnWeb("/metrics"))
	const trigger = "a scale names its range as its unit, lo-hi with lo less than hi, as in 1-5 (D6)"
	if body := got.Body.String(); got.Code != http.StatusUnprocessableEntity || !strings.Contains(body, trigger) || !strings.Contains(body, `<a href="/metrics">Back</a>`) {
		t.Errorf("a score with the range 5-1: %d %.600s", got.Code, body)
	}
	_, err := l.api.Get(t.Context(), "/pages?title=Pain")
	var refusal *client.Error
	if !errors.As(err, &refusal) || refusal.Status != http.StatusNotFound {
		t.Errorf("the page Pain after the refusal: %v, want a 404", err)
	}
}

// TestANewPersonWithTheTitleOfAPlainPageOffersToPromoteIt: the 409 of New person keeps "open it" and adds a link to the promote
// form of that page, with the fields typed. The link opens the form with the fields filled. A person that exists has
// nothing to promote, and neither has a period page or a category (the schema refuses to change their type).
func TestANewPersonWithTheTitleOfAPlainPageOffersToPromoteIt(t *testing.T) {
	l := emptyLife(t)
	sam := l.do(t, "/", "create-page", map[string]string{"title": "Sam"})
	ana := l.do(t, "/", "create-person", map[string]string{"title": "Ana"})
	l.do(t, "/", "create-page", map[string]string{"title": "2026-W43"})
	l.do(t, "/", "create-page", map[string]string{"title": "Run", "category": "Body"})
	refused := func(form url.Values) string {
		t.Helper()
		got := postForm(l.web, "/people", form)
		if got.Code != http.StatusConflict {
			t.Fatalf("POST /people %v: %d, want 409: %.300s", form, got.Code, got.Body.String())
		}
		return got.Body.String()
	}

	body := refused(url.Values{"title": {"Sam"}, "name": {"Samuel Ortiz"}, "birth_day": {"1980-02-03"}})
	for _, want := range []string{`open it</a>`, `href="` + href(sam, "self") + `?birth_day=1980-02-03&amp;name=Samuel&#43;Ortiz&amp;new=promote">make it a person, with what you typed</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the 409 of a person does not say %s:\n%.600s", want, body)
		}
	}
	// The link opens the promote form with the fields filled.
	opened := ask(l.web, http.MethodGet, href(sam, "self")+"?birth_day=1980-02-03&name=Samuel+Ortiz&new=promote")
	for _, want := range []string{`value="Samuel Ortiz"`, `value="1980-02-03"`} {
		if !strings.Contains(opened.Body.String(), want) {
			t.Errorf("the promote form is not filled: it lacks %s", want)
		}
	}
	// A page that is a person already has nothing to promote: only "open it".
	if body = refused(url.Values{"title": {"Ana"}}); strings.Contains(body, "make it a") || !strings.Contains(body, href(ana, "self")) {
		t.Errorf("the 409 of a taken person offers a promotion or no link to it:\n%.600s", body)
	}
	for _, title := range []string{"2026-W43", "Body"} {
		if body = refused(url.Values{"title": {title}}); strings.Contains(body, "make it a person") {
			t.Errorf("New person %s: the answer offers a promotion that the schema refuses:\n%.600s", title, body)
		}
	}
}

// TestARefusedReadingOfAnUnknownMetricLinksToTheFormOfTheMetric: the answer says the metric does not exist, in the words of the
// buttons, and links to the form that adds it with the name filled.
func TestARefusedReadingOfAnUnknownMetricLinksToTheFormOfTheMetric(t *testing.T) {
	l := emptyLife(t)
	got := postForm(l.web, "/measurements", url.Values{"metric": {"Ghost"}, "day": {"2026-10-12"}, "value": {"3"}})
	body := got.Body.String()
	if got.Code != http.StatusNotFound || !strings.Contains(body, `href="/metrics?name=Ghost&amp;new=register-metric"`) || !strings.Contains(body, "New metric") {
		t.Errorf("status %d, and the page has no link to the form of the metric:\n%.3000s", got.Code, body)
	}
}

// TestAVersionAboveTwoToThe53IsDrawnAndSentUnchanged: the version of a page is a text. A page whose version is more than
// 2^53, which a float64 cannot hold, is drawn with the whole number in the hidden field of its form, and web sends the
// number to the core as the form carried it, so the core compares the right version. A fake core gives the version: the
// real core reaches 2^53 after more saves than a test can make. The page is the page of the real core with its version
// changed. The stale versions are held by TestAStaleSaveIsTheErrorPageWithTheTextThatTheFormSent.
func TestAVersionAboveTwoToThe53IsDrawnAndSentUnchanged(t *testing.T) {
	const big = "9007199254740993" // 2^53 + 1
	l := richLife(t)
	self := l.hrefs["river"]
	page := must(t)(l.api.Get(t.Context(), self))
	page.Properties.(map[string]any)["version"] = big
	for i, a := range page.Actions {
		for j, f := range a.Fields {
			if a.Name == "save-body" && f.Name == "version" {
				page.Actions[i].Fields[j].Value = big
			}
		}
	}
	var entity bytes.Buffer
	if err := siren.WriteJSON(&entity, page, ""); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sent url.Values
	var parseErr error // reported by the goroutine of the test: the handler runs in the goroutine of the server
	core := http.NewServeMux()
	core.Handle("/", l.core.Handler)
	core.HandleFunc("GET "+self, func(w http.ResponseWriter, r *http.Request) { sirenAnswer(w, http.StatusOK, entity.String()) })
	core.HandleFunc("POST "+self+"/body", func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		mu.Lock()
		sent, parseErr = r.PostForm, err
		mu.Unlock()
		sirenAnswer(w, http.StatusOK, entity.String(), "Location", self)
	})
	h := webOver(t, core)

	if drawn := ask(h, http.MethodGet, self).Body.String(); !strings.Contains(drawn, `<input type="hidden" name="version" value="`+big+`">`) {
		t.Fatalf("the page does not draw the version %s in a hidden field", big)
	}
	form := drawnFormPostingTo(t, h, self, self+"/body")
	if got := form.Values.Get("version"); got != big {
		t.Fatalf("the browser sends the version %q, want %s", got, big)
	}
	form.Values.Set("body", "committed")
	if got := postForm(h, self+"/body", form.Values); got.Code != http.StatusSeeOther {
		t.Fatalf("the save: %d, want 303: %.300s", got.Code, got.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if parseErr != nil {
		t.Fatalf("the core could not read the form: %v", parseErr)
	}
	if sent.Get("version") != big || sent.Get("body") != "committed" || sent.Has("back") {
		t.Errorf("the core got %v, want the version %s unchanged, the new body and no back", sent, big)
	}
}
