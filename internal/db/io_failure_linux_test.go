package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// A child-local file-size limit makes actual filesystem writes fail with EFBIG.
// This covers SQLite write-error recovery, not disk exhaustion or power loss.
func TestFilesystemWriteFailure(t *testing.T) {
	for _, operation := range []string{"wal", "copy"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFilesystemWriteFailureChild$", "-test.count=1", "-test.v")
			cmd.Env = append(os.Environ(), "AMINTE_IO_FAILURE_OPERATION="+operation, "AMINTE_IO_FAILURE_DIR="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s child: %v (context %v)\n%s", operation, err, ctx.Err(), out)
			}
			if !bytes.Contains(out, []byte("filesystem I/O recovery verified")) {
				t.Fatalf("child did not complete its recovery assertions:\n%s", out)
			}
		})
	}
}

func TestFilesystemWriteFailureChild(t *testing.T) {
	dir := os.Getenv("AMINTE_IO_FAILURE_DIR")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	path := filepath.Join(dir, "life.db")
	if err := Init(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	d, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if d != nil {
			if err := d.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	var id int64
	// Direct SQL isolates the storage wrapper and canonical trigger graph; this
	// is not a capture/import workflow fixture.
	err = d.Write(t.Context(), func(tx *sql.Tx) error {
		if err := tx.QueryRow(`INSERT INTO entities(preferred_name_key,entity_type,body,source,created_at,updated_at)
			VALUES('storage witness','page','baselineword','cli','2026-01-02T03:04:05.000Z','2026-01-02T03:04:05.000Z') RETURNING id`).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Storage witness','storage witness')`, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline := readIOFailureState(t, d, id)
	if baseline.body != "baselineword" || baseline.names != "Storage witness" {
		t.Fatal("committed baseline does not contain the expected body and name")
	}
	checkIOFailureState(t, d, id, baseline, false)
	var busy, frames, checkpointed int
	if err := d.W.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil || busy != 0 || frames != 0 || checkpointed != 0 {
		t.Fatalf("baseline checkpoint: %d/%d/%d, %v", busy, frames, checkpointed, err)
	}

	reopen := func() {
		t.Helper()
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
		d = nil
		d, err = Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
	}
	switch operation := os.Getenv("AMINTE_IO_FAILURE_OPERATION"); operation {
	case "wal":
		body := strings.Repeat("retryword ", 32768)
		aliasInserted := false
		edit := func() error {
			return d.Write(t.Context(), func(tx *sql.Tx) error {
				if _, err := tx.Exec(`INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Retryalias','retryalias')`, id); err != nil {
					return err
				}
				aliasInserted = true
				_, err := tx.Exec("UPDATE entities SET body=? WHERE id=?", body, id)
				return err
			})
		}
		err = withIOFileSizeLimit(t, dir, edit)
		requireIOWriteError(t, err)
		if !aliasInserted {
			t.Fatal("failure occurred before the transaction made its first change")
		}
		// The production transaction helper/driver must have rolled back even if
		// the I/O error occurred during COMMIT. A new transaction must be usable.
		if err := d.Write(t.Context(), func(*sql.Tx) error { return nil }); err != nil {
			t.Fatalf("writer remains unusable after failed transaction: %v", err)
		}
		checkIOFailureState(t, d, id, baseline, false)
		reopen()
		checkIOFailureState(t, d, id, baseline, false)
		if err := edit(); err != nil {
			t.Fatalf("same edit after restoring file-size limit: %v", err)
		}
		retried := readIOFailureState(t, d, id)
		if retried.body != body || retried.names != "Retryalias|Storage witness" || retried.revision <= baseline.revision {
			t.Fatalf("retry did not persist body, alias and newer revision: body bytes=%d names=%q revision=%d", len(retried.body), retried.names, retried.revision)
		}
		reopen()
		checkIOFailureState(t, d, id, retried, true)
	case "copy":
		dest := filepath.Join(dir, "snapshot.db")
		err = withIOFileSizeLimit(t, dir, func() error { return Copy(t.Context(), path, dest) })
		requireIOWriteError(t, err)
		if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed copy left its partial destination: %v", err)
		}
		reopen()
		checkIOFailureState(t, d, id, baseline, false)
		if err := Copy(t.Context(), path, dest); err != nil {
			t.Fatalf("same destination after restoring file-size limit: %v", err)
		}
		copied, err := OpenSnapshot(t.Context(), dest)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := copied.Close(); err != nil {
				t.Error(err)
			}
		})
		checkIOFailureState(t, copied, id, baseline, false)
	default:
		t.Fatalf("unknown I/O failure operation %q", operation)
	}
	t.Log("filesystem I/O recovery verified")
}

func withIOFileSizeLimit(t *testing.T, dir string, operation func() error) error {
	t.Helper()
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	limited := original
	limited.Cur = 64 << 10
	if original.Cur <= limited.Cur {
		t.Fatalf("child already has file-size limit %d; need more than %d for recovery", original.Cur, limited.Cur)
	}
	wasIgnored := signal.Ignored(syscall.SIGXFSZ)
	signal.Ignore(syscall.SIGXFSZ)
	defer func() {
		if !wasIgnored {
			signal.Reset(syscall.SIGXFSZ)
		}
	}()
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Fatalf("restore child file-size limit: %v", err)
		}
	}()
	// Independently establish the OS failure, rather than accepting an unrelated
	// SQLite constraint, permission error or SQL setup failure as fault injection.
	f, err := os.Create(filepath.Join(dir, "limit-control"))
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteAt([]byte{1}, int64(limited.Cur))
	closeErr := f.Close()
	if !errors.Is(writeErr, syscall.EFBIG) || closeErr != nil {
		t.Fatalf("file-size limit control: write=%v close=%v; want EFBIG", writeErr, closeErr)
	}
	return operation()
}

