// Package dbtest makes the fresh life.db of a test: a new file in the test's temporary folder, made from the embedded
// DDL and opened with the connection settings of the program (docs/contract/connections.md). It imports only db, so
// the tests inside a package of aminte can use it too.
package dbtest

import (
	"path/filepath"
	"testing"

	"aminte/internal/db"
)

// New makes a fresh life.db and opens it as the program opens it. The cleanup of the test closes it, and an error of
// that Close fails the test. A test that opens the file again, or takes a snapshot of it, gets its path from the
// method Path of the DB that New returns. A test whose program opens the file itself uses File.
func New(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), File(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d
}

// File makes a fresh life.db and returns its path. The file is not open: it is for a test whose program opens the
// file itself (a command line with --db, a server process), or that closes its store on purpose.
func File(t testing.TB) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	return p
}
