package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// imports: the import path of contract/imports, run from the page's own SQL on 1 000 CSV rows: idempotent,
// all-or-nothing, and the three traps (WHERE true, the partial index's WHERE, CAST) are real.
func imports(s *S) {
	sec := s.d.Page("contract/imports.md")
	var blocks []string
	for _, b := range sqlBlocks(sec) {
		if strings.Contains(b, "ATTACH 'scratch.db' AS s;") {
			blocks = append(blocks, b)
		}
	}
	s.K("contract/imports has the import block", len(blocks) == 1)
	if len(blocks) == 0 {
		return
	}
	imp := blocks[0]
	m := regexp.MustCompile(`'(import:[a-z0-9_:.-]+)'`).FindStringSubmatch(imp)
	s.K("the import block names its importer as an import:<name> source", m != nil)
	src := "import:scale"
	if m != nil {
		src = m[1]
	}
	work := filepath.Join(s.dir, "import")
	os.MkdirAll(work, 0o755)
	scratch := filepath.Join(work, "scratch.db")

	// stage is what `sqlite3 scratch.db ".import --csv weights.csv staging"` makes of a CSV with a header: a table
	// named by the header whose every cell is TEXT, an empty cell an empty string (not NULL).
	stage := func(rows [][5]string) {
		os.Remove(scratch)
		w := s.connect(scratch)
		w.must(`CREATE TABLE staging("id" TEXT, "day" TEXT, "taken_at" TEXT, "tz" TEXT, "value" TEXT)`)
		w.must("BEGIN")
		for _, r := range rows {
			w.must("INSERT INTO staging VALUES (?,?,?,?,?)", r[0], r[1], r[2], r[3], r[4])
		}
		w.must("COMMIT")
		w.Close()
	}
	run := func(c *C, q string) string {
		q = strings.ReplaceAll(q, "'scratch.db'", "'"+filepath.ToSlash(scratch)+"'")
		if r := c.tryx(q); r != "OK" {
			c.tryx("ROLLBACK")
			c.tryx("DETACH s")
			return r
		}
		return "OK"
	}
	n := func(c *C) int64 { return c.n("select count(*) from measurements where source=?", src) }
	var rows [][5]string
	for i := range 1000 {
		day := fmt.Sprintf("2025-%02d-%02d", 1+i%12, 1+i%28)
		at, tz := day+"T07:30:00.000Z", "Europe/Berlin"
		if i%5 == 0 {
			at, tz = "", ""
		}
		rows = append(rows, [5]string{fmt.Sprintf("w%d", i), day, at, tz, strconv.FormatFloat(60+float64(i%50)/10, 'f', -1, 64)})
	}
	c := s.freshWith(F{Path: filepath.Join(work, "life.db")})
	c.metric("weight", "kg")
	stage(rows)
	r := run(c, imp)
	s.K("the document's block loads 1 000 rows", r == "OK" && n(c) == 1000, r, n(c))
	s.K("empty CSV cells became NULL through NULLIF (200 rows without taken_at and tz)", c.n("select count(*) from measurements where source=? and taken_at is null and tz is null", src) == 200)
	s.K("running it again inserts nothing", run(c, imp) == "OK" && n(c) == 1000)
	s.K("the values are REAL, converted by the STRICT column", c.n("select count(*) from measurements where source=? and typeof(value)='real'", src) == 1000)
	more := append([][5]string{}, rows[:10]...)
	for i := range 10 {
		more = append(more, [5]string{fmt.Sprintf("new%d", i), "2026-01-02", "", "", "70.5"})
	}
	stage(more)
	s.K("10 duplicate keys and 10 new rows: exactly the 10 new ones are inserted", run(c, imp) == "OK" && n(c) == 1010, n(c))
	before := n(c)
	for _, x := range []struct {
		label string
		bad   [5]string
	}{
		{"a value 'abc'", [5]string{"bad", "2026-02-01", "", "", "abc"}},
		{"an empty value cell (not stored as 0.0)", [5]string{"empty", "2026-02-01", "", "", ""}},
		{"a malformed day (not swallowed by DO NOTHING)", [5]string{"day", "2026-2-1", "", "", "71"}},
	} {
		var batch [][5]string
		for i := range 5 {
			batch = append(batch, [5]string{fmt.Sprintf("b%d%s", i, x.label[:3]), "2026-02-01", "", "", "71"})
		}
		stage(append(batch, x.bad))
		r := run(c, imp)
		s.K(x.label+" in the batch: the whole batch is rejected, nothing of it inserted", strings.HasPrefix(r, "ERR") && n(c) == before, clip(r, 80), n(c))
	}
	stage([][5]string{{"t1", "2026-03-01", "", "", "70"}})
	s.K("without NULLIF an empty taken_at ('' is not NULL) fails its CHECK", strings.Contains(run(c, strings.ReplaceAll(imp, "NULLIF(taken_at, '')", "taken_at")), "CHECK"))
	s.K("without WHERE true the statement is rejected (the ON is read as a join's)", regexp.MustCompile(`JOIN|syntax`).MatchString(run(c, strings.ReplaceAll(imp, "  FROM s.staging WHERE true", "  FROM s.staging"))))
	s.K("a conflict target without the index's WHERE does not match the partial unique index", strings.Contains(run(c, strings.ReplaceAll(imp, " WHERE import_key IS NOT NULL DO NOTHING", " DO NOTHING")), "does not match"))
	s.K("CAST hides garbage: 'abc' -> 0.0, '' -> 0.0, '12.5kg' -> 12.5", c.tab("select cast('abc' as real), cast('' as real), cast('12.5kg' as real)") == "0|0|12.5")
	stage([][5]string{{"cast1", "2026-03-05", "", "", "abc"}})
	s.K("with CAST the bad value 'abc' would be stored as 0.0 (the trap, shown)", run(c, strings.ReplaceAll(imp, "NULLIF(tz, ''), value,", "NULLIF(tz, ''), CAST(value AS REAL),")) == "OK" &&
		c.tab("select value from measurements where import_key='cast1'") == "0")
	ib := sqlBlocks(s.d.Page("contract/integrity-checks.md"))
	orphan := ""
	if len(ib) > 0 {
		if ls := strings.Split(ib[0], "\n"); len(ls) > 2 {
			orphan, _, _ = strings.Cut(ls[2], ";")
		}
	}
	s.K("afterwards: integrity_check ok, foreign_key_check empty, no orphan entities row", c.integrityOK() && orphan != "" && c.tabOrErr(orphan) == "")
	pc := c.rows("SELECT source, count(*), min(day), max(day) FROM measurements GROUP BY source")
	counted := false
	for _, r := range pc {
		counted = counted || (r[0] == src && r[1].(int64) >= 1010)
	}
	s.K("the per-source count of contract/imports answers", counted, tab(pc))
	rp := filepath.Join(work, "ro.db")
	s.freshWith(F{Path: rp}).Close()
	ro := s.readOnly(rp)
	ro.must("PRAGMA trusted_schema = OFF") // step 1 as documented: a reader that does not trust the schema
	cp := filepath.Join(work, "copy.db")
	vac := ro.tryx("VACUUM INTO ?", filepath.ToSlash(cp))
	cc := s.connect(cp)
	s.K("contract/imports step 1: a read-only connection makes the copy (VACUUM INTO)",
		vac == "OK" && cc.n("select count(*) from aminte_meta") == ro.n("select count(*) from aminte_meta") && cc.n("select count(*) from aminte_meta") > 0, vac)
	s.K("contract/imports step 1: the documented command opens the file read-only, with trusted_schema=OFF", strings.Contains(s.d.Page("contract/imports.md"), `sqlite3 -readonly -cmd "PRAGMA trusted_schema=OFF" life.db "VACUUM INTO`))
}
