package agent

import (
	"errors"
	"fmt"
	"strings"

	"lifelog/siren"
)

// page is the conversation of a day as the agent read it: the answer of GET /days/{day}/conversation. The page of the
// API says what is answered.
type page struct {
	Day     string
	Version string // "" when the day has no page yet
	Body    string // the body with LF line ends: a browser submits a textarea with CRLF, and the API keeps it as sent
	turns   []turn
}

// turn is one turn of the page, as the API parsed it: who is owner, lifelog or text. The key names the turn for the
// action reply. The API makes it from what the turn is, not from its place, so it does not move when a reply is written,
// when the owner says a new turn or when an earlier turn is removed. It changes when the owner edits the turn.
type turn struct{ who, key, text string }

// pageFrom reads a conversation entity. The day is its property day. A day with no page has no version and no turns.
// A turn with no key is an error: the server is too old to take a reply.
func pageFrom(e *siren.Entity) (*page, error) {
	props, _ := e.Properties.(map[string]any)
	day, _ := props["day"].(string)
	if day == "" {
		return nil, errors.New("the answer is not a conversation: it has no day")
	}
	p := &page{Day: day}
	if props["id"] == nil {
		return p, nil
	}
	body, ok := props["body"].(string)
	if props["version"] == nil || !ok {
		return nil, errors.New("the answer is not a page with an id, a version and a body")
	}
	p.Version, p.Body = fmt.Sprint(props["version"]), strings.ReplaceAll(body, "\r\n", "\n")
	turns, _ := props["turns"].([]any)
	for _, t := range turns {
		m, _ := t.(map[string]any)
		who, _ := m["who"].(string)
		key, _ := m["key"].(string)
		text, _ := m["text"].(string)
		if key == "" {
			return nil, fmt.Errorf("the answer is not a conversation that reply can answer: the turn %d has no key", len(p.turns)+1)
		}
		p.turns = append(p.turns, turn{who, key, text})
	}
	return p, nil
}

// pending says whether the last turn of the page is the owner's: nobody has answered it yet. Text that an edit adds
// after a reply is part of that reply. An edit that types a marker line of the owner adds a turn of the owner.
func (p *page) pending() bool {
	return len(p.turns) > 0 && p.turns[len(p.turns)-1].who == "owner"
}

// lastOwner is the key of the last turn of the owner. When the page waits, it is the last turn of the page, and the
// field turn of reply names it: the core puts the reply right after that turn.
func (p *page) lastOwner() string {
	for i := len(p.turns) - 1; i >= 0; i-- {
		if p.turns[i].who == "owner" {
			return p.turns[i].key
		}
	}
	return ""
}

// key names the state of the page: its day and its version. The version changes with each write to the page.
func (p *page) key() string { return p.Day + "/" + p.Version }
