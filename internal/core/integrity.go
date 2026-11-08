package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"aminte/text"
)

// IntegrityResult is the check groups of docs/contract/integrity-checks.md; OK when all checks of the data pass.
// SchemaDrift is outside OK (checkSchema says why).
type IntegrityResult struct {
	OK                            bool     `json:"ok"`
	IntegrityCheck                []string `json:"integrity_check"`
	ForeignKeys                   int      `json:"foreign_key_violations"`
	OrphanEntities                []int64  `json:"entities_without_domain_row"`
	InvalidTypedLinks             []int64  `json:"invalid_typed_links"`
	InvalidMeasurementCorrections []int64  `json:"invalid_measurement_corrections"`
	InvalidMeasurementChains      []int64  `json:"invalid_measurement_chains"`
	InvalidJournalNames           []int64  `json:"invalid_journal_names"`
	InvalidLinkDayRules           []int64  `json:"invalid_link_day_rules"`
	InvalidScaleValues            []int64  `json:"invalid_scale_values"`
	NameKeyMismatches             []int64  `json:"name_key_mismatches"`
	LinkDrift                     []int64  `json:"link_drift"`
	SchemaDrift                   []string `json:"schema_drift"`
	FullTextIndexOK               bool     `json:"full_text_index_ok"`
	FullTextError                 string   `json:"full_text_error,omitempty"`
}

// integrityDeadline bounds one run of Integrity. The full-text step holds the writer connection until it ends
// (docs/contract/integrity-checks.md), so a run that never ends would block every write. A run that passes the
// deadline ends with an error and no result. It is below the deadline of a request of the API (two minutes), so the
// error of a run that is too slow names the check, and the CLI and the snapshot's restore check are bound too.
const integrityDeadline = 90 * time.Second

// idCheck is one semantic check: a query that selects the id of every row that breaks a rule, and the list of the
// result it fills. An empty list is a passed check.
type idCheck struct {
	name  string
	query string
	dst   *[]int64
}

// idChecks are the semantic checks, in the order the result lists them. The page docs/contract/integrity-checks.md is
// the home of every query: this text is a copy of it, and TestIntegrityQueriesAreThoseOfTheContract keeps the two
// equal. The queries are read-only and never trust a trigger of the file they check.
func (r *IntegrityResult) idChecks() []idCheck {
	return []idCheck{
		{"orphan entities", `SELECT id FROM entities e WHERE NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id=e.id AND n.name_key=e.preferred_name_key)
 OR (e.entity_type='person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id=e.id))
 OR (e.entity_type='metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id=e.id))
 OR (e.entity_type='file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id=e.id))`, &r.OrphanEntities},
		{"typed link endpoints", `SELECT l.id FROM links l
 LEFT JOIN link_kinds k ON k.kind=l.kind
 LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id
 WHERE k.kind IS NULL OR f.id IS NULL OR t.id IS NULL
 OR (k.from_types IS NOT NULL AND instr(',' || k.from_types || ',', ',' || f.entity_type || ',')=0)
 OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0)
 ORDER BY l.id`, &r.InvalidTypedLinks},
		{"measurement corrections", `SELECT m.id FROM measurements m LEFT JOIN measurements p ON p.id=m.supersedes_id WHERE m.supersedes_id IS NOT NULL AND (p.id IS NULL OR p.metric_id IS NOT m.metric_id) ORDER BY m.id`, &r.InvalidMeasurementCorrections},
		{"measurement correction chains", `WITH RECURSIVE rooted(id) AS (
 SELECT id FROM measurements WHERE supersedes_id IS NULL
 UNION
 SELECT m.id FROM measurements m JOIN rooted r ON m.supersedes_id=r.id
)
SELECT id FROM measurements EXCEPT SELECT id FROM rooted ORDER BY id`, &r.InvalidMeasurementChains},
		{"journal name ownership", `SELECT n.id FROM entity_names n JOIN entities e ON e.id=n.entity_id
 WHERE (length(n.name_key)=10 AND date(n.name_key) IS n.name_key
   AND (e.entity_type<>'page' OR e.preferred_name_key IS NOT n.name_key OR e.day IS NOT n.name_key OR n.title IS NOT n.name_key))
 OR (e.is_journal
   AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key))
 OR (e.period IS NOT NULL AND n.name_key IS NOT e.preferred_name_key)
 OR ((n.name_key GLOB '[0-9][0-9][0-9][0-9]' OR n.name_key GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'
       OR n.name_key GLOB '[0-9][0-9][0-9][0-9]-q[0-9]' OR n.name_key GLOB '[0-9][0-9][0-9][0-9]-w[0-9][0-9]')
   AND (e.preferred_name_key IS NOT n.name_key OR n.title IS NOT upper(n.name_key))) ORDER BY n.id`, &r.InvalidJournalNames},
		{"link day-page rules", `SELECT l.id FROM links l LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id
 WHERE (l.kind='part-of' AND coalesce(t.is_journal,0))
 OR (l.kind='part-of' AND coalesce(f.is_journal,0))
 OR (l.kind='part-of' AND (t.period IS NOT NULL OR f.period IS NOT NULL)) ORDER BY l.id`, &r.InvalidLinkDayRules},
		{"scale ranges", `SELECT m.id FROM measurements m JOIN metrics x ON x.id=m.metric_id
 WHERE m.value IS NOT NULL AND x.unit GLOB '[0-9]*-[0-9]*' AND x.unit NOT GLOB '*[^0-9-]*' AND x.unit NOT GLOB '*-*-*'
 AND CAST(substr(x.unit, 1, instr(x.unit, '-') - 1) AS REAL) < CAST(substr(x.unit, instr(x.unit, '-') + 1) AS REAL)
 AND (m.value <> round(m.value)
   OR m.value NOT BETWEEN CAST(substr(x.unit, 1, instr(x.unit, '-') - 1) AS REAL) AND CAST(substr(x.unit, instr(x.unit, '-') + 1) AS REAL))
 ORDER BY m.id`, &r.InvalidScaleValues},
	}
}

