// Package db opens life.db the way docs/contract/connections.md requires: the per-connection pragmas, read
// back and refused if wrong, the SQLite version floor, BEGIN IMMEDIATE for every write, and read-only readers.
package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"modernc.org/sqlite"

	"aminte"
)

// minVersion is the writer's floor (connections.md: the WAL race fixed in 3.51.3).
const minVersion = 3051003

const applicationID = 0x4C494645
const schemaVersion = 1 // docs/decisions/D13-migrations-and-freeze.md

// The pragmas every writing connection sets and reads back, with the value it must read back.
var pragmas = []struct {
	name string
	set  string
	want int64
}{
	{"foreign_keys", "1", 1},
	{"recursive_triggers", "1", 1},
	{"synchronous", "2", 2},
	{"trusted_schema", "0", 0},
	{"busy_timeout", "5000", 5000},
}

func init() {
	d := &sqlite.Driver{}
	d.RegisterConnectionHook(checkConnection)
	sql.Register("aminte", d)
}

// checkConnection runs on every new connection: the version floor always, trusted_schema on readers, the pragmas
// on writers. A reader is known by the mode of its DSN's query, never by the text of the path. A value the check
// cannot read refuses the connection: an unread pragma is not a pragma at its wanted value.
func checkConnection(c sqlite.ExecQuerierContext, dsn string) error {
	v, err := version(c)
	if err != nil {
		return fmt.Errorf("the connection check: %w", err)
	}
	if v < minVersion {
		return fmt.Errorf("SQLite %d is older than %d (connections.md)", v, minVersion)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("the connection check: %w", err)
	}
	if u.Query().Get("mode") == "ro" {
		got, err := intOf(c, "PRAGMA trusted_schema")
		if err != nil {
			return fmt.Errorf("the connection check on a reader: %w", err)
		}
		if got != 0 {
			return fmt.Errorf("PRAGMA trusted_schema reads back %d on a reader, want 0; refusing to read", got)
		}
		return nil
	}
	for _, p := range pragmas {
		got, err := intOf(c, "PRAGMA "+p.name)
		if err != nil {
			return fmt.Errorf("the connection check on a writer: %w", err)
		}
		if got != p.want {
			return fmt.Errorf("PRAGMA %s reads back %d, want %d; refusing to write", p.name, got, p.want)
		}
	}
	return nil
}

func intOf(c sqlite.ExecQuerierContext, q string) (int64, error) {
	v, err := valueOf(c, q)
	if err != nil {
		return 0, err
	}
	n, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("%s reads back %v, not an integer", q, v)
	}
	return n, nil
}

// valueOf is the one value a statement of the connection check answers. The driver's hook gives no context: the
// check runs as the connection opens, before any request.
func valueOf(c sqlite.ExecQuerierContext, q string) (driver.Value, error) {
	rows, err := c.QueryContext(context.Background(), q, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", q, err)
	}
	defer rows.Close()
	v := make([]driver.Value, 1)
	if err := rows.Next(v); err != nil {
		return nil, fmt.Errorf("%s: %w", q, err)
	}
	return v[0], nil
}

// version is sqlite_version() as SQLITE_VERSION_NUMBER (X*1000000 + Y*1000 + Z).
func version(c sqlite.ExecQuerierContext) (int64, error) {
	v, err := valueOf(c, "SELECT sqlite_version()")
	if err != nil {
		return 0, err
	}
	s, _ := v.(string)
	var x, y, z int64
	if _, err := fmt.Sscanf(s, "%d.%d.%d", &x, &y, &z); err != nil {
		return 0, fmt.Errorf("sqlite_version() %q: %w", s, err)
	}
	return x*1000000 + y*1000 + z, nil
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	if readOnly {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
		q.Add("_pragma", "trusted_schema(0)")
	} else {
		q.Set("_defensive", "1")
		q.Set("_txlock", "immediate") // database/sql's Begin sends BEGIN IMMEDIATE
		for _, p := range pragmas {
			q.Add("_pragma", p.name+"("+p.set+")")
		}
	}
	return sqliteFileURI(path, q)
}

