// Package core is the one place that writes life.db: every write is a named operation that runs the SQL of
// docs/cookbook/ inside one BEGIN IMMEDIATE transaction. The rules are the docs'; this package cites them.
package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"lifelog/internal/db"
	"lifelog/text"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// nowSQL is the instant format of lifelog_meta.instants, written by SQLite so every row of a statement agrees.
const nowSQL = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"

// nullIfEmpty is the SQL value of an optional text: NULL when the text is empty.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Store is one open life.db. SnapshotDir is the folder the snapshot action writes to; empty means beside the file.
// Clock and Zone are the clock of the writer. Every surface reads from them today (Today), and the zone of the words
// for a time and the name of a snapshot (Local). A nil Clock is time.Now and a nil Zone is time.Local, the clock of this device; a test gives a fixed clock. The instants that a write stores in a row
// (created_at, updated_at and deleted_at) do not come from this clock: SQLite writes them from its own clock (nowSQL
// and the triggers of the schema). The taken_at of a reading is the client's.
type Store struct {
	DB          *db.DB
	SnapshotDir string
	// SettingsPath is the settings file (settings.go); empty means the store has none, and a read or a write of the
	// settings is refused.
	SettingsPath string
	Clock        func() time.Time
	Zone         *time.Location
}

// now is the instant of the store's clock: the Clock, else time.Now.
func (s *Store) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock()
}

// Local is the instant of the store's clock in the store's zone: its day is today, and the words for a time and the
// name of a snapshot are in its zone.
func (s *Store) Local() time.Time {
	zone := s.Zone
	if zone == nil {
		zone = time.Local
	}
	return s.now().In(zone)
}

// Today is the local calendar day of the store's clock (lifelog_meta.days).
func (s *Store) Today() string { return s.Local().Format(time.DateOnly) }

