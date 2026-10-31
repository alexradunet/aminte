package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"lifelog/client"
)

// stateDays reads the days of the state file.
func stateDays(t *testing.T, path string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s struct{ Days map[string]int }
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s.Days
}

func TestAMessageOfTheOwnerIsSaidToTheConversationAsTheOwnerWithViaWhatsapp(t *testing.T) {
	r := start(t)
	r.chat.say(t, "I walked by the river")
	body := r.api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "river") })
	if want := "[[2031-05-20]]\n\n**Owner** (09:30, via whatsapp): I walked by the river"; body != want {
		t.Errorf("page body %q, want %q", body, want)
	}
	says := r.api.says()
	if len(says) != 1 || says[0].Path != "/days/today/say" || says[0].Source != "" || says[0].Auth != "Bearer "+token ||
		!strings.Contains(says[0].Body, "text=I+walked+by+the+river") || !strings.Contains(says[0].Body, "via=whatsapp") {
		t.Errorf("say requests %+v, want one as the owner with the token, no source and via=whatsapp", says)
	}
	// The turn came from this relay: it is not sent back.
	r.settle(t)
	r.chat.none(t)
}

func TestAnotherSenderIsIgnoredAndAnImageOfTheOwnerGetsOneLine(t *testing.T) {
	r := start(t)
	r.chat.deliver(t, Message{Sender: "999", Text: "hi", Kind: KindText, At: fixedNow})
	r.chat.deliver(t, Message{Sender: "999", Kind: KindImage, At: fixedNow})
	r.chat.deliver(t, Message{Sender: owner, Kind: KindImage, At: fixedNow})
	if got := r.chat.next(t); got != "I read text for now; send it as text." {
		t.Errorf("line %q", got)
	}
	r.chat.say(t, "hello")
	r.api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "hello") })
	r.settle(t)
	r.chat.none(t)
	if n := len(r.api.says()); n != 1 {
		t.Errorf("%d says, want 1 (only the owner's text)", n)
	}
	if body := r.api.body(today); strings.Contains(body, "999") || strings.Contains(body, "image") {
		t.Errorf("page body %q", body)
	}
}

func TestTheReplyOfTheAgentOnThePageReachesTheChatOnce(t *testing.T) {
	r := start(t)
	r.chat.say(t, "hello")
	r.api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "hello") })
	r.api.agentReplies(today, "Hello, owner.")
	r.poll(t)
	if got := r.chat.next(t); got != "Hello, owner." {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	r.chat.none(t)
}

func TestATurnOfTheOwnerFromAnotherSurfaceGoesToTheChatMarked(t *testing.T) {
	r := start(t)
	r.settle(t) // the first look: no page yet
	r.api.ownerSaysElsewhere(today, "what did I do?")
	r.poll(t)
	if got := r.chat.next(t); got != "You wrote in lifelog: what did I do?" {
		t.Errorf("sent %q", got)
	}
	r.api.agentReplies(today, "You walked.")
	r.poll(t)
	if got := r.chat.next(t); got != "You walked." {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	r.chat.none(t)
}

func TestAFirstRunSendsNothingThatThePageHoldsAlready(t *testing.T) {
	api := &fakeAPI{}
	api.ownerSaysElsewhere(today, "earlier")
	api.agentReplies(today, "an old answer")
	r := startOn(t, api, t.TempDir()+"/state.json")
	r.settle(t)
	r.chat.none(t)
	if days := stateDays(t, r.state); len(days) != 1 || days[today] != 2 {
		t.Errorf("state days %v, want {%s: 2}", days, today)
	}
	api.agentReplies(today, "a new answer")
	r.poll(t)
	if got := r.chat.next(t); got != "a new answer" {
		t.Errorf("sent %q", got)
	}
}

func TestARestartSendsTheReplyThatCameWhileItWasOffOnce(t *testing.T) {
	api := &fakeAPI{}
	state := t.TempDir() + "/state.json"
	r := startOn(t, api, state)
	r.chat.say(t, "hello")
	api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "hello") })
	r.settle(t)
	if err := r.stop(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	api.agentReplies(today, "answered while the relay was off")
	r2 := startOn(t, api, state)
	if got := r2.chat.next(t); got != "answered while the relay was off" {
		t.Errorf("sent %q", got)
	}
	r2.settle(t)
	r2.chat.none(t)
	r2.stop(t)
	r3 := startOn(t, api, state)
	r3.settle(t)
	r3.chat.none(t)
}

