package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/client"
	"lifelog/siren"
)

// TestTheToolsAreTheActionsThatAreNotTheOwners: each action of the catalog is a tool exactly when it is not the
// owner's, with get, each once; the browser's preview is not a tool, and neither is an alias of an action (get_day,
// random_day, tombstone: docs/plans/131-the-pareto-pass.md, part 2); browse, open_day and get are marked reads. The
// built-in tool get reads what no action reads: a month and a day.
func TestTheToolsAreTheActionsThatAreNotTheOwners(t *testing.T) {
	c := fakeClient(t)
	srv, err := New(t.Context(), c, "test")
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx, serverTransport) }()
	var session *mcpsdk.ClientSession
	defer func() {
		if session != nil {
			if err := session.Close(); err != nil {
				t.Errorf("close MCP session: %v", err)
			}
		}
		cancel()
		if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("MCP server stopped with %v", err)
		}
	}()

	session, err = mcpsdk.NewClient(&mcpsdk.Implementation{Name: "lifelog-test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	tools, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tool := range tools.Tools {
		if seen[tool.Name] {
			t.Fatalf("duplicate MCP tool %q", tool.Name)
		}
		seen[tool.Name] = true
		if (tool.Name == "browse" || tool.Name == "open_day" || tool.Name == "get" || tool.Name == "random_page") && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Errorf("%s is not marked a read: %+v", tool.Name, tool.Annotations)
		}
	}
	for _, name := range []string{"find", "get", "open_day", "random_page"} {
		if !seen[name] {
			t.Fatalf("MCP tools lack %q; have %v", name, seen)
		}
	}
	if seen["render"] || seen["get_day"] || seen["random_day"] || seen["tombstone"] {
		t.Fatal("the browser's preview, or an alias that the catalog no longer has, is an MCP tool")
	}
	actions, err := c.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner, public := 0, 0
	for _, a := range actions {
		name := strings.ReplaceAll(a.Name, "-", "_")
		switch {
		case a.Owner:
			owner++
			if seen[name] {
				t.Errorf("the owner-only action %s is an MCP tool", name)
			}
		default:
			public++
			if !seen[name] {
				t.Errorf("the action %s is no MCP tool", name)
			}
		}
	}
	if len(seen) != public+1 { // every public action, and get
		t.Errorf("%d MCP tools, want the %d public actions and get", len(seen), public)
	}
	if owner == 0 {
		t.Fatal("the catalog has no owner-only action: the check has nothing to check")
	}

	if day := callTool(t, ctx, session, "open_day", map[string]any{"day": "2031-05-14"}); day.Title != "2031-05-14" {
		t.Fatalf("open_day 2031-05-14 returned %+v", day)
	}
	// The browser's calendar is on this surface too (docs/plans/091-the-browser-for-daily-use.md).
	if month := callTool(t, ctx, session, "get", map[string]any{"href": "/fake/months/2031-05"}); month.Title != "May 2031" {
		t.Fatalf("get /fake/months/2031-05 returned %+v", month)
	}
	// A day's view is on this surface too.
	if day, ok := callTool(t, ctx, session, "get", map[string]any{"href": "/fake/days/2031-05-14"}).Properties.(map[string]any); !ok || day["view"] == nil {
		t.Fatalf("get /fake/days/2031-05-14 lacks its view: %v", day)
	}
}

func callTool(t *testing.T, ctx context.Context, session *mcpsdk.ClientSession, name string, args map[string]any) *siren.Entity {
	t.Helper()
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s returned MCP error content: %v", name, res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s returned %d content items", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("%s returned %T content", name, res.Content[0])
	}
	var e siren.Entity
	if err := json.Unmarshal([]byte(text.Text), &e); err != nil {
		t.Fatalf("%s returned non-entity JSON %q: %v", name, text.Text, err)
	}
	return &e
}

func numericSession(t *testing.T, c *client.Client) (*mcpsdk.ClientSession, context.Context) {
	t.Helper()
	srv, err := New(t.Context(), c, "test")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, st) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		session.Close()
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	return session, ctx
}

