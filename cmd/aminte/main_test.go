package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheCommandTableDecidesTheFlagsTheUsageAndTheInterrupt: a flag a command does not take is refused, never
// ignored: --dry-run belongs to file, --token-file to serve. The usage names every command of the table. A setup command
// of the machine (init) keeps the OS's interrupt, as does a line with no command; a name that is no command is an extension, which gets the interrupt through its context and passes it on; every other command takes the interrupt through its context, snapshot too, as it runs through the
// client since plan 082.
func TestTheCommandTableDecidesTheFlagsTheUsageAndTheInterrupt(t *testing.T) {
	for _, argv := range [][]string{{"search", "x", "--dry-run"}, {"day", "--token-file", "t"}, {"search", "x", "--day", "2026-01-01"}, {"capture", "hello", "--snapshots", "x"}} {
		if _, err := parse(argv, noVars); err == nil || !strings.Contains(err.Error(), "does not take") {
			t.Errorf("%v: %v, want a refusal of the flag", argv, err)
		}
	}
	for _, argv := range [][]string{{"file", "x", "--dry-run"}, {"serve", "--token-file", "t"}, {"capture", "hello", "--day", "2026-01-01"}, {"snapshot", "--snapshots", "x"}} {
		if _, err := parse(argv, noVars); err != nil {
			t.Errorf("%v: %v, want the flag taken", argv, err)
		}
	}
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"init", "--url", "http://x"}, "init does not take --url"},
		{[]string{"init", "--source", "x"}, "init does not take --source"},
		{[]string{"serve", "--human"}, "serve does not take --human"},
		{[]string{"day", "--db", "a.db", "--url", "http://x"}, "--db and --url name two databases: give one"},
	} {
		if _, err := parse(tc.argv, noVars); err == nil || err.Error() != tc.want {
			t.Errorf("%v: %v, want %q", tc.argv, err, tc.want)
		}
	}
	for _, argv := range [][]string{{"init", "--db", "a.db"}, {"day", "--url", "http://x", "--human"}} {
		if _, err := parse(argv, noVars); err != nil {
			t.Errorf("%v: %v, want the flags taken", argv, err)
		}
	}
	usage := usageText()
	for _, c := range commands {
		if c.usage != "" && !strings.Contains(usage, "aminte "+c.name) {
			t.Errorf("the usage does not name %s", c.name)
		}
	}
	for _, args := range [][]string{nil, {"init"}, {"help"}} {
		if commandConsumesContext(opts{args: args}) {
			t.Errorf("%v takes the interrupt through its context; want the OS's interrupt", args)
		}
	}
	for _, cmd := range []string{"serve", "get", "actions", "do", "capture", "day", "page", "search", "rename", "file", "cover", "snapshot", "an-extension"} {
		if !commandConsumesContext(opts{args: []string{cmd}}) {
			t.Errorf("%s must take the interrupt through its context", cmd)
		}
	}
}

// TestAnUnknownCommandIsAnErrorAndNotTheUsage: a command that is not in the table is the error "unknown command", not a
// usageError, so main writes "aminte: unknown command ..." and exits 1. A line with no command, the line help and a
// line that parse refuses are a usageError, so main writes the refusal (if there is one) and the usage, and exits 2.
// runArgs itself writes nothing.
func TestAnUnknownCommandIsAnErrorAndNotTheUsage(t *testing.T) {
	for _, tc := range []struct {
		argv  []string
		usage bool   // main writes the usage and exits 2; else it exits 1
		want  string // the error that main writes; "" for none
	}{
		{[]string{"unknown"}, false, `unknown command "unknown": no command of aminte and no aminte-unknown beside this program or on PATH (aminte help)`},
		{[]string{"unknown", "--dry-run"}, false, `unknown command "unknown": no command of aminte and no aminte-unknown beside this program or on PATH (aminte help)`},
		{nil, true, ""},
		{[]string{"help"}, true, ""},
		{[]string{"search", "x", "--dry-run"}, true, "search does not take --dry-run"},
		{[]string{"--bogus"}, true, "unknown flag --bogus"},
	} {
		e, stdout, stderr := testEnv(t)
		err := runArgs(t.Context(), e, tc.argv)
		var u usageError
		usage := errors.As(err, &u)
		var said string
		switch {
		case usage && u.err != nil:
			said = u.err.Error()
		case !usage && err != nil:
			said = err.Error()
		}
		if err == nil || usage != tc.usage || said != tc.want {
			t.Errorf("%v: %v (a usage error: %v), want %q (a usage error: %v)", tc.argv, err, usage, tc.want, tc.usage)
		}
		if stdout.Len() != 0 || stderr.Len() != 0 {
			t.Errorf("%v: runArgs wrote %q and %q, want nothing", tc.argv, stdout, stderr)
		}
	}
}

func TestCommandCancellation(t *testing.T) {
	for _, args := range [][]string{{"get", "blocked"}, {"capture", "synthetic"}, {"search", "synthetic"}, {"actions"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					close(stopped)
				case <-release:
				}
			}))
			defer srv.Close()
			// This independent escape must run before server shutdown even on Fatal.
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			e, _, _ := testEnv(t)
			e.o = opts{args: args, url: srv.URL, source: "cli"}
			go func() { done <- runContext(ctx, e) }()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("not started")
			}
			cancel()
			select {
			case <-stopped:
			case <-time.After(10 * time.Second):
				t.Fatal("handler not canceled")
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("command did not return after cancellation")
			}
		})
	}
}

