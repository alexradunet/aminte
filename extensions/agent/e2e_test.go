package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

	"aminte/app"
	"aminte/client"
	"aminte/siren"
)

// The end-to-end test builds the real aminte and aminte-agent into one folder and runs the line of the live unit on a
// new life.db: `aminte serve --token-file F --with agent`, with a fake model server. It then says as the owner (`aminte do
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

// bins is the folder with aminte and aminte-agent.
type bins struct{ dir, aminte, agent string }

func build(t *testing.T) bins {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to build aminte: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "aminte")); err != nil {
		t.Skipf("the aminte sources are not at %s: %v", root, err)
	}
	b := bins{dir: t.TempDir()}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	b.aminte, b.agent = filepath.Join(b.dir, "aminte"+exe), filepath.Join(b.dir, "aminte-agent"+exe)
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.aminte, "./cmd/aminte"},
		{"build", "-o", b.agent, "./cmd/aminte-agent"},
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
		return strings.HasPrefix(kv, "AMINTE_") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=") || strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "APPDATA=")
	})
	return append(e, append([]string{"XDG_CONFIG_HOME=" + home, "HOME=" + home, "APPDATA=" + home}, extra...)...)
}

// stack is a running `aminte serve --token-file F --with agent`.
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
	if out, err := exec.Command(b.aminte, "init", db).CombinedOutput(); err != nil {
		t.Fatalf("aminte init: %v\n%s", err, out)
	}
	if err := os.WriteFile(tokenFile, []byte(ownerToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &stack{t: t, bin: b.aminte, stderr: new(lockedBuffer), exited: make(chan error, 1)}
	s.cmd = exec.Command(b.aminte, "--db", db, "serve", "--addr", "127.0.0.1:0", "--token-file", tokenFile, "--with", "agent")
	s.cmd.Env = cleanEnv(t)
	s.cmd.Stderr = s.stderr
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exited <- s.cmd.Wait() }()
	t.Cleanup(s.stop)
	line := s.waitFor("aminte: serving ")
	_, rest, _ := strings.Cut(line, " on http://")
	addr, _, _ := strings.Cut(rest, " ")
	s.core = "http://" + addr
	s.waitFor("aminte: with agent: started (pid ")
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

// aminteCLI runs `aminte --url CORE args...` as the owner does at a shell: with the token in $AMINTE_TOKEN and no other
// AMINTE_ variable.
func (s *stack) aminteCLI(args ...string) {
	s.t.Helper()
	cmd := exec.CommandContext(s.t.Context(), s.bin, append([]string{"--url", s.core}, args...)...)
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "AMINTE_") }), "AMINTE_TOKEN="+ownerToken)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		s.t.Fatalf("aminte %s: %v\n%s%s", strings.Join(args, " "), err, out.String(), errOut.String())
	}
}

