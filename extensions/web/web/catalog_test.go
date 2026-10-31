package web_test

// The settings of the browser and the catalog that serves them. The rules of plan 160 for the settings are held here: the
// PIN is 4 to 8 digits, the hash is in the file and the PIN is nowhere, an empty PIN
// keeps the stored one, lock_after needs a PIN, a refused save writes nothing, and the PIN is in no answer.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog-web/web"
	"lifelog/siren"
)

// saveSettings posts the form to the catalog of web, as the core forwards the action.
func saveSettings(l *life, form url.Values) *httptest.ResponseRecorder {
	return request(l.browser.Catalog(), http.MethodPost, "/settings", form.Encode(), map[string]string{"Lifelog-Source": "ui"})
}

// settingsFile is the file in the state folder of l, read as the keys it holds.
func settingsFile(t *testing.T, l *life) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(l.state, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("the settings file is not JSON: %v\n%s", err, raw)
	}
	return m
}

// TestTheCatalogOffersOneOwnerAction: GET /actions of the catalog is an entity of class actions with the action settings:
// owner-only, a POST, and the three fields, with the options of the two that have them and a password field for the PIN.
// The home of the app has a title and the values, and the home and the actions hold no PIN.
func TestTheCatalogOffersOneOwnerAction(t *testing.T) {
	l := emptyLife(t)
	catalog := l.browser.Catalog()
	var e siren.Entity
	rec := request(catalog, http.MethodGet, "/actions", "", nil)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &e) != nil || strings.Join(e.Class, " ") != "actions" || len(e.Actions) != 1 {
		t.Fatalf("GET /actions: %d %.300s", rec.Code, rec.Body.String())
	}
	a := e.Actions[0]
	if a.Name != "settings" || a.Method != "POST" || a.Href != "/settings" || !a.Owner || a.Danger {
		t.Errorf("the action: %+v", a)
	}
	want := map[string]struct {
		typ     string
		options []string
	}{
		"ground":     {"text", []string{"boards", "oak", "firelight", "embers"}},
		"lock_after": {"text", []string{"off", "5", "10", "15", "30", "60"}},
		"pin":        {"password", nil},
	}
	if len(a.Fields) != len(want) {
		t.Fatalf("the fields: %+v", a.Fields)
	}
	for _, f := range a.Fields {
		w, ok := want[f.Name]
		if !ok || f.Type != w.typ || strings.Join(f.Options, ",") != strings.Join(w.options, ",") || f.Value != nil || f.Required {
			t.Errorf("the field %+v, want type %s options %v and no value", f, w.typ, w.options)
		}
	}
	home := request(catalog, http.MethodGet, "/", "", nil)
	var h siren.Entity
	if home.Code != 200 || json.Unmarshal(home.Body.Bytes(), &h) != nil || h.Title != "This browser" {
		t.Fatalf("GET /: %d %.300s", home.Code, home.Body.String())
	}
	if props, _ := h.Properties.(map[string]any); props["ground"] != "boards" || props["lock_after"] != "off" {
		t.Errorf("the home: %v", h.Properties)
	}
}

