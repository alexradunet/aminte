package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"lifelog/app"
	"lifelog/client"
	"lifelog/siren"
)

// The end-to-end test builds the real lifelog and lifelog-agent into one folder and runs the line of the live unit on a
// new life.db: `lifelog serve --token-file F --with agent`, with a fake model server. It then says as the owner (`lifelog do
// say`) and reads the conversation page back through the API. It holds what the tests of ./agent cannot: the program,
// its environment, the discovery of the extension, the owner rule of the real core (a tool call of an owner action is
// refused), and the stop by a signal. The turns, the page and the model are held in detail by the tests of ./agent.
//
// Every wait has a deadline and polls a fact (a line of the log, a turn of the page, an exit). None sleeps a fixed time.

// wait is a guard against a deadlock, not a measure of speed.
const wait = 2 * time.Minute

// ownerToken is the token of the test: 40 characters of no meaning. It is the bearer token of the core.
const ownerToken = "e2e-token-e2e-token-e2e-token-e2e-token"

// modelKey is a key of no meaning. It must reach the model server and no other place.
const modelKey = "synthetic-key-e2e"

// lockedBuffer is the stderr of the server: the process writes it and the test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// bins is the folder with lifelog and lifelog-agent.
type bins struct{ dir, lifelog, agent string }

func build(t *testing.T) bins {
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
	b := bins{dir: t.TempDir()}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	b.lifelog, b.agent = filepath.Join(b.dir, "lifelog"+exe), filepath.Join(b.dir, "lifelog-agent"+exe)
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.lifelog, "./cmd/lifelog"},
		{"build", "-o", b.agent, "./cmd/lifelog-agent"},
	} {
		if out, err := exec.Command(goBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return b
}

// cleanEnv is the inherited environment without any variable that could point a child at another file, server or model
// than the ones of the test, and with the folders of the user in a folder of the test.
func cleanEnv(t *testing.T, extra ...string) []string {
	home := t.TempDir()
	e := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "LIFELOG_") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=") || strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "APPDATA=")
	})
	return append(e, append([]string{"XDG_CONFIG_HOME=" + home, "HOME=" + home, "APPDATA=" + home}, extra...)...)
}

// stack is a running `lifelog serve --token-file F --with agent`.
type stack struct {
	t      *testing.T
	bin    string
	cmd    *exec.Cmd
	stderr *lockedBuffer
	exited chan error
	core   string // http://127.0.0.1:PORT
	stoped bool
}

// startStack makes a new life.db and a token file, starts the server as the unit does and returns when the child has
// started.
func startStack(t *testing.T, b bins) *stack {
	t.Helper()
	work := t.TempDir()
	db, tokenFile := filepath.Join(work, "life.db"), filepath.Join(work, "token")
	if out, err := exec.Command(b.lifelog, "init", db).CombinedOutput(); err != nil {
		t.Fatalf("lifelog init: %v\n%s", err, out)
	}
	if err := os.WriteFile(tokenFile, []byte(ownerToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &stack{t: t, bin: b.lifelog, stderr: new(lockedBuffer), exited: make(chan error, 1)}
	s.cmd = exec.Command(b.lifelog, "--db", db, "serve", "--addr", "127.0.0.1:0", "--token-file", tokenFile, "--with", "agent")
	s.cmd.Env = cleanEnv(t)
	s.cmd.Stderr = s.stderr
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exited <- s.cmd.Wait() }()
	t.Cleanup(s.stop)
	line := s.waitFor("lifelog: serving ")
	_, rest, _ := strings.Cut(line, " on http://")
	addr, _, _ := strings.Cut(rest, " ")
	s.core = "http://" + addr
	s.waitFor("lifelog: with agent: started (pid ")
	return s
}

// waitFor waits until the log of the server holds the text and returns the line that holds it. It reads the log, never
// the clock.
func (s *stack) waitFor(text string) string {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		for l := range strings.SplitSeq(s.stderr.String(), "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		select {
		case err := <-s.exited:
			s.exited <- err
			s.t.Fatalf("the server ended before %q: %v\n%s", text, err, s.stderr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	s.t.Fatalf("no %q in the log after %s:\n%s", text, wait, s.stderr)
	return ""
}

// stop ends the server at the end of the test, when the test did not stop it. A server that does not end is killed.
func (s *stack) stop() {
	if s.stoped {
		return
	}
	select {
	case err := <-s.exited:
		s.exited <- err
		return
	default:
	}
	_ = s.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-s.exited:
	case <-time.After(wait):
		_ = s.cmd.Process.Kill()
		s.t.Error("the server did not stop")
	}
}

// lifelogCLI runs `lifelog --url CORE args...` as the owner does at a shell: with the token in $LIFELOG_TOKEN and no other
// LIFELOG_ variable.
func (s *stack) lifelogCLI(args ...string) {
	s.t.Helper()
	cmd := exec.CommandContext(s.t.Context(), s.bin, append([]string{"--url", s.core}, args...)...)
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "LIFELOG_") }), "LIFELOG_TOKEN="+ownerToken)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		s.t.Fatalf("lifelog %s: %v\n%s%s", strings.Join(args, " "), err, out.String(), errOut.String())
	}
}

