package core

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"aminte/internal/photo/phototest"
	"aminte/text"
)

func sha(c byte) string { return strings.Repeat(string(c), 64) }

func TestAddFileCreatesAFilePage(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "2026-10-04 Lake.jpg", SHA256: strings.ToUpper(sha('a')), MIME: "image/HEIC",
		Body: "The lake at dawn with [[Sam]].", Day: "2026-10-04", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	if k.Existing || k.Promoted || strings.Join(k.Sync.Created, ",") != "Sam" {
		t.Errorf("kept: %+v", k)
	}
	p, err := s.PageByID(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "file" || p.Day != "2026-10-04" || p.File == nil || p.File.SHA256 != sha('a') || p.File.MIME != "image/heic" || !p.File.Preview {
		t.Errorf("page: %+v, file %+v", p, p.File)
	}
	if got, _ := s.Preview(ctx, k.ID); !bytes.Equal(got, pic) {
		t.Error("the preview is not the one kept")
	}
	if got, _ := s.PreviewByTitle(ctx, "2026-10-04 LAKE.jpg"); !bytes.Equal(got, pic) {
		t.Error("the preview is not found by its title's key")
	}
	if got, _ := s.PreviewByTitle(ctx, "Nothing"); got != nil {
		t.Error("a title with no file has a preview")
	}
	// a tombstoned file page is not embedded, but its picture stays readable by its id
	if err := s.Tombstone(ctx, "cli", k.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PreviewByTitle(ctx, "2026-10-04 Lake.jpg"); err != nil || got != nil {
		t.Errorf("a tombstoned file page has a preview by its title: %d bytes, %v", len(got), err)
	}
	if got, _ := s.Preview(ctx, k.ID); !bytes.Equal(got, pic) {
		t.Error("a tombstoned file page has no preview by its id")
	}
}

func TestAddFileKeepsAnOriginalOnce(t *testing.T) {
	s := fresh(t)
	k, err := s.AddFile(ctx, "api", FileIn{Title: "Memo.m4a", SHA256: sha('b'), MIME: "audio/mp4", Body: "a transcript"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.AddFile(ctx, "import:audio", FileIn{Title: "Other name.m4a", SHA256: sha('b'), MIME: "audio/mp4", Body: "another text"})
	if err != nil || !again.Existing || again.ID != k.ID {
		t.Fatalf("the same original from another source: %+v %v", again, err)
	}
	if id, _ := s.PageID(ctx, "Other name.m4a"); id != 0 {
		t.Error("the re-send made a page")
	}
	// a missing preview is filled; an existing one is never replaced
	pic := phototest.JPEG(8, 8)
	if k2, _ := s.AddFile(ctx, "cli", FileIn{SHA256: sha('b'), MIME: "audio/mp4", Preview: pic}); !k2.PreviewAdded {
		t.Errorf("a missing preview was not added: %+v", k2)
	}
	other := append(append([]byte{}, pic[:len(pic)-2]...), 0, 0xFF, 0xD9)
	if k3, _ := s.AddFile(ctx, "cli", FileIn{SHA256: sha('b'), MIME: "audio/mp4", Preview: other}); k3.PreviewAdded {
		t.Error("an existing preview was replaced")
	}
	if got, _ := s.Preview(ctx, k.ID); !bytes.Equal(got, pic) {
		t.Error("the preview changed")
	}
	// a tombstoned file is left alone
	k4, _ := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf"})
	if err := s.Tombstone(ctx, "cli", k4.ID); err != nil {
		t.Fatal(err)
	}
	if k5, _ := s.AddFile(ctx, "cli", FileIn{SHA256: sha('c'), MIME: "application/pdf", Preview: pic}); !k5.Existing || !k5.Deleted || k5.PreviewAdded {
		t.Errorf("a tombstoned file: %+v", k5)
	}
}

func TestAddFilePromotesAGhost(t *testing.T) {
	s := fresh(t)
	day, _, err := s.Capture(ctx, "cli", "2026-10-04", "Dawn: ![[Lake.jpg]]", nil)
	if err != nil {
		t.Fatal(err)
	}
	ghost, _ := s.PageID(ctx, "Lake.jpg")
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "lake.jpg", SHA256: sha('d'), MIME: "image/jpeg", Body: "With [[Bob]]"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, ghost)
	if !k.Promoted || k.ID != ghost || p.Type != "file" || p.Title != "Lake.jpg" || p.Body != "With [[Bob]]" || titles(p.Out, "wikilink") != "Bob" {
		t.Errorf("promoted: %+v, page %+v", k, p)
	}
	if titles(p.In, "wikilink") != "2026-10-04" {
		t.Errorf("the day's embed does not land on the file: %v (day %d)", p.In, day)
	}
	// a page with text keeps it; text on both sides is refused
	if _, _, err := s.CreatePage(ctx, "cli", "Notes.pdf", "my own words", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Notes.pdf", SHA256: sha('e'), MIME: "application/pdf", Body: "OCR text"}); status(err) != 409 {
		t.Errorf("two texts merged: %v", err)
	}
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "Notes.pdf", SHA256: sha('e'), MIME: "application/pdf"})
	if p, _ := s.PageByID(ctx, k.ID); err != nil || p.Body != "my own words" || p.Type != "file" {
		t.Errorf("a page with text, no text sent: %v %+v", err, p)
	}
}

func TestFilePromotionDay(t *testing.T) {
	s := fresh(t)
	dayLists := func(day, title string) bool {
		t.Helper()
		d, err := s.Day(ctx, day)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range d.Rows {
			if strings.HasPrefix(r.What, "page") && r.Detail == title {
				return true
			}
		}
		return false
	}

	if _, _, err := s.Capture(ctx, "cli", "2026-10-04", "Dawn: ![[Lake.jpg]]", nil); err != nil {
		t.Fatal(err)
	}
	ghost, _ := s.PageID(ctx, "Lake.jpg")
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "lake.jpg", SHA256: sha('0'), MIME: "image/jpeg", Body: "With [[Bob]].", Day: "2026-10-04"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, ghost)
	if !k.Promoted || k.ID != ghost || p.Type != "file" || p.Title != "Lake.jpg" || p.Body != "With [[Bob]]." || p.Day != "2026-10-04" || titles(p.In, "wikilink") != "2026-10-04" || titles(p.Out, "wikilink") != "Bob" {
		t.Errorf("explicit-day promotion: kept %+v, page %+v", k, p)
	}
	if !dayLists("2026-10-04", "Lake.jpg") {
		t.Error("the promoted explicit-day file is absent from its day view")
	}
	again, err := s.AddFile(ctx, "cli", FileIn{Title: "Other Lake.jpg", SHA256: sha('0'), MIME: "image/jpeg", Day: "2026-10-06"})
	if err != nil || !again.Existing || again.ID != ghost {
		t.Fatalf("re-send by hash: %+v %v", again, err)
	}
	if p, _ = s.PageByID(ctx, ghost); p.Day != "2026-10-04" || p.Title != "Lake.jpg" || p.Body != "With [[Bob]]." {
		t.Errorf("re-send changed the promoted file page: %+v", p)
	}

	if _, _, err := s.Capture(ctx, "cli", "2026-10-05", "Trip: ![[Taken.jpg]]", nil); err != nil {
		t.Fatal(err)
	}
	takenGhost, _ := s.PageID(ctx, "Taken.jpg")
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "taken.jpg", SHA256: sha('1'), MIME: "image/jpeg", Taken: "2026-10-05"})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ = s.PageByID(ctx, takenGhost); !k.Promoted || p.Day != "2026-10-05" || !dayLists("2026-10-05", "Taken.jpg") {
		t.Errorf("taken-day promotion: kept %+v, page %+v", k, p)
	}

	var datedGhost int64
	if err := s.Do(ctx, "cli", func(tx *Tx) (err error) {
		datedGhost, err = tx.insertPage("page", "Already dated.jpg", text.TitleKey("Already dated.jpg"), "2026-10-01", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "Already dated.jpg", SHA256: sha('2'), MIME: "image/jpeg", Day: "2026-10-07"})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ = s.PageByID(ctx, datedGhost); !k.Promoted || p.Day != "2026-10-01" || !dayLists("2026-10-01", "Already dated.jpg") || dayLists("2026-10-07", "Already dated.jpg") {
		t.Errorf("promotion overwrote an existing day: kept %+v, page %+v", k, p)
	}

	if _, _, err := s.Capture(ctx, "cli", "2026-10-08", "Dry: ![[Dry.jpg]]", nil); err != nil {
		t.Fatal(err)
	}
	dryGhost, _ := s.PageID(ctx, "Dry.jpg")
	k, err = s.TryAddFile(ctx, "cli", FileIn{Title: "dry.jpg", SHA256: sha('3'), MIME: "image/jpeg", Day: "2026-10-08"})
	if err != nil || !k.Promoted || k.ID != 0 {
		t.Fatalf("dry-run promotion report: %+v %v", k, err)
	}
	if p, _ = s.PageByID(ctx, dryGhost); p.Type != "page" || p.Day != "" || p.File != nil {
		t.Errorf("dry-run promotion wrote to storage: %+v", p)
	}

	var conflictID int64
	if err := s.Do(ctx, "cli", func(tx *Tx) (err error) {
		conflictID, err = tx.insertPage("page", "Conflict.pdf", text.TitleKey("Conflict.pdf"), "2026-10-02", "my own words")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Conflict.pdf", SHA256: sha('4'), MIME: "application/pdf", Day: "2026-10-09", Body: "OCR text"}); status(err) != 409 {
		t.Fatalf("body conflict: %v", err)
	}
	if p, _ = s.PageByID(ctx, conflictID); p.Type != "page" || p.Day != "2026-10-02" || p.Body != "my own words" || p.File != nil {
		t.Errorf("body conflict changed storage: %+v", p)
	}

	// A file that is not a photo, with no day: it takes the day it is kept, the day of the store's clock, and no day
	// page shows it. A photo with no day has no day (TestUnknownPhotoDayHasNoCaptureOrPlaceSideEffects).
	s = fresh(t)
	s.Clock, s.Zone = func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }, time.UTC
	if _, _, err := s.Capture(ctx, "cli", "2099-01-01", "Undated: ![[scan.pdf]]", nil); err != nil {
		t.Fatal(err)
	}
	scanGhost, _ := s.PageID(ctx, "scan.pdf")
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "scan.pdf", SHA256: sha('5'), MIME: "application/pdf", Preview: phototest.JPEG(8, 8)})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = s.PageByID(ctx, scanGhost)
	if !k.Promoted || k.ID != scanGhost {
		t.Errorf("undated keep did not promote the ghost in place: %+v (ghost %d)", k, scanGhost)
	}
	if p.Day != "2026-10-10" {
		t.Errorf("undated keep used day %q, want the day of the store's clock 2026-10-10", p.Day)
	}
	if k.Day != "" || k.Embedded {
		t.Errorf("undated file got its own-day effects: %+v", k)
	}
	if dayID, _ := s.PageID(ctx, p.Day); dayID != 0 {
		t.Errorf("undated file made an automatic day page %d for %s", dayID, p.Day)
	}
}

