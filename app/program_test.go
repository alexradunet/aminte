package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"lifelog/siren"
)

func TestExitCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"no error", nil, 0},
		{"an interrupt", context.Canceled, 0},
		{"an interrupt that a layer wrapped", fmt.Errorf("serve: %w", context.Canceled), 0},
		{"an error", errors.New("the disk is full"), 1},
		{"a deadline is not an interrupt", context.DeadlineExceeded, 1},
		{"the owner acts first", OwnerActs(errors.New("pair the device")), ExitOwnerActs},
		{"the owner acts first, in a layer", fmt.Errorf("start: %w", OwnerActs(errors.New("pair the device"))), ExitOwnerActs},
		{"the owner acts first, joined with another error", errors.Join(errors.New("close"), OwnerActs(errors.New("pair the device"))), ExitOwnerActs},
		{"nothing to mark", OwnerActs(nil), 0},
	} {
		if got := ExitCode(tc.err); got != tc.want {
			t.Errorf("%s: exit status %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestOwnerActsKeepsTheCause: the mark adds no text and hides no cause, so a caller finds its own error with errors.Is.
func TestOwnerActsKeepsTheCause(t *testing.T) {
	cause := errors.New("not paired")
	err := OwnerActs(cause)
	if err.Error() != "not paired" || !errors.Is(err, cause) {
		t.Errorf("OwnerActs(%q) = %q, errors.Is cause = %v", cause, err, errors.Is(err, cause))
	}
}

func TestFinishPrintsOneLineForAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		stderr string
	}{
		{"no error", nil, 0, ""},
		{"an interrupt", context.Canceled, 0, ""},
		{"an error", errors.New("the disk is full"), 1, "lifelog-demo: the disk is full\n"},
		{"the owner acts first", OwnerActs(errors.New("pair the device")), ExitOwnerActs, "lifelog-demo: pair the device\n"},
	} {
		var stderr bytes.Buffer
		if got := finish("demo", tc.err, &stderr); got != tc.status || stderr.String() != tc.stderr {
			t.Errorf("%s: status %d and %q, want %d and %q", tc.name, got, stderr.String(), tc.status, tc.stderr)
		}
	}
}

// childEnv names what TestMainChild does in the child process.
const childEnv = "LIFELOG_APP_TEST_CHILD"

// TestMainChild is not a test of its own: TestMainEndsTheProcess runs it in a child process, because Main ends the
// process with the exit status.
func TestMainChild(t *testing.T) {
	kind := os.Getenv(childEnv)
	if kind == "" {
		t.Skip("run by TestMainEndsTheProcess")
	}
	Main("demo", func(ctx context.Context) error {
		switch kind {
		case "ok":
			return nil
		case "error":
			return errors.New("the disk is full")
		case "owner":
			return OwnerActs(errors.New("pair the device"))
		case "interrupt":
			fmt.Println("ready")
			<-ctx.Done()
			return ctx.Err()
		}
		return fmt.Errorf("unknown kind %q", kind)
	})
}

// child runs TestMainChild as a process of its own and returns its exit status and its stderr.
func child(t *testing.T, kind string, whileRunning func(*exec.Cmd)) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainChild$")
	cmd.Env = append(os.Environ(), childEnv+"="+kind)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var ready *bufio.Reader
	if whileRunning != nil {
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		ready = bufio.NewReader(out)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	guard := time.AfterFunc(time.Minute, func() { _ = cmd.Process.Kill() }) // a hang is a fault, not a wait
	defer guard.Stop()
	if whileRunning != nil {
		if line, err := ready.ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatalf("the child said %q, %v, want ready", line, err)
		}
		whileRunning(cmd)
		_, _ = ready.WriteTo(&bytes.Buffer{}) // the child's output ends when it ends
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, stderr.String()
	case errors.As(err, &ee):
		return ee.ExitCode(), stderr.String()
	}
	t.Fatalf("the child: %v", err)
	return -1, ""
}

// TestMainEndsTheProcess holds the shell of a program: 0 for no error, 1 for an error, 78 for an error that the owner
// mends, and a line "lifelog-NAME: err" on stderr for each error.
func TestMainEndsTheProcess(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		status int
		stderr string
	}{
		{"ok", 0, ""},
		{"error", 1, "lifelog-demo: the disk is full\n"},
		{"owner", ExitOwnerActs, "lifelog-demo: pair the device\n"},
	} {
		if status, stderr := child(t, tc.kind, nil); status != tc.status || stderr != tc.stderr {
			t.Errorf("%s: exit status %d and stderr %q, want %d and %q", tc.kind, status, stderr, tc.status, tc.stderr)
		}
	}
}

// TestMainEndsCleanlyOnAnInterrupt: SIGTERM cancels the context of run. The program that returns the cancel ends with
// status 0 and no line: the signal is the owner's word.
func TestMainEndsCleanlyOnAnInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a process of Windows gets no SIGTERM")
	}
	status, stderr := child(t, "interrupt", func(cmd *exec.Cmd) {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	})
	if status != 0 || stderr != "" {
		t.Errorf("after SIGTERM: exit status %d and stderr %q, want 0 and none", status, stderr)
	}
}

func TestAPIRefusesAnEmptyAddressWithOneSentence(t *testing.T) {
	t.Setenv("LIFELOG_TOKEN", "the-token")
	c, err := API("", "agent:demo")
	if c != nil || err == nil || !strings.Contains(err.Error(), "no LIFELOG_URL and no --url") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("API with no address = %v, %v, want no client and one sentence that names LIFELOG_URL and --url", c, err)
	}
	if got := ExitCode(err); got != ExitOwnerActs {
		t.Errorf("exit status %d, want %d: a missing address is the owner's to mend", got, ExitOwnerActs)
	}
}

// TestAPISendsTheSourceAndTheTokenOfTheEnvironment: the client writes as the source, and sends LIFELOG_TOKEN as the
// bearer token when it is set and none when it is not.
func TestAPISendsTheSourceAndTheTokenOfTheEnvironment(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		write(w, http.StatusOK, &siren.Entity{Title: "ok"})
	}))
	defer srv.Close()
	for _, tc := range []struct{ token, wantAuth string }{{"the-token", "Bearer the-token"}, {"", ""}} {
		t.Setenv("LIFELOG_TOKEN", tc.token)
		c, err := API(srv.URL, siren.ImportPrefix+"demo")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Get(t.Context(), "/"); err != nil {
			t.Fatal(err)
		}
		if got.Get("Authorization") != tc.wantAuth || got.Get(siren.SourceHeader) != siren.ImportPrefix+"demo" {
			t.Errorf("token %q: headers Authorization %q and %s %q, want %q and %q", tc.token,
				got.Get("Authorization"), siren.SourceHeader, got.Get(siren.SourceHeader), tc.wantAuth, siren.ImportPrefix+"demo")
		}
	}
}
