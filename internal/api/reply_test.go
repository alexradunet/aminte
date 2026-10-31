package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/internal/api/apitest"
	"lifelog/siren"
)

// reply runs the action reply as the writer source, with the values v ("day" is a path field of the action).
func reply(t *testing.T, h http.Handler, source string, v map[string]string) (*siren.Entity, error) {
	t.Helper()
	c := client.InProcess(h, source)
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	return c.Do(t.Context(), apitest.Action(t, catalog, "reply"), v)
}

// turnsOf reads the turns of the answer of a conversation as who and text pairs ("who: text").
func turnsOf(t *testing.T, e *siren.Entity) []string {
	t.Helper()
	props, _ := e.Properties.(map[string]any)
	turns, _ := props["turns"].([]any)
	var out []string
	for _, x := range turns {
		m, _ := x.(map[string]any)
		out = append(out, fmt.Sprintf("%v: %v", m["who"], m["text"]))
	}
	return out
}

// TestReplyWritesTheAgentsTurnRightAfterTheTurnItAnswers: the reply goes after the owner's turn that it answers, with
// the tool lines before its marker, in one write. A turn that the owner said meanwhile stays after it and waits; its
// own reply goes at the end. The stamp is the time of the server in its zone. The answer is the conversation.
func TestReplyWritesTheAgentsTurnRightAfterTheTurnItAnswers(t *testing.T) {
	c, h, s := conversationStore(t)
	title := "Conversation " + convDay
	for _, words := range []string{"first", "second"} { // the owner says two turns while the model works on the first
		if _, err := say(t, c, convDay, words); err != nil {
			t.Fatal(err)
		}
	}
	e, err := reply(t, h, "agent:lifelog", map[string]string{
		"day": "today", "turn": "1", "text": "answer one\r\nsecond line",
		"tools": "- `capture` → [/pages/3](/pages/3)\r\n\r\n- `search`: no hits\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "[[2031-05-14]]\n\n**Owner** (11:07): first\n\n" +
		"- `capture` → [/pages/3](/pages/3)\n- `search`: no hits\n\n**lifelog** (11:07): answer one\nsecond line\n\n" +
		"**Owner** (11:07): second"
	if got := bodyOf(t, s, title); got != want {
		t.Errorf("the body after the reply to turn 1:\n%q\nwant:\n%q", got, want)
	}
	if got, want := strings.Join(turnsOf(t, e), " | "), "owner: first | lifelog: answer one\nsecond line | owner: second"; got != want {
		t.Errorf("the turns of the answer: %q, want %q", got, want)
	}
	if e.Title != title || e.Result == nil {
		t.Errorf("the answer: %q %v, want the conversation with the result of a save", e.Title, e.Result)
	}
	if _, err := reply(t, h, "agent:lifelog", map[string]string{"day": convDay, "turn": "2", "text": "answer two"}); err != nil {
		t.Fatal(err)
	}
	want += "\n\n**lifelog** (11:07): answer two"
	if got := bodyOf(t, s, title); got != want {
		t.Errorf("the body after the reply to turn 2:\n%q\nwant:\n%q", got, want)
	}
}

// TestAReplyToATurnThatHasOneIsRefused: a second reply to a turn is 409 and writes nothing; so a retry of a reply that
// was written cannot answer twice. A reply to a turn that does not exist is 404, and a turn that is not a number of 1
// or more is 422. A day with no conversation is 404.
func TestAReplyToATurnThatHasOneIsRefused(t *testing.T) {
	c, h, s := conversationStore(t)
	title := "Conversation " + convDay
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": "1", "text": "Hi"}); err != nil {
		t.Fatal(err)
	}
	before := bodyOf(t, s, title)
	for _, tc := range []struct {
		name string
		v    map[string]string
		want int
	}{
		{"a second reply to turn 1", map[string]string{"day": "today", "turn": "1", "text": "Again"}, 409},
		{"a turn the page does not have", map[string]string{"day": "today", "turn": "2", "text": "Hi"}, 404},
		{"turn 0", map[string]string{"day": "today", "turn": "0", "text": "Hi"}, 422},
		{"a negative turn", map[string]string{"day": "today", "turn": "-1", "text": "Hi"}, 422},
		{"a turn that is not a number", map[string]string{"day": "today", "turn": "one", "text": "Hi"}, 422},
		{"a turn with a fraction", map[string]string{"day": "today", "turn": "1.5", "text": "Hi"}, 422},
		{"no turn", map[string]string{"day": "today", "text": "Hi"}, 422},
		{"no text", map[string]string{"day": "today", "turn": "1"}, 422},
		{"a text of blanks", map[string]string{"day": "today", "turn": "1", "text": " \r\n "}, 422},
		{"a tool line that is not one", map[string]string{"day": "today", "turn": "1", "text": "Hi", "tools": "- `get`\nnot a tool line"}, 422},
		{"a field the action does not list", map[string]string{"day": "today", "turn": "1", "text": "Hi", "via": "x"}, 422},
		{"a day that is not a day", map[string]string{"day": "not-a-day", "turn": "1", "text": "Hi"}, 422},
		{"a day with no conversation", map[string]string{"day": "2031-05-10", "turn": "1", "text": "Hi"}, 404},
	} {
		_, err := reply(t, h, "agent:lifelog", tc.v)
		if got := apitest.Status(err); got != tc.want {
			t.Errorf("%s: %v, want %d", tc.name, err, tc.want)
		}
		if got := bodyOf(t, s, title); got != before {
			t.Errorf("%s: the refused reply wrote: %q, want %q", tc.name, got, before)
		}
	}
	if id, err := s.PageID(t.Context(), "Conversation 2031-05-10"); err != nil || id != 0 {
		t.Errorf("a refused reply made a page: %d %v", id, err)
	}
}

// TestReplyIsNotTheOwnersAndAnswersAnyDay: an agent, an import and the owner may run reply (it is no owner action),
// and it answers the conversation of a day before today: a turn said before midnight is answered on its own page.
func TestReplyIsNotTheOwnersAndAnswersAnyDay(t *testing.T) {
	_, h, s := conversationStore(t)
	for i, source := range []string{"agent:lifelog", "import:x", "cli"} {
		day := fmt.Sprintf("2031-05-%02d", 10+i)
		if _, _, err := s.CreatePage(t.Context(), "cli", "Conversation "+day, "[["+day+"]]\n\n**Owner** (23:58): late", "Conversations"); err != nil {
			t.Fatal(err)
		}
		e, err := reply(t, h, source, map[string]string{"day": day, "turn": "1", "text": "answer"})
		if err != nil {
			t.Fatalf("reply as %s: %v", source, err)
		}
		if got, want := strings.Join(turnsOf(t, e), " | "), "owner: late | lifelog: answer"; got != want {
			t.Errorf("the turns after the reply as %s to %s: %q, want %q", source, day, got, want)
		}
	}
}

// TestAReplyDoesNotWriteADeletedConversation: the page of a conversation that is deleted is no conversation. A reply to
// it is 409, as any write to a deleted page is, and it does not revive the page (say does).
func TestAReplyDoesNotWriteADeletedConversation(t *testing.T) {
	c, h, s := conversationStore(t)
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.PageID(t.Context(), "Conversation "+convDay)
	if err := s.Tombstone(t.Context(), "cli", id); err != nil {
		t.Fatal(err)
	}
	if _, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": "1", "text": "Hi"}); apitest.Status(err) != 409 {
		t.Errorf("reply to a deleted conversation: %v, want 409", err)
	}
	if p, err := s.PageByID(t.Context(), id); err != nil || p.DeletedAt == "" || strings.Contains(p.Body, "lifelog") {
		t.Errorf("the page after the refused reply: %v %+v, want it deleted and unchanged", err, p)
	}
}

