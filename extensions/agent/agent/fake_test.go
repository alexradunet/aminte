package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aminte/client"
	"aminte/siren"
)

// token is the bearer token of the owner, as both clients of the rig send it.
const token = "owner-token"

// wait is a guard against a deadlock. Nothing waits for it in a green run.
const wait = 30 * time.Second

var fixedNow = time.Date(2031, 5, 20, 9, 30, 0, 0, time.UTC)

// today is the day that the fake API calls today, unless a test moves it (setToday).
const today = "2031-05-20"

// todayTitle is the title of the conversation page of that day.
const todayTitle = "Conversation 2031-05-20"

// fakePage is a page of the fake API.
type fakePage struct {
	id       int
	title    string
	body     string
	category string
	version  int
}

// seen is one request that the fake API received.
type seen struct{ Method, Path, Source, Auth, Body string }

// fakeAPI serves the catalog and a few pages, and answers every other path with an entity. A path that ends in /big
// answers a title that is longer than the bound of a result. A page title is redirected to its page, as the real
// API does. It keeps the rules of the real API that the agent depends on: every turn of the conversation has a key,
// reply writes the paragraph of aminte right after the owner turn that has the key that it names (404, 409 and 422 as
// the real API gives them), a line of the text that reads as a marker is escaped, and an agent may not save the body of
// a conversation page (403).
type fakeAPI struct {
	mu    sync.Mutex
	reqs  []seen
	pages []*fakePage
	// the settings of the model, as GET /settings answers them (an empty value is absent)
	modelURL, model, modelKey string
	extra                     []siren.Action   // actions that a connected app added to the catalog
	without                   []string         // actions that the catalog lacks
	catalogFails              bool             // GET /actions answers 503
	replyFails                bool             // reply answers 500
	replyFaults               []replyFault     // the next reply requests, one fault for each; then reply works
	loseAnswers               int              // the next replies are written, and the connection ends before the answer
	day                       string           // the local day of the server; "" is today
	now                       func() time.Time // the clock of the server; nil is fixedNow
	change                    chan struct{}    // closed at each change of a page
}

// notify tells the waiters that a page changed. The caller holds the lock.
func (f *fakeAPI) notify() {
	if f.change != nil {
		close(f.change)
	}
	f.change = make(chan struct{})
}

// waitBody waits until cond is true of the body of the page, and returns the body. It waits for a change, not for a
// time: the deadline is a guard against a deadlock.
func (f *fakeAPI) waitBody(t *testing.T, title string, cond func(body string) bool) string {
	t.Helper()
	deadline := time.After(wait)
	for {
		f.mu.Lock()
		body := ""
		if p := f.find(title); p != nil {
			body = p.body
		}
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
			t.Fatalf("the page %q did not reach the awaited state; body %q", title, body)
		}
	}
}

// waitReplyRequests waits until the API has received n requests of the action reply.
func (f *fakeAPI) waitReplyRequests(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(wait)
	for {
		f.mu.Lock()
		count := 0
		for _, q := range f.reqs {
			if q.Method == "POST" && strings.HasSuffix(q.Path, "/reply") {
				count++
			}
		}
		if f.change == nil {
			f.change = make(chan struct{})
		}
		ch := f.change
		f.mu.Unlock()
		if count >= n {
			return
		}
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("the API got %d requests of reply, want %d", count, n)
		}
	}
}

// waitReplies waits until the page holds n replies of the agent.
func (f *fakeAPI) waitReplies(t *testing.T, title string, n int) string {
	t.Helper()
	return f.waitBody(t, title, func(body string) bool { return strings.Count(body, "\n**aminte** (") >= n })
}

// setToday moves the local day of the server.
func (f *fakeAPI) setToday(day string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.day = day
}

// localDay is the local day of the server. The caller holds the lock.
func (f *fakeAPI) localDay() string {
	if f.day == "" {
		return today
	}
	return f.day
}

