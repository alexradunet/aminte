package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testEnv is the env of a command line that a test runs in-process: an environment with no variable, a program in a
// folder of its own, the clock of this device, and the output in the two buffers that it returns.
func testEnv(t *testing.T) (e env, stdout, stderr *bytes.Buffer) {
	t.Helper()
	stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
	exe := filepath.Join(t.TempDir(), "aminte")
	return env{
		stdin:      io.NopCloser(strings.NewReader("")),
		stdout:     stdout,
		stderr:     stderr,
		getenv:     noVars,
		executable: func() (string, error) { return exe, nil },
	}, stdout, stderr
}

// noVars is the getenv of an environment with no variable.
func noVars(string) string { return "" }

// serveInProcess runs a command line of serve in-process with e and returns the startup line that it writes to its
// stderr. The serve stops at the end of the test; an error that it returns fails the test.
func serveInProcess(t *testing.T, e env, argv ...string) string {
	t.Helper()
	return startupLine(t, func(ctx context.Context, stderr io.Writer) error {
		e.stderr = stderr
		return runArgs(ctx, e, argv)
	})
}

// startupLine runs serve, which writes its startup line to stderr and returns when ctx is done, and returns that line.
// The lines after it are read and dropped. ctx is done at the end of the test; an error that serve returns fails the
// test.
func startupLine(t *testing.T, serve func(ctx context.Context, stderr io.Writer) error) string {
	t.Helper()
	read, write := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		err := serve(ctx, write)
		write.Close()
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("serve shutdown timed out")
		}
		read.Close()
	})
	line := make(chan string, 1)
	go func() {
		r := bufio.NewReader(read)
		s, _ := r.ReadString('\n')
		line <- s
		// The rest of stderr is read and dropped: a pipe has no buffer, so a later line of serve would wait for a
		// reader until the end of the test. The copy ends when serve or the cleanup closes the pipe.
		_, _ = io.Copy(io.Discard, r)
	}()
	select {
	case s := <-line:
		if s == "" {
			t.Fatal("serve stopped before its startup line")
		}
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("serve startup timed out")
	}
	return ""
}

// servedAt is the scheme and the address of the startup line of serve,
// "aminte: serving PATH on SCHEME://ADDRESS[ (NOTE)] (version V, user_version N)".
func servedAt(t *testing.T, line string) (scheme, address string) {
	t.Helper()
	_, rest, on := strings.Cut(strings.TrimSpace(line), " on ")
	url, _, _ := strings.Cut(rest, " ")
	scheme, address, ok := strings.Cut(url, "://")
	if !on || !ok || address == "" {
		t.Fatalf("serve startup line: %q", line)
	}
	return scheme, address
}

// TestStartupLineReadsTheLinesAfterIt: a serve that writes more lines to its stderr after the startup line does not
// wait for a reader of them, so it stops at the end of the test, at once.
func TestStartupLineReadsTheLinesAfterIt(t *testing.T) {
	stopped := make(chan struct{})
	t.Run("serve", func(t *testing.T) {
		line := startupLine(t, func(ctx context.Context, stderr io.Writer) error {
			defer close(stopped)
			for _, s := range []string{"aminte: serving synthetic.db on http://127.0.0.1:1\n", "a second line\n", "a third line\n"} {
				if _, err := io.WriteString(stderr, s); err != nil {
					return err
				}
			}
			<-ctx.Done()
			return nil
		})
		if line != "aminte: serving synthetic.db on http://127.0.0.1:1\n" {
			t.Errorf("the startup line is %q", line)
		}
	})
	select {
	case <-stopped:
	default:
		t.Error("the serve did not stop at the end of its test")
	}
}
