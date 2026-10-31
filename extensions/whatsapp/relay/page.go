package relay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"lifelog/siren"
)

// page is the conversation of a day as the relay read it: the answer of GET /days/{day}/conversation. The relay keeps
// no page between two looks.
type page struct {
	Day   string
	turns []turn
}

// turn is one turn of the page, as the API parsed it: who is owner, lifelog or text, and via names the relay that
// carried the turn of the owner.
type turn struct{ who, via, text string }

// read is the conversation of a day (a local day, or the word today): GET /days/{day}/conversation. A day with no page
// has no turns.
func (r *Relay) read(ctx context.Context, day string) (*page, error) {
	e, err := r.API.Get(ctx, "/days/"+day+"/conversation")
	if err != nil {
		return nil, err
	}
	return pageFrom(e)
}

// pageFrom reads a conversation entity. The day is its property day.
func pageFrom(e *siren.Entity) (*page, error) {
	props, _ := e.Properties.(map[string]any)
	day, _ := props["day"].(string)
	if day == "" {
		return nil, errors.New("the answer is not a conversation: it has no day")
	}
	p := &page{Day: day}
	turns, _ := props["turns"].([]any)
	for _, t := range turns {
		m, _ := t.(map[string]any)
		who, _ := m["who"].(string)
		via, _ := m["via"].(string)
		text, _ := m["text"].(string)
		p.turns = append(p.turns, turn{who, via, text})
	}
	return p, nil
}

// pending says whether the last turn of the page is the owner's: the agent has not answered it yet.
func (p *page) pending() bool {
	return len(p.turns) > 0 && p.turns[len(p.turns)-1].who == "owner"
}

// dayBefore is the day before a day (YYYY-MM-DD).
func dayBefore(day string) (string, error) {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return "", fmt.Errorf("the server gave the day %q", day)
	}
	return t.AddDate(0, 0, -1).Format(time.DateOnly), nil
}