func sqliteFileURI(path string, q url.Values) string {
	return (&url.URL{Scheme: "file", Opaque: sqliteURIPath(path), RawQuery: q.Encode()}).String()
}

func sqliteURIPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// DB is one life.db: a single writing connection and a pool of read-only ones.
type DB struct {
	W, R *sql.DB
	path string
	keep bool // a snapshot under its restore check: Close leaves the file as it was
}

// Path is the file this DB opened.
func (d *DB) Path() string { return d.path }

// Open opens an existing life.db.
func Open(ctx context.Context, path string) (*DB, error) { return open(ctx, path, false) }

func open(ctx context.Context, path string, keep bool) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = abs
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no database at %s (create one with `aminte init`): %w", path, err)
	}
	w, err := sql.Open("aminte", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1) // one writer; BEGIN IMMEDIATE serialises against other processes
	r, err := sql.Open("aminte", dsn(path, true))
	if err != nil {
		return nil, errors.Join(err, w.Close())
	}
	var id int64
	// Validate compatibility before activating the writer. Refusal must not run
	// Close's maintenance against a file this application does not understand.
	if err := r.QueryRowContext(ctx, "PRAGMA application_id").Scan(&id); err != nil {
		return nil, errors.Join(err, r.Close(), w.Close())
	}
	if id != applicationID {
		return nil, errors.Join(fmt.Errorf("%s is not an Aminte database (application_id %#x)", path, id), r.Close(), w.Close())
	}
	var version int64
	if err := r.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return nil, errors.Join(err, r.Close(), w.Close())
	}
	if version != schemaVersion {
		return nil, errors.Join(fmt.Errorf("unsupported user_version %d (supported: %d)", version, schemaVersion), r.Close(), w.Close())
	}
	if err := w.PingContext(ctx); err != nil {
		return nil, errors.Join(err, r.Close(), w.Close())
	}
	return &DB{W: w, R: r, path: path, keep: keep}, nil
}

// Init creates a new life.db from the canonical DDL. It refuses an existing file.
func Init(ctx context.Context, path string) error {
	return initWith(path, func(w *sql.DB) error {
		_, err := w.ExecContext(ctx, aminte.Schema)
		return err
	})
}

func initWith(path string, apply func(*sql.DB) error) error {
	reserved, err := reserveDatabasePath(path)
	if err != nil {
		return err
	}
	w, err := sql.Open("aminte", dsn(path, false))
	if err != nil {
		return errors.Join(err, removeReservedDatabasePath(path, reserved))
	}
	if err := apply(w); err != nil {
		closeErr := w.Close()
		cleanupErr := removeReservedDatabasePath(path, reserved)
		return errors.Join(fmt.Errorf("applying schema.sql: %w", err), closeErr, cleanupErr)
	}
	// Seed-only statistics make later bulk capture choose scans as the file grows.
	// Let ordinary connection-close maintenance collect statistics from real data.
	// Close SQLite first: on Linux, the close of a descriptor drops every lock that the process holds on the file.
	// A close that fails returns its error and keeps the file, with the schema in it: the cleanup cannot remove a
	// path after the close, because only the open file shows that the path still names it (reserveDatabasePath).
	return errors.Join(w.Close(), reserved.Close())
}

// reserveDatabasePath creates path, which must not exist, and returns the new file open. The caller keeps the file
// open until its work on path ends: then it closes the file, or gives it to removeReservedDatabasePath. An open file
// keeps its identity. On Linux its inode stays in use, so the file system cannot give the inode number to a new file
// (ext4 gives the number of a removed file to the next new file). On Windows no other handle can remove or rename
// the file, because Go opens it without FILE_SHARE_DELETE; the share modes of this handle and of SQLite's handle
// allow each other.
func reserveDatabasePath(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		if os.IsExist(err) {
			return nil, refusal(path + " already exists")
		}
		return nil, fmt.Errorf("reserving %s: %w", path, err)
	}
	return f, nil
}

