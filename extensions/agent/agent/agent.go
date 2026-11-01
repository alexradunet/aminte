// Package agent answers the owner's turns of the conversation page with a model that calls the actions of the
// lifelog API. The page is the truth: the agent reads it, and a turn of the owner after its last reply is a turn to
// answer. The core writes both kinds of turn: the action say writes the owner's, and the action reply writes the
// agent's, right after the owner turn that it answers. A chat (the browser, WhatsApp) shows the page.
package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"lifelog-agent/model"
	"lifelog/client"
	"lifelog/client/tools"
	"lifelog/siren"
)

const (
	// maxSteps is the number of model steps in one turn.
	maxSteps = 16
	// turnTimeout is the longest time of one turn. One model step has its own limit (package model).
	turnTimeout = 5 * time.Minute
	// replyTimeout is the longest time of the write of a reply. It has its own limit: the timeout of the turn may be
	// spent, and the reply must reach the page.
	replyTimeout = 30 * time.Second
	// replyAttempts is the number of sends of one reply at most: the first, and the sends after a failure that a send
	// can mend (see send). They all fit in replyTimeout.
	replyAttempts = 4
	// maxResult is the largest tool result that goes to the model.
	maxResult = 64 << 10
	// maxBody is the largest part of the conversation that goes to the model. The end is kept.
	maxBody = 200 << 10
	// pollEvery is the time between two looks at today's page.
	pollEvery = 3 * time.Second
)

// Source is the name of the writer: the rows that the agent writes carry agent:lifelog.
const Source = "lifelog"

// noModel is the reply of a turn when the settings of lifelog name no model.
const noModel = "No model is set: open Settings."

// errNoModel says that the settings hold no model_url or no model.
var errNoModel = errors.New("no model is set")

// ErrNoAction says that the catalog lacks an action that the agent needs. The owner acts first: the server is too
// old, or it is not a lifelog server. A restart of the agent does not help.
var ErrNoAction = errors.New("the catalog lacks an action")

// Agent answers the turns of the owner. The conversation of a day is a page of life.db.
type Agent struct {
	Client *client.Client // sends siren.AgentPrefix + Source on every request, and so do the tools of the model
	// AsOwner is the owner's client: the owner's token, no source. The read of the settings uses it, because the
	// settings hold the key of the model, and the API refuses them to an agent. The model never has this client.
	AsOwner *client.Client
	Now     func() time.Time
	Log     *log.Logger // nil: the standard logger
	// Tick gives the moments when the agent looks at the page. nil: every three seconds.
	Tick <-chan time.Time

	logger *log.Logger
	// backoff is the wait before the second send of a reply. The wait doubles for each send after it. 0: one second. A
	// test sets it short.
	backoff time.Duration
	// tools and actions come from one read of the catalog. Only Run uses them, from one goroutine: they need no lock.
	tools   []tools.Tool
	actions map[string]siren.Action
	// checked is the day of the server for which the day before was looked at: on the first look, and on each new day.
	checked string
	// done is the key of the page that the agent read when its reply was refused or failed. The agent does not answer
	// that page again: a new run of the model would repeat the tool calls of the first run. It answers again when the
	// page changes. A 409 for the key of a turn means that the page changed (the owner edited the turn, or removed
	// it): the next look reads the new page, and the agent answers the turns that wait on it.
	done    string
	lookErr string
}

