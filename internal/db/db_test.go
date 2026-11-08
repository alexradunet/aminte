package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// Fresh returns an initialised throwaway database.
func Fresh(t *testing.T) *DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestInitRefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		path    string
		prepare func(*testing.T, string)
	}{
		{
			name: "empty file",
			path: filepath.Join(dir, "empty.db"),
			prepare: func(t *testing.T, p string) {
				t.Helper()
				writeFile(t, p, nil)
			},
		},
		{
			name: "nonempty file",
			path: filepath.Join(dir, "nonempty.db"),
			prepare: func(t *testing.T, p string) {
				t.Helper()
				writeFile(t, p, []byte("not an aminte database\n"))
			},
		},
		{
			name:    "foreign sqlite file",
			path:    filepath.Join(dir, "foreign.db"),
			prepare: createForeignSQLiteFile,
		},
		{
			name: "directory",
			path: filepath.Join(dir, "directory.db"),
			prepare: func(t *testing.T, p string) {
				t.Helper()
				if err := os.Mkdir(p, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.prepare(t, tc.path)
			before := snapshotPath(t, tc.path)
			if err := Init(t.Context(), tc.path); err == nil {
				t.Fatal("Init accepted an existing path")
			}
			assertSnapshot(t, tc.path, before)
		})
	}
}

type pathSnapshot struct {
	mode  os.FileMode
	bytes []byte
}