func TestAddFileRefuses(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreatePerson(ctx, "cli", "Bob Sample", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Bob Sample", SHA256: sha('a'), MIME: "image/jpeg"}); status(err) != 422 || !strings.Contains(err.Error(), "only a plain page is promoted") {
		t.Errorf("a person's title: %v, want the 422 of entities_promote_from_page", err)
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-04", "x", nil); err != nil {
		t.Fatal(err)
	}
	// a day page is never a file: entities_day_page_plain refuses the promotion, and nothing is written
	before := everyRow(t, s)
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "2026-10-04", SHA256: sha('a'), MIME: "image/jpeg"}); status(err) != 422 || !strings.Contains(err.Error(), "entities_day_page_plain") {
		t.Errorf("a day page's title: %v, want the 422 of entities_day_page_plain", err)
	}
	if after := everyRow(t, s); after != before {
		t.Errorf("a refused file wrote:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for name, f := range map[string]FileIn{
		"a short hash":     {Title: "A", SHA256: sha('a')[:63], MIME: "image/jpeg"},
		"a hash not hex":   {Title: "A", SHA256: sha('g'), MIME: "image/jpeg"},
		"no type":          {Title: "A", SHA256: sha('a'), MIME: ""},
		"a bad type":       {Title: "A", SHA256: sha('a'), MIME: "jpeg"},
		"a bad title":      {Title: "a/b", SHA256: sha('a'), MIME: "image/jpeg"},
		"a bad day":        {Title: "A", SHA256: sha('a'), MIME: "image/jpeg", Day: "2026-13-01"},
		"a PNG as preview": {Title: "A", SHA256: sha('a'), MIME: "image/jpeg", Preview: []byte("\x89PNG\r\n\x1a\n")},
	} {
		if _, err := s.AddFile(ctx, "cli", f); status(err) != 422 {
			t.Errorf("%s: %v", name, err)
		}
	}
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "Lake.jpg", SHA256: sha('f'), MIME: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if renamed, err := s.Rename(ctx, "cli", k.ID, "Lake 2.jpg"); err != nil || renamed != k.ID {
		t.Fatalf("stable file rename: %d %v", renamed, err)
	}
	if old, err := s.PageID(ctx, "Lake.jpg"); err != nil || old != k.ID {
		t.Fatalf("old file handle: %d %v", old, err)
	}
	if r, err := s.Integrity(ctx); err != nil || !r.OK {
		t.Errorf("integrity: %+v %v", r, err)
	}
}

