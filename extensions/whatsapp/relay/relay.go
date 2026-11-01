package relay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog/app"
	"lifelog/client"
	"lifelog/siren"
)

const (
	// pollEvery is the time between two looks at the conversation page.
	pollEvery = 3 * time.Second
	// sayTimeout is the longest time of one say. The action answers at once: the agent answers later, on the page.
	sayTimeout = 30 * time.Second
	// maxSend is the largest text that goes to the chat in one message. A longer text is cut and says so.
	maxSend = 16 << 10
)

// Relay carries the messages of the owner to the conversation page and the turns of the page to the chat.
type Relay struct {
	Chat Chat
	// API is the owner's client: the owner's token and no source. say is the owner's action, and the API refuses it to
	// an agent.
	API   *client.Client
	Owner string      // the number of the owner, digits only; a message of another sender is ignored
	State string      // the path of state.json
	Log   *log.Logger // nil: the standard logger
	// Tick gives the moments when the relay looks at the page. nil: every three seconds.
	Tick <-chan time.Time

	logger  *log.Logger
	say     siren.Action
	st      *state
	lookErr string
}

// Run carries the messages and the turns until the chat closes its channel (it returns nil) or ctx ends (it returns the
// error of ctx). It reads the catalog of the API and the state first, then looks at the page at once and at each tick.
// It runs in one goroutine: a say and a look never run together.
func (r *Relay) Run(ctx context.Context) error {
	r.logger = r.Log
	if r.logger == nil {
		r.logger = log.Default()
	}
	if err := r.readCatalog(ctx); err != nil {
		return err
	}
	var err error
	if r.st, err = loadState(r.State); err != nil {
		return err
	}
	tick := r.Tick
	if tick == nil {
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		tick = t.C
	}
	messages := r.Chat.Messages()
	r.look(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m, ok := <-messages:
			if !ok {
				return nil
			}
			r.accept(ctx, m)
		case <-tick:
			r.look(ctx)
		}
	}
}

// readCatalog finds the action say. Any other answer than a catalog with say is an error. A catalog with no say is an
// error that the owner acts on (app.OwnerActs): the server is too old, or it is not a lifelog server, and a restart of
// the relay does not help.
func (r *Relay) readCatalog(ctx context.Context) error {
	list, err := r.API.Catalog(ctx)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	for _, x := range list {
		if x.Name == "say" {
			r.say = x
			return nil
		}
	}
	return app.OwnerActs(errors.New("the catalog lacks the action say"))
}

// accept applies the rules of who may talk: another sender is ignored and logged, a message of the owner that is not
// text gets one line, and a text goes to the page with say.
func (r *Relay) accept(ctx context.Context, m Message) {
	switch {
	case m.Sender != r.Owner:
		r.logger.Printf("ignored a message of kind %s from %s", m.Kind, m.Sender)
	case m.Kind != KindText:
		r.send(ctx, "I read text for now; send it as text.")
	default:
		r.sayToPage(ctx, m)
	}
}

// sayToPage adds the message to today's conversation as the owner's turn, with via=whatsapp. The API stamps the turn
// and makes the page on the first turn of the day. The agent answers on the page, and a later look sends the answer.
func (r *Relay) sayToPage(ctx context.Context, m Message) {
	sctx, cancel := context.WithTimeout(ctx, sayTimeout)
	defer cancel()
	_, err := r.API.Do(sctx, r.say, map[string]string{"day": "today", "text": m.Text, "via": Via})
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		r.logger.Printf("say: %v", err)
		r.send(ctx, "I could not write your message to lifelog: "+firstLine(err.Error())+". Say it again.")
		return
	}
	if m.At.After(r.st.Said) {
		r.st.Said = m.At
		if err := r.st.save(r.State); err != nil {
			r.logger.Print(err)
		}
	}
}

// send sends a text to the chat and logs a failure.
func (r *Relay) send(ctx context.Context, text string) {
	if err := r.Chat.Send(ctx, cut(text)); err != nil && ctx.Err() == nil {
		r.logger.Printf("send failed: %v", err)
	}
}

