package api_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"aminte/internal/api/apitest"
	"aminte/internal/core"
	"aminte/siren"
)

// turnMaps are the turns of the answer of a conversation as the JSON client reads them.
func turnMaps(t *testing.T, e *siren.Entity) []map[string]any {
	t.Helper()
	props, _ := e.Properties.(map[string]any)
	turns, _ := props["turns"].([]any)
	var out []map[string]any
	for _, x := range turns {
		m, _ := x.(map[string]any)
		out = append(out, m)
	}
	return out
}

// keyOf is the key of the n-th turn of who (counted from 0) in the answer of a conversation.
func keyOf(t *testing.T, e *siren.Entity, who string, n int) string {
	t.Helper()
	for _, m := range turnMaps(t, e) {
		if m["who"] != who {
			continue
		}
		if n == 0 {
			key, _ := m["key"].(string)
			if key == "" {
				t.Fatalf("the %s turn has no key: %v", who, m)
			}
			return key
		}
		n--
	}
	t.Fatalf("the conversation has no such %s turn", who)
	return ""
}

// editBody saves the body of the conversation page of convDay as the owner, as an edit in the browser does.
func editBody(t *testing.T, s *core.Store, edit func(body string) string) {
	t.Helper()
	id, err := s.PageID(t.Context(), "Conversation "+convDay)
	if err != nil || id == 0 {
		t.Fatalf("the conversation page: %d %v", id, err)
	}
	p, err := s.PageByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(t.Context(), "cli", id, edit(p.Body), p.Version); err != nil {
		t.Fatal(err)
	}
}

// TestEveryTurnHasAKeyThatNamesIt: each turn of the conversation has a key, 16 hex digits, and no two keys of a page
// are the same, also for two turns with the same words in the same minute. A reply that is written after a turn does
// not change the key of a turn; an edit of the words of a turn does.
func TestEveryTurnHasAKeyThatNamesIt(t *testing.T) {
	c, h, s := conversationStore(t)
	for _, words := range []string{"ok", "ok", "other"} {
		if _, err := say(t, c, convDay, words); err != nil {
			t.Fatal(err)
		}
	}
	e := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	first, second, third := keyOf(t, e, "owner", 0), keyOf(t, e, "owner", 1), keyOf(t, e, "owner", 2)

	if _, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": first, "text": "Done."}); err != nil {
		t.Fatal(err)
	}
	e = apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	seen := map[string]bool{}
	hex16 := regexp.MustCompile(`^[0-9a-f]{16}$`)
	for _, m := range turnMaps(t, e) {
		key, _ := m["key"].(string)
		if !hex16.MatchString(key) {
			t.Errorf("the key of %v: %q, want 16 hex digits", m, key)
		}
		if seen[key] {
			t.Errorf("the key %q is on two turns of one page: %v", key, turnMaps(t, e))
		}
		seen[key] = true
	}
	if len(seen) != 4 {
		t.Errorf("the page has %d keys, want 4 for 4 turns", len(seen))
	}
	if got := []string{keyOf(t, e, "owner", 0), keyOf(t, e, "owner", 1), keyOf(t, e, "owner", 2)}; got[0] != first || got[1] != second || got[2] != third {
		t.Errorf("the keys of the owner's turns after a reply: %v, want %v", got, []string{first, second, third})
	}

	editBody(t, s, func(body string) string { return strings.Replace(body, ": other", ": other, edited", 1) })
	e = apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	if got := keyOf(t, e, "owner", 2); got == third {
		t.Errorf("the key of a turn whose words were edited is the same: %q", got)
	}
	if got := keyOf(t, e, "owner", 0); got != first {
		t.Errorf("the key of a turn that was not edited changed: %q, want %q", got, first)
	}
}

