package agent

import (
	"strings"
	"testing"

	"lifelog/siren"
)

// TestAnActionWithAFileFieldIsNotATool: the MCP server offers a file field as the path of a local file, which it
// reads and sends. This agent acts on text that any surface wrote to the page, so a path would let that text name
// the owner's token or the session store: an action with a file field is left out.
func TestAnActionWithAFileFieldIsNotATool(t *testing.T) {
	list, err := toolsOf([]siren.Action{
		{Name: "add-file", Method: "POST", Href: "/files", Fields: []siren.Field{{Name: "original", Type: "file", Title: "The file"}, {Name: "day", Type: "date"}}},
		{Name: "set-cover", Method: "POST", Href: "/pages/{id}/cover", Fields: []siren.Field{{Name: "id", In: "path", Required: true}, {Name: "picture", Type: "file"}}},
		{Name: "find", Method: "GET", Href: "/search", Fields: []siren.Field{{Name: "q", Type: "text"}}},
		{Name: "say", Owner: true, Method: "POST", Href: "/days/{day}/say", Fields: []siren.Field{{Name: "day", In: "path", Required: true}, {Name: "text", Type: "textarea", Required: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range list {
		names = append(names, tl.Name)
	}
	if got := strings.Join(names, " "); got != "find get" {
		t.Errorf("tools %q, want find and get only (no file field, no say)", got)
	}
}
