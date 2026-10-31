// Command lifelog-agent answers the owner's turns of the conversation page with a model. It is a client of the lifelog
// HTTP API and the only program that calls a model. See README.md.
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
	"syscall"

	"lifelog-agent/agent"
	"lifelog/app"
	"lifelog/client"
	"lifelog/siren"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := run(ctx, os.Args[1:], os.Getenv, os.Stderr)
	if err == nil || errors.Is(err, context.Canceled) {
		return // an interrupt ends the agent cleanly: the signal is the owner's word
	}
	fmt.Fprintln(os.Stderr, "lifelog-agent:", err)
	code := 1
	if errors.Is(err, agent.ErrNoAction) {
		code = app.ExitOwnerActs // the owner acts first: a restart of the agent does not help
	}
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	fs := flag.NewFlagSet("lifelog-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// The address is $LIFELOG_URL, which `lifelog agent` and `lifelog serve --with agent` set, or --url. There is no
	// default: a guess such as 127.0.0.1:7777 could be the owner's service, and a stale config with a missing file
	// would then write there.
	base := fs.String("url", getenv("LIFELOG_URL"), "address of lifelog (default $LIFELOG_URL); $LIFELOG_TOKEN is the bearer token")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil // the usage is printed: help is not an error
	} else if err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *base == "" {
		return errors.New("no LIFELOG_URL and no --url: run it as `lifelog agent` on a life.db that exists, or start `lifelog serve` and give --url")
	}
	token := getenv("LIFELOG_TOKEN")
	a := &agent.Agent{
		Client: client.Remote(*base, siren.AgentPrefix+agent.Source, token),
		// The settings hold the key of the model: the read of them is the owner's, with no source.
		AsOwner: client.Remote(*base, "", token),
		Log:     log.New(stderr, "lifelog-agent: ", log.LstdFlags),
	}
	return a.Run(ctx)
}
