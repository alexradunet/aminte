package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/siren"
)

// userOf is the user message of a model request: the conversation page.
func userOf(r chatReq) string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			return r.Messages[i].Content
		}
	}
	return ""
}

// toolNames are the names of the tools of a model request.
func toolNames(r chatReq) []string {
	var names []string
	for _, x := range r.Tools {
		names = append(names, x.Function.Name)
	}
	return names
}

// pathsOf are the paths of the requests of one method, in order.
func pathsOf(f *fakeAPI, method string) []string {
	var out []string
	for _, q := range f.requests() {
		if q.Method == method {
			out = append(out, q.Path)
		}
	}
	return out
}

// turnsReplied are the values of the field turn of the reply requests, in order.
func turnsReplied(f *fakeAPI) []string {
	var out []string
	for _, q := range f.replies() {
		out = append(out, q.Form.Get("turn"))
	}
	return out
}

func TestATurnOfTheOwnerGetsItsToolLinesAndItsReplyOnThePageInOneReply(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("capture", `{"day":"2031-05-20","text":"Walked by the river."}`)
		}
		return text("Written to today.")
	})
	r.api.say("I walked by the river")
	r.poll(t)
	got := r.api.waitReplies(t, todayTitle, 1)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): I walked by the river\n\n- `capture` → [/days/2031-05-20/capture](/days/2031-05-20/capture)\n\n**lifelog** (09:30): Written to today."
	if got != want {
		t.Errorf("page body\n%q\nwant\n%q", got, want)
	}
	if p := r.api.find(todayTitle); p.category != "Conversations" || p.version != 3 {
		t.Errorf("page %+v, want the version after one say and one reply", p)
	}
	for _, q := range r.api.requests() {
		if strings.HasSuffix(q.Path, "/say") || (q.Method == "POST" && strings.HasPrefix(q.Path, "/pages")) {
			t.Errorf("the agent sent the request %+v: only a surface of the owner says, and the core refuses an agent that saves the conversation", q)
		}
	}
	// One request writes the whole answer: the turn it answers, the words, the tool lines. The core stamps the turn and
	// keeps the page, so the request has no body, no version, no via and no time.
	replies := r.api.replies()
	if len(replies) != 1 {
		t.Fatalf("%d reply requests, want one: the page has the whole answer or none", len(replies))
	}
	q := replies[0]
	wantForm := url.Values{"turn": {"1"}, "text": {"Written to today."}, "tools": {"- `capture` → [/days/2031-05-20/capture](/days/2031-05-20/capture)"}}
	if q.Path != "/days/2031-05-20/reply" || q.Source != "agent:lifelog" || !maps.EqualFunc(q.Form, wantForm, slices.Equal) {
		t.Errorf("reply request %+v, want the path /days/2031-05-20/reply, the source agent:lifelog and the form %v", q, wantForm)
	}
	w := r.api.writes()
	if len(w) != 1 || w[0].Path != "/days/2031-05-20/capture" || w[0].Source != "agent:lifelog" || !strings.Contains(w[0].Body, "text=Walked+by+the+river.") {
		t.Errorf("writes %+v", w)
	}
	reqs := r.mod.requests()
	if len(reqs) != 2 || reqs[0].Model != "test-model" {
		t.Fatalf("model requests %+v", reqs)
	}
	if sys := reqs[0].Messages[0]; sys.Role != "system" || !strings.Contains(sys.Content, "Today is 2031-05-20") || !strings.Contains(sys.Content, "Conversation 2031-05-20") {
		t.Errorf("system message %+v", sys)
	}
	// The model sees the page as the owner left it: the owner's turn is the last paragraph.
	if got := userOf(reqs[0]); got != "[[2031-05-20]]\n\n**Owner** (09:30): I walked by the river" || len(reqs[0].Messages) != 2 {
		t.Errorf("user message %q", got)
	}
	if last := reqs[1].Messages[len(reqs[1].Messages)-1]; last.Role != "tool" || last.ToolCallID != "call1" {
		t.Errorf("tool message %+v", last)
	}
}