func TestAReplyAfterMidnightOnTheOldPageIsSentAndTheOldDayIsDropped(t *testing.T) {
	api := &fakeAPI{}
	r := startWith(t, api)
	r.chat.say(t, "good night")
	api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "good night") })
	r.settle(t)
	api.setToday("2031-05-21")
	r.settle(t)
	if days := stateDays(t, r.state); len(days) != 2 || days[today] != 1 || days["2031-05-21"] != 0 {
		t.Errorf("state days %v, want yesterday kept while its last turn is the owner's", days)
	}
	api.agentReplies(today, "Good night.") // the agent answered the turn of yesterday on its own page
	r.poll(t)
	if got := r.chat.next(t); got != "Good night." {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	if days := stateDays(t, r.state); len(days) != 1 || days["2031-05-21"] != 0 {
		t.Errorf("state days %v, want the answered day dropped", days)
	}
	api.ownerSaysElsewhere("2031-05-21", "good morning")
	r.poll(t)
	if got := r.chat.next(t); got != "You wrote in lifelog: good morning" {
		t.Errorf("sent %q", got)
	}
	r.chat.none(t)
}

// startWith starts a relay on the API that the test made.
func startWith(t *testing.T, api *fakeAPI) *rig {
	t.Helper()
	return startOn(t, api, t.TempDir()+"/state.json")
}