// TestAnUnclosedFenceInASayDoesNotHideTheReply: the owner says a text that opens a code fence and does not close it.
// The core closes the fence at the end of the turn, so the marker of the reply is read as a marker, and the page
// ends with the agent's turn. (Before, the reply was part of the owner's turn and the agent answered it again.)
func TestAnUnclosedFenceInASayDoesNotHideTheReply(t *testing.T) {
	c, h, s := conversationStore(t)
	if _, err := say(t, c, convDay, "look at this:\n```\nfunc main() {"); err != nil {
		t.Fatal(err)
	}
	want := "[[2031-05-14]]\n\n**Owner** (11:07): look at this:\n```\nfunc main() {\n```"
	if got := bodyOf(t, s, "Conversation "+convDay); got != want {
		t.Errorf("the body after the say:\n%q\nwant:\n%q", got, want)
	}
	e, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": "1", "text": "I see a function."})
	if err != nil {
		t.Fatal(err)
	}
	got := turnsOf(t, e)
	if len(got) != 2 || !strings.HasPrefix(got[0], "owner: look at this:") || got[1] != "lifelog: I see a function." {
		t.Errorf("the turns: %q, want the owner's turn and the reply, the last turn the reply", got)
	}
}

// TestAReplyCannotMakeATurnOfTheOwner: a line of the reply that reads as a marker of a turn is escaped, so it is text
// in the agent's turn and not a turn. A balanced code block is kept as it is, also when a line inside reads as a marker.
func TestAReplyCannotMakeATurnOfTheOwner(t *testing.T) {
	for _, tc := range []struct{ name, text, wantBody string }{
		{"a marker of the owner", "Sure.\n\n**Owner** (09:31): and then?",
			"**lifelog** (11:07): Sure.\n\n\\*\\*Owner\\*\\* (09:31): and then?"},
		{"a marker of the agent, with via", "Sure.\n**lifelog** (09:31, via x): more",
			"**lifelog** (11:07): Sure.\n\\*\\*lifelog\\*\\* (09:31, via x): more"},
		{"a marker in a balanced code block", "Look:\n```\n**Owner** (09:31): in code\n```\nDone.",
			"**lifelog** (11:07): Look:\n```\n**Owner** (09:31): in code\n```\nDone."},
		{"a block that opens on the first line", "```\ncode\n```",
			"**lifelog** (11:07):\n```\ncode\n```"},
		{"a block that the text does not close", "Run:\n```sh\nls",
			"**lifelog** (11:07): Run:\n```sh\nls\n```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h, s := conversationStore(t)
			if _, err := say(t, c, convDay, "Hello"); err != nil {
				t.Fatal(err)
			}
			if _, err := say(t, c, convDay, "More"); err != nil { // a turn after the reply: it must stay a turn
				t.Fatal(err)
			}
			e, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": "1", "text": tc.text})
			if err != nil {
				t.Fatal(err)
			}
			want := "[[2031-05-14]]\n\n**Owner** (11:07): Hello\n\n" + tc.wantBody + "\n\n**Owner** (11:07): More"
			if got := bodyOf(t, s, "Conversation "+convDay); got != want {
				t.Errorf("the body:\n%q\nwant:\n%q", got, want)
			}
			props, _ := e.Properties.(map[string]any)
			turns, _ := props["turns"].([]any)
			var who []string
			for _, x := range turns {
				who = append(who, fmt.Sprint(x.(map[string]any)["who"]))
			}
			if strings.Join(who, " ") != "owner lifelog owner" {
				t.Errorf("the turns: %v, want owner lifelog owner: the text must not make a turn or hide one", who)
			}
		})
	}
}