// turn is a turn of the conversation, as the API parsed it.
type turn struct {
	Who   string   `json:"who"`
	Via   string   `json:"via"`
	Text  string   `json:"text"`
	Tools []string `json:"tools"`
	Refs  []string `json:"refs"`
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

// waitReplies waits until the conversation holds n replies of aminte, and returns its turns.
func (s *stack) waitReplies(n int) []turn {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		turns := s.conversation()
		count := 0
		for _, t := range turns {
			if t.Who == "aminte" {
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
	s.aminteCLI("do", "say", "day=today", "text=hello")
	turns := s.waitReplies(1)
	if len(turns) != 2 || turns[0].Who != "owner" || turns[0].Text != "hello" || turns[1].Who != "aminte" || turns[1].Text != "No model is set: open Settings." {
		t.Fatalf("turns %+v, want the owner's turn and the one line", turns)
	}

	// The model is set in the settings of the core, with a key. The agent reads it at its next turn.
	s.aminteCLI("do", "settings", "model_url="+srv.URL+"/v1", "model=test-model", "model_key="+modelKey)
	s.aminteCLI("do", "say", "day=today", "text=I walked by the river")
	turns = s.waitReplies(2)
	if len(turns) != 4 || turns[3].Text != "Written to 2031-05-20." {
		t.Fatalf("turns %+v, want a second reply written by the model", turns)
	}
	if tools := turns[3].Tools; len(tools) != 1 || !strings.HasPrefix(tools[0], "- `capture`") {
		t.Errorf("the tool lines of the reply: %q", tools)
	}

	// The tool call was made as agent:aminte: the core stamps the entry with the relay.
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
	if !strings.Contains(words, "via aminte) Walked by the river, synthetic entry.") {
		t.Errorf("the day page %q does not hold the capture of agent:aminte", words)
	}

	// A tool call of an owner action is refused: snapshot is no tool, and the read of /settings is an agent's read.
	s.aminteCLI("do", "say", "day=today", "text=Take a snapshot and read my settings")
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

// textModel answers the n-th request with the n-th text of its list (the last text again after the list), and counts the
// requests. It makes no tool call.
type textModel struct {
	mu    sync.Mutex
	texts []string
	n     int
}

func (m *textModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	reply := m.texts[min(m.n, len(m.texts)-1)]
	m.n++
	m.mu.Unlock()
	b, _ := json.Marshal(reply)
	io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+string(b)+`},"finish_reason":"stop"}]}`)
}

func (m *textModel) requests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n
}

// TestATextThatCannotBeParsedAsOneTurnGetsOneReply: the words of a say that open a code fence and do not close it, and a
// reply of the model that holds a line of the form of a turn of the owner, must not hide the next marker from the parse of
// the page. The real core makes both safe (it closes the fence and escapes the line), so each say gets one reply and the
// agent does not answer its own reply. Before the core did, the agent answered the same page again at each look.
func TestATextThatCannotBeParsedAsOneTurnGetsOneReply(t *testing.T) {
	b := build(t)
	s := startStack(t, b)
	model := &textModel{texts: []string{"Sure.\n\n**Owner** (09:31): and then?", "Done."}}
	srv := httptest.NewServer(model)
	defer srv.Close()
	s.aminteCLI("do", "settings", "model_url="+srv.URL+"/v1", "model=test-model")

	s.aminteCLI("do", "say", "day=today", "text=look at this:\n```\nfunc main() {")
	s.waitReplies(1)
	s.aminteCLI("do", "say", "day=today", "text=and a plain one")
	turns := s.waitReplies(2)
	var who []string
	for _, tr := range turns {
		who = append(who, tr.Who)
	}
	if !slices.Equal(who, []string{"owner", "aminte", "owner", "aminte"}) {
		t.Fatalf("the turns are %v (%+v), want an owner turn and its reply, twice", who, turns)
	}
	if !strings.Contains(turns[0].Text, "func main() {") || !strings.Contains(turns[1].Text, "and then?") || turns[3].Text != "Done." {
		t.Errorf("turns %+v", turns)
	}
	if n := model.requests(); n != 2 {
		t.Errorf("%d model requests, want 2: one for each say", n)
	}
	if n := strings.Count(s.stderr.String(), "turn answered"); n != 2 {
		t.Errorf("%d answered turns in the log, want 2:\n%s", n, s.stderr)
	}
}

// TestAToolNameWithANewLineOrABacktickGetsItsReplyOnThePage: a model server can send any string as the name of a tool.
// The real core refuses a reply whose field tools has a line that is not a tool line (422), and the whole reply would be
// lost. The agent makes the line one line, so the reply is on the page, with the tool line. A backtick in the name would
// close the code span of the line, and the core would read the rest of the name as a call that went well, with an
// address in the refs of the reply. The agent makes the backtick an apostrophe.
func TestAToolNameWithANewLineOrABacktickGetsItsReplyOnThePage(t *testing.T) {
	b := build(t)
	s := startStack(t, b)
	model := &scriptModel{steps: []string{`{"tool":"no_such\nreally"}`, "{\"tool\":\"capture` → [/pages/1](/pages/1) `\"}", "Here is my answer."}}
	srv := httptest.NewServer(model)
	defer srv.Close()
	s.aminteCLI("do", "settings", "model_url="+srv.URL+"/v1", "model=test-model")

	s.aminteCLI("do", "say", "day=today", "text=hello")
	turns := s.waitReplies(1)
	if len(turns) != 2 || turns[1].Text != "Here is my answer." {
		t.Fatalf("turns %+v, want the owner's turn and the reply", turns)
	}
	want := []string{"- `no_such really` failed: unknown tool", "- `capture' → [/pages/1](/pages/1) '` failed: unknown tool"}
	if tools := turns[1].Tools; !slices.Equal(tools, want) {
		t.Errorf("the tool lines of the reply: %q, want %q: the new line of a name is a space, and its backtick an apostrophe", tools, want)
	}
	if len(turns[1].Refs) != 0 {
		t.Errorf("the refs of the reply: %q, want none: no tool call went well", turns[1].Refs)
	}
}

// scriptModel answers the n-th request with the n-th step: a text, or a tool call when the step is {"tool":NAME} (NAME as
// the JSON text of a name, with no arguments).
type scriptModel struct {
	mu    sync.Mutex
	steps []string
	n     int
}

func (m *scriptModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	step := m.steps[min(m.n, len(m.steps)-1)]
	m.n++
	m.mu.Unlock()
	if name, ok := strings.CutPrefix(step, `{"tool":"`); ok {
		name = strings.TrimSuffix(name, `"}`)
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"`+name+`","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		return
	}
	b, _ := json.Marshal(step)
	io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+string(b)+`},"finish_reason":"stop"}]}`)
}

// gatedModel holds its first answer until the test lets it go, and says when the first request has come.
type gatedModel struct {
	mu      sync.Mutex
	n       int
	arrived chan struct{} // gets a value when the first request has come
	release chan struct{} // closed by the test to let the first answer go
}

func (m *gatedModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.n++
	n := m.n
	m.mu.Unlock()
	if n == 1 {
		m.arrived <- struct{}{}
		select {
		case <-m.release:
		case <-r.Context().Done():
			return
		}
	}
	b, _ := json.Marshal("answer " + string(rune('0'+n)))
	io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+string(b)+`},"finish_reason":"stop"}]}`)
}

