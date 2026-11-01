package api

import (
	"regexp"
	"strings"
	"testing"
)

// TestTheFormsHintsAreInPlainWords: a POST row of the catalog that asks a person for a value has a hint, and a hint
// holds no status code and no code style, which are the words of the API text (Description). A row that asks for
// nothing (a button, such as revive or snapshot) needs no hint; a hint that it has is read like any other. The test
// reads every POST row, so a new action needs no edit here. save-body is the one exception: a client draws it as the
// editor of a page, not as a form with a note.
func TestTheFormsHintsAreInPlainWords(t *testing.T) {
	status := regexp.MustCompile(`\b[1-5][0-9][0-9]\b`)
	for _, s := range catalog {
		if s.Method != "POST" {
			continue
		}
		asks := false
		for _, f := range s.Fields {
			asks = asks || f.In != "path" && f.Type != "hidden"
		}
		if s.Hint == "" && asks && s.Name != "save-body" {
			t.Errorf("%s asks a person for a value and has no hint", s.Name)
		}
		if strings.Contains(s.Hint, "`") || status.MatchString(s.Hint) {
			t.Errorf("%s: the hint %q holds a status code or code style", s.Name, s.Hint)
		}
	}
}