// The rules of a file's type and preview are held by files_mime and files_preview: the writer sends the row and the
// 422 is SQLite's sentence, which names the constraint. Only the long edge of a preview is the writer's rule.
func TestAddFileLetsTheSchemaRefuseATypeAndAPreview(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	for name, tc := range map[string]struct {
		f    FileIn
		rule string
	}{
		"no type":          {FileIn{Title: "A", SHA256: sha('a'), MIME: ""}, "files_mime"},
		"a bad type":       {FileIn{Title: "A", SHA256: sha('a'), MIME: "jpeg"}, "files_mime"},
		"a type with a /":  {FileIn{Title: "A", SHA256: sha('a'), MIME: "a/b/c"}, "files_mime"},
		"a big preview":    {FileIn{Title: "A", SHA256: sha('a'), MIME: "image/jpeg", Preview: append(append([]byte{}, pic...), make([]byte, 1<<20)...)}, "files_preview"},
		"a wide preview":   {FileIn{Title: "A", SHA256: sha('a'), MIME: "image/jpeg", Preview: phototest.JPEG(1601, 10)}, "long edge"},
		"a PNG as preview": {FileIn{Title: "A", SHA256: sha('a'), MIME: "image/jpeg", Preview: []byte("\x89PNG\r\n\x1a\n")}, "JPEG"},
	} {
		before := everyRow(t, s)
		_, err := s.AddFile(ctx, "cli", tc.f)
		if status(err) != 422 || !strings.Contains(err.Error(), tc.rule) {
			t.Errorf("%s: %v, want a 422 that names %q", name, err, tc.rule)
		}
		if after := everyRow(t, s); after != before {
			t.Errorf("%s: a refused file wrote:\nbefore:\n%s\nafter:\n%s", name, before, after)
		}
	}
}

