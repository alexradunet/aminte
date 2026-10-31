package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"lifelog-web/view"
	"lifelog/siren"
)

// cardsPerColumn is how many of the newest addresses of today's conversation the chat column draws as cards. The column
// is on every page, so it reads few. The older addresses draw their link.
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
// nothing. The cards are read here, one request each, for the newest addresses of the conversation of today: the
// core embeds them for a client that asks, and web asks for the few that its column draws.
func (b *Browser) shellFacts(r *http.Request, status int, root, e *siren.Entity, f *view.Facts) {
	good := status < http.StatusBadRequest
	ctx, cancel := context.WithTimeout(r.Context(), shellTimeout)
	defer cancel()
	if root == nil {
		root, _ = b.core.read(ctx, withEmbed("/", "today,conversation"), pageWriter)
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
	// The column of any other page has the cards of the newest turns of today.
	if i := slices.IndexFunc(root.Entities, func(l siren.Link) bool { return l.HasRel("conversation") && l.Embedded() }); i >= 0 {
		b.embedLinks(ctx, root.Entities[i].Entities, cardsPerColumn, refRel)
		f.Cards = view.CardsOf(root.Entities[i].Entities)
	}
}

// embedLinks replaces, in place, each reference of links that has the rel by the entity that the core answers at its
// href. At most limit of them are read, the first ones in order, all at once. A reference whose read fails stays as it
// is. The reads are made as embedWriter, and they return before ctx ends.
func (b *Browser) embedLinks(ctx context.Context, links []siren.Link, limit int, rel string) {
	var todo []int
	for i, l := range links {
		if len(todo) == limit {
			break
		}
		if l.Href != "" && !l.Embedded() && l.HasRel(rel) {
			todo = append(todo, i)
		}
	}
	found := make([]*siren.Entity, len(todo))
	var wg sync.WaitGroup
	for n, i := range todo {
		wg.Go(func() { found[n] = b.core.entity(ctx, links[i].Href, embedWriter, 1) })
	}
	wg.Wait()
	for n, i := range todo {
		if found[n] != nil {
			if embedded := links[i].Embedding(found[n]); embedded.Embedded() { // an entity with no content is no representation
				links[i] = embedded
			}
		}
	}
}