func requireIOWriteError(t *testing.T, err error) {
	t.Helper()
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != sqlite3.SQLITE_IOERR_WRITE {
		t.Fatalf("filesystem-limited write: %v; want SQLITE_IOERR_WRITE (%d)", err, sqlite3.SQLITE_IOERR_WRITE)
	}
}

type ioFailureState struct {
	body, names, updatedAt string
	revision               int64
}

func readIOFailureState(t *testing.T, d *DB, id int64) ioFailureState {
	t.Helper()
	var state ioFailureState
	if err := d.R.QueryRow(`SELECT body,updated_at,revision,
		(SELECT group_concat(title,'|' ORDER BY name_key) FROM entity_names WHERE entity_id=e.id)
		FROM entities e WHERE id=?`, id).Scan(&state.body, &state.updatedAt, &state.revision, &state.names); err != nil {
		t.Fatal(err)
	}
	return state
}

func checkIOFailureState(t *testing.T, d *DB, id int64, want ioFailureState, retried bool) {
	t.Helper()
	if got := readIOFailureState(t, d, id); got != want {
		t.Fatalf("authoritative row changed: body bytes=%d names=%q timestamp=%q revision=%d; want body bytes=%d names=%q timestamp=%q revision=%d",
			len(got.body), got.names, got.updatedAt, got.revision, len(want.body), want.names, want.updatedAt, want.revision)
	}
	for _, term := range []string{"baselineword", "retryword", "retryalias"} {
		var count int
		if err := d.R.QueryRow("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH ? AND rowid=?", term, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		wantCount := 0
		if (term == "baselineword") != retried {
			wantCount = 1
		}
		if count != wantCount {
			t.Errorf("FTS %q count=%d; want %d", term, count, wantCount)
		}
	}
	var integrity string
	if err := d.R.QueryRow("SELECT group_concat(integrity_check,'|') FROM pragma_integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check=%q, %v", integrity, err)
	}
	var violations int
	if err := d.R.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign_key_check=%d, %v", violations, err)
	}
	if err := d.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)")
		return err
	}); err != nil {
		t.Fatalf("FTS content integrity: %v", err)
	}
}