// Run answers the turns of the owner until ctx ends (it returns the error of ctx). It reads the catalog of the API
// first. It then looks at the page at once and at each tick, and runs one turn at a time.
func (a *Agent) Run(ctx context.Context) error {
	a.logger = a.Log
	if a.logger == nil {
		a.logger = log.Default()
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	if err := a.readCatalog(ctx); err != nil {
		return err
	}
	tick := a.Tick
	if tick == nil {
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		tick = t.C
	}
	for {
		a.look(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick:
		}
	}
}

// readCatalog reads the catalog and replaces the tools and the actions with it. The tools and the actions come from
// the same read, so a tool and its action agree. A catalog with no say or no reply is ErrNoAction: say is the only
// way to write a turn of the owner, so with no say there is nothing to answer, and reply is the only way that the
// core takes the agent's turn. On an error the tools and the actions stay as they were.
func (a *Agent) readCatalog(ctx context.Context) error {
	list, err := a.Client.Catalog(ctx)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	actions := map[string]siren.Action{}
	for _, x := range list {
		actions[x.Name] = x
	}
	for _, name := range []string{"say", "reply"} {
		if _, ok := actions[name]; !ok {
			return fmt.Errorf("%w: %q", ErrNoAction, name)
		}
	}
	ts, err := toolsOf(list)
	if err != nil {
		return err
	}
	a.tools, a.actions = ts, actions
	return nil
}

// model reads the settings of lifelog and makes the client of the model. It is called at each turn, so a change in
// Settings is seen at once. The read is the owner's: the client AsOwner carries no source. The tools of the model
// use Client, so their read of /settings is an agent's, and the API refuses it. The key is never in an error.
func (a *Agent) model(ctx context.Context) (*model.Client, error) {
	e, err := a.AsOwner.Get(ctx, "/settings")
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	p, _ := e.Properties.(map[string]any)
	url, _ := p["model_url"].(string)
	name, _ := p["model"].(string)
	key, _ := p["model_key"].(string)
	if url == "" || name == "" {
		return nil, errNoModel
	}
	return model.New(url, name, key), nil
}

// look reads the page and answers a turn that waits. It logs a failure once for each kind, not every three seconds.
func (a *Agent) look(ctx context.Context) {
	err := a.lookAt(ctx)
	if ctx.Err() != nil {
		return
	}
	switch msg := fmt.Sprint(err); {
	case err == nil:
		a.lookErr = ""
	case msg != a.lookErr:
		a.lookErr = msg
		a.logger.Printf("look: %v", err)
	}
}

// lookAt answers the turn of the owner that waits on today's page. On the first look and on a new day it answers a
// turn that waits on the page of the day before first: a turn said before midnight is answered on its own page.
func (a *Agent) lookAt(ctx context.Context) error {
	today, err := a.read(ctx, "today")
	if err != nil {
		return err
	}
	if a.checked != today.Day {
		before, err := dayBefore(today.Day)
		if err != nil {
			return err
		}
		yesterday, err := a.read(ctx, before)
		if err != nil {
			return err
		}
		a.checked = today.Day
		if yesterday.pending() {
			a.logger.Printf("answering a turn that waited on the page of %s", yesterday.Day)
			a.turn(ctx, yesterday)
			return nil // the page of today is read again at the next look: the turn took time
		}
	}
	if today.pending() {
		a.turn(ctx, today)
	}
	return nil
}

// dayBefore is the day before a day (YYYY-MM-DD).
func dayBefore(day string) (string, error) {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return "", fmt.Errorf("the server gave the day %q", day)
	}
	return t.AddDate(0, 0, -1).Format(time.DateOnly), nil
}

// turn answers the owner turns that wait on the page p: it runs the model once on the page, and it writes the tool
// lines and the reply with the action reply in one request, so the page has the whole answer or none. The reply
// answers the last owner turn that the model read, and the field turn is the key of that turn. The core puts the reply
// right after that turn, wherever the turn stands then, so a turn that the owner says meanwhile stays after the reply
// and waits for its own run. If the owner edits the answered turn meanwhile, its key is not on the page any more and the
// core refuses the reply (409): the agent reads the page again at the next look.
func (a *Agent) turn(ctx context.Context, p *page) {
	if p.key() == a.done {
		return
	}
	tctx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()
	at := a.Now() // one time for the turn: the model reads the same day through the turn, also across midnight
	reply, lines, err := a.reply(tctx, p, at)
	if ctx.Err() != nil {
		return
	}
	if err != nil { // only the timeout of the turn reaches here
		reply = "I stopped: the turn took more than 5 minutes."
	}
	wctx, wcancel := context.WithTimeout(ctx, replyTimeout)
	defer wcancel()
	err = a.send(wctx, map[string]string{
		"day": p.Day, "turn": p.lastOwner(), "text": reply, "tools": strings.Join(lines, "\n"),
	})
	if err != nil {
		if ctx.Err() == nil {
			a.logger.Printf("reply was refused or failed (turn %s of %s): %v", p.lastOwner(), p.Day, err)
			a.done = p.key()
		}
		return
	}
	a.logger.Printf("turn answered: %d bytes out, %d tool calls", len(reply), len(lines))
}

