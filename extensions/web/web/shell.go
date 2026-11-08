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

// cardsPerColumn is how many of the newest addresses of today's conversation the chat column draws as cards. The column
// is on every page, so it draws few. The older addresses draw their link.
const cardsPerColumn = 6

// shellTimeout is the longest that the shell may take. The strip and the column are a help: a core that is slow draws
// the page without them.
const shellTimeout = 10 * time.Second

// shellFacts reads what the shell of a page draws, from the answers of the core and nothing else: the root of the API
// with the day of today and the conversation of today embedded (API.md, "Embedding"), the cards of the chat column,
// and for a conversation the days of its day bar and the cards that the core embedded in it. root is the root when the
// page read it already (Home).
//
// A read that fails draws the page without its part. The page of an error has logged its own cause, so its reads log
// nothing. The cards of the column are one more read: the conversation of today, with the thing at each of its
// addresses embedded by the core.
func (b *Browser) shellFacts(r *http.Request, status int, root, e *siren.Entity, f *view.Facts) {
	good := status < http.StatusBadRequest
	ctx, cancel := context.WithTimeout(r.Context(), shellTimeout)
	defer cancel()
	if root == nil {
		root = b.core.read(ctx, withEmbed("/", "today,conversation"), pageWriter)
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
	if good && shell.Chat == nil {
		logFault(r, "the chat column cannot be read", errors.New("the root gave no conversation of today"))
	}
	f.Shell = shell
	if !good {
		return
	}
	if slices.Contains(e.Class, "conversation") {
		// The core embedded the cards of the conversation view in e already (page).
		f.Cards, f.ChatDays = view.CardsOf(e.Entities), view.ChatDaysOf(e.Links)
		return
	}
	// The column of any other page has the cards of the newest turns of today. The core embeds the thing at each
	// address of that conversation, the newest first, and the column draws the first few.
	if i := slices.IndexFunc(root.Entities, func(l siren.Link) bool { return l.HasRel("conversation") && l.Embedded() }); i >= 0 {
		if conversation := b.core.read(ctx, withEmbed(root.Entities[i].Href, refRel), pageWriter); conversation != nil {
			refs := slices.DeleteFunc(slices.Clone(conversation.Entities), func(l siren.Link) bool { return !l.HasRel(refRel) })
			f.Cards = view.CardsOf(refs[:min(len(refs), cardsPerColumn)])
		}
	}
}
