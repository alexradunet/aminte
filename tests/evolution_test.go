package tests

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// evolution: evolution after the freeze (D13, D17, architecture/non-goals) — every CHECK is named and droppable by
// name; an unnamed one is not, a looser second CHECK does not relax the first, ADD CONSTRAINT checks existing rows;
// enums widen on a populated database; the partial-date and tokenizer paths of architecture/non-goals work; a
// link kind widens by migration; a promotion refuses invalid retained links; an entity uid is additive (D3); a comment outside
// a statement is not stored.
func evolution(s *S) {
	evolutionReaderCompatibility(s)
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }
	populated := func() *C {
		c := s.fresh()
		for _, t := range []string{"page", "person", "metric"} {
			c.thing(t)
		}
		c.page("Some page")
		return c
	}

	// ---- every CHECK named, every name droppable
	body := code(s.ddl)
	nchk := len(regexp.MustCompile(`\bCHECK\s*\(`).FindAllString(body, -1))
	var names []string
	for _, m := range regexp.MustCompile(`\bCONSTRAINT\s+(\w+)\s+CHECK\s*\(`).FindAllStringSubmatch(body, -1) {
		names = append(names, m[1])
	}
	s.K("every CHECK in schema is named", nchk > 0 && nchk == len(names), nchk, len(names))
	seen, dup := map[string]bool{}, []string{}
	for _, n := range names {
		if seen[n] {
			dup = append(dup, n)
		}
		seen[n] = true
	}
	s.K("constraint names are unique", len(dup) == 0, dup)
	var tables []string
	for _, m := range regexp.MustCompile(`CREATE TABLE (\w+)`).FindAllStringSubmatch(body, -1) {
		tables = append(tables, m[1])
	}
	var badName []string
	for _, n := range names {
		ok := false
		for _, t := range tables {
			ok = ok || strings.HasPrefix(n, t+"_")
		}
		if !ok {
			badName = append(badName, n)
		}
	}
	s.K("names are <table>_<column or rule>", len(badName) == 0, badName)
	type nt struct{ name, table string }
	var tblOf []nt
	for _, m := range regexp.MustCompile(`(?s)CREATE TABLE (\w+) \((.*?)\n\) STRICT;`).FindAllStringSubmatch(body, -1) {
		for _, n := range regexp.MustCompile(`\bCONSTRAINT\s+(\w+)\s+CHECK`).FindAllStringSubmatch(m[2], -1) {
			tblOf = append(tblOf, nt{n[1], m[1]})
		}
	}
	c := populated()
	var bad []string
	for _, x := range tblOf {
		c.must("BEGIN")
		if c.tryx("ALTER TABLE "+x.table+" DROP CONSTRAINT "+x.name) != "OK" {
			bad = append(bad, x.name)
		}
		c.must("ROLLBACK")
	}
	s.K("every named CHECK can be dropped by name on a populated database", len(tblOf) > 0 && len(bad) == 0, bad)
	s.K("integrity clean after the drops were rolled back", c.integrityOK())

	// ---- what a name buys (SQLite >= 3.53)
	t := s.connect("")
	t.must("CREATE TABLE u (x INTEGER CHECK (x > 0)) STRICT")
	s.K("an unnamed CHECK cannot be dropped", strings.Contains(t.tryx("ALTER TABLE u DROP CONSTRAINT x"), "no such constraint"))
	t.must("CREATE TABLE v (x INTEGER CONSTRAINT v_x CHECK (x > 0)) STRICT")
	t.must("ALTER TABLE v ADD CONSTRAINT v_x2 CHECK (x > -10)")
	s.K("adding a looser second CHECK does not relax the first (both apply)", err(t.tryx("INSERT INTO v VALUES (-5)")))
	t.must("INSERT INTO v VALUES (5)")
	s.K("ADD CONSTRAINT checks the existing rows (tightening is as safe as loosening)", err(t.tryx("ALTER TABLE v ADD CONSTRAINT v_x3 CHECK (x > 10)")))

	// ---- a derived column and its index leave the file in two steps. Nothing but the index names the column
	t = s.connect("")
	t.must("CREATE TABLE g (id INTEGER PRIMARY KEY, body TEXT NOT NULL, flag INTEGER NOT NULL GENERATED ALWAYS AS (instr(body, '[ ]') > 0) VIRTUAL) STRICT")
	t.must("CREATE INDEX g_flag ON g(id) WHERE flag")
	t.must("INSERT INTO g(body) VALUES ('- [ ] one'), ('prose'), ('- [ ] two')")
	whileIndexed := t.tryx("ALTER TABLE g DROP COLUMN flag")
	s.K("a generated column named by a partial index cannot be dropped", err(whileIndexed) && t.n("SELECT count(*) FROM g WHERE flag") == 2, whileIndexed)
	t.must("DROP INDEX g_flag")
	afterIndex := t.tryx("ALTER TABLE g DROP COLUMN flag")
	gone := t.tryx("SELECT flag FROM g")
	s.K("a VIRTUAL generated column is dropped once nothing names it, and the rows stay",
		afterIndex == "OK" && t.n("SELECT count(*) FROM g") == 3 && err(gone), afterIndex, gone)

	// ---- widening enums on a populated database
	use := func(c *C) string {
		_, e := c.tryIdentity("vehicle", "Vehicle witness", nil)
		if e != nil {
			return "ERR " + e.Error()
		}
		return "OK"
	}
	c = populated()
	s.K("entities_entity_type: the new value is refused before", err(use(c)))
	c.must("BEGIN")
	r1 := c.tryx("ALTER TABLE entities DROP CONSTRAINT entities_entity_type")
	r2 := c.tryx("ALTER TABLE entities ADD CONSTRAINT entities_entity_type CHECK (entity_type IN ('page','person','metric','file','vehicle'))")
	c.must("COMMIT")
	s.K("entities_entity_type: DROP and ADD the widened CHECK in one transaction", r1 == "OK" && r2 == "OK", r1, r2)
	s.K("entities_entity_type: the new value is accepted", use(c) == "OK")
	s.K("entities_entity_type: integrity and foreign keys clean", c.integrityOK())

	// ---- architecture/non-goals partial dates: DROP + ADD of people_birth_day
	c = s.fresh()
	p1 := c.named("person", "Ada", M{"birth_day": "1815-12-10"})
	p2 := c.named("person", "Ancestor")
	s.K("birth_day refuses 1870 and 1870-05 today", err(c.tryx("UPDATE people SET birth_day='1870' WHERE id=?", p2)) && err(c.tryx("UPDATE people SET birth_day='1870-05' WHERE id=?", p2)))
	c.must("BEGIN")
	r1 = c.tryx("ALTER TABLE people DROP CONSTRAINT people_birth_day")
	r2 = c.tryx("ALTER TABLE people ADD CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR date(birth_day) IS birth_day OR birth_day GLOB '[0-9][0-9][0-9][0-9]' OR (birth_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]' AND substr(birth_day, 6, 2) BETWEEN '01' AND '12'))")
	c.must("COMMIT")
	s.K("the named CHECK is dropped and re-added looser on a populated STRICT table", r1 == "OK" && r2 == "OK", r1, r2)
	s.K("YYYY and YYYY-MM are stored; full dates are untouched", c.tryx("UPDATE people SET birth_day='1870' WHERE id=?", p2) == "OK" &&
		c.tryx("UPDATE people SET birth_day='1870-05' WHERE id=?", p2) == "OK" && c.str("select birth_day from people where id=?", p1) == "1815-12-10")
	junk := true
	for _, v := range []string{"abc", "1870-13", "1870-5", "18700", "1870-05-01x"} {
		junk = junk && err(c.tryx("UPDATE people SET birth_day=? WHERE id=?", v, p2))
	}
	s.K("junk and month 13 are still refused", junk)
	s.K("integrity clean and the touch trigger still works", c.integrityOK() && c.n("select updated_at >= created_at from entities where id=?", p2) == 1)

	// ---- architecture/non-goals CJK search: the tokenizer switch is one transaction on a derived index
	c = s.fresh()
	jp := "日本語のノートを書く"
	for i, b := range []string{jp, "Zürich café notes", "plain english text"} {
		c.pageW(fmt.Sprintf("Text %d", i), nil, b)
	}
	hits := func(q string) int64 { return c.n("select count(*) from entities_fts where entities_fts match ?", q) }
	s.K("before: unicode61 finds the whole CJK run and accented words, not a part of the run", hits(jp) == 1 && hits("zurich") == 1 && hits("本語") == 0 && hits("ノート") == 0)
	c.must("BEGIN IMMEDIATE")
	c.must("DROP TABLE entities_fts")
	c.must("CREATE VIRTUAL TABLE entities_fts USING fts5(preferred, all_names, body, content='entity_search_content', content_rowid='id', tokenize='trigram remove_diacritics 1')")
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	c.must("COMMIT")
	s.K("after: trigram finds 3+-character parts and still folds accents", hits("本語の") == 1 && hits("ノート") == 1 && hits("日本語") == 1 && hits("zurich") == 1)
	s.K("...but not a two-character word (the known limit)", hits("本語") == 0)
	m := c.pageW("New text", nil, "これは新しい記録です")
	n1 := hits("新しい")
	c.must("UPDATE entities SET body='全く別の内容' WHERE id=?", m)
	s.K("the sync triggers keep working after the switch", n1 == 1 && hits("新しい") == 0 && hits("別の内") == 1)
	s.K("...and the FTS integrity-check passes", c.tryx("INSERT INTO entities_fts(entities_fts, rank) VALUES('integrity-check', 1)") == "OK")

	// ---- a link kind is widened by a migration (D8): drop the guard, update the row, recreate the guard
	c = populated()
	guard := c.str("select sql from sqlite_schema where name='link_kinds_structure_fixed'")
	c.must("BEGIN IMMEDIATE")
	c.must("DROP TRIGGER link_kinds_structure_fixed")
	c.must("UPDATE link_kinds SET to_types = 'page,person' WHERE kind = 'part-of'")
	c.must(guard)
	c.must("COMMIT")
	s.K("after the migration a part-of link may end at a person", c.link(c.thing("page"), c.named("person", ""), "part-of") == "OK")
	s.K("...and the guard is back: the next change is refused", strings.Contains(c.tryx("UPDATE link_kinds SET to_types = NULL WHERE kind = 'part-of'"), "fixed at registration"))

	// ---- type changes preserve the registry's endpoint rules. Only a plain page changes its type
	// (entities_promote_from_page), so the incoming edge ends at a plain page: the category of a part-of link.
	c = s.fresh()
	w := c.page("Lakeside")
	lakes := c.page("Lakes")
	s.K("the page [[Lakeside]] is filed in the category [[Lakes]]", c.link(w, lakes, "part-of") == "OK")
	// The outgoing edge needs a kind that restricts from_types: the shipped registry has none (a wikilink leaves any
	// type, a part-of leaves any entity), so the probe registers one, as a writer may (D8).
	c.must("INSERT INTO link_kinds(kind, from_types, to_types) VALUES ('authored', 'page', NULL)")
	author := c.page("Author")
	s.K("the page [[Author]] authored [[Lakeside]] by a registered kind that leaves a page only", c.link(author, w, "authored") == "OK")
	s.K("type change refuses a retained outgoing typed edge", strings.Contains(c.tryx("UPDATE entities SET entity_type='person' WHERE id=?", author), "retained link endpoint") && c.str("SELECT entity_type FROM entities WHERE id=?", author) == "page")
	s.K("type change refuses a retained incoming typed edge", strings.Contains(c.tryx("UPDATE entities SET entity_type='person' WHERE id=?", lakes), "retained link endpoint") && c.str("SELECT entity_type FROM entities WHERE id=?", lakes) == "page" && c.n("SELECT count(*) FROM links WHERE to_id=? AND kind='part-of'", lakes) == 1)

	// ---- an entity uid is additive after the freeze (D3): add, backfill, unique index, then NOT NULL
	c = populated()
	c.must("BEGIN IMMEDIATE")
	c.must("ALTER TABLE entities ADD COLUMN uid TEXT CONSTRAINT entities_uid CHECK (uid IS NULL OR length(uid) = 36)")
	for _, id := range c.col("select id from entities") {
		c.must("UPDATE entities SET uid = ? WHERE id = ?", uuid4(), id)
	}
	c.must("CREATE UNIQUE INDEX entities_uid_unique ON entities(uid)")
	c.must("ALTER TABLE entities ALTER COLUMN uid SET NOT NULL")
	c.must("COMMIT")
	s.K("every existing entity has a unique uid, and an entity without one is refused afterwards",
		c.n("select count(distinct uid) = count(*) from entities") == 1 && func() bool {
			_, e := c.tryIdentity("page", "Missing uid witness", nil)
			return e != nil && strings.Contains(e.Error(), "entities.uid")
		}())
	s.K("...with integrity and foreign keys clean", c.integrityOK())

	// ---- comments: inside a statement kept, outside dropped (why the rules live inside)
	t = s.connect("")
	t.must("-- outside comment\nCREATE TABLE k (\n  -- inside comment\n  x INTEGER\n) STRICT;")
	all := strings.Join(t.col("select coalesce(sql, '') from sqlite_schema"), " ")
	s.K("a comment inside a CREATE statement is stored in the file, one outside is not", strings.Contains(t.str("select sql from sqlite_schema where name='k'"), "inside comment") && !strings.Contains(all, "outside comment"))
}

