package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"aminte/internal/db"
)

// TakeSnapshot is the owner's snapshot action (docs/plans/082-snapshot-in-the-catalog.md): a dated copy of the
// database in SnapshotDir, or beside the database when none is set, and its restore check (docs/cookbook/take-a-
// snapshot.md, D25): the four integrity checks on a connection whose Close skips PRAGMA optimize, so the file is left
// as written. A snapshot that fails the check, or cannot be checked, is renamed to life-DATE.failed.db (D25). The path
// (the new one) is returned with the check. A refusal of the folder or the name (db.ErrRefused: no folder,
// a folder inside a git work tree, a name in use) is a 422 with the code snapshot_refused; any other error is a
// failure.
func (s *Store) TakeSnapshot(ctx context.Context, now time.Time) (string, *IntegrityResult, error) {
	path, err := db.Snapshot(ctx, s.DB.Path(), s.snapshotDir(), now)
	if errors.Is(err, db.ErrRefused) {
		return "", nil, &Error{Status: http.StatusUnprocessableEntity, Msg: err.Error(), Code: CodeSnapshotRefused, Err: err}
	}
	if err != nil {
		return "", nil, err
	}
	d, err := db.OpenSnapshot(ctx, path)
	if err != nil {
		return failedSnapshot(path, nil, fmt.Errorf("the snapshot %s cannot be checked: %w", path, err))
	}
	res, err := restoreCheck(ctx, d)
	if err != nil {
		return failedSnapshot(path, nil, fmt.Errorf("the restore check of %s: %w", path, err))
	}
	if !res.OK {
		return failedSnapshot(path, res, nil)
	}
	return path, res, nil
}

// snapshotDir is the folder of the snapshots: SnapshotDir, or the folder of the database when none is set.
func (s *Store) snapshotDir() string {
	if s.SnapshotDir != "" {
		return s.SnapshotDir
	}
	return filepath.Dir(s.DB.Path())
}

// OpenSnapshot opens the good snapshot named name in the snapshot folder, for the owner's download. Only a name that
// the action gives (db.IsSnapshotName) is opened, inside the folder only (os.Root): not the live file, not a
// .failed.db and not a path. Such a name, or a name with no regular file, is a 404. The caller closes the file.
func (s *Store) OpenSnapshot(name string) (*os.File, error) {
	if !db.IsSnapshotName(name) {
		return nil, NotFound("no snapshot %q: a snapshot is named life-YYYY-MM-DD.db or life-YYYY-MM-DDTHHMMSS.db", name)
	}
	root, err := os.OpenRoot(s.snapshotDir())
	if err != nil {
		return nil, fmt.Errorf("open the snapshot folder: %w", err)
	}
	defer root.Close() // a file opened through the root stays open
	f, err := root.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, NotFound("no snapshot %s in the snapshot folder", name)
	}
	if err != nil {
		return nil, fmt.Errorf("open the snapshot %s: %w", name, err)
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("read the snapshot %s: %w", name, err)
		}
		return nil, NotFound("no snapshot %s in the snapshot folder", name)
	}
	return f, nil
}

// failedSnapshot keeps a snapshot that did not pass its restore check, for the owner to read, under the failed name
// (db.MarkFailed), and returns the new path with res and err as they are. A rename that fails is joined to err, and the
// path stays the old one.
func failedSnapshot(path string, res *IntegrityResult, err error) (string, *IntegrityResult, error) {
	failed, renameErr := db.MarkFailed(path)
	return failed, res, errors.Join(err, renameErr)
}

// restoreCheck runs the integrity checks on the open snapshot d, then closes it. A Close that fails is an error of the
// check: the copy is then not known to be left as written.
func restoreCheck(ctx context.Context, d *db.DB) (*IntegrityResult, error) {
	res, err := (&Store{DB: d}).Integrity(ctx)
	if err := errors.Join(err, d.Close()); err != nil {
		return nil, err
	}
	return res, nil
}