func TestTheSettingsAreReadAtEveryTurnWithTheOwnersTokenAndNoSourceAndEveryOtherRequestHasTheSource(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.api.say("one")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1)
	r.api.setModel(r.api.modelURL, "other-model", "synthetic-key")
	r.api.say("two")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 2)
	reqs := r.mod.requests()
	if len(reqs) != 2 || reqs[0].Model != "test-model" || reqs[1].Model != "other-model" {
		t.Errorf("model requests %+v, want the change between two turns to be seen", reqs)
	}
	reads, writes := 0, 0
	for _, q := range r.api.requests() {
		if q.Auth != "Bearer "+token {
			t.Errorf("request %+v, want the token", q)
		}
		if q.Path == "/settings" {
			reads++
			if q.Method != "GET" || q.Source != "" {
				t.Errorf("settings request %+v, want a GET with no source", q)
			}
			continue
		}
		if q.Source != "agent:lifelog" {
			t.Errorf("request %+v, want the source agent:lifelog", q)
		}
		if q.Method != "GET" {
			writes++
		}
	}
	if reads != 2 || writes != 2 {
		t.Errorf("%d settings reads and %d writes, want 2 and 2 (one read and one save for each turn)", reads, writes)
	}
}

func TestATurnWithNoModelGivesOneLineOnThePageAndTheAgentGoesOn(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text("answer") })
	url := r.api.modelURL
	r.api.setModel("", "", "")
	r.api.say("hello")
	r.poll(t)
	const want = "No model is set: open Settings."
	if body := r.api.waitReplies(t, todayTitle, 1); !strings.HasSuffix(body, "**lifelog** (09:30): "+want) {
		t.Errorf("page body %q", body)
	}
	if n := len(r.mod.requests()); n != 0 {
		t.Errorf("%d model requests, want none", n)
	}
	r.api.setModel(url, "test-model", "")
	r.api.say("again")
	r.poll(t)
	if body := r.api.waitReplies(t, todayTitle, 2); !strings.HasSuffix(body, "**lifelog** (09:30): answer") {
		t.Errorf("page body after the model was set: %q", body)
	}
}

func TestAnOwnerActionIsNeverATool(t *testing.T) {
	list, err := toolsOf(catalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.Name == "snapshot" || x.Name == "say" || x.Name == "settings" {
			t.Errorf("the owner's action %s is a tool", x.Name)
		}
		if x.Name == "reply" {
			t.Error("reply is a tool: the agent writes the turn, and a model that called it would write a second one")
		}
	}
}

func TestAToolResultOverTheBoundIsCutWithANote(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("open_day", `{"day":"big"}`)
		}
		return text("read")
	})
	r.api.say("open it")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1)
	reqs := r.mod.requests()
	got := reqs[1].Messages[len(reqs[1].Messages)-1].Content
	if !strings.Contains(got, "[cut: the result has") || len(got) > maxResult+100 {
		t.Errorf("tool result has %d bytes and no note", len(got))
	}
}

func TestAnErrorOfTheAPIGoesToTheModelAsItsErrorEntityAndToThePageAsItsCode(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("get", `{"href":"/days/gone"}`)
		}
		return text("no such day")
	})
	r.api.say("x")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 1)
	reqs := r.mod.requests()
	if got := reqs[1].Messages[len(reqs[1].Messages)-1].Content; !strings.Contains(got, `"code":"not_found"`) || !strings.Contains(got, `"status":404`) {
		t.Errorf("tool result %s", got)
	}
	if !strings.Contains(body, "- `get` failed: not_found") {
		t.Errorf("page body %q", body)
	}
}

func TestABadToolCallIsAnErrorForTheModelNotACrash(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		switch n {
		case 1:
			return toolCall("nope", `{}`)
		case 2:
			return toolCall("capture", `{"day":`)
		}
		return text("done")
	})
	r.api.say("x")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 1)
	if !strings.HasSuffix(body, "**lifelog** (09:30): done") {
		t.Errorf("page body %q", body)
	}
	reqs := r.mod.requests()
	if got := reqs[1].Messages[len(reqs[1].Messages)-1].Content; !strings.Contains(got, `no tool "nope"`) {
		t.Errorf("unknown tool result %q", got)
	}
	if got := reqs[2].Messages[len(reqs[2].Messages)-1].Content; !strings.HasPrefix(got, "error:") {
		t.Errorf("bad arguments result %q", got)
	}
	if len(r.api.writes()) != 0 {
		t.Error("a bad call wrote")
	}
}