func snapshotPath(t *testing.T, p string) pathSnapshot {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	s := pathSnapshot{mode: info.Mode()}
	if !info.IsDir() {
		s.bytes, err = os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func assertSnapshot(t *testing.T, p string, want pathSnapshot) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("existing path was removed: %v", err)
	}
	if info.Mode() != want.mode {
		t.Fatalf("existing path mode changed from %v to %v", want.mode, info.Mode())
	}
	if info.IsDir() {
		return
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("existing file cannot be read: %v", err)
	}
	if !bytes.Equal(got, want.bytes) {
		t.Fatalf("existing file changed from %q to %q", want.bytes, got)
	}
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// replaceReservedFile removes the reserved file at p and writes b at p, as another process can do. It returns false
// when Windows refuses the remove: the reservation holds its file open without FILE_SHARE_DELETE, so on Windows no
// other file can take the path.
func replaceReservedFile(t *testing.T, p string, b []byte) bool {
	t.Helper()
	if err := os.Remove(p); err != nil {
		if runtime.GOOS == "windows" {
			return false
		}
		t.Fatal(err)
	}
	writeFile(t, p, b)
	return true
}

func createForeignSQLiteFile(t *testing.T, p string) {
	t.Helper()
	d, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("CREATE TABLE foreign_table(value TEXT)"); err != nil {
		d.Close()
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInitRefusesPathWhenParentIsNotDirectory(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	want := []byte("parent sentinel")
	writeFile(t, parent, want)
	p := filepath.Join(parent, "life.db")
	if err := Init(t.Context(), p); err == nil {
		t.Fatal("Init treated a non-directory parent as a free database path")
	}
	got, err := os.ReadFile(parent)
	if err != nil {
		t.Fatalf("parent sentinel was removed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("parent sentinel changed to %q", got)
	}
}

func TestInitConcurrentReservation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = Init(t.Context(), p)
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent Init successes = %d, errors = %v; want exactly one", successes, errs)
	}
	d, err := Open(t.Context(), p)
	if err != nil {
		t.Fatalf("winner database was not preserved as a valid life.db: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInitReservationBlocksCompetingLazyOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	applyErr := errors.New("controlled apply failure")
	var competitorErr error
	err := initWith(p, func(*sql.DB) error {
		competitorErr = Init(t.Context(), p)
		return applyErr
	})
	if err == nil || !strings.Contains(err.Error(), applyErr.Error()) {
		t.Fatalf("initWith error = %v, want controlled apply failure", err)
	}
	if competitorErr == nil || !strings.Contains(competitorErr.Error(), "already exists") {
		_, statErr := os.Stat(p)
		t.Fatalf("competing Init error = %v, file stat after cleanup = %v; want reservation refusal", competitorErr, statErr)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("failed reserved file remains after cleanup: %v", err)
	}
}

func TestInitApplyFailureRemovesOwnedReservation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	applyErr := errors.New("controlled apply failure")
	err := initWith(p, func(*sql.DB) error { return applyErr })
	if err == nil || !strings.Contains(err.Error(), applyErr.Error()) {
		t.Fatalf("initWith error = %v, want controlled apply failure", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("failed owned reservation remains: %v", err)
	}
}

func TestInitApplyFailurePreservesReplacement(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	replacement := []byte("replacement created after reservation\n")
	applyErr := errors.New("controlled apply failure")
	replaced := false
	err := initWith(p, func(*sql.DB) error {
		replaced = replaceReservedFile(t, p, replacement)
		return applyErr
	})
	if err == nil || !strings.Contains(err.Error(), applyErr.Error()) {
		t.Fatalf("initWith error = %v, want controlled apply failure", err)
	}
	if !replaced {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed owned reservation remains: %v", err)
		}
		return
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("replacement was removed: %v", err)
	}
	if !bytes.Equal(got, replacement) {
		t.Fatalf("replacement changed to %q", got)
	}
}

// TestAFailedInitRemovesTheWALOfItsFile: SQLite keeps the WAL of a database and its index (PATH-wal and PATH-shm)
// when its close cannot checkpoint. A cancelled Init interrupts SQLite, and the close then keeps the two. A WAL left
// beside the path goes into the next database at the path: a copy put back there reads the pages of the WAL. So the
// cleanup of a failed Init removes the two with its file, and only the two of its own file:
//   - SQLite wrote the file, so the file is not empty: the two go with the file. The test makes the close of SQLite
//     keep them with SQLITE_FCNTL_PERSIST_WAL, which gives the files of an interrupted close each time.
//   - Another file took the path: the WAL and the index there stay.
//   - The file is empty: SQLite did not read it, so a WAL beside it is not the WAL of this file, and it stays.
//   - SQLite deletes a WAL beside an empty file when it reads the file: so a WAL beside a file that SQLite wrote is
//     the WAL of this file.
//   - The WAL or the index cannot go: the file stays with them, so no WAL stays alone at the path.
func TestAFailedInitRemovesTheWALOfItsFile(t *testing.T) {
	failure := errors.New("controlled apply failure")
	// gone reports whether no file is at p.
	gone := func(t *testing.T, p string) bool {
		t.Helper()
		_, err := os.Lstat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return err != nil
	}
	// keepTheWAL writes a table into the database of w, at p, in WAL mode. It makes the close of SQLite keep the WAL
	// and its index, closes w, and returns the WAL.
	keepTheWAL := func(t *testing.T, w *sql.DB, p string) []byte {
		t.Helper()
		c, err := w.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ExecContext(t.Context(), "PRAGMA journal_mode = WAL; CREATE TABLE kept(x)"); err != nil {
			t.Fatal(err)
		}
		err = c.Raw(func(dc any) error {
			fc, ok := dc.(interface {
				FileControlPersistWAL(string, int) (int, error)
			})
			if !ok {
				return errors.New("the driver cannot keep the WAL")
			}
			_, err := fc.FileControlPersistWAL("main", 1)
			return err
		})
		if err := errors.Join(err, c.Close(), w.Close()); err != nil {
			t.Fatal(err)
		}
		wal, err := os.ReadFile(p + "-wal")
		if err != nil || len(wal) <= 32 || gone(t, p+"-shm") {
			t.Fatalf("SQLite did not keep the WAL (%d bytes, %v) and its index at its close", len(wal), err)
		}
		return wal
	}
	t.Run("the WAL of the file goes with the file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "life.db")
		err := initWith(p, func(w *sql.DB) error {
			keepTheWAL(t, w, p)
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatalf("initWith = %v, want the failure", err)
		}
		for _, name := range []string{p, p + "-wal", p + "-shm"} {
			if !gone(t, name) {
				t.Errorf("the cleanup left %s", name)
			}
		}
	})
	t.Run("another file took the path", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "life.db")
		theirs := map[string][]byte{p: []byte("another database\n"), p + "-wal": []byte("its WAL\n"), p + "-shm": []byte("its index\n")}
		replaced := false
		err := initWith(p, func(w *sql.DB) error {
			keepTheWAL(t, w, p)
			if replaced = replaceReservedFile(t, p, theirs[p]); replaced {
				writeFile(t, p+"-wal", theirs[p+"-wal"])
				writeFile(t, p+"-shm", theirs[p+"-shm"])
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatalf("initWith = %v, want the failure", err)
		}
		for name, want := range theirs {
			got, err := os.ReadFile(name)
			switch {
			case !replaced && !gone(t, name):
				t.Errorf("the reservation held its path, and the cleanup left %s", name)
			case replaced && (err != nil || !bytes.Equal(got, want)):
				t.Errorf("%s: %q %v, want the file of the other database", name, got, err)
			}
		}
	})
	t.Run("a WAL beside an empty file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "life.db")
		stale := []byte("a WAL of no database\n")
		writeFile(t, p+"-wal", stale)
		if err := initWith(p, func(*sql.DB) error { return failure }); !errors.Is(err, failure) {
			t.Fatalf("initWith = %v, want the failure", err)
		}
		if got, err := os.ReadFile(p + "-wal"); !gone(t, p) || err != nil || !bytes.Equal(got, stale) {
			t.Errorf("the cleanup changed a WAL that SQLite did not open: %q %v", got, err)
		}
	})
	t.Run("SQLite deletes a WAL beside an empty file", func(t *testing.T) {
		dir := t.TempDir()
		q := filepath.Join(dir, "other.db")
		w, err := sql.Open("aminte", dsn(q, false))
		if err != nil {
			t.Fatal(err)
		}
		wal := keepTheWAL(t, w, q)
		p := filepath.Join(dir, "life.db")
		writeFile(t, p+"-wal", wal)
		if err := Init(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		if !gone(t, p+"-wal") {
			t.Error("SQLite kept the WAL of another database beside a new file")
		}
		d, err := Open(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		err = d.R.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_schema WHERE name = 'kept'").Scan(&n)
		if err := errors.Join(err, d.Close()); err != nil || n != 0 {
			t.Errorf("the new database has the table of the other WAL: %d, %v", n, err)
		}
	})
	for _, side := range []string{"-wal", "-shm"} {
		t.Run("a "+side+" that cannot go", func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "life.db")
			err := initWith(p, func(w *sql.DB) error {
				// SQLite writes the file, in its first journal mode: no WAL.
				if _, err := w.ExecContext(t.Context(), "PRAGMA user_version = 7"); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				// A folder that is not empty cannot be removed, on Linux and on Windows.
				writeFile(t, filepath.Join(p+side, "inside"), nil)
				return failure
			})
			var refused *os.PathError
			if !errors.Is(err, failure) || !errors.As(err, &refused) || refused.Path != p+side {
				t.Fatalf("initWith = %v, want the failure and the error of %s", err, p+side)
			}
			if gone(t, p) || gone(t, p+side) {
				t.Errorf("the cleanup removed the file or its %s, and it could not remove the %s", side, side)
			}
		})
	}
}

func TestOpenRefusesForeignFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "other.db")
	o, _ := sql.Open("aminte", dsn(p, false))
	o.Exec("CREATE TABLE x(a)")
	o.Close()
	if _, err := Open(t.Context(), p); err == nil || !strings.Contains(err.Error(), "not an Aminte") {
		t.Fatalf("Open of a non-Aminte file: %v", err)
	}
}

