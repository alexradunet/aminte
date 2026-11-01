package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/app"
	"lifelog/client"
	"lifelog/siren"
)

// stateSeen reads the turns that the state file holds for each day: the number of turns that the relay has dealt with.
func stateSeen(t *testing.T, path string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s struct{ Seen map[string][]string }
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for day, keys := range s.Seen {
		out[day] = len(keys)
	}
	return out
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

// TestAReplyPlacedBeforeANewerTurnOfTheOwnerReachesTheChat: the owner sends two messages in a row, and the core puts the
// reply to the first one before the second turn. The relay has dealt with the second turn already. A relay that keeps a
// position in the turns would take the reply for a turn it had sent, and the chat would never get it.
func TestAReplyPlacedBeforeANewerTurnOfTheOwnerReachesTheChat(t *testing.T) {
	r := start(t)
	r.chat.say(t, "first")
	r.api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "first") })
	r.settle(t)
	r.chat.say(t, "second")
	r.api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "second") })
	r.settle(t) // the relay has dealt with the second turn
	r.api.edit(today, "[[2031-05-20]]\n\n**Owner** (09:30, via whatsapp): first\n\n**lifelog** (09:31): reply to first\n\n**Owner** (09:30, via whatsapp): second")
	r.poll(t)
	if got := r.chat.next(t); got != "reply to first" {
		t.Errorf("sent %q, want the reply that came before the newer turn", got)
	}
	r.api.agentReplies(today, "reply to second")
	r.poll(t)
	if got := r.chat.next(t); got != "reply to second" {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	r.chat.none(t)
}

