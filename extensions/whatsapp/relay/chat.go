// Package relay carries the words of the owner between a chat (WhatsApp) and the conversation page of aminte. It
// is a transport and nothing more. A message of the owner becomes the action say, with via=whatsapp. Each new turn of
// the page that did not come from the chat goes to the chat: the reply of the agent, and the words that the owner
// said on another surface. The relay calls no model: aminte-agent answers on the page.
package relay

import (
	"context"
	"time"
)

// Message kinds. Kind is KindText, or what the message was when it was not text.
const (
	KindText     = "text"
	KindImage    = "image"
	KindAudio    = "audio"
	KindDocument = "document"
	KindOther    = "other"
)

// Via is the name of this relay. The action say writes it in the brackets of the turn, so the relay can tell its own
// turns from the turns of other surfaces.
const Via = "whatsapp"

// Message is one message that arrived in a chat.
type Message struct {
	Sender string    // the number, digits only
	Text   string    // the text; "" when Kind is not KindText
	Kind   string    // KindText, or what it was
	At     time.Time // when the sender sent it
}

// Chat is the place where the owner talks to aminte.
type Chat interface {
	// Messages gives the messages that arrive. The channel closes when no more can arrive.
	Messages() <-chan Message
	// Send sends a text to the owner.
	Send(ctx context.Context, text string) error
}
