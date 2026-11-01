package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"lifelog/client"
)

// The shell of an extension program: the signal, the last line, the exit status and the client of the API. Every Go
// extension has the same shell, so it is written here once (extensions/README.md, "Exit and signals").

// ownerActs is the error that OwnerActs makes.
type ownerActs struct{ err error }

func (e *ownerActs) Error() string { return e.err.Error() }
func (e *ownerActs) Unwrap() error { return e.err }

// OwnerActs marks err as a fault that only the owner can mend: a value of the configuration that is wrong, a device
// that is not paired, a login that is missing. Another run would fail the same way. Main ends the program with
// ExitOwnerActs, and `serve` does not start it again. The text of err does not change. OwnerActs(nil) is nil.
func OwnerActs(err error) error {
	if err == nil {
		return nil
	}
	return &ownerActs{err}
}

// ExitCode is the exit status that Main gives the program for the error that run returned: 0 for no error and for
// context.Canceled (an interrupt is the owner's word), ExitOwnerActs for an error that OwnerActs marked (also one that
// a layer wrapped or joined), 1 for any other error. A program with a test of its own for an exit status asks here.
func ExitCode(err error) int {
	var o *ownerActs
	switch {
	case err == nil || errors.Is(err, context.Canceled):
		return 0
	case errors.As(err, &o):
		return ExitOwnerActs
	}
	return 1
}

// Main is the main function of a program lifelog-NAME. It runs run with a context that ends on an interrupt or on
// SIGTERM, prints the line "lifelog-NAME: err" on stderr when run fails, and ends the process with ExitCode of the
// error. A program that ends with no error returns from Main.
func Main(name string, run func(ctx context.Context) error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop() // os.Exit does not run a defer
	if code := finish(name, err, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

// finish prints the last line of a program that failed and gives its exit status.
func finish(name string, err error, stderr io.Writer) int {
	code := ExitCode(err)
	if code != 0 {
		fmt.Fprintf(stderr, "lifelog-%s: %v\n", name, err)
	}
	return code
}

// API is the client of the lifelog server at url, for the program that writes as source (for example
// siren.AgentPrefix+"agent", or siren.ImportPrefix+"sheets"; "" is the owner). It sends LIFELOG_TOKEN as the bearer
// token when the variable is set. It refuses an empty url: a program never guesses an address, because
// 127.0.0.1:7777 could be the server of another file (extensions/README.md). The refusal is OwnerActs.
func API(url, source string) (*client.Client, error) {
	if url == "" {
		return nil, OwnerActs(errors.New("no LIFELOG_URL and no --url: run it as `lifelog NAME` on a life.db that exists, or start `lifelog serve` and give --url"))
	}
	return client.Remote(url, source, os.Getenv("LIFELOG_TOKEN")), nil
}
