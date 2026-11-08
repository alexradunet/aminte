// Command aminte-mcp is the MCP server of aminte: the actions of the API as MCP tools on stdio. It is a client of
// the aminte HTTP API. See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"aminte-mcp/mcp"
	"aminte/app"
	"aminte/siren"
)

// version is the version of this extension. It starts equal to the version of aminte, and the two change apart.
const version = "0.1.0"

func main() {
	app.Main("mcp", func(ctx context.Context) error { return run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr) })
}

func run(ctx context.Context, args []string, stdin io.ReadCloser, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("aminte-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "mcp", "the name of the agent: the server writes as agent:NAME")
	// The address is $AMINTE_URL, which `aminte mcp` sets, or --url. There is no default: app.API refuses an empty
	// address. $AMINTE_TOKEN is sent as a bearer token when it is set.
	base := fs.String("url", os.Getenv("AMINTE_URL"), "address of aminte (default $AMINTE_URL); $AMINTE_TOKEN is sent as a bearer token when it is set")
	fs.String("source", "", "refused: name the agent with --agent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	named := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { named[f.Name] = true })
	if named["source"] { // one way to name the writer of MCP: an agent's
		return errors.New("mcp writes as agent:NAME: name the agent with --agent, not --source")
	}
	c, err := app.API("aminte mcp", *base, siren.AgentPrefix+*agent)
	if err != nil {
		return err
	}
	s, err := mcp.New(ctx, c, version)
	if err != nil {
		return fmt.Errorf("%w (run it as `aminte mcp`, or start `aminte serve` and give --url)", err)
	}
	if err := mcp.Run(ctx, s, stdin, stdout); err != nil && !errors.Is(err, context.Canceled) {
		return err // an interrupt ends the session cleanly: the signal is the owner's word
	}
	return nil
}