func TestTheStepBoundEndsATurn(t *testing.T) {
	r := start(t, func(int, chatReq) (int, string) { return toolCall("open_day", `{"day":"2031-05-20"}`) })
	r.api.say("loop")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 1)
	if n := len(r.mod.requests()); n != maxSteps {
		t.Errorf("%d model steps, want %d", n, maxSteps)
	}
	if strings.Count(body, "- `open_day`") != maxSteps || !strings.HasSuffix(body, "I stopped after 16 steps.") {
		t.Errorf("page body %q", body)
	}
}

func TestTheStepBoundGivesTheLastTextOfTheModel(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		return 200, `{"choices":[{"message":{"content":"working on it","tool_calls":[{"id":"c","type":"function","function":{"name":"open_day","arguments":"{}"}}]}}]}`
	})
	r.api.say("loop")
	r.poll(t)
	if body := r.api.waitReplies(t, todayTitle, 1); !strings.HasSuffix(body, "**lifelog** (09:30): working on it") {
		t.Errorf("page body %q", body)
	}
}

func TestAModelErrorGivesTheFailedReplyOnThePage(t *testing.T) {
	r := start(t, func(int, chatReq) (int, string) { return 503, "overloaded\nmore" })
	r.api.say("hi")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 1)
	_, got, _ := strings.Cut(body, "**lifelog** (09:30): ")
	if !strings.HasPrefix(got, "The model failed: ") || !strings.Contains(got, "503") || !strings.Contains(got, "overloaded") || strings.Contains(got, "more") || !strings.HasSuffix(got, ".") {
		t.Errorf("reply %q", got)
	}
}

func TestTwoTurnsBeforeALookAreAnsweredByOneReplyThatSeesBoth(t *testing.T) {
	api := &fakeAPI{}
	api.say("first")
	api.say("second")
	r := startOn(t, func(n int, req chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, fixedNow)
	body := api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): first\n\n**Owner** (09:30): second\n\n**lifelog** (09:30): answer 1"
	if body != want || api.body(todayTitle) != want {
		t.Errorf("page body %q, want %q", api.body(todayTitle), want)
	}
	if reqs := r.mod.requests(); len(reqs) != 1 || !strings.HasSuffix(userOf(reqs[0]), "second") || !strings.Contains(userOf(reqs[0]), "first") {
		t.Errorf("model requests %d: one run reads both turns", len(reqs))
	}
	if got := turnsReplied(api); !slices.Equal(got, []string{"2"}) {
		t.Errorf("the reply answers the turns %v, want 2: the last owner turn that the model read", got)
	}
}

func TestTheSecondTurnSeesTheFirstReplyOnThePage(t *testing.T) {
	r := start(t, func(n int, req chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.api.say("first")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1)
	r.api.say("second")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 2)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): first\n\n**lifelog** (09:30): answer 1\n\n**Owner** (09:30): second"
	if got := userOf(r.mod.requests()[1]); got != want {
		t.Errorf("second user message\n%q\nwant\n%q", got, want)
	}
}

func TestATurnIsAnsweredOnce(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.api.say("hello")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	r.settle(t)
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1", n)
	}
	if body := r.api.body(todayTitle); strings.Count(body, "**lifelog** (") != 1 {
		t.Errorf("page body %q", body)
	}
}

// TestTextThatAnEditAddsIsNeverATurn: only the action say writes a turn of the owner. Text that a save-body adds
// after a reply is part of that reply, and an edit of old words changes no turn.
func TestTextThatAnEditAddsIsNeverATurn(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text("answer") })
	r.api.say("hi")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1)
	r.api.edit(todayTitle, r.api.body(todayTitle)+"\n\nFrom the web: what did I do?")
	r.settle(t)
	r.api.edit(todayTitle, strings.Replace(r.api.body(todayTitle), "**Owner** (09:30): hi", "**Owner** (09:30): hi there", 1))
	r.settle(t)
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1: an edit is not a turn", n)
	}
	if body := r.api.body(todayTitle); strings.Count(body, "**lifelog** (") != 1 || !strings.HasSuffix(body, "From the web: what did I do?") {
		t.Errorf("page body %q", body)
	}
}