func TestLiteralDatabasePaths(t *testing.T) {
	type literalCase struct {
		name      string
		requested string
		sentinel  string
		relative  bool
	}

	root := t.TempDir()
	cases := []literalCase{
		{
			name:      "absolute percent escape text",
			requested: filepath.Join(root, "literal paths café", "life %23 percent.db"),
			sentinel:  filepath.Join(root, "literal paths café", "life # percent.db"),
		},
		{
			name:      "absolute fragment character",
			requested: filepath.Join(root, "literal paths café", "life # fragment.db"),
			sentinel:  filepath.Join(root, "literal paths café", "life "),
		},
		{
			name:      "relative percent escape text",
			requested: filepath.Join("relative café %23", "life %23.db"),
			sentinel:  filepath.Join("relative café #", "life #.db"),
			relative:  true,
		},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases, literalCase{
			name:      "absolute query character",
			requested: filepath.Join(root, "literal paths café", "life ? query.db"),
			sentinel:  filepath.Join(root, "literal paths café", "life "),
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.relative {
				cwd := filepath.Join(root, "cwd # %23 café")
				if err := os.MkdirAll(cwd, 0o755); err != nil {
					t.Fatal(err)
				}
				t.Chdir(cwd)
			}
			prepareLiteralSentinel(t, tc.sentinel)
			if err := os.MkdirAll(filepath.Dir(tc.requested), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := Init(t.Context(), tc.requested); err != nil {
				t.Fatalf("Init(t.Context(), %q) failed; requested literal file %q, alternate sentinel %q: %v", tc.requested, absForError(tc.requested), absForError(tc.sentinel), err)
			}
			assertSamePhysicalFile(t, tc.requested)
			assertLiteralSentinel(t, tc.sentinel)

			d, err := Open(t.Context(), tc.requested)
			if err != nil {
				t.Fatalf("Open(t.Context(), %q) failed for requested literal file %q: %v", tc.requested, absForError(tc.requested), err)
			}
			if err := d.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO aminte_meta(key, value) VALUES ('literal-path-test', ?)`, tc.name)
				return err
			}); err != nil {
				d.Close()
				t.Fatal(err)
			}
			var got string
			if err := d.R.QueryRow(`SELECT value FROM aminte_meta WHERE key = 'literal-path-test'`).Scan(&got); err != nil || got != tc.name {
				d.Close()
				t.Fatalf("read back from %q = %q, %v; want %q", absForError(tc.requested), got, err, tc.name)
			}
			d.Close()

			copyStem := "copy-" + strings.NewReplacer(" ", "-", "?", "query").Replace(tc.name)
			copyPath := filepath.Join(filepath.Dir(tc.requested), copyStem+" # %23.db")
			copySentinel := filepath.Join(filepath.Dir(tc.requested), copyStem+" ")
			prepareLiteralSentinel(t, copySentinel)
			if err := Copy(t.Context(), tc.requested, copyPath); err != nil {
				t.Fatalf("Copy(t.Context(), %q, %q) failed; source literal %q, destination literal %q: %v", tc.requested, copyPath, absForError(tc.requested), absForError(copyPath), err)
			}
			assertSamePhysicalFile(t, copyPath)
			assertLiteralSentinel(t, copySentinel)
			c, err := Open(t.Context(), copyPath)
			if err != nil {
				t.Fatalf("Open(t.Context(), copy %q) failed: %v", absForError(copyPath), err)
			}
			if err := c.R.QueryRow(`SELECT value FROM aminte_meta WHERE key = 'literal-path-test'`).Scan(&got); err != nil || got != tc.name {
				c.Close()
				t.Fatalf("read back from copied literal file %q = %q, %v; want %q", absForError(copyPath), got, err, tc.name)
			}
			c.Close()
		})
	}
}

const literalSentinel = "literal sqlite path sentinel\n"

func prepareLiteralSentinel(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(literalSentinel), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertLiteralSentinel(t *testing.T, p string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("alternate sentinel %q was removed: %v", absForError(p), err)
	}
	if string(b) != literalSentinel {
		t.Fatalf("alternate sentinel %q changed to %q", absForError(p), string(b))
	}
}

func assertSamePhysicalFile(t *testing.T, requested string) {
	t.Helper()
	d, err := Open(t.Context(), requested)
	if err != nil {
		t.Fatalf("Open(t.Context(), %q) after Init/Copy failed: %v", requested, err)
	}
	defer d.Close()
	var reported string
	if err := d.W.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&reported); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(requested)
	if err != nil {
		t.Fatalf("requested literal file %q does not exist: SQLite reported %q: %v", absForError(requested), absForError(reported), err)
	}
	got, err := os.Stat(reported)
	if err != nil {
		t.Fatalf("SQLite reported database file %q for requested %q, but it cannot be statted: %v", absForError(reported), absForError(requested), err)
	}
	if !os.SameFile(want, got) {
		t.Fatalf("SQLite opened %q, not requested literal file %q", absForError(reported), absForError(requested))
	}
}

func absForError(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func TestPragmasAndReaders(t *testing.T) {
	d := Fresh(t)
	for _, p := range pragmas {
		var v int64
		if err := d.W.QueryRow("PRAGMA " + p.name).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v != p.want {
			t.Errorf("PRAGMA %s = %d, want %d", p.name, v, p.want)
		}
	}
	if _, err := d.R.Exec("INSERT INTO aminte_meta(key, value) VALUES ('x', 'y')"); err == nil {
		t.Error("the read-only pool accepted a write")
	}
	var ts int64 = -1
	if err := d.R.QueryRow("PRAGMA trusted_schema").Scan(&ts); err != nil || ts != 0 {
		t.Errorf("the read-only pool has trusted_schema = %d (%v), want 0", ts, err)
	}
	var path string
	if err := d.W.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	trusting, _ := sql.Open("aminte", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=trusted_schema(1)")
	defer trusting.Close()
	if err := trusting.Ping(); err == nil || !strings.Contains(err.Error(), "trusted_schema") {
		t.Errorf("a reader with trusted_schema=ON was not refused: %v", err)
	}
	cp := filepath.Join(t.TempDir(), "copy.db")
	if err := Copy(t.Context(), path, cp); err != nil {
		t.Errorf("Copy through a reader with trusted_schema=OFF: %v", err)
	}
	checkShadowWrite := func(conn *sql.DB, defensive bool) {
		t.Helper()
		var tableCount, id int64
		var before []byte
		if err := conn.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='entities_fts_data'").Scan(&tableCount); err != nil || tableCount != 1 {
			t.Fatalf("shadow table fixture: count=%d err=%v", tableCount, err)
		}
		if err := conn.QueryRow("SELECT id, block FROM entities_fts_data ORDER BY id LIMIT 1").Scan(&id, &before); err != nil {
			t.Fatal(err)
		}
		result, err := conn.Exec("UPDATE entities_fts_data SET block=block WHERE id=?", id)
		if defensive {
			if err == nil {
				t.Error("defensive connection allowed a write to an existing FTS shadow row")
			}
		} else {
			if err != nil {
				t.Fatalf("non-defensive control refused the same valid shadow write: %v", err)
			}
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				t.Fatalf("non-defensive control affected=%d err=%v", n, err)
			}
		}
		var after []byte
		if err := conn.QueryRow("SELECT block FROM entities_fts_data WHERE id=?", id).Scan(&after); err != nil || !bytes.Equal(before, after) {
			t.Fatalf("shadow row changed: err=%v", err)
		}
	}
	checkShadowWrite(d.W, true)
	controlPath := filepath.Join(t.TempDir(), "non-defensive.db")
	if err := Init(t.Context(), controlPath); err != nil {
		t.Fatal(err)
	}
	control, err := sql.Open("sqlite", sqliteFileURI(controlPath, nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := control.Close(); err != nil {
			t.Error(err)
		}
	})
	checkShadowWrite(control, false)
}

func TestWriteTakesTheLockUpFront(t *testing.T) {
	d := Fresh(t)
	var path string
	if err := d.W.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", // the plain driver: this second writer is the test's, not ours
		"file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	err = d.Write(context.Background(), func(*sql.Tx) error {
		// nothing written yet, but BEGIN IMMEDIATE already holds the write lock
		_, err := other.Exec("BEGIN IMMEDIATE")
		if err == nil {
			t.Error("a second writer got the lock inside an open Write")
			if _, err := other.Exec("ROLLBACK"); err != nil {
				t.Error(err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotDestinationErrors(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "life.db")
	if err := Init(t.Context(), live); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{filepath.Join(root, "missing"), live} {
		if _, err := Snapshot(t.Context(), live, dest, time.Now()); err == nil {
			t.Errorf("accepted non-directory %s", dest)
		}
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// A dangling .git entry is still a privacy marker, not evidence of safety.
	if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(target, ".git")); err != nil {
		t.Logf("dangling-marker check unavailable: %v", err)
	} else if _, err := Snapshot(t.Context(), live, target, time.Now()); err == nil || !strings.Contains(err.Error(), "git work tree") {
		t.Errorf("dangling marker refusal: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".git" {
			t.Errorf("refusal left artifact %s", entry.Name())
		}
	}
	// A .git folder above the destination: the destination is inside that work tree.
	backups := filepath.Join(root, "repo", "backups")
	for _, dir := range []string{filepath.Join(root, "repo", ".git"), backups} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Snapshot(t.Context(), live, backups, time.Now()); err == nil || !strings.Contains(err.Error(), "git work tree") {
		t.Errorf("a destination under a .git folder: %v, want the refusal", err)
	}
	if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
		t.Errorf("the refusal left %v (%v)", entries, err)
	}
	after, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("destination error changed source")
	}
}

// TestCheckSnapshotDirTellsARefusalFromAFault: a folder that is not there, a file and a path under a file are the
// refusal "no folder". A folder that the process cannot reach is a fault: its parent has no permission to search, so
// the folder can exist, and the error is not ErrRefused.
func TestCheckSnapshotDirTellsARefusalFromAFault(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(root, "missing"), file, filepath.Join(file, "under")} {
		if _, err := CheckSnapshotDir(dir); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "no folder") {
			t.Errorf("CheckSnapshotDir(%s) = %v, want the refusal no folder", dir, err)
		}
	}

	if runtime.GOOS == "windows" {
		t.Skip("a folder mode does not stop a search on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root searches a folder with no permission")
	}
	locked := filepath.Join(root, "locked")
	inside := filepath.Join(locked, "snapshots")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(locked, 0o755); err != nil {
			t.Error(err)
		}
	})
	_, err := CheckSnapshotDir(inside)
	if err == nil || errors.Is(err, ErrRefused) || !errors.Is(err, os.ErrPermission) {
		t.Errorf("CheckSnapshotDir of a folder under a parent with no permission = %v, want a permission fault, not a refusal", err)
	}
}

// TestWriteReportsAFailedRollback: when fn fails and the rollback fails too, Write returns both errors in a
// RollbackError. The caller then knows that the transaction was not rolled back. Here fn commits by itself before it
// fails, so the rollback finds no open transaction.
func TestWriteReportsAFailedRollback(t *testing.T) {
	d := Fresh(t)
	failed := errors.New("fn failed")
	err := d.Write(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("COMMIT"); err != nil {
			return err
		}
		return failed
	})
	var rollback *sqlite.Error
	var both *RollbackError
	if !errors.Is(err, failed) || !errors.As(err, &rollback) || !errors.As(err, &both) {
		t.Fatalf("Write = %v, want a RollbackError with the error of fn and the SQLite error of the rollback", err)
	}
	if err := d.Write(t.Context(), func(*sql.Tx) error { return nil }); err != nil {
		t.Errorf("the writer is not usable after the failed rollback: %v", err)
	}
}

// TestCancelledWriteReportsOnlyTheCancellation: when the context is cancelled, database/sql rolls the transaction
// back. Write's own rollback then finds it done, and that is not a failure to report.
func TestCancelledWriteReportsOnlyTheCancellation(t *testing.T) {
	d := Fresh(t)
	opCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := d.Write(opCtx, func(tx *sql.Tx) error {
		cancel()
		// Wait until database/sql has rolled the transaction back.
		for {
			_, err := tx.ExecContext(context.Background(), "SELECT 1")
			if errors.Is(err, sql.ErrTxDone) {
				return opCtx.Err()
			}
			if err != nil {
				return err
			}
		}
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, sql.ErrTxDone) {
		t.Errorf("Write = %v, want the cancellation alone", err)
	}
}

// TestCloseLogsAFailedOptimize: a PRAGMA optimize that fails loses no data, so Close logs it and returns nil: a CLI
// command that is done must not exit 1 for it. Close still closes the readers.
func TestCloseLogsAFailedOptimize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	if err := Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	// Deliberate damage: with the writer closed first, PRAGMA optimize cannot run.
	if err := d.W.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close = %v, want nil: a skipped optimize loses no data", err)
	}
	if !strings.Contains(logged.String(), "PRAGMA optimize") {
		t.Errorf("Close logged %q, want a line about PRAGMA optimize", logged.String())
	}
	if err := d.R.PingContext(t.Context()); err == nil {
		t.Error("the readers are open after Close")
	}
}
