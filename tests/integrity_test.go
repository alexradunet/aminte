package tests

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// integrityFile owns a checkpointed synthetic baseline and the contract's literal checks.
// Semantic mutants use a small baseline; byte-corruption probes need multi-page b-trees.
type integrityFile struct {
	s          *S
	base       string
	statements []string
}

type integrityChecksResult struct{ ic, fk, orph string }

func newIntegrityFile(s *S, pageRows int) *integrityFile {
	blk := sqlBlocks(s.d.Page("contract/integrity-checks.md"))
	var sts []string
	if len(blk) > 0 {
		for _, st := range statements(blk[0]) {
			sts = append(sts, code(st))
		}
	}
	s.K("contract/integrity-checks has four groups: structure, foreign keys, domain/typed-edge semantics, FTS5",
		len(sts) == 10 && strings.HasPrefix(strings.ToLower(sts[0]), "pragma integrity_check") && strings.HasPrefix(strings.ToLower(sts[1]), "pragma foreign_key_check") &&
			strings.HasPrefix(strings.ToUpper(sts[2]), "SELECT ID FROM ENTITIES") && strings.HasPrefix(sts[3], "SELECT l.id FROM links") && strings.HasPrefix(sts[4], "SELECT m.id FROM measurements") && strings.Contains(sts[len(sts)-1], "'integrity-check', 1"), sts)
	if len(sts) != 10 {
		return nil
	}
	base := filepath.Join(s.dir, "base.db")
	c := s.freshWith(F{Path: base})
	c.must("BEGIN IMMEDIATE")
	for i := range pageRows { // physical probes need multi-page table and name-index b-trees
		if i%2 == 1 {
			c.pageW(fmt.Sprintf("Title %05d", i), "2026-09-30", fmt.Sprintf("BODYMARK%05d ", i)+strings.Repeat("lorem ipsum ", 20))
		} else {
			c.pageW(fmt.Sprintf("Note %05d", i), nil, fmt.Sprintf("BODYMARK%05d ", i)+strings.Repeat("dolor sit amet ", 20))
		}
	}
	c.thing("person") // one row of every domain type, so a query that forgets one table reports a false orphan
	c.thing("metric")
	c.thing("file")
	c.must("COMMIT")
	c.must("PRAGMA wal_checkpoint(TRUNCATE)")
	if err := c.Close(); err != nil {
		stop("close base writer: %v", err)
	}
	return &integrityFile{s: s, base: base, statements: sts}
}

// query opens a read-only connection for each live-file check, including damaged files.
func (f *integrityFile) query(path, query string) string {
	r := f.s.readOnly(path)
	rows, err := r.query(query)
	if closeErr := r.Close(); closeErr != nil {
		stop("close integrity reader: %v", closeErr)
	}
	if err != nil {
		return "Error: " + err.Error()
	}
	var out []string
	for _, row := range rows {
		out = append(out, strings.ReplaceAll(tab([][]any{row}), "; ", "\n"))
	}
	return strings.Join(out, "\n")
}

func (f *integrityFile) checks(path string) integrityChecksResult {
	ic, _, _ := strings.Cut(f.query(path, f.statements[0]), "\n")
	return integrityChecksResult{ic, f.query(path, f.statements[1]), f.query(path, strings.TrimSuffix(f.statements[2], ";"))}
}

