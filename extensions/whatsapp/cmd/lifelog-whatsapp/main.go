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
	"sync"
	"time"

	"lifelog-whatsapp/relay"
	"lifelog-whatsapp/whatsapp"
	"lifelog/app"
)

// The exit status is the one of app.Main: 0 for an interrupt, 1 for an error (a new process may try again), 78 when
// the owner must act first (the device is not paired, the connection ended for good, the server has no say).
func main() {
	app.Main("whatsapp", func(ctx context.Context) error { return run(ctx, os.Args[1:], os.Stdout, os.Stderr) })
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "login" {
		if len(args) > 1 {
			return fmt.Errorf("login takes no argument, got %q", args[1])
		}
		return login(ctx, stdout)
	}
	fs := flag.NewFlagSet("lifelog-whatsapp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// The address is $LIFELOG_URL, which `lifelog whatsapp` sets, or --url. There is no default: app.API refuses an
	// empty address. $LIFELOG_TOKEN is the bearer token.
	base := fs.String("url", os.Getenv("LIFELOG_URL"), "address of lifelog (default $LIFELOG_URL); $LIFELOG_TOKEN is the bearer token")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil // the usage is printed: help is not an error
	} else if err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	// The owner's words go to the page with say, and the owner's number is in the settings: both are the owner's, so
	// the client has the owner's token and no source.
	api, err := app.API(*base, "")
	if err != nil {
		return err
	}
	dir, err := app.StateDir("whatsapp")
	if err != nil {
		return fmt.Errorf("state folder: %w", err)
	}
	sessionPath, statePath, err := whatsapp.Prepare(dir)
	if err != nil {
		return err
	}
	logger := log.New(stderr, "lifelog-whatsapp: ", log.LstdFlags)
	r := &relay.Relay{API: api, State: statePath, Log: logger}
	return serve(ctx, r, sessionPath, logger)
}

func login(ctx context.Context, stdout io.Writer) (err error) {
	dir, err := app.StateDir("whatsapp")
	if err != nil {
		return fmt.Errorf("state folder: %w", err)
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

// endError is the error of a connection that ended for good. The owner acts first when the device is logged out,
// replaced, banned or outdated: a new process would end the same way. A failure that Retry marks may pass.
func endError(ended whatsapp.Ended) error {
	err := errors.New(ended.Reason)
	if ended.Retry {
		return err
	}
	return app.OwnerActs(err)
}

func serve(ctx context.Context, r *relay.Relay, sessionPath string, logger *log.Logger) (err error) {
	s, err := whatsapp.Open(ctx, sessionPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	if s.Number() == "" {
		return whatsapp.ErrNotPaired
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
		return endError(*ended)
	}
	return runErr
}