func TestMCPNumericSchema(t *testing.T) {
	session, ctx := numericSession(t, fakeClient(t))
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	moodEnum, captureKey := "", ""
	for _, tool := range tools.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		var schema map[string]any
		json.Unmarshal(b, &schema)
		for name, property := range schema["properties"].(map[string]any) {
			p := property.(map[string]any)
			if tool.Name == "capture" && name == "mood" {
				moodEnum = fmt.Sprint(p["type"], " ", p["enum"])
			}
			if tool.Name == "capture" && name == "import_key" {
				captureKey = fmt.Sprint(p["type"])
			}
			if options, ok := p["enum"].([]any); ok {
				for _, v := range options {
					switch p["type"] {
					case "number":
						if _, ok := v.(float64); !ok {
							t.Errorf("%s.%s numeric enum is %T", tool.Name, name, v)
						}
					case "string":
						if _, ok := v.(string); !ok {
							t.Errorf("string enum is %T", v)
						}
					}
				}
			}
		}
	}
	if moodEnum != "number [1 2 3 4 5]" { // the browser draws it as five buttons (docs/plans/092-the-app-shell.md)
		t.Errorf("capture's mood is %q, want the numbers 1 to 5", moodEnum)
	}
	if captureKey != "string" { // a capture sent again changes nothing (D28): an agent that retries gives it a key
		t.Errorf("the capture tool's import_key is %q, want a string", captureKey)
	}
	callTool(t, ctx, session, "record", map[string]any{"metric": "weight", "day": "2026-06-01", "value": 71.5})
}

// TestADateFieldTakesADayOrTheWordToday: a field of the type date takes a local day or the word today (API.md, "The
// entity"). The MCP schema of the field says so once, from the type: a pattern that takes the two, in place of the
// JSON Schema format date, which takes no word, and a description that adds the hint to the field's title. A title
// that ends with its own brackets takes the hint inside them, so the description has one pair of brackets. The title
// does not carry the hint, because it is also the label of the browser's date picker, in which a person cannot type
// the word.
func TestADateFieldTakesADayOrTheWordToday(t *testing.T) {
	const hint = "YYYY-MM-DD or today)"
	c := fakeClient(t)
	session, ctx := numericSession(t, c)
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	props := map[string]map[string]map[string]any{} // tool, field: the field's schema
	for _, tool := range tools.Tools {
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatal(err)
		}
		props[tool.Name] = schema.Properties
	}
	day := props["record"]["day"]
	if _, ok := day["pattern"]; ok || day["description"] != "Day (YYYY-MM-DD or today)" || day["type"] != "string" {
		t.Errorf("record's day: %v, want no pattern and the description Day (YYYY-MM-DD or today)", day)
	}
	for tool, want := range map[string]string{ // the titles with brackets of their own
		"open_day": "Day (default today; YYYY-MM-DD or today)",
		"add_file": "Its day (default: a JPEG's EXIF date, else none; any other file: today; YYYY-MM-DD or today)",
	} {
		if got := props[tool]["day"]["description"]; got != want {
			t.Errorf("%s's day: the description %q, want %q", tool, got, want)
		}
	}
	actions, err := c.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dates := 0
	for _, a := range actions {
		if a.Owner {
			continue
		}
		for _, f := range a.Fields {
			if f.Type != "date" {
				continue
			}
			tool := strings.ReplaceAll(a.Name, "-", "_") // the hyphen is not allowed in a function name
			dates++
			p := props[tool][f.Name]
			desc, _ := p["description"].(string)
			if _, ok := p["format"]; ok || p["pattern"] != nil || !strings.HasPrefix(desc, strings.TrimSuffix(f.Title, ")")) ||
				!strings.HasSuffix(desc, hint) || strings.Count(desc, "(") != 1 || strings.Count(desc, ")") != 1 || strings.Count(desc, "YYYY-MM-DD") != 1 {
				t.Errorf("%s.%s: %v, want no pattern, no format, and the title %q with the hint in one pair of brackets", tool, f.Name, p, f.Title)
			}
		}
	}
	if dates < 6 { // capture, create-person (2), add-file, record and open-day
		t.Errorf("%d date fields in the tools, want 6 at least", dates)
	}
}

