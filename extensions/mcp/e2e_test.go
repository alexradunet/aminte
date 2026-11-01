package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/client/tools"
	"lifelog/siren"
)

// The end-to-end test builds the real lifelog and lifelog-mcp into one folder and runs `lifelog mcp --agent test --db F`.
// That line holds the discovery of step 1 of plan 135 (lifelog finds lifelog-mcp beside itself), the temporary server
// of the extension contract (extensions/README.md) and the MCP server over HTTP. The tests of ./mcp hold the schemas,
// the arguments and the cancellation on a fake catalog; this test holds the surface against the real writer.
//
// The API does not show the source of a row on an entity, so this test cannot read agent:test back. The header that
// carries it is held by client/client_test.go and the sentence of the --source refusal is held below.

// built is the folder with lifelog, lifelog-mcp and a new life.db.
type built struct{ dir, lifelog, db string }

func build(t *testing.T) built {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to build lifelog: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "lifelog")); err != nil {
		t.Skipf("the lifelog sources are not at %s: %v", root, err)
	}
	b := built{dir: t.TempDir()}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	b.lifelog, b.db = filepath.Join(b.dir, "lifelog"+exe), filepath.Join(b.dir, "life.db")
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.lifelog, "./cmd/lifelog"},
		{"build", "-o", filepath.Join(b.dir, "lifelog-mcp"+exe), "./cmd/lifelog-mcp"},
	} {
		if out, err := exec.Command(goBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	if out, err := b.run("init", b.db); err != nil {
		t.Fatalf("lifelog init: %v\n%s", err, out)
	}
	return b
}

// env is the environment of a child: the inherited one without any variable that could point it at another file or
// server than the one of the test.
func env() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "LIFELOG_")
	})
}

func (b built) run(args ...string) ([]byte, error) {
	cmd := exec.Command(b.lifelog, args...)
	cmd.Env = env()
	return cmd.Output()
}

func TestTheMCPServerOnTheRealLifelog(t *testing.T) {
	b := build(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // a guard against a deadlock
	defer cancel()

	cmd := exec.Command(b.lifelog, "mcp", "--agent", "test", "--db", b.db)
	cmd.Env = env()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "e2e", Version: "0"}, nil).
		Connect(ctx, &mcpsdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			session.Close()
		}
	}()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tool := range listed.Tools {
		have[tool.Name] = true
	}
	// A floor of two names: the comparison below reads the full list from the catalog, and it would pass with an empty list on both sides.
	for _, name := range []string{"find", "capture"} {
		if !have[name] {
			t.Errorf("no tool %q", name)
		}
	}
	// The tools are exactly the actions that the catalog does not mark as the owner's, and get.
	out, err := b.run("actions", "--db", b.db)
	if err != nil {
		t.Fatalf("lifelog actions: %v", err)
	}
	var catalog []struct {
		Name  string `json:"name"`
		Owner bool   `json:"owner"`
	}
	if err := json.Unmarshal(out, &catalog); err != nil {
		t.Fatal(err)
	}
	wantTools := map[string]bool{"get": true}
	for _, a := range catalog {
		if !a.Owner {
			wantTools[tools.Name(a.Name)] = true
		} else if have[tools.Name(a.Name)] {
			t.Errorf("the owner's action %s is a tool", a.Name)
		}
	}
	for name := range wantTools {
		if !have[name] {
			t.Errorf("no tool %q", name)
		}
	}
	for name := range have {
		if !wantTools[name] {
			t.Errorf("the tool %q is no action of the catalog and not get", name)
		}
	}

	// capture writes a day page.
	const text = "Walked by the river, synthetic entry."
	capture, _ := call(t, ctx, session, "capture", map[string]any{"day": "2031-05-20", "text": text})
	if capture.Title != "2031-05-20" {
		t.Errorf("capture answered %q, want the day page 2031-05-20", capture.Title)
	}

	// save_body with a stale version is the error entity stale_version.
	page, _ := call(t, ctx, session, "create_page", map[string]any{"title": "Synthetic page", "body": "one"})
	id, version := fmt.Sprint(props(t, page)["id"]), fmt.Sprint(props(t, page)["version"])
	call(t, ctx, session, "save_body", map[string]any{"id": id, "body": "two", "version": version})
	text2 := callError(t, ctx, session, "save_body", map[string]any{"id": id, "body": "three", "version": version})
	if !strings.Contains(text2, "stale_version") {
		t.Errorf("a stale save answered %s, want stale_version", text2)
	}

	// edit_person sets the name of a person and removes a date; a field left out stays.
	person, _ := call(t, ctx, session, "create_person", map[string]any{"title": "Synthetic Sam", "name": "Sam One", "birth_day": "1980-02-03", "death_day": "2020-01-01"})
	edited, _ := call(t, ctx, session, "edit_person", map[string]any{"id": fmt.Sprint(props(t, person)["id"]), "name": "Sam Two", "death_day": ""})
	if got, _ := props(t, edited)["person"].(map[string]any); got["name"] != "Sam Two" || got["birth_day"] != "1980-02-03" || got["death_day"] != nil {
		t.Errorf("edit_person left the person %v, want Sam Two born 1980-02-03 with no death day", got)
	}

	// add_file sends a local file by its path.
	picture := filepath.Join(t.TempDir(), "synthetic.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(picture, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	file, _ := call(t, ctx, session, "add_file", map[string]any{"original": picture, "day": "2031-05-21"})
	if file.Title != "synthetic.png" {
		t.Errorf("add_file kept %q, want synthetic.png", file.Title)
	}
	self := fmt.Sprintf("/pages/%v", props(t, file)["id"])
	if kept, _ := call(t, ctx, session, "get", map[string]any{"href": self}); kept.Title != "synthetic.png" || kept.LinkByRel("preview") == nil {
		t.Errorf("get %s: %q with the links %v, want the file with its picture", self, kept.Title, kept.Links)
	}

	// get reads the catalog.
	if catalog, _ := call(t, ctx, session, "get", map[string]any{"href": "/actions"}); len(catalog.Actions) == 0 {
		t.Error("get /actions lists no action")
	}

	// The session writes as an agent: the owner's integrity check is refused to it (API.md, "integrity").
	if text := callError(t, ctx, session, "get", map[string]any{"href": "/integrity"}); !strings.Contains(text, "forbidden") {
		t.Errorf("get /integrity as an agent answered %s, want forbidden", text)
	}

	if err := session.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	closed = true

	// The file holds what the session wrote (read after the session, with the CLI).
	out, err = b.run("get", "/days/2031-05-20", "--db", b.db)
	if err != nil {
		t.Fatalf("lifelog get: %v", err)
	}
	var day siren.Entity
	if err := json.Unmarshal(out, &day); err != nil {
		t.Fatalf("lifelog get: %v: %s", err, out)
	}
	if day.Title != "2031-05-20" || !strings.Contains(string(out), text) {
		t.Errorf("the day page %q does not hold the text: %s", day.Title, out)
	}
}

