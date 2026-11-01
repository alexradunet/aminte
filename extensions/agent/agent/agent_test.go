package agent

import (
	"context"
	"encoding/json"
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
	keys := r.api.keysOf("owner", today)
	if len(keys) != 1 || len(keys[0]) != 16 {
		t.Fatalf("the keys of the owner turns are %q", keys)
	}
	wantForm := url.Values{"turn": {keys[0]}, "text": {"Written to today."}, "tools": {"- `capture` → [/days/2031-05-20/capture](/days/2031-05-20/capture)"}}
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
	keys := api.keysOf("owner", today)
	if got := turnsReplied(api); len(keys) != 2 || !slices.Equal(got, []string{keys[1]}) {
		t.Errorf("the reply answers the turns %v of the owner turns %v, want the key of the second: the last owner turn that the model read", got, keys)
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

// TestTextThatAnEditAddsAfterAReplyIsPartOfThatReply: text that a save-body adds after a reply, with no marker line, is
// part of that reply, and an edit of old words changes no turn. The agent answers none of it.
func TestTextThatAnEditAddsAfterAReplyIsPartOfThatReply(t *testing.T) {
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

// TestAMarkerLineThatTheOwnerTypesInAnEditIsATurnOfTheOwner: the core escapes a marker line in the words of say and
// reply only. The owner may type a marker line in an edit of the page, and the core reads it as a turn of the owner.
// When it is the last turn, the agent answers it, as it answers any turn that waits.
func TestAMarkerLineThatTheOwnerTypesInAnEditIsATurnOfTheOwner(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.api.say("hi")
	r.poll(t)
	r.api.waitReplies(t, todayTitle, 1)
	r.api.edit(todayTitle, r.api.body(todayTitle)+"\n\n**Owner** (10:00): and one more thing")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 2)
	r.settle(t)
	if want := "[[2031-05-20]]\n\n**Owner** (09:30): hi\n\n**lifelog** (09:30): answer 1\n\n**Owner** (10:00): and one more thing\n\n**lifelog** (09:30): answer 2"; body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	if n := len(r.mod.requests()); n != 2 {
		t.Errorf("%d model requests, want 2: one for each turn of the owner", n)
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

// TestTextThatTheOwnerAddsToTheAnsweredTurnWhileTheModelRunsIsAnsweredByASecondRun: text that an edit adds below a turn
// is part of that turn, so the key of the turn changes. The core refuses the reply to the old key (409), and the agent
// reads the page again at the next look. The second run reads the added text, and the reply goes after it. The agent
// sends no body, so it cannot lose the edit.
func TestTextThatTheOwnerAddsToTheAnsweredTurnWhileTheModelRunsIsAnsweredByASecondRun(t *testing.T) {
	api := &fakeAPI{}
	api.say("hello")
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			api.edit(todayTitle, api.body(todayTitle)+"\n\nAdded on the web.")
		}
		return text(fmt.Sprintf("noted %d", n))
	}, api, fixedNow)
	api.waitBody(t, todayTitle, func(body string) bool { return strings.Contains(body, "Added on the web.") })
	r.poll(t) // the first look is done: its reply was refused
	body := api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): hello\n\nAdded on the web.\n\n**lifelog** (09:30): noted 2"
	if body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	if reqs := r.mod.requests(); len(reqs) != 2 || !strings.HasSuffix(userOf(reqs[1]), "Added on the web.") {
		t.Errorf("%d model requests, want 2: the second run reads the added text", len(reqs))
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
	if keys, got := api.keysOf("owner", today), turnsReplied(api); len(keys) != 2 || !slices.Equal(got, keys) {
		t.Errorf("the replies answer the turns %v, want the keys %v of the two owner turns, the first and then the second", got, keys)
	}
}

// TestAnEditOfAnEarlierTurnAndANewTurnWhileTheModelRunsAreBothAnswered: the core finds the place of the reply by the key
// of the answered turn, not by the bytes of the page that the model read. An edit of an old turn does not move the key
// of the answered turn, so the reply does not go behind the new turn, and the new turn waits for its own reply.
func TestAnEditOfAnEarlierTurnAndANewTurnWhileTheModelRunsAreBothAnswered(t *testing.T) {
	api := &fakeAPI{}
	api.add(todayTitle, "[[2031-05-20]]\n\n**Owner** (09:30): frist\n\n**lifelog** (09:30): answer 0\n\n**Owner** (09:30): second")
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			api.edit(todayTitle, strings.Replace(api.body(todayTitle), "frist", "first", 1))
			api.say("third")
		}
		return text(fmt.Sprintf("answer %d", n))
	}, api, fixedNow)
	api.waitBody(t, todayTitle, func(body string) bool { return strings.Contains(body, "answer 1") })
	r.poll(t)
	body := api.waitReplies(t, todayTitle, 3)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): first\n\n**lifelog** (09:30): answer 0\n\n**Owner** (09:30): second\n\n**lifelog** (09:30): answer 1\n\n**Owner** (09:30): third\n\n**lifelog** (09:30): answer 2"
	if body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	if n := len(r.mod.requests()); n != 2 {
		t.Errorf("%d model requests, want 2: one for each turn", n)
	}
}