func (f *integrityFile) copy(name string) string {
	path := filepath.Join(f.s.dir, name+".db")
	data, err := os.ReadFile(f.base)
	if err != nil {
		stop("read base: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		stop("copy: %v", err)
	}
	return path
}

// physicalIntegrity checks zeroed/truncated files and index/body byte damage.
// It is a baseline suite, separate from semantic-rule mutant repetitions.
func physicalIntegrity(s *S) {
	f := newIntegrityFile(s, 3000)
	if f == nil {
		return
	}
	r := f.checks(f.base)
	s.K("physical corruption baseline is structurally and semantically clean", r == integrityChecksResult{"ok", "", ""}, r)
	sizeConn := s.readOnly(f.base)
	ps := sizeConn.n("PRAGMA page_size")
	if err := sizeConn.Close(); err != nil {
		stop("close size reader: %v", err)
	}
	if ps < 512 {
		stop("invalid page size %d", ps)
	}

	flip := func(p string, at int) {
		b, err := os.ReadFile(p)
		if err != nil {
			stop("read corruption target: %v", err)
		}
		if at < 0 || at >= len(b) {
			stop("corruption offset %d outside %d bytes", at, len(b))
		}
		b[at] ^= 1
		if err := os.WriteFile(p, b, 0o644); err != nil {
			stop("write corruption target: %v", err)
		}
	}
	p := f.copy("zero")

	root, err := strconv.ParseInt(f.query(p, "SELECT rootpage FROM sqlite_schema WHERE name='entities' AND type='table'"), 10, 64)
	if err != nil || root < 1 {
		stop("entity table root: %d, %v", root, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		stop("read zero target: %v", err)
	}
	start, end := (root-1)*ps, root*ps
	if start < 0 || end > int64(len(b)) {
		stop("table page %d outside file", root)
	}
	clear(b[start:end])
	if err := os.WriteFile(p, b, 0o644); err != nil {
		stop("write zero page: %v", err)
	}
	s.K("a zeroed table page: integrity_check is not ok", f.checks(p).ic != "ok")

	p = f.copy("trunc")
	st, err := os.Stat(p)
	if err != nil {
		stop("stat truncation target: %v", err)
	}
	if st.Size() <= 3*ps {
		stop("truncation fixture too short")
	}
	if err := os.Truncate(p, st.Size()-3*ps); err != nil {
		stop("truncate fixture: %v", err)
	}
	s.K("a file truncated by three pages: integrity_check is not ok", f.checks(p).ic != "ok")

	p = f.copy("idx")
	reader := s.readOnly(p)
	indexName := reader.str(`SELECT il.name FROM pragma_index_list('entity_names') il WHERE il."unique"=1 AND (SELECT count(*) FROM pragma_index_info(il.name))=1 AND (SELECT name FROM pragma_index_info(il.name))='name_key'`)
	indexRoot := reader.n("SELECT rootpage FROM sqlite_schema WHERE name=? AND type='index'", indexName)
	if err := reader.Close(); err != nil {
		stop("close index reader: %v", err)
	}
	if indexRoot < 1 {
		stop("no live unique registry-key index")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		stop("read index target: %v", err)
	}
	// Follow the actual index b-tree, excluding tables and free/stale pages.
	visited := map[int64]bool{}
	var pages []int64
	var visit func(int64)
	visit = func(page int64) {
		if visited[page] {
			stop("cycle in index fixture at page %d", page)
		}
		visited[page] = true
		lo, hi := (page-1)*ps, page*ps
		if page < 1 || hi > int64(len(data)) {
			stop("index child page outside fixture: %d", page)
		}
		block := data[lo:hi]
		header := 0
		if page == 1 {
			header = 100
		}
		kind := block[header]
		if kind != 2 && kind != 10 {
			stop("non-index page %d type %d", page, kind)
		}
		pages = append(pages, page)
		if kind == 10 {
			return
		}
		count := int(binary.BigEndian.Uint16(block[header+3 : header+5]))
		if header+12+2*count > len(block) {
			stop("bad index pointer array")
		}
		visit(int64(binary.BigEndian.Uint32(block[header+8 : header+12])))
		for i := 0; i < count; i++ {
			pos := int(binary.BigEndian.Uint16(block[header+12+2*i : header+14+2*i]))
			if pos < 0 || pos+4 > len(block) {
				stop("bad index cell offset")
			}
			visit(int64(binary.BigEndian.Uint32(block[pos : pos+4])))
		}
	}
	visit(indexRoot)
	j := -1
	for _, page := range pages {
		lo := (page - 1) * ps
		block := data[lo : lo+ps]
		for _, m := range regexp.MustCompile(`title 012[0-9][0-9]`).FindAllIndex(block, -1) {
			candidate := f.copy("idx-candidate")
			flip(candidate, int(lo)+m[0]+6)
			if f.checks(candidate).ic != "ok" {
				p = candidate
				j = int(lo) + m[0]
				break
			}
		}
		if j >= 0 {
			break
		}
	}
	s.K("a flipped byte in a name_key inside the live unique registry index: integrity_check is not ok", j >= 0 && f.checks(p).ic != "ok", indexName, indexRoot, j)
	s.K("index-only damage preserves authoritative table names", j >= 0 && f.query(p, "SELECT count(*) FROM entity_names NOT INDEXED WHERE name_key LIKE 'title 012%'") == f.query(f.base, "SELECT count(*) FROM entity_names NOT INDEXED WHERE name_key LIKE 'title 012%'"))

	// without secure_delete a b-tree split leaves stale copies of cells in free space: flip copies until one is a live row
	j, n := -1, "0"
	baseData, err := os.ReadFile(f.base)
	if err != nil {
		stop("read body target: %v", err)
	}
	for _, m := range regexp.MustCompile(`lorem ipsum lorem`).FindAllIndex(baseData, -1) {
		p = f.copy("val")
		flip(p, m[0])
		n = f.query(p, "SELECT count(*) FROM entities WHERE body LIKE '%korem ipsum lorem%' OR body LIKE '%morem ipsum lorem%'")
		if n == "1" {
			j = m[0]
			break
		}
	}
	s.K("a flipped byte inside a body: the text changed and integrity_check is STILL ok", j >= 0 && n == "1" && f.checks(p).ic == "ok", j, n)
}

// integrity checks foreign keys, typed ownership/edges and derived FTS content
// independently on small fresh-file fixtures, including deliberate semantic damage.
func integrity(s *S) {
	f := newIntegrityFile(s, 1)
	if f == nil {
		return
	}
	sts := f.statements
	semanticIntegrity(s, sts)
	orphan := strings.TrimSuffix(sts[2], ";")
	r := f.checks(f.base)
	s.K("a clean file with a row of every type: integrity ok, foreign_key_check empty, orphan query empty", r == integrityChecksResult{"ok", "", ""}, r)
	p := f.copy("fk")
	w := s.connect(p)
	w.must("PRAGMA foreign_keys=OFF")
	ins := w.tryx("INSERT INTO measurements(metric_id,day,value,created_at,source) VALUES (99999,'2026-09-30',100,'2026-09-30T10:00:00.000Z','ui')")
	if err := w.Close(); err != nil {
		stop("close FK damage writer: %v", err)
	}
	r = f.checks(p)
	s.K("a reading of no metric, written with foreign_keys=OFF (STRICT does not enforce FKs): only foreign_key_check sees it", ins == "OK" && r.ic == "ok" && r.fk != "" && r.orph == "", ins, r)

	p = f.copy("named-plain")
	w = s.connect(p)
	w.must("PRAGMA foreign_keys=ON")
	plain := w.anyIdentity("page")
	if err := w.Close(); err != nil {
		stop("close plain: %v", err)
	}
	r = f.checks(p)
	s.K("a fully named plain entity has no missing domain row", r == integrityChecksResult{"ok", "", ""}, r, plain)

	p = f.copy("missing-name")
	w = s.connect(p)
	w.must("PRAGMA foreign_keys=OFF")
	oid := w.rows("INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES ('page','missing-owner'," + NOW + "," + NOW + ",'ui') RETURNING id")[0][0].(int64)
	if err := w.Close(); err != nil {
		stop("close name damage: %v", err)
	}
	r = f.checks(p)
	s.K("missing preferred ownership is detected by FK and semantic checks, not structure", r.ic == "ok" && r.fk != "" && r.orph == ids(oid), r)

	// The preferred key is a name that another entity owns. The name exists, so the orphan query must test who owns it
	// (n.entity_id=e.id), not only that some entity owns it.
	p = f.copy("name-of-another")
	w = s.connect(p)
	w.must("PRAGMA foreign_keys=OFF")
	owned := w.str("SELECT n.name_key FROM entity_names n JOIN entities o ON o.id=n.entity_id WHERE o.entity_type='page' ORDER BY n.entity_id LIMIT 1")
	claimant := w.rows("INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES ('page',?,"+NOW+","+NOW+",'ui') RETURNING id", owned)
	if len(claimant) != 1 {
		stop("the entity that prefers a name of another: %d rows", len(claimant))
	}
	tid, _ := claimant[0][0].(int64)
	if err := w.Close(); err != nil {
		stop("close preferred-owner damage: %v", err)
	}
	r = f.checks(p)
	s.K("a preferred key that another entity owns is an orphan: the orphan query asks who owns the name", r.ic == "ok" && r.fk != "" && r.orph == ids(tid) && tid != 0 && owned != "None", r, owned)
	for _, typ := range []string{"person", "metric", "file"} {
		p = f.copy("half-" + typ)
		w = s.connect(p)
		w.must("PRAGMA foreign_keys=ON")
		id := w.identity(typ, "ui", "Missing "+typ+" extension", nil, "")
		if err := w.Close(); err != nil {
			stop("close extension damage: %v", err)
		}
		r = f.checks(p)
		s.K("a "+typ+" with an owned name but no extension: only the orphan query sees it", r == integrityChecksResult{"ok", "", ids(id)}, r)
	}

	// Deliberate typed-edge damage: structural/FK/domain checks remain clean.
	typed := s.fresh()
	target := typed.page("Category")
	source := typed.metric("Weight", "kg")
	if result := typed.link(source, target, "part-of"); result != "OK" {
		stop("typed edge fixture: %s", result)
	}
	typed.must("DROP TRIGGER entities_endpoint_types")
	typed.must("UPDATE entities SET entity_type='person' WHERE id=?", target)
	typed.must("INSERT INTO people(id,name) VALUES (?,'P')", target) // a complete person, so only the edge is damaged
	s.K("typed-edge semantic query detects deliberate endpoint damage", typed.integrityOK() && typed.tab(orphan) == "" && typed.tab(sts[3]) != "")

	// ---- the fourth check: the FTS index against pages (it writes, so a writer connection)
	c := s.fresh()
	c.dayPage("2026-09-30", "alpha beta")
	c.page("Gamma")
	clean := true
	for _, st := range sts {
		clean = clean && c.tryx(st) == "OK"
	}
	s.K("on a clean file every statement of contract/integrity-checks runs without error", clean)
	c.must("INSERT INTO entities_fts(rowid, preferred, all_names, body) VALUES (999, 'ghost', 'ghost', 'drifted')")
	s.K("a drifted FTS index: PRAGMA integrity_check still says ok", c.tab("PRAGMA integrity_check") == "ok")
	s.K("...and the FTS5 integrity-check of contract/integrity-checks fails", strings.HasPrefix(c.tryx(sts[len(sts)-1]), "ERR"))
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	s.K("...until 'rebuild' repairs it", c.tryx(sts[len(sts)-1]) == "OK")

	// A connection keeps state of the virtual table between its statements: a reader that read the old index still
	// reports FTS5 corruption in integrity_check after the rebuild of the writer, and a new reader says ok.
	stalePath := filepath.Join(s.dir, "stale-reader.db")
	sw := s.freshWith(F{Path: stalePath})
	sw.page("Alpha")
	sw.must("INSERT INTO entities_fts(rowid, preferred, all_names, body) VALUES (999, 'ghost', 'ghost', 'drifted')")
	old := s.readOnly(stalePath)
	s.K("a reader checks the drifted file: integrity_check says ok", old.tab("PRAGMA integrity_check") == "ok")
	sw.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	s.K("after the rebuild, the reader that read the old index reports FTS5 corruption in integrity_check", strings.Contains(old.tab("PRAGMA integrity_check"), "fts5"))
	s.K("...and a new reader says ok", s.readOnly(stalePath).tab("PRAGMA integrity_check") == "ok")

	gone := c.page("Gone")
	c.must("PRAGMA foreign_keys=OFF")
	c.must("DROP TRIGGER entities_no_delete")
	c.must("DELETE FROM entities WHERE id=?", gone)
	s.K("an identity deleted past its guard leaves stale FTS content", strings.HasPrefix(c.tryx(sts[len(sts)-1]), "ERR"))
}

// These probes damage stored data with guards restored before the literal contract
// checks run. Schema-object presence alone cannot discover these failures.
func semanticIntegrity(s *S, checks []string) {
	c := s.fresh()
	other := c.metric("Other", "kg")
	c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at) VALUES(100,1,'2026-10-01',3,'ui'," + NOW + ")")
	c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(101,1,'2026-10-01',4,100,'ui'," + NOW + ")")
	s.K("correction integrity accepts a correction of the same metric", c.tab(checks[4]) == "")
	bypass(c, "measurements_supersede_metric", func() {
		c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(102,?,'2026-10-01',5,101,'ui',"+NOW+")", other)
	})
	s.K("correction integrity detects a correction of another metric", c.integrityOK() && c.tab(checks[4]) == "102")

	c = s.fresh()
	c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at) VALUES(200,1,'2026-10-01',3,'ui'," + NOW + ")")
	c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(3,1,'2026-10-01',NULL,200,'ui'," + NOW + ")")
	s.K("correction integrity accepts retractions and nonmonotonic ids", c.tab(checks[5]) == "" && c.n("SELECT count(*) FROM measurement_values") == 0)
	bypass(c, "measurements_no_update", func() { c.must("UPDATE measurements SET supersedes_id=3 WHERE id=200") })
	s.K("correction integrity detects a rootless cycle", c.integrityOK() && c.tab(checks[5]) == "3; 200")

	for _, journal := range []bool{false, true} {
		c = s.fresh()
		title, alias, key := "Ordinary", "2026-10-01", "2026-10-01"
		if journal {
			title, alias, key = "2026-10-01", "Extra", "extra"
		}
		var owner int64
		if journal {
			owner = c.dayPage(title, "")
		} else {
			owner = c.page(title)
		}
		s.K("journal integrity accepts canonical ownership and ordinary names", c.tab(checks[6]) == "")
		bypass(c, "entity_names_day_insert", func() { c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,?,?)", owner, alias, key) })
		witness := "journal integrity detects a date name on another owner"
		if journal {
			witness = "journal integrity detects extra journal aliases"
		}
		s.K(witness, c.integrityOK() && c.tab(checks[6]) == c.tab("SELECT id FROM entity_names WHERE name_key=?", key))
	}

	// A period page has only its own key (D30): the same query finds an alias on it, and a period key on another page.
	c = s.fresh()
	week, ordinary := c.page("2026-W43"), c.page("Ordinary two")
	s.K("period-name integrity accepts a period page and an ordinary page", c.tab(checks[6]) == "")
	bypass(c, "entity_names_day_insert", func() {
		c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,?,?)", week, "October week", "october week")
		c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,?,?)", ordinary, "2026-q1", "2026-q1")
	})
	s.K("period-name integrity detects an alias on a period page and a period key on another page, spelled in lower case",
		c.integrityOK() && c.tab(checks[6]) == c.tab("SELECT id FROM entity_names WHERE name_key IN ('october week','2026-q1') ORDER BY id"))

	// The rules that only a trigger holds (links_endpoint_types, measurements_scale_range) have a query each.
	c = s.fresh()
	start, day, category := c.page("Start"), c.dayPage("2026-10-02", ""), c.page("Category")
	link := func(id int64, from, to int64, kind string) {
		c.must("INSERT INTO links(id,from_id,to_id,kind,created_at,source) VALUES(?,?,?,?,"+NOW+",'ui')", id, from, to, kind)
	}
	link(501, start, category, "part-of")
	s.K("the link-rule query accepts a part-of link between plain pages", c.tab(checks[7]) == "")
	bypass(c, "links_endpoint_types", func() {
		link(511, start, day, "part-of")
		link(512, day, category, "part-of")
	})
	s.K("the link-rule query detects a part-of link to or from a day page, while the registry query sees nothing",
		c.integrityOK() && c.tab(checks[7]) == "511; 512" && c.tab(checks[3]) == "")

	c = s.fresh()
	scale := c.n("SELECT entity_id FROM entity_names WHERE name_key = 'mood'")
	weight := c.metric("Weight", "kg")
	reading := func(id, metric int64, value any) {
		c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(?,?,'2026-10-01',?,?,'ui',"+NOW+")", id, metric, value, nil)
	}
	reading(600, scale, 5)
	reading(601, weight, 99.5)
	s.K("the scale query accepts a whole number inside the range, and any value of a metric that is not a scale", c.tab(checks[8]) == "")
	bypass(c, "measurements_scale_range", func() {
		reading(610, scale, 6)
		reading(611, scale, 2.5)
		reading(612, scale, 0)
	})
	s.K("the scale query detects a value above the range, a fraction and a value below it", c.integrityOK() && c.tab(checks[8]) == "610; 611; 612")
}