func TestNormalMIME(t *testing.T) {
	if NormalMIME("Text/Plain; charset=utf-8") != "text/plain" || NormalMIME("a/b/c") != "" || NormalMIME("") != "" {
		t.Error("NormalMIME")
	}
}

// TestTheDayPageBodyQueryFindsTheDayPageByAnIndex: dayPageBody, the query with which keep reads the body of the day
// page of a photo's day, finds the day page and not a page that only has that day. The test runs the query and plans
// it by itself: SQLite searches by an index and scans no table, so the cost of the query does not grow with the
// number of pages.
func TestTheDayPageBodyQueryFindsTheDayPageByAnIndex(t *testing.T) {
	s := fresh(t)
	const day = "2031-02-03"
	if err := s.Do(t.Context(), "cli", func(tx *Tx) error {
		for i := range 200 {
			if _, _, err := tx.CreatePage(fmt.Sprintf("Synthetic lookup page %03d", i), "", day); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := s.DB.W.QueryRowContext(t.Context(), dayPageBody, day).Scan(&body); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("with no day page the lookup found %q, %v; want no row", body, err)
	}
	if _, _, err := s.Capture(t.Context(), "cli", day, "The day's words.", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.W.QueryRowContext(t.Context(), dayPageBody, day).Scan(&body); err != nil || body != "The day's words." {
		t.Fatalf("the lookup found %q, %v; want the day page's body", body, err)
	}
	// Plan the lookup on the writer connection, where keep runs it.
	rows, err := s.DB.W.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+dayPageBody, day)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(plan) == 0 {
		t.Fatal("the lookup has no query plan")
	}
	for _, step := range plan {
		if fields := strings.Fields(step); len(fields) > 0 && fields[0] == "SCAN" {
			t.Errorf("the day page lookup scans a table, not an index: %v", plan)
		}
	}
}

// photo is a kept photo as the writer sees it: a JPEG with a preview, taken on day. Its position is never read (D21).
func photo(c byte, title, day string) FileIn {
	return FileIn{Title: title, SHA256: sha(c), MIME: "image/jpeg", Taken: day, Preview: phototest.JPEG(8, 8)}
}

// TestUnknownPhotoDayHasNoCaptureOrPlaceSideEffects: a photo with no Day and no Taken has an unknown capture day. keep
// finds that from the type and the days alone: the caller does not say it.
func TestUnknownPhotoDayHasNoCaptureOrPlaceSideEffects(t *testing.T) {
	s := fresh(t)
	in := FileIn{Title: "Synthetic photo.jpg", MIME: "image/jpeg", SHA256: strings.Repeat("a", 64), Body: "Selected picture"}
	kept, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.PageByID(ctx, kept.ID)
	if err != nil || page.Day != "" || kept.Day != "" || kept.Embedded {
		t.Fatalf("unknown capture %+v %+v %v", kept, page, err)
	}
	var null bool
	if err := s.DB.R.QueryRow("SELECT day IS NULL FROM entities WHERE id=?", kept.ID).Scan(&null); err != nil || !null {
		t.Fatalf("day is not SQL NULL %v %v", null, err)
	}
	in.Body = "Retry must preserve owner text"
	again, err := s.AddFile(ctx, "cli", in)
	if err != nil || again.ID != kept.ID {
		t.Fatalf("retry %+v %v", again, err)
	}
	page, err = s.PageByID(ctx, kept.ID)
	if err != nil || page.Body != "Selected picture" || page.Day != "" {
		t.Fatalf("retry changed owner %+v %v", page, err)
	}
	var plain int64
	err = s.Do(ctx, "cli", func(tx *Tx) error {
		var e error
		plain, _, e = tx.CreatePage("Promotion.jpg", "Owner body", "2012-03-04")
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.PageByID(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := s.AddFile(ctx, "cli", FileIn{Title: "Promotion.jpg", MIME: "image/jpeg", SHA256: strings.Repeat("b", 64)})
	if err != nil || promoted.ID != plain || promoted.StoredDay != original.Day || promoted.Day != "" {
		t.Fatalf("promotion attribution %+v %v", promoted, err)
	}
	page, err = s.PageByID(ctx, plain)
	if err != nil || page.Day != "2012-03-04" || page.Day != original.Day || page.Body != original.Body {
		t.Fatalf("promotion changed owner %+v %v", page, err)
	}
}

func TestUnknownPhotoDryRunDedupKnownDayMissingPreviewAndTombstone(t *testing.T) {
	s := fresh(t)
	in := photo('c', "Owner.jpg", "2012-03-04")
	preview := in.Preview
	in.Preview = nil
	kept, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := s.PageByID(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	unknown := FileIn{Title: "Different.jpg", MIME: "image/jpeg", SHA256: in.SHA256, Body: "Do not replace", Preview: preview}
	before, err := countRows(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	tried, err := s.TryAddFile(ctx, "cli", unknown)
	if err != nil || tried.ID != kept.ID || tried.StoredDay != "2012-03-04" || tried.Day != "" {
		t.Fatalf("dry dedup %+v %v", tried, err)
	}
	raw, err := s.Preview(ctx, kept.ID)
	if err != nil || raw != nil {
		t.Fatal("dry run filled preview")
	}
	again, err := s.AddFile(ctx, "cli", unknown)
	if err != nil || again.ID != kept.ID || again.StoredDay != "2012-03-04" || again.Embedded {
		t.Fatalf("known attribution %+v %v", again, err)
	}
	after, err := s.PageByID(ctx, kept.ID)
	if err != nil || after.Day != prior.Day || after.Title != prior.Title || after.Body != prior.Body {
		t.Fatal("dedup replaced ownership")
	}
	counts, err := countRows(ctx, s)
	if err != nil || counts.Pages != before.Pages || counts.Links["part-of"] != before.Links["part-of"] {
		t.Fatal("unknown dedup incidental writes")
	}
	if err = s.Tombstone(ctx, "cli", kept.ID); err != nil {
		t.Fatal(err)
	}
	again, err = s.AddFile(ctx, "cli", unknown)
	if err != nil || !again.Deleted || again.StoredDay != prior.Day {
		t.Fatalf("tombstone retry %+v %v", again, err)
	}
	after, err = s.PageByID(ctx, kept.ID)
	if err != nil || after.DeletedAt == "" || after.Day != prior.Day {
		t.Fatal("unknown retry revived tombstone")
	}
}

func daySyncJSON(t *testing.T, k Kept) *Sync {
	t.Helper()
	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		DaySync *Sync `json:"day_sync"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result.DaySync
}

func TestPhotoDaySynchronizationFeedback(t *testing.T) {
	s := fresh(t)
	id, _, err := s.Capture(ctx, "cli", "2026-10-04", "[[Old target]] [[bad/title]]", nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.PageID(ctx, "Old target")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", target); err != nil {
		t.Fatal(err)
	}
	f := FileIn{Title: "New photo.jpg", SHA256: sha('d'), MIME: "image/jpeg", Preview: phototest.JPEG(8, 8), Day: "2026-10-04", Body: "[[File target]]"}
	assert := func(k Kept) {
		t.Helper()
		sync := daySyncJSON(t, k)
		if sync == nil || strings.Join(sync.Revived, ",") != "Old target" || strings.Join(sync.Skipped, ",") != "bad/title" {
			t.Errorf("day sync: %+v", sync)
		}
		if k.Sync == nil || strings.Join(k.Sync.Created, ",") != "File target" {
			t.Errorf("file sync: %+v", k.Sync)
		}
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	dry, err := s.TryAddFile(ctx, "cli", f)
	if err != nil {
		t.Fatal(err)
	}
	assert(dry)
	old, err := s.PageByID(ctx, target)
	if err != nil || old.DeletedAt == "" {
		t.Fatalf("dry revival leaked: %+v %v", old, err)
	}
	after, err := s.PageByID(ctx, id)
	if err != nil || after.Body != before.Body {
		t.Fatal("dry body leaked")
	}
	k, err := s.AddFile(ctx, "cli", f)
	if err != nil {
		t.Fatal(err)
	}
	assert(k)
	old, err = s.PageByID(ctx, target)
	if err != nil || old.DeletedAt != "" {
		t.Fatal("not revived")
	}
	again, err := s.AddFile(ctx, "cli", f)
	if err != nil || again.Embedded || daySyncJSON(t, again) != nil || again.Sync != nil {
		t.Fatalf("no-op: %+v %v", again, err)
	}
}

func TestPhotoEmbedNFDLongTitleDeduplication(t *testing.T) {
	s := fresh(t)
	title := strings.Repeat("é", 100) + ".jpg"
	body := "![[" + strings.Repeat("e\u0301", 100) + ".jpg|existing]]"
	day, _, err := s.Capture(ctx, "cli", "2026-10-04", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: sha('e'), MIME: "image/jpeg", Preview: phototest.JPEG(8, 8), Day: "2026-10-04"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if k.Embedded || p.Body != body || titles(p.Out, "wikilink") != title {
		t.Fatalf("duplicate/graph: %+v %+v", k, p)
	}
}