// TestAnEditThatRemovesAnEarlierExchangeWhileTheModelRunsDoesNotMoveTheReply: the agent names the turn by its key and
// not by its count. The owner removes the first exchange and says a new turn while the model works on c. The count of
// the owner turns then names the new turn; the key still names c, so the reply goes right after c, and the new turn
// stays the last turn of the page and is answered by the next run.
func TestAnEditThatRemovesAnEarlierExchangeWhileTheModelRunsDoesNotMoveTheReply(t *testing.T) {
	api := &fakeAPI{}
	api.add(todayTitle, "[[2031-05-20]]\n\n**Owner** (09:30): a\n\n**lifelog** (09:30): ra\n\n**Owner** (09:30): b\n\n**lifelog** (09:30): rb\n\n**Owner** (09:30): c")
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			api.edit(todayTitle, "[[2031-05-20]]\n\n**Owner** (09:30): b\n\n**lifelog** (09:30): rb\n\n**Owner** (09:30): c")
			api.say("d")
		}
		return text(fmt.Sprintf("answer %d", n))
	}, api, fixedNow)
	api.waitBody(t, todayTitle, func(body string) bool { return strings.Contains(body, "answer 1") })
	r.poll(t)
	body := api.waitReplies(t, todayTitle, 3)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): b\n\n**lifelog** (09:30): rb\n\n**Owner** (09:30): c\n\n**lifelog** (09:30): answer 1\n\n**Owner** (09:30): d\n\n**lifelog** (09:30): answer 2"
	if body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	if n := len(r.mod.requests()); n != 2 {
		t.Errorf("%d model requests, want 2: one for c and one for d", n)
	}
}

