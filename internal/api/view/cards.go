package view

import (
	"slices"
	"strings"

	"lifelog/internal/text"
	"lifelog/siren"
)

// The cards of a thread (docs/plans/148-the-chat-screen.md): each address that an agent's tool line names is a thing
// of the database. The conversation names the addresses as sub-entities with the rel ref, and ?embed=ref replaces
// them by the entity at the address (Facts.Cards); this file draws the entity as a badge by its class, as the browser
// draws a value by its type. A card is one link to the thing, on the badge of the tool line that named it, above the
// words of the turn.

// CardsOf are the entities that the sub-entities ref of a conversation carry, by address (Facts.Cards). A reference
// that was not embedded (an address that answered an error, one beyond the bound of the embedding, or none that this
// server serves) has no entity: it draws as a plain link.
func CardsOf(links []siren.Link) map[string]*siren.Entity {
	cards := map[string]*siren.Entity{}
	for _, l := range links {
		if l.HasRel("ref") && l.Embedded() {
			cards[l.Href] = l.Entity()
		}
	}
	return cards
}

// Card is one address of a turn, as a badge of the thread draws it: an icon (or a small picture), a title and one
// value. Kind picks the icon and the words: link (the address alone), day, page, person, file, metric, series,
// measurement, search, conversation or other. A part that a kind does not use stays empty.
type Card struct {
	Href, Kind, Title string
	Icon              string
	Value             string // a mood, a reading, a count
	Picture           string
}

// CardOf is the card of one address of a turn. An address that the server did not resolve (an older turn, beyond
// the bound of the embedding or of the column, or beyond the six of a turn) is the plain link, as an address that
// answered an error.
func (v *view) CardOf(href string) Card { return v.cardOf(href, v.cards[href]) }

// cardIcons are the icons of the kinds of a card that is not a leaf.
var cardIcons = map[string]string{"page": "files", "person": "people", "file": "image", "metric": "metrics",
	"series": "metrics", "measurement": "metrics", "search": "search", "conversation": "chat", "day": "calendar"}

// cardOf is the card of the entity e at href. A nil entity, an entity with a property that JSON cannot carry, and
// an entity of no title give the plain link.
func (v *view) cardOf(href string, e *siren.Entity) Card {
	link := Card{Href: href, Kind: "link", Title: linkWords(href)}
	if e == nil {
		return link
	}
	shape, err := jsonShape(e.Properties)
	if err != nil {
		return link
	}
	props, _ := shape.(map[string]any)
	cv := &view{Entity: e, Props: shape, Shell: v.Shell}
	c := Card{Href: href, Title: e.Title}
	switch kind := cardKind(e.Class); {
	case kind == "day" || kind == "day-page":
		c = cv.dayCard(c, props, kind)
	case kind == "series":
		c = cv.seriesCard(c, props)
	case kind == "measurement":
		c.Kind, c.Title = "measurement", Num(props["metric"])
		c.Value = cv.valueWords(props["value"], Num(props["unit"]), Num(props["day"]))
	case kind == "search":
		c = cv.searchCard(c, props)
	case kind == "conversation":
		c.Kind = "conversation"
		c.Title = "Chat, " + cv.Date(Num(props["day"]))
		c.Value = Count(len(anyList(props["turns"])), "turn", "turns")
	case kind != "":
		c = cv.pageCard(c, props, kind)
	default:
		c.Kind = "other"
		if len(e.Class) > 0 {
			c.Value = e.Class[0]
		}
	}
	if c.Title == "" {
		c.Title = linkWords(href)
	}
	c.Icon = cardIcons[c.Kind]
	return c
}

// linkWords are the words of a plain link: the address, cut at 80 characters with an ellipsis. The href stays whole.
func linkWords(href string) string {
	if r := []rune(href); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return href
}

// cardKind picks the kind of an entity from its classes. A conversation is also a page, and a day page is a page
// and a day page: the more particular class wins. A kind of page is its own (person, file, metric, page); "" is a class without a card.
func cardKind(class []string) string {
	for _, k := range []string{"conversation", "day", "day-page", "series", "measurement", "search", "person", "file", "metric", "page"} {
		if slices.Contains(class, k) {
			return k
		}
	}
	return ""
}

// valueWords is a reading as a line says it: "7 h · Tue 27 Oct".
func (v *view) valueWords(value any, unit, day string) string {
	words := Num(value)
	if unit != "" {
		words += " " + unitWords(unit)
	}
	if day != "" {
		short, _, _ := strings.Cut(v.clock().relative(day), " · ")
		words += " · " + short
	}
	return words
}

// dayCard is a day, or a day page: the date, and the mood of a day.
func (v *view) dayCard(c Card, props map[string]any, kind string) Card {
	c.Kind = "day"
	day := Num(props["day"])
	if !text.IsDay(day) {
		day = c.Title
	}
	c.Title = v.Date(day)
	if mood := anyList(props["mood"]); kind == "day" && len(mood) > 0 {
		c.Value = "mood " + Num(mood[0])
		for _, m := range mood[1:] {
			c.Value += ", " + Num(m)
		}
	}
	return c
}

// pageCard is a page, a person, a file or a metric's page: its title and its cover (a file's own picture
// when it has no cover).
func (v *view) pageCard(c Card, props map[string]any, kind string) Card {
	c.Kind = kind
	if cover, _ := props["cover"].(map[string]any); cover != nil {
		c.Picture = "/pages/" + Num(cover["id"]) + "/preview"
	} else if kind == "file" {
		c.Picture = v.Link("preview")
	}
	return c
}

// seriesCard is a metric: its name and its latest reading with the unit and the day.
func (v *view) seriesCard(c Card, props map[string]any) Card {
	c.Kind = "series"
	readings := anyList(props["readings"])
	if n := len(readings); n > 0 {
		last, _ := readings[n-1].(map[string]any)
		c.Value = v.valueWords(last["value"], Num(props["unit"]), Num(last["day"]))
	}
	return c
}

// searchCard is a search: its words and the count of its hits.
func (v *view) searchCard(c Card, props map[string]any) Card {
	c.Kind = "search"
	c.Title = "Search: " + Num(props["q"])
	c.Value = Count(len(anyList(props["hits"])), "hit", "hits")
	return c
}
