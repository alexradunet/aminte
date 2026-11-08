package api

import (
	"strings"

	"aminte/siren"
)

// Editing (docs/plans/091-the-browser-for-daily-use.md): an editor as tall as its body.

// saveBody is the save-body of a page, its body and version filled. Its textarea is as tall as the body, 12 to 40
// lines, so a long page is edited without a scroll inside a small box.
func saveBodyAction(id int64, body, version string) siren.Action {
	a := action("save-body", idParam(id), map[string]any{"body": body, "version": version})
	a.Field("body").Rows = min(max(strings.Count(body, "\n")+2, 12), 40)
	return a
}
