package view

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"aminte/siren"
)

// rootOf is the root of the API as a client reads it with ?embed=today: JSON, decoded into an entity.
func rootOf(t *testing.T, body string) *siren.Entity {
	t.Helper()
	e := &siren.Entity{}
	if err := json.Unmarshal([]byte(body), e); err != nil {
		t.Fatal(err)
	}
	return e
}

const rootJSON = `{"class":["root"],"title":"Aminte",
 "properties":{"today":"2031-05-14","zone":"+14","utc_offset":50400,"recent":[]},
 "entities":[
  {"rel":["today"],"href":"/days/2031-05-14","title":"2031-05-14","class":["day"],
   "properties":{"day":"2031-05-14","mood":[4],"streak":3,"written":true,
     "habits":[{"name":"Smoke","done":false},{"name":"Stretch","done":null},{"name":"Walk","done":true}]}}],
 "links":[{"rel":["self"],"href":"/"},{"rel":["app"],"href":"/with/notes","title":"Notes"},{"rel":["app"],"href":"/with/maps","title":"maps"},{"rel":["tasks"],"href":"/tasks","title":"Tasks"}]}`

// TestTheShellIsReadFromTheRoot: the day of today and its zone, the strip of the embedded day, the apps and the task
// service.
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
	if !slices.Equal(shell.Apps, []AppLink{{"notes", "Notes"}, {"maps", "maps"}}) || !shell.Tasks {
		t.Errorf("apps %v, tasks %v", shell.Apps, shell.Tasks)
	}
}

// TestTheShellKeepsWhatWasNotEmbedded: a root whose today is a reference gives a shell with no strip; a root with no day as today gives no shell; a zone that the database has is that zone, and a name
// that is empty or Local is the zone of this machine.
func TestTheShellKeepsWhatWasNotEmbedded(t *testing.T) {
	bare := `{"class":["root"],"title":"Aminte","properties":{"today":"2031-05-14","zone":"UTC","utc_offset":0},
	 "entities":[{"rel":["today"],"href":"/days/2031-05-14"}]}`
	shell, err := ShellOf(rootOf(t, bare))
	if err != nil || shell.Strip != nil || shell.Zone != time.UTC || shell.Tasks || len(shell.Apps) != 0 {
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
