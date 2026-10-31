package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

	"lifelog-whatsapp/relay"
	"lifelog/app"
	"lifelog/client"
	"lifelog/siren"
)

// The end-to-end test builds the real lifelog and lifelog-whatsapp into one folder. The connection to WhatsApp needs a real
// number, so it has no test. The relay is the part of the program that carries the words, and this test runs it in the
// process with a fake chat against the real `lifelog serve`: a message of the chat becomes a say, and a reply that an
// agent writes on the page reaches the chat. The tests of ./relay hold the rules on a fake catalog; this test holds the
// relay against the real writer. The program itself is run for what it refuses, for the pairing and for the start by
// `serve --with whatsapp`.

// wait is a guard against a deadlock.
const wait = 2 * time.Minute

// ownerToken is the token of the test: 40 characters of no meaning. It is the bearer token of the core.
const ownerToken = "e2e-token-e2e-token-e2e-token-e2e-token"

// ownerNumber is the number of the owner in the fake chat.
const ownerNumber = "40712345678"

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

// waitLog waits until the log holds the text, and fails when the process ends first or the time is spent. It reads the
// log, never the clock.
func waitLog(t *testing.T, log *lockedBuffer, done <-chan error, text string) string {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		for l := range strings.SplitSeq(log.String(), "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		select {
		case err := <-done:
			t.Fatalf("the process ended (%v) before the line %q; log:\n%s", err, text, log)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("no line %q after %s; log:\n%s", text, wait, log)
	return ""
}

// fakeChat is the chat of the owner on a phone: in carries the messages, sent holds the texts that the relay sent.
type fakeChat struct {
	in   chan relay.Message
	sent chan string
}

func (c *fakeChat) Messages() <-chan relay.Message { return c.in }

func (c *fakeChat) Send(_ context.Context, text string) error {
	c.sent <- text
	return nil
}

func (c *fakeChat) next(t *testing.T) string {
	t.Helper()
	select {
	case s := <-c.sent:
		return s
	case <-time.After(wait):
		t.Fatal("nothing was sent to the chat")
		return ""
	}
}

// turn is a turn of the conversation, as the API parsed it.
type turn struct {
	Who  string `json:"who"`
	Via  string `json:"via"`
	Text string `json:"text"`
}

// conversation is the conversation of today, as the owner reads it, with the id and the version of the page when it exists.
func conversation(t *testing.T, c *client.Client) (turns []turn, id, version, body string) {
	t.Helper()
	e, err := c.Get(t.Context(), "/days/today/conversation")
	if err != nil {
		t.Fatal(err)
	}
	props, _ := e.Properties.(map[string]any)
	raw, _ := json.Marshal(props["turns"])
	if err := json.Unmarshal(raw, &turns); err != nil {
		t.Fatal(err)
	}
	if props["id"] != nil {
		id, version, body = jsonString(props["id"]), jsonString(props["version"]), jsonString(props["body"])
	}
	return turns, id, version, body
}

// jsonString is the text of a string or of a number that the API sent.
func jsonString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// waitTurns waits until the conversation holds n turns, and returns them.
func waitTurns(t *testing.T, c *client.Client, n int) []turn {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if turns, _, _, _ := conversation(t, c); len(turns) >= n {
			return turns
		}
		<-time.After(20 * time.Millisecond)
	}
	t.Fatalf("the conversation did not reach %d turns", n)
	return nil
}

