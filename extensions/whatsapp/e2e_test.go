package main_test

import (
	"bufio"
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
	"lifelog/siren"
)

// The end-to-end test builds the real lifelog and lifelog-whatsapp into one folder and runs
// `lifelog whatsapp --terminal --db F` against a fake model. That line holds the discovery of the
// extension (lifelog finds lifelog-whatsapp beside itself), the temporary server of the extension contract
// (extensions/README.md), the conversation page and the tool calls over HTTP. The tests of ./agent hold the turns
// on a fake catalog; this test holds the surface against the real writer. The connection to WhatsApp has no test.

// wait is a guard against a deadlock.
const wait = 2 * time.Minute

// built is the folder with lifelog, lifelog-whatsapp and a new life.db.
type built struct{ dir, lifelog, ext, db string }

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
	b.lifelog, b.ext, b.db = filepath.Join(b.dir, "lifelog"+exe), filepath.Join(b.dir, "lifelog-whatsapp"+exe), filepath.Join(b.dir, "life.db")
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.lifelog, "./cmd/lifelog"},
		{"build", "-o", b.ext, "./cmd/lifelog-whatsapp"},
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

// env is the environment of a child: the inherited one without any variable that could point it at another file,
// server or model than the ones of the test, and with a state folder of its own.
func env(t *testing.T, extra ...string) []string {
	home := t.TempDir()
	e := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "LIFELOG_") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=") || strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "APPDATA=")
	})
	return append(e, append([]string{"XDG_CONFIG_HOME=" + home, "HOME=" + home, "APPDATA=" + home}, extra...)...)
}

func (b built) run(args ...string) ([]byte, error) {
	cmd := exec.Command(b.lifelog, args...)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "LIFELOG_") })
	return cmd.Output()
}

// setModel sets the model in the settings file beside the database, as the owner does with `lifelog do settings`.
func (b built) setModel(t *testing.T, url, key string) {
	t.Helper()
	args := []string{"do", "settings", "--db", b.db, "model_url=" + url, "model=test-model"}
	if key != "" {
		args = append(args, "model_key="+key)
	}
	if out, err := b.run(args...); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			out = append(out, ee.Stderr...)
		}
		t.Fatalf("lifelog do settings: %v\n%s", err, out)
	}
}

// fakeModel answers by the count of requests: the first with a capture tool call, the second with text, the third
// with a read of /settings (the model tries to read the key), the fourth and later with text.
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
	if n == 1 {
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call1","type":"function","function":{"name":"capture","arguments":"{\"day\":\"2031-05-20\",\"text\":\"Walked by the river, synthetic entry.\"}"}}]},"finish_reason":"tool_calls"}]}`)
		return
	}
	if n == 3 {
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call2","type":"function","function":{"name":"get","arguments":"{\"href\":\"/settings\"}"}}]},"finish_reason":"tool_calls"}]}`)
		return
	}
	if n > 3 {
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"The settings are not mine to read."},"finish_reason":"stop"}]}`)
		return
	}
	io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Written to 2031-05-20."},"finish_reason":"stop"}]}`)
}

