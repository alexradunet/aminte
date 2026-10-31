package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/internal/db"
)

// A local driver exercises database/sql's actual Scan, EOF and automatic Close paths.
// No SQLite corruption is implied by these execution-failure fixtures.
type integrityConnector struct {
	check, fault           string
	closed, queries, execs int
	deadlines              []time.Time // the deadline of the context of each query and exec; zero when it has none
}

func (c *integrityConnector) noteDeadline(ctx context.Context) {
	d, _ := ctx.Deadline()
	c.deadlines = append(c.deadlines, d)
}

func (c *integrityConnector) Connect(context.Context) (driver.Conn, error) {
	return &integrityConn{c}, nil
}
func (c *integrityConnector) Driver() driver.Driver { return integrityDriver{} }

type integrityDriver struct{}

func (integrityDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type integrityConn struct{ c *integrityConnector }

func (*integrityConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*integrityConn) Begin() (driver.Tx, error) { return integrityTx{}, nil }
func (*integrityConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return integrityTx{}, nil
}

type integrityTx struct{}

func (integrityTx) Commit() error   { return nil }
func (integrityTx) Rollback() error { return nil }
func (c *integrityConn) Close() error {
	if c.c.fault == "conn-close" {
		return errIntegrityFault
	}
	return nil
}

var errIntegrityFault = errors.New("synthetic integrity execution failure")

func (c *integrityConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.c.noteDeadline(ctx)
	c.c.queries++
	check := "orphan entities"
	if strings.Contains(query, "PRAGMA integrity_check") {
		check = "integrity_check"
	} else if strings.Contains(query, "PRAGMA foreign_key_check") {
		check = "foreign_key_check"
	} else if strings.Contains(query, "WITH RECURSIVE rooted") {
		check = "measurement correction chains"
	} else if strings.Contains(query, "SELECT n.id FROM entity_names") {
		check = "journal name ownership"
	} else if strings.Contains(query, "JOIN link_kinds") {
		check = "typed link endpoints"
	} else if strings.Contains(query, "SELECT l.id FROM links") {
		check = "link day-page rules"
	} else if strings.Contains(query, "JOIN metrics x") {
		check = "scale ranges"
	} else if strings.Contains(query, "SELECT m.id FROM measurements") {
		check = "measurement corrections"
	} else if strings.Contains(query, "FROM entity_names ORDER BY id") {
		check = "name keys"
	} else if strings.Contains(query, "preferred_name_key FROM entities WHERE deleted_at IS NULL") {
		check = "link drift"
	} else if strings.Contains(query, "FROM sqlite_schema") {
		check = "schema drift"
	}
	fault := ""
	if check == c.c.check {
		fault = c.c.fault
	}
	if fault == "query" {
		return nil, errIntegrityFault
	}
	return &integrityRows{c: c.c, check: check, fault: fault}, nil
}
func (c *integrityConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	c.c.noteDeadline(ctx)
	c.c.execs++
	switch c.c.fault {
	case "fts":
		return nil, errIntegrityFault
	case "fts-deadline":
		return nil, context.DeadlineExceeded
	}
	return driver.RowsAffected(0), nil
}

type integrityRows struct {
	c            *integrityConnector
	check, fault string
	index        int
}

func (r *integrityRows) Columns() []string {
	if r.check == "foreign_key_check" {
		return []string{"table", "rowid", "parent", "fkid"}
	}
	if r.check == "name keys" {
		return []string{"id", "title", "name_key"}
	}
	if r.check == "schema drift" {
		return []string{"type", "name", "tbl_name", "sql"}
	}
	if r.check == "link drift" {
		return []string{"id", "body", "preferred_name_key"}
	}
	return []string{"value"}
}
func (r *integrityRows) Close() error {
	r.c.closed++
	if r.fault == "close" || r.fault == "scan-close" {
		return errIntegrityFault
	}
	return nil
}
func (r *integrityRows) Next(dest []driver.Value) error {
	count := 0
	if r.check == "integrity_check" || r.fault != "" {
		count = 1
	}
	if r.fault == "scan" || r.fault == "scan-close" {
		count = 2
	}
	if r.index >= count {
		if r.fault == "late" {
			return errIntegrityFault
		}
		return io.EOF
	}
	if r.check == "foreign_key_check" {
		copy(dest, []driver.Value{"measurements", int64(7), "metrics", int64(0)})
	} else if r.check == "name keys" {
		copy(dest, []driver.Value{int64(42), "Title", "not the key of Title"})
	} else if r.check == "schema drift" {
		copy(dest, []driver.Value{"table", "stray", "stray", "CREATE TABLE stray(a)"})
	} else if r.check == "link drift" {
		copy(dest, []driver.Value{int64(42), "[[Linked]]", "own"}) // its target has no page: the links differ
	} else if r.check == "integrity_check" {
		dest[0] = "ok"
	} else {
		dest[0] = int64(42)
	}
	if r.fault == "diagnostic" && r.check == "integrity_check" {
		dest[0] = "synthetic structural diagnostic"
	}
	if r.fault == "diagnostic" && r.check == "foreign_key_check" {
		dest[1] = nil
	}
	if r.index == 1 {
		if r.check == "integrity_check" {
			dest[0] = nil
		} else if r.check == "foreign_key_check" {
			dest[1] = "not an integer"
		} else if r.check == "name keys" {
			dest[0] = "not an integer"
		} else if r.check == "schema drift" {
			dest[0] = nil
		} else if r.check == "link drift" {
			dest[0] = "not an integer"
		} else {
			dest[0] = "not an integer"
		}
	}
	r.index++
	return nil
}
func faultIntegrityStore(t *testing.T, c *integrityConnector) *Store {
	t.Helper()
	d := sql.OpenDB(c)
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	return &Store{DB: &db.DB{R: d, W: d}}
}

// integrityCheckNames are the reads of Integrity in the order it runs them.
var integrityCheckNames = []string{"integrity_check", "foreign_key_check", "orphan entities", "typed link endpoints", "measurement corrections",
	"measurement correction chains", "journal name ownership", "link day-page rules", "scale ranges", "name keys", "link drift", "schema drift"}

func TestIntegrityReadFailures(t *testing.T) {
	for i, check := range integrityCheckNames {
		for _, fault := range []string{"query", "late", "scan", "close", "scan-close"} {
			t.Run(check+"/"+fault, func(t *testing.T) {
				c := &integrityConnector{check: check, fault: fault}
				result, err := faultIntegrityStore(t, c).Integrity(context.Background())
				if err == nil || !strings.Contains(err.Error(), check) {
					t.Errorf("result=%+v error=%v; want identified execution failure", result, err)
				}
				if fault != "scan" && !errors.Is(err, errIntegrityFault) {
					t.Errorf("lost fault: %v", err)
				}
				wantClosed := i + 1
				if fault == "query" {
					wantClosed--
				}
				if c.closed != wantClosed || c.queries != i+1 || c.execs != 0 {
					t.Errorf("closed=%d queries=%d execs=%d; want %d/%d/0", c.closed, c.queries, c.execs, wantClosed, i+1)
				}
			})
		}
	}
}
func TestIntegrityResultSemantics(t *testing.T) {
	c := &integrityConnector{}
	r, err := faultIntegrityStore(t, c).Integrity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || !r.FullTextIndexOK || r.FullTextError != "" || len(r.IntegrityCheck) != 1 || r.IntegrityCheck[0] != "ok" || r.ForeignKeys != 0 || len(r.OrphanEntities) != 0 {
		t.Errorf("unexpected result: %+v", r)
	}
	if c.closed != len(integrityCheckNames) || c.execs != 1 {
		t.Errorf("closed=%d execs=%d", c.closed, c.execs)
	}
}

// An error of the full-text step that is not a corruption result (a busy file, a deadline, a cancelled context) is
// returned as an error. It is not a finding: it says nothing about the index.
func TestIntegrityFullTextStepFailureIsAnErrorNotAFinding(t *testing.T) {
	for _, fault := range []string{"fts", "fts-deadline"} {
		t.Run(fault, func(t *testing.T) {
			c := &integrityConnector{fault: fault}
			r, err := faultIntegrityStore(t, c).Integrity(context.Background())
			if err == nil || r != nil {
				t.Fatalf("result=%+v error=%v; want no result and an error", r, err)
			}
			if want := map[string]error{"fts": errIntegrityFault, "fts-deadline": context.DeadlineExceeded}[fault]; !errors.Is(err, want) {
				t.Errorf("error %v does not wrap %v", err, want)
			}
		})
	}
}

// A damaged full-text index is a finding, not an error: the index and its content drift apart (the triggers that keep
// them equal are dropped on this throwaway file), and the check reports that.
func TestIntegrityReportsADamagedFullTextIndex(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Indexed", "first words", "")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB.R.Query(`SELECT name FROM sqlite_schema WHERE type='trigger' AND name LIKE '%fts%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Close(); err != nil || len(names) == 0 {
		t.Fatalf("triggers %v: %v", names, err)
	}
	for _, n := range names {
		if _, err := s.DB.W.Exec(`DROP TRIGGER ` + n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.W.Exec(`UPDATE entities SET body='other words entirely' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	r, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.OK || r.FullTextIndexOK || r.FullTextError == "" {
		t.Errorf("a damaged index: %+v", r)
	}
}

// Every read and the full-text step, which holds the writer connection, run under the deadline of Integrity.
func TestIntegrityRunsUnderItsDeadline(t *testing.T) {
	c := &integrityConnector{}
	start := time.Now()
	if _, err := faultIntegrityStore(t, c).Integrity(context.Background()); err != nil {
		t.Fatal(err)
	}
	end := time.Now()
	if len(c.deadlines) != len(integrityCheckNames)+1 { // the reads and the full-text exec
		t.Fatalf("%d statements noted; want %d", len(c.deadlines), len(integrityCheckNames)+1)
	}
	for i, d := range c.deadlines {
		if d.IsZero() || d.After(end.Add(integrityDeadline)) || !d.After(start.Add(integrityDeadline-time.Minute)) {
			t.Errorf("statement %d: deadline %v; want about %v after the call (%v to %v)", i, d, integrityDeadline, start, end)
		}
	}
}

// A run that its context ends gives an error and no result, not a result of the checks that ran.
func TestIntegrityEndsWithTheErrorOfItsContext(t *testing.T) {
	s := fresh(t)
	done, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := s.Integrity(done)
	if r != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%+v error=%v; want no result and context.Canceled", r, err)
	}
}

func TestIntegrityDiagnostics(t *testing.T) {
	for _, check := range integrityCheckNames {
		t.Run(check, func(t *testing.T) {
			c := &integrityConnector{check: check, fault: "diagnostic"}
			r, err := faultIntegrityStore(t, c).Integrity(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if r.OK == (check != "schema drift") || !r.FullTextIndexOK { // a drift is outside ok
				t.Errorf("diagnostic semantics: %+v", r)
			}
			switch check {
			case "integrity_check":
				if len(r.IntegrityCheck) != 1 || r.IntegrityCheck[0] != "synthetic structural diagnostic" {
					t.Errorf("diagnostic lost: %+v", r)
				}
			case "foreign_key_check":
				if r.ForeignKeys != 1 {
					t.Errorf("foreign key diagnostic lost: %+v", r)
				}
			case "typed link endpoints":
				if len(r.InvalidTypedLinks) != 1 || r.InvalidTypedLinks[0] != 42 {
					t.Errorf("typed-link diagnostic lost: %+v", r)
				}
			case "measurement corrections":
				if len(r.InvalidMeasurementCorrections) != 1 || r.InvalidMeasurementCorrections[0] != 42 {
					t.Errorf("correction diagnostic lost: %+v", r)
				}
			case "measurement correction chains":
				if len(r.InvalidMeasurementChains) != 1 || r.InvalidMeasurementChains[0] != 42 {
					t.Errorf("semantic diagnostic lost: %+v", r)
				}
			case "journal name ownership":
				if len(r.InvalidJournalNames) != 1 || r.InvalidJournalNames[0] != 42 {
					t.Errorf("semantic diagnostic lost: %+v", r)
				}
			case "orphan entities":
				if len(r.OrphanEntities) != 1 || r.OrphanEntities[0] != 42 {
					t.Errorf("orphan diagnostic lost: %+v", r)
				}
			case "link day-page rules":
				if len(r.InvalidLinkDayRules) != 1 || r.InvalidLinkDayRules[0] != 42 {
					t.Errorf("link day-page diagnostic lost: %+v", r)
				}
			case "scale ranges":
				if len(r.InvalidScaleValues) != 1 || r.InvalidScaleValues[0] != 42 {
					t.Errorf("scale diagnostic lost: %+v", r)
				}
			case "schema drift":
				if !slices.Contains(r.SchemaDrift, "extra table stray") {
					t.Errorf("schema diagnostic lost: %+v", r)
				}
			case "link drift":
				if len(r.LinkDrift) != 1 || r.LinkDrift[0] != 42 {
					t.Errorf("link drift diagnostic lost: %+v", r)
				}
			case "name keys":
				if len(r.NameKeyMismatches) != 1 || r.NameKeyMismatches[0] != 42 {
					t.Errorf("name key diagnostic lost: %+v", r)
				}
			}
			want := len(integrityCheckNames)
			if check == "link drift" {
				want++ // the lookup of the target, which has no page: the links differ before the stored links are read
			}
			if c.closed != want || c.execs != 1 {
				t.Errorf("closed=%d execs=%d; want %d/1", c.closed, c.execs, want)
			}
		})
	}
}

func TestIntegrityDetectsDeliberatePreferredOwnerDamage(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Owner", "prose", "")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberate external damage only: same pooled connection, FK disabled for this write.
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=OFF; UPDATE entities SET preferred_name_key='mood' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	r, err := s.Integrity(ctx)
	if err != nil || r.OK || !reflect.DeepEqual(r.OrphanEntities, []int64{id}) {
		t.Fatalf("independent preferred-owner diagnostic: %+v %v", r, err)
	}
}

// The measurement-corrections check finds a correction of another metric even when the trigger that refuses it is
// gone: the semantic query does not trust the triggers of the file it checks.
func TestIntegrityDetectsACorrectionOfAnotherMetric(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"Steps", "Floors"} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: name, Unit: "count"}); err != nil {
			t.Fatal(err)
		}
	}
	root, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec("DROP TRIGGER measurements_supersede_metric"); err != nil {
		t.Fatal(err)
	}
	var damaged int64
	if err := s.DB.W.QueryRow(`INSERT INTO measurements(metric_id,day,value,supersedes_id,source,created_at) SELECT (SELECT entity_id FROM entity_names WHERE name_key='floors'),'2020-01-02',3,?,'ui',`+nowSQL+` RETURNING id`, root).Scan(&damaged); err != nil {
		t.Fatal(err)
	}
	result, err := s.Integrity(ctx)
	if err != nil || result.OK || result.ForeignKeys != 0 || !result.FullTextIndexOK || len(result.InvalidMeasurementCorrections) != 1 || result.InvalidMeasurementCorrections[0] != damaged {
		t.Fatalf("correction damage: %+v %v", result, err)
	}
}
