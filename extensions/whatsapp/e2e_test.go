package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

	"aminte-whatsapp/relay"
	"aminte/app"
	"aminte/client"
	"aminte/siren"
)

// The end-to-end test builds the real aminte, aminte-whatsapp and aminte-agent into one folder. The connection to
// WhatsApp needs a real number, so it has no test. The relay is the part of the program that carries the words, and these
// tests run it in the process with a fake chat against the real `aminte serve`: a message of the chat becomes a say, and
// a reply that an agent writes on the page reaches the chat. One test runs the real agent (`serve --with agent`) with a
// fake model server, and two messages in a row. The tests of ./relay hold the rules on a fake catalog; these tests hold
// the relay against the real writer. The program itself is run for what it refuses, for the pairing and for the start
// by `serve --with whatsapp`.

// wait is a guard against a deadlock.
const wait = 2 * time.Minute

// ownerToken is the token of the test: 40 characters of no meaning. It is the bearer token of the core.
const ownerToken = "e2e-token-e2e-token-e2e-token-e2e-token"

// ownerNumber is the number of the owner in the fake chat.
const ownerNumber = "40712345678"

// built is the folder with aminte, aminte-whatsapp, aminte-agent and a new life.db.
type built struct{ dir, aminte, ext, agent, db string }

func build(t *testing.T) built {
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
	b := built{dir: t.TempDir()}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	b.aminte, b.ext, b.agent, b.db = filepath.Join(b.dir, "aminte"+exe), filepath.Join(b.dir, "aminte-whatsapp"+exe),
		filepath.Join(b.dir, "aminte-agent"+exe), filepath.Join(b.dir, "life.db")
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.aminte, "./cmd/aminte"},
		{"build", "-o", b.ext, "./cmd/aminte-whatsapp"},
		{"build", "-C", filepath.Join(root, "extensions", "agent"), "-o", b.agent, "./cmd/aminte-agent"},
	} {
		if out, err := exec.Command(goBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	if out, err := b.run("init", b.db); err != nil {
		t.Fatalf("aminte init: %v\n%s", err, out)
	}
	return b
}

// env is the environment of a child: the inherited one without any variable that could point it at another file,
// server or model than the ones of the test, and with a state folder of its own.
func env(t *testing.T, extra ...string) []string {
	home := t.TempDir()
	e := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "AMINTE_") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=") || strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "APPDATA=")
	})
	return append(e, append([]string{"XDG_CONFIG_HOME=" + home, "HOME=" + home, "APPDATA=" + home}, extra...)...)
}

func (b built) run(args ...string) ([]byte, error) {
	cmd := exec.Command(b.aminte, args...)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "AMINTE_") })
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
	Key  string `json:"key"`
	Via  string `json:"via"`
	Text string `json:"text"`
}

// conversation is the turns of the conversation of today, as the owner reads them.
func conversation(t *testing.T, c *client.Client) []turn {
	t.Helper()
	e, err := c.Get(t.Context(), "/days/today/conversation")
	if err != nil {
		t.Fatal(err)
	}
	props, _ := e.Properties.(map[string]any)
	raw, _ := json.Marshal(props["turns"])
	var turns []turn
	if err := json.Unmarshal(raw, &turns); err != nil {
		t.Fatal(err)
	}
	return turns
}

// waitTurns waits until the conversation holds n turns, and returns them.
func waitTurns(t *testing.T, c *client.Client, n int) []turn {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if turns := conversation(t, c); len(turns) >= n {
			return turns
		}
		<-time.After(20 * time.Millisecond)
	}
	t.Fatalf("the conversation did not reach %d turns", n)
	return nil
}

