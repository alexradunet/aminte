package view

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"lifelog/siren"
)

// rootOf is the root of the API as a client reads it with ?embed=today,conversation: JSON, decoded into an entity.
func rootOf(t *testing.T, body string) *siren.Entity {
	t.Helper()
	e := &siren.Entity{}
	if err := json.Unmarshal([]byte(body), e); err != nil {
		t.Fatal(err)
	}
	return e
}

const rootJSON = `{"class":["root"],"title":"Lifelog",
 "properties":{"today":"2031-05-14","zone":"+14","utc_offset":50400,"recent":[]},
 "entities":[
  {"rel":["today"],"href":"/days/2031-05-14","title":"2031-05-14","class":["day"],
   "properties":{"day":"2031-05-14","mood":[4],"streak":3,"written":true,
     "habits":[{"name":"Smoke","done":false},{"name":"Stretch","done":null},{"name":"Walk","done":true}]}},
  {"rel":["conversation"],"href":"/days/2031-05-14/conversation","title":"Conversation 2031-05-14","class":["conversation","page"],
   "properties":{"day":"2031-05-14","version":"7","turns":[
     {"who":"owner","at":"09:00","via":"whatsapp","text":"Hello"},
     {"who":"lifelog","at":"09:01","text":"Hi","tools":["- ` + "`read_day`" + ` → /days/2031-05-14"],"refs":["/days/2031-05-14"]}]},
   "actions":[{"name":"say","method":"POST","href":"/days/2031-05-14/say","fields":[{"name":"text","type":"textarea"}]}]}],
 "links":[{"rel":["self"],"href":"/"},{"rel":["app"],"href":"/with/notes","title":"Notes"},{"rel":["app"],"href":"/with/maps","title":"maps"},{"rel":["tasks"],"href":"/tasks","title":"Tasks"}]}`

// TestTheShellIsReadFromTheRoot: the day of today and its zone, the strip of the embedded day, the column of the
// embedded conversation, the apps and the task service. The column posts to the word today and not to the date.
func TestTheShellIsReadFromTheRoot(t *testing.T) {
	shell, err := ShellOf(rootOf(t, rootJSON))
	if err != nil {
		t.Fatal(err)
	}
	if _, offset := time.Date(2031, 5, 14, 12, 0, 0, 0, time.UTC).In(shell.Zone).Zone(); shell.Today != "2031-05-14" || offset != 14*3600 {
		t.Errorf("today %q, offset %d; want 2031-05-14 and the 14 hours of the name +14, which the zone database lacks", shell.Today, offset)
	}
	s := shell.Strip
	if s == nil || !s.Written || !s.Mood || s.Streak != 3 || len(s.Habits) != 3 {
		t.Fatalf("the strip: %+v", s)
	}
	for i, want := range []struct{ name, state string }{{"Smoke", "no"}, {"Stretch", "open"}, {"Walk", "done"}} {
		if got := s.Habits[i]; got.Name != want.name || got.State() != want.state {
			t.Errorf("habit %d is %s %s, want %s %s", i, got.Name, got.State(), want.name, want.state)
		}
	}
	c := shell.Chat
	if c == nil || c.Version != "7" || len(c.Turns) != 2 || c.Turns[0].Via != "whatsapp" || c.Turns[1].Tools[0] != "- `read_day` → /days/2031-05-14" {
		t.Fatalf("the chat: %+v", c)
	}
	if c.Say.Name != "say" || c.Say.Href != "/days/today/say" || len(c.Say.Fields) != 1 {
		t.Errorf("the action say of the column: %+v, want it posted to /days/today/say", c.Say)
	}
	if !slices.Equal(shell.Apps, []AppLink{{"notes", "Notes"}, {"maps", "maps"}}) || !shell.Tasks {
		t.Errorf("apps %v, tasks %v", shell.Apps, shell.Tasks)
	}
}

// TestTheShellKeepsWhatWasNotEmbedded: a root whose today and conversation are references gives a shell with no strip
// and no column; a root with no day as today gives no shell; a zone that the database has is that zone, and a name
// that is empty or Local is the zone of this machine.
func TestTheShellKeepsWhatWasNotEmbedded(t *testing.T) {
	bare := `{"class":["root"],"title":"Lifelog","properties":{"today":"2031-05-14","zone":"UTC","utc_offset":0},
	 "entities":[{"rel":["today"],"href":"/days/2031-05-14"},{"rel":["conversation"],"href":"/days/2031-05-14/conversation"}]}`
	shell, err := ShellOf(rootOf(t, bare))
	if err != nil || shell.Strip != nil || shell.Chat != nil || shell.Zone != time.UTC || shell.Tasks || len(shell.Apps) != 0 {
		t.Errorf("a root with references: %+v, %v", shell, err)
	}
	for _, body := range []string{`{"class":["root"],"properties":{}}`, `{"class":["root"],"properties":{"today":"soon"}}`, `{"class":["root"]}`} {
		if shell, err := ShellOf(rootOf(t, body)); err == nil || shell != nil {
			t.Errorf("%s: got a shell %v", body, shell)
		}
	}
	for _, name := range []string{"", "Local"} {
		if zone := zoneOf(name, 3600); zone != time.Local {
			t.Errorf("the zone %q is %v, want the zone of this machine", name, zone)
		}
	}
}

// TestTheCardsAreTheEmbeddedRefs: a reference that was not embedded has no card, and a sub-entity of another rel is
// not a card.
func TestTheCardsAreTheEmbeddedRefs(t *testing.T) {
	link := func(rel, href string, embedded bool) siren.Link {
		l := siren.Link{Rel: []string{rel}, Href: href}
		if embedded {
			l = l.Embedding(&siren.Entity{Class: []string{"day"}, Title: href, Properties: map[string]any{"day": href}})
		}
		return l
	}
	links := []siren.Link{link("ref", "/a", true), link("item", "/x", true), link("ref", "/b", false), link("ref", "/c", true), link("ref", "/d", true)}
	cards := CardsOf(links)
	var keys []string
	for k := range cards {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"/a", "/c", "/d"}) {
		t.Errorf("the cards: %v", keys)
	}
	if card := cards["/a"]; card == nil || card.Title != "/a" || card.Class[0] != "day" {
		t.Errorf("the card of /a: %+v", card)
	}
	day := []siren.Link{{Rel: []string{"earlier"}, Href: "/days/2031-05-12/conversation", Title: "2031-05-12"}, {Rel: []string{"day"}, Title: "no"},
		{Rel: []string{"earlier"}, Href: "/days/2031-05-13/conversation", Title: "2031-05-13"}}
	if got := ChatDaysOf(day); !slices.Equal(got, []string{"2031-05-12", "2031-05-13"}) {
		t.Errorf("the days of the day bar: %v", got)
	}
}
