package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aminte/internal/db/dbtest"
)

// aminte snapshot is the catalog's action in-process, --snapshots choosing the folder. The store's clock is fixed, so the
// name of the snapshot does not depend on the day the test runs.
func TestSnapshotCommandRunsTheAction(t *testing.T) {
	live := dbtest.File(t)
	dir := t.TempDir()
	e, stdout, _ := testEnv(t)
	e.clock = func() time.Time { return time.Date(2031, 5, 14, 12, 0, 0, 0, time.Local) }
	if err := runArgs(t.Context(), e, []string{"snapshot", "--db", live, "--snapshots", dir}); err != nil {
		t.Fatalf("snapshot: %v\n%s", err, stdout)
	}
	want := filepath.Join(dir, "life-2031-05-14.db")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("no snapshot at %s: %v\n%s", want, err, stdout)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("the report lacks the restore check:\n%s", stdout)
	}
	// Over --url the server's folder is the one.
	err := runArgs(t.Context(), e, []string{"snapshot", "--url", "http://127.0.0.1:1", "--snapshots", dir})
	if err == nil || !strings.Contains(err.Error(), "drop --snapshots") {
		t.Fatalf("--url with --snapshots: %v", err)
	}
	// serve refuses a snapshot folder that is not one, before it opens anything.
	err = runArgs(t.Context(), e, []string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "absent.db"), "--snapshots", filepath.Join(t.TempDir(), "missing")})
	if err == nil || !strings.Contains(err.Error(), "no folder") {
		t.Fatalf("serve --snapshots missing: %v", err)
	}
	o, err := parse([]string{"serve", "--snapshots", dir}, noVars)
	if err != nil || o.snapshots != dir {
		t.Fatalf("parse: %+v %v", o, err)
	}
}

// TestSnapshotCommandRefusesAGitWorkTree: snapshot --snapshots of a folder inside a git work tree is refused before anything
// opens. --db names a file that is not there, so a command that opened it would fail with another error, and nothing
// is written in the folder or at the path of --db.
func TestSnapshotCommandRefusesAGitWorkTree(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "absent.db")
	backups := filepath.Join(root, "repo", "backups")
	for _, dir := range []string{filepath.Join(root, "repo", ".git"), backups} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e, _, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"snapshot", "--db", live, "--snapshots", backups}); err == nil || !strings.Contains(err.Error(), "git work tree") {
		t.Errorf("snapshot --snapshots inside a git work tree: %v, want the refusal", err)
	}
	if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
		t.Errorf("the refused snapshot left %v (%v)", entries, err)
	}
	if _, err := os.Lstat(live); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused snapshot made %s: %v", live, err)
	}
}