func TestTheTerminalAgentWritesACaptureAndKeepsTheConversation(t *testing.T) {
	b := build(t)
	model := &fakeModel{}
	srv := httptest.NewServer(model)
	defer srv.Close()

	b.setModel(t, srv.URL+"/v1", "synthetic-key")

	cmd := exec.Command(b.lifelog, "whatsapp", "--terminal", "--db", b.db)
	cmd.Env = env(t)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited { // the input first: the extension holds the output until its read of the input ends
			stdin.Close()
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	day := time.Now().Format(time.DateOnly)

	if _, err := io.WriteString(stdin, "I walked by the river\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	readLine := func() string {
		lines := make(chan string, 1)
		go func() {
			line, _ := reader.ReadString('\n')
			lines <- strings.TrimSpace(line)
		}()
		select {
		case got := <-lines:
			return got
		case <-time.After(wait):
			t.Fatalf("no reply; stderr:\n%s", stderr.String())
			return ""
		}
	}
	if got := readLine(); got != "Written to 2031-05-20." {
		t.Errorf("reply %q; stderr:\n%s", got, stderr.String())
	}
	// A second message: the model calls get on /settings. The tool carries the source agent:whatsapp, so the API
	// refuses it, and the key reaches no output.
	if _, err := io.WriteString(stdin, "What is my model key?\n"); err != nil {
		t.Fatal(err)
	}
	second := readLine()
	if second != "The settings are not mine to read." {
		t.Errorf("second reply %q; stderr:\n%s", second, stderr.String())
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		waited = true
		if err != nil {
			t.Fatalf("exit: %v; stderr:\n%s", err, stderr.String())
		}
	case <-time.After(wait):
		t.Fatal("the agent did not stop after the end of its input")
	}
	if strings.Contains(stderr.String(), "synthetic-key") || strings.Contains(second, "synthetic-key") {
		t.Error("the model key is in the log or in the reply")
	}

	// The model read the conversation page as its user message and was offered the tools.
	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.reqs) != 4 {
		t.Fatalf("%d model requests, want 4", len(model.reqs))
	}
	if raw, _ := json.Marshal(model.reqs[3]["messages"]); strings.Contains(string(raw), "synthetic-key") || !strings.Contains(string(raw), "forbidden") {
		t.Errorf("the result of get /settings that the model read: %s", raw)
	}
	if model.auth[0] != "Bearer synthetic-key" {
		t.Errorf("the model got the header %q, want the key from the settings", model.auth[0])
	}
	msgs, _ := model.reqs[0]["messages"].([]any)
	user, _ := msgs[len(msgs)-1].(map[string]any)
	if s, _ := user["content"].(string); !strings.Contains(s, "[["+day+"]]") || !strings.HasSuffix(s, "I walked by the river") {
		t.Errorf("the model read %q", s)
	}
	if tools, _ := model.reqs[0]["tools"].([]any); len(tools) < 10 {
		t.Errorf("%d tools offered", len(tools))
	}

	// The file holds the capture and the conversation (read after the run, with the CLI).
	out, err := b.run("get", "/days/2031-05-20", "--db", b.db)
	if err != nil {
		t.Fatalf("lifelog get: %v", err)
	}
	if !strings.Contains(string(out), "Walked by the river, synthetic entry.") {
		t.Errorf("the day page does not hold the capture: %s", out)
	}
	out, err = b.run("get", "/pages?title=Conversation%20"+day, "--db", b.db)
	if err != nil {
		t.Fatalf("lifelog get: %v", err)
	}
	var page siren.Entity
	if err := json.Unmarshal(out, &page); err != nil {
		t.Fatalf("lifelog get: %v: %s", err, out)
	}
	body, _ := page.Properties.(map[string]any)["body"].(string)
	if strings.Contains(string(out), "synthetic-key") {
		t.Error("the model key is on the conversation page")
	}
	for _, want := range []string{"[[" + day + "]]", "**Owner** (", ", via whatsapp): I walked by the river", "- `get` failed: forbidden", "): The settings are not mine to read.", "- `capture` → [/days/2031-05-20", "**lifelog** (", "): Written to 2031-05-20."} {
		if !strings.Contains(body, want) {
			t.Errorf("the conversation page lacks %q: %q", want, body)
		}
	}
}