// aminte init PATH makes the file at PATH, whatever $AMINTE_DB names; --db beats PATH, and both together are a
// mistake, refused before anything is written (docs/plans/098-the-codebase-made-cohesive.md).
func TestInitHonoursThePathWhenTheEnvironmentNamesAnother(t *testing.T) {
	named := filepath.Join(t.TempDir(), "env.db")
	e, _, _ := testEnv(t)
	e.getenv = func(name string) string { return map[string]string{"AMINTE_DB": named}[name] }
	given := filepath.Join(t.TempDir(), "given.db")
	if err := runArgs(t.Context(), e, []string{"init", given}); err != nil {
		t.Fatalf("init PATH: %v", err)
	}
	if _, err := os.Stat(given); err != nil {
		t.Errorf("init PATH did not make %s: %v", given, err)
	}
	if _, err := os.Stat(named); err == nil {
		t.Errorf("init PATH made the file $AMINTE_DB names, %s", named)
	}

	other := filepath.Join(t.TempDir(), "other.db")
	if err := runArgs(t.Context(), e, []string{"init", filepath.Join(t.TempDir(), "unused.db"), "--db", other}); err == nil || !strings.Contains(err.Error(), "--db") {
		t.Errorf("init PATH --db OTHER is refused: %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Errorf("a refused init made %s", other)
	}
}

// TestAUsageErrorNamesTheCommandAndARepeatedFieldIsRefused: the error of use is the first line of the usage of the
// command, from the table; `do` refuses a field given twice instead of keeping the last one.
func TestAUsageErrorNamesTheCommandAndARepeatedFieldIsRefused(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"rename", "1"}, "rename PAGE-ID NEW-TITLE"},
		{[]string{"get"}, "get PATH"},
		{[]string{"file"}, "file PATH [--title T]"},
		{[]string{"do", "capture", "text=a", "text=b"}, "text is given twice"},
	} {
		e, _, _ := testEnv(t)
		file := filepath.Join(t.TempDir(), "x.db")
		if err := runArgs(t.Context(), e, []string{"init", file}); err != nil {
			t.Fatal(err)
		}
		err := runArgs(t.Context(), e, append(tc.argv, "--db", file))
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%v: %v, want %q", tc.argv, err, tc.want)
		}
	}
}

// TestAShortcutRefusesAnArgumentItDoesNotRead: a command that reads no argument, or one at most, refuses another with
// its synopsis (AGENTS.md, "Refused, never ignored"). Nothing runs: no output, no copy of the file, no listener.
func TestAShortcutRefusesAnArgumentItDoesNotRead(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "life.db")
	e, _, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"init", live}); err != nil {
		t.Fatal(err)
	}
	copies := filepath.Join(dir, "copies")
	if err := os.Mkdir(copies, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"day", []string{"day", "2031-05-20", "extra"}},
		{"snapshot", []string{"snapshot", "extra", "--snapshots", copies}},
		{"actions", []string{"actions", "extra"}},
		{"serve", []string{"serve", "extra", "--addr", "127.0.0.1:0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, stdout, _ := testEnv(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			err := runArgs(ctx, e, append(tc.argv, "--db", live))
			if want := commandNamed(tc.name).synopsis(); err == nil || err.Error() != want {
				t.Errorf("%v: error %v, want %q", tc.argv, err, want)
			}
			if stdout.Len() != 0 {
				t.Errorf("%v: wrote %q, want nothing", tc.argv, stdout.String())
			}
			if left, _ := os.ReadDir(copies); len(left) != 0 {
				t.Errorf("%v: left %d file(s) in the snapshot folder", tc.argv, len(left))
			}
		})
	}
}

// TestTheUsageOfServeSaysWhatServeTakes: serve takes a loopback address, a snapshot folder, a token file and the
// extensions to run. The usage says that a token turns on the bearer check, and names no flag of the network that the
// core had (--public, --allow-origin, --tls-cert, --tls-key) and no HTML.
func TestTheUsageOfServeSaysWhatServeTakes(t *testing.T) {
	usage := usageText()
	for _, want := range []string{
		"  aminte serve [--addr 127.0.0.1:7777] [--snapshots DIR] [--token-file PATH]\n",
		"the API: Siren JSON, on a loopback address; a token (--token-file PATH or\n",
		"$AMINTE_TOKEN) turns on the bearer check\n",
		"  aminte serve ... --with NAME       also run the extension aminte-NAME as a child, and start it again when it fails\n",
		`--url URL      talk to a running "aminte serve" instead of opening the file ($AMINTE_TOKEN if it has a token)`,
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("the usage lacks %q", want)
		}
	}
	for _, bad := range []string{"--public", "--allow-origin", "--tls-cert", "--tls-key", "HTML", "browser"} {
		if strings.Contains(usage, bad) {
			t.Errorf("the usage still says %q", bad)
		}
	}
}