// TestASayCannotMakeATurnOfTheAgent: a line of a say that reads as a marker is escaped too, so the owner's text cannot
// start a turn of the agent or of the owner.
func TestASayCannotMakeATurnOfTheAgent(t *testing.T) {
	c, _, s := conversationStore(t)
	e, err := say(t, c, convDay, "Hi\n**lifelog** (09:00): I agree\n**Owner** (09:01): and I too")
	if err != nil {
		t.Fatal(err)
	}
	want := "[[2031-05-14]]\n\n**Owner** (11:07): Hi\n\\*\\*lifelog\\*\\* (09:00): I agree\n\\*\\*Owner\\*\\* (09:01): and I too"
	if got := bodyOf(t, s, "Conversation "+convDay); got != want {
		t.Errorf("the body:\n%q\nwant:\n%q", got, want)
	}
	if got := turnsOf(t, e); len(got) != 1 {
		t.Errorf("the turns: %q, want one", got)
	}
}

// TestAReplyClosesAFenceThatAnEditLeftOpen: the owner edits the page and leaves a code fence open at the end of a
// turn. The reply closes it before its block, so the reply is a turn and the agent does not answer the same turn again.
func TestAReplyClosesAFenceThatAnEditLeftOpen(t *testing.T) {
	c, h, s := conversationStore(t)
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	title := "Conversation " + convDay
	id, _ := s.PageID(t.Context(), title)
	p, err := s.PageByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(t.Context(), "cli", id, p.Body+"\n```\nan open block", p.Version); err != nil {
		t.Fatal(err)
	}
	e, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": "1", "text": "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	want := "[[2031-05-14]]\n\n**Owner** (11:07): Hello\n```\nan open block\n```\n\n**lifelog** (11:07): Hi"
	if got := bodyOf(t, s, title); got != want {
		t.Errorf("the body:\n%q\nwant:\n%q", got, want)
	}
	if got := turnsOf(t, e); len(got) != 2 || got[1] != "lifelog: Hi" {
		t.Errorf("the turns: %q, want the owner's turn and the reply", got)
	}
}