// say adds a turn of the owner to the page of today, as the action say does: it makes the page on the first turn.
func (f *fakeAPI) say(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	day := f.localDay()
	p := f.find("Conversation " + day)
	if p == nil {
		p = &fakePage{id: len(f.pages) + 1, title: "Conversation " + day, body: "[[" + day + "]]", category: "Conversations", version: 1}
		f.pages = append(f.pages, p)
	}
	p.body, p.version = p.body+"\n\n**Owner** (09:30): "+text, p.version+1
	f.notify()
}

// addAction adds an action to the catalog, as a connected app does when it registers.
func (f *fakeAPI) addAction(a siren.Action) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extra = append(f.extra, a)
}

// dropAction removes an action from the catalog.
func (f *fakeAPI) dropAction(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.without = append(f.without, name)
}

// failCatalog makes GET /actions fail (or work again).
func (f *fakeAPI) failCatalog(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogFails = on
}

// setReplyFails makes the action reply fail (or work again).
func (f *fakeAPI) setReplyFails(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyFails = on
}

// replyFault is a reply request that fails: the status that it answers, and an edit that the owner makes right after it.
// Nothing is written for the request itself.
type replyFault struct {
	status int
	edit   [3]string // title of the page, words to replace, new words; "" for no edit
}

// failReplies makes the next reply requests answer these statuses, one for each. Nothing is written for them. A 503 is
// the busy file of the real core (code busy).
func (f *fakeAPI) failReplies(statuses ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range statuses {
		f.replyFaults = append(f.replyFaults, replyFault{status: s})
	}
}

// failReplyThenEdit makes the next reply request answer the status, and the owner then edits the page: the words from
// become the words to. The edit comes before the next request of the agent, so the key of the turn is another one.
func (f *fakeAPI) failReplyThenEdit(status int, title, from, to string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyFaults = append(f.replyFaults, replyFault{status: status, edit: [3]string{title, from, to}})
}

// loseNextAnswer makes the next reply be written, and then ends the connection before the answer: the agent cannot tell
// whether the reply is on the page.
func (f *fakeAPI) loseNextAnswer() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loseAnswers++
}

// otherReplies adds a reply to the end of a page, as another writer of aminte turns would.
func (f *fakeAPI) otherReplies(title, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.find(title)
	p.body, p.version = p.body+"\n\n**aminte** (09:30): "+text, p.version+1
	f.notify()
}

// setModel changes the settings, as the owner would in aminte.
func (f *fakeAPI) setModel(url, name, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelURL, f.model, f.modelKey = url, name, key
}

// marker is the start of a turn, as the real API reads it.
var marker = regexp.MustCompile(`^\*\*(Owner|aminte)\*\* \((\d\d[:.]\d\d)(?:, via ([a-z0-9_:.-]+))?\): ?`)

// fakeTurn is a turn of a page as the fake API reads it. at is the index of the paragraph that holds its marker (0 for
// a paragraph before the first marker).
type fakeTurn struct {
	who, key, text string
	at             int
}

// turnsOf reads the turns of a body, as the real API reads them: a marker starts a turn, a block of tool lines
// belongs to the marker after it, and any other paragraph is part of the turn before. Each turn gets a key from what
// it is and not from its place: the same key after a reply, after a new turn and after the removal of an earlier turn,
// and a new key after an edit of its words. Two turns that are the same get different keys.
func turnsOf(day, body string) []*fakeTurn {
	var turns []*fakeTurn
	var stamps []string                                                                  // who, minute and relay of each turn
	for i, para := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") { // the real API reads CRLF as LF
		m := marker.FindStringSubmatch(para)
		switch {
		case m != nil:
			who := "owner"
			if m[1] == "aminte" {
				who = "aminte"
			}
			turns = append(turns, &fakeTurn{who: who, text: para[len(m[0]):], at: i})
			stamps = append(stamps, who+"|"+m[2]+"|"+m[3])
		case strings.HasPrefix(para, "[["+day+"]]"), strings.HasPrefix(para, "- `"):
			// the first line, or the tool lines of the marker that follows
		case len(turns) == 0:
			turns = append(turns, &fakeTurn{who: "text", text: para, at: i})
			stamps = append(stamps, "text||")
		default:
			turns[len(turns)-1].text += "\n\n" + para
		}
	}
	before := map[string]int{}
	for i, t := range turns {
		same := fmt.Sprintf("%s|%q", stamps[i], t.text)
		h := fnv.New64a()
		fmt.Fprintf(h, "%s|%d", same, before[same])
		before[same]++
		t.key = fmt.Sprintf("%016x", h.Sum64())
	}
	return turns
}

