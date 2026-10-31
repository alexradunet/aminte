package api_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
	"lifelog/siren"
)

const convDay = "2031-05-14"

// conversationStore is a store whose clock stands at 09:07 UTC on convDay, and whose zone is UTC+2, so the stamp of a
// turn is 11.07.
func conversationStore(t *testing.T) (*client.Client, http.Handler, *core.Store) {
	t.Helper()
	at := time.Date(2031, 5, 14, 9, 7, 0, 0, time.UTC)
	s := &core.Store{DB: dbtest.New(t), Clock: func() time.Time { return at }, Zone: time.FixedZone("test", 2*3600)}
	h := api.New(s)
	return client.InProcess(h, "cli"), h, s
}

func say(t *testing.T, c *client.Client, day, words string) (*siren.Entity, error) {
	t.Helper()
	conv := apitest.Must(t)(c.Get(t.Context(), "/days/"+day+"/conversation"))
	return c.Do(t.Context(), apitest.Action(t, conv, "say"), map[string]string{"text": words})
}

func bodyOf(t *testing.T, s *core.Store, title string) string {
	t.Helper()
	id, err := s.PageID(t.Context(), title)
	if err != nil || id == 0 {
		t.Fatalf("the page %s: %d %v", title, id, err)
	}
	p, err := s.PageByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Body
}

// TestSayMakesTheConversationPageAndAppendsTurns: the first turn makes the page Conversation DAY in the category
// Conversations with the first line [[DAY]]; a second turn follows after a blank line; the stamp is the time in the
// zone of the store; a browser's CRLF becomes LF; today is the store's day.
func TestSayMakesTheConversationPageAndAppendsTurns(t *testing.T) {
	c, _, s := conversationStore(t)
	e, err := say(t, c, "today", "How did I sleep?")
	if err != nil {
		t.Fatal(err)
	}
	title := "Conversation " + convDay
	want := "[[" + convDay + "]]\n\n**Owner** (11:07): How did I sleep?"
	if got := bodyOf(t, s, title); got != want {
		t.Errorf("the body after one turn: %q, want %q", got, want)
	}
	if e.Title != title || strings.Join(e.Class, " ") != "conversation page" {
		t.Errorf("the answer: %q %v, want the conversation of the day, with its page", e.Title, e.Class)
	}
	if sync, _ := e.Result.(map[string]any); sync["linked"] == nil || !strings.Contains(fmt.Sprint(sync["linked"]), convDay) {
		t.Errorf("the result: %v, want the sync of a body save, linking the day", e.Result)
	}
	if _, err := say(t, c, convDay, "Line one\r\nline two\r\n"); err != nil {
		t.Fatal(err)
	}
	want += "\n\n**Owner** (11:07): Line one\nline two"
	if got := bodyOf(t, s, title); got != want {
		t.Errorf("the body after two turns: %q, want %q", got, want)
	}
	id, _ := s.PageID(t.Context(), title)
	p, err := s.PageByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Day != convDay {
		t.Errorf("the page is of the day %q, want %s", p.Day, convDay)
	}
	filed := false
	for _, l := range p.Out {
		filed = filed || l.Kind == "part-of" && l.Title == "Conversations"
	}
	if !filed {
		t.Errorf("the page is not filed in Conversations: %v", p.Out)
	}
}