// serveCore runs the real `aminte serve --token-file F` on the new life.db, with the programs named by with beside it, and
// gives the address. The server ends with the test.
func serveCore(t *testing.T, b built, with ...string) string {
	t.Helper()
	tokenFile := filepath.Join(b.dir, "token")
	if err := os.WriteFile(tokenFile, []byte(ownerToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--db", b.db, "serve", "--addr", "127.0.0.1:0", "--token-file", tokenFile}
	for _, w := range with {
		args = append(args, "--with", w)
	}
	serve := exec.Command(b.aminte, args...)
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
	line := waitLog(t, stderr, done, "aminte: serving ")
	_, rest, _ := strings.Cut(line, " on http://")
	addr, _, _ := strings.Cut(rest, " ")
	for _, w := range with {
		waitLog(t, stderr, done, "aminte: with "+w+": started (pid ")
	}
	return "http://" + addr
}

// action finds an action of the catalog by its name.
func action(t *testing.T, c *client.Client, name string) siren.Action {
	t.Helper()
	list, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("the catalog has no action %s", name)
	return siren.Action{}
}

// startRelay runs the relay in the process, with a fake chat, and stops it with the test. Tick nil is a tick every 50
// ms; a test that wants to say when the relay looks gives a channel.
func startRelay(t *testing.T, core string, tick <-chan time.Time) *fakeChat {
	t.Helper()
	chat := &fakeChat{in: make(chan relay.Message), sent: make(chan string, 10)}
	if tick == nil {
		ticker := time.NewTicker(50 * time.Millisecond)
		t.Cleanup(ticker.Stop)
		tick = ticker.C
	}
	r := &relay.Relay{
		Chat: chat, API: client.Remote(core, "", ownerToken), Owner: ownerNumber, State: filepath.Join(t.TempDir(), "state.json"),
		Log: log.New(io.Discard, "", 0), Tick: tick,
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
	return chat
}

// TestAMessageOfTheChatBecomesASayAndAReplyOnThePageReachesTheChat runs the real `aminte serve --token-file F` on a new
// life.db and the relay in the process, with a fake chat. The reply is written as aminte-agent writes it: with reply,
// as agent:aminte.
func TestAMessageOfTheChatBecomesASayAndAReplyOnThePageReachesTheChat(t *testing.T) {
	b := build(t)
	core := serveCore(t, b)
	asOwner := client.Remote(core, "", ownerToken)
	asAgent := client.Remote(core, siren.AgentPrefix+"aminte", ownerToken)
	chat := startRelay(t, core, nil)

	// A message of the chat becomes a say as the owner, and the page names the relay.
	chat.in <- relay.Message{Sender: ownerNumber, Text: "I walked by the river", Kind: relay.KindText, At: time.Now()}
	turns := waitTurns(t, asOwner, 1)
	if len(turns) != 1 || turns[0].Who != "owner" || turns[0].Via != "whatsapp" || turns[0].Text != "I walked by the river" {
		t.Fatalf("turns %+v, want the owner's turn via whatsapp", turns)
	}

	// An agent writes its reply on the page, to the turn that it names by its key. The relay sends the reply to the chat,
	// and only it: the turn of the chat is not echoed.
	if turns[0].Key == "" {
		t.Fatalf("turns %+v, want a key on the turn: reply names the turn by it", turns)
	}
	if _, err := asAgent.Do(t.Context(), action(t, asAgent, "reply"), map[string]string{"day": "today", "turn": turns[0].Key, "text": "Written to today."}); err != nil {
		t.Fatal(err)
	}
	if got := chat.next(t); got != "Written to today." {
		t.Errorf("the chat got %q, want the reply of the agent", got)
	}

	// The owner says something on another surface: the chat gets it marked.
	if _, err := asOwner.Do(t.Context(), action(t, asOwner, "say"), map[string]string{"day": "today", "text": "from the browser"}); err != nil {
		t.Fatal(err)
	}
	if got := chat.next(t); got != "You wrote in aminte: from the browser" {
		t.Errorf("the chat got %q, want the turn of the other surface, marked", got)
	}
	// The texts came in the order of the page, and each came once: the turn of the chat, which the page names with via, was
	// not echoed before the reply, and the reply was not sent again before the turn of the other surface.

	// The owner edits the reply on the page: it is a changed turn with a new key, so the chat gets it again with its new
	// words. The turns around it are not sent again.
	e, err := asOwner.Get(t.Context(), "/days/today/conversation")
	if err != nil {
		t.Fatal(err)
	}
	props, _ := e.Properties.(map[string]any)
	body, _ := props["body"].(string)
	edited := strings.Replace(body, "Written to today.", "Written to today, and checked.", 1)
	if edited == body {
		t.Fatalf("the page %q has no reply to edit", body)
	}
	if _, err := asOwner.Do(t.Context(), action(t, asOwner, "save-body"), map[string]string{"id": fmt.Sprint(props["id"]), "version": fmt.Sprint(props["version"]), "body": edited}); err != nil {
		t.Fatal(err)
	}
	if got := chat.next(t); got != "Written to today, and checked." {
		t.Errorf("the chat got %q, want the reply with its new words", got)
	}
	// No other turn changed: the next text is the next turn of the page. A turn that was sent again would come before it.
	if _, err := asOwner.Do(t.Context(), action(t, asOwner, "say"), map[string]string{"day": "today", "text": "after the edit"}); err != nil {
		t.Fatal(err)
	}
	if got := chat.next(t); got != "You wrote in aminte: after the edit" {
		t.Errorf("the chat got %q, want the next turn only", got)
	}
}

// feedTicks gives the relay a tick every 50 ms, and returns the function that stops it and waits for it. The function can
// be called more than once.
func feedTicks(tick chan<- time.Time) (stop func()) {
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		every := time.NewTicker(50 * time.Millisecond)
		defer every.Stop()
		for {
			select {
			case <-every.C:
				select {
				case tick <- time.Now():
				case <-quit:
					return
				}
			case <-quit:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(quit) })
		<-done
	}
}

// gatedModel is a model server that holds its first answer until the test opens the gate, and tells the test when the
// first request came. The later answers come at once. The n-th answer is "answer n".
type gatedModel struct {
	mu      sync.Mutex
	n       int
	first   chan struct{} // closed when the first request came
	gate    chan struct{} // the first answer waits for it to close
	started sync.Once
}

func (m *gatedModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.n++
	n := m.n
	m.mu.Unlock()
	if n == 1 {
		m.started.Do(func() { close(m.first) })
		select {
		case <-m.gate:
		case <-r.Context().Done():
			return
		}
	}
	b, _ := json.Marshal(fmt.Sprintf("answer %d", n))
	io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+string(b)+`},"finish_reason":"stop"}]}`)
}

// TestTwoMessagesInARowGetTheirTwoRepliesOnThePhone runs the real server with the real agent and the relay in the
// process. The owner sends a second message while the model works on the first. The core puts the first reply before the
// second turn, and the agent answers the second turn next. The relay has dealt with the second turn before the first
// reply lands, and the phone must still get both replies, once each and in the order of the page.
func TestTwoMessagesInARowGetTheirTwoRepliesOnThePhone(t *testing.T) {
	b := build(t)
	core := serveCore(t, b, "agent")
	asOwner := client.Remote(core, "", ownerToken)
	model := &gatedModel{first: make(chan struct{}), gate: make(chan struct{})}
	srv := httptest.NewServer(model)
	defer srv.Close()
	if _, err := asOwner.Do(t.Context(), action(t, asOwner, "settings"), map[string]string{"model_url": srv.URL + "/v1", "model": "test-model"}); err != nil {
		t.Fatal(err)
	}
	tick := make(chan time.Time)
	chat := startRelay(t, core, tick)
	look := func() { // the relay takes the third tick only after the look of the second is done
		for range 3 {
			select {
			case tick <- time.Now():
			case <-time.After(wait):
				t.Fatal("the relay did not take the tick")
			}
		}
	}

	chat.in <- relay.Message{Sender: ownerNumber, Text: "first", Kind: relay.KindText, At: time.Now()}
	select {
	case <-model.first:
	case <-time.After(wait):
		t.Fatal("the agent did not ask the model")
	}
	chat.in <- relay.Message{Sender: ownerNumber, Text: "second", Kind: relay.KindText, At: time.Now()}
	waitTurns(t, asOwner, 2)
	look() // the relay has dealt with both turns of the owner, and the reply to the first is not on the page yet
	close(model.gate)
	stopFeed := feedTicks(tick) // the relay looks every 50 ms until the test has both replies
	t.Cleanup(stopFeed)

	if got := chat.next(t); got != "answer 1" {
		t.Errorf("the phone got %q first, want the reply to the first message", got)
	}
	if got := chat.next(t); got != "answer 2" {
		t.Errorf("the phone got %q second, want the reply to the second message", got)
	}
	stopFeed()
	look() // one more full look at the page as it is now
	select {
	case extra := <-chat.sent:
		t.Errorf("an extra text went to the phone: %q", extra)
	default:
	}
	var order []string
	for _, tr := range conversation(t, asOwner) {
		order = append(order, tr.Who+": "+tr.Text)
	}
	if want := []string{"owner: first", "aminte: answer 1", "owner: second", "aminte: answer 2"}; !slices.Equal(order, want) {
		t.Errorf("the page holds %q, want %q", order, want)
	}
}

// TestTheTransportRefusesWhatItCannotRun: each missing piece is refused with its name or the line that says what to run.
// A flag that the program does not take is a plain error (1). A missing address of aminte is the owner's to mend (78).
func TestTheTransportRefusesWhatItCannotRun(t *testing.T) {
	b := build(t)
	missing := filepath.Join(b.dir, "missing", "life.db")
	for _, tc := range []struct {
		name   string
		line   []string
		status int
		want   string
	}{
		{"the owner flag is gone", []string{b.aminte, "whatsapp", "--owner", "123", "--db", b.db}, 1, "flag provided but not defined: -owner"},
		{"the terminal flag is gone", []string{b.aminte, "whatsapp", "--terminal", "--db", b.db}, 1, "flag provided but not defined: -terminal"},
		{"the token file flag is gone", []string{b.aminte, "whatsapp", "--token-file", "token", "--db", b.db}, 1, "flag provided but not defined: -token-file"},
		{"no server", []string{b.ext}, app.ExitOwnerActs, "no AMINTE_URL and no --url: run it as `aminte whatsapp` on"},
		{"no file", []string{b.aminte, "whatsapp", "--db", missing}, app.ExitOwnerActs, "no AMINTE_URL and no --url: run it as `aminte whatsapp` on"},
		{"a source flag", []string{b.aminte, "whatsapp", "--source", "x", "--db", b.db}, 1, "flag provided but not defined: -source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(tc.line[0], tc.line[1:]...)
			cmd.Env = env(t)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != tc.status {
				t.Fatalf("%v: %v, want exit status %d", tc.line[1:], err, tc.status)
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
	cmd := exec.Command(b.aminte, "whatsapp", "--db", b.db)
	cmd.Env = env(t)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != app.ExitOwnerActs {
		t.Fatalf("%v, want exit status %d; stderr %s", err, app.ExitOwnerActs, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not paired: run aminte whatsapp login") {
		t.Errorf("stderr %q", stderr.String())
	}
}

// TestServeWithWhatsappStartsTheTransportAndDoesNotStartItAgainWhenTheOwnerMustAct: `aminte serve --with whatsapp`
// runs aminte-whatsapp as a child. The state folder is new, so the device is not paired: the child exits with 78,
// and serve says that it does not start it again (the contract of extensions/README.md, "Run by serve"). A SIGTERM
// ends serve with exit 0. The transport of a message under --with needs a paired session, so it has no test.
func TestServeWithWhatsappStartsTheTransportAndDoesNotStartItAgainWhenTheOwnerMustAct(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a SIGTERM cannot be sent to a process on Windows")
	}
	b := build(t)
	cmd := exec.Command(b.aminte, "serve", "--with", "whatsapp", "--db", b.db, "--addr", "127.0.0.1:0")
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