// TestATurnOfAnotherSurfaceAndTheReplyBeforeItAreEachSentOnce: the owner says T2 in the browser while the model works on
// the first turn. The relay sends T2 marked. The reply to the first turn then lands before T2: it is sent once, and T2 is
// not sent again.
func TestATurnOfAnotherSurfaceAndTheReplyBeforeItAreEachSentOnce(t *testing.T) {
	r := start(t)
	r.chat.say(t, "first")
	r.api.waitBody(t, today, func(b string) bool { return strings.Contains(b, "first") })
	r.settle(t)
	r.api.ownerSaysElsewhere(today, "from the browser")
	r.poll(t)
	if got := r.chat.next(t); got != "You wrote in lifelog: from the browser" {
		t.Errorf("sent %q", got)
	}
	r.api.edit(today, "[[2031-05-20]]\n\n**Owner** (09:30, via whatsapp): first\n\n**lifelog** (09:31): reply to first\n\n**Owner** (09:30): from the browser")
	r.poll(t)
	if got := r.chat.next(t); got != "reply to first" {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	r.chat.none(t)
}

// TestAnEditAboveTheTurnsSendsNothingAgain: a paragraph that an edit adds before the first marker is a turn of its own in
// the parse of the page, and every other turn moves by one place. No turn is new.
func TestAnEditAboveTheTurnsSendsNothingAgain(t *testing.T) {
	r := start(t)
	r.settle(t)
	r.api.ownerSaysElsewhere(today, "one")
	r.api.agentReplies(today, "answer one")
	r.poll(t)
	r.chat.next(t)
	r.chat.next(t)
	body := r.api.body(today)
	r.api.edit(today, strings.Replace(body, "[[2031-05-20]]", "[[2031-05-20]]\n\nNotes that the owner wrote above the talk.", 1))
	r.settle(t)
	r.chat.none(t)
	r.api.agentReplies(today, "answer two")
	r.poll(t)
	if got := r.chat.next(t); got != "answer two" {
		t.Errorf("sent %q", got)
	}
}

// TestAParagraphAddedAboveATurnIsPartOfTheTurnBeforeItAndThatTurnIsSentAgain: a paragraph with no marker line belongs to
// the turn before it. That turn has new words, so it has a new key, and the chat gets it again with the paragraph. The
// turn below the paragraph keeps its key, and it is not sent again.
func TestAParagraphAddedAboveATurnIsPartOfTheTurnBeforeItAndThatTurnIsSentAgain(t *testing.T) {
	r := start(t)
	r.settle(t)
	r.api.ownerSaysElsewhere(today, "one")
	r.api.agentReplies(today, "answer one")
	r.api.ownerSaysElsewhere(today, "two")
	r.poll(t)
	for _, want := range []string{"You wrote in lifelog: one", "answer one", "You wrote in lifelog: two"} {
		if got := r.chat.next(t); got != want {
			t.Errorf("sent %q, want %q", got, want)
		}
	}
	body := r.api.body(today)
	r.api.edit(today, strings.Replace(body, "\n\n**Owner** (09:30): two", "\n\nA paragraph above two.\n\n**Owner** (09:30): two", 1))
	r.poll(t)
	if got := r.chat.next(t); got != "answer one\n\nA paragraph above two." {
		t.Errorf("sent %q, want the turn before the paragraph, with the paragraph", got)
	}
	r.settle(t)
	r.chat.none(t)
}

// TestADeletedPageThatIsRevivedSendsNoEarlierTurnAgain: a deleted page has no turns, and a revive brings them back with the
// new turn. The relay keeps what it dealt with, so only the new turn is sent.
func TestADeletedPageThatIsRevivedSendsNoEarlierTurnAgain(t *testing.T) {
	r := start(t)
	r.settle(t)
	r.api.ownerSaysElsewhere(today, "one")
	r.api.agentReplies(today, "answer one")
	r.poll(t)
	r.chat.next(t)
	r.chat.next(t)
	body := r.api.remove(today)
	r.settle(t)
	r.chat.none(t)
	r.api.edit(today, body)
	r.api.ownerSaysElsewhere(today, "back again")
	r.poll(t)
	if got := r.chat.next(t); got != "You wrote in lifelog: back again" {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	r.chat.none(t)
}

// TestTwoTurnsOfTheSameMinuteAndTheSameWordsAreBothSentAndNoneAgain: the core gives two such turns two keys, so the
// relay sends both and sends none again.
func TestTwoTurnsOfTheSameMinuteAndTheSameWordsAreBothSentAndNoneAgain(t *testing.T) {
	api := &fakeAPI{}
	state := t.TempDir() + "/state.json"
	r := startOn(t, api, state)
	r.settle(t)
	api.agentReplies(today, "Done.")
	r.poll(t)
	if got := r.chat.next(t); got != "Done." {
		t.Errorf("sent %q", got)
	}
	api.agentReplies(today, "Done.")
	r.poll(t)
	if got := r.chat.next(t); got != "Done." {
		t.Errorf("sent %q for the second turn", got)
	}
	r.settle(t)
	r.chat.none(t)
	r.stop(t)
	r2 := startOn(t, api, state)
	r2.settle(t)
	r2.chat.none(t)
}

func TestAFirstRunSendsNothingThatThePageHoldsAlready(t *testing.T) {
	api := &fakeAPI{}
	api.ownerSaysElsewhere(today, "earlier")
	api.agentReplies(today, "an old answer")
	r := startOn(t, api, t.TempDir()+"/state.json")
	r.settle(t)
	r.chat.none(t)
	if days := stateSeen(t, r.state); len(days) != 1 || days[today] != 2 {
		t.Errorf("state seen %v, want {%s: 2}", days, today)
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
	if days := stateSeen(t, r.state); len(days) != 2 || days[today] != 1 || days["2031-05-21"] != 0 {
		t.Errorf("state seen %v, want yesterday kept while its last turn is the owner's", days)
	}
	api.agentReplies(today, "Good night.") // the agent answered the turn of yesterday on its own page
	r.poll(t)
	if got := r.chat.next(t); got != "Good night." {
		t.Errorf("sent %q", got)
	}
	r.settle(t)
	if days := stateSeen(t, r.state); len(days) != 1 || days["2031-05-21"] != 0 {
		t.Errorf("state seen %v, want the answered day dropped", days)
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
	if err := os.WriteFile(state, []byte(`{"seen":{"2031-05-17":[],"2031-05-20":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := startOn(t, api, state)
	r.settle(t)
	r.chat.none(t)
	if days := stateSeen(t, state); len(days) != 1 || days[today] != 0 {
		t.Errorf("state seen %v, want only today", days)
	}
	for _, q := range api.requests() {
		if strings.Contains(q.Path, "2031-05-17") {
			t.Errorf("the relay read %s: a day older than yesterday is dropped, not read", q.Path)
		}
	}
}

// TestAnEditOfTheWordsOfATurnGivesItANewKeyAndSendsItAgain: the key of a turn is made from its words. A turn that the
// owner edits is a changed turn, and the chat gets it again. A turn that the edit leaves as it is, is not sent again.
func TestAnEditOfTheWordsOfATurnGivesItANewKeyAndSendsItAgain(t *testing.T) {
	api := &fakeAPI{}
	r := startWith(t, api)
	r.settle(t)
	api.ownerSaysElsewhere(today, "one")
	api.agentReplies(today, "Teh answer.")
	r.poll(t)
	if got := r.chat.next(t); got != "You wrote in lifelog: one" {
		t.Errorf("sent %q", got)
	}
	if got := r.chat.next(t); got != "Teh answer." {
		t.Errorf("sent %q", got)
	}
	api.edit(today, strings.Replace(api.body(today), "Teh answer.", "The answer.", 1))
	r.poll(t)
	if got := r.chat.next(t); got != "The answer." {
		t.Errorf("sent %q, want the changed turn", got)
	}
	api.edit(today, api.body(today)+"\n\nA note that the owner added.")
	r.poll(t)
	if got := r.chat.next(t); got != "The answer.\n\nA note that the owner added." {
		t.Errorf("sent %q, want the turn with the added text: it is a changed turn", got)
	}
	r.settle(t)
	r.chat.none(t)
}

// TestAnEditThatTakesTurnsAwayDoesNotHideTheNextTurnOfTheSameWords: a reply that the owner removed leaves no key in the
// state. A later reply of the same minute with the same words has the same key, and it is a new turn.
func TestAnEditThatTakesTurnsAwayDoesNotHideTheNextTurnOfTheSameWords(t *testing.T) {
	api := &fakeAPI{}
	r := startWith(t, api)
	r.settle(t)
	api.ownerSaysElsewhere(today, "one")
	api.agentReplies(today, "Noted.")
	r.poll(t)
	r.chat.next(t)
	r.chat.next(t)
	api.edit(today, "[[2031-05-20]]\n\n**Owner** (09:30): one")
	r.settle(t)
	if days := stateSeen(t, r.state); days[today] != 1 {
		t.Errorf("state seen %v, want the one turn that the page holds", days)
	}
	api.ownerSaysElsewhere(today, "two")
	api.agentReplies(today, "Noted.")
	r.poll(t)
	if got := r.chat.next(t); got != "You wrote in lifelog: two" {
		t.Errorf("sent %q", got)
	}
	if got := r.chat.next(t); got != "Noted." {
		t.Errorf("sent %q, want the new reply: its key is the key of the reply that was removed", got)
	}
	r.settle(t)
	r.chat.none(t)
}

// TestAPageWithNoTurnsKeepsTheStateOfItsDay: a deleted page has no turns, and a revive brings them back. The keys stay,
// so the turns that come back are not sent again (see also TestADeletedPageThatIsRevivedSendsNoEarlierTurnAgain).
func TestAPageWithNoTurnsKeepsTheStateOfItsDay(t *testing.T) {
	api := &fakeAPI{}
	r := startWith(t, api)
	r.settle(t)
	api.ownerSaysElsewhere(today, "one")
	api.agentReplies(today, "answer one")
	r.poll(t)
	r.chat.next(t)
	r.chat.next(t)
	api.remove(today)
	r.settle(t)
	if days := stateSeen(t, r.state); days[today] != 2 {
		t.Errorf("state seen %v, want the two keys kept", days)
	}
}

func TestAnAnswerWithATurnThatHasNoKeyIsAnErrorAndAnAnswerWithKeysHasTheKeysOfItsTurns(t *testing.T) {
	good := &siren.Entity{Properties: map[string]any{"day": today, "turns": []any{
		map[string]any{"who": "owner", "key": "0123456789abcdef", "at": "09:30", "text": "hi"},
		map[string]any{"who": "lifelog", "key": "fedcba9876543210", "at": "09:31", "text": "hello"},
	}}}
	p, err := pageFrom(good)
	if err != nil || !slices.Equal(p.keys(), []string{"0123456789abcdef", "fedcba9876543210"}) || p.pending() {
		t.Fatalf("page %+v, error %v, want the two keys of the turns, and a page that does not wait", p, err)
	}
	for name, turn := range map[string]map[string]any{
		"no key":    {"who": "owner", "text": "hi"},
		"empty key": {"who": "owner", "key": "", "text": "hi"},
		"a number":  {"who": "owner", "key": 1, "text": "hi"},
	} {
		bad := &siren.Entity{Properties: map[string]any{"day": today, "turns": []any{turn}}}
		if _, err := pageFrom(bad); err == nil || !strings.Contains(err.Error(), "key") {
			t.Errorf("%s: error %v, want one that names the key", name, err)
		}
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
	if days := stateSeen(t, r.state); days[today] != 0 {
		t.Errorf("state seen %v: a turn that was not sent is not marked", days)
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

// TestAnEarlierVersionOfTheStateFileIsAFirstRun: the file of the agent that lived in this program (a page, no turns), and
// the file of the relay that counted turns (days), are read as a first run. The time of the last save of the first one is
// the time of the last message that was said, so a message that came during the switch is said.
func TestAnEarlierVersionOfTheStateFileIsAFirstRun(t *testing.T) {
	for name, old := range map[string]string{
		"the agent":       `{"day":"2031-05-20","id":"1","version":"3","body":"[[2031-05-20]]","saved_at":"2031-05-20T08:00:00Z"}`,
		"the counting":    `{"days":{"2031-05-20":3},"said":"2031-05-20T08:00:00Z"}`,
		"the empty relay": `{"days":{},"said":"0001-01-01T00:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			api := &fakeAPI{}
			api.agentReplies(today, "an old answer")
			state := t.TempDir() + "/state.json"
			if err := os.WriteFile(state, []byte(old), 0o600); err != nil {
				t.Fatal(err)
			}
			r := startOn(t, api, state)
			r.settle(t)
			r.chat.none(t)
			if seen := stateSeen(t, state); seen[today] != 1 {
				t.Errorf("state seen %v, want the page adopted", seen)
			}
		})
	}
}

func TestTheSavedAtOfTheAgentStateFileIsTheTimeOfTheLastSaidMessage(t *testing.T) {
	state := t.TempDir() + "/state.json"
	old := `{"day":"2031-05-20","id":"1","version":"3","body":"[[2031-05-20]]","saved_at":"2031-05-20T08:00:00Z"}`
	if err := os.WriteFile(state, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2031, 5, 20, 8, 0, 0, 0, time.UTC)
	if got, ok, err := LastSaid(state); err != nil || !ok || !got.Equal(want) {
		t.Errorf("LastSaid %v %v %v, want %v", got, ok, err, want)
	}
	// The relay keeps that time through its first save, and a newer message moves it.
	r := startOn(t, &fakeAPI{}, state)
	r.settle(t)
	if got, ok, err := LastSaid(state); err != nil || !ok || !got.Equal(want) {
		t.Errorf("LastSaid after the first run %v %v %v, want %v", got, ok, err, want)
	}
	r.chat.say(t, "hello")
	r.settle(t) // the relay takes a tick only after it has said the message and saved the state
	if got, _, _ := LastSaid(state); !got.Equal(fixedNow) {
		t.Errorf("LastSaid after a say %v, want %v", got, fixedNow)
	}
	if file, err := os.ReadFile(state); err != nil || strings.Contains(string(file), "saved_at") {
		t.Errorf("state file %q (%v): the old key must not stay", file, err)
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
	if err := run(noSay); app.ExitCode(err) != app.ExitOwnerActs || !strings.Contains(err.Error(), "say") {
		t.Errorf("a catalog with no say: %v, want an error that names say, and exit status %d", err, app.ExitOwnerActs)
	}
	down := &fakeAPI{}
	down.setCatalogFails(true)
	if err := run(down); err == nil || app.ExitCode(err) != 1 {
		t.Errorf("a catalog that fails: %v, want an error with exit status 1", err)
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
