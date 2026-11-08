package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"aminte-whatsapp/relay"
)

// libraryLog is the log of whatsmeow: its warnings and errors only.
func libraryLog() waLog.Logger { return waLog.Stdout("whatsmeow", "WARN", false) }

// Login pairs the device of s as a linked device of the number of aminte. It draws each QR code on out and returns
// when the owner of the phone scans it (nil), when the codes time out, or when ctx ends. A store that is paired
// already is an error.
func Login(ctx context.Context, s *Session, out io.Writer) error {
	if n := s.Number(); n != "" {
		return fmt.Errorf("already paired as %s; delete %s to pair again", n, s.Path())
	}
	cl := whatsmeow.NewClient(s.dev, libraryLog())
	codes, err := cl.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("pairing: %w", err)
	}
	// After the scan the server disconnects the client, which connects again to finish the pairing: a client that
	// leaves before that connection is logged out at its next one (the Connected event of whatsmeow says to wait
	// for it). The handler reports the connection that follows the success.
	var paired atomic.Bool
	connected := make(chan struct{}, 1)
	handler := cl.AddEventHandler(func(evt any) {
		if _, ok := evt.(*events.Connected); ok && paired.Load() {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
	})
	defer cl.RemoveEventHandler(handler)
	if err := cl.Connect(); err != nil {
		return fmt.Errorf("pairing: %w", err)
	}
	defer cl.Disconnect()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-codes:
			if !ok {
				return errors.New("pairing: the channel of codes closed with no result")
			}
			switch item.Event {
			case "code":
				if err := drawCode(out, item.Code); err != nil {
					return err
				}
			case "success":
				paired.Store(true)
				fmt.Fprintln(out, "Scanned. Finishing the pairing with the phone...")
				select {
				case <-connected:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(pairingWait):
					return errors.New("pairing: the phone did not confirm the device in time; run aminte whatsapp login again")
				}
			case "timeout":
				return errors.New("pairing: nobody scanned the code in time")
			default:
				return fmt.Errorf("pairing: %s %v", item.Event, item.Error)
			}
		}
	}
}

// pairingWait is the longest wait for the connection that finishes a pairing after the scan.
const pairingWait = 60 * time.Second

// drawCode writes the instruction and the code, black on white on any terminal theme.
func drawCode(out io.Writer, code string) error {
	lines, err := Render(code)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\nOpen WhatsApp on the phone of aminte, Linked devices, Link a device, and scan this.\n\n")
	for _, l := range lines {
		b.WriteString("\x1b[30;107m" + l + "\x1b[0m\n")
	}
	_, err = io.WriteString(out, b.String())
	return err
}

// Ended says why the connection ended for good: whatsmeow does not connect again after these events. Retry says
// whether a new process may try again (a failure of the connection) or not (the device is unlinked, replaced,
// banned or outdated: the owner must act first).
type Ended struct {
	Reason string
	Retry  bool
}

// Chat is the chat of the owner on WhatsApp.
type Chat struct {
	cl      *whatsmeow.Client
	owner   types.JID
	after   time.Time
	log     *log.Logger
	msgs    chan relay.Message
	handler uint32
	done    chan struct{}
	stop    sync.Once
	endOnce sync.Once
	ended   chan Ended
	reads   sync.WaitGroup // the read receipts in flight: Close waits for them
	// ctx is the context of the run. whatsmeow calls the handler with no context, so the handler takes this one.
	ctx context.Context
}

// Connect connects the paired device of s and starts to pass the messages of owner (digits) that are newer than
// after. Close ends it. The device must be paired.
func Connect(ctx context.Context, s *Session, owner string, after time.Time, l *log.Logger) (*Chat, error) {
	if s.Number() == "" {
		return nil, ErrNotPaired
	}
	c := &Chat{
		cl: whatsmeow.NewClient(s.dev, libraryLog()), owner: types.NewJID(owner, types.DefaultUserServer), after: after, log: l,
		msgs: make(chan relay.Message, 32), done: make(chan struct{}), ended: make(chan Ended, 1), ctx: ctx,
	}
	c.handler = c.cl.AddEventHandler(c.handle)
	if err := c.cl.Connect(); err != nil {
		c.cl.RemoveEventHandler(c.handler)
		return nil, fmt.Errorf("connect: %w", err)
	}
	return c, nil
}

// Ended gives the end of the connection, once, when it ends for good.
func (c *Chat) Ended() <-chan Ended { return c.ended }

// end records the end of the connection: the first reason only.
func (c *Chat) end(e Ended) { c.endOnce.Do(func() { c.ended <- e }) }

// Close disconnects and waits for the read receipts in flight. The channel of Messages stays open: the relay ends
// with its context.
func (c *Chat) Close() {
	c.stop.Do(func() {
		c.cl.RemoveEventHandler(c.handler)
		c.cl.Disconnect()
		close(c.done)
		c.reads.Wait()
	})
}

// Messages gives the messages that Filter lets through.
func (c *Chat) Messages() <-chan relay.Message { return c.msgs }

// Send sends a text to the owner.
func (c *Chat) Send(ctx context.Context, text string) error {
	_, err := c.cl.SendMessage(ctx, c.owner, &waE2E.Message{Conversation: proto.String(text)})
	return err
}

// handle is the handler of the events of whatsmeow.
func (c *Chat) handle(evt any) {
	switch e := evt.(type) {
	case *events.LoggedOut:
		c.end(Ended{Reason: "logged out: the device was unlinked on the phone; run aminte whatsapp login again"})
	case *events.StreamReplaced:
		c.end(Ended{Reason: "another client took this session over; one relay runs on a session"})
	case *events.TemporaryBan:
		c.end(Ended{Reason: fmt.Sprintf("WhatsApp banned the number for %s (reason %v)", e.Expire, e.Code)})
	case *events.ClientOutdated:
		c.end(Ended{Reason: "WhatsApp refused this client as outdated: build aminte-whatsapp with a newer whatsmeow"})
	case *events.ConnectFailure:
		c.end(Ended{Reason: fmt.Sprintf("the connection failed: %v %s", e.Reason, e.Message), Retry: true})
	case *events.Message:
		if e.Info.Sender.Server == types.HiddenUserServer && e.Info.SenderAlt.User == "" {
			if pn, err := c.cl.Store.LIDs.GetPNForLID(c.ctx, e.Info.Sender); err == nil {
				e.Info.SenderAlt = pn
			}
		}
		m, ok := Filter(e, c.after)
		if !ok {
			return
		}
		if m.Sender == c.owner.User { // the owner sees that the message arrived
			// The receipt is a call to the server: it runs beside the handler, which whatsmeow calls one event at a
			// time, so a slow receipt does not hold the next event. Close waits for it; it ends in 10 seconds at most.
			id, chat, sender := e.Info.ID, e.Info.Chat, e.Info.Sender
			c.reads.Add(1)
			go func() {
				defer c.reads.Done()
				rctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
				defer cancel()
				if err := c.cl.MarkRead(rctx, []types.MessageID{id}, time.Now(), chat, sender); err != nil && c.ctx.Err() == nil {
					c.log.Printf("mark read failed: %v", err)
				}
			}()
		}
		select {
		case c.msgs <- m:
		case <-c.done:
		case <-c.ctx.Done():
		}
	}
}
