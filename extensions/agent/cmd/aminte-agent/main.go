// Command aminte-agent answers the owner's turns of the conversation page with a model. It is a client of the aminte
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

	"aminte-agent/agent"
	"aminte/app"
	"aminte/siren"
)

func main() {
	app.Main("agent", func(ctx context.Context) error { return run(ctx, os.Args[1:], os.Stderr) })
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("aminte-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// The address is $AMINTE_URL, which `aminte agent` and `aminte serve --with agent` set, or --url. There is no
	// default: app.API refuses an empty address. $AMINTE_TOKEN is the bearer token.
	base := fs.String("url", os.Getenv("AMINTE_URL"), "address of aminte (default $AMINTE_URL); $AMINTE_TOKEN is the bearer token")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil // the usage is printed: help is not an error
	} else if err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	api, err := app.API("aminte agent", *base, siren.AgentPrefix+agent.Source)
	if err != nil {
		return err
	}
	a := &agent.Agent{
		Client: api,
		// The settings hold the key of the model: the read of them is the owner's, with no source.
		AsOwner: api.As(""),
		Log:     log.New(stderr, "aminte-agent: ", log.LstdFlags),
	}
	return a.Run(ctx)
}
