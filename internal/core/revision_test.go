package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"aminte/internal/photo/phototest"
)

func TestConditionalSaveRejectsEqualClockStaleToken(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Clock witness", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var clock string
	if err := s.DB.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, id).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "committed [[Kept]]", before.Version); err != nil {
		t.Fatal(err)
	}
	// Deterministic equal-clock witness, not a timing-dependent collision search.
	// Only audit time is restored; committed editable state/version is untouched.
	if _, err := s.DB.W.Exec(`UPDATE entities SET updated_at=? WHERE id=?`, clock, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale [[Forbidden]]", before.Version); status(err) != 409 {
		t.Fatalf("stale token: %v; want 409", err)
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != "committed [[Kept]]" || after.Version == before.Version || titles(after.Out, "wikilink") != "Kept" {
		t.Fatalf("stale save changed committed state: %+v", after)
	}
	target, err := s.PageID(ctx, "Forbidden")
	if err != nil {
		t.Fatal(err)
	}
	if target != 0 {
		t.Fatal("stale save created a target")
	}
}

// A save whose reply was lost is sent again with the old version. When the body is the one the page holds, the save
// is a no-op that succeeds and answers the live version. When the body differs, the version decides: 409.
func TestSaveBodyResentWithAStaleVersionIsANoop(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Resend witness", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	const body = "second [[Kept]]"
	if _, err := s.SaveBody(ctx, "cli", id, body, read.Version); err != nil {
		t.Fatal(err)
	}
	saved, err := s.PageByID(ctx, id)
	if err != nil || saved.Version == read.Version {
		t.Fatalf("the save did not advance the version: %+v %v", saved, err)
	}

	r, err := s.SaveBody(ctx, "cli", id, body, read.Version) // the retry: the old version, the same body
	if err != nil {
		t.Fatalf("a resent body with a stale version: %v; want success", err)
	}
	if len(r.Linked) != 1 || r.Linked[0] != "Kept" || len(r.Added) != 0 || len(r.Created) != 0 {
		t.Errorf("the answer of the resend: %+v; want the link Kept and nothing added or created", r)
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != saved.Version || after.Body != body || titles(after.Out, "wikilink") != "Kept" {
		t.Errorf("the resend changed the page: %+v, was %+v", after, saved)
	}

	if _, err := s.SaveBody(ctx, "cli", id, body+" changed", read.Version); status(err) != 409 {
		t.Errorf("a different body with a stale version: %v; want 409", err)
	}
	if again, err := s.PageByID(ctx, id); err != nil || again.Version != saved.Version || again.Body != body {
		t.Errorf("the refused save changed the page: %+v %v", again, err)
	}
}

func TestEditRevisionNoopRollbackAndTypedMutation(t *testing.T) {
	s := fresh(t)
	id, err := s.CreatePerson(ctx, "cli", "Person witness", "Name", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, before.Body, before.Version); err != nil {
		t.Fatal(err)
	}
	same, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != before.Version {
		t.Fatal("body no-op consumed revision")
	}
	if _, err := s.DB.W.Exec(`UPDATE people SET name=name WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	same, err = s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != before.Version {
		t.Fatal("typed no-op consumed revision")
	}
	rollback := errors.New("rollback edit")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.SetBody(id, "rollback [[Missing]]"); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	same, err = s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != before.Version || same.Body != before.Body {
		t.Fatal("rollback consumed revision or body")
	}
	if _, err := s.DB.W.Exec(`UPDATE people SET name='New name' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	changed, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Version == before.Version {
		t.Fatal("typed mutation retained stale revision")
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale", before.Version); status(err) != 409 {
		t.Fatalf("typed stale save: %v", err)
	}
}

func TestEditRevisionExhaustionAndCancellation(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Exhaustion witness", "kept", "")
	if err != nil {
		t.Fatal(err)
	}
	// Explicit accelerated counter fixture; no clock or production connection bypass.
	if _, err := s.DB.W.Exec(`UPDATE entities SET revision=9223372036854775807 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version != "9223372036854775807" {
		t.Fatalf("lossy token %q", before.Version)
	}
	if _, err := s.SaveBody(ctx, "cli", id, before.Body, before.Version); err != nil {
		t.Fatalf("exhausted no-op: %v", err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "changed [[Forbidden]]", before.Version); err == nil {
		t.Fatal("exhausted edit accepted")
	}
	if err := s.Tombstone(ctx, "cli", id); err == nil {
		t.Fatal("exhausted lifecycle change accepted")
	}
	if _, err := s.DB.W.Exec(`UPDATE entities SET revision=1 WHERE id=?`, id); err == nil {
		t.Fatal("counter decrease accepted")
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.Body != before.Body || after.DeletedAt != "" {
		t.Fatalf("exhausted write changed state: %+v", after)
	}
	if target, err := s.PageID(ctx, "Forbidden"); err != nil || target != 0 {
		t.Fatalf("target after exhaustion: %d %v", target, err)
	}
	normal, _, err := s.CreatePage(ctx, "cli", "Canceled witness", "before", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, normal)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	err = s.Do(canceled, "cli", func(tx *Tx) error {
		if _, err := tx.SetBody(normal, "changed [[Canceled target]]"); err != nil {
			return err
		}
		cancel()
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	current, err := s.PageByID(ctx, normal)
	if err != nil {
		t.Fatal(err)
	}
	if current.Body != p.Body || current.Version != p.Version {
		t.Fatalf("cancellation changed state: %+v", current)
	}
	if target, err := s.PageID(ctx, "Canceled target"); err != nil || target != 0 {
		t.Fatalf("target after cancellation: %d %v", target, err)
	}
	var clock string
	if err := s.DB.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, normal).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", normal, "timestamp bypass", clock); status(err) != 409 {
		t.Fatalf("timestamp token accepted: %v", err)
	}
}

// revisionClock is the edit token and the two audit instants of an entity, read straight from the file.
func revisionClock(t *testing.T, s *Store, id int64) (revision int64, createdAt, updatedAt string) {
	t.Helper()
	if err := s.DB.R.QueryRow(`SELECT revision, created_at, updated_at FROM entities WHERE id = ?`, id).
		Scan(&revision, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	return
}

// editedRows are the day-view rows flagged as edited.
func editedRows(t *testing.T, s *Store, day string) []string {
	t.Helper()
	d, err := s.Day(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range d.Rows {
		if strings.Contains(r.What, "(edited)") {
			out = append(out, r.What+" "+r.Detail)
		}
	}
	return out
}

// A row whose creation transaction is its only write is at revision 1 with updated_at equal to created_at, for every
// kind of page and every way the writer creates one; the day view then does not call it edited. Creation is not an
// edit: the owned preferred name and the typed detail row are part of it (aminte_meta.edit_revisions).
func TestCreationIsNotAnEdit(t *testing.T) {
	const day = "2026-10-04"
	s := fresh(t)
	created := map[string]int64{}
	add := func(name string, id int64, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		created[name] = id
	}
	var id int64
	var err error
	err = s.Do(ctx, "cli", func(tx *Tx) (e error) { id, _, e = tx.CreatePage("Plain note", "text", day); return })
	add("plain page", id, err)
	id, err = s.CreatePerson(ctx, "cli", "Sam Sample", "Sam Sample", "1990-01-02", "")
	add("person", id, err)
	err = s.Do(ctx, "cli", func(tx *Tx) (e error) {
		id, _, e = tx.CreatePage("Linked note", "met [[Sam Sample]] at [[Lakeside]] #health", day)
		return
	})
	add("page with wikilinks", id, err)
	_, err = s.CreateMetric(ctx, "cli", NewMetric{Name: "Sleep", Unit: "h", Note: "hours slept, see [[Lakeside]]"})
	if err == nil {
		id, err = s.PageID(ctx, "Sleep")
	}
	add("metric with a note", id, err)
	_, err = s.CreateMetric(ctx, "cli", NewMetric{Name: "Steps", Unit: "count"})
	if err == nil {
		id, err = s.PageID(ctx, "Steps")
	}
	add("metric without a note", id, err)
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "2026-10-04 Lake.jpg", SHA256: sha('a'), MIME: "image/jpeg",
		Body: "the lake with [[Sam Sample]]", Day: day, Preview: phototest.JPEG(8, 8)})
	add("file", k.ID, err)
	// link targets a body save made: ghosts
	for _, title := range []string{"Lakeside", "health"} {
		id, err = s.PageID(ctx, title)
		add("ghost "+title, id, err)
	}

	for name, id := range created {
		if id == 0 {
			t.Errorf("%s was not created", name)
			continue
		}
		revision, createdAt, updatedAt := revisionClock(t, s, id)
		if revision != 1 || updatedAt != createdAt {
			t.Errorf("%s: revision %d, created %s, updated %s; want revision 1 and updated_at = created_at", name, revision, createdAt, updatedAt)
		}
	}
	if got := editedRows(t, s, day); len(got) != 0 {
		t.Errorf("never-edited pages shown as edited: %v", got)
	}

	// the clock alone is no edit: a skewed updated_at (a deliberate fixture, the revision untouched) is not flagged
	if _, err := s.DB.W.Exec(`UPDATE entities SET updated_at = '2030-01-01T00:00:00.000Z' WHERE id = ?`, created["plain page"]); err != nil {
		t.Fatal(err)
	}
	if got := editedRows(t, s, day); len(got) != 0 {
		t.Errorf("a page whose only change is its updated_at is shown as edited: %v", got)
	}

	// the control: a later change of the page's own content is an edit
	p, err := s.PageByID(ctx, created["plain page"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", p.ID, "text, rewritten", p.Version); err != nil {
		t.Fatal(err)
	}
	if revision, _, _ := revisionClock(t, s, p.ID); revision != 2 {
		t.Errorf("edited page: revision %d, want 2", revision)
	}
	got := editedRows(t, s, day)
	if len(got) != 1 || !strings.Contains(got[0], "Plain note") {
		t.Errorf("edited rows: %v; want only the page that was edited", got)
	}
}

// Creating a page by a first capture of a day writes its text after creating it, which the day view never flags
// (it lists the day page apart, not as a page written that day); a day with no text, only a mood, is untouched.
func TestCapturedDayPageWithoutTextIsAtRevisionOne(t *testing.T) {
	s := fresh(t)
	mood := 4.0
	id, _, err := s.Capture(ctx, "cli", "2026-10-05", "", &mood)
	if err != nil {
		t.Fatal(err)
	}
	if revision, createdAt, updatedAt := revisionClock(t, s, id); revision != 1 || updatedAt != createdAt {
		t.Errorf("day page made by a mood: revision %d, created %s, updated %s", revision, createdAt, updatedAt)
	}
}

// An incoming link is not an edit of the page it points at: a capture that tags #health must not make another
// writer's open edit of the health page stale. The page that links is the one whose content changed.
func TestIncomingLinkDoesNotInvalidateAnOpenEdit(t *testing.T) {
	s := fresh(t)
	health, _, err := s.CreatePage(ctx, "cli", "health", "notes on health", "")
	if err != nil {
		t.Fatal(err)
	}
	topic, _, err := s.CreatePage(ctx, "cli", "Topic", "a topic", "")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other", "another", "")
	if err != nil {
		t.Fatal(err)
	}
	open, err := s.PageByID(ctx, health) // the page another writer holds open
	if err != nil {
		t.Fatal(err)
	}
	_, _, updatedAt := revisionClock(t, s, health)

	// an unrelated capture, a body save of another page and a link made by hand all point at it
	day, _, err := s.Capture(ctx, "cli", "2026-10-06", "ran, felt fine #health", nil)
	if err != nil {
		t.Fatal(err)
	}
	topicPage, err := s.PageByID(ctx, topic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "api", topic, "about [[health]]", topicPage.Version); err != nil {
		t.Fatal(err)
	}
	if err := addLink(s, "ui", topic, health, "part-of", "context"); err != nil {
		t.Fatal(err)
	}
	if err := addLink(s, "ui", other, health, "part-of", ""); err != nil {
		t.Fatal(err)
	}
	if err := dropLink(s, "ui", other, health, "part-of"); err != nil {
		t.Fatal(err)
	}
	now, err := s.PageByID(ctx, health)
	if err != nil {
		t.Fatal(err)
	}
	if len(now.In) < 2 {
		t.Fatalf("the page is not linked to: %+v", now.In)
	}
	if now.Version != open.Version {
		t.Errorf("incoming links moved the version of the page they point at: %s -> %s", open.Version, now.Version)
	}
	if _, _, upd := revisionClock(t, s, health); upd != updatedAt {
		t.Errorf("incoming links moved updated_at: %s -> %s", updatedAt, upd)
	}
	if _, err := s.SaveBody(ctx, "ui", health, "notes on health, rewritten", open.Version); err != nil {
		t.Errorf("the open edit was refused after unrelated incoming links: %v", err)
	}
	// the pages that did the linking are the ones that changed
	if revision, _, _ := revisionClock(t, s, day); revision < 2 {
		t.Errorf("the day page that captured the tag is at revision %d", revision)
	}
}
