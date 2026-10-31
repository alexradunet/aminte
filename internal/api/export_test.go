package api

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"lifelog/internal/core"
)

// HoldWriteLock makes a second writer on the file of s that holds the write lock until the test ends, so that a write
// of s waits for it. When busyMs is more than 0, it also sets the busy wait of the store's one writer connection to
// busyMs milliseconds, so that the write meets a real SQLITE_BUSY soon. Only this test's connection changes: the
// settings that the program sets on a new connection stay as they are (docs/contract/connections.md).
func HoldWriteLock(t *testing.T, s *core.Store, busyMs int) {
	t.Helper()
	if busyMs > 0 {
		if _, err := s.DB.W.ExecContext(t.Context(), "PRAGMA busy_timeout = "+strconv.Itoa(busyMs)); err != nil {
			t.Fatal(err)
		}
	}
	other, err := sql.Open("sqlite", "file:"+filepath.ToSlash(s.DB.Path())) // the plain driver: this writer is the test's
	if err != nil {
		t.Fatal(err)
	}
	conn, err := other.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.ExecContext(context.WithoutCancel(t.Context()), "ROLLBACK")
		conn.Close()
		other.Close()
	})
}

// PathOf is the route of a catalog action.
func PathOf(action string) string { return specOf(action).Path }
