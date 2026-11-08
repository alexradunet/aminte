package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"aminte/internal/photo/phototest"
)

// TestSetCoverKeepsThePhotoAndShowsItFirst: a new photo is kept as a file with no day and no place, and its embed
// goes first in the page's body, so it is the person's picture. The same picture again writes nothing. A kept file
// is named by its title. A title taken that day gets a number.
func TestSetCoverKeepsThePhotoAndShowsItFirst(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", ana, "Met at school.", "1"); err != nil {
		t.Fatal(err)
	}
	first, err := s.SetCover(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	body := func() string {
		t.Helper()
		p, err := s.PageByID(ctx, ana)
		if err != nil {
			t.Fatal(err)
		}
		return p.Body
	}
	if got, want := body(), "![[Ana 2026-10-15.jpg]]\n\nMet at school."; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	f, err := s.PageByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "file" || f.Title != "Ana 2026-10-15.jpg" || f.Day != "" {
		t.Errorf("the file page = %s %q day %q, want a file with no day", f.Type, f.Title, f.Day)
	}
	var days int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM entities WHERE entity_type = 'page' AND day IS NOT NULL`).Scan(&days); err != nil || days != 0 {
		t.Errorf("day pages = %d, %v: a picture of a person is not put in a day", days, err)
	}

	if _, err := s.SetCover(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic}); err != nil {
		t.Fatal(err)
	}
	if got := body(); strings.Count(got, "![[") != 1 {
		t.Errorf("the same picture again changed the body: %q", got)
	}

	second, err := s.SetCover(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('b'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := body(), "![[Ana 2026-10-15 2.jpg]]\n\n![[Ana 2026-10-15.jpg]]\n\nMet at school."; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	rows, err := s.NamedMentions(ctx, Scope{}, 10, 0)
	if err != nil || len(rows) != 1 || rows[0].Picture != second.ID {
		t.Errorf("NamedMentions = %+v, %v, want the picture %d", rows, err, second.ID)
	}

	// A kept file by its title: the older one goes first again.
	if _, err := s.SetCoverOf(ctx, "cli", ana, "Ana 2026-10-15.jpg"); err != nil {
		t.Fatal(err)
	}
	if got := body(); !strings.HasPrefix(got, "![[Ana 2026-10-15.jpg]]\n\n") {
		t.Errorf("body = %q, want the older picture first", got)
	}
}

// TestSetCoverRefuses: a page that does not exist, a deleted page, a picture with no preview and a title that is
// not a kept picture are refused, and nothing is written.
func TestSetCoverRefuses(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.CreatePerson(ctx, "cli", "Gone", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Plain", "words", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		err    error
		status int
	}{
		{"no page", func() error {
			_, e := s.SetCover(ctx, "cli", 999, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
			return e
		}(), 404},
		{"deleted page", func() error {
			_, e := s.SetCover(ctx, "cli", gone, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
			return e
		}(), 409},
		{"no preview", func() error {
			_, e := s.SetCover(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/heic"})
			return e
		}(), 422},
		{"a file with no picture", func() error { _, e := s.SetCoverOf(ctx, "cli", ana, "Scan.pdf"); return e }(), 422},
		{"not a file", func() error { _, e := s.SetCoverOf(ctx, "cli", ana, "Plain"); return e }(), 422},
		{"no such title", func() error { _, e := s.SetCoverOf(ctx, "cli", ana, "Nothing.jpg"); return e }(), 422},
		{"a new photo on a file page", func() error {
			_, e := s.SetCover(ctx, "cli", pageByTitle(t, s, "Scan.pdf").ID, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
			return e
		}(), 422},
		{"a kept picture on a file page", func() error { _, e := s.SetCoverOf(ctx, "cli", pageByTitle(t, s, "Scan.pdf").ID, "Scan.pdf"); return e }(), 422},
	} {
		if status(c.err) != c.status {
			t.Errorf("%s: %v (status %d), want %d", c.name, c.err, status(c.err), c.status)
		}
	}
	var files int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM files`).Scan(&files); err != nil || files != 1 {
		t.Errorf("files = %d, %v, want only Scan.pdf", files, err)
	}
	if p, err := s.PageByID(ctx, ana); err != nil || p.Body != "" {
		t.Errorf("Ana's body = %q, %v, want it empty", p.Body, err)
	}
}

// TestSetCoverOfAnyTypeKeepsNoDay: a picture is kept with no day, also when its type is not image/*: a PDF sent with
// a preview. keep gives no day of its own only to an image/* type, so setCover sets unknownCaptureDay itself.
func TestSetCoverOfAnyTypeKeepsNoDay(t *testing.T) {
	s := fresh(t)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.SetCover(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "application/pdf", Preview: phototest.JPEG(8, 8)})
	if err != nil {
		t.Fatal(err)
	}
	var null bool
	if err := s.DB.R.QueryRowContext(ctx, `SELECT day IS NULL FROM entities WHERE id = ?`, k.ID).Scan(&null); err != nil || !null {
		t.Errorf("the picture's day is NULL: %v, %v; want true", null, err)
	}
	if k.Day != "" || k.Embedded {
		t.Errorf("the picture got a day of its own: %+v", k)
	}
}

