package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"aminte/internal/db"
	"aminte/internal/db/dbtest"
)

// TestASnapshotTellsARefusalFromAFailure: a name in use, a folder inside a git work tree and a folder that is not there
// are refusals, a 422 with the code snapshot_refused. A database that is not there is a failure: it is not a refusal.
func TestASnapshotTellsARefusalFromAFailure(t *testing.T) {
	s := fresh(t)
	now := time.Date(2031, 5, 14, 9, 30, 0, 0, time.UTC)
	refused := func(t *testing.T, err error) {
		t.Helper()
		var e *Error
		if !errors.As(err, &e) || e.Status != 422 || e.Code != "snapshot_refused" {
			t.Errorf("err = %#v, want a 422 snapshot_refused", err)
		}
	}
	t.Run("a name in use", func(t *testing.T) {
		s.SnapshotDir = t.TempDir()
		for _, name := range []string{"life-2031-05-14.db", "life-2031-05-14T093000.db"} {
			if err := os.WriteFile(filepath.Join(s.SnapshotDir, name), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := s.TakeSnapshot(ctx, now)
		refused(t, err)
	})
	t.Run("a git work tree", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		s.SnapshotDir = repo
		_, _, err := s.TakeSnapshot(ctx, now)
		refused(t, err)
	})
	t.Run("no folder", func(t *testing.T) {
		s.SnapshotDir = filepath.Join(t.TempDir(), "absent")
		_, _, err := s.TakeSnapshot(ctx, now)
		refused(t, err)
	})
	t.Run("no database", func(t *testing.T) {
		// Deliberate damage: the file of the store is closed and removed.
		path := dbtest.File(t)
		d, err := db.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		gone := &Store{DB: d, SnapshotDir: t.TempDir()}
		if err := gone.DB.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		_, _, err = gone.TakeSnapshot(ctx, now)
		var e *Error
		if !errors.Is(err, os.ErrNotExist) || errors.As(err, &e) {
			t.Errorf("err = %#v, want a failure that is not a refusal", err)
		}
	})
}

// TestARestoreCheckReportsAFailedClose: the restore check closes the copy that it checked. A Close that fails is an
// error of the check, because the copy is then not known to be left as written. The fault driver of
// integrity_test.go fails the Close of its connection.
func TestARestoreCheckReportsAFailedClose(t *testing.T) {
	for _, fault := range []string{"", "conn-close"} {
		s := faultIntegrityStore(t, &integrityConnector{fault: fault})
		res, err := restoreCheck(ctx, s.DB)
		switch {
		case fault == "" && (err != nil || res == nil || !res.OK):
			t.Errorf("a clean check = %+v, %v; want a passed check", res, err)
		case fault != "" && (res != nil || !errors.Is(err, errIntegrityFault)):
			t.Errorf("a check whose Close fails = %+v, %v; want no result and the fault", res, err)
		}
	}
}

// TestSnapshot is Store.TakeSnapshot on a live file that the writer still holds open
// (docs/cookbook/take-a-snapshot.md): the dated name, the time added on a second snapshot that day, never an existing
// file, the restore check passing and leaving the snapshot as it was.
func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	d := dbtest.New(t)
	live := d.Path()
	if _, _, err := (&Store{DB: d}).Capture(ctx, "cli", "2026-10-02", "Walked to [[Lakeside]] with [[Sam]].", nil); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 300 { // enough rows that PRAGMA optimize would have statistics to write into the snapshot
		if _, _, err := (&Store{DB: d}).Capture(ctx, "cli", day.AddDate(0, 0, i).Format("2006-01-02"), fmt.Sprintf("Ran with [[Sam]] past [[Note %d]].", i), nil); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	s := &Store{DB: d, SnapshotDir: dir}
	now := time.Date(2026, 10, 2, 14, 30, 5, 0, time.Local)
	first, res, err := s.TakeSnapshot(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "life-2026-10-02.db" {
		t.Errorf("the first snapshot of the day is %s, want life-2026-10-02.db", filepath.Base(first))
	}
	if !res.OK {
		t.Errorf("the restore check failed on a clean file: %+v", res)
	}
	// the restore check, as TakeSnapshot runs it, on a snapshot no check has touched yet
	untouched, err := db.Snapshot(t.Context(), live, t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	b0 := mustRead(t, untouched)
	r, err := db.OpenSnapshot(t.Context(), untouched)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	r.R.QueryRow("SELECT count(*) FROM entity_names WHERE title IN ('2026-10-02', 'Lakeside', 'Sam')").Scan(&n)
	if res, err := (&Store{DB: r}).Integrity(ctx); err != nil || !res.OK {
		t.Errorf("the restore check on the snapshot: %+v %v", res, err)
	}
	r.Close()
	if n != 3 {
		t.Errorf("the snapshot holds %d of the 3 pages", n)
	}
	if !bytes.Equal(b0, mustRead(t, untouched)) {
		t.Error("the restore check changed the snapshot (PRAGMA optimize at close?)")
	}

	before := mustRead(t, first)
	second, res, err := s.TakeSnapshot(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "life-2026-10-02T143005.db" || !res.OK {
		t.Errorf("the second snapshot of the day is %s (restore check ok: %v), want life-2026-10-02T143005.db", filepath.Base(second), res.OK)
	}
	after, _ := os.ReadFile(first)
	if !bytes.Equal(before, after) {
		t.Error("the restore check or the second snapshot changed the first snapshot")
	}
	if _, _, err := s.TakeSnapshot(ctx, now); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a third snapshot in the same second: %v, want a refusal", err)
	}
	if !bytes.Equal(before, mustRead(t, first)) {
		t.Error("a refused snapshot changed an existing file")
	}
	// SQLite itself writes into an existing empty file: the refusal is the writer's own
	next := now.AddDate(0, 0, 1)
	for _, name := range []string{"life-2026-10-03.db", "life-2026-10-03T143005.db"} {
		os.WriteFile(filepath.Join(dir, name), nil, 0o644)
	}
	if _, _, err := s.TakeSnapshot(ctx, next); err == nil {
		t.Error("a snapshot onto an existing empty file was not refused")
	}
	if b := mustRead(t, filepath.Join(dir, "life-2026-10-03T143005.db")); len(b) != 0 {
		t.Errorf("a refused snapshot wrote %d bytes into an existing empty file", len(b))
	}
}

func TestSnapshotLiteralSpecialCharacterPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	liveDir := filepath.Join(root, "live café %23")
	live := filepath.Join(liveDir, "life %23.db")
	writeSnapshotSentinel(t, filepath.Join(liveDir, "life #.db"))
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := db.Init(t.Context(), live); err != nil {
		t.Fatalf("Init(%q) failed with alternate sentinel %q: %v", live, filepath.Join(liveDir, "life #.db"), err)
	}
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := (&Store{DB: d}).Capture(ctx, "cli", "2026-10-05", "Literal snapshot path for [[Sam]].", nil); err != nil {
		d.Close()
		t.Fatal(err)
	}
	d.Close()

	dir := filepath.Join(root, "snapshots %23 café")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	destinationSentinel := filepath.Join(root, "snapshots # café", "life-2026-10-05.db")
	writeSnapshotSentinel(t, destinationSentinel)
	now := time.Date(2026, 10, 5, 8, 9, 10, 0, time.Local)
	got, res, err := snapshotStore(t, live, dir).TakeSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("the snapshot of %q into %q failed: %v", live, dir, err)
	}
	want := filepath.Join(dir, "life-2026-10-05.db")
	assertSnapshotDestination(t, got, want)
	if !res.OK {
		t.Fatalf("restore check failed on literal snapshot path: %+v", res)
	}
	assertSnapshotSentinel(t, filepath.Join(liveDir, "life #.db"))
	assertSnapshotSentinel(t, destinationSentinel)
	snap, err := db.OpenSnapshot(t.Context(), want)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var n int
	if err := snap.R.QueryRow(`SELECT count(*) FROM entity_names WHERE title IN ('2026-10-05', 'Sam')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("literal snapshot %q has %d captured pages (%v), want 2", want, n, err)
	}
}

const snapshotSentinel = "literal snapshot sentinel\n"

// snapshotStore opens the life.db at live for the owner's snapshot action, with dir as its folder. The test closes it.
func snapshotStore(t *testing.T, live, dir string) *Store {
	t.Helper()
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return &Store{DB: d, SnapshotDir: dir}
}

func assertSnapshotDestination(t *testing.T, got, want string) {
	t.Helper()
	// Resolving the destination directory can expand Windows short names. The
	// dated filename and actual destination file must still match exactly.
	if filepath.Base(got) != filepath.Base(want) {
		t.Errorf("snapshot filename = %q, want %q", filepath.Base(got), filepath.Base(want))
	}
	gotInfo, err := os.Lstat(got)
	if err != nil {
		t.Errorf("returned snapshot %q does not exist: %v", got, err)
		return
	}
	wantInfo, err := os.Lstat(want)
	if err != nil {
		t.Errorf("literal snapshot destination %q does not exist: %v", want, err)
		return
	}
	if !gotInfo.Mode().IsRegular() || !wantInfo.Mode().IsRegular() || !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("snapshot %q is not the regular file at literal destination %q", got, want)
	}
}

func writeSnapshotSentinel(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(snapshotSentinel), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotSentinel(t *testing.T, p string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("alternate sentinel %q was removed: %v", p, err)
	}
	if string(b) != snapshotSentinel {
		t.Fatalf("alternate sentinel %q changed to %q", p, string(b))
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSnapshotPhysicalDestination: the snapshot folder is checked at its path and at the folder that a symbolic link or
// a junction leads to. A .git folder or file at or above either one refuses the snapshot: nothing is written there,
// and the live file stays as it was. A link to a safe folder takes the snapshot in the folder that it leads to.
func TestSnapshotPhysicalDestination(t *testing.T) {
	for _, kind := range []string{"symlink", "junction"} {
		for _, marker := range []string{"directory", "file"} {
			for _, layout := range []string{"destination", "ancestor", "lexical", "safe"} {
				t.Run(kind+"/"+marker+"/"+layout, func(t *testing.T) {
					if kind == "junction" && runtime.GOOS != "windows" {
						t.Skip("Windows directory junctions require Windows")
					}
					root := t.TempDir()
					live := dbtest.File(t)
					before := mustRead(t, live)
					repo := filepath.Join(root, "repo")
					target := filepath.Join(repo, "backups")
					if layout == "safe" || layout == "lexical" {
						target = filepath.Join(root, "safe #%'")
					}
					for _, dir := range []string{repo, target} {
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					git := filepath.Join(repo, ".git")
					if marker == "directory" {
						if err := os.Mkdir(git, 0o755); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(git, []byte("gitdir: synthetic-marker\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					link := filepath.Join(root, "linked")
					if layout == "lexical" {
						link = filepath.Join(repo, "linked")
					}
					if kind == "junction" {
						if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
							t.Skipf("cannot create test-owned junction: %v: %s", err, out)
						}
					} else if err := os.Symlink(target, link); err != nil {
						t.Skipf("cannot create test-owned directory symlink: %v", err)
					}
					if kind == "junction" {
						info, err := os.Lstat(link)
						if err != nil {
							t.Fatal(err)
						}
						got, err := os.Readlink(link)
						if err != nil || !strings.EqualFold(got, target) {
							t.Fatalf("junction Readlink: %q %v, want %q", got, err, target)
						}
						t.Logf("junction mode %s; Readlink resolves target", info.Mode())
					}
					dest := link
					physical := target
					if layout == "ancestor" {
						dest = filepath.Join(link, "child")
						physical = filepath.Join(target, "child")
						if err := os.Mkdir(physical, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					now := time.Date(2026, 10, 2, 14, 30, 5, 0, time.Local)
					if layout == "safe" {
						p, res, err := snapshotStore(t, live, dest).TakeSnapshot(context.Background(), now)
						if err != nil || !res.OK {
							t.Fatalf("safe linked destination: %s %+v %v", p, res, err)
						}
						resolved, err := filepath.EvalSymlinks(physical)
						if err != nil {
							t.Fatal(err)
						}
						if filepath.Dir(p) != resolved {
							t.Errorf("copy path %s is not physical destination %s", p, resolved)
						}
					} else {
						if _, _, err := snapshotStore(t, live, dest).TakeSnapshot(context.Background(), now); err == nil || !strings.Contains(err.Error(), "git work tree") {
							t.Errorf("snapshot refusal: %v", err)
						}
						for _, dir := range []string{dest, physical} {
							entries, err := os.ReadDir(dir)
							if err != nil {
								t.Fatal(err)
							}
							if len(entries) != 0 {
								t.Errorf("refused destination %s contains artifacts: %v", dir, entries)
							}
						}
					}
					if !bytes.Equal(before, mustRead(t, live)) {
						t.Error("snapshot changed source bytes")
					}
				})
			}
		}
	}
}

func TestSnapshotNeverFollowsDestinationFileSymlinks(t *testing.T) {
	for _, occupied := range []string{"daily", "timestamp"} {
		t.Run(occupied, func(t *testing.T) {
			root := t.TempDir()
			live := dbtest.File(t)
			dir, repo := filepath.Join(root, "snapshots"), filepath.Join(root, "repo")
			for _, path := range []string{dir, filepath.Join(repo, ".git")} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			daily := filepath.Join(dir, "life-2031-02-03.db")
			timestamp := filepath.Join(dir, "life-2031-02-03T040506.db")
			leaf := daily
			if occupied == "timestamp" {
				if err := os.WriteFile(daily, []byte("existing snapshot sentinel"), 0o600); err != nil {
					t.Fatal(err)
				}
				leaf = timestamp
			}
			target := filepath.Join(repo, "private.db")
			if err := os.Symlink(target, leaf); err != nil {
				t.Skipf("cannot create test-owned file symlink: %v", err)
			}
			now := time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC)
			path, result, err := snapshotStore(t, live, dir).TakeSnapshot(context.Background(), now)
			if occupied == "daily" {
				if err != nil || result == nil || !result.OK {
					t.Errorf("occupied daily entry: path=%q result=%+v error=%v; want a valid timestamp snapshot", path, result, err)
				} else {
					assertSnapshotDestination(t, path, timestamp)
				}
			} else {
				if err == nil {
					t.Error("snapshot accepted an occupied timestamp entry")
				}
				if got := string(mustRead(t, daily)); got != "existing snapshot sentinel" {
					t.Errorf("existing snapshot changed: %q", got)
				}
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("snapshot escaped into a git repository through the filename: %v", err)
			}
			if got, err := os.Readlink(leaf); err != nil || got != target {
				t.Errorf("snapshot changed the existing symlink: %q, %v", got, err)
			}
		})
	}
}

// TestASnapshotThatFailsItsCheckLandsUnderTheFailedName: the live file is damaged on purpose, so the copy fails its
// restore check. The copy is kept for the owner to read, but under life-DATE.failed.db (or the time form), so that no
// restore routine picks it and no script counts it as good (D25). A later failed snapshot never overwrites an earlier one.
func TestASnapshotThatFailsItsCheckLandsUnderTheFailedName(t *testing.T) {
	s := fresh(t)
	dir := t.TempDir()
	s.SnapshotDir = dir
	id, _, err := s.CreatePage(ctx, "cli", "Owner", "prose", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2031, 5, 14, 9, 30, 0, 0, time.UTC)
	names := func() []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		return got
	}
	// A clean snapshot first: the day name is taken, so the failed one below uses the time form.
	if _, res, err := s.TakeSnapshot(ctx, now); err != nil || !res.OK {
		t.Fatalf("clean snapshot: %+v %v", res, err)
	}
	// Deliberate external damage only: same pooled connection, FK disabled for this write.
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=OFF; UPDATE entities SET preferred_name_key='mood' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	path, res, err := s.TakeSnapshot(ctx, now)
	if err != nil || res == nil || res.OK {
		t.Fatalf("damaged snapshot: %+v %v; want a failed check and no error", res, err)
	}
	if filepath.Base(path) != "life-2031-05-14T093000.failed.db" {
		t.Errorf("returned path %s, want life-2031-05-14T093000.failed.db", path)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("the returned path is not the file: %v", err)
	}
	if got, want := names(), []string{"life-2031-05-14.db", "life-2031-05-14T093000.failed.db"}; !slices.Equal(got, want) {
		t.Fatalf("snapshot folder = %v, want %v", got, want)
	}
	// The good copy goes away. The day name is free again and fails again: it takes the failed name of the day.
	if err := os.Remove(filepath.Join(dir, "life-2031-05-14.db")); err != nil {
		t.Fatal(err)
	}
	first := mustRead(t, path)
	again, res, err := s.TakeSnapshot(ctx, now)
	if err != nil || res == nil || res.OK {
		t.Fatalf("second damaged snapshot: %+v %v", res, err)
	}
	if filepath.Base(again) != "life-2031-05-14.failed.db" {
		t.Errorf("second failed snapshot = %s, want life-2031-05-14.failed.db", again)
	}
	// A third failure the same day finds both failed names taken: it must not overwrite either.
	third, res, err := s.TakeSnapshot(ctx, now)
	if err != nil || res == nil || res.OK {
		t.Fatalf("third damaged snapshot: %+v %v", res, err)
	}
	if third == again || third == path || !strings.HasSuffix(third, ".failed.db") {
		t.Errorf("third failed snapshot = %s, want a new name that ends in .failed.db", third)
	}
	if !bytes.Equal(first, mustRead(t, path)) {
		t.Error("a later failed snapshot overwrote an earlier one")
	}
	if got := names(); len(got) != 3 {
		t.Errorf("snapshot folder = %v, want the three failed copies", got)
	}
	for _, name := range names() {
		if !strings.HasSuffix(name, ".failed.db") {
			t.Errorf("%s in the snapshot folder has no .failed.db name", name)
		}
	}
}
