// Command lifelog-whatsapp is an always-on agent for lifelog: the owner talks to it on WhatsApp (or on a terminal),
// a model answers and calls the actions of the lifelog API. It is a client of the lifelog HTTP API. See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"lifelog-whatsapp/agent"
	"lifelog-whatsapp/terminal"
	"lifelog-whatsapp/whatsapp"
	"lifelog/app"
	"lifelog/client"
	"lifelog/siren"
)

// Exit codes: 0 an interrupt or the end of the input, 1 an error (a new process may try again), app.ExitOwnerActs
// (78) not paired, 3 the connection ended for good (logged out, replaced, banned, outdated: the owner acts first).
const (
	exitError     = 1
	exitNotPaired = app.ExitOwnerActs
	exitEnded     = 3
)

// exitCode carries the exit status of the process with the error.
type exitCode struct {
	code int
	err  error
}

func (e *exitCode) Error() string { return e.err.Error() }
func (e *exitCode) Unwrap() error { return e.err }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	if err == nil || errors.Is(err, context.Canceled) {
		return // an interrupt ends the agent cleanly: the signal is the owner's word
	}
	fmt.Fprintln(os.Stderr, "lifelog-whatsapp:", err)
	code := exitError
	var ec *exitCode
	if errors.As(err, &ec) {
		code = ec.code
	}
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.ReadCloser, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "login" {
		if len(args) > 1 {
			return fmt.Errorf("login takes no argument, got %q", args[1])
		}
		return login(ctx, stdout)
	}
	fs := flag.NewFlagSet("lifelog-whatsapp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// The address is $LIFELOG_URL, which `lifelog whatsapp` sets, or --url. There is no default: a guess such as
	// 127.0.0.1:7777 could be the owner's service, and a stale config with a missing file would then write there.
	base := fs.String("url", getenv("LIFELOG_URL"), "address of lifelog (default $LIFELOG_URL)")
	tokenFile := fs.String("token-file", "", "a file with the bearer token ($LIFELOG_TOKEN wins when it is set)")
	terminalMode := fs.Bool("terminal", false, "answer on this terminal, not on WhatsApp")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil // the usage is printed: help is not an error
	} else if err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *base == "" {
		return errors.New("no LIFELOG_URL and no --url: run it as `lifelog whatsapp` on a life.db that exists, or start `lifelog serve` and give --url")
	}
	token := getenv("LIFELOG_TOKEN")
	if token == "" && *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			return fmt.Errorf("token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	dir, err := whatsapp.StateDir()
	if err != nil {
		return err
	}
	sessionPath, statePath, err := whatsapp.Prepare(dir)
	if err != nil {
		return err
	}
	logger := log.New(stderr, "lifelog-whatsapp: ", log.LstdFlags)
	a := &agent.Agent{
		Client: client.Remote(*base, siren.AgentPrefix+agent.Source, token),
		// The settings hold the key of the model and the owner's number, and the phone's message is the owner's
		// turn: both go as the owner, with no source.
		AsOwner: client.Remote(*base, "", token),
		State:   statePath,
		Log:     logger,
	}
	if *terminalMode {
		a.Owner = terminal.Sender
		return serveTerminal(ctx, a, stdin, stdout)
	}
	return serveWhatsApp(ctx, a, sessionPath, logger)
}

func login(ctx context.Context, stdout io.Writer) (err error) {
	dir, err := whatsapp.StateDir()
	if err != nil {
		return err
	}
	sessionPath, _, err := whatsapp.Prepare(dir)
	if err != nil {
		return err
	}
	s, err := whatsapp.Open(ctx, sessionPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	if err := whatsapp.Login(ctx, s, stdout); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Paired. Start the agent with: lifelog whatsapp")
	return nil
}

func serveTerminal(ctx context.Context, a *agent.Agent, stdin io.ReadCloser, stdout io.Writer) (err error) {
	chat := terminal.New(ctx, stdin, stdout, a.Owner, time.Now)
	defer func() { err = errors.Join(err, chat.Close()) }()
	a.Chat = chat
	return a.Run(ctx)
}

func serveWhatsApp(ctx context.Context, a *agent.Agent, sessionPath string, logger *log.Logger) (err error) {
	s, err := whatsapp.Open(ctx, sessionPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	if s.Number() == "" {
		return &exitCode{exitNotPaired, whatsapp.ErrNotPaired}
	}
	// A message is answered when it is newer than the earlier of the start and the last save of the page: an
	// offline message is answered once, and a restart does not answer what the page holds. The start is taken
	// before the wait for the number, so a message sent during that wait is answered too.
	after := time.Now()
	if saved, ok, err := agent.LastSave(a.State); err != nil {
		return err
	} else if ok && saved.Before(after) {
		after = saved
	}
	// The number comes from the settings, before the connection. A number that is set is used for the whole run.
	if a.Owner, err = agent.WaitOwner(ctx, a.AsOwner, logger, nil); err != nil {
		return err
	}
	chat, err := whatsapp.Connect(ctx, s, a.Owner, after, logger)
	if err != nil {
		return err
	}
	defer chat.Close()
	a.Chat = chat

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var ended *whatsapp.Ended
	wg.Add(1)
	go func() { // ends the run when the connection ends for good: the unit restarts it, or stops
		defer wg.Done()
		select {
		case e := <-chat.Ended():
			ended = &e
			cancel()
		case <-runCtx.Done():
		}
	}()
	runErr := a.Run(runCtx)
	cancel()
	wg.Wait()
	if ended != nil {
		code := exitEnded
		if ended.Retry {
			code = exitError
		}
		return &exitCode{code, errors.New(ended.Reason)}
	}
	return runErr
}