func TestSourceIsRefusedOnTheLineOfMCP(t *testing.T) {
	b := build(t)
	cmd := exec.Command(b.lifelog, "mcp", "--source", "x", "--db", b.db)
	cmd.Env = env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() == 0 {
		t.Fatalf("lifelog mcp --source x: %v, want a non-zero status", err)
	}
	if want := "mcp writes as agent:NAME: name the agent with --agent, not --source"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}

// TestWithoutAServerTheMCPServerRefuses: lifelog-mcp never guesses an address, because 127.0.0.1:7777 may be the
// server of another file. Alone with no LIFELOG_URL and no --url it refuses, and so does `lifelog mcp` on a file that
// does not exist, which gives it no LIFELOG_URL.
func TestWithoutAServerTheMCPServerRefuses(t *testing.T) {
	b := build(t)
	const want = "no LIFELOG_URL and no --url"
	for _, line := range [][]string{
		{filepath.Join(b.dir, "lifelog-mcp"), "--agent", "x"},
		{b.lifelog, "mcp", "--agent", "x", "--db", filepath.Join(b.dir, "missing", "life.db")},
	} {
		cmd := exec.Command(line[0], line[1:]...)
		cmd.Env = env()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() == 0 {
			t.Fatalf("%v: %v, want a non-zero status", line[1:], err)
		}
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("%v: stderr %q, want %q", line[1:], stderr.String(), want)
		}
	}
}

func call(t *testing.T, ctx context.Context, s *mcpsdk.ClientSession, name string, args map[string]any) (*siren.Entity, string) {
	t.Helper()
	res, text := callRaw(t, ctx, s, name, args)
	if res.IsError {
		t.Fatalf("%s: error result: %s", name, text)
	}
	var e siren.Entity
	if err := json.Unmarshal([]byte(text), &e); err != nil {
		t.Fatalf("%s: not an entity: %v: %s", name, err, text)
	}
	return &e, text
}

func callError(t *testing.T, ctx context.Context, s *mcpsdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, text := callRaw(t, ctx, s, name, args)
	if !res.IsError {
		t.Fatalf("%s: want an error result, got %s", name, text)
	}
	return text
}

func callRaw(t *testing.T, ctx context.Context, s *mcpsdk.ClientSession, name string, args map[string]any) (*mcpsdk.CallToolResult, string) {
	t.Helper()
	res, err := s.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: %d contents", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("%s: %T content", name, res.Content[0])
	}
	return res, text.Text
}

func props(t *testing.T, e *siren.Entity) map[string]any {
	t.Helper()
	p, ok := e.Properties.(map[string]any)
	if !ok {
		t.Fatalf("%q has no properties", e.Title)
	}
	return p
}
