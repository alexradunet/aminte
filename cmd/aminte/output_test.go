package main

import (
	"bytes"
	"errors"
	"io"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"aminte/siren"
)

// TestHumanWritesAReadableSummary: --human writes the title and the classes, the result of a write, the properties in
// the order of their names (a text of more lines indented below its name, an empty text left out, any other value as
// JSON), then the embedded links, the links and the actions. The expected texts are written by hand.
func TestHumanWritesAReadableSummary(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    *siren.Entity
		want string
	}{
		{"a title only", &siren.Entity{Title: "Ana"}, "# Ana  ()\n"},
		{"properties that are not an object", &siren.Entity{Title: "Ana", Class: []string{"page"}, Properties: []int{1}},
			"# Ana  (page)\n"},
		{"every part", &siren.Entity{
			Class:  []string{"page", "file"},
			Title:  "Lakeside",
			Result: map[string]any{"linked": []string{"Sam"}},
			Properties: map[string]any{
				"title": "Lakeside", "body": "Walked with [[Sam]].\nCold water.", "kind": "", "id": 12, "tags": []string{"walk"},
			},
			Entities: []siren.Link{{Rel: []string{"backlink"}, Title: "2031-05-14", Href: "/pages/3"}},
			Links:    []siren.Link{{Rel: []string{"self"}, Href: "/pages/12"}, {Rel: []string{"collection", "up"}, Href: "/pages"}},
			Actions:  []siren.Action{{Name: "save-body", Method: "POST", Href: "/pages/12/body"}},
		}, `# Lakeside  (page file)
result: {"linked":["Sam"]}
body:
    Walked with [[Sam]].
    Cold water.
id: 12
tags: ["walk"]
title: Lakeside
  → backlink     2031-05-14                   /pages/3
  ↗ self         /pages/12
  ↗ collection up /pages
  ! save-body      POST /pages/12/body
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := human(&b, tc.e); err != nil {
				t.Fatal(err)
			}
			if b.String() != tc.want {
				t.Errorf("human:\n%s\nwant:\n%s", b.String(), tc.want)
			}
		})
	}
}

// errFull is the error of failWriter.
var errFull = errors.New("synthetic: the disk is full")

// failWriter is a stdout that refuses every write, as a full disk or a closed pipe does.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errFull }

// TestACommandReportsAFailedWriteOfItsOutput: when the stdout of a command refuses a write, the command returns that
// error, so main writes it and exits 1, and the output is not lost in silence. The first line makes the life.db that
// the other lines read: init writes the file, then its line.
func TestACommandReportsAFailedWriteOfItsOutput(t *testing.T) {
	live := filepath.Join(t.TempDir(), "life.db")
	for _, argv := range [][]string{
		{"init", live},
		{"actions", "--db", live},
		{"actions", "--db", live, "--human"},
		{"day", "2031-04-11", "--db", live},
		{"day", "2031-04-11", "--db", live, "--human"},
	} {
		e, _, _ := testEnv(t)
		e.stdout = failWriter{}
		if err := runArgs(t.Context(), e, argv); !errors.Is(err, errFull) {
			t.Errorf("%v: %v, want the error of the write", argv, err)
		}
	}
}

// TestHumanReportsAValueThatJSONCannotWrite: --human writes a result and a property that is not a text as JSON; a value
// that JSON cannot write is an error, not an empty line.
func TestHumanReportsAValueThatJSONCannotWrite(t *testing.T) {
	for _, e := range []*siren.Entity{
		{Title: "a result", Result: math.NaN()},
		{Title: "a property", Properties: map[string]any{"value": math.Inf(1)}},
	} {
		if err := human(io.Discard, e); err == nil {
			t.Errorf("%s: no error", e.Title)
		}
	}
}

// TestActionsShowThePlaceholderAfterTheField: the human list of the catalog shows the example of a field after it.
func TestActionsShowThePlaceholderAfterTheField(t *testing.T) {
	live := filepath.Join(t.TempDir(), "life.db")
	for _, argv := range [][]string{{"init", live}, {"actions", "--db", live, "--human"}} {
		e, stdout, _ := testEnv(t)
		if err := runArgs(t.Context(), e, argv); err != nil {
			t.Fatal(err)
		}
		if argv[0] == "actions" && !strings.Contains(stdout.String(), `name ("Weight, Mood, Walk")`) {
			t.Errorf("the placeholder of name is not shown:\n%s", stdout.String())
		}
	}
}