// TestSayIsTheOwnersAndViaNamesTheRelay: an agent or an import cannot speak as the owner (403, and no page). The
// owner's say names a relay with via: the paragraph then reads (HH:MM, via NAME). Without via it reads (HH:MM). A via
// that is not a writer name is 422.
func TestSayIsTheOwnersAndViaNamesTheRelay(t *testing.T) {
	doSay := func(h http.Handler, source string, v map[string]string) error {
		t.Helper()
		c := client.InProcess(h, source)
		conv := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
		_, err := c.Do(t.Context(), apitest.Action(t, conv, "say"), v)
		return err
	}
	for _, source := range []string{"agent:x", "import:y"} {
		t.Run(source, func(t *testing.T) {
			_, h, s := conversationStore(t)
			if err := doSay(h, source, map[string]string{"text": "Hi"}); apitest.Status(err) != 403 {
				t.Errorf("say as %s: %v, want 403", source, err)
			}
			if id, err := s.PageID(t.Context(), "Conversation "+convDay); err != nil || id != 0 {
				t.Errorf("a refused say made the page: %d %v", id, err)
			}
		})
	}
	for _, tc := range []struct{ via, marker string }{
		{"phone", "**Owner** (11:07, via phone): Hi"},
		{"a:b.c", "**Owner** (11:07, via a:b.c): Hi"},
		{"", "**Owner** (11:07): Hi"},
	} {
		t.Run("via "+tc.via, func(t *testing.T) {
			_, h, s := conversationStore(t)
			c := client.InProcess(h, "cli")
			conv := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
			v := map[string]string{"text": "Hi"}
			if tc.via != "" {
				v["via"] = tc.via
			}
			e, err := c.Do(t.Context(), apitest.Action(t, conv, "say"), v)
			if err != nil {
				t.Fatal(err)
			}
			if want := "[[" + convDay + "]]\n\n" + tc.marker; bodyOf(t, s, "Conversation "+convDay) != want {
				t.Errorf("the body: %q, want %q", bodyOf(t, s, "Conversation "+convDay), want)
			}
			props, _ := e.Properties.(map[string]any)
			turns, _ := props["turns"].([]any)
			if len(turns) != 1 {
				t.Fatalf("the turns: %v, want one", turns)
			}
			turn, _ := turns[0].(map[string]any)
			via, has := turn["via"]
			if tc.via == "" && has || tc.via != "" && via != tc.via {
				t.Errorf("the turn: %v, want via %q", turn, tc.via)
			}
		})
	}
	_, h, s := conversationStore(t)
	for _, bad := range []string{"Bad Name", "UPPER", strings.Repeat("a", 65)} {
		if err := doSay(h, "cli", map[string]string{"text": "Hi", "via": bad}); apitest.Status(err) != 422 {
			t.Errorf("say with via %q: %v, want 422", bad, err)
		}
	}
	if id, err := s.PageID(t.Context(), "Conversation "+convDay); err != nil || id != 0 {
		t.Errorf("a refused say made the page: %d %v", id, err)
	}
}

// TestSayRefusesWhatItCannotWrite: an empty text, a text of blanks and a day that is not a day are 422 and write
// nothing, not even the page.
func TestSayRefusesWhatItCannotWrite(t *testing.T) {
	c, _, s := conversationStore(t)
	conv := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	say := apitest.Action(t, conv, "say")
	for _, words := range []string{"", "  \r\n ", "\n"} {
		if _, err := c.Do(t.Context(), say, map[string]string{"text": words}); apitest.Status(err) != 422 {
			t.Errorf("say %q: %v, want 422", words, err)
		}
	}
	if _, err := c.Get(t.Context(), "/days/tomorrow/conversation"); apitest.Status(err) != 422 {
		t.Errorf("GET /days/tomorrow/conversation: %v, want 422", err)
	}
	bad := say
	bad.Href = "/days/not-a-day/say"
	if _, err := c.Do(t.Context(), bad, map[string]string{"text": "Hello"}); apitest.Status(err) != 422 {
		t.Errorf("say on a day that is not one: %v, want 422", err)
	}
	if id, err := s.PageID(t.Context(), "Conversation "+convDay); err != nil || id != 0 {
		t.Errorf("a refused say made the page: %d %v", id, err)
	}
	if id, err := s.PageID(t.Context(), "Conversation not-a-day"); err != nil || id != 0 {
		t.Errorf("a refused say made a page for a day that is not one: %d %v", id, err)
	}
}

