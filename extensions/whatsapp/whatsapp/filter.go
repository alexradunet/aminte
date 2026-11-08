package whatsapp

import (
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"aminte-whatsapp/relay"
)

// Filter turns an event into a message of the chat, or says that the relay must not see it. A message is passed
// on when it is not from this device, not from a history sync, not an edit, not a group message and newer than
// after (the earlier of the start of the program and the last message that the relay said, so that a message that
// came while the program was off is said once, and a restart does not say it again). A reaction
// and a protocol message (a delete, a read marker) are not messages. A message that is not text is passed on with
// its kind, so that the relay can refuse it with a line.
func Filter(evt *events.Message, after time.Time) (relay.Message, bool) {
	info := evt.Info
	switch {
	case info.IsFromMe, info.IsGroup, evt.SourceWebMsg != nil, evt.IsEdit:
		return relay.Message{}, false
	case !info.Timestamp.After(after):
		return relay.Message{}, false
	}
	m := evt.Message
	if m.GetReactionMessage() != nil || m.GetProtocolMessage() != nil {
		return relay.Message{}, false
	}
	out := relay.Message{Sender: senderNumber(info), At: info.Timestamp, Kind: relay.KindOther}
	switch {
	case m.GetConversation() != "":
		out.Kind, out.Text = relay.KindText, m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		out.Kind, out.Text = relay.KindText, m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		out.Kind = relay.KindImage
	case m.GetAudioMessage() != nil:
		out.Kind = relay.KindAudio
	case m.GetDocumentMessage() != nil:
		out.Kind = relay.KindDocument
	}
	return out, true
}

// senderNumber is the number of the sender, digits only. A sender can be a hidden id (a lid): then the number is
// the alternative address, when WhatsApp gave one.
func senderNumber(info types.MessageInfo) string {
	if info.Sender.Server == types.HiddenUserServer && info.SenderAlt.User != "" {
		return info.SenderAlt.User
	}
	return info.Sender.User
}
