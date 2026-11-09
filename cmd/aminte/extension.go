package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"aminte/internal/api"
	"aminte/internal/core"
	"aminte/internal/db"
)

// exitError is the exit status of an extension. The extension already said its own message, so main exits with the
// code and writes nothing more.
type exitError struct{ code int }

func (x exitError) Error() string { return fmt.Sprintf("the extension exited with status %d", x.code) }

// findExtension is the path of the program aminte-NAME, with exe the path of this program. Its folder comes first: the extension that
// the owner installed beside aminte wins over a program of the same name on PATH, which any user may put there.
func findExtension(exe, name string) (string, error) {
	notFound := fmt.Errorf("unknown command %q: no command of aminte and no aminte-%s beside this program or on PATH (aminte help)", name, name)
	if strings.ContainsAny(name, `/\`) { // a name is a word, never a path
		return "", notFound
	}
	beside := filepath.Join(filepath.Dir(exe), "aminte-"+name)
	if path, err := exec.LookPath(beside); err == nil {
		return path, nil
	} else if _, statErr := os.Stat(beside); statErr == nil {
		return "", fmt.Errorf("%s is beside this program but cannot run: %w", beside, err) // a file that is not executable
	}
	if path, err := exec.LookPath("aminte-" + name); err == nil {
		return path, nil
	}
	return "", notFound
}

// extension is `aminte NAME [ARGS...]`: it runs the program aminte-NAME with the words after the name, gives it the
// API of the file on a temporary loopback server (or the --url), waits for it, and ends with its status. The contract
// of an extension is extensions/README.md.
func extension(ctx context.Context, e env, name string, args []string) (err error) {
	switch {
	case slices.Contains(e.o.given, "--source"):
		return errors.New("an extension names its own writer: drop --source")
	case slices.Contains(e.o.given, "--human"):
		return errors.New("an extension prints its own output: drop --human")
	}
	exe, err := program(e)
	if err != nil {
		return err
	}
	path, err := findExtension(exe, name)
	if err != nil {
		return err
	}
	file, err := dbPath(e)
	if err != nil {
		return err
	}
	if file, err = filepath.Abs(file); err != nil {
		return err
	}
	url := e.o.url
	if url == "" {
		if _, statErr := os.Stat(file); statErr == nil {
			stop, addr, serr := serveTemporarily(ctx, e, file)
			if serr != nil {
				return serr
			}
			defer func() {
				serr := stop()
				if serr == nil {
					return
				}
				var exit exitError
				if errors.As(err, &exit) { // main says nothing of an exitError: the file's error is said here
					fmt.Fprintln(e.stderr, "aminte:", serr)
					return
				}
				err = errors.Join(err, serr)
			}()
			url = "http://" + addr
		}
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.stdin, e.stdout, e.stderr
	cmd.Env = extensionEnv(os.Environ(), exe, file, url)
	interruptOnCancel(cmd)
	err = cmd.Run()
	if errors.Is(err, context.Canceled) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		return nil // the interrupt was the owner's word, and the extension ended cleanly on it
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return exitError{code: max(ee.ExitCode(), 1)} // -1: a signal ended it
	}
	return err
}

// interruptOnCancel makes the end of the context of cmd an interrupt of the extension, and gives it ten seconds to end
// before the kill.
func interruptOnCancel(cmd *exec.Cmd) {
	if runtime.GOOS != "windows" {
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	} else {
		// Windows cannot send an interrupt to another process, and the console gives the extension the Ctrl-C itself.
		// So Cancel does nothing, and WaitDelay gives the extension the same ten seconds to end as on Unix.
		cmd.Cancel = func() error { return nil }
	}
	cmd.WaitDelay = 10 * time.Second
}

// extensionEnv is the environment of the extension: the inherited one, with the three variables of the contract set
// and a AMINTE_URL that it inherited removed when there is no server for it.
func extensionEnv(inherited []string, exe, file, url string) []string {
	env := slices.DeleteFunc(slices.Clone(inherited), func(kv string) bool {
		return strings.HasPrefix(kv, "AMINTE_URL=") || strings.HasPrefix(kv, "AMINTE_EXE=") || strings.HasPrefix(kv, "AMINTE_DB=")
	})
	env = append(env, "AMINTE_EXE="+exe, "AMINTE_DB="+file)
	if url != "" {
		env = append(env, "AMINTE_URL="+url)
	}
	return env
}

// serveTemporarily opens the file and serves it on a loopback port, as serve does for a local listener. It lives until
// stop is called; stop drains the server, waits for it and closes the file. The server does not follow ctx: an
// interrupt reaches the extension, and the extension may still need the API while it ends.
func serveTemporarily(ctx context.Context, e env, file string) (stop func() error, addr string, err error) {
	d, err := db.Open(ctx, file)
	if err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", errors.Join(err, d.Close())
	}
	handler, err := api.NetworkAuthority(listener.Addr(), api.NewWith(&core.Store{DB: d, Clock: e.clock}, nil))
	if err != nil {
		return nil, "", errors.Join(err, listener.Close(), d.Close())
	}
	srv := &http.Server{Addr: listener.Addr().String(), Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	srvCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan error, 1)
	go func() { done <- serveHTTP(srvCtx, srv, listener, 10*time.Second) }()
	return func() error {
		cancel()
		return errors.Join(<-done, d.Close())
	}, listener.Addr().String(), nil
}
