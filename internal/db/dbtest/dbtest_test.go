package dbtest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"testing"

	"aminte/internal/db"
)

// TestNewOpensAFreshFileAndTheTestClosesIt: the file of New has a table of the DDL, the writer reads back the
// pragmas of the connection contract, and the readers read back trusted_schema=OFF and refuse a write. After the
// test, the cleanup closed the writer and the readers, and the file is gone with the temporary folder of the test.
func TestNewOpensAFreshFileAndTheTestClosesIt(t *testing.T) {
	var d *db.DB
	t.Run("the test", func(t *testing.T) {
		d = New(t)
		var tables int
		if err := d.R.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'entities'").Scan(&tables); err != nil || tables != 1 {
			t.Errorf("the table entities: count %d, %v; want 1", tables, err)
		}
		for _, p := range []struct {
			name string
			want int64
		}{{"foreign_keys", 1}, {"recursive_triggers", 1}, {"synchronous", 2}, {"trusted_schema", 0}, {"busy_timeout", 5000}} {
			var got int64 = -1
			if err := d.W.QueryRow("PRAGMA " + p.name).Scan(&got); err != nil || got != p.want {
				t.Errorf("the writer's PRAGMA %s = %d, %v; want %d", p.name, got, err, p.want)
			}
		}
		var trusted int64 = -1
		if err := d.R.QueryRow("PRAGMA trusted_schema").Scan(&trusted); err != nil || trusted != 0 {
			t.Errorf("the readers' PRAGMA trusted_schema = %d, %v; want 0", trusted, err)
		}
		if _, err := d.R.Exec("INSERT INTO aminte_meta(key, value) VALUES ('synthetic', 'row')"); err == nil {
			t.Error("the readers accepted a write")
		}
	})
	if d == nil {
		return // New failed, and the sub-test said why
	}
	if d.W.Ping() == nil || d.R.Ping() == nil {
		t.Error("the writer or the readers are open after the test")
	}
	if _, err := os.Stat(d.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the file %s after the test: %v; want no file", d.Path(), err)
	}
}

// TestFileIsAFreshFileThatNoConnectionHolds: the path of File names a life.db that db.Open accepts, and no connection
// holds it open: in WAL mode the last connection to close removes the -wal file.
func TestFileIsAFreshFileThatNoConnectionHolds(t *testing.T) {
	p := File(t)
	if _, err := os.Stat(p + "-wal"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the file %s-wal: %v; want no file", p, err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

// recorder is a test that keeps its cleanups and its errors, so a test of the kit can run the cleanups and read what
// they reported. The other methods are those of the real test.
type recorder struct {
	testing.TB
	cleanups []func()
	errors   []string
}

func (r *recorder) Cleanup(f func())  { r.cleanups = append(r.cleanups, f) }
func (r *recorder) Error(args ...any) { r.errors = append(r.errors, fmt.Sprint(args...)) }

// TestAFailedCloseFailsTheTestOfNew: the cleanup of New fails the test when its Close fails. A failed PRAGMA optimize is
// not such a failure (Close logs it), so the test gives the writer a connection whose own close fails.
func TestAFailedCloseFailsTheTestOfNew(t *testing.T) {
	r := &recorder{TB: t}
	d := New(r)
	if err := d.W.Close(); err != nil {
		t.Fatal(err)
	}
	d.W = sql.OpenDB(closeFails{})
	if err := d.W.PingContext(t.Context()); err != nil { // opens the connection whose close fails
		t.Fatal(err)
	}
	for _, f := range slices.Backward(r.cleanups) {
		f()
	}
	if len(r.errors) == 0 {
		t.Error("the Close of the cleanup failed, and the test did not fail")
	}
}

// closeFails is a connector of connections that accept a statement and fail their close.
type closeFails struct{}

func (closeFails) Connect(context.Context) (driver.Conn, error) { return closeFailsConn{}, nil }
func (closeFails) Driver() driver.Driver                        { return nil }

type closeFailsConn struct{}

func (closeFailsConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (closeFailsConn) Close() error                        { return errors.New("the connection cannot close") }
func (closeFailsConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (closeFailsConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.ResultNoRows, nil
}
