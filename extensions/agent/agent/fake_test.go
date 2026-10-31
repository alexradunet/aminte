package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/siren"
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
// API does.
type fakeAPI struct {
	mu    sync.Mutex
	reqs  []seen
	pages []*fakePage
	// sneak, when it is not empty, is added to the body of the page by someone else just before the next save
	// (the version changes, so the save is stale). alwaysStale does it before every save.
	sneak       string
	alwaysStale bool
	// the settings of the model, as GET /settings answers them (an empty value is absent)
	modelURL, model, modelKey string
	extra                     []siren.Action // actions that a connected app added to the catalog
	without                   []string       // actions that the catalog lacks
	catalogFails              bool           // GET /actions answers 503
	saveFails                 bool           // save-body answers 500
	day                       string         // the local day of the server; "" is today
	change                    chan struct{}  // closed at each change of a page
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

// waitReplies waits until the page holds n replies of the agent.
func (f *fakeAPI) waitReplies(t *testing.T, title string, n int) string {
	t.Helper()
	return f.waitBody(t, title, func(body string) bool { return strings.Count(body, "\n**lifelog** (") >= n })
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

// setSaveFails makes the save of a page fail (or work again).
func (f *fakeAPI) setSaveFails(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveFails = on
}

// setModel changes the settings, as the owner would in lifelog.
func (f *fakeAPI) setModel(url, name, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelURL, f.model, f.modelKey = url, name, key
}

// conversation is the entity of GET /days/{day}/conversation: the day, and the page's id, version, body and turns when
// the page exists. The turns are the paragraphs of the body, as the real API reads them: a marker starts a turn, a
// block of tool lines belongs to the marker after it, and any other paragraph is part of the turn before.
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
	type turn struct{ who, text string }
	var turns []*turn
	for _, para := range strings.Split(strings.ReplaceAll(p.body, "\r\n", "\n"), "\n\n") { // the real API reads CRLF as LF
		who := ""
		switch {
		case strings.HasPrefix(para, "**Owner**"):
			who = "owner"
		case strings.HasPrefix(para, "**lifelog**"):
			who = "lifelog"
		}
		switch {
		case who != "":
			text := para
			if i := strings.Index(para, "): "); i >= 0 {
				text = para[i+3:]
			}
			turns = append(turns, &turn{who, text})
		case strings.HasPrefix(para, "[["+day+"]]"), strings.HasPrefix(para, "- `"):
			// the first line, or the tool lines of the marker that follows
		case len(turns) == 0:
			turns = append(turns, &turn{"text", para})
		default:
			turns[len(turns)-1].text += "\n\n" + para
		}
	}
	out := []any{}
	for _, t := range turns {
		out = append(out, map[string]any{"who": t.who, "text": t.text})
	}
	props["turns"] = out
	return siren.Entity{Class: []string{"conversation"}, Properties: props}
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
		if f.saveFails {
			fail(500, "internal")
			break
		}
		fmt.Sscanf(r.URL.Path, "/pages/%d/body", &id)
		p := f.pages[id-1]
		if f.sneak != "" || f.alwaysStale {
			p.body, p.version = p.body+"\n\n"+f.sneak+"x", p.version+1
			f.sneak = ""
			f.notify()
		}
		if r.PostForm.Get("version") != fmt.Sprint(p.version) {
			fail(409, "stale_version")
			break
		}
		p.body, p.version = r.PostForm.Get("body"), p.version+1
		f.notify()
		e = p.entity()
	case strings.HasSuffix(r.URL.Path, "/big"):
		e.Title = strings.Repeat("é", maxResult)
	case strings.HasSuffix(r.URL.Path, "/gone"):
		fail(404, "not_found")
	}
	json.NewEncoder(w).Encode(e)
}

func (f *fakeAPI) requests() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.reqs...)
}

// writes are the requests that are not a GET and not a write of a page (save-body).
func (f *fakeAPI) writes() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, r := range f.reqs {
		if r.Method != "GET" && !strings.HasPrefix(r.Path, "/pages") {
			out = append(out, r)
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

// rig is an agent on fakes, started.
type rig struct {
	api    *fakeAPI
	mod    *fakeModel
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
	r := &rig{api: api, mod: &fakeModel{script: script}, tick: make(chan time.Time), done: make(chan error, 1), now: now}
	modSrv := httptest.NewServer(r.mod)
	t.Cleanup(modSrv.Close)
	r.api.setModel(modSrv.URL, "test-model", "")
	apiSrv := httptest.NewServer(r.api)
	t.Cleanup(apiSrv.Close)
	a := &Agent{
		Client: client.Remote(apiSrv.URL, siren.AgentPrefix+Source, token), AsOwner: client.Remote(apiSrv.URL, "", token),
		Now: r.clock, Log: log.New(io.Discard, "", 0), Tick: r.tick,
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