func TestATextThatAnEditAddsToAWaitingTurnIsAnsweredWithThatTurn(t *testing.T) {
	api := &fakeAPI{}
	api.say("hi")
	api.edit(todayTitle, api.body(todayTitle)+"\n\nI mean the walk.")
	r := startOn(t, func(n int, _ chatReq) (int, string) { return text("answer") }, api, fixedNow)
	api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	if reqs := r.mod.requests(); len(reqs) != 1 || !strings.HasSuffix(userOf(reqs[0]), "hi\n\nI mean the walk.") {
		t.Errorf("model requests %+v, want one that reads the turn and its added text", reqs)
	}
}

func TestAPageWithAWaitingTurnIsAnsweredAtTheStartAndAnAnsweredPageIsNot(t *testing.T) {
	api := &fakeAPI{}
	api.add(todayTitle, "[[2031-05-20]]\n\n**Owner** (08:15): said in the browser")
	r := startOn(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, fixedNow)
	api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1", n)
	}
	if body := api.body(todayTitle); !strings.HasSuffix(body, "**lifelog** (09:30): answer 1") {
		t.Errorf("page body %q", body)
	}
	// The same page, now answered, with a new agent: a restart answers nothing.
	r2 := startOn(t, func(n int, _ chatReq) (int, string) { return text("again") }, api, fixedNow)
	r2.settle(t)
	if n := len(r2.mod.requests()); n != 0 {
		t.Errorf("%d model requests after the restart, want none", n)
	}
	api.say("two")
	r2.poll(t)
	body := api.waitReplies(t, todayTitle, 2)
	if got := userOf(r2.mod.requests()[0]); !strings.Contains(got, "**lifelog** (09:30): answer 1") || !strings.HasSuffix(got, "**Owner** (09:30): two") {
		t.Errorf("the restarted agent lost the thread: %q", got)
	}
	if strings.Count(body, "**lifelog** (") != 2 {
		t.Errorf("page body %q", body)
	}
}

// TestABodyThatABrowserSavedWithCRLFIsReadWithLFAndTheReplyGoesAfterIt: a browser submits a textarea with CRLF line ends,
// and the API keeps the body as it was sent. The model reads LF, and the core writes the page with LF.
func TestABodyThatABrowserSavedWithCRLFIsReadWithLFAndTheReplyGoesAfterIt(t *testing.T) {
	api := &fakeAPI{}
	api.add(todayTitle, "[[2031-05-20]]\r\n\r\n**Owner** (08:15): said in the browser\r\n\r\nand more")
	r := startOn(t, func(n int, _ chatReq) (int, string) { return text("answer") }, api, fixedNow)
	body := api.waitReplies(t, todayTitle, 1)
	want := "[[2031-05-20]]\n\n**Owner** (08:15): said in the browser\n\nand more\n\n**lifelog** (09:30): answer"
	if body != want {
		t.Errorf("page body %q, want %q", body, want)
	}
	if got := userOf(r.mod.requests()[0]); strings.Contains(got, "\r") {
		t.Errorf("the model read a CR: %q", got)
	}
}

// TestTextThatTheOwnerAddsWhileTheModelRunsStaysAboveTheReply: an edit is part of the turn before it, so the reply goes
// after it. The agent sends no body, so it cannot lose the edit.
func TestTextThatTheOwnerAddsWhileTheModelRunsStaysAboveTheReply(t *testing.T) {
	api := &fakeAPI{}
	api.say("hello")
	startOn(t, func(n int, _ chatReq) (int, string) {
		api.edit(todayTitle, api.body(todayTitle)+"\n\nAdded on the web.")
		return text("noted")
	}, api, fixedNow)
	body := api.waitReplies(t, todayTitle, 1)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): hello\n\nAdded on the web.\n\n**lifelog** (09:30): noted"
	if body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
}