// TestTheConversationOfADay: a day with no page has no turns and offers say alone; a day with a page carries the
// turns, the page's properties, links and actions, and say with its day filled; a tombstoned page is no conversation,
// and a say revives it.
func TestTheConversationOfADay(t *testing.T) {
	c, _, s := conversationStore(t)
	get := func() *siren.Entity { return apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation")) }
	e := get()
	props, _ := e.Properties.(map[string]any)
	if turns, ok := props["turns"].([]any); !ok || len(turns) != 0 || props["day"] != convDay {
		t.Errorf("a day with no talk: %v, want day and an empty turns", props)
	}
	if strings.Join(e.Class, " ") != "conversation" || len(e.Actions) != 1 || e.Actions[0].Name != "say" || e.Actions[0].Href != "/days/"+convDay+"/say" {
		t.Errorf("a day with no talk: %v %v, want the class conversation and say alone", e.Class, e.Actions)
	}
	if apitest.Href(e, "self") != "/days/"+convDay+"/conversation" {
		t.Errorf("self: %q", apitest.Href(e, "self"))
	}
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	// An agent's turn, as an extension writes it: the tool lines, then the paragraph.
	id, _ := s.PageID(t.Context(), "Conversation "+convDay)
	p, err := s.PageByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(t.Context(), "agent:test", id, p.Body+"\n\n- `search`\n- `get`: no hits\n\n**lifelog** (11:08): Hello.", p.Version); err != nil {
		t.Fatal(err)
	}
	e = get()
	props, _ = e.Properties.(map[string]any)
	turns, _ := props["turns"].([]any)
	if len(turns) != 2 {
		t.Fatalf("the turns: %v, want two", turns)
	}
	reply, _ := turns[1].(map[string]any)
	if reply["who"] != "lifelog" || reply["at"] != "11:08" || reply["text"] != "Hello." || fmt.Sprint(reply["tools"]) != "[- `search` - `get`: no hits]" {
		t.Errorf("the reply: %v", reply)
	}
	if props["id"] == nil || props["version"] == nil || props["title"] != "Conversation "+convDay || props["body"] == nil {
		t.Errorf("the properties do not carry the page: %v", props)
	}
	if strings.Join(e.Class, " ") != "conversation page" || apitest.Href(e, "page") != siren.PageHref(id) {
		t.Errorf("the class %v and the link page %q", e.Class, apitest.Href(e, "page"))
	}
	selves := 0
	for _, l := range e.Links {
		for _, r := range l.Rel {
			if r == "self" {
				selves++
			}
		}
	}
	if selves != 1 || e.Actions[0].Name != "say" {
		t.Errorf("%d self links; the first action %q, want one and say", selves, e.Actions[0].Name)
	}
	apitest.Action(t, e, "save-body")
	// Tombstone the page: it is no conversation, and a say brings it back with its text.
	if err := s.Tombstone(t.Context(), "cli", id); err != nil {
		t.Fatal(err)
	}
	if e = get(); strings.Join(e.Class, " ") != "conversation" || len(e.Actions) != 1 {
		t.Errorf("a tombstoned page: %v %v, want no page", e.Class, e.Actions)
	}
	if _, err := say(t, c, convDay, "Back"); err != nil {
		t.Fatal(err)
	}
	if turns, _ := get().Properties.(map[string]any)["turns"].([]any); len(turns) != 3 {
		t.Errorf("after the say that revived the page: %v, want three turns", turns)
	}
}

// TestAConversationPageLinksTheViewThatCarriesIt: the JSON of the page of a conversation is the page's, and it links
// (rel conversation) to the view of its day that carries it; the view does not link to itself; a page titled
// Conversation notes has no such link, and a page that was deleted is a page. A UI that shows the view and not the page
// (the browser extension) follows this link.
func TestAConversationPageLinksTheViewThatCarriesIt(t *testing.T) {
	c, _, s := conversationStore(t)
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.PageID(t.Context(), "Conversation "+convDay)
	self := siren.PageHref(id)
	if e := apitest.Must(t)(c.Get(t.Context(), self)); strings.Join(e.Class, " ") != "page" || apitest.Href(e, "self") != self {
		t.Errorf("the JSON of the page: %v %q, want the page", e.Class, apitest.Href(e, "self"))
	}
	if got, want := apitest.Href(apitest.Must(t)(c.Get(t.Context(), self)), "conversation"), "/days/"+convDay+"/conversation"; got != want {
		t.Errorf("the link conversation of the page: %q, want %q", got, want)
	}
	if got := apitest.Href(apitest.Must(t)(c.Get(t.Context(), "/days/"+convDay+"/conversation")), "conversation"); got != "" {
		t.Errorf("the conversation links to itself: %q", got)
	}
	// A page that is not a conversation, and one with a title that is not of a day, have no such link.
	other := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Conversation notes"}))
	if got := apitest.Href(apitest.Must(t)(c.Get(t.Context(), apitest.Href(other, "self"))), "conversation"); got != "" {
		t.Errorf("a page titled Conversation notes links to a conversation: %q", got)
	}
	// A page of a conversation that is deleted stays a page, and it is a plain GET: the core redirects nothing.
	if err := s.Tombstone(t.Context(), "cli", id); err != nil {
		t.Fatal(err)
	}
	if e := apitest.Must(t)(c.Get(t.Context(), self)); !slices.Contains(e.Class, "deleted") {
		t.Errorf("a tombstoned conversation page is %v, want a deleted page", e.Class)
	}
}

// TestSayIsForToday: say on a day that is not the local today is 422 with its reason and writes nothing; the
// conversation of another day offers no say; today, and the word today, work.
func TestSayIsForToday(t *testing.T) {
	c, _, s := conversationStore(t)
	today := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation")), "say")
	for _, day := range []string{"2031-05-13", "2031-05-15"} {
		e := apitest.Must(t)(c.Get(t.Context(), "/days/"+day+"/conversation"))
		for _, a := range e.Actions {
			if a.Name == "say" {
				t.Errorf("the conversation of %s offers say", day)
			}
		}
		past := today
		past.Href = "/days/" + day + "/say"
		_, err := c.Do(t.Context(), past, map[string]string{"text": "Hello"})
		if apitest.Status(err) != 422 || !strings.Contains(fmt.Sprint(err), "say is for today: the agent answers today's conversation") {
			t.Errorf("say on %s: %v, want 422 with its reason", day, err)
		}
		if id, err := s.PageID(t.Context(), "Conversation "+day); err != nil || id != 0 {
			t.Errorf("a refused say made the page of %s: %d %v", day, id, err)
		}
	}
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Errorf("say on today's date: %v", err)
	}
	if _, err := say(t, c, "today", "Hello again"); err != nil {
		t.Errorf("say on today: %v", err)
	}
}

