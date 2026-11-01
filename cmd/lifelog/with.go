package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"lifelog/app"
	"lifelog/internal/api"
)

// restartDelay is the wait before serve starts a failed extension again (extensions/README.md, run by serve). A test
// shortens it.
var restartDelay = 10 * time.Second

// probeEvery is the wait between two probes of the catalog of a child that is not up yet. The wait is on another
// process, and nothing signals that it is ready, so it is a poll. probeFor is the longest time a child has to serve
// its catalog after it started; after it, the child is taken as an app that serves nothing. A test shortens it.
const probeEvery = 250 * time.Millisecond

var probeFor = 30 * time.Second

// startWith starts a child for each --with of the line, after the listener is up, and gives the context that
// serveHTTP follows. That context ends only after the children ended: on ctx.Done the children get an interrupt and
// are awaited, then the server drains. wait ends the children (if serveHTTP ended by itself) and joins every
// goroutine. With no --with, the context is ctx and wait does nothing.
func startWith(ctx context.Context, e env, listen net.Addr, apps *api.Apps) (serveCtx context.Context, wait func()) {
	paths := e.o.serve.withPaths
	if len(paths) == 0 {
		return ctx, func() {}
	}
	url := "http://" + listen.String() // the listener is on a loopback address (api.LoopbackAddress): a child reaches it there
	childCtx, endChildren := context.WithCancel(ctx)
	serveCtx, endServe := context.WithCancel(context.WithoutCancel(ctx))
	var children sync.WaitGroup
	for i, name := range e.o.serve.with {
		children.Add(1)
		go func() {
			defer children.Done()
			superviseWith(childCtx, e, name, paths[i], url, apps)
		}()
	}
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		<-childCtx.Done()
		children.Wait()
		endServe()
	}()
	return serveCtx, func() {
		endChildren()
		<-joined
		endServe()
	}
}

// withEnv is the environment of a child of serve: the one of any extension, and the token of this server. A
// LIFELOG_TOKEN that the child could inherit is removed: the child gets the token of this server or none.
func withEnv(e env, exe, file, url string) []string {
	env := slices.DeleteFunc(extensionEnv(os.Environ(), exe, file, url), func(kv string) bool {
		return strings.HasPrefix(kv, "LIFELOG_TOKEN=") || strings.HasPrefix(kv, "LIFELOG_LISTEN=") // runWithOnce sets the address
	})
	if token := e.o.serve.token; token != "" {
		env = append(env, "LIFELOG_TOKEN="+token)
	}
	return env
}

// superviseWith runs the extension at path until it ends for good or ctx ends, by the rule of extensions/README.md:
// exit 0 is an end by its own choice; 78 (app.ExitOwnerActs) wants the owner first; any other end, 2 of a panic too,
// starts it again after restartDelay.
func superviseWith(ctx context.Context, e env, name, path, url string, apps *api.Apps) {
	say := func(format string, a ...any) {
		fmt.Fprintf(e.stderr, "lifelog: with %s: %s\n", name, fmt.Sprintf(format, a...))
	}
	exe, err := program(e)
	if err == nil {
		var file string
		if file, err = filepath.Abs(e.o.db); err == nil {
			err = runWith(ctx, e, name, path, withEnv(e, exe, file, url), apps, say)
		}
	}
	if err != nil {
		say("not run: %v", err)
	}
}

func runWith(ctx context.Context, e env, name, path string, env []string, apps *api.Apps, say func(string, ...any)) error {
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer stdin.Close()
	for {
		again, err := runWithOnce(ctx, e, name, path, env, apps, stdin, say)
		if err != nil {
			say("not started: %v, started again in %s", err, restartDelay)
		} else if !again {
			return nil
		}
		t := time.NewTimer(restartDelay)
		select {
		case <-ctx.Done():
			t.Stop()
			say("ended")
			return nil
		case <-t.C:
		}
	}
}

// freeLoopbackAddr is a loopback address that nothing listens on now: the port is taken from the system and given
// back at once. The child binds it a moment later. Another process may take the port in between; then the child
// fails to bind, exits, and the restart rule starts it again with a new port.
func freeLoopbackAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	return addr, l.Close()
}

// runWithOnce runs the extension once and says how it ended. again tells whether the rule starts it again.
//
// The child gets LIFELOG_LISTEN, the address it may serve a catalog on (extensions/README.md). The address is new for
// each start: a restart never meets the address of the child that died. The server does not read the stdout of a
// child (it is the log of the server, shared by all children) and does not trust an address that a child writes: it
// chose the address and it probes that one. A child that serves nothing ignores the variable.
func runWithOnce(ctx context.Context, e env, name, path string, env []string, apps *api.Apps, stdin *os.File, say func(string, ...any)) (again bool, err error) {
	addr, err := freeLoopbackAddr()
	if err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = stdin, e.stdout, e.stderr, append(slices.Clone(env), "LIFELOG_LISTEN="+addr)
	interruptOnCancel(cmd)
	if err := cmd.Start(); err != nil {
		return false, err
	}
	say("started (pid %d)", cmd.Process.Pid)
	// The probe goroutine belongs to this start. It ends when the probe has an answer, when probeFor has passed, or
	// when probeCtx ends; this function waits for it before it removes the app, so a late Register cannot follow Remove.
	probeCtx, endProbe := context.WithCancel(ctx)
	var probe sync.WaitGroup
	probe.Add(1)
	go func() {
		defer probe.Done()
		probeCatalog(probeCtx, name, addr, apps, say)
	}()
	err = cmd.Wait()
	endProbe()
	probe.Wait()
	apps.Remove(name)
	clean := cmd.ProcessState != nil && cmd.ProcessState.Success() // Wait gives the context's error for a clean end after Cancel
	switch {
	case err == nil || ctx.Err() != nil && clean: // the extension's own choice, or a clean end on the interrupt of the stop
		say("ended")
		return false, nil
	case ctx.Err() != nil: // the stop: the extension did not end cleanly on the interrupt, or was killed after the wait
		say("ended by the stop: %v", err)
		return false, nil
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() < 0 { // Wait failed, or a signal ended it
		say("ended by a signal (%v), started again in %s", err, restartDelay)
		return true, nil
	}
	status := ee.ExitCode()
	if status == app.ExitOwnerActs {
		say("ended with status %d: not started again (the owner acts first)", status)
		return false, nil
	}
	say("ended with status %d, started again in %s", status, restartDelay)
	return true, nil
}

// probeCatalog registers the child at addr (api.Apps.Register) once it is up, and says one line for the outcome. The
// handshake is the registry's; this function only repeats it while the child does not answer, and writes the line. It
// never says a line for a probe. It returns when ctx ends (the child ended) without a line.
func probeCatalog(ctx context.Context, name, addr string, apps *api.Apps, say func(string, ...any)) {
	deadline := time.NewTimer(probeFor)
	defer deadline.Stop()
	for {
		got, err := apps.Register(ctx, name, "http://"+addr)
		switch {
		case ctx.Err() != nil:
			return
		case err == nil && got.Tasks:
			say("the task service, %d lists, on %s", got.Lists, addr)
			return
		case err == nil:
			say("a catalog of %d actions on %s", got.Actions, addr)
			return
		case !notUpYet(err):
			say("%v", err)
			return
		}
		t := time.NewTimer(probeEvery)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-deadline.C:
			t.Stop()
			say("serves no catalog")
			return
		case <-t.C:
		}
	}
}

// notUpYet says that the handshake failed because nothing answers at the address yet: the dial was refused, or the
// answer was late.
func notUpYet(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial" || errors.Is(err, context.DeadlineExceeded)
}