// TestAReplyNamesItsTurnByItsKey: the reply goes right after the turn that its key names, also when an edit moved the
// turn or removed an earlier one while the agent worked. The turn that the owner said meanwhile stays after the reply
// and waits for its own. Of two turns with the same words in the same minute, the key names one.
func TestAReplyNamesItsTurnByItsKey(t *testing.T) {
	c, h, s := conversationStore(t)
	title := "Conversation " + convDay
	a, err := say(t, c, convDay, "A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": keyOf(t, a, "owner", 0), "text": "RA"}); err != nil {
		t.Fatal(err)
	}
	b, err := say(t, c, convDay, "B") // the agent reads the page and works on B
	if err != nil {
		t.Fatal(err)
	}
	keyB := keyOf(t, b, "owner", 1)
	editBody(t, s, func(string) string { return "[[" + convDay + "]]\n\n**Owner** (11:07): B" }) // the owner removes the first exchange
	if _, err := say(t, c, convDay, "C"); err != nil {
		t.Fatal(err)
	}
	e, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": keyB, "text": "RB"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(turnsOf(t, e), " | "), "owner: B | aminte: RB | owner: C"; got != want {
		t.Errorf("the turns after the reply to B: %q, want %q: C waits for its own reply", got, want)
	}

	// Two turns with the same words in the same minute have two keys; the reply goes after the one that is named.
	c, h, s = conversationStore(t)
	for range 2 {
		if _, err := say(t, c, convDay, "ok"); err != nil {
			t.Fatal(err)
		}
	}
	e = apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	if _, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": keyOf(t, e, "owner", 1), "text": "R2"}); err != nil {
		t.Fatal(err)
	}
	if got, want := bodyOf(t, s, title), "[["+convDay+"]]\n\n**Owner** (11:07): ok\n\n**Owner** (11:07): ok\n\n**aminte** (11:07): R2"; got != want {
		t.Errorf("the body after the reply to the second turn:\n%q\nwant:\n%q", got, want)
	}
}

// TestAReplyWithAKeyThatIsNotOnThePageIsRefused: a key that no owner turn of the page has now is 409, so the agent
// reads the page again: the key of a turn whose words were edited, a key that never was one, the number of a turn (the
// old form), and the key of a turn of the agent. A missing key is 422, and a day with no page is 404. A refusal
// writes nothing.
func TestAReplyWithAKeyThatIsNotOnThePageIsRefused(t *testing.T) {
	c, h, s := conversationStore(t)
	title := "Conversation " + convDay
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	e := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	key := keyOf(t, e, "owner", 0)
	e, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": key, "text": "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := keyOf(t, e, "aminte", 0)
	editBody(t, s, func(body string) string { return strings.Replace(body, ": Hello", ": Hello again", 1) })
	before := bodyOf(t, s, title)
	edited := keyOf(t, apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation")), "owner", 0)
	for _, tc := range []struct {
		name string
		v    map[string]string
		want int
	}{
		{"the key of a turn whose words were edited", map[string]string{"day": "today", "turn": key, "text": "Hi"}, 409},
		{"a key that never was one", map[string]string{"day": "today", "turn": "0123456789abcdef", "text": "Hi"}, 409},
		{"the number of a turn", map[string]string{"day": "today", "turn": "1", "text": "Hi"}, 409},
		{"the key of a turn of the agent", map[string]string{"day": "today", "turn": agentKey, "text": "Hi"}, 409},
		{"the key of a turn that has a reply", map[string]string{"day": "today", "turn": edited, "text": "Hi"}, 409},
		{"no key", map[string]string{"day": "today", "text": "Hi"}, 422},
		{"a key of blanks", map[string]string{"day": "today", "turn": "  ", "text": "Hi"}, 422},
		{"a day with no conversation", map[string]string{"day": "2031-05-10", "turn": key, "text": "Hi"}, 404},
	} {
		_, err := reply(t, h, "agent:aminte", tc.v)
		if got := apitest.Status(err); got != tc.want {
			t.Errorf("%s: %v, want %d", tc.name, err, tc.want)
		}
		if got := bodyOf(t, s, title); got != before {
			t.Errorf("%s: the refused reply wrote:\n%q\nwant:\n%q", tc.name, got, before)
		}
	}
}

// TestAListOfCodeLinesShowsAsAListInTheTurn: the owner says, and the agent replies, a list of lines that start with
// a dash, a space and a backtick. The page keeps a backslash before each (it is no tool line there), and the turns
// give the words as they were said: a client that draws them shows a list, and a relay that sends them sends "- `x`".
// The key is that of the words as said, and it does not move when the reply is written.
func TestAListOfCodeLinesShowsAsAListInTheTurn(t *testing.T) {
	c, h, s := conversationStore(t)
	if _, err := say(t, c, convDay, "Files:\n- `main.go`\n- `util.go`"); err != nil {
		t.Fatal(err)
	}
	key := ownerKeyOf(t, c, 1)
	e, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": key, "text": "Found:\n- `a.go` is here\n- `b.go`", "tools": "- `search`: 2 hits"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(turnsOf(t, e), " | "), "owner: Files:\n- `main.go`\n- `util.go` | aminte: Found:\n- `a.go` is here\n- `b.go`"; got != want {
		t.Errorf("the turns:\n%q\nwant:\n%q", got, want)
	}
	if got := keyOf(t, e, "owner", 0); got != key {
		t.Errorf("the key of the owner's turn moved from %s to %s when the reply was written", key, got)
	}
	want := "[[2031-05-14]]\n\n**Owner** (11:07): Files:\n\\- `main.go`\n\\- `util.go`\n\n- `search`: 2 hits\n\n**aminte** (11:07): Found:\n\\- `a.go` is here\n\\- `b.go`"
	if got := bodyOf(t, s, "Conversation "+convDay); got != want {
		t.Errorf("the page:\n%q\nwant:\n%q", got, want)
	}
	if tools := turnMaps(t, e)[1]["tools"]; fmt.Sprint(tools) != "[- `search`: 2 hits]" {
		t.Errorf("the tool lines of the reply: %v, want the one line that the agent sent", tools)
	}
}