// TestTheOwnerSaysMoreWhileTheModelRunsAndTheReplyGoesBeforeThatTurn: the new turn must stay the last turn of the
// page, so that the next look answers it. A reply after it would hide it.
func TestTheOwnerSaysMoreWhileTheModelRunsAndTheReplyGoesBeforeThatTurn(t *testing.T) {
	api := &fakeAPI{}
	api.say("first")
	r := startOn(t, func(n int, req chatReq) (int, string) {
		if n == 1 {
			api.say("second")
		}
		return text(fmt.Sprintf("answer %d", n))
	}, api, fixedNow)
	mid := "[[2031-05-20]]\n\n**Owner** (09:30): first\n\n**lifelog** (09:30): answer 1\n\n**Owner** (09:30): second"
	api.waitBody(t, todayTitle, func(body string) bool { return body == mid })
	r.poll(t)
	body := api.waitReplies(t, todayTitle, 2)
	if want := mid + "\n\n**lifelog** (09:30): answer 2"; body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	reqs := r.mod.requests()
	if len(reqs) != 2 || !strings.Contains(userOf(reqs[1]), "answer 1") || !strings.HasSuffix(userOf(reqs[1]), "second") {
		t.Errorf("model requests %d: the second run reads the page with both turns", len(reqs))
	}
	if got := turnsReplied(api); !slices.Equal(got, []string{"1", "2"}) {
		t.Errorf("the replies answer the turns %v, want 1 and then 2", got)
	}
}

// TestAnEditOfAnEarlierTurnAndANewTurnWhileTheModelRunsAreBothAnswered: the core finds the place of the reply by the
// number of the turn, not by the bytes of the page that the model read. So an edit of old words does not move the reply
// behind the new turn, and the new turn waits for its own reply.
func TestAnEditOfAnEarlierTurnAndANewTurnWhileTheModelRunsAreBothAnswered(t *testing.T) {
	api := &fakeAPI{}
	api.say("frist")
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			api.edit(todayTitle, strings.Replace(api.body(todayTitle), "frist", "first", 1))
			api.say("second")
		}
		return text(fmt.Sprintf("answer %d", n))
	}, api, fixedNow)
	api.waitBody(t, todayTitle, func(body string) bool { return strings.Contains(body, "answer 1") })
	r.poll(t)
	body := api.waitReplies(t, todayTitle, 2)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): first\n\n**lifelog** (09:30): answer 1\n\n**Owner** (09:30): second\n\n**lifelog** (09:30): answer 2"
	if body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	if n := len(r.mod.requests()); n != 2 {
		t.Errorf("%d model requests, want 2: one for each turn", n)
	}
}

// TestAReplyToATurnThatHasAReplyAlreadyEndsTheTurnWithOneLogLine: the core refuses a second reply to a turn (409) and
// writes nothing. The agent does not run the model again for that turn. The refusal is not an error of the agent.
func TestAReplyToATurnThatHasAReplyAlreadyEndsTheTurnWithOneLogLine(t *testing.T) {
	api := &fakeAPI{}
	api.say("hello")
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		api.otherReplies(todayTitle, "the answer of another writer")
		return text(fmt.Sprintf("answer %d", n))
	}, api, fixedNow)
	api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	r.settle(t)
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1: the turn is not run again", n)
	}
	if want := "[[2031-05-20]]\n\n**Owner** (09:30): hello\n\n**lifelog** (09:30): the answer of another writer"; api.body(todayTitle) != want {
		t.Errorf("page body %q, want %q: the refused reply wrote nothing", api.body(todayTitle), want)
	}
	if got := turnsReplied(api); !slices.Equal(got, []string{"1"}) {
		t.Errorf("reply requests for the turns %v, want one for the turn 1", got)
	}
	if lines := r.log.lines("reply was refused"); len(lines) != 1 {
		t.Errorf("%d log lines about the refusal %q, want 1", len(lines), lines)
	}
}

// TestAModelReplyThatHoldsAnOwnerMarkerLineGivesOneModelRequest: a model that copies the format of the page writes a line
// that reads as a turn of the owner. The core escapes it, so the page ends with the turn of lifelog, and the agent does
// not answer its own reply.
func TestAModelReplyThatHoldsAnOwnerMarkerLineGivesOneModelRequest(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		return text(fmt.Sprintf("Sure %d.\n\n**Owner** (09:31): and then?", n))
	})
	r.api.say("hello")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	r.settle(t)
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests for one say, want 1; page %q", n, r.api.body(todayTitle))
	}
	if want := `\*\*Owner\*\* (09:31): and then?`; !strings.HasSuffix(body, want) {
		t.Errorf("page body %q, want it to end with the escaped line %q", body, want)
	}
}

