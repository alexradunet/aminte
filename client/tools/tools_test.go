package tools

import (
	"fmt"
	"strings"
	"testing"

	"aminte/siren"
)

func TestToolsRefuseNormalizedDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []siren.Action
		want    string
	}{
		{"catalog collision", []siren.Action{{Name: "same-name"}, {Name: "same_name"}}, `"same_name"`},
		{"built-in get", []siren.Action{{Name: "get"}}, `"get"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Of(tc.actions, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Of() = %v, want a collision containing %s", err, tc.want)
			}
		})
	}
	list, err := Of([]siren.Action{{Name: "get", Owner: true}, {Name: "find"}, {Name: "import-find"}, {Name: "open-day", Method: "GET"}}, nil)
	if err != nil {
		t.Fatalf("owner-only duplicate and distinct public tools were rejected: %v", err)
	}
	if got, want := names(list), "find import_find open_day get"; got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}
}

func names(list []Tool) string {
	var out []string
	for _, x := range list {
		out = append(out, x.Name)
	}
	return strings.Join(out, " ")
}

func TestAnOwnerActionIsNeverAToolWhateverKeepSays(t *testing.T) {
	list, err := Of([]siren.Action{{Name: "snapshot", Owner: true}, {Name: "find"}},
		func(siren.Action) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got := names(list); got != "find get" {
		t.Fatalf("tools = %q, want find and get only", got)
	}
}

func TestKeepNarrowsTheList(t *testing.T) {
	actions := []siren.Action{
		{Name: "add-file", Fields: []siren.Field{{Name: "original", Type: "file"}}},
		{Name: "find", Fields: []siren.Field{{Name: "q", Type: "text"}}},
	}
	list, err := Of(actions, func(a siren.Action) bool {
		for _, f := range a.Fields {
			if f.Type == "file" {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(list); got != "find get" {
		t.Fatalf("tools = %q, want find and get only", got)
	}
}

func TestArgumentsFlattenToFormValues(t *testing.T) {
	got, err := Arguments([]byte(`{"day":"2031-05-14","mood":3,"value":72.50,"note":null}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"day": "2031-05-14", "mood": "3", "value": "72.50"}
	if len(got) != len(want) {
		t.Fatalf("arguments = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("arguments[%s] = %q, want %q", k, got[k], v)
		}
	}
	if _, err := Arguments([]byte(`{"list":[1]}`)); err == nil {
		t.Fatal("a list argument was accepted")
	}
	if _, err := Arguments([]byte(`{"day":`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if got, err := Arguments(nil); err != nil || len(got) != 0 {
		t.Fatalf("Arguments(nil) = %v, %v", got, err)
	}
}

// TestAFileFieldIsThePathOfALocalFile: a file field of an action (a recording, a picture) is a string in the
// tool's schema, the path of a local file, which the agent reads and sends as the CLI does
// (docs/plans/131-the-pareto-pass.md, part 1).
func TestAFileFieldIsThePathOfALocalFile(t *testing.T) {
	list, err := Of([]siren.Action{{Name: "capture", Method: "POST", Href: "/days/{day}/capture", Fields: []siren.Field{
		{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea"},
		{Name: "audio", Type: "file", Title: "A recording"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	props, _ := list[0].Schema["properties"].(map[string]any)
	audio, _ := props["audio"].(map[string]any)
	if audio["type"] != "string" || !strings.Contains(fmt.Sprint(audio["description"]), "path of a local file") || props["text"] == nil || props["day"] == nil {
		t.Errorf("the tool's properties: %v", props)
	}
}

// TestAPlaceholderIsAnExampleInTheDescription: the schema has no field for a hint, so the example of a field is the
// end of its description. It is words, not a value of the type ("Pick a metric"), so it is not a JSON Schema example.
func TestAPlaceholderIsAnExampleInTheDescription(t *testing.T) {
	list, err := Of([]siren.Action{{Name: "register-metric", Method: "POST", Href: "/metrics", Fields: []siren.Field{
		{Name: "name", Type: "text", Title: "Name", Placeholder: "Weight, Mood, Walk"}, {Name: "note", Type: "text"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	props, _ := list[0].Schema["properties"].(map[string]any)
	name, _ := props["name"].(map[string]any)
	if name["description"] != "Name Placeholder: Weight, Mood, Walk." {
		t.Errorf("the description of name: %v", name["description"])
	}
	if note, _ := props["note"].(map[string]any); note["description"] != nil || note["examples"] != nil {
		t.Errorf("a field with no placeholder has a description: %v", note)
	}
}
