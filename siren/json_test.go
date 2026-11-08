package siren_test

import (
	"bytes"
	"math"
	"testing"

	"aminte/siren"
)

// TestWriteJSONKeepsWhatABodySays: the one JSON writer of the API, the CLI and the MCP server. < > & stay as they
// are, the indent is the one given, and a newline ends the output. A value that JSON cannot carry is an error.
func TestWriteJSONKeepsWhatABodySays(t *testing.T) {
	v := map[string]any{"body": "a < b > c & d", "n": 1.5}
	for _, tc := range []struct{ indent, want string }{
		{"  ", "{\n  \"body\": \"a < b > c & d\",\n  \"n\": 1.5\n}\n"}, // the API and the CLI
		{"", "{\"body\":\"a < b > c & d\",\"n\":1.5}\n"},               // the MCP server, which then removes the newline
	} {
		var b bytes.Buffer
		if err := siren.WriteJSON(&b, v, tc.indent); err != nil {
			t.Fatal(err)
		}
		if b.String() != tc.want {
			t.Errorf("indent %q: %q, want %q", tc.indent, b.String(), tc.want)
		}
	}
	if err := siren.WriteJSON(&bytes.Buffer{}, math.NaN(), ""); err == nil {
		t.Error("a NaN was written")
	}
}
