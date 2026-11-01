package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
)

// lockedBuffer is a writer that the server, its supervisors and a test read and write at once.
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

// withServer is a serve that runs in-process with fake extensions beside it. dir holds the program lifelog, the fake
// extensions and, for each of them, NAME.mode (what it does) and NAME.log (what it saw), as testdata/fakeext says.
type withServer struct {
	dir, db  string
	stderr   *lockedBuffer
	cancel   context.CancelFunc
	done     chan error
	base     string // http://HOST:PORT of the listener
	stopOnce sync.Once
}

// startWithServer writes the mode of each fake (name -> mode), starts `lifelog serve --with NAME...` with the extra
// words, and returns when the server said it serves. The restart delay is 50 ms.
func startWithServer(t *testing.T, modes map[string]string, extra ...string) *withServer {
	t.Helper()
	w := newWithServer(t)
	argv := []string{"--db", w.db, "serve", "--addr", "127.0.0.1:0"}
	for name, mode := range modes {
		fakeExtensionAs(t, w.dir, name)
		if err := os.WriteFile(filepath.Join(w.dir, "lifelog-"+name+".mode"), []byte(mode), 0o644); err != nil {
			t.Fatal(err)
		}
		argv = append(argv, "--with", name)
	}
	w.start(t, argv, extra)
	return w
}

// newWithServer is the folder, the file and the settings of a serve that runs with children: the restart delay is
// 50 ms, and the children get the environment of the test.
func newWithServer(t *testing.T) *withServer {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot send an interrupt to a process: the console does")
	}
	old := restartDelay
	restartDelay = 50 * time.Millisecond
	t.Cleanup(func() { restartDelay = old })
	w := &withServer{dir: t.TempDir(), db: dbtest.File(t), stderr: new(lockedBuffer), done: make(chan error, 1)}
	t.Setenv("FAKE_DIR", w.dir)
	t.Setenv("LIFELOG_TOKEN", "inherited-token-must-not-reach-the-child")
	return w
}

// start runs the command line in-process and returns when the server said it serves.
func (w *withServer) start(t *testing.T, argv, extra []string) {
	t.Helper()
	e, _, _ := testEnv(t)
	e.executable = func() (string, error) { return filepath.Join(w.dir, "lifelog"), nil }
	e.stdout, e.stderr = new(lockedBuffer), w.stderr
	e.clock = func() time.Time { return time.Date(2031, 2, 3, 12, 0, 0, 0, time.Local) }
	// The ambient LIFELOG_TOKEN is the parent's, and serve does not read it (getenv is noVars): the server has a token
	// only through --token-file, and a child never inherits the ambient one.
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go func() { w.done <- runArgs(ctx, e, append(argv, extra...)) }()
	t.Cleanup(func() { _ = w.stop(t) })
	line := w.waitFor(t, "lifelog: serving ")
	_, address := servedAt(t, line)
	w.base = "http://" + address
}

// stop interrupts the server and waits for it to return.
func (w *withServer) stop(t *testing.T) error {
	t.Helper()
	var err error
	w.stopOnce.Do(func() {
		w.cancel()
		select {
		case err = <-w.done:
		case <-time.After(30 * time.Second):
			t.Error("serve did not stop")
		}
	})
	return err
}

// waitFor waits until the stderr of the server holds the text, and returns the line that holds it.
func (w *withServer) waitFor(t *testing.T, text string) string {
	t.Helper()
	return waitUntil(t, func() (string, bool) {
		for _, l := range strings.Split(w.stderr.String(), "\n") {
			if strings.Contains(l, text) {
				return l, true
			}
		}
		select {
		case err := <-w.done:
			t.Fatalf("serve ended before %q: %v\n%s", text, err, w.stderr.String())
		default:
		}
		return "", false
	}, text)
}

// logOf is the lines that the fake extension NAME wrote when it started, waiting for at least n of them.
func (w *withServer) logOf(t *testing.T, name string, n int) []string {
	t.Helper()
	path := filepath.Join(w.dir, "lifelog-"+name+".log")
	return waitUntil(t, func() ([]string, bool) {
		b, _ := os.ReadFile(path)
		got := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(b) == 0 || len(got) < n {
			return nil, false
		}
		return got, true
	}, name+" log")
}

func waitUntil[T any](t *testing.T, ok func() (T, bool), what string) T {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if v, done := ok(); done {
			return v
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("waited 30 s for %s", what)
		}
	}
}