// removeReservedDatabasePath closes the reserved file and removes path if path still names that file. It compares
// the two while the file is open, so os.SameFile cannot take another file for it (see reserveDatabasePath). It also
// removes the WAL of the file and its index, path-wal and path-shm, when SQLite wrote the file.
func removeReservedDatabasePath(path string, reserved *os.File) error {
	held, err := reserved.Stat()
	if err != nil {
		return errors.Join(fmt.Errorf("checking reserved database %s for cleanup: %w", path, err), reserved.Close())
	}
	current, err := os.Lstat(path)
	if err != nil {
		closeErr := reserved.Close()
		if os.IsNotExist(err) {
			return closeErr
		}
		return errors.Join(fmt.Errorf("checking reserved database %s for cleanup: %w", path, err), closeErr)
	}
	if !os.SameFile(held, current) {
		return reserved.Close()
	}
	// SQLite keeps the WAL and its index at its close when it cannot checkpoint, as after an interrupt. A WAL left
	// beside the path goes into the next database there. The two are of this file only when SQLite wrote the file:
	// SQLite reads a new file while it is empty, and it then deletes a WAL beside it. They go before the file, and
	// while the handle is still open: on Windows the handle keeps any other process from removing or renaming the
	// file, so the path still names it. On Linux another process that does not use Init can still change the path.
	if held.Size() > 0 {
		for _, side := range []string{path + "-wal", path + "-shm"} {
			if err := os.Remove(side); err != nil && !errors.Is(err, os.ErrNotExist) {
				// The file stays with its WAL, so that no WAL stays alone at the path.
				return errors.Join(fmt.Errorf("removing %s of failed database %s: %w", side, path, err), reserved.Close())
			}
		}
	}
	// Windows does not remove a file while a handle holds it, so the handle closes before the remove.
	closeErr := reserved.Close()
	if err := os.Remove(path); err != nil {
		return errors.Join(fmt.Errorf("removing failed database %s: %w", path, err), closeErr)
	}
	return closeErr
}

// Close runs PRAGMA optimize on the writer, except on a snapshot under its restore check, and then closes the
// readers and the writer. It closes them also when the optimize fails. A failed optimize is logged and is not an
// error of Close: it only keeps the planner statistics as they were, so no data is lost, and a command that is done
// must not exit 1 for it. Close has no caller's context: it runs at the end, often after a signal cancelled the
// operation's context, and the optimize must run then too. busy_timeout limits its wait for the lock.
func (d *DB) Close() error {
	if !d.keep {
		if _, err := d.W.ExecContext(context.Background(), "PRAGMA optimize"); err != nil {
			log.Printf("aminte: PRAGMA optimize skipped, no data is lost: %v", err)
		}
	}
	return errors.Join(d.R.Close(), d.W.Close())
}

// Write runs fn in one BEGIN IMMEDIATE transaction, committed when fn returns nil. When fn fails and the rollback
// fails too, Write returns a *RollbackError with both errors: the transaction can then still be open, or kept in part.
func (d *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		// ErrTxDone: database/sql rolled the transaction back already, because ctx was cancelled.
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			return &RollbackError{Err: err, Rollback: rerr}
		}
		return err
	}
	return tx.Commit()
}

// RollbackError is the error of a Write, or of a savepoint inside one, whose fn failed and whose rollback failed too.
// Err is the error of fn and Rollback the error of the rollback; errors.Is and errors.As find both. A caller must not
// read it as the error of fn alone: the transaction can still be open, or kept in part.
type RollbackError struct{ Err, Rollback error }

func (e *RollbackError) Error() string   { return e.Err.Error() + "\nrollback: " + e.Rollback.Error() }
func (e *RollbackError) Unwrap() []error { return []error{e.Err, e.Rollback} }

// Copy writes a copy of a life.db to a new file through a read-only connection (docs/contract/imports.md step 1:
// VACUUM INTO). It refuses an existing target.
func Copy(ctx context.Context, from, to string) error {
	// An absolute path also keeps a relative filename beginning "file:" from
	// becoming a SQLite URI with a different destination from our reservation.
	to, err := filepath.Abs(to)
	if err != nil {
		return err
	}
	return copyWith(from, to, func(r *sql.DB) error {
		_, err := r.ExecContext(ctx, `VACUUM INTO ?`, to)
		return err
	})
}