// TestAReplyKeepsTheWordsAndTheKeyOfTheTurnItAnswers: an edit of the owner can end the turn in a line that starts with
// a dash, a space and a backtick, or leave a code fence open. The reply is written after the turn, with tool lines or
// without. The turn keeps its words and its key (the reply escapes such a last line and the page is read with the
// fence closed), the reply keeps its own tool lines only, and the relay does not send the turn a second time. The same
// holds for a turn that is not the last.
func TestAReplyKeepsTheWordsAndTheKeyOfTheTurnItAnswers(t *testing.T) {
	for _, edit := range []struct{ name, add string }{
		{"a last line that starts like a tool line", "\n- `ls -la`"},
		{"a last line with an arrow", "\n- `rm` → /pages/1"},
		{"a tool line after a blank line", "\n\n- `ls -la`"},
		{"two tool lines and a blank edge", "\n- `ls`\n- `rm`\n\n\n"},
		{"a fence that is left open", "\n```\ncode"},
		{"a tool line in a fence that is left open", "\n```\n- `ls`"},
		{"a block, then a tool line", "\n```\nx\n```\n- `ls`"},
	} {
		for _, tools := range []string{"", "- `search`: no hits"} {
			for _, later := range []bool{false, true} {
				name := fmt.Sprintf("%s, tools %q, a later turn %v", edit.name, tools, later)
				t.Run(name, func(t *testing.T) {
					c, h, s := conversationStore(t)
					if _, err := say(t, c, convDay, "Run this"); err != nil {
						t.Fatal(err)
					}
					editBody(t, s, func(b string) string { return b + edit.add })
					if later {
						if _, err := say(t, c, convDay, "And that"); err != nil {
							t.Fatal(err)
						}
					}
					read := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
					key, words := keyOf(t, read, "owner", 0), turnMaps(t, read)[0]["text"]
					e, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": key, "text": "Done.", "tools": tools})
					if err != nil {
						t.Fatal(err)
					}
					got := turnMaps(t, e)
					if k := keyOf(t, e, "owner", 0); k != key {
						t.Errorf("the reply moved the key of the turn it answers from %s to %s", key, k)
					}
					if got[0]["text"] != words {
						t.Errorf("the reply changed the words of the turn it answers:\n%q\nwas:\n%q", got[0]["text"], words)
					}
					if got[0]["tools"] != nil {
						t.Errorf("the turn of the owner has tool lines: %v", got[0]["tools"])
					}
					wantTools := "<nil>"
					if tools != "" {
						wantTools = "[" + tools + "]"
					}
					if got[1]["who"] != "aminte" || got[1]["text"] != "Done." || fmt.Sprint(got[1]["tools"]) != wantTools {
						t.Errorf("the reply: %v, want aminte, Done. and the tools %s", got[1], wantTools)
					}
					if later && (len(got) != 3 || got[2]["text"] != "And that") {
						t.Errorf("the turns: %v, want the later turn after the reply", got)
					}
				})
			}
		}
	}
}