// TestSettingsAreWrittenWithAHashAtomicallyAndWithMode0600: a save writes the file with the three keys of the settings file
// of the core, the PIN as a salted hash and nowhere in clear, with mode 0600 and no temporary file left. The file is read
// again by a new start of web.
func TestSettingsAreWrittenWithAHashAtomicallyAndWithMode0600(t *testing.T) {
	l := emptyLife(t)
	if rec := saveSettings(l, url.Values{"ground": {"oak"}, "lock_after": {"10"}, "pin": {"13579"}}); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %.300s", rec.Code, rec.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(l.state, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := settingsFile(t, l)
	if m["ground"] != "oak" || m["lock_after"] != "10" || !strings.HasPrefix(m["lock_pin_hash"], "pbkdf2-sha256$200000$") || len(m) != 3 {
		t.Errorf("the settings file: %s", raw)
	}
	if strings.Contains(string(raw), "13579") || strings.Contains(string(raw), `"pin"`) {
		t.Errorf("the settings file holds the PIN: %s", raw)
	}
	info, err := os.Stat(filepath.Join(l.state, "settings.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the settings file: %v, mode %v, want 0600", err, info.Mode())
	}
	entries, err := os.ReadDir(l.state)
	if err != nil || len(entries) != 1 {
		t.Errorf("the state folder holds %v after a save, want the settings file alone (%v)", entries, err)
	}
	// A new start reads the file: the lock is on and the PIN opens it.
	restarted, err := web.New(web.Options{CoreURL: l.core.URL, Token: testToken, StateDir: l.state, Now: l.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	cookie := l.cookie // the session of the first start: the new start does not know it, so the lock holds it shut
	locked := request(restarted, http.MethodGet, "/days/"+today, "", map[string]string{"Cookie": cookie, "Accept": "text/html"})
	if locked.Code != http.StatusSeeOther || !strings.HasPrefix(locked.Header().Get("Location"), "/locked") {
		t.Fatalf("a new start with the lock on: %d to %q", locked.Code, locked.Header().Get("Location"))
	}
	if rec := request(restarted, http.MethodPost, "/unlock", "pin=13579", map[string]string{"Cookie": cookie}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("the PIN that was saved: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if page := ask(signedIn{restarted, cookie}, http.MethodGet, "/days/"+today).Body.String(); !strings.Contains(page, `data-ground="oak"`) {
		t.Error("the ground that was saved is not on the page")
	}
}

// TestTheCatalogRefusesWhatItDoesNotTake: a field that the action does not list, a value outside the options and a PIN that
// is not 4 to 8 digits are 422, a lock_after with no PIN is 422, and nothing is written.
func TestTheCatalogRefusesWhatItDoesNotTake(t *testing.T) {
	l := emptyLife(t)
	for name, form := range map[string]url.Values{
		"an unknown field":      {"colour": {"red"}},
		"a ground that is none": {"ground": {"marble"}},
		"a lock_after of 7":     {"lock_after": {"7"}, "pin": {"1234"}},
		"letters in the PIN":    {"lock_after": {"10"}, "pin": {"abc1"}},
		"a PIN of 3 digits":     {"lock_after": {"10"}, "pin": {"123"}},
		"a PIN of 9 digits":     {"lock_after": {"10"}, "pin": {"123456789"}},
		"a PIN with a space":    {"lock_after": {"10"}, "pin": {"12 34"}},
		"a PIN of other digits": {"lock_after": {"10"}, "pin": {"١٢٣٤"}},
		"no PIN to lock with":   {"lock_after": {"10"}},
	} {
		rec := saveSettings(l, form)
		var e siren.Entity
		props := map[string]any{}
		if json.Unmarshal(rec.Body.Bytes(), &e) == nil {
			props, _ = e.Properties.(map[string]any)
		}
		if rec.Code != http.StatusUnprocessableEntity || props["code"] != "invalid" {
			t.Errorf("%s: %d %v, want 422 invalid", name, rec.Code, props)
		}
	}
	if _, err := os.Stat(filepath.Join(l.state, "settings.json")); err == nil {
		t.Error("a refused save wrote the file")
	}
	// A query is a field too: it is refused, not ignored.
	if rec := request(l.browser.Catalog(), http.MethodPost, "/settings?colour=red", "", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unknown field in the query: %d", rec.Code)
	}
	// A body larger than the field needs is refused.
	if rec := saveSettings(l, url.Values{"ground": {strings.Repeat("a", 5000)}}); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a body of 5000 bytes: %d, want 413", rec.Code)
	}
}

// TestEmptyFieldsClearAndAnEmptyPINKeepsTheStoredOne: a field that is left out stays; an empty one
// clears that setting; an empty PIN keeps the stored PIN; lock_after off keeps the hash; a new PIN replaces it.
func TestEmptyFieldsClearAndAnEmptyPINKeepsTheStoredOne(t *testing.T) {
	l := emptyLife(t)
	if rec := saveSettings(l, url.Values{"ground": {"embers"}, "lock_after": {"10"}, "pin": {"13579"}}); rec.Code != 200 {
		t.Fatalf("save: %d", rec.Code)
	}
	hash := settingsFile(t, l)["lock_pin_hash"]
	if rec := saveSettings(l, url.Values{"lock_after": {"30"}, "pin": {""}}); rec.Code != 200 { // the form never shows the PIN
		t.Fatalf("an empty PIN: %d", rec.Code)
	}
	if m := settingsFile(t, l); m["lock_after"] != "30" || m["lock_pin_hash"] != hash || m["ground"] != "embers" {
		t.Errorf("an empty PIN did not keep the stored one, or a field left out changed: %v", m)
	}
	if rec := saveSettings(l, url.Values{"ground": {""}}); rec.Code != 200 {
		t.Fatalf("an empty ground: %d", rec.Code)
	}
	if m := settingsFile(t, l); m["ground"] != "" || m["lock_after"] != "30" {
		t.Errorf("an empty ground did not clear it: %v", m)
	}
	if rec := saveSettings(l, url.Values{"lock_after": {"off"}, "pin": {"2468"}}); rec.Code != 200 {
		t.Fatalf("off: %d", rec.Code)
	}
	if m := settingsFile(t, l); m["lock_after"] != "" || m["lock_pin_hash"] == "" || m["lock_pin_hash"] == hash {
		t.Errorf("after off with a new PIN: %v", m)
	}
}

// TestThePINIsInNoAnswer: not the PIN, not its hash and not the name of the key of the hash, in the answer of the save, in the
// home or in the catalog.
func TestThePINIsInNoAnswer(t *testing.T) {
	l := emptyLife(t)
	answers := []*httptest.ResponseRecorder{saveSettings(l, url.Values{"lock_after": {"10"}, "pin": {"13579"}})}
	for _, path := range []string{"/", "/actions"} {
		answers = append(answers, request(l.browser.Catalog(), http.MethodGet, path, "", nil))
	}
	for i, rec := range answers {
		if rec.Code != 200 {
			t.Fatalf("answer %d: %d", i, rec.Code)
		}
		for _, leak := range []string{"13579", "pbkdf2", "lock_pin_hash", `"value": "pbkdf2`} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("answer %d holds %q", i, leak)
			}
		}
	}
	if props := answers[0].Body.String(); !strings.Contains(props, `"lock_after":"10"`) && !strings.Contains(props, `"lock_after": "10"`) {
		t.Errorf("the save does not say the minutes: %s", props)
	}
}