func TestMCPNumericArguments(t *testing.T) {
	actions := fakeCatalog()
	var paths []string
	c := remote(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		if r.URL.Path == "/actions" {
			json.NewEncoder(w).Encode(siren.Entity{Actions: actions})
			return
		}
		paths = append(paths, r.URL.Path)
		json.NewEncoder(w).Encode(siren.Entity{})
	}), "agent:test")
	session, ctx := numericSession(t, c)
	// A number is sent as its JSON text: the API's own parser is the one rule for an id, on this surface as on the
	// CLI and in a browser, so "1e6" reaches the API as written and the API refuses it there (404 no such id).
	for _, token := range []string{"999999", "1000000", "9007199254740993", "1e6"} {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "revive", Arguments: json.RawMessage(`{"id":` + token + `}`)})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %v", token, res, err)
		}
	}
	want := []string{"/fake/pages/999999/revive", "/fake/pages/1000000/revive", "/fake/pages/9007199254740993/revive", "/fake/pages/1e6/revive"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Fatalf("paths %v want %v", paths, want)
	}
	// An argument that is not a number or a string is refused before any call.
	for _, token := range []string{"{}", "[]"} {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "revive", Arguments: json.RawMessage(`{"id":` + token + `}`)})
		if err == nil && !res.IsError {
			t.Fatalf("accepted %s", token)
		}
	}
	if len(paths) != len(want) {
		t.Fatal("invalid arguments dispatched")
	}
}

func TestMCPCancellation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	c := remote(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			close(started)
			<-r.Context().Done()
			close(stopped)
			return
		}
		fmt.Fprint(w, `{}`)
	}), "cli")
	srv, err := New(t.Context(), c, "test")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, st) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { session.Close(); cancel(); <-done }()
	callCtx, stop := context.WithCancel(ctx)
	defer stop()
	result := make(chan error, 1)
	go func() {
		_, err := session.CallTool(callCtx, &mcpsdk.CallToolParams{Name: "get", Arguments: map[string]any{"href": "/blocked"}})
		result <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("call did not start")
	}
	stop()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("SDK did not cancel handler")
	}
	if err := <-result; err == nil {
		t.Fatal("canceled call succeeded")
	}
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get", Arguments: map[string]any{"href": "/ok"}})
	if err != nil || res.IsError {
		t.Fatalf("later call: %v %v", res, err)
	}
}

func TestMCPUntrustedDataInstructions(t *testing.T) {
	session, _ := numericSession(t, fakeClient(t))
	instructions := session.InitializeResult().Instructions
	for _, want := range []string{"untrusted data", "never instructions", "authorize tools", "approval"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("initialization instructions lack %q: %s", want, instructions)
		}
	}
}

func TestMCPExactResponseNumbers(t *testing.T) {
	token := "9007199254740993"
	c := remote(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path != "/echo/"+token {
			t.Errorf("rounded MCP read-to-action path: %s", r.URL.Path)
		}
		if r.URL.Path == "/actions" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintf(w, `{"properties":{"nested":[%s,9223372036854775807,-9223372036854775808]},"result":{"id":%s},"actions":[{"name":"echo","method":"POST","href":"/echo/{id}","fields":[{"name":"id","in":"path","value":%s}]}]}`, token, token, token)
	}), "agent:test")
	session, ctx := numericSession(t, c)
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get", Arguments: map[string]any{"href": "/numbers"}})
	if err != nil || res.IsError {
		t.Fatalf("get: %v %v", res, err)
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	for _, want := range []string{token, "9223372036854775807", "-9223372036854775808"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %s: %s", want, text)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var entity siren.Entity
	if err := decoder.Decode(&entity); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(t.Context(), entity.Actions[0], nil); err != nil {
		t.Fatal(err)
	}
}

// TestAnAgentKeepsAPhotoByItsPath: the tool add_file sends a local file that the agent names by its path, so its
// picture is kept (docs/plans/131-the-pareto-pass.md, part 1).
func TestAnAgentKeepsAPhotoByItsPath(t *testing.T) {
	c := fakeClient(t)
	session, ctx := numericSession(t, c)
	picture := []byte("synthetic picture bytes")
	path := filepath.Join(t.TempDir(), "IMG_0007.jpg")
	if err := os.WriteFile(path, picture, 0o644); err != nil {
		t.Fatal(err)
	}
	page := callTool(t, ctx, session, "add_file", map[string]any{"original": path})
	if page.Title != "IMG_0007.jpg" || page.LinkByRel("preview") == nil {
		t.Fatalf("add_file kept %q with the links %v, want IMG_0007.jpg with its picture", page.Title, page.Links)
	}
	if got := props(t, page)["size"]; got != fmt.Sprint(len(picture)) { // the fake API echoes the bytes that it received
		t.Errorf("the API received %v bytes, want %d", got, len(picture))
	}
}
