package agent

import (
	"errors"
	"fmt"
	"strings"

	"lifelog/siren"
)

// page is the conversation of a day as the agent read it: the answer of GET /days/{day}/conversation. The agent
// keeps no page between two looks. The page of the API says what is answered.
type page struct {
	Day     string
	ID      string // "" when the day has no page yet
	Version string
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
	p.ID, p.Version, p.Body = fmt.Sprint(props["id"]), fmt.Sprint(props["version"]), strings.ReplaceAll(body, "\r\n", "\n")
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

// owners is the number of turns of the owner.
func (p *page) owners() int {
	n := 0
	for _, t := range p.turns {
		if t.who == "owner" {
			n++
		}
	}
	return n
}

// key names the last turn: the day, the number of turns and the words of the last one. The key of a page changes when
// the owner says more, or edits the words of the turn.
func (p *page) key() string {
	if len(p.turns) == 0 {
		return p.Day
	}
	return fmt.Sprintf("%s/%d/%s", p.Day, len(p.turns), p.turns[len(p.turns)-1].text)
}