func copyWith(from, to string, copy func(*sql.DB) error) error {
	if _, err := os.Stat(from); err != nil {
		return fmt.Errorf("no database at %s: %w", from, err)
	}
	// VACUUM INTO accepts an existing empty file. Reserve that file exclusively:
	// a separate existence check would follow dangling links and race other copies.
	reserved, err := reserveDatabasePath(to)
	if err != nil {
		return err
	}
	// Not dsn(from, true): a reader's query_only refuses VACUUM INTO ("attempt to write a readonly database"),
	// although the copy writes nothing to from. mode=ro and trusted_schema=OFF still hold.
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "trusted_schema(0)")
	r, err := sql.Open("aminte", sqliteFileURI(from, q))
	if err != nil {
		return errors.Join(err, removeReservedDatabasePath(to, reserved))
	}
	copyErr := copy(r)
	if err := errors.Join(copyErr, r.Close()); err != nil {
		return errors.Join(err, removeReservedDatabasePath(to, reserved))
	}
	// A close that fails returns its error and keeps the copy that VACUUM INTO wrote, as initWith keeps its file.
	// Snapshot returns the path with the error.
	return reserved.Close()
}

// ErrRefused is the cause of a refusal: the caller asks for what cannot be, and nothing failed. A snapshot folder
// that is not a folder or is inside a git work tree, and a database file that exists already, are refusals.
// errors.Is tells a refusal from a failure (a disk, a permission, a context that ran out).
var ErrRefused = errors.New("refused")

// refusal is an error whose cause is ErrRefused, with its own message.
type refusal string

func (r refusal) Error() string { return string(r) }
func (refusal) Unwrap() error   { return ErrRefused }

// Snapshot takes a snapshot of a life.db into dir (docs/cookbook/take-a-snapshot.md, D25): a Copy named by the local
// day of now, life-YYYY-MM-DD.db, or life-YYYY-MM-DDTHHMMSS.db when the day has one already. It never overwrites a
// file and refuses a folder inside a git work tree. It returns the snapshot's path.
func Snapshot(ctx context.Context, from, dir string, now time.Time) (string, error) {
	// Use the resolved directory path so the supplied directory symlink is no longer traversed.
	dir, err := CheckSnapshotDir(dir)
	if err != nil {
		return "", err
	}
	to := filepath.Join(dir, "life-"+now.Format("2006-01-02")+".db")
	if _, err := os.Lstat(to); err == nil {
		to = filepath.Join(dir, "life-"+now.Format("2006-01-02T150405")+".db")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check snapshot path %s: %w", to, err)
	}
	return to, Copy(ctx, from, to)
}

// snapshotName is the name that Snapshot gives a good snapshot. A .failed.db name, the live file and its -wal and -shm
// do not match it.
var snapshotName = regexp.MustCompile(`^life-[0-9]{4}-[0-9]{2}-[0-9]{2}(T[0-9]{6})?\.db$`)

// IsSnapshotName says whether name is the name of a good snapshot, as Snapshot writes it.
func IsSnapshotName(name string) bool { return snapshotName.MatchString(name) }

// MarkFailed renames a snapshot that did not pass its restore check from NAME.db to NAME.failed.db
// (life-2026-10-02.failed.db, life-2026-10-02T143005.failed.db), and returns the new path. The old name is then free,
// and no routine that restores or counts life-*.db snapshots by their good names can pick the file (D25). It never
// overwrites a file: when the failed name is taken, it puts a number before ".failed.db" (NAME.2.failed.db).
func MarkFailed(path string) (string, error) {
	base, ok := strings.CutSuffix(path, ".db")
	if !ok {
		return path, fmt.Errorf("mark failed snapshot %s: the name does not end in .db", path)
	}
	for n := 1; ; n++ {
		to := base + ".failed.db"
		if n > 1 {
			to = fmt.Sprintf("%s.%d.failed.db", base, n)
		}
		_, err := os.Lstat(to)
		if err == nil {
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return path, fmt.Errorf("mark failed snapshot %s: %w", path, err)
		}
		if err := os.Rename(path, to); err != nil {
			return path, fmt.Errorf("mark failed snapshot %s: %w", path, err)
		}
		return to, nil
	}
}