// Integrity runs the checks: SQLite's own two, the semantic checks of idChecks, the name keys, and the full-text
// index's. All of them but the last read in one transaction, so the result is one view of the file while a writer
// goes on. The full-text check writes nothing but needs the writer connection: it runs after the others and holds
// the connection until it ends. The whole run is under integrityDeadline.
func (s *Store) Integrity(ctx context.Context) (*IntegrityResult, error) {
	r := &IntegrityResult{IntegrityCheck: []string{}}
	ctx, cancel := context.WithTimeout(ctx, integrityDeadline)
	defer cancel()
	tx, err := s.DB.R.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("integrity: %w", err)
	}
	defer tx.Rollback() // a read transaction has nothing to keep: Commit below ends it, and a failure ends it here
	var line string
	if err := integrityRead(ctx, tx, "integrity_check", `PRAGMA integrity_check`, []any{&line}, func() {
		r.IntegrityCheck = append(r.IntegrityCheck, line)
	}); err != nil {
		return nil, err
	}
	var table, parent string
	var rowID sql.NullInt64 // WITHOUT ROWID tables report NULL.
	var foreignKey int64
	if err := integrityRead(ctx, tx, "foreign_key_check", `PRAGMA foreign_key_check`, []any{&table, &rowID, &parent, &foreignKey}, func() {
		r.ForeignKeys++
	}); err != nil {
		return nil, err
	}
	semanticOK := true
	for _, c := range r.idChecks() {
		*c.dst = []int64{}
		var id int64
		if err := integrityRead(ctx, tx, c.name, c.query, []any{&id}, func() { *c.dst = append(*c.dst, id) }); err != nil {
			return nil, err
		}
		semanticOK = semanticOK && len(*c.dst) == 0
	}
	if err := r.checkNameKeys(ctx, tx); err != nil {
		return nil, err
	}
	semanticOK = semanticOK && len(r.NameKeyMismatches) == 0
	if r.LinkDrift, err = driftedLinkPages(ctx, tx); err != nil {
		return nil, err
	}
	semanticOK = semanticOK && len(r.LinkDrift) == 0
	if err := r.checkSchema(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("integrity: %w", err)
	}
	// the FTS check is an INSERT that writes nothing, so it runs on the writing connection
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		// Only a corruption result is a finding about the index. A deadline, a cancelled context or a busy file says
		// nothing about it: that is an error, so a restore check does not fail for a reason that is not damage.
		var se *sqlite.Error
		if ctx.Err() != nil || !errors.As(err, &se) || se.Code()&0xff != sqlite3.SQLITE_CORRUPT {
			return nil, fmt.Errorf("integrity: full-text check: %w", err)
		}
		r.FullTextError = fmt.Sprint(err)
	} else {
		r.FullTextIndexOK = true
	}
	r.OK = len(r.IntegrityCheck) == 1 && r.IntegrityCheck[0] == "ok" && r.ForeignKeys == 0 && semanticOK && r.FullTextIndexOK
	return r, nil
}

// checkNameKeys compares every entity_names.name_key with text.TitleKey of its title. SQLite cannot compute the key,
// so Go does (docs/contract/integrity-checks.md). The list holds the ids of entity_names.
func (r *IntegrityResult) checkNameKeys(ctx context.Context, tx *sql.Tx) error {
	r.NameKeyMismatches = []int64{}
	var id int64
	var title, key string
	return integrityRead(ctx, tx, "name keys", `SELECT id, title, name_key FROM entity_names ORDER BY id`, []any{&id, &title, &key}, func() {
		if text.TitleKey(title) != key {
			r.NameKeyMismatches = append(r.NameKeyMismatches, id)
		}
	})
}

// integrityRead reads every row of one check into dst and runs row, as each does. Its error names the check, so an
// incomplete check is not taken for a completed diagnostic result.
func integrityRead(ctx context.Context, tx *sql.Tx, check, query string, dst []any, row func()) error {
	rows, err := tx.QueryContext(ctx, query)
	if err := each(rows, err, dst, row); err != nil {
		return fmt.Errorf("%s: %w", check, err)
	}
	return nil
}
