package api_test

import (
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheEditorIsTallAsItsBody: a page's save-body textarea grows with its body, 12 to 40 lines, and the catalog marks
// the body required: save-body refuses an empty body.
func TestTheEditorIsTallAsItsBody(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, tc := range []struct {
		lines, rows int
	}{{1, 12}, {20, 21}, {38, 39}, {39, 40}, {200, 40}} {
		body := strings.Repeat("line\n", tc.lines-1) + "line"
		p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Lines " + strings.Repeat("x", tc.lines), "body": body}))
		save := apitest.Action(t, p, "save-body")
		f := save.Field("body")
		if f == nil || f.Rows != tc.rows || !f.Required {
			t.Errorf("%d lines: the field body is %+v, want rows %d and required", tc.lines, f, tc.rows)
		}
	}
}

// TestAnEmptySaveIsRefused: save-body marks the body required, but a client that does not check the form can still send
// an empty body. The save is refused 422 with the words missing field body, and the page keeps its body and its
// version.
func TestAnEmptySaveIsRefused(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := apitest.Action(t, p, "save-body")
	version, _ := save.Field("version").Value.(string)
	_, err := c.Do(t.Context(), save, map[string]string{"body": "", "version": version})
	if code, status := codeOf(t, err); code != "invalid" || status != 422 || !strings.Contains(err.Error(), "missing field body") {
		t.Errorf("an empty save: code %q status %d %v, want invalid 422 with missing field body", code, status, err)
	}
	after := apitest.Must(t)(c.Get(t.Context(), apitest.Href(p, "self"))).Properties.(map[string]any)
	if after["body"] != "v1" || after["version"] != version {
		t.Errorf("after an empty save the page has body %q and version %q, want %q and %q", after["body"], after["version"], "v1", version)
	}
}
