package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aminte/client"
	"aminte/internal/api"
	"aminte/internal/api/apitest"
	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/internal/photo"
	"aminte/internal/photo/phototest"
	"aminte/siren"
)

// TestTheStoresClockIsTheOneClock: the store's clock is in a zone far from UTC, at an instant where the UTC day and
// the local day are not the same. Each read of today or now agrees with the local day or the instant of that clock:
// the root's today, the redirect of /days/today, the day page that a capture with no day makes, the day of a page and
// of a kept file that have no day, and the name of a snapshot.
func TestTheStoresClockIsTheOneClock(t *testing.T) {
	d := dbtest.New(t)
	const today, utcDay = "2026-03-11", "2026-03-10"
	zone := time.FixedZone("+14", 14*60*60)               // the offset of Pacific/Kiritimati, with no zone database
	now := time.Date(2026, 3, 10, 11, 30, 0, 0, time.UTC) // 2026-03-11 01:30:00 in the zone
	s := &core.Store{DB: d, SnapshotDir: t.TempDir(), Clock: func() time.Time { return now }, Zone: zone}
	h := api.NewWith(s, nil)
	c := client.InProcess(h, "cli")
	ctx := t.Context()

	root := apitest.Must(t)(c.Get(ctx, "/"))
	if got := props(root)["today"]; got != today {
		t.Errorf("the root's today is %v, want %s", got, today)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/days/today", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/days/"+today {
		t.Errorf("/days/today: %d to %q, want 303 to /days/%s", rec.Code, rec.Header().Get("Location"), today)
	}

	// A capture with no day: it writes on the local day, and the answer names that day.
	req := httptest.NewRequest("POST", "/days/today/capture", strings.NewReader(url.Values{"text": {"Woke before the sun."}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != "/days/"+today {
		t.Fatalf("a capture with no day: %d to %q, want 200 and /days/%s", rec.Code, rec.Header().Get("Location"), today)
	}
	if id, err := s.PageID(ctx, today); err != nil || id == 0 {
		t.Errorf("the capture made no day page %s: %v", today, err)
	}
	if id, err := s.PageID(ctx, utcDay); err != nil || id != 0 {
		t.Errorf("the capture made the day page of the UTC day %s: id %d, %v", utcDay, id, err)
	}

	// A page and a file that is not a photo, both with no day, take the local day.
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Night thought"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf"}))
	for _, title := range []string{"Night thought", "Scan.pdf"} {
		id, err := s.PageID(ctx, title)
		if err != nil {
			t.Fatal(err)
		}
		page, err := s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if page.Day != today {
			t.Errorf("%s has the day %q, want %s", title, page.Day, today)
		}
	}

	// A snapshot is named by the local day, and a second one of that day by the local time too.
	var names []string
	for range 2 {
		e := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "snapshot"), nil))
		path, _ := props(e)["snapshot"].(string)
		names = append(names, filepath.Base(path))
	}
	if want := "life-2026-03-11.db life-2026-03-11T013000.db"; strings.Join(names, " ") != want {
		t.Errorf("the snapshots are %v, want %s", names, want)
	}
}

