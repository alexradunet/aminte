package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"aminte/internal/photo/phototest"
	"aminte/text"
	"golang.org/x/text/unicode/norm"
)

func TestStableRenameKeepsIdentityAndOldReferences(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "First name", "original [[Target]]", "")
	if err != nil {
		t.Fatal(err)
	}
	mention, _, err := s.CreatePage(ctx, "cli", "Mention", "[[First name]]", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Second name", "Final name", "FINAL NAME", "First name"} {
		got, err := s.Rename(ctx, "cli", id, name)
		if err != nil || got != id {
			t.Fatalf("rename %q = %d, %v; want stable %d", name, got, err, id)
		}
	}
	for _, name := range []string{"First name", "Second name", "Final name"} {
		got, err := s.PageID(ctx, name)
		if err != nil || got != id {
			t.Fatalf("lookup %q = %d, %v; want %d", name, got, err, id)
		}
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != before.Body || after.Version == before.Version || len(after.In) != 1 || after.In[0].ID != mention {
		t.Fatalf("rename changed identity/prose/backlinks: %+v", after)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale [[Forbidden]]", before.Version); status(err) != 409 {
		t.Fatalf("name mutation stale token = %v", err)
	}
	p, err := s.PageByID(ctx, mention)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveBody(ctx, "cli", mention, "[[First name]] ![[Second name|display]] [[Final name]]", p.Version)
	if err != nil || len(r.Linked) != 1 {
		t.Fatalf("aliases not deduplicated: %+v %v", r, err)
	}
	p, err = s.PageByID(ctx, mention)
	if err != nil || len(p.Out) != 1 || p.Out[0].ID != id {
		t.Fatalf("alias edges = %+v %v", p, err)
	}
}

func TestReferenceGrammarRejectsUnaddressableNamesAtomically(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"Lab [old]", "_old_", "a`b`c", "R&amp;D", "a`b`c"} {
		if _, _, err := s.CreatePage(ctx, "cli", name, "[[Forbidden]]", ""); status(err) != 422 {
			t.Fatalf("unaddressable name %q = %v; want 422", name, err)
		}
		if id, err := s.PageID(ctx, name); err != nil || id != 0 {
			t.Fatalf("rejected name persisted: %d %v", id, err)
		}
	}
	if id, err := s.PageID(ctx, "Forbidden"); err != nil || id != 0 {
		t.Fatalf("rejected create had side effects: %d %v", id, err)
	}
}

