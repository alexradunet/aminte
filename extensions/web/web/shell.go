package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"aminte-web/view"
	"aminte/siren"
)

// shellTimeout is the longest that the shell may take. The strip is a help: a core that is slow draws the page without
// it.
const shellTimeout = 10 * time.Second

// shellFacts reads what the shell of a page draws, from the answers of the core and nothing else: the root of the API
// with the day of today embedded (API.md, "Embedding"). root is the root when the page read it already (Home).
//
// A read that fails draws the page without its part. The page of an error has logged its own cause, so its reads log
// nothing.
func (b *Browser) shellFacts(r *http.Request, status int, root *siren.Entity, f *view.Facts) {
	good := status < http.StatusBadRequest
	ctx, cancel := context.WithTimeout(r.Context(), shellTimeout)
	defer cancel()
	if root == nil {
		root = b.core.read(ctx, withEmbed("/", "today"), pageWriter)
	}
	if root == nil {
		if good {
			logFault(r, "the shell cannot be read", errors.New("the root gave no entity"))
		}
		return
	}
	shell, err := view.ShellOf(root)
	if err != nil {
		if good {
			logFault(r, "the shell cannot be read", err)
		}
		return
	}
	shell.Apps = slices.DeleteFunc(shell.Apps, func(a view.AppLink) bool { return a.Name == appName }) // its form is on /settings
	settings := b.settings()
	shell.Ground, shell.LockMinutes = groundOf(settings), settings.lockMinutes()
	if good && shell.Strip == nil {
		logFault(r, "the Today strip cannot be read", errors.New("the root gave no day of today"))
	}
	f.Shell = shell
}