// TestAnEditOfTheAnsweredTurnWhileTheModelRunsMakesTheReplyAConflictAndTheChangedTurnIsAnsweredAfterReadingAgain: the
// key of a turn changes when the owner edits its words. The core refuses the reply to the old key (409): the page
// changed. The agent reads the page again at the next look and runs the model on the turn as it is now.
func TestAnEditOfTheAnsweredTurnWhileTheModelRunsMakesTheReplyAConflictAndTheChangedTurnIsAnsweredAfterReadingAgain(t *testing.T) {
	api := &fakeAPI{}
	api.say("frist")
	oldKey := api.keysOf("owner", today)[0]
	r := startOn(t, func(n int, req chatReq) (int, string) {
		if n == 1 {
			api.edit(todayTitle, strings.Replace(api.body(todayTitle), "frist", "first", 1))
		}
		return text(fmt.Sprintf("answer %d", n))
	}, api, fixedNow)
	api.waitBody(t, todayTitle, func(body string) bool { return strings.Contains(body, "first") }) // the edit is made in the run
	r.poll(t)                                                                                      // the first look is done: its reply was refused
	body := api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	if want := "[[2031-05-20]]\n\n**Owner** (09:30): first\n\n**lifelog** (09:30): answer 2"; body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	reqs := r.mod.requests()
	if len(reqs) != 2 || !strings.HasSuffix(userOf(reqs[1]), "**Owner** (09:30): first") {
		t.Errorf("%d model requests, want 2: the second run reads the turn as the owner edited it", len(reqs))
	}
	newKey := api.keysOf("owner", today)[0]
	if got := turnsReplied(api); newKey == oldKey || !slices.Equal(got, []string{oldKey, newKey}) {
		t.Errorf("the replies answer the keys %v, want the old key %s (refused) and then the new key %s", got, oldKey, newKey)
	}
	if lines := r.log.lines("reply was refused"); len(lines) != 1 {
		t.Errorf("%d log lines about the refusal %q, want 1", len(lines), lines)
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
	if keys, got := api.keysOf("owner", today), turnsReplied(api); len(keys) != 1 || !slices.Equal(got, keys) {
		t.Errorf("reply requests for the turns %v, want one for the key %v of the turn", got, keys)
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
	if n := len(api.replies()); n != replyAttempts {
		t.Errorf("%d reply requests, want %d: the same reply is sent again a few times, and then no more", n, replyAttempts)
	}
	if lines := r.log.lines("reply was refused or failed"); len(lines) != 1 {
		t.Errorf("%d log lines about the failure %q, want 1", len(lines), lines)
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
	keys := api.keysOf("owner", "2031-05-20")
	if q := api.replies(); len(q) != 1 || len(keys) != 1 || q[0].Path != "/days/2031-05-20/reply" || q[0].Form.Get("turn") != keys[0] {
		t.Errorf("reply requests %+v, want one to the day of the page, for the key %v of its turn", q, keys)
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

// TestAnEditOfATurnOfYesterdayWhileTheModelRunsIsAnsweredAtALaterLook: the reply to the old key is 409 (the page
// changed), as it is for today's page. The agent reads the page of the day before at each look while a turn waits on it,
// so the edited turn is answered at the next look and not only after a restart. When the turn is answered, the agent
// reads that page no more.
func TestAnEditOfATurnOfYesterdayWhileTheModelRunsIsAnsweredAtALaterLook(t *testing.T) {
	api := &fakeAPI{}
	api.add(todayTitle, "[[2031-05-20]]\n\n**Owner** (23:59): good nihgt")
	api.setToday("2031-05-21")
	after := time.Date(2031, 5, 21, 0, 0, 30, 0, time.UTC)
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			api.edit(todayTitle, strings.Replace(api.body(todayTitle), "nihgt", "night", 1))
		}
		return text(fmt.Sprintf("answer %d", n))
	}, api, after)
	api.waitBody(t, todayTitle, func(body string) bool { return strings.Contains(body, "night") }) // the edit is made in the run
	r.poll(t)                                                                                      // the first look is done: its reply was refused
	body := api.waitReplies(t, todayTitle, 1)
	if want := "[[2031-05-20]]\n\n**Owner** (23:59): good night\n\n**lifelog** (00:00): answer 2"; body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	if n := len(r.mod.requests()); n != 2 {
		t.Errorf("%d model requests, want 2: the first run, and the run on the edited turn", n)
	}
	if lines := r.log.lines("reply was refused"); len(lines) != 1 {
		t.Errorf("%d log lines about the refusal %q, want 1", len(lines), lines)
	}
	r.settle(t) // the look that finds the page answered
	reads := strings.Count(strings.Join(pathsOf(api, "GET"), " "), "/days/2031-05-20/conversation")
	r.settle(t)
	if got := strings.Count(strings.Join(pathsOf(api, "GET"), " "), "/days/2031-05-20/conversation"); got != reads {
		t.Errorf("%d reads of the day before after %d: the page is answered, and it is not read again", got, reads)
	}
}

// TestATurnOfYesterdayThatTheCoreRefusedIsReadAgainButNotRunAgainAndDoesNotBlockToday: the agent reads the day before
// at each look while a turn waits on it. A reply that the core refused (403) is not run again until that page changes,
// and the refusal on the page of today does not make the agent run the model on the page of the day before again.
func TestATurnOfYesterdayThatTheCoreRefusedIsReadAgainButNotRunAgainAndDoesNotBlockToday(t *testing.T) {
	api := &fakeAPI{}
	api.add(todayTitle, "[[2031-05-20]]\n\n**Owner** (23:59): good night")
	api.setToday("2031-05-21")
	api.say("good morning")
	api.failReplies(403, 403) // the reply to yesterday, and then the reply to today
	after := time.Date(2031, 5, 21, 0, 0, 30, 0, time.UTC)
	r := startOn(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, after)
	r.settle(t)
	r.settle(t)
	r.settle(t)
	if n := len(r.mod.requests()); n != 2 {
		t.Errorf("%d model requests, want 2: one for each page, and no run again on a page that the core refused", n)
	}
	if n := len(api.replies()); n != 2 {
		t.Errorf("%d reply requests, want 2", n)
	}
	if lines := r.log.lines("answering a turn that waited"); len(lines) != 1 {
		t.Errorf("%d log lines about the page of the day before %q, want 1", len(lines), lines)
	}
	reads := strings.Count(strings.Join(pathsOf(api, "GET"), " "), "/days/2031-05-20/conversation")
	if reads < 3 {
		t.Errorf("%d reads of the day before, want at least 3: it is read at each look while a turn waits on it", reads)
	}
	api.edit(todayTitle, strings.Replace(api.body(todayTitle), "good night", "good night!", 1))
	r.poll(t)
	body := api.waitReplies(t, todayTitle, 1)
	if want := "[[2031-05-20]]\n\n**Owner** (23:59): good night!\n\n**lifelog** (00:00): answer 3"; body != want {
		t.Errorf("page body\n%q\nwant\n%q: the edit of the turn that waits makes the agent answer it", body, want)
	}
}

// TestTheDayBeforeIsReadOnTheFirstLookAndOnANewDayWhileNoTurnWaitsOnIt: a page that no turn waits on is looked at once.
func TestTheDayBeforeIsReadOnTheFirstLookAndOnANewDayWhileNoTurnWaitsOnIt(t *testing.T) {
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

// sameReplies reports whether every reply request has the form of the first one: the agent sends the same reply again.
func sameReplies(f *fakeAPI) bool {
	replies := f.replies()
	for _, q := range replies {
		if !maps.EqualFunc(q.Form, replies[0].Form, slices.Equal) {
			return false
		}
	}
	return len(replies) > 0
}

// TestAReplyThatFailsWithABusyCoreIsSentAgainAndTheModelRunsOnce: the core answers 503 when the file is busy, and it
// writes nothing. A second send of the same reply is safe, because the core refuses a second reply to one turn.
func TestAReplyThatFailsWithABusyCoreIsSentAgainAndTheModelRunsOnce(t *testing.T) {
	api := &fakeAPI{}
	api.failReplies(503, 502)
	api.say("hello")
	r := startOn(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("capture", `{"day":"2031-05-20","text":"x"}`)
		}
		return text("done")
	}, api, fixedNow)
	body := api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	if want := "[[2031-05-20]]\n\n**Owner** (09:30): hello\n\n- `capture` → [/days/2031-05-20/capture](/days/2031-05-20/capture)\n\n**lifelog** (09:30): done"; body != want {
		t.Errorf("page body\n%q\nwant\n%q: the tool line and the reply are on the page", body, want)
	}
	if n := len(r.mod.requests()); n != 2 { // the tool call and the text: one run
		t.Errorf("%d model requests, want 2: one run of the model", n)
	}
	if n := len(api.writes()); n != 1 {
		t.Errorf("%d tool writes, want 1: the tool call is not made twice", n)
	}
	if n := len(api.replies()); n != 3 || !sameReplies(api) {
		t.Errorf("%d reply requests (all the same: %v), want 3: two refused with 5xx and one written", n, sameReplies(api))
	}
	if lines := r.log.lines("reply was refused or failed"); len(lines) != 0 {
		t.Errorf("log lines %q, want none: the reply was written", lines)
	}
	if lines := r.log.lines("turn answered"); len(lines) != 1 {
		t.Errorf("%d log lines about the answer %q, want 1", len(lines), lines)
	}
}

// TestAReplyWhoseAnswerWasLostIsNotAnsweredTwice: the core writes the reply, and the connection ends before the agent
// reads the answer. The agent sends the reply again, and the core refuses it (409), because the turn has a reply: that
// means that the first send was written.
func TestAReplyWhoseAnswerWasLostIsNotAnsweredTwice(t *testing.T) {
	api := &fakeAPI{}
	api.loseNextAnswer()
	api.say("hello")
	r := startOn(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, fixedNow)
	body := api.waitReplies(t, todayTitle, 1)
	r.settle(t)
	r.settle(t)
	if want := "[[2031-05-20]]\n\n**Owner** (09:30): hello\n\n**lifelog** (09:30): answer 1"; body != want || api.body(todayTitle) != want {
		t.Errorf("page body %q, want %q", api.body(todayTitle), want)
	}
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1", n)
	}
	if n := len(api.replies()); n != 2 {
		t.Errorf("%d reply requests, want 2: the first was written, the second was refused", n)
	}
	if lines := r.log.lines("reply was refused or failed"); len(lines) != 0 {
		t.Errorf("log lines %q, want none: a 409 after a send means that the first send was written", lines)
	}
	if lines := r.log.lines("turn answered"); len(lines) != 1 {
		t.Errorf("%d log lines about the answer %q, want 1", len(lines), lines)
	}
}

