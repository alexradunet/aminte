package core

import (
	"context"
	"database/sql"
	"errors"

	"aminte/internal/db"
)

// Tx is one BEGIN IMMEDIATE write transaction and the writer whose rows it writes (aminte_meta.source).
// Every write operation is a method on it, so one write can do several operations: CreateMetric registers a metric
// and files it, and internal/scaletest writes each synthetic day in one transaction and its notes 50 to a
// transaction. Each Store method is one operation alone. Its SQL uses the context of the owning Do call.
type Tx struct {
	tx     *sql.Tx
	ctx    context.Context
	today  func() string // the local day of the store's clock (Store.Today)
	source string        // the writer (aminte_meta.source), held by the CHECKs of the rows that carry it
}

// Do runs fn in one write transaction, committed when fn returns nil. It does not check source: a row that carries it
// meets entities_source, measurements_source or links_source, and refused gives the 422.
func (s *Store) Do(ctx context.Context, source string, fn func(*Tx) error) error {
	return refused(s.DB.Write(ctx, func(tx *sql.Tx) error { return fn(&Tx{tx: tx, ctx: ctx, today: s.Today, source: source}) }))
}

// alone runs fn as the one operation of its own write transaction (Do): the Store form of a Tx method. s stands for
// the receiver, because a method cannot have a type parameter. On an error the write did not commit, or its rollback
// failed too (a fault, as refused says). In both cases the result is the zero value: no surface reports what the
// write did not keep.
func alone[T any](s *Store, ctx context.Context, source string, fn func(*Tx) (T, error)) (T, error) {
	var v T
	if err := s.Do(ctx, source, func(t *Tx) (err error) { v, err = fn(t); return err }); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// errDryRun rolls a DryRun back after fn succeeded.
var errDryRun = errors.New("dry run")

// dryRun runs fn in a write transaction and always rolls it back: what a write would do, without doing it. Only
// errDryRun itself is a clean dry run: a rollback that failed after it is a fault (refused).
func (s *Store) dryRun(ctx context.Context, source string, fn func(*Tx) error) error {
	err := s.Do(ctx, source, func(t *Tx) error {
		if err := fn(t); err != nil {
			return err
		}
		return errDryRun
	})
	if err == errDryRun {
		return nil
	}
	return err
}

// savepoint runs fn inside a savepoint of the transaction. When fn fails and the savepoint rolls back what fn wrote,
// the transaction goes on: skipped is the error of fn, as refused gives it (a constraint or trigger of the DDL is the
// 422 that Do would return; another error that SQLite rolled back to the savepoint, such as a function that does not
// exist, stays as it is). A wikilink target that cannot be made: the rest of the work is unaffected (syncWikilinks).
// savepoint is the one place where a Tx method catches an error of its SQL and goes on: ROLLBACK TO succeeds only
// while the transaction and the savepoint are open, so a skip cannot hide a transaction that SQLite ended. fault is
// an error of the savepoint itself, and the caller must fail the write with it: the SAVEPOINT failed, the RELEASE
// failed, or fn failed and the savepoint then could not roll back or release (a *db.RollbackError with both errors,
// as db.Write gives for a transaction). The state of the transaction is then not known. A RELEASE that fails can keep
// what fn wrote, and when SAVEPOINT or RELEASE fails because the file is full, SQLite can roll back the whole
// transaction.
func (t *Tx) savepoint(fn func() error) (skipped, fault error) {
	if _, err := t.tx.ExecContext(t.ctx, `SAVEPOINT step`); err != nil {
		return nil, err
	}
	if err := fn(); err != nil {
		if _, e := t.tx.ExecContext(t.ctx, `ROLLBACK TO step`); e != nil {
			return nil, &db.RollbackError{Err: err, Rollback: e}
		}
		if _, e := t.tx.ExecContext(t.ctx, `RELEASE step`); e != nil {
			return nil, &db.RollbackError{Err: err, Rollback: e}
		}
		return refused(err), nil
	}
	_, err := t.tx.ExecContext(t.ctx, `RELEASE step`)
	return nil, err
}