// uuid4 is a random version-4 UUID: 36 characters.
func uuid4() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// These isolated SQLite probes distinguish preserving rows from preserving old SQL.
// They do not migrate any Aminte database or define a pre-freeze migration path.
func evolutionReaderCompatibility(s *S) {
	c := s.connect(filepath.Join(s.dir, "compatibility.db"))
	c.must("PRAGMA journal_mode=WAL")
	c.must("PRAGMA foreign_keys=ON")
	c.must("PRAGMA recursive_triggers=ON")
	c.must("PRAGMA synchronous=FULL")
	c.must("PRAGMA trusted_schema=OFF")
	c.must("CREATE TABLE compatibility_probe (old_name TEXT NOT NULL) STRICT")
	c.must("INSERT INTO compatibility_probe VALUES ('retained')")
	c.must("ALTER TABLE compatibility_probe ADD COLUMN added INTEGER")
	s.K("adding a column changes SELECT star but preserves explicit projections",
		c.tab("SELECT * FROM compatibility_probe") == "retained|None" &&
			c.tab("SELECT old_name FROM compatibility_probe") == "retained")
	s.K("adding a column refuses the former positional insert",
		strings.HasPrefix(c.tryx("INSERT INTO compatibility_probe VALUES ('positional')"), "ERR") &&
			c.n("SELECT count(*) FROM compatibility_probe") == 1)
	c.must("INSERT INTO compatibility_probe(old_name) VALUES ('explicit')")
	explicitRows := c.tab("SELECT old_name, added FROM compatibility_probe ORDER BY old_name")
	s.K("explicit column insert survives an additive nullable column",
		explicitRows == "explicit|None; retained|None", explicitRows)
	c.must("ALTER TABLE compatibility_probe RENAME COLUMN old_name TO new_name")
	oldQuery := c.tryx("SELECT old_name FROM compatibility_probe")
	renamedRows := c.tab("SELECT new_name FROM compatibility_probe ORDER BY new_name")
	s.K("column rename preserves data but refuses the former projection",
		strings.HasPrefix(oldQuery, "ERR") && renamedRows == "explicit; retained", oldQuery, renamedRows)
}
