package web_test

// The page of the settings. The core keeps no settings, so web draws the page itself. The form of the settings of the
// browser, "This browser", is held with the catalog of web (catalog_test.go). This test has no connected app, so the page
// has the backup alone.

import (
	"net/http"
	"strings"
	"testing"
)

// TestTheSettingsPageIsDrawnByWeb: the page has its title, the link to the backup, and no form of the core. The core has
// no /settings, so the page does not ask for it: a GET of /settings through the API is a 404, and the page is still a 200.
func TestTheSettingsPageIsDrawnByWeb(t *testing.T) {
	l := emptyLife(t)
	page := pageWithStatus(t, l.web, "/settings", http.StatusOK)
	for _, want := range []string{"<h1>Settings</h1>", "<h2>Backup</h2>", `href="/actions?new=snapshot"`} {
		if !strings.Contains(page, want) {
			t.Errorf("/settings lacks %s:\n%s", want, mainPartOf(page))
		}
	}
	for _, gone := range []string{"model_key", "model_url", "owner_number", "<h2>Properties</h2>", "<h2>This browser</h2>"} {
		if strings.Contains(page, gone) {
			t.Errorf("/settings draws %s", gone)
		}
	}
	if _, err := l.api.Get(t.Context(), "/settings"); err == nil {
		t.Error("the core answers GET /settings, and it keeps no settings")
	}
}