// TestAConflictOnASecondSendIsAnAnswerOnlyWhenAnEarlierSendMayHaveBeenWritten: the owner edits the answered turn between
// two sends, so the second send gets 409 (no owner turn has the key). The core answers 503 with the code busy when it
// wrote nothing, so that 409 is a refusal: the reply is not on the page, and the log says so once. A 502 says nothing
// about the write. The agent cannot tell, and it counts the 409 as the sign of a write. In both cases the next look
// reads the page as the owner edited it, and the new run of the model answers it once.
func TestAConflictOnASecondSendIsAnAnswerOnlyWhenAnEarlierSendMayHaveBeenWritten(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		refused int // the log lines "reply was refused or failed"
	}{
		{"busy file (503): nothing was written", 503, 1},
		{"bad gateway (502): the write is not known", 502, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{}
			api.say("frist")
			api.failReplyThenEdit(tc.status, todayTitle, "frist", "first")
			r := startOn(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, fixedNow)
			api.waitReplyRequests(t, 2) // the failed send, and the send that the edit makes a conflict
			r.settle(t)
			r.settle(t)
			if want := "[[2031-05-20]]\n\n**Owner** (09:30): first\n\n**lifelog** (09:30): answer 2"; api.body(todayTitle) != want {
				t.Errorf("page body\n%q\nwant\n%q: one reply, to the turn as the owner edited it", api.body(todayTitle), want)
			}
			if n := len(r.mod.requests()); n != 2 {
				t.Errorf("%d model requests, want 2: the first run and the run on the edited turn", n)
			}
			if lines := r.log.lines("reply was refused or failed"); len(lines) != tc.refused {
				t.Errorf("%d log lines about a refusal %q, want %d", len(lines), lines, tc.refused)
			}
			// The log says "turn answered" for each reply that the agent counts as written. The reply of the first run is
			// never one of them when the core wrote nothing.
			if lines := r.log.lines("turn answered"); len(lines) != 2-tc.refused {
				t.Errorf("%d log lines about an answer %q, want %d", len(lines), lines, 2-tc.refused)
			}
		})
	}
}