// TestAMessageOfTheChatBecomesASayAndAReplyOnThePageReachesTheChat runs the real `lifelog serve --token-file F` on a new
// life.db and the relay in the process, with a fake chat. The reply is written as lifelog-agent writes it: with
// save-body, as agent:lifelog.
func TestAMessageOfTheChatBecomesASayAndAReplyOnThePageReachesTheChat(t *testing.T) {
	b := build(t)
	tokenFile := filepath.Join(b.dir, "token")
	if err := os.WriteFile(tokenFile, []byte(ownerToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	serve := exec.Command(b.lifelog, "--db", b.db, "serve", "--addr", "127.0.0.1:0", "--token-file", tokenFile)
	serve.Env = env(t)
	stderr := &lockedBuffer{}
	serve.Stderr = stderr
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- serve.Wait() }()
	t.Cleanup(func() {
		select {
		case err := <-done:
			done <- err
		default:
			_ = serve.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(wait):
				_ = serve.Process.Kill()
				t.Error("the server did not stop")
			}
		}
	})
	line := waitLog(t, stderr, done, "lifelog: serving ")
	_, rest, _ := strings.Cut(line, " on http://")
	addr, _, _ := strings.Cut(rest, " ")
	core := "http://" + addr

	asOwner := client.Remote(core, "", ownerToken)
	asAgent := client.Remote(core, siren.AgentPrefix+"lifelog", ownerToken)
	chat := &fakeChat{in: make(chan relay.Message), sent: make(chan string, 10)}
	ticker := time.NewTicker(50 * time.Millisecond)
	t.Cleanup(ticker.Stop)
	r := &relay.Relay{
		Chat: chat, API: asOwner, Owner: ownerNumber, State: filepath.Join(t.TempDir(), "state.json"),
		Log: log.New(io.Discard, "", 0), Tick: ticker.C,
	}
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan error, 1)
	go func() { ran <- r.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-ran:
		case <-time.After(wait):
			t.Error("the relay did not stop")
		}
	})

	// A message of the chat becomes a say as the owner, and the page names the relay.
	chat.in <- relay.Message{Sender: ownerNumber, Text: "I walked by the river", Kind: relay.KindText, At: time.Now()}
	turns := waitTurns(t, asOwner, 1)
	if len(turns) != 1 || turns[0].Who != "owner" || turns[0].Via != "whatsapp" || turns[0].Text != "I walked by the river" {
		t.Fatalf("turns %+v, want the owner's turn via whatsapp", turns)
	}

	// An agent writes its reply on the page. The relay sends it to the chat, and only it: the turn of the chat is not echoed.
	_, id, version, body := conversation(t, asOwner)
	actions, err := asAgent.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var save siren.Action
	for _, a := range actions {
		if a.Name == "save-body" {
			save = a
		}
	}
	reply := body + "\n\n**lifelog** (" + time.Now().Format("15:04") + "): Written to today."
	if _, err := asAgent.Do(t.Context(), save, map[string]string{"id": id, "body": reply, "version": version}); err != nil {
		t.Fatal(err)
	}
	if got := chat.next(t); got != "Written to today." {
		t.Errorf("the chat got %q, want the reply of the agent", got)
	}

	// The owner says something on another surface: the chat gets it marked.
	actions, err = asOwner.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if a.Name == "say" {
			if _, err := asOwner.Do(t.Context(), a, map[string]string{"day": "today", "text": "from the browser"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := chat.next(t); got != "You wrote in lifelog: from the browser" {
		t.Errorf("the chat got %q, want the turn of the other surface, marked", got)
	}
	// The texts came in the order of the page, and each came once: the turn of the chat, which the page names with via, was
	// not echoed before the reply, and the reply was not sent again before the turn of the other surface.
}

// TestTheTransportRefusesWhatItCannotRun: each missing piece is refused with its name or the line that says what to run.
func TestTheTransportRefusesWhatItCannotRun(t *testing.T) {
	b := build(t)
	missing := filepath.Join(b.dir, "missing", "life.db")
	for _, tc := range []struct {
		name string
		line []string
		want string
	}{
		{"the owner flag is gone", []string{b.lifelog, "whatsapp", "--owner", "123", "--db", b.db}, "flag provided but not defined: -owner"},
		{"the terminal flag is gone", []string{b.lifelog, "whatsapp", "--terminal", "--db", b.db}, "flag provided but not defined: -terminal"},
		{"no server", []string{b.ext}, "no LIFELOG_URL and no --url"},
		{"no file", []string{b.lifelog, "whatsapp", "--db", missing}, "no LIFELOG_URL and no --url"},
		{"a source flag", []string{b.lifelog, "whatsapp", "--source", "x", "--db", b.db}, "flag provided but not defined: -source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(tc.line[0], tc.line[1:]...)
			cmd.Env = env(t)
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

// TestServingWithoutAPairingExitsAsTheOwnerActs: the session store is new, so the device is not paired (no network). The
// check comes before the wait for the owner's number: no number is set here, and the exit is still 78.
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

// TestServeWithWhatsappStartsTheTransportAndDoesNotStartItAgainWhenTheOwnerMustAct: `lifelog serve --with whatsapp`
// runs lifelog-whatsapp as a child. The state folder is new, so the device is not paired: the child exits with 78,
// and serve says that it does not start it again (the contract of extensions/README.md, "Run by serve"). A SIGTERM
// ends serve with exit 0. The transport of a message under --with needs a paired session, so it has no test.
func TestServeWithWhatsappStartsTheTransportAndDoesNotStartItAgainWhenTheOwnerMustAct(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a SIGTERM cannot be sent to a process on Windows")
	}
	b := build(t)
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
		waitLog(t, stderr, done, want)
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
