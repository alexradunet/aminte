package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"lifelog/internal/api/view"
	"lifelog/siren"
)

// cardsPerColumn is how many of the newest addresses of today's conversation the chat column draws as cards: the
// column is on every page, so it reads fewer than the conversation view, which draws every address that one embedding
// gives (maxEmbedded). The older addresses draw their link.
const cardsPerColumn = 6

// shellFacts reads what the shell of a browser page draws, through the server's own handler and nothing else: the
// root of the API with the day of today and the conversation of today embedded (API.md, "Embedding"), and the cards
// and the day bar of a conversation. Only the settings of the browser (ground, lock) come from the store; they move
// out of the core with the browser.
//
// A read that fails draws the page without its part: the strip and the column are a help. The read that failed has
// logged its own cause. The page of an error has logged its own cause, so its reads log nothing (quietKey).
func (h *server) shellFacts(r *http.Request, status int, e *siren.Entity, f *view.Facts) {
	good := status < http.StatusBadRequest
	if !good {
		r = r.WithContext(context.WithValue(r.Context(), quietKey{}, true))
	}
	root := answeredEntity(h.inside(r, "/?"+embedField+"=today,conversation"))
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
	// A failed read of the settings draws the boards: the floor is a help.
	ground, err := shellGround(h.s)
	if err != nil && good {
		logFault(r, "the settings cannot be read", err)
	}
	shell.Ground = ground
	shell.LockMinutes = lockMinutesOf(r)
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
		// The conversation view is the whole chat: the cards of its newest turns and the days of its day bar.
		h.embedLinks(r, e.Entities, maxEmbedded, refRel)
		f.Cards, f.ChatDays = view.CardsOf(e.Entities), view.ChatDaysOf(e.Links)
		return
	}
	// The column of any other page has the cards of the newest turns of today, fewer of them.
	if i := slices.IndexFunc(root.Entities, func(l siren.Link) bool { return l.HasRel("conversation") && l.Embedded() }); i >= 0 {
		h.embedLinks(r, root.Entities[i].Entities, cardsPerColumn, refRel)
		f.Cards = view.CardsOf(root.Entities[i].Entities)
	}
}