// TestAFailedWriteLeavesTheSettingsAsTheyWere: the copy in memory changes only after the file is written. A state folder that
// is gone makes the write fail with 500, the words of the cause stay in the log, and the lock is as it was.
func TestAFailedWriteLeavesTheSettingsAsTheyWere(t *testing.T) {
	logs := captureLog(t)
	f := newLockFixture(t, "")
	if err := os.RemoveAll(f.l.state); err != nil {
		t.Fatal(err)
	}
	rec := saveSettings(f.l, url.Values{"lock_after": {"5"}, "pin": {testPIN}})
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), f.l.state) {
		t.Errorf("a write that fails: %d %.200s, want 500 with no path", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "write the settings") {
		t.Errorf("the cause is not in the log: %q", logs.String())
	}
	if f.l.clock.add(time.Hour); f.isLocked() {
		t.Error("a write that failed turned the lock on")
	}
}

// bodyGround finds the ground in the body element of a page.
var bodyGround = regexp.MustCompile(`<body[^>]* data-ground="([^"]*)"`)

// TestManyReadersAndWritersSeeWholeSettings: the settings are read by each request and written by the catalog. Under the
// race detector, a reader never sees a half value.
func TestManyReadersAndWritersSeeWholeSettings(t *testing.T) {
	l := emptyLife(t)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			ground := []string{"boards", "oak", "firelight", "embers"}[i]
			for range 5 {
				if rec := saveSettings(l, url.Values{"ground": {ground}}); rec.Code != 200 {
					t.Errorf("save %s: %d", ground, rec.Code)
				}
			}
		})
		wg.Go(func() {
			for range 10 {
				m := bodyGround.FindAllStringSubmatch(ask(l.web, http.MethodGet, "/days/"+today).Body.String(), -1)
				if len(m) != 1 || !slices.Contains([]string{"boards", "oak", "firelight", "embers"}, m[0][1]) {
					t.Errorf("a page with the grounds %v", m)
				}
			}
		})
	}
	wg.Wait()
	if g := settingsFile(t, l)["ground"]; g == "" {
		t.Error("no ground in the file after the writes")
	}
}

