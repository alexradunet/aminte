package api_test

import (
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
	"lifelog/siren"
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

	if _, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": first, "text": "Done."}); err != nil {
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
	if _, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": keyOf(t, a, "owner", 0), "text": "RA"}); err != nil {
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
	e, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": keyB, "text": "RB"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(turnsOf(t, e), " | "), "owner: B | lifelog: RB | owner: C"; got != want {
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
	if _, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": keyOf(t, e, "owner", 1), "text": "R2"}); err != nil {
		t.Fatal(err)
	}
	if got, want := bodyOf(t, s, title), "[["+convDay+"]]\n\n**Owner** (11:07): ok\n\n**Owner** (11:07): ok\n\n**lifelog** (11:07): R2"; got != want {
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
	e, err := reply(t, h, "agent:lifelog", map[string]string{"day": "today", "turn": key, "text": "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := keyOf(t, e, "lifelog", 0)
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
		_, err := reply(t, h, "agent:lifelog", tc.v)
		if got := apitest.Status(err); got != tc.want {
			t.Errorf("%s: %v, want %d", tc.name, err, tc.want)
		}
		if got := bodyOf(t, s, title); got != before {
			t.Errorf("%s: the refused reply wrote:\n%q\nwant:\n%q", tc.name, got, before)
		}
	}
}
