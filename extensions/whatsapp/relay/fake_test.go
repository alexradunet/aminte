package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/siren"
)

const owner = "40712345678"

// token is the bearer token of the owner, as the client of the rig sends it.
const token = "owner-token"

// wait is a guard against a deadlock. Nothing waits for it in a green run.
const wait = 30 * time.Second

var fixedNow = time.Date(2031, 5, 20, 9, 30, 0, 0, time.UTC)

// today is the day that the fake API calls today, unless a test moves it (setToday).
const today = "2031-05-20"

// seen is one request that the fake API received.
type seen struct{ Method, Path, Source, Auth, Body string }

// fakeAPI serves the catalog, the settings and the conversation pages. A page is the body of a day.
type fakeAPI struct {
	mu          sync.Mutex
	reqs        []seen
	bodies      map[string]string // the body of the conversation page of a day; a day with no key has no page
	day         string            // the local day of the server; "" is today
	ownerNumber string
	sayFails    bool
	failCatalog bool
	without     []string      // actions that the catalog lacks
	change      chan struct{} // closed at each change of a page
}

// notify tells the waiters that a page changed. The caller holds the lock.
func (f *fakeAPI) notify() {
	if f.change != nil {
		close(f.change)
	}
	f.change = make(chan struct{})
}

// waitBody waits until cond is true of the body of the page of a day. It waits for a change, not for a time: the
// deadline is a guard against a deadlock.
func (f *fakeAPI) waitBody(t *testing.T, day string, cond func(body string) bool) string {
	t.Helper()
	deadline := time.After(wait)
	for {
		f.mu.Lock()
		body := f.bodies[day]
		if f.change == nil {
			f.change = make(chan struct{})
		}
		ch := f.change
		f.mu.Unlock()
		if cond(body) {
			return body
		}
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("the page of %s did not reach the awaited state; body %q", day, body)
		}
	}
}

func (f *fakeAPI) localDay() string {
	if f.day == "" {
		return today
	}
	return f.day
}

func (f *fakeAPI) setToday(day string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.day = day
}

func (f *fakeAPI) setOwnerNumber(n string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ownerNumber = n
}

func (f *fakeAPI) setSayFails(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sayFails = on
}

func (f *fakeAPI) setCatalogFails(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCatalog = on
}

func (f *fakeAPI) dropAction(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.without = append(f.without, name)
}

// appendTo adds a paragraph to the page of a day, and makes the page when it is new. The caller holds the lock.
func (f *fakeAPI) appendTo(day, paragraph string) {
	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	body, ok := f.bodies[day]
	if !ok {
		body = "[[" + day + "]]"
	}
	f.bodies[day] = body + "\n\n" + paragraph
	f.notify()
}

// ownerSaysElsewhere adds a turn of the owner that no relay carried, as the browser does.
func (f *fakeAPI) ownerSaysElsewhere(day, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendTo(day, "**Owner** (09:30): "+text)
}

// agentReplies adds a reply of the agent to the page of a day.
func (f *fakeAPI) agentReplies(day, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendTo(day, "**lifelog** (09:31): "+text)
}

// edit replaces the body of a page, as a save-body of another surface would.
func (f *fakeAPI) edit(day, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies[day] = body
	f.notify()
}

// remove takes the page of a day away, as a delete does: the conversation of the day has no turns. It gives the body, so
// that a test can bring the page back.
func (f *fakeAPI) remove(day string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	body := f.bodies[day]
	delete(f.bodies, day)
	f.notify()
	return body
}

func (f *fakeAPI) body(day string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[day]
}

var marker = regexp.MustCompile(`^\*\*(Owner|lifelog)\*\* \((\d\d:\d\d)(?:, via ([a-z0-9_:.-]+))?\): ?`)