// turn is a turn of the conversation, as the API parsed it.
type turn struct {
	Who   string   `json:"who"`
	Via   string   `json:"via"`
	Text  string   `json:"text"`
	Tools []string `json:"tools"`
}

// conversation is the conversation of today, read as the owner.
func (s *stack) conversation() []turn {
	s.t.Helper()
	e, err := client.Remote(s.core, "", ownerToken).Get(s.t.Context(), "/days/today/conversation")
	if err != nil {
		s.t.Fatal(err)
	}
	raw, err := json.Marshal(e.Properties)
	if err != nil {
		s.t.Fatal(err)
	}
	var props struct{ Turns []turn }
	if err := json.Unmarshal(raw, &props); err != nil {
		s.t.Fatal(err)
	}
	return props.Turns
}

// waitReplies waits until the conversation holds n replies of lifelog, and returns its turns.
func (s *stack) waitReplies(n int) []turn {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		turns := s.conversation()
		count := 0
		for _, t := range turns {
			if t.Who == "lifelog" {
				count++
			}
		}
		if count >= n {
			return turns
		}
		select {
		case err := <-s.exited:
			s.exited <- err
			s.t.Fatalf("the server ended before the reply %d: %v\n%s", n, err, s.stderr)
		case <-time.After(50 * time.Millisecond):
		}
	}
	s.t.Fatalf("no reply %d after %s; turns %+v\n%s", n, wait, s.conversation(), s.stderr)
	return nil
}

