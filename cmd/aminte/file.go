package main

import (
	"context"
	"errors"
	"os"
)

// keepFile is `aminte file PATH`: the add-file action with the original streamed from disk (hashed by the API,
// never stored), its text read from --text, and a picture from --preview when the original is not one aminte reads.
func keepFile(ctx context.Context, e env, args []string) error {
	o := e.o
	if len(args) != 1 {
		return errors.New(e.cmd.synopsis())
	}
	vals := map[string]string{"original": args[0]}
	if o.file.title != "" {
		vals["title"] = o.file.title
	}
	if o.file.text != "" {
		b, err := os.ReadFile(o.file.text)
		if err != nil {
			return err
		}
		vals["body"] = string(b)
	}
	for k, v := range map[string]string{"mime": o.file.mime, "day": o.day, "preview": o.file.preview} {
		if v != "" {
			vals[k] = v
		}
	}
	if o.file.dryRun {
		vals["dry_run"] = "1"
	}
	return doAction(ctx, e, e.cmd.wraps, vals)
}

// setCover is `aminte cover PAGE-ID [PHOTO]`: the set-cover action, with a new photo streamed from disk (hashed by
// the API, never stored) or, with no PHOTO, the kept picture that --title names. The API refuses a call with neither.
func setCover(ctx context.Context, e env, args []string) error {
	o := e.o
	if len(args) < 1 || len(args) > 2 {
		return errors.New(e.cmd.synopsis() + ": a new PHOTO, or --title of a kept picture")
	}
	vals := map[string]string{"id": args[0]}
	if len(args) == 2 {
		vals["original"] = args[1]
	}
	if o.file.title != "" {
		vals["title"] = o.file.title
	}
	if o.file.preview != "" {
		vals["preview"] = o.file.preview
	}
	return doAction(ctx, e, e.cmd.wraps, vals)
}