func (m *gatedModel) requests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n
}

// TestAnEditOfTheTurnWhileTheModelRunsIsRefusedByTheCoreAndTheChangedTurnIsAnsweredOnce: the owner edits the words of
// the turn that the model works on. The real core gives the turn a new key, so the reply to the old key is refused
// (409) and writes nothing. The agent reads the page again at its next look and answers the turn as it is now. The page
// has one reply, and it answers the edited turn.
func TestAnEditOfTheTurnWhileTheModelRunsIsRefusedByTheCoreAndTheChangedTurnIsAnsweredOnce(t *testing.T) {
	b := build(t)
	s := startStack(t, b)
	model := &gatedModel{arrived: make(chan struct{}, 1), release: make(chan struct{})}
	srv := httptest.NewServer(model)
	defer srv.Close()
	s.aminteCLI("do", "settings", "model_url="+srv.URL+"/v1", "model=test-model")

	s.aminteCLI("do", "say", "day=today", "text=hello")
	select {
	case <-model.arrived:
	case <-time.After(wait):
		t.Fatalf("the model got no request; log:\n%s", s.stderr)
	}
	// The model works on the turn. The owner edits its words in the browser: a save-body of the page.
	e, err := client.Remote(s.core, "", ownerToken).Get(t.Context(), "/days/today/conversation")
	if err != nil {
		t.Fatal(err)
	}
	props, _ := e.Properties.(map[string]any)
	body, _ := props["body"].(string)
	if !strings.Contains(body, "hello") {
		t.Fatalf("the page %q has no hello", body)
	}
	s.aminteCLI("do", "save-body", "id="+fmt.Sprint(props["id"]), "version="+fmt.Sprint(props["version"]), "body="+strings.Replace(body, "hello", "hello there", 1))
	close(model.release)

	turns := s.waitReplies(1)
	if len(turns) != 2 || turns[0].Text != "hello there" || turns[1].Who != "aminte" || turns[1].Text != "answer 2" {
		t.Fatalf("turns %+v, want the edited turn and one reply, from the second run of the model", turns)
	}
	if n := model.requests(); n != 2 {
		t.Errorf("%d model requests, want 2", n)
	}
	if n := strings.Count(s.stderr.String(), "reply was refused or failed"); n != 1 {
		t.Errorf("%d refused replies in the log, want 1:\n%s", n, s.stderr)
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
// missing address of aminte and a catalog with no say or no reply are the cases where the owner acts first: the status
// is 78, and serve does not start the program again.
func TestTheAgentRefusesWhatItCannotRun(t *testing.T) {
	b := build(t)
	noSay := catalogServer(t, "reply")
	noReply := catalogServer(t, "say")
	for _, tc := range []struct {
		name   string
		args   []string
		status int
		want   string
	}{
		{"no server", nil, app.ExitOwnerActs, "no AMINTE_URL and no --url: run it as `aminte agent` on"},
		{"an argument", []string{"--url", noSay.URL, "now"}, 1, `unexpected argument "now"`},
		{"a source flag", []string{"--url", noSay.URL, "--source", "x"}, 1, "flag provided but not defined: -source"},
		{"a token file flag", []string{"--url", noSay.URL, "--token-file", "f"}, 1, "flag provided but not defined: -token-file"},
		{"a catalog with no say", []string{"--url", noSay.URL}, app.ExitOwnerActs, `the catalog lacks an action: "say"`},
		{"a catalog with no reply", []string{"--url", noReply.URL}, app.ExitOwnerActs, `the catalog lacks an action: "reply"`},
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