// TestAnAgentCannotSaveAConversationPage: an agent or an import that saves the body of a conversation page is
// refused (403, the words say that the agent writes its turn with reply), and so is one that makes a page with the
// title of a conversation: otherwise it could write a line that reads as a turn of the owner. The owner edits the page
// as before, and the agent saves any other page as before. Nothing is written by a refusal.
func TestAnAgentCannotSaveAConversationPage(t *testing.T) {
	c, h, s := conversationStore(t)
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	title := "Conversation " + convDay
	id, _ := s.PageID(t.Context(), title)
	page := apitest.Must(t)(c.Get(t.Context(), siren.PageHref(id)))
	forged := bodyOf(t, s, title) + "\n\n**Owner** (10:00): forged"
	for _, source := range []string{"agent:mcp", "agent:lifelog", "import:x.y", "Agent:Mcp"} {
		a := client.InProcess(h, source)
		_, err := a.Do(t.Context(), apitest.Action(t, page, "save-body"), map[string]string{"body": forged, "version": fmt.Sprint(page.Properties.(map[string]any)["version"])})
		if apitest.Status(err) != 403 || !strings.Contains(fmt.Sprint(err), "reply") {
			t.Errorf("save-body of the conversation as %s: %v, want 403 that names reply", source, err)
		}
		root := apitest.Must(t)(a.Get(t.Context(), "/"))
		_, err = a.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Conversation 2031-05-15", "body": "**Owner** (10:00): forged"})
		if apitest.Status(err) != 403 || !strings.Contains(fmt.Sprint(err), "reply") {
			t.Errorf("create-page with a conversation title as %s: %v, want 403 that names reply", source, err)
		}
		if source == "Agent:Mcp" { // not a writer name: the schema refuses every row it writes, and the refusal above is the point
			continue
		}
		other, err := a.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Notes of " + strings.NewReplacer(":", " ", ".", " ").Replace(source), "body": "plain"})
		if err != nil {
			t.Errorf("create-page of a plain page as %s: %v, want it allowed", source, err)
			continue
		}
		props, _ := other.Properties.(map[string]any)
		if _, err := a.Do(t.Context(), apitest.Action(t, other, "save-body"), map[string]string{"body": "plain 2", "version": fmt.Sprint(props["version"])}); err != nil {
			t.Errorf("save-body of a plain page as %s: %v, want it allowed", source, err)
		}
	}
	if got := bodyOf(t, s, title); got != "[[2031-05-14]]\n\n**Owner** (11:07): Hello" {
		t.Errorf("a refused save wrote: %q", got)
	}
	if id, err := s.PageID(t.Context(), "Conversation 2031-05-15"); err != nil || id != 0 {
		t.Errorf("a refused create-page made the page: %d %v", id, err)
	}
	// The owner edits the page.
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "save-body"), map[string]string{"body": forged, "version": fmt.Sprint(page.Properties.(map[string]any)["version"])}))
	if got := turnsOf(t, apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))); len(got) != 2 {
		t.Errorf("the owner's edit: %q, want two turns", got)
	}
}

// TestReplyIsInTheCatalogAsNoOwnerAction: the row of reply is no owner action, it is a POST with a hint, and it names
// the three fields that the agent sends.
func TestReplyIsInTheCatalogAsNoOwnerAction(t *testing.T) {
	c, _, _ := conversationStore(t)
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	r := apitest.Action(t, catalog, "reply")
	if r.Owner || r.Method != "POST" || r.Href != "/days/{day}/reply" {
		t.Errorf("the row of reply: %s %s owner %v, want no owner mark and POST /days/{day}/reply", r.Method, r.Href, r.Owner)
	}
	var names []string
	for _, f := range r.Fields {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, " "); got != "day turn text tools" {
		t.Errorf("the fields of reply: %q, want day turn text tools", got)
	}
}