// TestAReplyThatTheCoreRefusesIsNotSentAgain: a 4xx is the answer of the core about this reply. A second send gets the
// same answer.
func TestAReplyThatTheCoreRefusesIsNotSentAgain(t *testing.T) {
	for _, status := range []int{403, 404, 409, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			api := &fakeAPI{}
			api.failReplies(status)
			api.say("hello")
			r := startOn(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) }, api, fixedNow)
			r.settle(t)
			r.settle(t)
			if n := len(api.replies()); n != 1 {
				t.Errorf("%d reply requests, want 1", n)
			}
			if n := len(r.mod.requests()); n != 1 {
				t.Errorf("%d model requests, want 1", n)
			}
			if lines := r.log.lines("reply was refused or failed"); len(lines) != 1 {
				t.Errorf("%d log lines about the refusal %q, want 1", len(lines), lines)
			}
		})
	}
}

// TestACancelDuringTheWaitBetweenTwoSendsEndsTheAgentAtOnce: the wait between two sends ends with the context.
func TestACancelDuringTheWaitBetweenTwoSendsEndsTheAgentAtOnce(t *testing.T) {
	api := &fakeAPI{replyFails: true}
	api.say("hello")
	r := startWithBackoff(t, func(n int, _ chatReq) (int, string) { return text("answer") }, api, fixedNow, time.Hour)
	api.waitReplyRequests(t, 1)
	r.cancel()
	select {
	case err := <-r.done:
		r.done <- err
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(wait):
		t.Fatal("Run did not return: the wait between two sends does not end with the context")
	}
	if n := len(api.replies()); n != 1 {
		t.Errorf("%d reply requests, want 1", n)
	}
}