// TestAReplyThatCannotBeWrittenIsNotRunAgainUntilThePageChanges: a new run of the model would repeat the tool calls of
// the first run.
func TestAReplyThatCannotBeWrittenIsNotRunAgainUntilThePageChanges(t *testing.T) {
	api := &fakeAPI{replyFails: true}
	api.say("hello")
	r := startOn(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, fixedNow)
	r.settle(t)
	r.settle(t)
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1: the same turn is not tried again", n)
	}
	if body := api.body(todayTitle); strings.Contains(body, "**lifelog**") {
		t.Errorf("a reply that could not be saved is on the page: %q", body)
	}
	api.setReplyFails(false)
	api.say("hello again")
	r.poll(t)
	body := api.waitReplies(t, todayTitle, 1)
	if n := len(r.mod.requests()); n != 2 || !strings.HasSuffix(body, "answer 2") {
		t.Errorf("%d model requests, page %q: a new turn is answered", n, body)
	}
}

// TestATurnSaidBeforeMidnightIsAnsweredOnItsOwnPage: the first look of a day reads the day before. The reply is added
// to the page of that day, and the page of today is not made.
func TestATurnSaidBeforeMidnightIsAnsweredOnItsOwnPageAtTheFirstLookOfTheNewDay(t *testing.T) {
	api := &fakeAPI{}
	api.add(todayTitle, "[[2031-05-20]]\n\n**Owner** (23:59): good night")
	api.setToday("2031-05-21")
	after := time.Date(2031, 5, 21, 0, 0, 30, 0, time.UTC)
	r := startOn(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, after)
	body := api.waitReplies(t, todayTitle, 1)
	if !strings.HasSuffix(body, "**Owner** (23:59): good night\n\n**lifelog** (00:00): answer 1") {
		t.Errorf("yesterday's page %q", body)
	}
	if q := api.replies(); len(q) != 1 || q[0].Path != "/days/2031-05-20/reply" || q[0].Form.Get("turn") != "1" {
		t.Errorf("reply requests %+v, want one to the day of the page, for its turn 1", q)
	}
	sys := r.mod.requests()[0].Messages[0].Content
	if !strings.Contains(sys, "Today is 2031-05-21") || !strings.Contains(sys, "Conversation 2031-05-20") {
		t.Errorf("system message %q: it names today and the day of the page", sys)
	}
	r.settle(t)
	if api.find("Conversation 2031-05-21") != nil {
		t.Error("the page of the new day was made by the agent")
	}
	api.say("good morning")
	r.poll(t)
	api.waitReplies(t, "Conversation 2031-05-21", 1)
	if n := len(r.mod.requests()); n != 2 {
		t.Errorf("%d model requests, want 2", n)
	}
	if body := api.body(todayTitle); strings.Count(body, "**lifelog** (") != 1 {
		t.Errorf("yesterday's page was written again: %q", body)
	}
}

func TestTheDayBeforeIsReadOnTheFirstLookAndOnANewDayOnly(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text("answer") })
	r.settle(t)
	r.settle(t)
	if got := pathsOf(r.api, "GET"); strings.Count(strings.Join(got, " "), "/days/2031-05-19/conversation") != 1 {
		t.Errorf("reads %v, want one of the day before the first day", got)
	}
	r.api.setToday("2031-05-21")
	r.setClock(time.Date(2031, 5, 21, 8, 0, 0, 0, time.UTC))
	r.settle(t)
	r.settle(t)
	if got := pathsOf(r.api, "GET"); strings.Count(strings.Join(got, " "), "/days/2031-05-20/conversation") != 1 {
		t.Errorf("reads %v, want one of the day before the new day", got)
	}
}

func TestTheConversationSentToTheModelKeepsItsEnd(t *testing.T) {
	long := strings.Repeat("é", maxBody) // two bytes each
	got := tail(long + "END")
	if !strings.HasSuffix(got, "END") || len(got) > maxBody+100 || !strings.HasPrefix(got, "[the first ") {
		t.Errorf("tail has %d bytes, prefix %.30q", len(got), got)
	}
	if tail("short") != "short" {
		t.Error("a short conversation was changed")
	}
}

func TestACancelledContextEndsTheTurnAndWritesNoReply(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	api := &fakeAPI{}
	api.say("hang") // the page is there before the agent starts: its first look runs the turn
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		started <- struct{}{}
		<-release
		return text("late")
	}, api, fixedNow)
	t.Cleanup(func() { close(release) }) // runs before the server closes, so its handler can end
	<-started
	r.cancel()
	select {
	case err := <-r.done:
		r.done <- err
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(wait):
		t.Fatal("Run did not return after the cancel")
	}
	if body := r.api.body(todayTitle); strings.Contains(body, "**lifelog**") {
		t.Errorf("a cancelled turn wrote a reply: %q", body)
	}
}