// conversation is the entity of GET /days/{day}/conversation: the day, and the turns when the page exists. The turns
// are the paragraphs of the body, as the real API reads them: a marker starts a turn, a block of tool lines belongs to
// the marker after it, and any other paragraph is part of the turn before.
func (f *fakeAPI) conversation(day string) siren.Entity {
	if day == "today" {
		day = f.localDay()
	}
	props := map[string]any{"day": day, "turns": []any{}}
	body, ok := f.bodies[day]
	if !ok {
		return siren.Entity{Class: []string{"conversation"}, Properties: props}
	}
	type turn struct{ who, at, via, text string }
	var turns []*turn
	for _, para := range strings.Split(body, "\n\n") {
		m := marker.FindStringSubmatch(para)
		switch {
		case m != nil:
			who := "owner"
			if m[1] == "lifelog" {
				who = "lifelog"
			}
			turns = append(turns, &turn{who, m[2], m[3], para[len(m[0]):]})
		case strings.HasPrefix(para, "[["+day+"]]"), strings.HasPrefix(para, "- `"):
			// the first line, or the tool lines of the marker that follows
		case len(turns) == 0:
			turns = append(turns, &turn{"text", "", "", para})
		default:
			turns[len(turns)-1].text += "\n\n" + para
		}
	}
	out := []any{}
	for _, t := range turns {
		m := map[string]any{"who": t.who, "text": t.text}
		if t.at != "" {
			m["at"] = t.at
		}
		if t.via != "" {
			m["via"] = t.via
		}
		out = append(out, m)
	}
	props["id"], props["body"], props["turns"] = 1, body, out
	return siren.Entity{Class: []string{"conversation"}, Properties: props}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, seen{r.Method, r.URL.Path, r.Header.Get(siren.SourceHeader), r.Header.Get("Authorization"), string(data)})
	w.Header().Set("Content-Type", "application/vnd.siren+json")
	e := siren.Entity{Title: "ok " + r.URL.Path, Links: []siren.Link{{Rel: []string{"self"}, Href: r.URL.Path}}}
	fail := func(status int, code string) {
		w.WriteHeader(status)
		e = siren.Entity{Properties: map[string]any{"status": status, "code": code, "message": "no\nsecond line"}}
	}
	src := r.Header.Get(siren.SourceHeader) // an agent or an import is not the owner
	notOwner := strings.HasPrefix(src, "agent:") || strings.HasPrefix(src, "import:")
	switch {
	case r.URL.Path == "/actions":
		if f.failCatalog {
			fail(503, "unavailable")
			break
		}
		var list []siren.Action
		for _, a := range catalog() {
			if !slices.Contains(f.without, a.Name) {
				list = append(list, a)
			}
		}
		e = siren.Entity{Actions: list}
	case r.URL.Path == "/settings" && r.Method == "GET":
		if notOwner {
			fail(403, "forbidden")
			break
		}
		props := map[string]any{}
		if f.ownerNumber != "" {
			props["owner_number"] = f.ownerNumber
		}
		e = siren.Entity{Class: []string{"settings"}, Properties: props}
	case strings.HasSuffix(r.URL.Path, "/conversation") && r.Method == "GET":
		e = f.conversation(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/days/"), "/conversation"))
	case strings.HasSuffix(r.URL.Path, "/say") && r.Method == "POST":
		switch {
		case notOwner:
			fail(403, "forbidden")
		case refuseUnknown(r):
			fail(422, "invalid")
		case f.sayFails:
			fail(422, "invalid")
		default:
			day := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/days/"), "/say")
			if day == "today" {
				day = f.localDay()
			}
			stamp := "(09:30)"
			if via := r.PostForm.Get("via"); via != "" {
				stamp = "(09:30, via " + via + ")"
			}
			f.appendTo(day, "**Owner** "+stamp+": "+r.PostForm.Get("text"))
			e = f.conversation(day)
		}
	}
	json.NewEncoder(w).Encode(e)
}

func (f *fakeAPI) requests() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.reqs...)
}

// says are the requests of the action say.
func (f *fakeAPI) says() []seen {
	var out []seen
	for _, q := range f.requests() {
		if q.Method == "POST" && strings.HasSuffix(q.Path, "/say") {
			out = append(out, q)
		}
	}
	return out
}

// refuseUnknown reports a form name that the row of say in the catalog does not list: the real API answers 422 for it.
func refuseUnknown(r *http.Request) bool {
	for _, a := range catalog() {
		for name := range r.PostForm {
			if a.Name == "say" && a.Field(name) == nil {
				return true
			}
		}
	}
	return false
}

