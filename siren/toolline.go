package siren

import "strings"

// Tool is a tool line of a conversation as a badge says it: the name of the tool, the words after it and, when the
// words are an address of this server, that address.
type Tool struct{ Name, Words, Href string }

// ToolLine reads a tool line as the conversation page writes it (API.md, conversation): “- `name` “,
// “- `name` → [href](href)“, “- `name` → href“ or “- `name` failed: code“. The words are what follows the name,
// without the arrow and the mark of a link; Href is the address after the arrow when it starts with a slash, else "".
// The API's refs of a turn and the badges of a browser's thread read the line through this one function. A line
// that is no such line is a name alone.
func ToolLine(line string) Tool {
	line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
	rest, ok := strings.CutPrefix(line, "`")
	if !ok {
		return Tool{Name: line}
	}
	name, words, ok := strings.Cut(rest, "`")
	if !ok {
		return Tool{Name: line}
	}
	words = strings.TrimSpace(words)
	after, arrow := strings.CutPrefix(words, "→")
	words = strings.TrimSpace(after)
	t := Tool{Name: name, Words: words}
	if !arrow {
		return t
	}
	if inner, ok := strings.CutPrefix(words, "["); ok {
		if label, href, ok := strings.Cut(inner, "]("); ok {
			t.Words = label
			t.Href, _, _ = strings.Cut(href, ")")
		}
	} else if f := strings.Fields(words); len(f) > 0 {
		t.Href = f[0]
	}
	if !strings.HasPrefix(t.Href, "/") {
		t.Href = ""
	}
	return t
}
