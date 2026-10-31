// Command lifelog-whatsapp is the transport between WhatsApp and lifelog: the owner talks to lifelog on WhatsApp, and
// the answer of lifelog-agent comes back to the phone. It is a client of the lifelog HTTP API and calls no model. See
// README.md.
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

	"lifelog-whatsapp/relay"
	"lifelog-whatsapp/whatsapp"
	"lifelog/app"
	"lifelog/client"
)

// Exit codes: 0 an interrupt, 1 an error (a new process may try again), app.ExitOwnerActs (78) not paired or a
// server with no say, 3 the connection ended for good (logged out, replaced, banned, outdated: the owner acts first).
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
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	if err == nil || errors.Is(err, context.Canceled) {
		return // an interrupt ends the relay cleanly: the signal is the owner's word
	}
	fmt.Fprintln(os.Stderr, "lifelog-whatsapp:", err)
	code := exitError
	var ec *exitCode
	if errors.As(err, &ec) {
		code = ec.code
	} else if errors.Is(err, relay.ErrNoAction) {
		code = exitNotPaired // the owner acts first, as for a pairing: a restart does not help
	}
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
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
	// The owner's words go to the page with say, and the owner's number is in the settings: both are the owner's, so
	// the client has the owner's token and no source.
	r := &relay.Relay{API: client.Remote(*base, "", token), State: statePath, Log: logger}
	return serve(ctx, r, sessionPath, logger)
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
	fmt.Fprintln(stdout, "Paired. Start the relay with: lifelog serve --with whatsapp")
	return nil
}

func serve(ctx context.Context, r *relay.Relay, sessionPath string, logger *log.Logger) (err error) {
	s, err := whatsapp.Open(ctx, sessionPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	if s.Number() == "" {
		return &exitCode{exitNotPaired, whatsapp.ErrNotPaired}
	}
	// A message is said when it is newer than the earlier of the start and the last message that was said: a message
	// that came while the relay was off is said once, and a restart does not say it again. The start is taken before
	// the wait for the number, so a message sent during that wait is said too.
	after := time.Now()
	if said, ok, err := relay.LastSaid(r.State); err != nil {
		return err
	} else if ok && said.Before(after) {
		after = said
	}
	// The number comes from the settings, before the connection. A number that is set is used for the whole run.
	if r.Owner, err = relay.WaitOwner(ctx, r.API, logger, nil); err != nil {
		return err
	}
	chat, err := whatsapp.Connect(ctx, s, r.Owner, after, logger)
	if err != nil {
		return err
	}
	defer chat.Close()
	r.Chat = chat

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
	runErr := r.Run(runCtx)
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
