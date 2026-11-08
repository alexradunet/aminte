package whatsapp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unicode/utf8"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"

	"aminte-whatsapp/relay"
)

var (
	t0    = time.Date(2031, 5, 20, 9, 30, 0, 0, time.UTC)
	after = t0.Add(-time.Hour)
)

// event is a message event from the owner's number, at t0.
func event(m *waE2E.Message) *events.Message {
	sender := types.NewJID("40712345678", types.DefaultUserServer)
	return &events.Message{
		Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: sender, Sender: sender}, ID: "A1", Timestamp: t0},
		Message: m,
	}
}

func TestFilter(t *testing.T) {
	text := &waE2E.Message{Conversation: proto.String("hello")}
	for _, tc := range []struct {
		name   string
		mutate func(*events.Message)
		want   relay.Message
		pass   bool
	}{
		{"text", func(*events.Message) {}, relay.Message{Sender: "40712345678", Text: "hello", Kind: relay.KindText, At: t0}, true},
		{"extended text", func(e *events.Message) {
			e.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("a link")}}
		}, relay.Message{Sender: "40712345678", Text: "a link", Kind: relay.KindText, At: t0}, true},
		{"image", func(e *events.Message) { e.Message = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}} },
			relay.Message{Sender: "40712345678", Kind: relay.KindImage, At: t0}, true},
		{"audio", func(e *events.Message) { e.Message = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{}} },
			relay.Message{Sender: "40712345678", Kind: relay.KindAudio, At: t0}, true},
		{"document", func(e *events.Message) { e.Message = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{}} },
			relay.Message{Sender: "40712345678", Kind: relay.KindDocument, At: t0}, true},
		{"a sticker", func(e *events.Message) { e.Message = &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}} },
			relay.Message{Sender: "40712345678", Kind: relay.KindOther, At: t0}, true},
		{"a hidden id with a number", func(e *events.Message) {
			e.Info.Sender = types.NewJID("1234", types.HiddenUserServer)
			e.Info.SenderAlt = types.NewJID("40799999999", types.DefaultUserServer)
		}, relay.Message{Sender: "40799999999", Text: "hello", Kind: relay.KindText, At: t0}, true},
		{"from me", func(e *events.Message) { e.Info.IsFromMe = true }, relay.Message{}, false},
		{"a group", func(e *events.Message) { e.Info.IsGroup = true }, relay.Message{}, false},
		{"history", func(e *events.Message) { e.SourceWebMsg = &waWeb.WebMessageInfo{} }, relay.Message{}, false},
		{"old", func(e *events.Message) { e.Info.Timestamp = after }, relay.Message{}, false},
		{"an edit", func(e *events.Message) { e.IsEdit = true }, relay.Message{}, false},
		{"a reaction", func(e *events.Message) { e.Message = &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{}} }, relay.Message{}, false},
		{"a protocol message", func(e *events.Message) { e.Message = &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{}} }, relay.Message{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := event(text)
			tc.mutate(e)
			got, ok := Filter(e, after)
			if ok != tc.pass || got != tc.want {
				t.Errorf("Filter = %+v, %v; want %+v, %v", got, ok, tc.want, tc.pass)
			}
		})
	}
}

func TestRenderHasTheSizeOfTheMatrixAndABlankQuietZone(t *testing.T) {
	const text = "2@synthetic,code,not,a,real,pairing,code"
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := Render(text)
	if err != nil {
		t.Fatal(err)
	}
	total := code.Size + 2*quiet
	if want := (total + 1) / 2; len(lines) != want {
		t.Fatalf("%d lines, want %d", len(lines), want)
	}
	for i, l := range lines {
		if n := utf8.RuneCountInString(l); n != total {
			t.Errorf("line %d has %d columns, want %d", i, n, total)
		}
	}
	first, last := []rune(lines[0]), []rune(lines[len(lines)-1])
	for _, c := range []rune{first[0], first[total-1], last[0], last[total-1]} {
		if c != ' ' {
			t.Errorf("a corner of the quiet zone is %q, want blank", c)
		}
	}
	// A module of the code, read back from the half blocks, equals the matrix.
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			row, col := y+quiet, x+quiet
			c := []rune(lines[row/2])[col]
			dark := c == '█' || (row%2 == 0 && c == '▀') || (row%2 == 1 && c == '▄')
			if dark != code.Black(x, y) {
				t.Fatalf("module %d,%d is %v in the drawing, %v in the code", x, y, dark, code.Black(x, y))
			}
		}
	}
}

func TestTheStateFolderAndTheSessionStoreArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix modes")
	}
	dir := filepath.Join(t.TempDir(), "aminte-whatsapp")
	session, state, err := Prepare(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(session) != "session.db" || filepath.Base(state) != "state.json" {
		t.Errorf("paths %s %s", session, state)
	}
	s, err := Open(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	if s.Number() != "" {
		t.Errorf("a new store is paired as %q", s.Number())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, session: 0o600} {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: %v, %v; want mode %o", path, fi, err, want)
		}
	}
	// A folder made with a wider mode is narrowed.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %o after Prepare", fi.Mode().Perm())
	}
	// The store opens again, with its tables.
	s, err = Open(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
