package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aminte/internal/db"
)

// schemaObjectsQuery selects the objects of the schema that the DDL names. Two kinds of rows are left out on purpose.
// The statistics tables (sqlite_stat1 and its kin) are made by PRAGMA optimize, never by the DDL. The shadow tables
// of a virtual table, and their indexes, belong to the module that owns the virtual table (FTS5): another SQLite
// version can write another shape for them, and only the CREATE VIRTUAL TABLE row is the writer's.
const schemaObjectsQuery = `SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_schema
 WHERE name NOT LIKE 'sqlite\_stat%' ESCAPE '\'
 AND tbl_name NOT IN (SELECT name FROM pragma_table_list WHERE schema = 'main' AND type = 'shadow')`

// queryer is what both a read transaction and a pool can do.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// schemaObjects reads the objects of the schema of q: the key is the type and the name, the value the table name and
// the text of the DDL with its white space folded to single spaces. A comment stays in the text.
func schemaObjects(ctx context.Context, q queryer) (map[string]string, error) {
	objects := map[string]string{}
	var typ, name, table, ddl string
	rows, err := q.QueryContext(ctx, schemaObjectsQuery)
	if err := each(rows, err, []any{&typ, &name, &table, &ddl}, func() {
		objects[typ+" "+name] = table + " " + strings.Join(strings.Fields(ddl), " ")
	}); err != nil {
		return nil, err
	}
	return objects, nil
}

// embeddedSchemaObjects is schemaObjects of a file that the embedded DDL builds in a temporary folder. The file lives
// only for this call.
func embeddedSchemaObjects(ctx context.Context) (objects map[string]string, err error) {
	dir, err := os.MkdirTemp("", "aminte-schema-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	path := filepath.Join(dir, "life.db")
	if err := db.Init(ctx, path); err != nil {
		return nil, err
	}
	d, err := db.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, d.Close()) }() // Close before RemoveAll: a defer runs last first
	return schemaObjects(ctx, d.R)
}

// checkSchema fills SchemaDrift: the objects of the schema that the live file lacks (missing), has beyond the DDL
// (extra) or holds with another text (changed), each as "change type name". The lists are sorted by type and name, in
// that order of change. The check is not part of ok: a drift says that this binary and this file disagree, which the
// owner reads, and it is no damage to the data. A snapshot of a drifted file is as good a copy as the file is.
func (r *IntegrityResult) checkSchema(ctx context.Context, live queryer) error {
	r.SchemaDrift = []string{}
	want, err := embeddedSchemaObjects(ctx)
	if err != nil {
		return fmt.Errorf("schema drift: the embedded schema: %w", err)
	}
	have, err := schemaObjects(ctx, live)
	if err != nil {
		return fmt.Errorf("schema drift: %w", err)
	}
	var missing, extra, changed []string
	for key, w := range want {
		if h, ok := have[key]; !ok {
			missing = append(missing, key)
		} else if h != w {
			changed = append(changed, key)
		}
	}
	for key := range have {
		if _, ok := want[key]; !ok {
			extra = append(extra, key)
		}
	}
	for _, group := range []struct {
		word string
		keys []string
	}{{"missing", missing}, {"extra", extra}, {"changed", changed}} {
		sort.Strings(group.keys)
		for _, key := range group.keys {
			r.SchemaDrift = append(r.SchemaDrift, group.word+" "+key)
		}
	}
	return nil
}