// keysOf are the keys of the turns of one kind (owner, aminte or text) of the conversation of a day, in the order of
// the page, as GET /days/{day}/conversation gives them.
func (f *fakeAPI) keysOf(who, day string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	if p := f.find("Conversation " + day); p != nil {
		for _, t := range turnsOf(day, p.body) {
			if t.who == who {
				keys = append(keys, t.key)
			}
		}
	}
	return keys
}

// conversation is the entity of GET /days/{day}/conversation: the day, and the page's id, version, body and turns when
// the page exists.
func (f *fakeAPI) conversation(day string) siren.Entity {
	if day == "today" {
		day = f.localDay()
	}
	props := map[string]any{"day": day, "turns": []any{}}
	p := f.find("Conversation " + day)
	if p == nil {
		return siren.Entity{Class: []string{"conversation"}, Properties: props}
	}
	props["id"], props["version"], props["body"] = p.id, p.version, p.body
	out := []any{}
	for _, t := range turnsOf(day, p.body) {
		out = append(out, map[string]any{"who": t.who, "key": t.key, "text": t.text})
	}
	props["turns"] = out
	return siren.Entity{Class: []string{"conversation"}, Properties: props}
}

// markerLine is a line that the real API reads as the start of a turn.
var markerLine = regexp.MustCompile(`(?m)^\*\*(Owner|aminte)\*\* \(`)