func catalog() []siren.Action {
	return []siren.Action{
		{Name: "say", Owner: true, Method: "POST", Href: "/days/{day}/say", Fields: []siren.Field{
			{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea", Required: true}, {Name: "via", Type: "hidden"}}},
		{Name: "save-body", Method: "POST", Href: "/pages/{id}/body", Fields: []siren.Field{
			{Name: "id", Type: "number", In: "path", Required: true}, {Name: "body", Type: "textarea", Required: true}, {Name: "version", Type: "hidden", Required: true}}},
	}
}

// fakeChat is a chat that the test drives: in carries the messages of the owner, sent holds what the relay sent.
type fakeChat struct {
	in       chan Message
	sent     chan string
	mu       sync.Mutex
	failSend bool
}

func newFakeChat() *fakeChat { return &fakeChat{in: make(chan Message), sent: make(chan string, 100)} }

func (c *fakeChat) Messages() <-chan Message { return c.in }

func (c *fakeChat) Send(_ context.Context, s string) error {
	c.mu.Lock()
	fail := c.failSend
	c.mu.Unlock()
	if fail {
		return errors.New("the chat is down")
	}
	c.sent <- s
	return nil
}

func (c *fakeChat) setFailSend(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failSend = on
}

// say puts a text of the owner in the chat. It returns when the relay has taken it.
func (c *fakeChat) say(t *testing.T, s string) {
	c.deliver(t, Message{Sender: owner, Text: s, Kind: KindText, At: fixedNow})
}

func (c *fakeChat) deliver(t *testing.T, m Message) {
	t.Helper()
	select {
	case c.in <- m:
	case <-time.After(wait):
		t.Fatal("the relay did not take the message")
	}
}

// next is the next text that the relay sent to the chat.
func (c *fakeChat) next(t *testing.T) string {
	t.Helper()
	select {
	case s := <-c.sent:
		return s
	case <-time.After(wait):
		t.Fatal("nothing was sent to the chat")
		return ""
	}
}

// none checks that the relay sent nothing more.
func (c *fakeChat) none(t *testing.T) {
	t.Helper()
	select {
	case s := <-c.sent:
		t.Errorf("an extra text was sent to the chat: %q", s)
	default:
	}
}

// rig is a relay on fakes, started.
type rig struct {
	api    *fakeAPI
	chat   *fakeChat
	state  string
	tick   chan time.Time
	cancel context.CancelFunc
	done   chan error
}

func start(t *testing.T) *rig {
	t.Helper()
	return startOn(t, &fakeAPI{}, t.TempDir()+"/state.json")
}

// startOn starts a relay on an API and a state file that may have been used before.
func startOn(t *testing.T, api *fakeAPI, state string) *rig {
	t.Helper()
	r := &rig{api: api, chat: newFakeChat(), state: state, tick: make(chan time.Time), done: make(chan error, 1)}
	apiSrv := httptest.NewServer(r.api)
	t.Cleanup(apiSrv.Close)
	rl := &Relay{Chat: r.chat, API: client.Remote(apiSrv.URL, "", token), Owner: owner, State: state, Log: log.New(io.Discard, "", 0), Tick: r.tick}
	ctx, cancel := context.WithCancel(t.Context())
	r.cancel = cancel
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(wait):
			t.Error("the relay did not stop")
		}
	})
	go func() { r.done <- rl.Run(ctx) }()
	return r
}

// poll makes the relay look at the page and waits until it has taken the tick. The relay takes a tick only after the
// look before it is done.
func (r *rig) poll(t *testing.T) {
	t.Helper()
	select {
	case r.tick <- fixedNow:
	case <-time.After(wait):
		t.Fatal("the relay did not take the tick")
	}
}

// settle polls three times: the third tick is taken only after the second look is done, so the relay has had one full
// look at the page as it is now.
func (r *rig) settle(t *testing.T) {
	t.Helper()
	r.poll(t)
	r.poll(t)
	r.poll(t)
}

// stop cancels the relay and waits for Run.
func (r *rig) stop(t *testing.T) error {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		r.done <- err // the cleanup reads it again
		return err
	case <-time.After(wait):
		t.Fatal("Run did not return")
		return nil
	}
}