// look sends the new turns of the page to the chat. It logs a failure once for each kind, not every three seconds.
func (r *Relay) look(ctx context.Context) {
	err := r.sync(ctx)
	if ctx.Err() != nil {
		return
	}
	switch msg := fmt.Sprint(err); {
	case err == nil:
		r.lookErr = ""
	case msg != r.lookErr:
		r.lookErr = msg
		r.logger.Printf("look: %v", err)
	}
}

// sync sends the turns that the chat has not got. It watches today, and a day before today while the agent may still
// answer its last turn: a turn said before midnight is answered on its own page, and the answer may come after midnight.
func (r *Relay) sync(ctx context.Context) error {
	today, err := r.read(ctx, "today")
	if err != nil {
		return err
	}
	if r.st.Seen == nil { // the first run: what the page holds now is not sent
		r.st.Seen = map[string][]string{today.Day: today.keys()}
		return r.st.save(r.State)
	}
	before, err := dayBefore(today.Day)
	if err != nil {
		return err
	}
	if _, ok := r.st.Seen[today.Day]; !ok { // a new day: all its turns are new
		r.st.Seen[today.Day] = []string{}
		if err := r.st.save(r.State); err != nil {
			return err
		}
	}
	for _, day := range slices.Sorted(maps.Keys(r.st.Seen)) {
		p := today
		if day < before { // too old: the page has the answer, and the chat does not get it late
			p = nil
		} else if day != today.Day {
			if p, err = r.read(ctx, day); err != nil {
				return err
			}
		}
		if p != nil {
			if err := r.forward(ctx, p); err != nil {
				return err
			}
		}
		if day != today.Day && (p == nil || !p.pending()) {
			delete(r.st.Seen, day)
			if err := r.st.save(r.State); err != nil {
				return err
			}
		}
	}
	return nil
}

// forward sends the turns of the page that the chat has not got. A reply of the agent goes as it is. A turn of the owner
// from another surface goes marked, so the chat shows the same thread as the page. A turn that came from this relay
// is not sent back. A turn is new when the state does not hold its key, wherever it stands on the page. The state moves
// after each send: a crash between the send and the save sends one turn twice, and never loses one. At the end the
// state of the day is the keys of the turns that the page has now: the key of a turn that was removed is dropped, so it
// cannot hide a new turn that gets the same key (the same words, in the same minute, from the same relay).
func (r *Relay) forward(ctx context.Context, p *page) error {
	dealt := map[string]bool{}
	for _, k := range r.st.Seen[p.Day] {
		dealt[k] = true
	}
	for _, t := range p.turns {
		if dealt[t.key] {
			continue
		}
		if text := forwarded(t); text != "" {
			if err := r.Chat.Send(ctx, cut(text)); err != nil {
				return fmt.Errorf("send: %w", err)
			}
		}
		r.st.Seen[p.Day] = append(r.st.Seen[p.Day], t.key)
		if err := r.st.save(r.State); err != nil {
			return err
		}
	}
	// Every key of the page is in the state now, and no key is twice in the page: the state has the same keys as the
	// page when it has as many. A page with no turns is a deleted page, or a day with no page: its turns come back when
	// say makes the page live again, and they are not new. So its state stays.
	if keys := p.keys(); len(keys) > 0 && len(keys) != len(r.st.Seen[p.Day]) {
		r.st.Seen[p.Day] = keys
		return r.st.save(r.State)
	}
	return nil
}

// forwarded is the text that the chat gets for a turn, and "" when it gets none.
func forwarded(t turn) string {
	switch {
	case t.who == "lifelog":
		return t.text
	case t.who == "owner" && t.via != Via:
		return "You wrote in lifelog: " + t.text
	}
	return ""
}

// cut shortens s to maxSend bytes at a character boundary and says so.
func cut(s string) string {
	if len(s) <= maxSend {
		return s
	}
	n := maxSend
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "\n[cut: the text is longer]"
}

// firstLine is the first line of s.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