// fakeExtensionAs writes the fake extension as lifelog-NAME into dir.
func fakeExtensionAs(t *testing.T, dir, name string) {
	t.Helper()
	path := fakeExtension(t, dir)
	want := filepath.Join(dir, "lifelog-"+name)
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func port(base string) string { return base[strings.LastIndex(base, ":")+1:] }

// TestServeWithGivesTheChildTheListenerAndNothingElse: the child gets the loopback address of the listener, an empty
// stdin and no arguments, answers its own GET / with 200, and has no token on a local listener, whatever the parent's
// environment holds.
func TestServeWithGivesTheChildTheListenerAndNothingElse(t *testing.T) {
	w := startWithServer(t, map[string]string{"fake": "wait"})
	got := w.logOf(t, "fake", 1)[0]
	want := "url=http://127.0.0.1:" + port(w.base) + " token= stdin=0 args=0 status=200"
	if got != want {
		t.Errorf("the child saw %q, want %q", got, want)
	}
	if line := w.waitFor(t, "lifelog: with fake: started (pid "); line == "" {
		t.Error("no start line")
	}
}

// TestServeWithGivesTheChildTheTokenOfTheServer: the child reaches a server that has a token with the token of the
// server, not the token of the parent's environment.
func TestServeWithGivesTheChildTheTokenOfTheServer(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := startWithServer(t, map[string]string{"fake": "wait"}, "--token-file", tokenFile)
	got := w.logOf(t, "fake", 1)[0]
	want := "url=http://127.0.0.1:" + port(w.base) + " token=" + testToken + " stdin=0 args=0 status=200"
	if got != want {
		t.Errorf("the child saw %q, want %q", got, want)
	}
}

// TestServeWithAppliesTheExitRule: 1, 2 and 3 start the child again (2 is the status of a Go program that panics); 0
// and 78 do not, and each has its line. 78 is the one status for "the owner acts first": 3 is not.
func TestServeWithAppliesTheExitRule(t *testing.T) {
	for _, status := range []string{"1", "2", "3"} {
		t.Run(status+" starts again", func(t *testing.T) {
			w := startWithServer(t, map[string]string{"fake": "exit-once=" + status})
			w.logOf(t, "fake", 2) // the second start: the first one ended with the status
			w.waitFor(t, "lifelog: with fake: ended with status "+status+", started again in "+restartDelay.String())
			if n := strings.Count(w.stderr.String(), "lifelog: with fake: started (pid "); n < 2 {
				t.Errorf("%d start lines, want 2\n%s", n, w.stderr.String())
			}
		})
	}
	for _, c := range []struct{ mode, line string }{
		{"exit=0", "lifelog: with fake: ended\n"},
		{"exit=78", "lifelog: with fake: ended with status 78: not started again (the owner acts first)\n"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			w := startWithServer(t, map[string]string{"fake": c.mode})
			w.waitFor(t, strings.TrimSuffix(c.line, "\n"))
			// A start again would come after restartDelay (50 ms): a window of ten times that shows there is none.
			time.Sleep(10 * restartDelay)
			if n := len(w.logOf(t, "fake", 1)); n != 1 {
				t.Errorf("the child started %d times, want 1", n)
			}
			if strings.Contains(w.stderr.String(), "started again in") {
				t.Errorf("the log says the child is started again:\n%s", w.stderr.String())
			}
		})
	}
}

// TestServeWithRunsEveryNameGiven: two --with run two children.
func TestServeWithRunsEveryNameGiven(t *testing.T) {
	w := startWithServer(t, map[string]string{"fake": "wait", "fake2": "wait"})
	w.logOf(t, "fake", 1)
	w.logOf(t, "fake2", 1)
	for _, name := range []string{"fake", "fake2"} {
		w.waitFor(t, "lifelog: with "+name+": started (pid ")
	}
}

// TestServeWithRefusesBeforeAnythingOpens: an unknown name, a name twice and a flag of the network that serve no longer
// takes end the command with no listener and a file that is not touched.
func TestServeWithRefusesBeforeAnythingOpens(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.db")
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeExtension(t, dir)
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{"serve", "--with", "nosuch"}, "lifelog-nosuch"},
		{[]string{"serve", "--with", "fake", "--with", "nosuch"}, "lifelog-nosuch"},
		{[]string{"serve", "--with", "fake", "--with", "fake"}, "given twice"}, // two children on one session: refused
		{[]string{"serve", "--public", "--token-file", tokenFile, "--with", "fake"}, "unknown flag --public"},
		{[]string{"serve", "--tls-cert", "c.pem", "--with", "fake"}, "unknown flag --tls-cert"},
	} {
		e, _, stderr := testEnv(t)
		e.executable = func() (string, error) { return filepath.Join(dir, "lifelog"), nil }
		err := runArgs(t.Context(), e, append([]string{"--db", absent}, append(c.argv, "--addr", "127.0.0.1:0")...))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want a refusal with %q", c.argv, err, c.want)
		}
		if strings.Contains(stderr.String(), "serving") {
			t.Errorf("%v: the server started: %s", c.argv, stderr)
		}
		if _, err := os.Stat(absent); err == nil {
			t.Fatalf("%v: the file was created", c.argv)
		}
	}
}

// TestServeWithEndsTheChildBeforeTheDrain: on an interrupt the child is interrupted first and awaited; it writes a
// capture to the server while it ends and exits 0 only when the server answered 200. The capture is on the day page
// after serve returned.
func TestServeWithEndsTheChildBeforeTheDrain(t *testing.T) {
	w := startWithServer(t, map[string]string{"fake": "capture"})
	w.logOf(t, "fake", 1)
	if err := w.stop(t); err != nil {
		t.Fatalf("serve: %v", err)
	}
	out := w.stderr.String()
	ended, stopped := strings.Index(out, "lifelog: with fake: ended\n"), strings.Index(out, "lifelog: stopped serving")
	if ended < 0 || stopped < 0 || ended > stopped {
		t.Errorf("the child must end before the drain ends:\n%s", out)
	}
	if strings.Contains(out, "ended with status") {
		t.Errorf("the child did not get 200 for its capture:\n%s", out)
	}
	d, err := db.Open(t.Context(), w.db)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := &core.Store{DB: d}
	id, err := store.PageID(t.Context(), "2031-02-03")
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.PageByID(t.Context(), id)
	if err != nil || !strings.Contains(page.Body, "written while the child ended") {
		t.Fatalf("the capture of the child is not on the day page: %+v, %v", page, err)
	}
}