// Error carries an HTTP-shaped status so every surface reports the same failure the same way. Code, when set,
// names the failure for a client that branches on it (docs/plans/079-client-contract-hygiene.md); the API fills
// a general one from the status otherwise.
type Error struct {
	Status int
	Msg    string
	Code   string
	Err    error  // the cause, when one is kept for errors.Is: a context that ran out, a driver's refusal
	Metric string // for a 404: the name of the metric that does not exist, so a surface can offer to add it
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

// invalidCause is a 422 that keeps its cause: SQLite's refusal of a query, with the message as the client sees it.
func invalidCause(err error, format string, a ...any) error {
	return &Error{Status: http.StatusUnprocessableEntity, Msg: fmt.Sprintf(format, a...), Err: err}
}

// NotFound is a 404: the row the request names does not exist.
func NotFound(format string, a ...any) error {
	return &Error{Status: http.StatusNotFound, Msg: fmt.Sprintf(format, a...)}
}

// Invalid is a 422: the request is well-formed, and a rule refuses it.
func Invalid(format string, a ...any) error {
	return &Error{Status: http.StatusUnprocessableEntity, Msg: fmt.Sprintf(format, a...)}
}

// Conflict is a 409 with no code: the row is not in the state the request needs.
func Conflict(format string, a ...any) error {
	return &Error{Status: http.StatusConflict, Msg: fmt.Sprintf(format, a...)}
}

// conflictCode is a 409 with a code a client branches on (e.g. stale_version).
func conflictCode(code, format string, a ...any) error {
	return &Error{Status: http.StatusConflict, Msg: fmt.Sprintf(format, a...), Code: code}
}

// The codes of the failures that core names. The API sends them as the code of a problem, and siren holds the same
// strings for the clients, which must not import core (internal/api/codes_test.go compares the two).
const (
	CodeStaleVersion    = "stale_version"    // a page saved from a version that is not the current one
	CodeSnapshotRefused = "snapshot_refused" // the snapshot action refused its target
	// CodeBusy is the code of a write that met a locked file (SQLITE_BUSY): the CLI and the service write one file,
	// and a write can wait longer than busy_timeout for the other.
	CodeBusy = "busy"
)

// driverRefusal matches the words that the driver adds round the text of SQLite: a "constraint failed: " prefix and
// the extended result code in brackets at the end ("(275)").
var driverRefusal = regexp.MustCompile(`(?s)^(?:constraint failed: )?(.*?)(?: \(\d+\))?$`)

// plainRefusal is the text of a refusal without the driver's words. What stays is the text of SQLite: the name of a
// failed constraint ("CHECK constraint failed: people_death_day_order"), or the message of a trigger.
func plainRefusal(msg string) string {
	return driverRefusal.ReplaceAllString(msg, "$1")
}

// refused turns a constraint or trigger failure of the DDL into a 422 with SQLite's own message. A write that found the
// file locked (SQLITE_BUSY, with its extended codes) is a 503 with CodeBusy: its transaction rolled back, so nothing was
// applied and a retry is safe. A write whose
// rollback failed (db.RollbackError) is a fault, whatever its fn returned: the transaction can be open or kept in part.
// Its text keeps the error of fn, and its chain only the error of the rollback, so no surface reads it as a refusal.
func refused(err error) error {
	if err == nil {
		return nil
	}
	var rb *db.RollbackError
	if errors.As(err, &rb) {
		return fmt.Errorf("%v\nrollback: %w", rb.Err, rb.Rollback)
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code()&0xff == sqlite3.SQLITE_CONSTRAINT { // CHECKs, FKs, UNIQUE and RAISE(ABORT)
		return &Error{Status: http.StatusUnprocessableEntity, Msg: plainRefusal(se.Error())}
	}
	if errors.As(err, &se) && se.Code()&0xff == sqlite3.SQLITE_BUSY {
		return &Error{Status: http.StatusServiceUnavailable, Msg: "the database file is busy: another writer holds it; nothing was written, so a retry is safe", Code: CodeBusy, Err: err}
	}
	return err
}

// The checks of a surface's input that every write and read shares, so one refusal has one message.

// jsonArray is a list of strings or ids as a JSON array, for a query that reads it with json_each. A value of these
// types always encodes, so it has no error. A nil list is "null": give an empty list.
func jsonArray[T string | int64](list []T) string {
	b, _ := json.Marshal(list)
	return string(b)
}

// checkDay refuses a value that is not a local day (lifelog_meta.days); what names the field.
func checkDay(what, s string) error {
	if !text.IsDay(s) {
		return Invalid("%s %q is not a day (YYYY-MM-DD)", what, s)
	}
	return nil
}

// checkWindow refuses a window that is not two local days in order, both inclusive.
func checkWindow(from, through string) error {
	if !text.IsDay(from) || !text.IsDay(through) || from > through {
		return Invalid("from %q and through %q must be days (YYYY-MM-DD), from first", from, through)
	}
	return nil
}

// checkTitle refuses a title the contract does not admit (docs/contract/titles-and-wikilinks.md); what names the field.
func checkTitle(what, title string) error {
	if !text.ValidTitle(title) {
		return Invalid("%s %q is not a valid title (docs/contract/titles-and-wikilinks.md)", what, title)
	}
	return nil
}

// The one loop of core over the rows of a query. rows and err are what QueryContext returned, so a caller passes its
// call straight in. Every Scan, Err and Close branch is here.

// each scans every row into dst and then runs row. A Scan error ends the read. The rows are closed in every case, and
// the error of the query, of a Scan, of the rows (Err) or of their Close is returned.
func each(rows *sql.Rows, err error, dst []any, row func()) (e error) {
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			e = errors.Join(e, cerr)
		}
	}()
	for rows.Next() {
		if err := rows.Scan(dst...); err != nil {
			return err
		}
		row()
	}
	return rows.Err()
}

// list reads every row into a slice: fields gives the destinations of one row's columns in v. The slice is never nil
// when the read succeeds, so an empty list is [] in JSON.
func list[T any](rows *sql.Rows, err error, fields func(v *T) []any) ([]T, error) {
	var v T
	out := []T{}
	if err := each(rows, err, fields(&v), func() { out = append(out, v) }); err != nil {
		return nil, err
	}
	return out, nil
}

// groupCounts reads the rows of a GROUP BY count, a key and its count, into a map. A key with no row is absent.
func groupCounts(rows *sql.Rows, err error) (map[string]int, error) {
	out := map[string]int{}
	var k string
	var n int
	if err := each(rows, err, []any{&k, &n}, func() { out[k] = n }); err != nil {
		return nil, err
	}
	return out, nil
}
