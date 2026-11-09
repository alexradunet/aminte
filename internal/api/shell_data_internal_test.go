package api

// The data that a client needs to draw a frame, in the answers of the API: the Today strip on the day, the clock and
// the apps on the root. The browser extension reads them through ?embed,
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

// TestTheRootCarriesTheFrame: the clock (zone and offset), the day of today as a reference that ?embed replaces, the
// connected apps that run. A zone with no name in the database has the name of its offset. The root of a server with
// no app has no link app.
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
	if want := []string{"today /days/2031-05-14"}; !slices.Equal(refs, want) {
		t.Errorf("the sub-entities: %v, want %v", refs, want)
	}
	if root.LinkByRel("app") != nil {
		t.Error("a root with no app has a link app")
	}

	if err := apps.set("notes", "http://127.0.0.1:1", "Notes", nil); err != nil {
		t.Fatal(err)
	}
	apps.Expect("quiet") // known, and not running: no link
	if err := apps.set("maps", "http://127.0.0.1:2", "", nil); err != nil {
		t.Fatal(err)
	}
	var links []string
	for _, l := range readData(t, h, "/").Links {
		if l.HasRel("app") {
			links = append(links, l.Rel[0]+" "+l.Href+" "+l.Title)
		}
	}
	if want := []string{"app /with/maps maps", "app /with/notes Notes"}; !slices.Equal(links, want) {
		t.Errorf("the links of the apps: %q, want %q", links, want)
	}

	// The frame in one read: the day replaces its reference.
	framed := readData(t, h, "/?embed=today").Entities
	if len(framed) != 1 || !framed[0].Embedded() {
		t.Fatalf("the frame: %+v", framed)
	}
	day := framed[0]
	if p := propsOf(*day.Entity()); day.Class[0] != "day" || day.Href != "/days/2031-05-14" || p["day"] != dataToday || p["written"] != false || p["streak"] != 0.0 {
		t.Errorf("the embedded day: %v %q %v", day.Class, day.Href, p)
	}
}
