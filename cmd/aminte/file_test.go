package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"aminte/internal/core"
	"aminte/internal/db"
	"aminte/internal/db/dbtest"
	"aminte/internal/photo/phototest"
)

// TestFileCommand is `aminte file PATH` on a throwaway database: the original is hashed and never stored, its text
// read from --text, its picture made from it; the same file again keeps one page.
func TestFileCommand(t *testing.T) {
	dir, live := t.TempDir(), dbtest.File(t)
	pic, text := filepath.Join(dir, "IMG_0001.jpg"), filepath.Join(dir, "caption.txt")
	os.WriteFile(pic, phototest.JPEG(64, 48), 0o644)
	os.WriteFile(text, []byte("Dawn at the lake with [[Sam]]."), 0o644)
	e, _, _ := testEnv(t)
	for range 2 {
		if err := runArgs(t.Context(), e, []string{"file", pic, "--db", live, "--text", text, "--day", "2026-10-04"}); err != nil {
			t.Fatal(err)
		}
	}
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	id, _ := s.PageID(context.Background(), "IMG_0001.jpg")
	p, err := s.PageByID(context.Background(), id)
	if err != nil || p.Type != "file" || p.Body != "Dawn at the lake with [[Sam]]." || p.Day != "2026-10-04" || p.File == nil || !p.File.Preview {
		t.Fatalf("the file page: %v %+v", err, p)
	}
	var n int
	d.R.QueryRow(`SELECT count(*) FROM files`).Scan(&n)
	if n != 1 {
		t.Errorf("%d files rows after keeping one file twice", n)
	}
}

// TestCoverCommand is `aminte cover PAGE-ID PHOTO` and `aminte cover PAGE-ID --title T` on a throwaway database:
// a new photo is kept as a file with no day and becomes the page's cover; a kept picture named by its title becomes
// it next. A file page takes no cover, and a call with neither a photo nor a title is refused by the API.
func TestCoverCommand(t *testing.T) {
	dir, live := t.TempDir(), dbtest.File(t)
	pic, other := filepath.Join(dir, "trip.jpg"), filepath.Join(dir, "other.jpg")
	os.WriteFile(pic, phototest.JPEG(64, 48), 0o644)
	os.WriteFile(other, phototest.JPEG(48, 64), 0o644)
	e, _, _ := testEnv(t)
	run := func(args ...string) error { return runArgs(t.Context(), e, append(args, "--db", live)) }
	if err := run("file", other, "--title", "Other.jpg"); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	trip, _, err := s.CreatePage(t.Context(), "cli", "Trip", "Away.", "")
	if err != nil {
		t.Fatal(err)
	}
	cover := func() string {
		t.Helper()
		p, err := s.PageByID(t.Context(), trip)
		if err != nil || p.Cover == nil {
			t.Fatalf("Trip: %v, cover %+v", err, p)
		}
		return p.Cover.Title
	}
	if err := run("cover", strconv.FormatInt(trip, 10), pic, "--title", "Trip cover.jpg"); err != nil {
		t.Fatal(err)
	}
	if got := cover(); got != "Trip cover.jpg" {
		t.Errorf("the cover after a new photo = %q, want Trip cover.jpg", got)
	}
	if f, err := s.PageByID(t.Context(), mustID(t, s, "Trip cover.jpg")); err != nil || f.Type != "file" || f.Day != "" {
		t.Errorf("the new photo: %v %+v, want a file with no day", err, f)
	}
	if err := run("cover", strconv.FormatInt(trip, 10), "--title", "Other.jpg"); err != nil {
		t.Fatal(err)
	}
	if got := cover(); got != "Other.jpg" {
		t.Errorf("the cover after a kept picture = %q, want Other.jpg", got)
	}
	if err := run("cover", strconv.FormatInt(mustID(t, s, "Other.jpg"), 10), "--title", "Other.jpg"); err == nil {
		t.Error("a file page took a cover")
	}
	if err := run("cover", strconv.FormatInt(trip, 10)); err == nil {
		t.Error("cover with neither a photo nor a title was taken")
	}
	// --preview reaches the request even with no PHOTO: the client then reads the named file, and a missing file is an
	// error. A flag that is dropped in silence would give no error.
	missing := filepath.Join(dir, "missing-preview.jpg")
	if err := run("cover", strconv.FormatInt(trip, 10), "--title", "Other.jpg", "--preview", missing); err == nil || !strings.Contains(err.Error(), "missing-preview.jpg") {
		t.Errorf("cover --title with --preview of a missing file: %v, want an error that names the file", err)
	}
}

func mustID(t *testing.T, s *core.Store, title string) int64 {
	t.Helper()
	id, err := s.PageID(t.Context(), title)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
