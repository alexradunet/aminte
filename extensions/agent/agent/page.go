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

// turn is one turn of the page, as the API parsed it: who is owner, lifelog or text.
type turn struct{ who, text string }

// pageFrom reads a conversation entity. The day is its property day. A day with no page has no ID and no turns.
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
		text, _ := m["text"].(string)
		p.turns = append(p.turns, turn{who, text})
	}
	return p, nil
}

// pending says whether the last turn of the page is the owner's: nobody has answered it yet. Text that an edit adds
// after a reply is part of that reply, so an edit is never a turn.
func (p *page) pending() bool {
	return len(p.turns) > 0 && p.turns[len(p.turns)-1].who == "owner"
}

// owners is the number of turns of the owner. It is the number of the last one, which the field turn of reply names.
func (p *page) owners() int {
	n := 0
	for _, t := range p.turns {
		if t.who == "owner" {
			n++
		}
	}
	return n
}

// key names the state of the page: its day and its version. The version changes with each write to the page.
func (p *page) key() string { return p.Day + "/" + p.Version }