// TestEveryDayFieldReadsTheWordToday: in a zone far from UTC, the word "today" in each field of the catalog whose type
// is date is the local day of the store's clock, not the UTC day. Each case sends the word in its field and reads back
// the day that the action wrote or opened. A date field with no case fails the test, so a new day field cannot miss the
// word. A photo whose EXIF names another day keeps the day of the field, through the multipart form. The word is exact:
// "Today" is not a day, and a write refuses it with the 422 of the schema's CHECK (open-day, a read, with its own).
func TestEveryDayFieldReadsTheWordToday(t *testing.T) {
	const today = "2026-03-11"
	zone := time.FixedZone("+14", 14*60*60)               // the offset of Pacific/Kiritimati, with no zone database
	now := time.Date(2026, 3, 10, 11, 30, 0, 0, time.UTC) // 2026-03-11 01:30:00 in the zone; the UTC day is 2026-03-10
	s := &core.Store{DB: dbtest.New(t), Clock: func() time.Time { return now }, Zone: zone}
	c := client.InProcess(api.NewWith(s, nil), "cli")
	ctx := t.Context()
	catalog := apitest.Must(t)(c.Get(ctx, "/actions"))
	do := func(action string, v map[string]string) (*siren.Entity, error) {
		return c.Do(ctx, apitest.Action(t, catalog, action), v)
	}
	apitest.Must(t)(do("register-metric", map[string]string{"name": "weight", "unit": "kg"}))
	n := 0 // each call writes a new title, and sends a new photo
	title := func(s string) string { n++; return fmt.Sprintf("%s %d", s, n) }
	cases := map[string]struct { // "action field"
		send    func(word string) (*siren.Entity, error) // the action with the word in the field
		refusal string                                   // a part of the message of the refusal of "Today"
	}{
		"capture day": {func(w string) (*siren.Entity, error) {
			return do("capture", map[string]string{"day": w, "text": "Woke early."})
		}, "CHECK constraint failed: entities_day"},
		"say day": {func(w string) (*siren.Entity, error) {
			return do("say", map[string]string{"day": w, "text": "Good morning."})
		}, "is not a local day"},
		"reply day": {func(w string) (*siren.Entity, error) {
			said, err := do("say", map[string]string{"day": "today", "text": "Anything new?"})
			if err != nil {
				return nil, err
			}
			return do("reply", map[string]string{"day": w, "turn": keyOf(t, said, "owner", 0), "text": "Nothing."})
		}, "is not a local day"},
		"create-person birth_day": {func(w string) (*siren.Entity, error) {
			return do("create-person", map[string]string{"title": title("Born"), "birth_day": w})
		}, "CHECK constraint failed: people_birth_day"},
		"create-person death_day": {func(w string) (*siren.Entity, error) {
			return do("create-person", map[string]string{"title": title("Died"), "death_day": w})
		}, "CHECK constraint failed: people_death_day"},
		"add-file day": {func(w string) (*siren.Entity, error) {
			jpg := filepath.Join(t.TempDir(), title("IMG")+".jpg")
			taken := fmt.Sprintf("2019-06-%02d 07:41:12", n) // the photo's own day in its EXIF, which the field overrides
			if err := os.WriteFile(jpg, phototest.WithExif(phototest.JPEG(64, 48), photo.Meta{Taken: taken}, true), 0o644); err != nil {
				t.Fatal(err)
			}
			return c.Do(ctx, apitest.Action(t, catalog, "add-file"), map[string]string{"title": filepath.Base(jpg), "day": w, "original": jpg})
		}, "CHECK constraint failed: entities_day"},
		"record day": {func(w string) (*siren.Entity, error) {
			return do("record", map[string]string{"metric": "weight", "day": w, "value": "71"})
		}, "CHECK constraint failed: measurements_day"},
		"open-day day": {func(w string) (*siren.Entity, error) {
			return do("open-day", map[string]string{"day": w})
		}, "day must be a day"},
	}
	type dayCase = struct {
		send    func(word string) (*siren.Entity, error)
		refusal string
	}
	caseOf := func(send func(word string) (*siren.Entity, error), refusal string) dayCase {
		return dayCase{send, refusal}
	}
	// promote and edit-person send a day of a person to a page that exists already.
	idOf := func(e *siren.Entity, err error) (string, error) {
		if err != nil {
			return "", err
		}
		return fmt.Sprint(props(e)["id"]), nil
	}
	for _, field := range []string{"birth_day", "death_day"} {
		refusal := "CHECK constraint failed: people_" + field
		cases["promote "+field] = caseOf(func(w string) (*siren.Entity, error) {
			id, err := idOf(do("create-page", map[string]string{"title": title("Page")}))
			if err != nil {
				return nil, err
			}
			return do("promote", map[string]string{"id": id, field: w})
		}, refusal)
		cases["edit-person "+field] = caseOf(func(w string) (*siren.Entity, error) {
			id, err := idOf(do("create-person", map[string]string{"title": title("Person")}))
			if err != nil {
				return nil, err
			}
			return do("edit-person", map[string]string{"id": id, field: w})
		}, refusal)
	}
	var fields []string
	for _, a := range catalog.Actions {
		for _, f := range a.Fields {
			if f.Type != "date" {
				continue
			}
			key := a.Name + " " + f.Name
			fields = append(fields, key)
			tc, ok := cases[key]
			if !ok {
				t.Errorf("the date field %s has no case here", key)
				continue
			}
			e, err := tc.send("today")
			if err != nil {
				t.Errorf("%s with the word today: %v", key, err)
				continue
			}
			got := props(e)["day"]
			if person, ok := props(e)["person"].(map[string]any); ok { // a person's days are in its details
				got = person[f.Name]
			}
			if got != today {
				t.Errorf("%s with the word today: the day %v, want %s", key, got, today)
			}
			if _, err := tc.send("Today"); apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(err.Error(), tc.refusal) {
				t.Errorf("%s with the word Today: %v, want a 422 that says %s", key, err, tc.refusal)
			}
		}
	}
	for key := range cases {
		if !slices.Contains(fields, key) {
			t.Errorf("the case %s names no date field of the catalog", key)
		}
	}
}