// fakeModel answers by the count of requests: the first with a capture tool call, the second with text, the third
// with a call of the owner's action snapshot, the fourth with a read of /settings (the model tries to read the key),
// the fifth and later with text.
type fakeModel struct {
	mu   sync.Mutex
	reqs []map[string]any
	auth []string // the Authorization header of each request
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	n := len(f.reqs)
	f.mu.Unlock()
	if r.URL.Path != "/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	call := func(id, name, args string) {
		b, _ := json.Marshal(args)
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"`+id+`","type":"function","function":{"name":"`+name+`","arguments":`+string(b)+`}}]},"finish_reason":"tool_calls"}]}`)
	}
	say := func(s string) {
		b, _ := json.Marshal(s)
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+string(b)+`},"finish_reason":"stop"}]}`)
	}
	switch n {
	case 1:
		call("call1", "capture", `{"day":"2031-05-20","text":"Walked by the river, synthetic entry."}`)
	case 2:
		say("Written to 2031-05-20.")
	case 3:
		call("call2", "snapshot", `{}`)
	case 4:
		call("call3", "get", `{"href":"/settings"}`)
	default:
		say("Those are the owner's to run.")
	}
}

func TestSayGetsItsReplyFromTheAgentOnThePageAndAnOwnerActionIsRefused(t *testing.T) {
	b := build(t)
	s := startStack(t, b)
	model := &fakeModel{}
	srv := httptest.NewServer(model)
	defer srv.Close()

	// No model is set: the reply is one line, and the agent goes on.
	s.lifelogCLI("do", "say", "day=today", "text=hello")
	turns := s.waitReplies(1)
	if len(turns) != 2 || turns[0].Who != "owner" || turns[0].Text != "hello" || turns[1].Who != "lifelog" || turns[1].Text != "No model is set: open Settings." {
		t.Fatalf("turns %+v, want the owner's turn and the one line", turns)
	}

	// The model is set in the settings of the core, with a key. The agent reads it at its next turn.
	s.lifelogCLI("do", "settings", "model_url="+srv.URL+"/v1", "model=test-model", "model_key="+modelKey)
	s.lifelogCLI("do", "say", "day=today", "text=I walked by the river")
	turns = s.waitReplies(2)
	if len(turns) != 4 || turns[3].Text != "Written to 2031-05-20." {
		t.Fatalf("turns %+v, want a second reply written by the model", turns)
	}
	if tools := turns[3].Tools; len(tools) != 1 || !strings.HasPrefix(tools[0], "- `capture`") {
		t.Errorf("the tool lines of the reply: %q", tools)
	}

	// The tool call was made as agent:lifelog: the core stamps the entry with the relay.
	day, err := client.Remote(s.core, "", ownerToken).Get(t.Context(), "/days/2031-05-20")
	if err != nil {
		t.Fatal(err)
	}
	words := ""
	props, _ := day.Properties.(map[string]any)
	rows, _ := props["view"].([]any)
	for _, row := range rows {
		if r, _ := row.(map[string]any); r["what"] == "day page" {
			words, _ = r["detail"].(string)
		}
	}
	if !strings.Contains(words, "via lifelog) Walked by the river, synthetic entry.") {
		t.Errorf("the day page %q does not hold the capture of agent:lifelog", words)
	}

	// A tool call of an owner action is refused: snapshot is no tool, and the read of /settings is an agent's read.
	s.lifelogCLI("do", "say", "day=today", "text=Take a snapshot and read my settings")
	turns = s.waitReplies(3)
	if len(turns) != 6 || turns[5].Text != "Those are the owner's to run." {
		t.Fatalf("turns %+v, want a third reply", turns)
	}
	if tools := turns[5].Tools; len(tools) != 2 || tools[0] != "- `snapshot` failed: unknown tool" || tools[1] != "- `get` failed: forbidden" {
		t.Errorf("the tool lines of the third reply: %q", tools)
	}

	// The model got the key as its credential, was offered tools without the owner's actions, and read the page.
	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.reqs) != 5 {
		t.Fatalf("%d model requests, want 5", len(model.reqs))
	}
	if model.auth[0] != "Bearer "+modelKey {
		t.Errorf("the model got the header %q, want the key from the settings", model.auth[0])
	}
	offered, _ := model.reqs[0]["tools"].([]any)
	if len(offered) < 10 {
		t.Errorf("%d tools offered", len(offered))
	}
	for _, o := range offered {
		fn, _ := o.(map[string]any)["function"].(map[string]any)
		if name, _ := fn["name"].(string); name == "say" || name == "snapshot" || name == "settings" {
			t.Errorf("the owner's action %s is offered as a tool", name)
		}
	}
	msgs, _ := model.reqs[1]["messages"].([]any)
	user, _ := msgs[1].(map[string]any)
	if page, _ := user["content"].(string); !strings.Contains(page, "**Owner** (") || !strings.HasSuffix(page, "I walked by the river") {
		t.Errorf("the model read %q", page)
	}
	if raw, _ := json.Marshal(model.reqs[4]["messages"]); strings.Contains(string(raw), modelKey) || !strings.Contains(string(raw), "forbidden") {
		t.Errorf("the result of get /settings that the model read: %s", raw)
	}

	// The key is in no turn and in no log line.
	all, _ := json.Marshal(s.conversation())
	if strings.Contains(string(all), modelKey) || strings.Contains(s.stderr.String(), modelKey) {
		t.Error("the model key is on the page or in the log")
	}
	if n := strings.Count(s.stderr.String(), "with agent: started"); n != 1 {
		t.Errorf("the agent started %d times, want 1; log:\n%s", n, s.stderr)
	}

	// A SIGTERM ends the server, and the agent with it, with exit 0.
	if runtime.GOOS == "windows" {
		return
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-s.exited:
		s.exited <- err
		s.stoped = true
		if err != nil {
			t.Errorf("serve after SIGTERM: %v; log:\n%s", err, s.stderr)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("serve did not stop after SIGTERM; log:\n%s", s.stderr)
	}
	if !strings.Contains(s.stderr.String(), "with agent: ended") {
		t.Errorf("the log has no end of the agent:\n%s", s.stderr)
	}
}

// catalogServer is a server that answers GET /actions with the named actions and nothing else.
func catalogServer(t *testing.T, names ...string) *httptest.Server {
	t.Helper()
	var actions []siren.Action
	for _, n := range names {
		actions = append(actions, siren.Action{Name: n, Method: "POST", Href: "/" + n})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		json.NewEncoder(w).Encode(siren.Entity{Class: []string{"actions"}, Actions: actions})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTheAgentRefusesWhatItCannotRun: each missing piece is refused with its name or the line that says what to run. A
// catalog with no say is the one case where the owner acts first: the status is 78, and serve does not start the
// program again.
func TestTheAgentRefusesWhatItCannotRun(t *testing.T) {
	b := build(t)
	noSay := catalogServer(t, "save-body")
	noSave := catalogServer(t, "say")
	for _, tc := range []struct {
		name   string
		args   []string
		status int
		want   string
	}{
		{"no server", nil, 1, "no LIFELOG_URL and no --url"},
		{"an argument", []string{"--url", noSay.URL, "now"}, 1, `unexpected argument "now"`},
		{"a source flag", []string{"--url", noSay.URL, "--source", "x"}, 1, "flag provided but not defined: -source"},
		{"a token file flag", []string{"--url", noSay.URL, "--token-file", "f"}, 1, "flag provided but not defined: -token-file"},
		{"a catalog with no say", []string{"--url", noSay.URL}, app.ExitOwnerActs, `the catalog lacks an action: "say"`},
		{"a catalog with no save-body", []string{"--url", noSave.URL}, app.ExitOwnerActs, `the catalog lacks an action: "save-body"`},
		{"no server at the address", []string{"--url", "http://127.0.0.1:1"}, 1, "catalog:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), b.agent, tc.args...)
			cmd.Env = cleanEnv(t)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != tc.status {
				t.Fatalf("%v: %v, want exit status %d; stderr %q", tc.args, err, tc.status, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}