// TestAToolLineIsOneLine: a tool name from the model, a code from a connected app and an href can hold a control
// character. The core refuses a line of the field tools that does not start with a tool mark (422), and the reply is
// lost. So each part of a tool line is one line. A tool name from the model can also hold a backtick. It must not close
// the code span of the line: the rest of the name would read as the words of a call that went well, with an address.
func TestAToolLineIsOneLine(t *testing.T) {
	const forged = "capture` → [/pages/1](/pages/1) `" // the name of a tool that does not exist
	quoted, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	name := string(quoted[1 : len(quoted)-1]) // toolCall takes the JSON text of a name, without its quotes
	r := start(t, func(n int, _ chatReq) (int, string) {
		switch n {
		case 1:
			return toolCall(`no_such\nreally`, `{}`) // the JSON text of a name with a new line
		case 2:
			return toolCall(`get`, `{"href":"/days/badcode"}`)
		case 3:
			return toolCall(`get`, `{"href":"/days/badhref"}`)
		case 4:
			return toolCall(name, `{}`)
		}
		return text("Here is my answer.")
	})
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the log of the agent: %q", r.log.lines(""))
		}
	})
	r.api.say("hello")
	r.poll(t)
	body := r.api.waitReplies(t, todayTitle, 1)
	want := "[[2031-05-20]]\n\n**Owner** (09:30): hello\n\n" +
		"- `no_such really` failed: unknown tool\n" +
		"- `get` failed: bad code\n" +
		"- `get` → [/pages/1 2](/pages/1 2)\n" +
		"- `capture' → [/pages/1](/pages/1) '` failed: unknown tool\n\n" +
		"**lifelog** (09:30): Here is my answer."
	if body != want {
		t.Errorf("page body\n%q\nwant\n%q", body, want)
	}
	// The way the API and a chat read the line: one name, the words "failed: unknown tool", and no address.
	last := body[strings.LastIndex(body, "\n- `")+1 : strings.Index(body, "\n\n**lifelog**")]
	if got := siren.ToolLine(last); got.Name != strings.ReplaceAll(forged, "`", "'") || got.Words != "failed: unknown tool" || got.Href != "" {
		t.Errorf("siren.ToolLine(%q) = %+v, want the whole name of the tool, the words of the failure and no address", last, got)
	}
}

func TestACharacterThatBreaksALineBecomesASpace(t *testing.T) {
	for in, want := range map[string]string{
		"plain":            "plain",
		"two\nlines":       "two lines",
		"crlf\r\nbreak":    "crlf  break",
		"tab\there":        "tab here",
		"nul\x00and\x1bx":  "nul and x",
		"ls\u2028ps\u2029": "ls ps ",
		"é and 日本":         "é and 日本",
	} {
		if got := oneLine(in); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAnAnswerWithATurnThatHasNoKeyIsRefused: reply names a turn by its key. A server that gives no key is too old.
func TestAnAnswerWithATurnThatHasNoKeyIsRefused(t *testing.T) {
	good := &siren.Entity{Properties: map[string]any{"day": today, "id": 1, "version": 2, "body": "[[" + today + "]]\n\n**Owner** (09:30): hi",
		"turns": []any{map[string]any{"who": "owner", "key": "0123456789abcdef", "text": "hi"}}}}
	p, err := pageFrom(good)
	if err != nil || !p.pending() || p.lastOwner() != "0123456789abcdef" {
		t.Fatalf("page %+v, error %v, want a page that waits and names the key of its last owner turn", p, err)
	}
	for name, turn := range map[string]map[string]any{
		"no key":    {"who": "owner", "text": "hi"},
		"empty key": {"who": "owner", "key": "", "text": "hi"},
		"a number":  {"who": "owner", "key": 1, "text": "hi"},
	} {
		bad := &siren.Entity{Properties: map[string]any{"day": today, "id": 1, "version": 2, "body": "x", "turns": []any{turn}}}
		if _, err := pageFrom(bad); err == nil || !strings.Contains(err.Error(), "key") {
			t.Errorf("%s: error %v, want one that names the key", name, err)
		}
	}
}
