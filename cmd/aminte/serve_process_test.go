package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"aminte/internal/core"
	"aminte/internal/db"
	"aminte/internal/db/dbtest"
)

// TestExecutableServeDrainsActiveRequest: an interrupt and a SIGTERM (the signal of systemctl stop and restart) both
// stop new work, let an admitted request finish, and end the process with exit 0.
func TestExecutableServeDrainsActiveRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot send os.Interrupt or SIGTERM to another process")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "aminte")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// -cover: the coverage map counts the process (tests/README.md).
	if out, err := exec.CommandContext(ctx, "go", "build", "-cover", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) { drainsOn(ctx, t, binary, sig) })
	}
}

// withoutAminteVars is env without any variable of aminte (AMINTE_DB, AMINTE_TOKEN and the others). A token in the
// environment of a developer turns on the bearer check of the serve, and the plain requests of this test send none.
func withoutAminteVars(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, "AMINTE_") })
}

func drainsOn(ctx context.Context, t *testing.T, binary string, sig os.Signal) {
	live := dbtest.File(t)
	cmd := exec.CommandContext(ctx, binary, "--db", live, "serve", "--addr", "127.0.0.1:0")
	// The program writes its coverage to GOCOVERDIR: the coverage folder of this test binary when a coverage run gives
	// one (tests/README.md), else a folder that the test discards. With no GOCOVERDIR, a program built with -cover
	// writes a warning to its stderr before the startup line.
	coverDir := t.TempDir()
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
		coverDir = f.Value.String()
	}
	cmd.Env = append(withoutAminteVars(os.Environ()), "GOCOVERDIR="+coverDir)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	stderrLines := bufio.NewReader(stderr)
	startup, err := stderrLines.ReadString('\n')
	if err != nil {
		t.Fatalf("server startup: %q, %v", startup, err)
	}
	// The line says which program serves which file schema: the first thing to read at 2 a.m.
	if want := "(version " + version + ", user_version 1)"; !strings.HasSuffix(strings.TrimSpace(startup), want) {
		t.Errorf("server startup: %q, want it to end with %q", startup, want)
	}
	scheme, address := servedAt(t, startup)
	if scheme != "http" {
		t.Fatalf("server startup: %q", startup)
	}
	connection, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	const body = "text=Completed+after+interrupt"
	if _, err := fmt.Fprintf(connection, "POST /days/2031-02-03/capture HTTP/1.1\r\nHost: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\nExpect: 100-continue\r\nConnection: close\r\n\r\n", address, len(body)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusContinue {
		t.Fatalf("handler did not begin reading the request: %s", response.Status)
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	// The listener closing is the observable shutdown barrier. Keep the admitted
	// request's body pending until then; no sleep assumes how fast shutdown runs.
	closedCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	for {
		probe, err := (&net.Dialer{}).DialContext(closedCtx, "tcp", address)
		if closedCtx.Err() != nil {
			t.Fatalf("server did not close its listener: %v", closedCtx.Err())
		}
		if err != nil {
			break
		}
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := io.WriteString(connection, body); err != nil {
		t.Fatalf("shutdown discarded the admitted request: %v", err)
	}
	response, err = http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("shutdown closed the active request before its response: %v", err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("request completion: status=%s read=%v close=%v", response.Status, readErr, closeErr)
	}
	// The stop line follows the drain. The process closes its stderr at exit, so the read ends there.
	rest, err := io.ReadAll(stderrLines)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), "aminte: stopped serving") {
		t.Errorf("stderr after the drain: %q, want the stop line", rest)
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatalf("server exit after successful drain: %v", err)
	}
	database, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := &core.Store{DB: database}
	id, err := store.PageID(ctx, "2031-02-03")
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.PageByID(ctx, id)
	if err != nil || !strings.Contains(page.Body, "Completed after interrupt") {
		t.Fatalf("drained request was not saved: page=%+v error=%v", page, err)
	}
}