// send writes a reply with the action reply. A second send of the same reply cannot answer a turn twice, because the
// core refuses a reply to a turn that has one (409). So a failure that a second send can mend is sent again, up to
// replyAttempts sends in all, with a wait that doubles before each, and all within the time of ctx. Such a failure is a
// 5xx (the file is busy) or an error with no status (the request or its answer was lost). A 4xx is the answer of the
// core about this reply, and the same reply gets the same answer, so it is not sent again. A 409 after the first send
// says that an earlier send was written and only its answer was lost: the reply is on the page. If the page changed
// instead (the owner edited the turn between two sends), the next look reads it again and answers the turn that waits.
func (a *Agent) send(ctx context.Context, form map[string]string) error {
	wait := a.backoff
	if wait == 0 {
		wait = time.Second
	}
	for try := 1; ; try++ {
		_, err := a.Client.Do(ctx, a.actions["reply"], form)
		var ce *client.Error
		refused := errors.As(err, &ce)
		switch {
		case err == nil:
			return nil
		case try > 1 && refused && ce.Code() == siren.CodeConflict:
			return nil
		case try == replyAttempts || ctx.Err() != nil || (refused && ce.Status < 500):
			return err
		}
		select {
		case <-time.After(wait):
			wait *= 2
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// reply makes the reply of a turn on the page p. The error is the error of ctx.
func (a *Agent) reply(ctx context.Context, p *page, at time.Time) (string, []string, error) {
	// An app that serve --with registers can join the catalog after the start, and leave it when it restarts. A
	// failed read keeps the last good tools: the turn goes on.
	if err := a.readCatalog(ctx); err != nil && ctx.Err() == nil {
		a.logger.Printf("%v: the turn keeps the tools of the last read", err)
	}
	m, err := a.model(ctx)
	switch {
	case ctx.Err() != nil:
		return "", nil, ctx.Err()
	case errors.Is(err, errNoModel):
		a.logger.Printf("%v: open Settings in lifelog", err)
		return noModel, nil, nil
	case err != nil:
		a.logger.Printf("%v", err)
		return "I could not read the settings: " + firstLine(err.Error()) + ".", nil, nil
	}
	return a.answer(ctx, m, p, at)
}

// read is the conversation of a day (a local day, or the word today): GET /days/{day}/conversation. The page has no
// version and no turns when the day has no page yet.
func (a *Agent) read(ctx context.Context, day string) (*page, error) {
	e, err := a.Client.Get(ctx, "/days/"+day+"/conversation")
	if err != nil {
		return nil, err
	}
	return pageFrom(e)
}

// answer runs the steps of one turn on the body of the page p, at the time at. It returns the reply and one line for
// each tool call. Its error is the error of ctx.
func (a *Agent) answer(ctx context.Context, m *model.Client, p *page, at time.Time) (string, []string, error) {
	msgs := []model.Message{model.Text("system", systemText(at, p.Day)), model.Text("user", tail(p.Body))}
	fns := make([]model.Tool, len(a.tools))
	for i, t := range a.tools {
		fns[i] = model.Tool{Name: t.Name, Description: t.Description, Parameters: t.Schema}
	}
	last := ""
	var lines []string
	for step := 1; step <= maxSteps; step++ {
		r, err := m.Chat(ctx, msgs, fns)
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		if err != nil {
			a.logger.Printf("step %d: %v", step, err)
			return "The model failed: " + firstLine(err.Error()) + ".", lines, nil
		}
		calls := r.Message.ToolCalls
		a.logger.Printf("step %d: %d tool calls, %d bytes of text", step, len(calls), len(r.Text()))
		if text := strings.TrimSpace(r.Text()); text != "" {
			last = text
		}
		if len(calls) == 0 {
			if last == "" {
				return "The model sent no text.", lines, nil
			}
			return last, lines, nil
		}
		msgs = append(msgs, r.Message)
		for _, c := range calls {
			result, line := a.call(ctx, c.Function.Name, string(c.Function.Arguments))
			if ctx.Err() != nil {
				return "", nil, ctx.Err()
			}
			lines = append(lines, line)
			msgs = append(msgs, model.Message{Role: "tool", ToolCallID: c.ID, Content: &result})
		}
	}
	if last != "" {
		return last, lines, nil
	}
	return fmt.Sprintf("I stopped after %d steps.", maxSteps), lines, nil
}

// call runs one tool call. It returns the text for the model (the Siren entity as JSON, cut at maxResult, or the
// sentence of an error; an error of the API is its error entity, which has the status) and the line for the page. The
// line is one line: the name comes from the model, and the code and the href from the server, and none is trusted.
func (a *Agent) call(ctx context.Context, name, rawArgs string) (result, line string) {
	label := oneLine(name)
	var t *tools.Tool
	for i := range a.tools {
		if a.tools[i].Name == name {
			t = &a.tools[i]
		}
	}
	if t == nil {
		return fmt.Sprintf("error: there is no tool %q", name), fmt.Sprintf("- `%s` failed: unknown tool", label)
	}
	args, err := tools.Arguments([]byte(rawArgs))
	if err != nil {
		return "error: " + err.Error(), fmt.Sprintf("- `%s` failed: bad arguments", label)
	}
	e, err := tools.Run(ctx, a.Client, *t, args)
	if err != nil {
		code := "error"
		var ce *client.Error
		if errors.As(err, &ce) && ce.Code() != "" {
			code = ce.Code()
		}
		line = fmt.Sprintf("- `%s` failed: %s", label, oneLine(code))
	} else {
		line = fmt.Sprintf("- `%s`", label)
		if self := e.LinkByRel("self"); self != nil {
			href := oneLine(self.Href)
			line += fmt.Sprintf(" → [%s](%s)", href, href)
		}
	}
	if e == nil {
		return "error: " + err.Error(), line // no entity: the sentence of the error stands alone
	}
	var b bytes.Buffer
	if werr := siren.WriteJSON(&b, e, ""); werr != nil {
		return "error: " + werr.Error(), line
	}
	return cut(strings.TrimSuffix(b.String(), "\n")), line
}

// oneLine is s with each character that ends or breaks a line (a control character, a line separator or a paragraph
// separator) replaced by a space. The core refuses a reply (422) whose field tools has a line that does not start with
// the mark of a tool line, and the whole reply would be lost.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
}

// cut shortens s to maxResult bytes at a character boundary and says so.
func cut(s string) string {
	if len(s) <= maxResult {
		return s
	}
	n := maxResult
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return fmt.Sprintf("%s\n[cut: the result has %d bytes; the first %d are shown]", s[:n], len(s), n)
}

// tail keeps the last maxBody bytes of the conversation, from a character boundary, and says what it cut.
func tail(s string) string {
	if len(s) <= maxBody {
		return s
	}
	n := len(s) - maxBody
	for n < len(s) && !utf8.RuneStart(s[n]) {
		n++
	}
	return fmt.Sprintf("[the first %d bytes of this conversation are cut]\n%s", n, s[n:])
}

// firstLine is the first line of s.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
