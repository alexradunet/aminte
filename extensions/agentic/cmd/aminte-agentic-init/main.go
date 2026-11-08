// Command aminte-agentic-init is the extension behind `aminte agentic-init`: it makes the folder of the aminte
// program ready for a coding agent opened in it (plan 090). It reads AMINTE_EXE and AMINTE_DB, which `aminte` sets
// for an extension (extensions/README.md). It opens no database and calls no API.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"aminte-agentic/agentic"
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "aminte-agentic-init:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("aminte-agentic-init", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the error carries the message; main prints it once
	force := fs.Bool("force", false, "replace a file that has another text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q: the only flag is --force", fs.Arg(0))
	}
	exe, dbPath := getenv("AMINTE_EXE"), getenv("AMINTE_DB")
	if exe == "" || dbPath == "" {
		return errors.New("run it as aminte agentic-init: it needs AMINTE_EXE and AMINTE_DB")
	}
	dbPath, err := filepath.Abs(dbPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	var out bytes.Buffer
	res, err := agentic.Init(agentic.Paths{Dir: dir, Exe: exe, DB: dbPath}, *force)
	for _, r := range res {
		fmt.Fprintf(&out, "%-9s %s\n", r.State, filepath.Join(dir, filepath.FromSlash(r.Path)))
	}
	if err == nil {
		addHints(&out, res, dbPath)
	}
	// The lines of the files written before a failure are shown, as they were in the core.
	if _, werr := stdout.Write(out.Bytes()); err == nil {
		err = werr
	}
	return err
}

// addHints adds the lines that say what to do next: replace a kept file, make the database.
func addHints(out *bytes.Buffer, res []agentic.Result, dbPath string) {
	for _, r := range res {
		if r.State == agentic.Kept {
			fmt.Fprintln(out, "a kept file has another text; aminte agentic-init --force replaces it")
			break
		}
	}
	if _, err := os.Stat(dbPath); err != nil {
		fmt.Fprintf(out, "no life.db yet: aminte init %s makes it\n", dbPath)
	}
}