// TestTheSettingsPageHasTheFormOfThisBrowser: with the app web connected to the core (`serve --with web`), the page of
// the settings has the form of the core and, under it, the form "This browser", which is the action web-settings of the
// catalog of the core, with the values that web keeps. A save through web goes to the core, which forwards it to the
// catalog with its owner rule, and the browser returns to the page with the toast. The core's form has no field of the
// browser: each setting of the browser is one field, in the form "This browser".
func TestTheSettingsPageHasTheFormOfThisBrowser(t *testing.T) {
	l := emptyLife(t)
	l.core.Connect(t, "web", l.browser.Catalog())
	page := ask(l.web, http.MethodGet, "/settings").Body.String()
	for _, want := range []string{"<h2>This browser</h2>", `action="/with/web/settings"`, `name="model"`, // the core's own form is still there
		`<input type="password" name="pin" value="" autocomplete="new-password">`, `<option selected>boards</option>`, `<option selected>off</option>`,
		`<input type="hidden" name="back" value="/settings">`} {
		if !strings.Contains(page, want) {
			t.Errorf("the settings page lacks %s", want)
		}
	}
	if strings.Count(page, "<h2>This browser</h2>") != 1 || strings.Contains(page, "pbkdf2") {
		t.Errorf("the settings page draws the form twice, or shows a hash")
	}
	for field, want := range map[string]int{`name="ground"`: 1, `name="lock_after"`: 1, `name="pin"`: 1, `name="lock_pin"`: 0} {
		if n := strings.Count(page, field); n != want {
			t.Errorf("the settings page has %s %d times, want %d: one way to set each setting of the browser", field, n, want)
		}
	}
	if n := strings.Count(page, `data-action="web-settings"`); n != 1 {
		t.Errorf("the action is drawn %d times, want once, in its own place and not in the menu", n)
	}

	// The form posts to web; web sends it to the core; the core sends it to the catalog.
	got := postForm(l.web, "/with/web/settings", url.Values{"ground": {"firelight"}, "lock_after": {"15"}, "pin": {"8642"}, "back": {"/settings"}})
	if got.Code != http.StatusSeeOther || !strings.HasPrefix(got.Header().Get("Location"), "/settings?feedback=") {
		t.Fatalf("the save: %d to %q\n%.300s", got.Code, got.Header().Get("Location"), got.Body.String())
	}
	after := ask(l.web, http.MethodGet, got.Header().Get("Location")).Body.String()
	for _, want := range []string{"Settings saved.", `data-ground="firelight"`, `<option selected>firelight</option>`, `<option selected>15</option>`,
		`<script src="/assets/lock.js" data-minutes="15" defer></script>`} {
		if !strings.Contains(after, want) {
			t.Errorf("the page after the save lacks %s", want)
		}
	}
	if m := settingsFile(t, l); m["ground"] != "firelight" || m["lock_after"] != "15" || m["lock_pin_hash"] == "" {
		t.Errorf("the file after the save: %v", m)
	}
	// The core's own owner rule: an agent is refused, and the file is as it was.
	agent := request(l.core.Handler, http.MethodPost, "/with/web/settings", url.Values{"ground": {"oak"}}.Encode(),
		map[string]string{"Lifelog-Source": "agent:test", "Accept": "application/vnd.siren+json"})
	if agent.Code != http.StatusForbidden || settingsFile(t, l)["ground"] != "firelight" {
		t.Errorf("an agent set the ground: %d, ground %q", agent.Code, settingsFile(t, l)["ground"])
	}
	// The catalog of the core lists it as web-settings, owner-only.
	catalog := request(l.core.Handler, http.MethodGet, "/actions", "", map[string]string{"Accept": "application/vnd.siren+json"}).Body.String()
	if !strings.Contains(catalog, `"name": "web-settings"`) || !strings.Contains(catalog, `"owner": true`) {
		t.Errorf("the catalog of the core lacks web-settings")
	}
}

// TestThisBrowserIsNoAppOfTheRail: the catalog of web is a connected app of the core, but its one form is on /settings.
// The rail lists no app "This browser", and the home of the app leads a browser to /settings, so the form is drawn in
// one place.
func TestThisBrowserIsNoAppOfTheRail(t *testing.T) {
	l := emptyLife(t)
	l.core.Connect(t, "web", l.browser.Catalog())
	page := ask(l.web, http.MethodGet, "/days/"+today).Body.String()
	if strings.Contains(page, `href="/with/web"`) || strings.Contains(page, "This browser") {
		t.Error("the rail lists the app web")
	}
	for _, path := range []string{"/with/web", "/with/web/"} {
		if rec := ask(l.web, http.MethodGet, path); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings" {
			t.Errorf("GET %s: %d %q, want 303 to /settings", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