// CheckSnapshotDir is the folder a snapshot may land in, resolved to its physical path: it must exist, and
// neither it nor what it resolves to may lie inside a git work tree (a snapshot holds health data and private
// notes, which git history cannot forget). A command checks it before it opens anything.
func CheckSnapshotDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	// Only a path that is not a folder is the refusal: a folder the process cannot reach (a permission, a disk) is
	// a fault.
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || err == nil && !fi.IsDir():
		return "", refusal(fmt.Sprintf("no folder %s for the snapshot", dir))
	case err != nil:
		return "", fmt.Errorf("check snapshot folder %s: %w", dir, err)
	}
	physical, err := snapshotPhysicalDir(dir)
	if err != nil {
		return "", fmt.Errorf("resolve snapshot folder %s: %w", dir, err)
	}
	for _, candidate := range []string{dir, physical} {
		repo, err := gitWorkTree(candidate)
		if err != nil {
			return "", fmt.Errorf("check snapshot folder %s: %w", candidate, err)
		}
		if repo != "" {
			return "", refusal(fmt.Sprintf("%s is inside the git work tree %s: a snapshot holds health data and private notes, "+
				"which git history cannot forget; choose a folder outside it", candidate, repo))
		}
	}
	return physical, nil
}

// snapshotPhysicalDir also follows Windows junctions, which Go 1.27 marks
// ModeIrregular rather than ModeSymlink (so EvalSymlinks alone skips them).
func snapshotPhysicalDir(dir string) (string, error) {
	if runtime.GOOS != "windows" {
		return filepath.EvalSymlinks(dir)
	}
	for links := 0; links < 255; links++ {
		volume := filepath.VolumeName(dir)
		parts := strings.Split(strings.TrimLeft(dir[len(volume):], `\/`), string(filepath.Separator))
		current := volume + string(filepath.Separator)
		followed := false
		for i, part := range parts {
			current = filepath.Join(current, part)
			fi, err := os.Lstat(current)
			if err != nil {
				return "", err
			}
			if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
				continue
			}
			target, err := os.Readlink(current)
			if err != nil {
				return "", fmt.Errorf("read directory link %s: %w", current, err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			dir = filepath.Join(append([]string{target}, parts[i+1:]...)...)
			followed = true
			break
		}
		if !followed {
			return filepath.EvalSymlinks(dir)
		}
	}
	return "", fmt.Errorf("too many directory links in snapshot folder %s", dir)
}

// gitWorkTree is the folder at or above dir that holds a .git entry, or "".
func gitWorkTree(dir string) (string, error) {
	for d := dir; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		up := filepath.Dir(d)
		if up == d {
			return "", nil
		}
		d = up
	}
}

// OpenSnapshot opens a snapshot for its restore check (docs/cookbook/take-a-snapshot.md): as Open opens life.db —
// the FTS5 check is an INSERT, refused on a read-only connection — but Close runs no PRAGMA optimize, which can
// write to the file, so the checks leave the snapshot as it was.
func OpenSnapshot(ctx context.Context, path string) (*DB, error) { return open(ctx, path, true) }

// readerIdleConns is the number of idle read connections that the pool keeps. It is the default of database/sql:
// nothing in this package sets another.
const readerIdleConns = 2

// RenewReaders closes the idle read connections, so the next read opens a new one. A connection keeps state of a
// virtual table between its statements. After the FTS5 'rebuild' of another connection, a reader that read the old
// index still reports corruption of the new one in PRAGMA integrity_check. A reader in use stays in the pool when it
// is released: a caller that must not meet one runs RenewReaders when no read is open.
func (d *DB) RenewReaders() {
	d.R.SetMaxIdleConns(0)
	d.R.SetMaxIdleConns(readerIdleConns)
}