func TestADayOlderThanYesterdayIsDroppedAndNeverRead(t *testing.T) {
	api := &fakeAPI{}
	api.ownerSaysElsewhere("2031-05-17", "old")
	api.agentReplies("2031-05-17", "an answer that the chat never got")
	state := t.TempDir() + "/state.json"
	if err := os.WriteFile(state, []byte(`{"days":{"2031-05-17":0,"2031-05-20":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := startOn(t, api, state)
	r.settle(t)
	r.chat.none(t)
	if days := stateDays(t, state); len(days) != 1 || days[today] != 0 {
		t.Errorf("state days %v, want only today", days)
	}
	for _, q := range api.requests() {
		if strings.Contains(q.Path, "2031-05-17") {
			t.Errorf("the relay read %s: a day older than yesterday is dropped, not read", q.Path)
		}
	}
}

func TestTextThatAnEditAddsToAReplyIsNotSentAndAnEditThatTakesTurnsAwayDoesNotHideTheNextReply(t *testing.T) {
	api := &fakeAPI{}
	r := startWith(t, api)
	r.settle(t)
	api.ownerSaysElsewhere(today, "one")
	api.agentReplies(today, "answer one")
	r.poll(t)
	r.chat.next(t)
	r.chat.next(t)
	api.edit(today, api.body(today)+"\n\nA note that the owner added.")
	r.settle(t)
	r.chat.none(t)
	// The owner takes the last turn away. The next reply is a new turn and is sent.
	api.edit(today, "[[2031-05-20]]\n\n**Owner** (09:30): one")
	r.settle(t)
	api.agentReplies(today, "answer two")
	r.poll(t)
	if got := r.chat.next(t); got != "answer two" {
		t.Errorf("sent %q", got)
	}
}

func TestATextThatCannotBeSentIsTriedAgainAtTheNextLook(t *testing.T) {
	api := &fakeAPI{}
	r := startWith(t, api)
	r.settle(t)
	r.chat.setFailSend(true)
	api.agentReplies(today, "the answer")
	r.settle(t)
	r.chat.none(t)
	if days := stateDays(t, r.state); days[today] != 0 {
		t.Errorf("state days %v: a turn that was not sent is not marked", days)
	}
	r.chat.setFailSend(false)
	r.poll(t)
	if got := r.chat.next(t); got != "the answer" {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	r.chat.none(t)
}

func TestASayThatTheAPIRefusesTellsTheOwnerWithTheFirstLineOfTheReason(t *testing.T) {
	api := &fakeAPI{}
	api.setSayFails(true)
	r := startWith(t, api)
	r.chat.say(t, "hello")
	got := r.chat.next(t)
	if !strings.HasPrefix(got, "I could not write your message to lifelog: ") || !strings.HasSuffix(got, ". Say it again.") || strings.Contains(got, "second line") {
		t.Errorf("sent %q", got)
	}
	if _, ok, err := LastSaid(r.state); ok || err != nil {
		t.Errorf("LastSaid %v %v: a message that was not said is not marked", ok, err)
	}
}

func TestTheTimeOfTheLastSaidMessageIsKeptForTheNextRun(t *testing.T) {
	r := start(t)
	at := fixedNow.Add(-time.Hour)
	r.chat.deliver(t, Message{Sender: owner, Text: "older", Kind: KindText, At: at})
	r.chat.deliver(t, Message{Sender: owner, Text: "newer", Kind: KindText, At: fixedNow})
	r.chat.deliver(t, Message{Sender: owner, Text: "late arrival of an older one", Kind: KindText, At: at})
	r.api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "late arrival") })
	got, ok, err := LastSaid(r.state)
	if err != nil || !ok || !got.Equal(fixedNow) {
		t.Errorf("LastSaid %v %v %v, want %v", got, ok, err, fixedNow)
	}
	if _, ok, err := LastSaid(r.state + ".missing"); ok || err != nil {
		t.Errorf("LastSaid of a missing file: %v %v", ok, err)
	}
}

func TestAnEarlierVersionOfTheStateFileIsAFirstRun(t *testing.T) {
	api := &fakeAPI{}
	api.agentReplies(today, "an old answer")
	state := t.TempDir() + "/state.json"
	old := `{"day":"2031-05-20","id":"1","version":"3","body":"[[2031-05-20]]","saved_at":"2031-05-20T08:00:00Z"}`
	if err := os.WriteFile(state, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LastSaid(state); ok || err != nil {
		t.Errorf("LastSaid of the earlier file: %v %v, want none", ok, err)
	}
	r := startOn(t, api, state)
	r.settle(t)
	r.chat.none(t)
	if days := stateDays(t, state); days[today] != 1 {
		t.Errorf("state days %v, want the page adopted", days)
	}
}

func TestAStateFileThatIsNotJSONStopsTheRunWithTheNameOfTheFile(t *testing.T) {
	state := t.TempDir() + "/state.json"
	if err := os.WriteFile(state, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(&fakeAPI{})
	t.Cleanup(api.Close)
	rl := &Relay{Chat: newFakeChat(), API: client.Remote(api.URL, "", token), Owner: owner, State: state, Log: log.New(io.Discard, "", 0)}
	if err := rl.Run(t.Context()); err == nil || !strings.Contains(err.Error(), state) {
		t.Errorf("Run returned %v, want an error that names %s", err, state)
	}
}

func TestACatalogWithoutSayIsRefusedAsAnActionTheOwnerMustAddButACatalogThatFailsIsNot(t *testing.T) {
	run := func(api *fakeAPI) error {
		srv := httptest.NewServer(api)
		t.Cleanup(srv.Close)
		rl := &Relay{Chat: newFakeChat(), API: client.Remote(srv.URL, "", token), Owner: owner, State: t.TempDir() + "/state.json", Log: log.New(io.Discard, "", 0)}
		return rl.Run(t.Context())
	}
	noSay := &fakeAPI{}
	noSay.dropAction("say")
	if err := run(noSay); !errors.Is(err, ErrNoAction) {
		t.Errorf("a catalog with no say: %v, want ErrNoAction", err)
	}
	down := &fakeAPI{}
	down.setCatalogFails(true)
	if err := run(down); err == nil || errors.Is(err, ErrNoAction) {
		t.Errorf("a catalog that fails: %v, want an error that is not ErrNoAction", err)
	}
}

func TestAClosedChatEndsTheRunWithNoErrorAndACancelEndsItWithTheErrorOfTheContext(t *testing.T) {
	r := start(t)
	close(r.chat.in)
	select {
	case err := <-r.done:
		r.done <- err
		if err != nil {
			t.Errorf("Run returned %v after the chat closed, want nil", err)
		}
	case <-time.After(wait):
		t.Fatal("Run did not return after the chat closed")
	}
	r2 := start(t)
	if err := r2.stop(t); !errors.Is(err, context.Canceled) {
		t.Errorf("Run returned %v after the cancel, want context.Canceled", err)
	}
}

func TestATextLongerThanTheBoundIsCutAtACharacterWithANote(t *testing.T) {
	r := start(t)
	r.settle(t)
	r.api.agentReplies(today, strings.Repeat("é", maxSend))
	r.poll(t)
	got := r.chat.next(t)
	if !strings.HasSuffix(got, "[cut: the text is longer]") || len(got) > maxSend+40 || strings.Contains(got, "�") {
		t.Errorf("sent %d bytes, tail %q", len(got), got[max(0, len(got)-40):])
	}
	if short := cut("short"); short != "short" {
		t.Errorf("a short text was changed: %q", short)
	}
}
