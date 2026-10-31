package web_test

// The page of the settings of the core: the form of the model, the key and the number of the phone. The form of the
// settings of the browser itself, "This browser", is held with the catalog of web (catalog_test.go). This test has no
// connected app, so the page is the page of the core's form alone.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// settingsChatColumn is the chat column of a page: the popover #chat, up to the end of its form and its hint.
var settingsChatColumn = regexp.MustCompile(`(?s)<div class="chat compact" id="chat".*?</form>\s*<p class="hint">.*?</p>\s*</div>`)

// TestTheSettingsPageDrawsTheFieldsInOneForm: the model URL, the model id, the number of the phone and the key are fields of one
// form whose one loud button is Save. The key is a password input that holds its value, and the browser is told not to fill
// it. A save is a redirect back to /settings with the toast. The generic list of properties, the key among them, is not
// drawn.
func TestTheSettingsPageDrawsTheFieldsInOneForm(t *testing.T) {
	l := emptyLife(t)
	page := ask(l.web, http.MethodGet, "/settings").Body.String()
	form := regexp.MustCompile(`(?s)<form method="POST" action="/settings" autocomplete="off" data-action="settings">.*?</form>`).FindString(page)
	for _, want := range []string{`<input type="text" name="model_url" value="">`, `<input type="text" name="model" value="">`, `<input type="text" name="owner_number" value="">`,
		`<input type="password" name="model_key" value="" autocomplete="new-password">`, `<button class="primary">Save</button>`} {
		if !strings.Contains(form, want) {
			t.Errorf("the form of /settings lacks %s:\n%s", want, form)
		}
	}
	if n := strings.Count(settingsChatColumn.ReplaceAllString(page, ""), `class="primary"`); n != 1 {
		t.Errorf("/settings has %d loud buttons beside the chat column, want 1", n)
	}

	saved := postForm(l.web, "/settings", url.Values{"model_url": {"http://127.0.0.1:1234/v1"}, "model": {"synthetic-model"}, "model_key": {"synthetic-key"},
		"owner_number": {"40712345678"}}, "Referer", refererOnWeb("/settings"))
	if saved.Code != http.StatusSeeOther || !strings.HasPrefix(saved.Header().Get("Location"), "/settings?feedback=") {
		t.Fatalf("POST /settings: %d to %q, want 303 to /settings with a receipt: %.300s", saved.Code, saved.Header().Get("Location"), saved.Body.String())
	}
	page = ask(l.web, http.MethodGet, saved.Header().Get("Location")).Body.String()
	for _, want := range []string{`class="toast"`, "Settings saved.", `<input type="text" name="model_url" value="http://127.0.0.1:1234/v1">`,
		`<input type="text" name="model" value="synthetic-model">`, `<input type="text" name="owner_number" value="40712345678">`,
		`<input type="password" name="model_key" value="synthetic-key" autocomplete="new-password">`} {
		if !strings.Contains(page, want) {
			t.Errorf("/settings after a save lacks %s", want)
		}
	}
	if strings.Contains(page, "<h2>Properties</h2>") {
		t.Error("/settings lists its properties, the key among them")
	}
}