// TestASayKeepsTheKeyOfTheTurnBeforeItWhenAnEditLeftAFenceOpen: the say closes the fence that an edit left open at the
// end of the page, so the turn before it has the words that it had: the same key, and no second send by a relay.
func TestASayKeepsTheKeyOfTheTurnBeforeItWhenAnEditLeftAFenceOpen(t *testing.T) {
	c, _, s := conversationStore(t)
	if _, err := say(t, c, convDay, "Look"); err != nil {
		t.Fatal(err)
	}
	editBody(t, s, func(b string) string { return b + "\n```\ncode" })
	read := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	key, words := keyOf(t, read, "owner", 0), turnMaps(t, read)[0]["text"]
	e, err := say(t, c, convDay, "More")
	if err != nil {
		t.Fatal(err)
	}
	if k := keyOf(t, e, "owner", 0); k != key || turnMaps(t, e)[0]["text"] != words {
		t.Errorf("the say changed the turn before it: key %s then %s, words %q then %q", key, k, words, turnMaps(t, e)[0]["text"])
	}
}

// TestAnEditThatRemovesAnEqualTurnGivesItsKeyToTheLaterOne: a key is made from the who, the minute, the relay and the
// words of a turn, and from the number of turns before it that have the same four. When the owner removes the first of
// two equal turns, the second becomes the first of its kind and has the key that the removed turn had. A reply with
// the key that the agent read for the second turn is 409, and the agent reads the page again. A removed turn that was
// different moves no key.
func TestAnEditThatRemovesAnEqualTurnGivesItsKeyToTheLaterOne(t *testing.T) {
	c, h, s := conversationStore(t)
	if _, err := say(t, c, convDay, "ok"); err != nil {
		t.Fatal(err)
	}
	first := ownerKeyOf(t, c, 1)
	if _, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": first, "text": "R1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := say(t, c, convDay, "ok"); err != nil {
		t.Fatal(err)
	}
	second := ownerKeyOf(t, c, 2)
	if first == second {
		t.Fatalf("two equal turns have one key: %s", first)
	}
	editBody(t, s, func(string) string { return "[[" + convDay + "]]\n\n**Owner** (11:07): ok" })
	if got := ownerKeyOf(t, c, 1); got != first {
		t.Errorf("the turn that stays has the key %s, want the key %s of the turn that was removed", got, first)
	}
	_, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": second, "text": "R2"})
	if apitest.Status(err) != 409 {
		t.Errorf("a reply with the key that the second turn had: %v, want 409", err)
	}
	if _, err := reply(t, h, "agent:aminte", map[string]string{"day": "today", "turn": first, "text": "R2"}); err != nil {
		t.Errorf("a reply with the key that the agent reads now: %v, want it written", err)
	}

	// A removed turn that was different moves no key.
	c, _, s = conversationStore(t)
	for _, words := range []string{"one", "ok", "ok"} {
		if _, err := say(t, c, convDay, words); err != nil {
			t.Fatal(err)
		}
	}
	before := []string{ownerKeyOf(t, c, 2), ownerKeyOf(t, c, 3)}
	editBody(t, s, func(b string) string { return strings.Replace(b, "\n\n**Owner** (11:07): one", "", 1) })
	if got := []string{ownerKeyOf(t, c, 1), ownerKeyOf(t, c, 2)}; got[0] != before[0] || got[1] != before[1] {
		t.Errorf("the keys after the removal of a different turn: %v, want %v", got, before)
	}
}
