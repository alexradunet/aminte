package api

// The data that a client needs to draw a frame, in the answers of the API: the Today strip on the day, the clock and
// the apps on the root, the refs and the day bar on the conversation. The browser extension reads them through ?embed,
// and so does any other client.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/siren"
)

const dataToday = "2031-05-14"

// dataStore is a synthetic file with the clock fixed at the start of dataToday in UTC.
func dataStore(t *testing.T) *core.Store {
	t.Helper()
	at := time.Date(2031, 5, 14, 0, 0, 0, 0, time.UTC)
	return &core.Store{DB: dbtest.New(t), Clock: func() time.Time { return at }, Zone: time.UTC}
}

// readData is the entity of a GET as a JSON client reads it.
func readData(t *testing.T, h http.Handler, target string) *siren.Entity {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Accept", sirenType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	e := &siren.Entity{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), e) != nil {
		t.Fatalf("GET %s: %d %.300s", target, rec.Code, rec.Body.String())
	}
	return e
}

func propsOf(e siren.Entity) map[string]any { p, _ := e.Properties.(map[string]any); return p }

// TestTheDayOfTodayCarriesTheStrip: the streak ends today (or yesterday), written says that the day page has words, and
// the habits are the daily metrics with no unit, each done, not done or null. Three days are written, the 12th to the
// 14th. Walk is done on the 14th, Smoke is 0 on the 14th, Stretch has no reading on the 14th, and Weight has a unit and
// so is no habit. Any other day carries none of the three.
func TestTheDayOfTodayCarriesTheStrip(t *testing.T) {
	s, ctx := dataStore(t), t.Context()
	yes := true
	for _, m := range []core.NewMetric{{Name: "Walk", Daily: &yes}, {Name: "Stretch", Daily: &yes}, {Name: "Smoke", Daily: &yes}, {Name: "Weight", Unit: "kg", Daily: &yes}} {
		if _, err := s.CreateMetric(ctx, "cli", m); err != nil {
			t.Fatal(err)
		}
	}
	for _, day := range []string{"2031-05-12", "2031-05-13", "2031-05-14"} {
		if _, _, err := s.Capture(ctx, "cli", day, "Synthetic words.", nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []core.Reading{{Metric: "Walk", Day: dataToday, Value: 1}, {Metric: "Smoke", Day: dataToday, Value: 0},
		{Metric: "Stretch", Day: "2031-05-04", Value: 1}, {Metric: "Weight", Day: dataToday, Value: 70}, {Metric: "Walk", Day: "2031-05-13", Value: 0}} {
		if _, err := s.Record(ctx, "cli", r); err != nil {
			t.Fatal(err)
		}
	}
	h := NewWith(s, nil)
	p := propsOf(*readData(t, h, "/days/"+dataToday))
	if p["streak"] != 3.0 || p["written"] != true {
		t.Errorf("today: streak %v, written %v; want 3 and true", p["streak"], p["written"])
	}
	want := []any{map[string]any{"name": "Smoke", "done": false}, map[string]any{"name": "Stretch", "done": nil}, map[string]any{"name": "Walk", "done": true}}
	if !reflect.DeepEqual(p["habits"], want) {
		t.Errorf("the habits: %v, want %v", p["habits"], want)
	}
	for _, day := range []string{"2031-05-13", "2031-05-11"} {
		for _, name := range []string{"streak", "written", "habits"} {
			if v, has := propsOf(*readData(t, h, "/days/"+day))[name]; has {
				t.Errorf("%s has %s = %v; only the day of today has it", day, name, v)
			}
		}
	}
	// A day with no words yet counts the days before it, and a day with no habit has no habits.
	empty := dataStore(t)
	for _, day := range []string{"2031-05-12", "2031-05-13"} {
		if _, _, err := empty.Capture(ctx, "cli", day, "Synthetic words.", nil); err != nil {
			t.Fatal(err)
		}
	}
	p = propsOf(*readData(t, NewWith(empty, nil), "/days/"+dataToday))
	if v, has := p["written"]; p["streak"] != 2.0 || !has || v != false || p["habits"] != nil {
		t.Errorf("a day with no words yet: streak %v, written %v, habits %v; want 2, false and none", p["streak"], v, p["habits"])
	}
}

// TestTheRootCarriesTheFrame: the clock (zone and offset), the day of today and its conversation as references that
// ?embed replaces, the connected apps that run and the task service. A zone with no name in the database has the name
// of its offset. The root of a server with no app and no task service has neither link.
func TestTheRootCarriesTheFrame(t *testing.T) {
	s := dataStore(t)
	s.Zone = time.FixedZone("+14", 14*3600)
	s.Clock = func() time.Time { return time.Date(2031, 5, 13, 23, 30, 0, 0, time.UTC) } // the 14th, 13:30 in the zone
	apps := NewApps()
	h := NewWith(s, apps)

	root := readData(t, h, "/")
	p := propsOf(*root)
	if p["today"] != dataToday || p["zone"] != "+14" || p["utc_offset"] != 50400.0 {
		t.Errorf("the clock: %v %v %v, want 2031-05-14 +14 50400", p["today"], p["zone"], p["utc_offset"])
	}
	var refs []string
	for _, l := range root.Entities {
		refs = append(refs, l.Rel[0]+" "+l.Href)
		if l.Embedded() {
			t.Errorf("the sub-entity %s is embedded in an answer that asked for no embed", l.Href)
		}
	}
	if want := []string{"today /days/2031-05-14", "conversation /days/2031-05-14/conversation"}; !slices.Equal(refs, want) {
		t.Errorf("the sub-entities: %v, want %v", refs, want)
	}
	if root.LinkByRel("app") != nil || root.LinkByRel("tasks") != nil {
		t.Error("a root with no app and no task service has a link app or tasks")
	}

	if err := apps.set("notes", "http://127.0.0.1:1", "Notes", nil); err != nil {
		t.Fatal(err)
	}
	apps.Expect("quiet") // known, and not running: no link
	if err := apps.set("maps", "http://127.0.0.1:2", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := apps.setTasks("svc", "http://127.0.0.1:3", actionsOfWrites(), nil); err != nil {
		t.Fatal(err)
	}
	var links []string
	for _, l := range readData(t, h, "/").Links {
		if l.HasRel("app") || l.HasRel("tasks") {
			links = append(links, l.Rel[0]+" "+l.Href+" "+l.Title)
		}
	}
	if want := []string{"app /with/maps maps", "app /with/notes Notes", "tasks /tasks Tasks"}; !slices.Equal(links, want) {
		t.Errorf("the links of the apps: %q, want %q", links, want)
	}

	// The frame in one read: the day and the conversation replace their references.
	framed := readData(t, h, "/?embed=today,conversation").Entities
	if len(framed) != 2 || !framed[0].Embedded() || !framed[1].Embedded() {
		t.Fatalf("the frame: %+v", framed)
	}
	day, conv := framed[0], framed[1]
	if p := propsOf(*day.Entity()); day.Class[0] != "day" || day.Href != "/days/2031-05-14" || p["day"] != dataToday || p["written"] != false || p["streak"] != 0.0 {
		t.Errorf("the embedded day: %v %q %v", day.Class, day.Href, p)
	}
	if conv.Class[0] != "conversation" || len(conv.Actions) != 1 || conv.Actions[0].Name != "say" || conv.Actions[0].Href != "/days/2031-05-14/say" {
		t.Errorf("the embedded conversation: %v %v", conv.Class, conv.Actions)
	}
}

// TestAConversationNamesItsRefsAndTheEarlierDays: the refs are the addresses of the tool lines, the newest turn
// first, each once, six of a turn at most; the links earlier are the three newest days before today that have a live
// conversation page, oldest first, the same on the conversation of any day.
func TestAConversationNamesItsRefsAndTheEarlierDays(t *testing.T) {
	s, ctx := dataStore(t), t.Context()
	page := func(day, body string) int64 {
		id, _, err := s.AppendParagraph(ctx, "cli", core.ConversationTitle(day), day, "", conversationFirstLine(day),
			func(string) string { return conversationFirstLine(day) + "\n\n" + body })
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	lines := func(days ...string) string {
		out := ""
		for _, d := range days {
			out += "- `open` → /days/" + d + "\n"
		}
		return out
	}
	older := lines("2031-01-01", "2031-01-02", "2031-01-03", "2031-01-04", "2031-01-05", "2031-01-06", "2031-01-07", "2031-01-08")
	newer := lines("2031-01-03", "2031-02-01", "2031-02-02") + "- `failed_tool` failed: not_found\n"
	page(dataToday, "**Owner** (09:00): Show me.\n\n"+older+"\n**aminte** (09:01): Here.\n\n**Owner** (09:02): More.\n\n"+newer+"\n**aminte** (09:03): And more.\n")
	for _, day := range []string{"2031-05-01", "2031-05-10", "2031-05-12", "2031-05-13", "2031-04-29"} {
		page(day, "")
	}
	tomb := page("2031-05-11", "")
	if err := s.Tombstone(ctx, "cli", tomb); err != nil {
		t.Fatal(err)
	}
	h := NewWith(s, nil)

	conv := readData(t, h, "/days/today/conversation")
	var refs, earlier []string
	for _, l := range conv.Entities {
		if l.HasRel("ref") {
			refs = append(refs, l.Href)
		}
	}
	wantRefs := []string{"/days/2031-01-03", "/days/2031-02-01", "/days/2031-02-02", "/days/2031-01-01", "/days/2031-01-02", "/days/2031-01-04", "/days/2031-01-05", "/days/2031-01-06"}
	if !slices.Equal(refs, wantRefs) {
		t.Errorf("the refs:\n%q\nwant\n%q", refs, wantRefs)
	}
	for _, l := range conv.Links {
		if l.HasRel("earlier") {
			earlier = append(earlier, l.Title+" "+l.Href)
		}
	}
	wantEarlier := []string{"2031-05-10 /days/2031-05-10/conversation", "2031-05-12 /days/2031-05-12/conversation", "2031-05-13 /days/2031-05-13/conversation"}
	if !slices.Equal(earlier, wantEarlier) {
		t.Errorf("the earlier days: %q, want %q", earlier, wantEarlier)
	}
	// The same days on the conversation of a past day, and on a day with no page at all.
	for _, day := range []string{"2031-05-10", "2031-03-03"} {
		var got []string
		for _, l := range readData(t, h, "/days/"+day+"/conversation").Links {
			if l.HasRel("earlier") {
				got = append(got, l.Title+" "+l.Href)
			}
		}
		if !slices.Equal(got, wantEarlier) {
			t.Errorf("the earlier days on %s: %q, want %q", day, got, wantEarlier)
		}
	}
	// ?embed=ref replaces each ref by the thing it names, and the address with no thing stays a reference.
	for _, l := range readData(t, h, "/days/today/conversation?embed=ref").Entities {
		if l.HasRel("ref") && (!l.Embedded() || l.Class[0] != "day") {
			t.Errorf("the ref %s: embedded %v, class %v", l.Href, l.Embedded(), l.Class)
		}
	}

	// Beyond 14 days there is no earlier day.
	far := dataStore(t)
	if _, _, err := far.AppendParagraph(ctx, "cli", core.ConversationTitle("2031-04-29"), "2031-04-29", "", "[[2031-04-29]]", func(string) string { return "[[2031-04-29]]\n" }); err != nil {
		t.Fatal(err)
	}
	if e := readData(t, NewWith(far, nil), "/days/today/conversation"); e.LinkByRel("earlier") != nil {
		t.Errorf("a conversation 15 days back is on the day bar: %v", e.Links)
	}
}