// reply is the action reply of the real API: the paragraph of aminte, with the tool lines before it, goes right
// before the marker of the turn that follows the owner turn that has the key, or at the end of the page. It gives the
// status and the code of a refusal, and 0 when the reply is written. A key that no owner turn has is 409, as it is
// for the real API: the page changed. The caller holds the lock.
func (f *fakeAPI) reply(day string, form url.Values) (int, string) {
	if len(f.replyFaults) > 0 {
		fault := f.replyFaults[0]
		f.replyFaults = f.replyFaults[1:]
		if title := fault.edit[0]; title != "" {
			p := f.find(title)
			p.body, p.version = strings.Replace(p.body, fault.edit[1], fault.edit[2], 1), p.version+1
		}
		if fault.status == 503 { // the busy file of the real core: nothing was written, and a retry is safe
			return fault.status, siren.CodeBusy
		}
		return fault.status, "unavailable"
	}
	if f.replyFails {
		return 500, "internal"
	}
	key := strings.TrimSpace(form.Get("turn"))
	text := strings.TrimSpace(strings.ReplaceAll(form.Get("text"), "\r\n", "\n"))
	if key == "" || text == "" {
		return 422, "invalid"
	}
	var tools []string
	for l := range strings.SplitSeq(strings.ReplaceAll(form.Get("tools"), "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		if !strings.HasPrefix(l, "- `") {
			return 422, "invalid"
		}
		tools = append(tools, l)
	}
	p := f.find("Conversation " + day)
	if p == nil {
		return 404, "not_found"
	}
	paras := strings.Split(strings.ReplaceAll(p.body, "\r\n", "\n"), "\n\n") // the core writes the page with LF line ends
	isMarker := func(para string, who string) bool { return strings.HasPrefix(para, "**"+who+"**") }
	at := -1
	for _, t := range turnsOf(day, p.body) {
		if t.who == "owner" && t.key == key {
			at = t.at
		}
	}
	if at < 0 {
		return 409, "conflict"
	}
	end := len(paras)
	for i := at + 1; i < len(paras); i++ {
		if isMarker(paras[i], "Owner") || isMarker(paras[i], "aminte") {
			end = i
			break
		}
	}
	if end < len(paras) && isMarker(paras[end], "aminte") {
		return 409, "conflict"
	}
	text = markerLine.ReplaceAllString(text, `\*\*$1\*\* (`)
	clock := fixedNow
	if f.now != nil {
		clock = f.now()
	}
	block := "**aminte** (" + clock.Format("15:04") + "): " + text
	if len(tools) > 0 {
		block = strings.Join(tools, "\n") + "\n\n" + block
	}
	out := append(slices.Clone(paras[:end]), block)
	p.body, p.version = strings.Join(append(out, paras[end:]...), "\n\n"), p.version+1
	f.notify()
	return 0, ""
}

func (f *fakeAPI) find(title string) *fakePage {
	for _, p := range f.pages {
		if p.title == title {
			return p
		}
	}
	return nil
}

// add makes a page, as another surface would.
func (f *fakeAPI) add(title, body string) *fakePage {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &fakePage{id: len(f.pages) + 1, title: title, body: body, version: 1}
	f.pages = append(f.pages, p)
	f.notify()
	return p
}

// edit replaces the body of a page, as another surface would.
func (f *fakeAPI) edit(title, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.find(title)
	p.body, p.version = body, p.version+1
	f.notify()
}

func (f *fakeAPI) body(title string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.find(title); p != nil {
		return p.body
	}
	return ""
}

func (p *fakePage) entity() siren.Entity {
	return siren.Entity{Title: p.title, Properties: map[string]any{"id": p.id, "version": p.version, "body": p.body},
		Links: []siren.Link{{Rel: []string{"self"}, Href: fmt.Sprintf("/pages/%d", p.id)}}}
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
		e = siren.Entity{Properties: map[string]any{"status": status, "code": code, "message": "no"}}
	}
	var id int
	src := r.Header.Get(siren.SourceHeader) // an agent or an import is not the owner
	notOwner := strings.HasPrefix(src, "agent:") || strings.HasPrefix(src, "import:")
	switch {
	case r.URL.Path == "/actions":
		if f.catalogFails {
			fail(503, "unavailable")
			break
		}
		var list []siren.Action
		for _, a := range append(catalog(), f.extra...) {
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
		for k, v := range map[string]string{"model_url": f.modelURL, "model": f.model, "model_key": f.modelKey} {
			if v != "" {
				props[k] = v
			}
		}
		e = siren.Entity{Class: []string{"settings"}, Properties: props}
	case strings.HasSuffix(r.URL.Path, "/conversation") && r.Method == "GET":
		e = f.conversation(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/days/"), "/conversation"))
	case strings.HasSuffix(r.URL.Path, "/body") && r.Method == "POST":
		if refuseUnknown(r, "save-body") {
			fail(422, "invalid")
			break
		}
		fmt.Sscanf(r.URL.Path, "/pages/%d/body", &id)
		p := f.pages[id-1]
		if notOwner && strings.HasPrefix(p.title, "Conversation ") { // the core refuses an agent: it writes with reply
			fail(403, "forbidden")
			break
		}
		if r.PostForm.Get("version") != fmt.Sprint(p.version) {
			fail(409, "stale_version")
			break
		}
		p.body, p.version = r.PostForm.Get("body"), p.version+1
		f.notify()
		e = p.entity()
	case strings.HasSuffix(r.URL.Path, "/reply") && r.Method == "POST":
		day := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/days/"), "/reply")
		if day == "today" {
			day = f.localDay()
		}
		if refuseUnknown(r, "reply") {
			fail(422, "invalid")
			break
		}
		status, code := f.reply(day, r.PostForm)
		f.notify()
		if status != 0 {
			fail(status, code)
			break
		}
		if f.loseAnswers > 0 { // written, and the answer never comes
			f.loseAnswers--
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		e = f.conversation(day)
	case strings.HasSuffix(r.URL.Path, "/big"):
		e.Title = strings.Repeat("é", maxResult)
	case strings.HasSuffix(r.URL.Path, "/gone"):
		fail(404, "not_found")
	case strings.HasSuffix(r.URL.Path, "/badcode"): // a connected app can send any code
		fail(409, "bad\ncode")
	case strings.HasSuffix(r.URL.Path, "/badhref"):
		e.Links = []siren.Link{{Rel: []string{"self"}, Href: "/pages/1\n2"}}
	}
	json.NewEncoder(w).Encode(e)
}

func (f *fakeAPI) requests() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.reqs...)
}

// writes are the requests that are not a GET and not a write of the conversation (reply, save-body).
func (f *fakeAPI) writes() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, r := range f.reqs {
		if r.Method != "GET" && !strings.HasPrefix(r.Path, "/pages") && !strings.HasSuffix(r.Path, "/reply") {
			out = append(out, r)
		}
	}
	return out
}

// replyReq is one request of the action reply: its path, its source and its form.
type replyReq struct {
	Path, Source string
	Form         url.Values
}

// replies are the requests of the action reply.
func (f *fakeAPI) replies() []replyReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []replyReq
	for _, r := range f.reqs {
		if r.Method == "POST" && strings.HasSuffix(r.Path, "/reply") {
			form, _ := url.ParseQuery(r.Body)
			out = append(out, replyReq{r.Path, r.Source, form})
		}
	}
	return out
}

// refuseUnknown reports a form name that the row of the action in the catalog does not list: the real API answers 422
// for it (API.md, "A request names only the fields of the action").
func refuseUnknown(r *http.Request, action string) bool {
	for _, a := range catalog() {
		if a.Name != action {
			continue
		}
		for name := range r.PostForm {
			if a.Field(name) == nil {
				return true
			}
		}
	}
	return false
}

func catalog() []siren.Action {
	return []siren.Action{
		{Name: "open-day", Method: "GET", Href: "/days/{day}", Fields: []siren.Field{{Name: "day", Type: "date", In: "path"}}},
		{Name: "capture", Method: "POST", Href: "/days/{day}/capture", Fields: []siren.Field{
			{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea", Title: "Text"}}},
		{Name: "say", Owner: true, Method: "POST", Href: "/days/{day}/say", Fields: []siren.Field{
			{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea", Required: true}, {Name: "via", Type: "hidden"}}},
		{Name: "reply", Method: "POST", Href: "/days/{day}/reply", Fields: []siren.Field{
			{Name: "day", Type: "date", In: "path", Required: true}, {Name: "turn", Type: "text", Required: true},
			{Name: "text", Type: "textarea", Required: true}, {Name: "tools", Type: "text"}}},
		{Name: "save-body", Method: "POST", Href: "/pages/{id}/body", Fields: []siren.Field{
			{Name: "id", Type: "number", In: "path", Required: true}, {Name: "body", Type: "textarea", Required: true}, {Name: "version", Type: "hidden", Required: true}}},
		{Name: "snapshot", Owner: true, Method: "POST", Href: "/snapshot"},
		{Name: "settings", Owner: true, Method: "POST", Href: "/settings", Fields: []siren.Field{
			{Name: "model_url", Type: "text"}, {Name: "model", Type: "text"}, {Name: "model_key", Type: "password"}}},
	}
}

// chatReq is the part of a request to the model that the tests read.
type chatReq struct {
	Model string
	Tools []struct {
		Function struct{ Name string }
	}
	Messages []struct {
		Role       string
		Content    string
		ToolCallID string `json:"tool_call_id"`
	}
}

// fakeModel answers the n-th request (from 1) with script(n, request). A script returns a JSON message.
type fakeModel struct {
	mu     sync.Mutex
	reqs   []chatReq
	script func(n int, r chatReq) (status int, body string)
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req chatReq
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	n := len(f.reqs)
	f.mu.Unlock()
	status, body := f.script(n, req)
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func (f *fakeModel) requests() []chatReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chatReq(nil), f.reqs...)
}

func text(s string) (int, string) {
	b, _ := json.Marshal(s)
	return 200, `{"choices":[{"message":{"role":"assistant","content":` + string(b) + `},"finish_reason":"stop"}]}`
}

func toolCall(name, args string) (int, string) {
	a, _ := json.Marshal(args)
	return 200, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call1","type":"function","function":{"name":"` +
		name + `","arguments":` + string(a) + `}}]},"finish_reason":"tool_calls"}]}`
}

// logBuffer is the log of the agent: the agent writes it and a test reads it.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// lines are the lines of the log that hold text.
func (l *logBuffer) lines(text string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for line := range strings.SplitSeq(l.b.String(), "\n") {
		if strings.Contains(line, text) {
			out = append(out, line)
		}
	}
	return out
}

// rig is an agent on fakes, started.
type rig struct {
	api    *fakeAPI
	mod    *fakeModel
	log    *logBuffer
	tick   chan time.Time
	cancel context.CancelFunc
	done   chan error
	mu     sync.Mutex
	now    time.Time
}

func (r *rig) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

// setClock moves the clock of the agent.
func (r *rig) setClock(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = t
}

func start(t *testing.T, script func(n int, r chatReq) (int, string)) *rig {
	t.Helper()
	return startOn(t, script, &fakeAPI{}, fixedNow)
}

// startOn starts an agent on an API that may have been used before, with its clock at now.
func startOn(t *testing.T, script func(n int, r chatReq) (int, string), api *fakeAPI, now time.Time) *rig {
	t.Helper()
	return startWithBackoff(t, script, api, now, time.Millisecond)
}

// startWithBackoff starts an agent that waits backoff before the second send of a reply.
func startWithBackoff(t *testing.T, script func(n int, r chatReq) (int, string), api *fakeAPI, now time.Time, backoff time.Duration) *rig {
	t.Helper()
	r := &rig{api: api, mod: &fakeModel{script: script}, log: new(logBuffer), tick: make(chan time.Time), done: make(chan error, 1), now: now}
	modSrv := httptest.NewServer(r.mod)
	t.Cleanup(modSrv.Close)
	r.api.setModel(modSrv.URL, "test-model", "")
	r.api.mu.Lock()
	r.api.now = r.clock // the server stamps a reply with the clock of the test
	r.api.mu.Unlock()
	apiSrv := httptest.NewServer(r.api)
	t.Cleanup(apiSrv.Close)
	a := &Agent{
		Client: client.Remote(apiSrv.URL, siren.AgentPrefix+Source, token), AsOwner: client.Remote(apiSrv.URL, "", token),
		Now: r.clock, Log: log.New(r.log, "", 0), Tick: r.tick, backoff: backoff,
	}
	ctx, cancel := context.WithCancel(t.Context())
	r.cancel = cancel
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(wait):
			t.Error("the agent did not stop")
		}
	})
	go func() { r.done <- a.Run(ctx) }()
	return r
}

// poll makes the agent look at the page and waits until it has taken the tick. The agent takes a tick only after the
// look before it is done.
func (r *rig) poll(t *testing.T) {
	t.Helper()
	select {
	case r.tick <- fixedNow:
	case <-time.After(wait):
		t.Fatal("the agent did not take the tick")
	}
}

// settle polls three times: the third tick is taken only after the second look is done, so the agent has had one
// full look at the page as it is now.
func (r *rig) settle(t *testing.T) {
	t.Helper()
	r.poll(t)
	r.poll(t)
	r.poll(t)
}