// TestAWriteToAPageThatIsNotLive: every write that needs a live page gives one answer for a page that does not exist,
// a 404 "no page N", and one for a tombstoned page, a 409 that names its id and its title: page N "Title" is deleted:
// restore it first (livePage). The owner thus reads which page, also when the write named it by its title (a category).
func TestAWriteToAPageThatIsNotLive(t *testing.T) {
	s := fresh(t)
	live, _, err := s.CreatePage(ctx, "cli", "Live", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, _, err := s.CreatePage(ctx, "cli", "Old Group", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Ana.jpg", SHA256: sha('a'), MIME: "image/jpeg", Preview: phototest.JPEG(8, 8)}); err != nil {
		t.Fatal(err)
	}
	const missing = 9999
	want := fmt.Sprintf(`page %d "Old Group" is deleted: restore it first (Restore, revive)`, gone)
	writes := []struct {
		name  string
		write func(id int64) error
	}{
		{"save-body", func(id int64) error { _, err := s.SaveBody(ctx, "cli", id, "words", "1"); return err }},
		{"rename", func(id int64) error { _, err := s.Rename(ctx, "cli", id, "New Title"); return err }},
		{"file-pages", func(id int64) error { _, err := s.FilePages(ctx, "cli", []int64{id}, "Group", "", false); return err }},
		{"set-cover", func(id int64) error {
			_, err := s.SetCover(ctx, "cli", id, "2026-10-16", FileIn{SHA256: sha('b'), MIME: "image/jpeg", Preview: phototest.JPEG(8, 8)})
			return err
		}},
		{"set-cover-of", func(id int64) error { _, err := s.SetCoverOf(ctx, "cli", id, "Ana.jpg"); return err }},
	}
	for _, w := range writes {
		if err := w.write(missing); status(err) != 404 || err.Error() != "no page 9999" {
			t.Errorf("%s of a missing page: %v (status %d), want 404 %q", w.name, err, status(err), "no page 9999")
		}
		if err := w.write(gone); status(err) != 409 || err.Error() != want {
			t.Errorf("%s of a tombstoned page: %v (status %d), want 409 %q", w.name, err, status(err), want)
		}
	}
	// a tombstoned page as the category of a filing
	if _, err := s.FilePages(ctx, "cli", []int64{live}, "Old Group", "", false); status(err) != 409 || err.Error() != want {
		t.Errorf("file-pages in a tombstoned category: %v (status %d), want 409 %q", err, status(err), want)
	}
}

// TestOnlyALookupWithBodyReadsTheBody: livePage and lookup give the title and the version of a page, and leave the
// body empty. body is the last column of entities, so a write that checks a long page (each id of a bulk filing, each
// title that a write looks up) does not read the overflow pages of its body. lookupWithBody gives the body too, for
// keep and RegisterMetric.
func TestOnlyALookupWithBodyReadsTheBody(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Long", "words of the body", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		for name, find := range map[string]func() (*pageRef, error){
			"livePage":       func() (*pageRef, error) { return tx.livePage(id) },
			"lookup":         func() (*pageRef, error) { return tx.lookup("long") },
			"lookupWithBody": func() (*pageRef, error) { return tx.lookupWithBody("long") },
		} {
			p, err := find()
			if err != nil {
				return err
			}
			body := ""
			if name == "lookupWithBody" {
				body = "words of the body"
			}
			if p == nil || p.ID != id || p.Title != "Long" || p.Version != "1" || p.Body != body {
				t.Errorf("%s = %+v; want the page Long at version 1, with the body %q", name, p, body)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTitlesAreUniqueByKey(t *testing.T) {
	s := fresh(t)
	s.CreatePage(ctx, "cli", "Café notes", "", "")
	_, _, err := s.CreatePage(ctx, "cli", "CAFÉ NOTES", "", "")
	var ex *ExistsError
	if !errors.As(err, &ex) {
		t.Errorf("a second page with the same key: %v", err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "CON", "", ""); status(err) != 422 {
		t.Errorf("a device name: %v", err)
	}
}

func TestRenamedIdentityWritesThroughRetainedHandle(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Typo", "prose", "")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := s.Rename(ctx, "cli", id, "Correct")
	if err != nil || renamed != id {
		t.Fatalf("rename: %d %v", renamed, err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other", "[[Typo]]", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "#REDIRECT [[Other]]", before.Version); err != nil {
		t.Fatal(err)
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != "#REDIRECT [[Other]]" || titles(after.Out, "wikilink") != "Other,REDIRECT" || len(after.Out) != 2 {
		t.Fatalf("ordinary prose: %+v", after)
	}
	if err := s.Do(ctx, "import:synthetic", func(tx *Tx) error { _, err := tx.SetBody(id, "imported [[Other]]"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := addLink(s, "cli", id, other, "part-of", "note"); err != nil {
		t.Fatal(err)
	}
	if err := dropLink(s, "cli", id, other, "part-of"); err != nil {
		t.Fatal(err)
	}
	if err := addLink(s, "cli", other, id, "part-of", "reverse"); err != nil {
		t.Fatal(err)
	}
	if err := dropLink(s, "cli", other, id, "part-of"); err != nil {
		t.Fatal(err)
	}
	mention, err := s.PageByID(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if titles(mention.Out, "wikilink") != "Correct" || len(mention.Out) != 1 {
		t.Fatalf("old handle endpoint: %+v", mention)
	}
	old, err := s.PageID(ctx, "Typo")
	if err != nil || old != id {
		t.Fatalf("retained handle: %d %v", old, err)
	}
}

func TestRename(t *testing.T) {
	s := fresh(t)
	old, _, _ := s.CreatePage(ctx, "cli", "Sourdogh", "Feed the starter. [[Baking]]", "")
	if _, _, err := s.Capture(ctx, "cli", "2026-09-29", "baked, see [[Sourdogh]]", nil); err != nil {
		t.Fatal(err)
	}
	baking, _ := s.PageID(ctx, "Baking")
	addLink(s, "cli", old, baking, "part-of", "")
	to, err := s.Rename(ctx, "cli", old, "Sourdough")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, to)
	if p.Body != "Feed the starter. [[Baking]]" || titles(p.Out, "wikilink") != "Baking" || titles(p.Out, "part-of") != "Baking" {
		t.Errorf("the new page: %+v", p)
	}
	if to != old {
		t.Fatalf("rename changed id: %d to %d", old, to)
	}
	if got, err := s.PageID(ctx, "Sourdogh"); err != nil || got != old {
		t.Fatalf("old alias = %d %v", got, err)
	}
	// Backlinks retain their endpoint; no redirect traversal is needed.
	if !strings.Contains(titles(p.In, "wikilink"), "2026-09-29") {
		t.Errorf("backlinks of the new page: %+v", p.In)
	}
	for _, c := range []struct {
		id    int64
		title string
		want  int
	}{
		{to, "Baking", 409},      // into an existing page with text of its own
		{to, "Health/Diet", 422}, // not a valid title
	} {
		if _, err := s.Rename(ctx, "cli", c.id, c.title); status(err) != c.want && !(c.want == 409 && isExists(err)) {
			t.Errorf("rename %d to %q: %v, want %d", c.id, c.title, err, c.want)
		}
	}
	// a typo ghost, empty, into an existing person
	if _, _, err := s.Capture(ctx, "cli", "2026-09-30", "met [[Sm]]", nil); err != nil {
		t.Fatal(err)
	}
	ghost, _ := s.PageID(ctx, "Sm")
	sam, _ := s.CreatePerson(ctx, "cli", "Sam", "Sam Example", "", "")
	if to, err := s.Rename(ctx, "cli", ghost, "Sam"); !isExists(err) || to != 0 {
		t.Errorf("ghost conflict must refuse without merging: %d, %v", to, err)
	}
	pe, err := s.PageByID(ctx, sam)
	if err != nil || len(pe.In) != 0 {
		t.Fatalf("conflict changed person: %+v %v", pe, err)
	}
	g, err := s.PageByID(ctx, ghost)
	if err != nil || !strings.Contains(titles(g.In, "wikilink"), "2026-09-30") {
		t.Fatalf("conflict lost ghost backlink: %+v %v", g, err)
	}
}

func isExists(err error) bool {
	_, ok := err.(*ExistsError)
	return ok || status(err) == 409
}

func TestNameOwnershipAndJournalReservations(t *testing.T) {
	s := fresh(t)
	ordinary, _, err := s.CreatePage(ctx, "cli", "Dated note", "prose", "")
	if err != nil {
		t.Fatal(err)
	}
	day, _, err := s.Capture(ctx, "cli", "2026-02-28", "day prose", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again, _, err := s.Capture(ctx, "cli", "2026-02-28", "", nil); err == nil || again != 0 {
		t.Fatalf("empty capture = %d %v", again, err)
	}
	if again, _, err := s.Capture(ctx, "cli", "2026-02-28", "more", nil); err != nil || again != day {
		t.Fatalf("existing day = %d %v", again, err)
	}
	if err := s.Promote(ctx, "cli", day, "", "", ""); err == nil {
		t.Fatal("day promoted")
	}
	if _, err := s.Rename(ctx, "cli", ordinary, "New dated note"); err != nil {
		t.Fatalf("dated ordinary alias control: %v", err)
	}
	if err := s.Tombstone(ctx, "cli", day); err != nil {
		t.Fatal(err)
	}
	if again, _, err := s.Capture(ctx, "cli", "2026-02-28", "revived", nil); err != nil || again != day {
		t.Fatalf("revival=%d %v", again, err)
	}
	for _, q := range []string{
		`INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES('page','missing',` + nowSQL + `,` + nowSQL + `,'cli')`,
		`INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES('page','mood',` + nowSQL + `,` + nowSQL + `,'cli')`,
	} {
		if err := s.DB.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err == nil {
			t.Fatalf("deferred owner commit accepted: %s", q)
		}
	}
	r, err := s.Integrity(ctx)
	if err != nil || !r.OK {
		t.Fatalf("integrity after refusals: %+v %v", r, err)
	}
}

// TestTheSchemaRefusesARenameOfAJournalDay: a day page keeps its date, and a date is the name of its day page alone.
// The triggers of the schema hold both rules (entity_names_day_insert), so a refused rename is a 422 with the
// sentence of the trigger, and it writes nothing. A date that a day page holds is a title another page has: a 409
// exists, as for any title. A day page renamed to its own date writes nothing, so no trigger refuses it: the writer
// refuses it with a 409 conflict, as step 0 of docs/cookbook/rename-a-page.md says.
func TestTheSchemaRefusesARenameOfAJournalDay(t *testing.T) {
	s := fresh(t)
	day, _, err := s.Capture(ctx, "cli", "2026-02-28", "day prose", nil)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := s.CreatePage(ctx, "cli", "Dated note", "prose", "")
	if err != nil {
		t.Fatal(err)
	}
	const (
		sentence = "a journal day has only its canonical date name"
		ownDate  = "a day page's canonical date is not renamed"
	)
	for _, c := range []struct {
		name   string
		id     int64
		title  string
		status int
		exists int64 // the page that the ExistsError of a 409 names; 0: the 409 is the conflict of the writer
	}{
		{"a day page to a free title", day, "Heading", 422, 0},
		{"a day page to the title of another page", day, "Dated note", 409, page},
		{"a page to a date that no page has", page, "2026-03-01", 422, 0},
		{"a page to the date of a day page", page, "2026-02-28", 409, day},
		{"a day page to its own date", day, "2026-02-28", 409, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := everyRow(t, s)
			_, err := s.Rename(ctx, "cli", c.id, c.title)
			var ex *ExistsError
			switch {
			case c.status == 422:
				if status(err) != 422 || !strings.Contains(err.Error(), sentence) {
					t.Errorf("got %v (status %d), want a 422 with %q", err, status(err), sentence)
				}
			case c.exists != 0:
				if !errors.As(err, &ex) || ex.ID != c.exists {
					t.Errorf("got %v, want the ExistsError of page %d", err, c.exists)
				}
			default:
				if status(err) != 409 || !strings.Contains(err.Error(), ownDate) {
					t.Errorf("got %v (status %d), want the 409 conflict %q", err, status(err), ownDate)
				}
			}
			if after := everyRow(t, s); after != before {
				t.Errorf("the rename wrote:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestStableNamesRevivalSelfAndRollback(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Old", "kept", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "New"); err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "New"); err != nil {
		t.Fatal(err)
	}
	same, err := s.PageByID(ctx, id)
	if err != nil || same.Version != before.Version {
		t.Fatalf("noop token: %+v %v", same, err)
	}
	fail := errors.New("rollback")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.rename(id, "Rolled back"); err != nil {
			return err
		}
		return fail
	}); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	same, err = s.PageByID(ctx, id)
	if err != nil || !reflect.DeepEqual(before, same) {
		t.Fatalf("rename rollback: %+v %v", same, err)
	}
	if got, err := s.PageID(ctx, "Rolled back"); err != nil || got != 0 {
		t.Fatalf("rolled back alias retained: %d %v", got, err)
	}
	r, err := s.SaveBody(ctx, "cli", id, "[[Old]] [[New]]", before.Version)
	if err != nil || len(r.Linked) != 0 {
		t.Fatalf("self aliases: %+v %v", r, err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	source, _, err := s.CreatePage(ctx, "cli", "Source", "[[Old]] [[New]]", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, source)
	if err != nil || len(p.Out) != 1 || p.Out[0].ID != id {
		t.Fatalf("revived alias edge: %+v %v", p, err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	r, err = s.SaveBody(ctx, "cli", source, "[[Old]] [[New]]", p.Version)
	if err != nil || len(r.Revived) != 1 || len(r.Linked) != 1 {
		t.Fatalf("revival feedback: %+v %v", r, err)
	}
}

func TestMoodIdentitySurvivesRename(t *testing.T) {
	s := fresh(t)
	if _, err := s.Rename(ctx, "cli", 1, "Wellbeing"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Mood", "Wellbeing"} {
		if _, err := s.Record(context.Background(), "cli", Reading{Metric: name, Day: "2026-02-28", Value: 0}); status(err) != 422 {
			t.Fatalf("invalid Mood via %q: %v", name, err)
		}
	}
	v := float64(4)
	_, _, err := s.Capture(ctx, "cli", "2026-02-28", "normal", &v)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Series(ctx, "Mood", "2026-02-27", "2026-02-28")
	if err != nil || len(rows) != 1 || rows[0].Metric != "Wellbeing" {
		t.Fatalf("alias series: %+v %v", rows, err)
	}
	bad := float64(0)
	if _, err := s.Correct(ctx, "cli", rows[0].ID, &bad); status(err) != 422 {
		t.Fatalf("invalid renamed Mood correction: %v", err)
	}
}

func TestNameRevisionExhaustionRefusesAtomically(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Old", "kept [[Target]]", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "New"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "Old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`UPDATE entities SET revision=9223372036854775807 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "Old"); err != nil {
		t.Fatalf("exhausted no-op: %v", err)
	}
	for _, name := range []string{"OLD", "New", "Unowned"} {
		if _, err := s.Rename(ctx, "cli", id, name); err == nil {
			t.Fatalf("exhausted rename %q accepted", name)
		}
		after, err := s.PageByID(ctx, id)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("exhaustion changed state: %+v %v", after, err)
		}
	}
	if id, err := s.PageID(ctx, "Unowned"); err != nil || id != 0 {
		t.Fatalf("failed alias persisted=%d %v", id, err)
	}
	r, err := s.Integrity(ctx)
	if err != nil || !r.OK {
		t.Fatalf("exhaustion damaged search: %+v %v", r, err)
	}
}

func TestDatedFileRenameKeepsOriginalAndOldEmbed(t *testing.T) {
	s := fresh(t)
	f := photo('a', "Old.jpg", "2019-06-03")
	f.Body = "caption [[Subject]]"
	kept, err := s.AddFile(ctx, "cli", f)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	day := dayOf(t, s, "2019-06-03")
	if day == nil {
		t.Fatal("missing day")
	}
	body := day.Body
	image, err := s.Preview(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(image) == 0 {
		t.Fatal("missing preview")
	}
	if got, err := s.Rename(ctx, "cli", kept.ID, "New.jpg"); err != nil || got != kept.ID {
		t.Fatalf("dated file rename: %d %v", got, err)
	}
	after, err := s.PageByID(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != before.Body || after.Day != before.Day || after.File.SHA256 != before.File.SHA256 || after.File.MIME != before.File.MIME || after.Version == before.Version {
		t.Fatalf("file changed identity/data: %+v", after)
	}
	for _, name := range []string{"Old.jpg", "New.jpg"} {
		id, err := s.PageID(ctx, name)
		if err != nil || id != kept.ID {
			t.Fatalf("file alias %q: %d %v", name, id, err)
		}
		preview, err := s.PreviewByTitle(ctx, name)
		if err != nil || !bytes.Equal(preview, image) {
			t.Fatalf("preview alias %q: %v", name, err)
		}
	}
	again, err := s.AddFile(ctx, "cli", f)
	if err != nil || again.ID != kept.ID || !again.Existing || again.Embedded {
		t.Fatalf("same original added embed after rename: %+v %v", again, err)
	}
	day = dayOf(t, s, "2019-06-03")
	if day.Body != body || len(day.Out) != 1 || day.Out[0].ID != kept.ID {
		t.Fatalf("old embed changed: %+v", day)
	}
	p, err := s.PageByID(ctx, day.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", day.ID, p.Body, p.Version); err != nil {
		t.Fatal(err)
	}
	p, err = s.PageByID(ctx, day.ID)
	if err != nil || len(p.Out) != 1 || p.Out[0].ID != kept.ID {
		t.Fatalf("old embed no longer resolves: %+v %v", p, err)
	}
}

// dayOf is the day page of day, nil when there is none.
func dayOf(t *testing.T, s *Store, day string) *Page {
	t.Helper()
	id, _ := s.PageID(ctx, day)
	if id == 0 {
		return nil
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReferenceNameContractProductionPipeline(t *testing.T) {
	data, err := os.ReadFile("../../docs/contract/titles-and-wikilinks.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(strings.ReplaceAll(string(data), "\r\n", "\n"), "<!-- reference-name-vectors -->")
	if !ok {
		t.Fatal("missing name vectors")
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		t.Fatal("missing name JSON block")
	}
	encoded, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("unterminated name vectors")
	}
	var vectors []struct {
		Name, Key string
		Accepted  bool
	}
	if err := json.Unmarshal([]byte(encoded), &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 30 {
		t.Fatalf("loaded %d name vectors; want 30", len(vectors))
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			if got := text.ValidTitle(v.Name); got != v.Accepted {
				t.Fatalf("predicate %v want %v", got, v.Accepted)
			}
			s := fresh(t)
			id, _, err := s.CreatePage(ctx, "cli", v.Name, "", "")
			if !v.Accepted {
				if status(err) != 422 || id != 0 {
					t.Fatalf("rejected create=%d %v", id, err)
				}
				if got, err := s.PageID(ctx, v.Name); err != nil || got != 0 {
					t.Fatalf("rejected name persisted=%d %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if text.TitleKey(v.Name) != v.Key {
				t.Fatalf("key=%q want %q", text.TitleKey(v.Name), v.Key)
			}
			source, _, err := s.CreatePage(ctx, "cli", "Vector source", "", "")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{v.Name, norm.NFC.String(v.Name)} {
				for _, body := range []string{"See [[" + name + "]].", "See ![[" + name + "]].", "See [[" + name + "|display]]."} {
					p, err := s.PageByID(ctx, source)
					if err != nil {
						t.Fatal(err)
					}
					r, err := s.SaveBody(ctx, "cli", source, body, p.Version)
					if err != nil || len(r.Linked) != 1 {
						t.Fatalf("save %q: %+v %v", body, r, err)
					}
					p, err = s.PageByID(ctx, source)
					if err != nil || p.Body != body || len(p.Out) != 1 || p.Out[0].ID != id {
						t.Fatalf("stored %q: %+v %v", body, p, err)
					}
					token := p.Version
					if _, err := s.SaveBody(ctx, "cli", source, body, token); err != nil {
						t.Fatal(err)
					}
					again, err := s.PageByID(ctx, source)
					if err != nil || again.Version != token {
						t.Fatalf("re-save token: %+v %v", again, err)
					}
					if got, err := s.PageID(ctx, name); err != nil || got != id {
						t.Fatalf("resolve %q: %d %v", name, got, err)
					}
				}
			}
			if _, err := s.Rename(ctx, "cli", id, "New vector name"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Rename(ctx, "cli", id, v.Name); err != nil {
				t.Fatal(err)
			}
			if err := s.Promote(ctx, "cli", id, "", "", ""); err != nil {
				t.Fatal(err)
			}
			p, err := s.PageByID(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveBody(ctx, "cli", source, p.Body, p.Version); err != nil {
				t.Fatal(err)
			}
			p, err = s.PageByID(ctx, source)
			if err != nil || len(p.Out) != 1 || p.Out[0].ID != id || p.Out[0].Type != "person" {
				t.Fatalf("promoted alias: %+v %v", p, err)
			}
		})
	}
	// Explicit contextual limitations, not stronger arbitrary compositional safety.
	for _, body := range []string{"See [[a`b]] and [[c`d]].", "[[Ref]]\n\n[Ref]: https://example.invalid", "[[Health *Diet*]]"} {
		_, got, _ := text.Targets(body, "")
		if len(got) != 0 {
			t.Fatalf("context limit %q: %q", body, got)
		}
	}
}

func TestCanonicalNamesSearchConflictPathsAndRebuild(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Blue", "needle original", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "Sky"); err != nil {
		t.Fatal(err)
	}
	check := func(query string, want int64) {
		t.Helper()
		hits, err := s.SearchWith(ctx, query, SearchOptions{}, 1, 0)
		if err != nil || len(hits) != 1 || hits[0].ID != want {
			t.Fatalf("search %q: %+v %v; want %d", query, hits, err, want)
		}
		r, err := s.Integrity(ctx)
		if err != nil || !r.OK {
			t.Fatalf("integrity: %+v %v", r, err)
		}
	}
	for _, q := range []string{"Blue original", "Sky original", `all_names:"blue sky"`, `all_names:Blue body:original`, `body:"needle original"`} {
		check(q, id)
	}
	empty, err := s.SearchWith(ctx, `body:"Blue needle"`, SearchOptions{}, 10, 0)
	if err != nil || len(empty) != 0 {
		t.Fatalf("alias must not duplicate body: %+v %v", empty, err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other owner", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Blue','blue') ON CONFLICT DO NOTHING`, other)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check("Blue original", id)
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "SKY"); err != nil {
		t.Fatal(err)
	}
	check("Sky needle", id)
	fail := errors.New("rollback search")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.rename(id, "Rollback"); err != nil {
			return err
		}
		if _, err := tx.SetBody(id, "rolled back"); err != nil {
			return err
		}
		return fail
	}); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	check("Blue original", id)
	hits, err := s.SearchWith(ctx, "Rollback", SearchOptions{}, 10, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("rollback indexed: %+v %v", hits, err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != before.Body {
		t.Fatal("rollback changed body")
	}
	if _, err := s.SaveBody(ctx, "cli", id, "replacement needle", p.Version); err != nil {
		t.Fatal(err)
	}
	check("Blue replacement", id)
	if _, err := s.DB.W.Exec(`INSERT INTO entities_fts(entities_fts) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	check("Sky replacement", id)
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchWith(ctx, "replacement", SearchOptions{}, 10, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("tombstone visible: %+v %v", hits, err)
	}
	if err := s.Revive(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	check("Blue replacement", id)
	if _, err := s.DB.W.Exec(`INSERT INTO entities_fts(entities_fts,rowid,preferred,all_names,body) SELECT 'delete',id,preferred,all_names,body FROM entity_search_content WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	r, err := s.Integrity(ctx)
	if err != nil || r.OK || r.FullTextIndexOK {
		t.Fatalf("missing document not detected: %+v %v", r, err)
	}
	if _, err := s.DB.W.Exec(`INSERT INTO entities_fts(entities_fts) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	check("Blue replacement", id)
}

func TestCanonicalSearchRankLimitAndStableTie(t *testing.T) {
	s := fresh(t)
	var first int64
	for i, name := range []string{"Alpha", "Beta"} {
		id, _, err := s.CreatePage(ctx, "cli", name, "needle", "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
		if _, err := s.Rename(ctx, "cli", id, name+" old"); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := s.SearchWith(ctx, "needle", SearchOptions{}, 1, 0)
	if err != nil || len(hits) != 1 || hits[0].ID != first {
		t.Fatalf("ranked limited tie: %+v %v; want %d", hits, err, first)
	}
	hits, err = s.SearchWith(ctx, "needle", SearchOptions{}, 10, 0)
	if err != nil || len(hits) != 2 || hits[0].ID != first || hits[1].ID <= first {
		t.Fatalf("one document per entity: %+v %v", hits, err)
	}
	for _, h := range hits {
		if h.Snippet != "⟦needle⟧" {
			t.Fatalf("body field snippet %s: %q", fmt.Sprint(h.ID), h.Snippet)
		}
	}
}

// TestSearchSnippetKeepsTheBracketsOfTheText says the marks of a match cannot be confused with the text: a body's
// [[wikilinks]] and check boxes keep their brackets, and only the matched word is marked.
func TestSearchSnippetKeepsTheBracketsOfTheText(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "cli", "Plans", "Coffee with [[Ana]]; we planned the [[Garden project]]. - [x] seeds", ""); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchWith(ctx, "garden", SearchOptions{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range hits {
		if h.Title == "Plans" {
			got = append(got, h.Snippet)
		}
	}
	want := "Coffee with [[Ana]]; we planned the [[⟦Garden⟧ project]]. - [x] seeds"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("snippet of Plans = %q, want [%q]", got, want)
	}
}

// TestASaveSaysWhichLinksItAdded: a save's Linked is every page the body links after it; Added is the ones this save
// linked that the page did not link before, so a capture can report what it added and not the whole day again.
func TestASaveSaysWhichLinksItAdded(t *testing.T) {
	s := fresh(t)
	_, r, err := s.Capture(ctx, "cli", "2026-10-14", "Ran with [[Sam]] at [[Lakeside Park]].", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Added, []string{"Sam", "Lakeside Park"}) {
		t.Errorf("first capture added %v", r.Added)
	}
	_, r, err = s.Capture(ctx, "cli", "2026-10-14", "Lunch with [[Mihai]] and [[Sam]]. #food", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Linked, []string{"Sam", "Lakeside Park", "Mihai", "food"}) || !reflect.DeepEqual(r.Added, []string{"Mihai", "food"}) {
		t.Errorf("second capture linked %v, added %v; want every link, and Mihai and food added", r.Linked, r.Added)
	}
	_, r, err = s.Capture(ctx, "cli", "2026-10-14", "Home again.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 0 {
		t.Errorf("a capture with no link added %v", r.Added)
	}
}

// TestTitlesByPrefixFoldsLikeATitleKey: a prefix finds the names whose key starts with its title key (case, ß, a
// decomposed accent), an alias with its page's preferred title, never a tombstoned page, in key order, limited.
func TestTitlesByPrefixFoldsLikeATitleKey(t *testing.T) {
	s := fresh(t)
	page := func(title string) int64 {
		t.Helper()
		id, _, err := s.CreatePage(ctx, "cli", title, "", "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	page("Sam Example")
	page("SAMUEL")
	page("Sb")
	page("Straße")
	page("Café")
	old := page("Samwise draft")
	if _, err := s.Rename(ctx, "cli", old, "Gardener"); err != nil {
		t.Fatal(err)
	}
	gone := page("Samantha")
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	show := func(prefix string, limit int) string {
		t.Helper()
		ms, err := s.TitlesByPrefix(ctx, prefix, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range ms {
			out = append(out, m.Name+"="+m.Title)
		}
		return strings.Join(out, ", ")
	}
	for _, c := range []struct {
		prefix string
		limit  int
		want   string
	}{
		{"sam", 10, "Sam Example=Sam Example, SAMUEL=SAMUEL, Samwise draft=Gardener"},
		{"  SAM", 2, "Sam Example=Sam Example, SAMUEL=SAMUEL"},
		{"sa", 10, "Sam Example=Sam Example, SAMUEL=SAMUEL, Samwise draft=Gardener"},
		{"STRAS", 10, "Straße=Straße"},
		{"cafe\u0301", 10, "Café=Café"},
		{"zz", 10, ""},
	} {
		if got := show(c.prefix, c.limit); got != c.want {
			t.Errorf("TitlesByPrefix(%q, %d) = %q, want %q", c.prefix, c.limit, got, c.want)
		}
	}
	if _, err := s.TitlesByPrefix(ctx, "  ", 10); status(err) != 422 {
		t.Errorf("a blank prefix: %v", err)
	}
}