// TestTheAgentRefusesWhatItCannotRun: each missing piece is refused with its name or the line that says what to run.
func TestTheAgentRefusesWhatItCannotRun(t *testing.T) {
	b := build(t)
	missing := filepath.Join(b.dir, "missing", "life.db")
	for _, tc := range []struct {
		name string
		line []string
		env  []string
		want string
	}{
		{"the owner flag is gone", []string{b.lifelog, "whatsapp", "--owner", "123", "--terminal", "--db", b.db}, nil, "flag provided but not defined: -owner"},
		{"no server", []string{b.ext, "--terminal"}, nil, "no LIFELOG_URL and no --url"},
		{"no file", []string{b.lifelog, "whatsapp", "--terminal", "--db", missing}, nil, "no LIFELOG_URL and no --url"},
		{"a source flag", []string{b.lifelog, "whatsapp", "--source", "x", "--db", b.db}, nil, "flag provided but not defined: -source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(tc.line[0], tc.line[1:]...)
			cmd.Env = env(t, tc.env...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() == 0 {
				t.Fatalf("%v: %v, want a non-zero status", tc.line[1:], err)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}

// TestServingWithoutAPairingExitsWithTwo: the session store is new, so the device is not paired (no network). The
// check comes before the wait for the owner's number: no number is set here, and the exit is still 2.
func TestServingWithoutAPairingExitsAsTheOwnerActs(t *testing.T) {
	b := build(t)
	cmd := exec.Command(b.lifelog, "whatsapp", "--db", b.db)
	cmd.Env = env(t)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != app.ExitOwnerActs {
		t.Fatalf("%v, want exit status %d; stderr %s", err, app.ExitOwnerActs, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not paired: run lifelog whatsapp login") {
		t.Errorf("stderr %q", stderr.String())
	}
}

// TestAnInterruptEndsTheTerminalAgentAtOnce: the read of the terminal blocks until a line comes, and a close of the
// file does not end it. The agent does not wait for it: an interrupt ends the run now, with exit 0, and not after
// the ten seconds that lifelog gives an extension before it kills it.
func TestAnInterruptEndsTheTerminalAgentAtOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an interrupt cannot be sent to a process on Windows")
	}
	b := build(t)
	srv := httptest.NewServer(&fakeModel{})
	defer srv.Close()
	b.setModel(t, srv.URL+"/v1", "")
	cmd := exec.Command(b.lifelog, "whatsapp", "--terminal", "--db", b.db)
	cmd.Env = env(t)
	stdin, err := cmd.StdinPipe() // stays open: the read of the agent blocks on it
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited { // the input first: the extension holds the output until its read of the input ends
			stdin.Close()
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	if _, err := io.WriteString(stdin, "hello\n"); err != nil { // a reply shows that the agent runs
		t.Fatal(err)
	}
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		lines <- strings.TrimSpace(line)
	}()
	select {
	case <-lines:
	case <-time.After(wait):
		t.Fatalf("no reply; stderr:\n%s", stderr.String())
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		waited = true
		if err != nil {
			t.Errorf("exit after an interrupt: %v; stderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the agent did not stop within 5 seconds of an interrupt; stderr:\n%s", stderr.String())
	}
	stdin.Close()
}

// lockedBuffer is a buffer that a process writes to and a test reads from.
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

// TestServeWithWhatsappStartsTheAgentAndDoesNotStartItAgainWhenTheOwnerMustAct: `lifelog serve --with whatsapp`
// runs lifelog-whatsapp as a child. The state folder is new, so the device is not paired: the child exits with 2,
// and serve says that it does not start it again (the contract of extensions/README.md, "Run by serve"). A SIGTERM
// ends serve with exit 0. The answer to a `say` under --with needs a paired session, so it has no test.
func TestServeWithWhatsappStartsTheAgentAndDoesNotStartItAgainWhenTheOwnerMustAct(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a SIGTERM cannot be sent to a process on Windows")
	}
	b := build(t)
	srv := httptest.NewServer(&fakeModel{})
	defer srv.Close()
	b.setModel(t, srv.URL+"/v1", "")
	if out, err := b.run("do", "settings", "--db", b.db, "owner_number=40712345678"); err != nil {
		t.Fatalf("lifelog do settings: %v\n%s", err, out)
	}
	cmd := exec.Command(b.lifelog, "serve", "--with", "whatsapp", "--db", b.db, "--addr", "127.0.0.1:0")
	cmd.Env = env(t)
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ended := false
	defer func() {
		if !ended {
			cmd.Process.Kill()
			<-done
		}
	}()
	for _, want := range []string{"serving", "with whatsapp: started", "not started again (the owner acts first)"} {
		deadline := time.Now().Add(wait)
		for !strings.Contains(stderr.String(), want) {
			select {
			case err := <-done:
				ended = true
				t.Fatalf("serve ended (%v) before the line %q; stderr:\n%s", err, want, stderr.String())
			case <-time.After(50 * time.Millisecond):
			}
			if time.Now().After(deadline) {
				t.Fatalf("no line %q; stderr:\n%s", want, stderr.String())
			}
		}
	}
	if !strings.Contains(stderr.String(), "not paired") {
		t.Errorf("the child did not say why it ended; stderr:\n%s", stderr.String())
	}
	if n := strings.Count(stderr.String(), "with whatsapp: started"); n != 1 {
		t.Errorf("the child started %d times, want 1; stderr:\n%s", n, stderr.String())
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		ended = true
		if err != nil {
			t.Errorf("serve after SIGTERM: %v; stderr:\n%s", err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("serve did not stop after SIGTERM; stderr:\n%s", stderr.String())
	}
}