func TestTheModelCannotReadTheKeyThroughGet(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("get", `{"href":"/settings"}`)
		}
		return text("I cannot read the settings.")
	})
	r.api.setModel(r.api.modelURL, "test-model", "synthetic-key-ABC")
	r.api.say("what is my key?")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 1)
	reqs := r.mod.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d model requests, want 2", len(reqs))
	}
	result := reqs[1].Messages[len(reqs[1].Messages)-1]
	if result.Role != "tool" || !strings.Contains(result.Content, "forbidden") {
		t.Errorf("tool result %+v, want the error entity of a 403", result)
	}
	if !strings.Contains(body, "- `get` failed: forbidden") {
		t.Errorf("page body %q", body)
	}
	for name, text := range map[string]string{"the tool result": result.Content, "the page": body, "the model request": userOf(reqs[1])} {
		if strings.Contains(text, "synthetic-key-ABC") {
			t.Errorf("the key is in %s: %q", name, text)
		}
	}
	// The model server still got the key as its credential: that is the one use of it.
	for _, q := range r.api.requests() {
		if q.Path == "/settings" && q.Source == "agent:lifelog" && q.Method == "GET" {
			return
		}
	}
	t.Error("the get of the model carried no agent source")
}

func TestAnActionAddedToTheCatalogBetweenTwoTurnsIsAToolOfTheSecondTurn(t *testing.T) {
	r := start(t, func(n int, req chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.api.say("first")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1) // the first turn is done: the catalog change comes after it
	r.api.addAction(siren.Action{Name: "app-note", Method: "POST", Href: "/with/app/note"})
	r.api.say("second")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 2)
	reqs := r.mod.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d model requests, want 2", len(reqs))
	}
	if slices.Contains(toolNames(reqs[0]), "app_note") {
		t.Error("the first turn has the tool of the later action")
	}
	if !slices.Contains(toolNames(reqs[1]), "app_note") {
		t.Errorf("the second turn has the tools %v, want app_note too", toolNames(reqs[1]))
	}
}

func TestACatalogReadThatFailsAtATurnKeepsTheToolsOfTheLastReadAndTheTurnGoesOn(t *testing.T) {
	r := start(t, func(n int, req chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.api.say("first")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1)
	r.api.failCatalog(true)
	r.api.say("second")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 2)
	if !strings.HasSuffix(body, "answer 2") {
		t.Errorf("page body %q, want the answer of the model", body)
	}
	reqs := r.mod.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d model requests, want 2", len(reqs))
	}
	if a, b := toolNames(reqs[0]), toolNames(reqs[1]); len(a) == 0 || !slices.Equal(a, b) {
		t.Errorf("tools of turn 1 %v, tools of turn 2 %v: want the same, not empty", a, b)
	}
}

// runOnce runs an agent on an API until Run returns on its own, and gives the error.
func runOnce(t *testing.T, api *fakeAPI) error {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	a := &Agent{Client: client.Remote(srv.URL, siren.AgentPrefix+Source, token), AsOwner: client.Remote(srv.URL, "", token), Log: log.New(io.Discard, "", 0)}
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	return a.Run(ctx)
}

// TestACatalogWithoutSayOrReplyIsRefusedAsAnActionTheOwnerMustAdd: the program exits 78 for it (the owner acts
// first). A catalog that cannot be read is another error: the program exits 1, and serve starts it again.
func TestACatalogWithoutSayOrReplyIsRefusedButACatalogThatFailsIsNot(t *testing.T) {
	for _, name := range []string{"say", "reply"} {
		api := &fakeAPI{}
		api.dropAction(name)
		if err := runOnce(t, api); !errors.Is(err, ErrNoAction) || !strings.Contains(err.Error(), name) {
			t.Errorf("a catalog with no %s: %v, want ErrNoAction naming it", name, err)
		}
	}
	api := &fakeAPI{}
	api.failCatalog(true)
	if err := runOnce(t, api); err == nil || errors.Is(err, ErrNoAction) {
		t.Errorf("a catalog that fails: %v, want an error that is not ErrNoAction", err)
	}
}