// TestAConversationPageKeepsItsTitle: a live conversation page offers no rename and no promote; rename of it, and
// rename of a plain page to such a title, is 409 and writes nothing; a plain page is renamed as before.
func TestAConversationPageKeepsItsTitle(t *testing.T) {
	c, _, s := conversationStore(t)
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatal(err)
	}
	title := "Conversation " + convDay
	id, _ := s.PageID(t.Context(), title)
	page := apitest.Must(t)(c.Get(t.Context(), siren.PageHref(id)))
	for _, a := range page.Actions {
		if a.Name == "rename" || a.Name == "promote" {
			t.Errorf("a conversation page offers %s", a.Name)
		}
	}
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	plain := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Plain notes"}))
	rename := apitest.Action(t, plain, "rename")
	refused := func(href, to string) {
		t.Helper()
		a := rename
		a.Href = href
		_, err := c.Do(t.Context(), a, map[string]string{"title": to})
		if apitest.Status(err) != 409 || !strings.Contains(fmt.Sprint(err), "a conversation page keeps its title: its day names it") {
			t.Errorf("rename to %q at %s: %v, want 409 with its reason", to, href, err)
		}
	}
	refused(siren.PageHref(id)+"/rename", "Something else")
	refused(rename.Href, "Conversation 2031-05-15")
	if got, _ := s.PageID(t.Context(), title); got != id {
		t.Errorf("the conversation page: %d, want %d", got, id)
	}
	if got, _ := s.PageID(t.Context(), "Plain notes"); got == 0 {
		t.Error("a refused rename changed the plain page")
	}
	if got, _ := s.PageID(t.Context(), "Conversation 2031-05-15"); got != 0 {
		t.Error("a refused rename made the title")
	}
	renamed := apitest.Must(t)(c.Do(t.Context(), rename, map[string]string{"title": "Plain notes 2"}))
	if renamed.Title != "Plain notes 2" {
		t.Errorf("a plain page renamed: %q", renamed.Title)
	}
}

// TestSayRevivesADeletedConversationsCategory: a category page Conversations that was deleted (the owner's file
// keeps one from an earlier design) does not refuse the first say of a day: the write revives it, as it revives a
// deleted conversation page, so the page is filed in it.
func TestSayRevivesADeletedConversationsCategory(t *testing.T) {
	c, _, s := conversationStore(t)
	id, _, err := s.CreatePage(t.Context(), "cli", "Conversations", "The old category.", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(t.Context(), "cli", id); err != nil {
		t.Fatal(err)
	}
	if _, err := say(t, c, convDay, "Hello"); err != nil {
		t.Fatalf("the first say with a deleted category: %v", err)
	}
	cat, err := s.PageByID(t.Context(), id)
	if err != nil || cat.DeletedAt != "" {
		t.Errorf("the category after the say: %v, deleted at %q; want it live", err, cat.DeletedAt)
	}
}

// TestCaptureStampsTheHourAsSayDoes: a capture begins with the brackets of say, (HH:MM) in the zone of the store,
// and (HH:MM, via NAME) when an agent or an import relayed it, so a day page says when each entry was added, from
// every surface alike. A browser's CRLF becomes LF. A capture of a mood alone adds no text and no stamp.
func TestCaptureStampsTheHourAsSayDoes(t *testing.T) {
	_, h, s := conversationStore(t)
	capture := func(source string, v map[string]string) {
		t.Helper()
		c := client.InProcess(h, source)
		home := apitest.Must(t)(c.Get(t.Context(), "/"))
		if _, err := c.Do(t.Context(), apitest.Action(t, home, "capture"), v); err != nil {
			t.Fatal(err)
		}
	}
	capture("cli", map[string]string{"text": "Ran 5k with [[Sam]]."})
	capture("agent:whatsapp", map[string]string{"text": " Slept well.\r\nA second line.\r\n"})
	capture("import:y.z", map[string]string{"mood": "4"})
	want := "(11:07) Ran 5k with [[Sam]].\n\n(11:07, via whatsapp) Slept well.\nA second line."
	if got := bodyOf(t, s, convDay); got != want {
		t.Errorf("the day page:\n%q\nwant:\n%q", got, want)
	}
}