// TestRefusedSetCoverOfReturnsNoSync: when the write of SetCoverOf rolls back, the answer is a zero Sync, not
// the links of a body that was never saved. A temporary trigger refuses the last statement of the body's link sync:
// the delete of a link that the new body does not name.
func TestRefusedSetCoverOfReturnsNoSync(t *testing.T) {
	s := fresh(t)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	bo, err := s.CreatePerson(ctx, "cli", "Bo", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Ana.jpg", SHA256: sha('a'), MIME: "image/jpeg", Preview: phototest.JPEG(8, 8)}); err != nil {
		t.Fatal(err)
	}
	// Deliberate damage: a wikilink that Ana's empty body does not name, so the link sync deletes it.
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'wikilink', '2026-10-16T00:00:00.000Z', 'cli')`, ana, bo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.ExecContext(ctx, `CREATE TEMP TRIGGER refuse_link_delete BEFORE DELETE ON links BEGIN SELECT RAISE(ABORT, 'the test refuses the delete'); END`); err != nil {
		t.Fatal(err)
	}
	r, err := s.SetCoverOf(ctx, "cli", ana, "Ana.jpg")
	if status(err) != 422 {
		t.Fatalf("SetCoverOf = %v (status %d), want the 422 of the refused delete", err, status(err))
	}
	if !reflect.DeepEqual(r, Sync{}) {
		t.Errorf("a refused SetCoverOf returned %+v, want a zero Sync", r)
	}
	if p, err := s.PageByID(ctx, ana); err != nil || p.Body != "" {
		t.Errorf("Ana's body = %q, %v, want it empty: the write rolled back", p.Body, err)
	}
}

// TestNamedMentionsPictureIsTheFirstEmbedThatIsAKeptPicture: a title that names a plain page is skipped, a person
// with no embed has no picture, and every person gets the file of their own first picture.
func TestNamedMentionsPictureIsTheFirstEmbedThatIsAKeptPicture(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	a, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreatePerson(ctx, "cli", "Bo", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePerson(ctx, "cli", "Cy", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Plain", "Only words.", ""); err != nil {
		t.Fatal(err)
	}
	pa, err := s.SetCover(ctx, "cli", a, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := s.SetCover(ctx, "cli", b, "2026-10-15", FileIn{SHA256: sha('b'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	// Bo's body now starts with the picture; put a plain page's embed before it.
	if _, err := s.SaveBody(ctx, "cli", b, "![[Plain]]\n\n![[Bo 2026-10-15.jpg]]", "2"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.NamedMentions(ctx, Scope{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"Ana": pa.ID, "Bo": pb.ID, "Cy": 0}
	if len(rows) != len(want) {
		t.Fatalf("NamedMentions = %+v, want %d people", rows, len(want))
	}
	for _, r := range rows {
		if r.Picture != want[r.Title] {
			t.Errorf("%s: Picture = %d, want %d", r.Title, r.Picture, want[r.Title])
		}
	}
}

// TestATxPictureThatFailsReturnsNoResult: the Tx methods setCover and setCoverOf return no result with an error,
// also when the save of the body failed after its links were synced, so a write that goes on after the error has no
// result to report. A temporary trigger refuses the delete of a stray wikilink row: the save of the body then fails at
// its last step. The write is rolled back at the end.
func TestATxPictureThatFailsReturnsNoResult(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	stray, err := s.CreatePerson(ctx, "cli", "Stray", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Ana.jpg", SHA256: sha('a'), MIME: "image/jpeg", Preview: pic}); err != nil {
		t.Fatal(err)
	}
	end := errors.New("the test ends the write")
	err = s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.tx.ExecContext(tx.ctx, `INSERT INTO links(from_id, to_id, kind, created_at, source)
			VALUES (?, ?, 'wikilink', '2026-10-16T00:00:00.000Z', 'cli')`, ana, stray); err != nil {
			return err
		}
		if _, err := tx.tx.ExecContext(tx.ctx, `CREATE TEMP TRIGGER refuse_a_link_delete BEFORE DELETE ON links
			BEGIN SELECT RAISE(ABORT, 'synthetic fault'); END`); err != nil {
			return err
		}
		k, err := tx.setCover(ana, "2026-10-16", FileIn{SHA256: sha('b'), MIME: "image/jpeg", Preview: pic})
		if err == nil || !reflect.DeepEqual(k, Kept{}) {
			t.Errorf("SetCover = %+v, %v; want no result and the fault", k, err)
		}
		r, err := tx.setCoverOf(ana, "Ana.jpg")
		if err == nil || !reflect.DeepEqual(r, Sync{}) {
			t.Errorf("SetCoverOf = %+v, %v; want no result and the fault", r, err)
		}
		return end
	})
	if !errors.Is(err, end) {
		t.Errorf("Do = %v, want the end of the test", err)
	}
}

// TestEveryPageHasACover: a plain page, a metric and a day page have a cover: the first live file with a
// preview that the body embeds, by any of its names. A day page takes the photo of its day with no action. A
// tombstoned file and a file with no picture are passed over. A file page has no cover: its own picture stands for
// it.
func TestEveryPageHasACover(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	keep := func(f FileIn) int64 {
		t.Helper()
		k, err := s.AddFile(ctx, "cli", f)
		if err != nil {
			t.Fatal(err)
		}
		return k.ID
	}
	lake := keep(FileIn{Title: "Lake.jpg", SHA256: sha('a'), MIME: "image/jpeg", Day: "2026-10-01", Preview: pic})
	gone := keep(FileIn{Title: "Gone.jpg", SHA256: sha('b'), MIME: "image/jpeg", Preview: pic})
	keep(FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf"})
	if _, err := s.Rename(ctx, "cli", lake, "Lake at dusk.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Trip", "Away.\n\n![[Gone.jpg]] ![[Scan.pdf]] ![[Lake.jpg]]", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil { // after the save: a save that links a tombstone revives it
		t.Fatal(err)
	}
	place, _, err := s.CreatePage(ctx, "cli", "Shore", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", place, "![[Lake at dusk.jpg]]", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Swims"}); err != nil {
		t.Fatal(err)
	}
	swims := pageByTitle(t, s, "Swims")
	if _, err := s.SaveBody(ctx, "cli", swims.ID, "Laps.\n![[Lake.jpg]]", swims.Version); err != nil {
		t.Fatal(err)
	}
	want := func(title string, c *Cover) {
		t.Helper()
		got := pageByTitle(t, s, title).Cover
		if (got == nil) != (c == nil) || got != nil && *got != *c {
			t.Errorf("%s: cover = %+v, want %+v", title, got, c)
		}
	}
	want("Trip", &Cover{ID: lake, Title: "Lake at dusk.jpg", Embed: "Lake.jpg"})
	want("Shore", &Cover{ID: lake, Title: "Lake at dusk.jpg", Embed: "Lake at dusk.jpg"})
	want("Swims", &Cover{ID: lake, Title: "Lake at dusk.jpg", Embed: "Lake.jpg"})
	want("2026-10-01", &Cover{ID: lake, Title: "Lake at dusk.jpg", Embed: "Lake.jpg"})
	want("Lake at dusk.jpg", nil)
	want("Scan.pdf", nil)
}

// TestTheListsShowTheCover: each list of Browse gives the cover of a row as its picture, and 0 for a row with none. A
// file filed in a category gives its own picture.
func TestTheListsShowTheCover(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "Lake.jpg", SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	scan, err := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('b'), MIME: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FilePages(ctx, "cli", []int64{k.ID, scan.ID}, "Trips", "", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Away", "![[Lake.jpg]]", "Trips"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Plain", "words", ""); err != nil {
		t.Fatal(err)
	}
	place, _, err := s.CreatePage(ctx, "cli", "Shore", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", place, "![[Lake.jpg]]", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Swims"}); err != nil {
		t.Fatal(err)
	}
	swims := pageByTitle(t, s, "Swims")
	if _, err := s.SaveBody(ctx, "cli", swims.ID, "![[Lake.jpg]]", swims.Version); err != nil {
		t.Fatal(err)
	}
	pages, err := s.PlainPages(ctx, Scope{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, r := range pages {
		got["page "+r.Title] = r.Picture
	}
	metrics, err := s.MetricRows(ctx, Scope{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range metrics {
		got["metric "+r.Title] = r.Picture
	}
	filed, err := s.FiledIn(ctx, pageByTitle(t, s, "Trips").ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range filed {
		got["filed "+r.Title] = r.Picture
	}
	want := map[string]int64{"page Away": k.ID, "page Plain": 0, "page Trips": 0, "page Shore": k.ID, "metric Mood": 0, "metric Swims": k.ID,
		"filed Away": k.ID, "filed Lake.jpg": k.ID, "filed Scan.pdf": 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pictures = %v, want %v", got, want)
	}
}

// TestPicturesAreTheFilesThatKeepOne: the covers a page may take are the live files with a preview, of any kind,
// newest day first; a file with no preview and a tombstoned one are not among them.
func TestPicturesAreTheFilesThatKeepOne(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	for _, f := range []FileIn{
		{Title: "Old.jpg", SHA256: sha('a'), MIME: "image/jpeg", Day: "2026-01-01", Preview: pic},
		{Title: "Frame.mp4", SHA256: sha('b'), MIME: "video/mp4", Day: "2026-03-01", Preview: pic},
		{Title: "Memo.m4a", SHA256: sha('c'), MIME: "audio/mp4", Day: "2026-04-01"},
		{Title: "Gone.jpg", SHA256: sha('d'), MIME: "image/jpeg", Day: "2026-05-01", Preview: pic},
		{Title: "Scan.pdf", SHA256: sha('e'), MIME: "application/pdf", Day: "2026-02-01", Preview: pic},
	} {
		if _, err := s.AddFile(ctx, "cli", f); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Tombstone(ctx, "cli", pageByTitle(t, s, "Gone.jpg").ID); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Pictures(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Title)
	}
	if want := []string{"Frame.mp4", "Scan.pdf", "Old.jpg"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Pictures = %v, want %v", got, want)
	}
}

// TestSetCoverOfAPictureTheBodyEmbeds: a picture the body embeds already becomes the cover by trading places with the
// cover's embed, labels and spellings kept, so nothing is added and nothing is lost; switching back gives the
// body it had. A picture the body does not embed goes first. Pictures lists the others, in the order of the body.
func TestSetCoverOfAPictureTheBodyEmbeds(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	for i, title := range []string{"A.jpg", "B.jpg", "K.jpg"} {
		if _, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: sha(byte('a' + i)), MIME: "image/jpeg", Preview: pic}); err != nil {
			t.Fatal(err)
		}
	}
	const start = "Swam.\n\n![[A.jpg|the lake]] then ![[b.JPG]]\n\nDinner."
	trip, _, err := s.CreatePage(ctx, "cli", "Trip", start, "")
	if err != nil {
		t.Fatal(err)
	}
	state := func() (string, string) {
		t.Helper()
		p, err := s.PageByID(ctx, trip)
		if err != nil || p.Cover == nil {
			t.Fatalf("Trip: %v %+v", err, p)
		}
		return p.Body, p.Cover.Title
	}
	if body, cover := state(); body != start || cover != "A.jpg" {
		t.Fatalf("at the start: %q %q", body, cover)
	}
	if _, err := s.SetCoverOf(ctx, "cli", trip, "B.jpg"); err != nil {
		t.Fatal(err)
	}
	if body, cover := state(); body != "Swam.\n\n![[b.JPG]] then ![[A.jpg|the lake]]\n\nDinner." || cover != "B.jpg" {
		t.Errorf("after B: %q %q", body, cover)
	}
	if _, err := s.SetCoverOf(ctx, "cli", trip, "A.jpg"); err != nil {
		t.Fatal(err)
	}
	if body, _ := state(); body != start {
		t.Errorf("back to A: %q, want %q", body, start)
	}
	if _, err := s.SetCoverOf(ctx, "cli", trip, "K.jpg"); err != nil {
		t.Fatal(err)
	}
	if body, cover := state(); body != "![[K.jpg]]\n\n"+start || cover != "K.jpg" {
		t.Errorf("after K, kept and not embedded: %q %q", body, cover)
	}
}

// TestANewCoverOfADayIsNamedForTheDay: a new photo set as a day page's cover with no title is named for the day alone,
// "2026-10-04.jpg", then "2026-10-04 2.jpg"; any other page's is named for the page and today.
func TestANewCoverOfADayIsNamedForTheDay(t *testing.T) {
	s := fresh(t)
	day, _, err := s.Capture(ctx, "cli", "2026-10-04", "A walk by the lake.", nil)
	if err != nil {
		t.Fatal(err)
	}
	trip, _, err := s.CreatePage(ctx, "cli", "Trip", "Away.", "")
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct {
		page int64
		want string
	}{{day, "2026-10-04.jpg"}, {day, "2026-10-04 2.jpg"}, {trip, "Trip 2026-10-17.jpg"}} {
		k, err := s.SetCover(ctx, "cli", c.page, "2026-10-17", FileIn{SHA256: sha(byte('a' + i)), MIME: "image/jpeg", Preview: phototest.JPEG(8, 8)})
		if err != nil {
			t.Fatal(err)
		}
		if p, err := s.PageByID(ctx, k.ID); err != nil || p.Title != c.want {
			t.Errorf("the new cover %d: %q, %v; want %q", i, p.Title, err, c.want)
		}
	}
	if p, err := s.PageByID(ctx, day); err != nil || p.Cover == nil || p.Cover.Title != "2026-10-04 2.jpg" || p.Day != "2026-10-04" {
		t.Errorf("the day page: %v %+v", err, p)
	}
}
