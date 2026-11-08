package tests

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// identity: identity, provenance and deletion (schema.sql, D3, D8, D11) — the supertype and its composite foreign
// keys, ids carried by RETURNING, `source` on every row, `import_key` on entities (a re-run inserts nothing,
// cookbook/import-a-row-once), no hard deletes, updated_at kept by triggers.
func identity(s *S) {
	editRevisions(s)
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }
	typeProbe := func(typ string) bool { _, e := cTypeProbe(s, typ); return e != nil }

	// ---- the supertype: a row's type and its table agree
	c := s.fresh()
	s.K("entities.entity_type rejects an unknown type", typeProbe("foo"))
	s.K("entities.entity_type has no event (D22)", typeProbe("event"))
	e := c.thing("page")
	s.K("entities.entity_type has no task (D23)", typeProbe("task"))
	s.K("a people row cannot take a plain page's id", err(c.tryx("INSERT INTO people(id,name) VALUES (?, 'p')", e)))
	s.K("a domain row needs its entities and pages rows", err(c.tryx("INSERT INTO people(id,name) VALUES (9999, 'x')")))
	s.K("a domain row cannot claim another type", err(c.tryx("INSERT INTO people(id,entity_type,name) VALUES (?, 'page', 'x')", e)))
	s.K("entities.entity_type rejects task independently of preferred ownership", typeProbe("task"))
	var odd []string
	for _, t := range c.col("select name from pragma_table_list where schema='main' and type='table' and name not like 'sqlite_%'") {
		if strings.Join(c.col("select type from pragma_table_info(?) where pk > 0 order by pk", t), ",") != "INTEGER" {
			odd = append(odd, t)
		}
	}
	s.K("the tables without a single INTEGER primary key are exactly the two registries", slices.Equal(slices.Sorted(slices.Values(odd)), []string{"aminte_meta", "link_kinds"}), odd)

	// ---- ids by RETURNING
	c = s.fresh()
	for i := range 10 { // entity ids run ahead of link ids
		c.page(fmt.Sprintf("Seed %d", i))
	}
	m := c.dayPage("2026-09-30", "")
	g := c.page("Target")
	c.link(m, g, "wikilink")
	s.K("last_insert_rowid() moves: after a link insert it is the link's id, not the page's", c.n("select last_insert_rowid()") != m)
	var uses []string
	for _, b := range s.d.CookbookBlocks() {
		if strings.Contains(b.sql, "last_insert_rowid") {
			uses = append(uses, b.key)
		}
	}
	s.K("no cookbook block uses last_insert_rowid()", len(uses) == 0, uses)
	c = s.fresh()
	for i := range 10 {
		c.page(fmt.Sprintf("Seed %d", i))
	}
	p := P{}
	sync := func(st string) { // the cookbook/save-a-body link sync, inside cookbook/capture's transaction
		if strings.HasPrefix(strings.ToUpper(st), "UPDATE ENTITIES SET BODY") {
			c.link(p["page_id"], c.page("Aminte"), "wikilink")
		}
	}
	_, e2 := c.runBlock(s.d.Block("capture"), p, sync)
	s.K("cookbook/capture run literally, with a link sync inside: the mood reading points at the day page the RETURNING gave",
		e2 == nil && c.tab("select captured_with_id from measurements") == val(p["page_id"]) && c.str("select title from entity_names where entity_id=?", p["page_id"]) == "2026-09-29", e2, p)

	// ---- source on every row
	c = s.fresh()
	for _, t := range []string{"entities", "links", "measurements"} {
		s.K(t+".source is TEXT NOT NULL with no default", c.tab("select type, \"notnull\", dflt_value from pragma_table_info(?) where name='source'", t) == "TEXT|1|None")
	}
	s.K("an entities row without a source is refused", func() bool { _, e := c.tryIdentity("page", "Missing source", M{"source": nil}); return e != nil }())

	sourceProbe := func(value any) error {
		_, e := c.tryIdentity("page", fmt.Sprintf("Source fixture %d", s.next()), M{"source": value})
		return e
	}
	for _, v := range []string{"ui", "cli", "api", "agent:claude", "import:bank_csv", "import:health-2026.v2"} {
		s.K(fmt.Sprintf("source %q accepted on entities and measurements", v), sourceProbe(v) == nil && c.measure(1, "2026-01-01", 3, M{"source": v}) == "OK")
	}
	for _, v := range []any{"", "UI", "agent claude", strings.Repeat("x", 65), "a/b", "manual ", nil} {
		s.K(fmt.Sprintf("source %.20v rejected on entities and measurements", v), sourceProbe(v) != nil && err(c.measure(1, "2026-01-01", 3, M{"source": v})))
	}

	s.K("links.source takes the same GLOB as entities.source", err(c.link(c.page("Source A"), c.page("Source B"), "part-of", "UI")))
	a, b := c.page("Source C"), c.page("Source D")
	s.K("entities.source cannot change", err(c.tryx("UPDATE entities SET source='cli' WHERE id=?", a)))
	s.K("a no-op full-row update and a tombstone still pass", c.tryx("UPDATE entities SET source=source, deleted_at="+NOW+" WHERE id=?", a) == "OK")
	c.link(a, b, "part-of", "agent:claude")
	s.K("links.source cannot change", err(c.tryx("UPDATE links SET source='ui' WHERE from_id=?", a)))
	s.K("a link note edit still passes", c.tryx("UPDATE links SET note='n', source=source WHERE from_id=?", a) == "OK")

	// Probe endpoints independently of provenance, with valid alternate pages.
	endpointConn := s.fresh()
	from, to, other := endpointConn.page("From"), endpointConn.page("To"), endpointConn.page("Other")
	endpointConn.link(from, to, "wikilink")
	s.K("a link's endpoints cannot change", err(endpointConn.tryx("UPDATE links SET from_id=? WHERE from_id=? AND to_id=?", other, from, to)) &&
		err(endpointConn.tryx("UPDATE links SET to_id=? WHERE from_id=? AND to_id=?", other, from, to)) &&
		endpointConn.tab("SELECT from_id,to_id FROM links") == ids(from)+"|"+ids(to))

	// ---- no hard deletes
	c = s.fresh()
	byType := map[string]int64{"page": c.thing("page"), "person": c.thing("person"), "metric": c.thing("metric"), "file": c.thing("file")}
	n0 := c.n("select count(*) from entities where source <> 'schema'")
	for _, x := range [][2]string{{"page", "entities"}, {"person", "people"}, {"metric", "metrics"}, {"file", "files"}} {
		s.K("DELETE FROM "+x[1]+" is refused", err(c.tryx("DELETE FROM "+x[1]+" WHERE id=?", byType[x[0]])))
		s.K("DELETE of the "+x[0]+" entities row is refused", err(c.tryx("DELETE FROM entities WHERE id=?", byType[x[0]])))
	}
	s.K("the page rows of named entities cannot be deleted either", err(c.tryx("DELETE FROM entity_names WHERE entity_id=?", byType["person"])))
	s.K("nothing was removed (four entities, one id each)", c.n("select count(*) from entities where source <> 'schema'") == n0 && n0 == 4)
	s.K("REPLACE INTO owned names is blocked under recursive_triggers=ON",
		err(c.tryx(fmt.Sprintf("REPLACE INTO entity_names(id,entity_id,title,name_key) SELECT id,entity_id,'Replaced','replaced' FROM entity_names WHERE entity_id=%d", byType["page"]))) &&
			c.str("select body from entities where id=?", byType["page"]) == "x")
	o := c.anyIdentity("page")
	s.K("an orphan entities row cannot be deleted either (only the entities trigger can stop it)", err(c.tryx("DELETE FROM entities WHERE id=?", o)))
	c.must("PRAGMA foreign_keys=OFF")
	s.K("with foreign_keys=OFF: DELETE FROM entities still refused (the trigger, not the FK)", err(c.tryx("DELETE FROM entities WHERE id=?", byType["person"])))
	s.K("with foreign_keys=OFF: DELETE FROM people still refused", err(c.tryx("DELETE FROM people WHERE id=?", byType["person"])))
	c.must("PRAGMA foreign_keys=ON")
	c.link(byType["person"], byType["page"], "part-of")
	s.K("links rows may be hard-deleted (the one such table)", c.tryx("DELETE FROM links WHERE from_id=?", byType["person"]) == "OK" && c.n("select count(*) from links") == 0)
	c.must("INSERT INTO link_kinds(kind) VALUES ('spare')")
	s.K("an unreferenced registry row may be deleted (registries are administrative)", c.tryx("DELETE FROM link_kinds WHERE kind='spare'") == "OK")
	c.link(byType["person"], byType["page"], "part-of")
	s.K("a referenced link kind is refused by its foreign key, not by a trigger", strings.Contains(strings.ToUpper(c.tryx("DELETE FROM link_kinds WHERE kind='part-of'")), "FOREIGN KEY"))

	// ---- updated_at, kept by triggers
	c = s.fresh()
	byType = map[string]int64{"page": c.thing("page"), "person": c.thing("person")}
	c.must("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z'")
	for _, x := range [][2]string{{"page", "UPDATE entities SET body='y' WHERE id=?"}, {"person", "UPDATE people SET birth_day='1990-01-01' WHERE id=?"}} {
		c.must(x[1], byType[x[0]])
		s.K("updating a "+x[0]+" bumps entities.updated_at", c.n("select updated_at > created_at from entities where id=?", byType[x[0]]) == 1)
	}

	// ---- entities.import_key: a re-run, a replay or a retry inserts nothing (cookbook/import-a-row-once)
	c = s.fresh()

	importProbe := func(source string, key any) error {
		_, e := c.tryIdentity("page", fmt.Sprintf("Import fixture %d", s.next()), M{"source": source, "import_key": key})
		return e
	}
	firstProbe, secondProbe := importProbe("import:x", "k1"), importProbe("import:x", "k1")
	s.K("a second entity with the same (source, import_key) is refused", firstProbe == nil && secondProbe != nil && strings.Contains(secondProbe.Error(), "UNIQUE"))
	c = s.fresh()
	var B []string
	for _, st := range statements(s.d.Block("import-a-row-once")) {
		if v := verb(st); v != "BEGIN" && v != "COMMIT" {
			B = append(B, st)
		}
	}
	if len(B) != 3 {
		stop("cookbook/import-a-row-once has %d statements besides BEGIN and COMMIT, not 3", len(B))
	}
	insEnt, insPage, upd := B[0], B[1], B[2]
	K := "notes/sourdough.md"
	runImport := func(key string) [][]any {
		c.must("BEGIN IMMEDIATE")
		got := c.rows(insEnt, P{"import_key": key})
		if len(got) > 0 {
			c.must(insPage, P{"page_id": got[0][0]})
		}
		c.must("COMMIT")
		return got
	}
	c.must("BEGIN IMMEDIATE")
	first, importErr := c.query(insEnt, P{"import_key": K})
	s.K("cookbook/import-a-row-once: the first insert returns an id", importErr == nil && len(first) == 1, importErr)
	if importErr == nil && len(first) == 1 {
		c.must(insPage, P{"page_id": first[0][0]})
		c.must("COMMIT")
		again := runImport(K)
		fid := first[0][0]
		body := func() string { return c.str("select body from entities where id=?", fid) }
		s.K("cookbook/import-a-row-once: the first run returns an id, the re-run returns none and adds no page",
			len(first) == 1 && len(again) == 0 && c.n("select count(*) from entity_names where name_key='sourdough'") == 1 && c.integrityOK())

		importProbe = func(source string, key any) error {
			_, e := c.tryIdentity("page", fmt.Sprintf("Import fixture %d", s.next()), M{"source": source, "import_key": key})
			return e
		}
		s.K("the same import_key under another source is another row (per-source namespace)", importProbe("import:health", K) == nil)
		firstUnkeyed, secondUnkeyed := importProbe("import:vault", nil), importProbe("import:vault", nil)
		s.K("rows without an import_key never collide", firstUnkeyed == nil && secondUnkeyed == nil)
		duplicate := importProbe("import:vault", K)
		s.K("a plain INSERT of a known key is refused (the index is unique)", duplicate != nil && strings.Contains(duplicate.Error(), "UNIQUE"))

		keyFixed := s.K("entities.import_key cannot change", strings.Contains(c.tryx("UPDATE entities SET import_key='notes/other.md' WHERE id=?", fid), "never changed"))
		keyNotCleared := s.K("...nor be cleared", strings.Contains(c.tryx("UPDATE entities SET import_key=NULL WHERE id=?", fid), "never changed"))
		if keyFixed && keyNotCleared {
			c.must(upd, P{"import_key": K})
			s.K("cookbook/import-a-row-once: a changed note is updated in its own page", body() == "Feed the starter the night before; 75% water.", body())
			c.must("UPDATE entities SET body='old' WHERE id=?", fid)
			c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", fid)
			c.must(upd, P{"import_key": K})
			again = runImport(K)
			s.K("cookbook/import-a-row-once: a tombstoned import is neither updated nor inserted again",
				body() == "old" && len(again) == 0 && c.n("select count(*) from entity_names where name_key='sourdough'") == 1)
		}
	}

	c = s.fresh()
	pg := c.thing("page")
	c.thing("person")
	c.must("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z'")
	time.Sleep(2 * time.Millisecond)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", pg)
	r := c.rows("select updated_at, deleted_at from entities where id=?", pg)[0]
	s.K("a tombstone bumps updated_at to the tombstone instant, and the trigger does not re-fire itself", r[0] == r[1] && val(r[0]) > "2000", r)
}

func cTypeProbe(s *S, typ string) (int64, error) {
	return s.fresh().tryIdentity(typ, "Type witness", nil)
}

func editRevisions(s *S) {
	c := s.fresh()
	page := c.page("Revision witness")
	revision := func(id int64) int64 { return c.n("SELECT revision FROM entities WHERE id=?", id) }
	stamp := func(id int64) string { return c.str("SELECT updated_at FROM entities WHERE id=?", id) }
	s.K("an edit revision starts positive", revision(page) > 0)

	// ---- creation is not an edit: the entity, its owned preferred name and its typed row are one write
	created := true
	for i := range 25 { // cookbook/capture's two statements, the clock supplied by the statement
		id := c.addPage(fmt.Sprintf("Created page %d", i), nil, "body", autoKey)
		created = created && id != 0 && revision(id) == 1 && c.str("SELECT updated_at = created_at FROM entities WHERE id=?", id) == "1"
	}
	s.K("creation is not an edit: a page made by cookbook/capture's entity and owned-name statements is at revision 1 with updated_at = created_at", created)
	var edited []string
	for _, typ := range []string{"page", "person", "metric", "file"} {
		id := c.identity(typ, "ui", "Created "+typ, nil, "")
		switch typ {
		case "person", "metric", "file":
			c.domain(typ, id, nil)
		}
		if revision(id) != 1 || c.str("SELECT updated_at = created_at FROM entities WHERE id=?", id) != "1" {
			edited = append(edited, typ)
		}
	}
	s.K("creation is not an edit: a page, person, metric and file stay at revision 1 with updated_at = created_at after their typed row", len(edited) == 0, edited)
	renamed := c.page("Rename witness")
	renameBefore := revision(renamed)
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Renamed witness','renamed witness')", renamed)
	c.must("UPDATE entities SET preferred_name_key='renamed witness' WHERE id=?", renamed)
	s.K("a rename, the new spelling and then its selection, advances revision", revision(renamed) > renameBefore)
	nameBefore := revision(page)
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Revision alias','revision alias')", page)
	s.K("owned alias insertion advances revision", revision(page) > nameBefore)
	nameBefore = revision(page)
	c.must("UPDATE entity_names SET title='REVISION ALIAS' WHERE entity_id=? AND name_key='revision alias'", page)
	s.K("equivalent owned spelling edit advances revision", revision(page) > nameBefore)
	nameBefore = revision(page)
	c.must("UPDATE entities SET preferred_name_key='revision alias' WHERE id=?", page)
	s.K("already-owned preferred selection advances revision", revision(page) > nameBefore)
	nameBefore = revision(page)
	c.must("UPDATE entity_names SET title=title WHERE entity_id=?", page)
	c.must("UPDATE entities SET preferred_name_key=preferred_name_key WHERE id=?", page)
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'REVISION ALIAS','revision alias') ON CONFLICT(name_key) DO NOTHING", page)
	s.K("owned spelling selection and conflict no-ops preserve revision", revision(page) == nameBefore)
	c.must("BEGIN IMMEDIATE")
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Rollback alias','rollback alias')", page)
	c.must("UPDATE entity_names SET title='Revision Alias' WHERE name_key='revision alias'")
	c.must("UPDATE entities SET preferred_name_key='rollback alias' WHERE id=?", page)
	c.must("ROLLBACK")
	s.K("owned name changes rollback spelling selection and revision", revision(page) == nameBefore && c.str("SELECT preferred_name_key FROM entities WHERE id=?", page) == "revision alias" && c.str("SELECT title FROM entity_names WHERE name_key='revision alias'") == "REVISION ALIAS" && c.n("SELECT count(*) FROM entity_names WHERE name_key='rollback alias'") == 0)
	_, zeroErr := c.tryIdentity("page", "Zero revision", M{"revision": 0})
	s.K("zero revision refused", zeroErr != nil && strings.Contains(zeroErr.Error(), "entities_revision"))
	c.must("UPDATE entities SET revision=5 WHERE id=?", page)
	s.K("revision decrease refused", strings.HasPrefix(c.tryx("UPDATE entities SET revision=4 WHERE id=?", page), "ERR"))
	before := revision(page)
	c.must("UPDATE entities SET body=body WHERE id=?", page)
	s.K("body no-op preserves revision", revision(page) == before)
	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE entities SET body='rollback' WHERE id=?", page)
	c.must("ROLLBACK")
	s.K("rollback restores revision", revision(page) == before)
	c.must("UPDATE entities SET body='committed' WHERE id=?", page)
	s.K("body changes advance revision", revision(page) > before)
	before = revision(page)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", page)
	s.K("lifecycle changes advance revision", revision(page) > before)
	before = revision(page)
	c.must("UPDATE entities SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?", page)
	s.K("clock alone does not advance revision", revision(page) == before)
	for _, typ := range []string{"person", "metric", "file"} {
		id := c.identity(typ, "ui", "Detail "+typ, nil, "")
		c.domain(typ, id, nil)
		if typ == "person" {
			before = revision(id)
			c.must("UPDATE people SET name='changed' WHERE id=?", id)
			s.K("person details advance revision", revision(id) > before)
		}
		if typ == "metric" {
			before = revision(id)
			c.must("UPDATE metrics SET daily=1 WHERE id=?", id)
			s.K("metric details advance revision", revision(id) > before)
			before = revision(id)
			c.must("UPDATE metrics SET daily=1 WHERE id=?", id)
			s.K("a no-op edit of the daily mark keeps the revision", revision(id) == before)
		}
		if typ == "file" {
			before = revision(id)
			c.must("UPDATE files SET preview=? WHERE id=?", jpegBytes, id)
			s.K("file details advance revision", revision(id) > before)
		}
	}
	other := c.page("Other endpoint")
	// a link is an edit of the page it leaves and of no page it points at
	before, otherBefore, otherStamp := revision(page), revision(other), stamp(other)
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'part-of',"+NOW+",'ui')", page, other)
	s.K("a link insertion advances the page it leaves", revision(page) > before)
	s.K("a link insertion advances no page it points at", revision(other) == otherBefore && stamp(other) == otherStamp)
	before = revision(page)
	c.must("UPDATE links SET note='note' WHERE from_id=? AND to_id=?", page, other)
	s.K("a link note edit advances the page it leaves", revision(page) > before)
	s.K("a link note edit advances no page it points at", revision(other) == otherBefore && stamp(other) == otherStamp)
	before = revision(page)
	c.must("DELETE FROM links WHERE from_id=? AND to_id=?", page, other)
	s.K("a link deletion advances the page it leaves", revision(page) > before)
	s.K("a link deletion advances no page it points at", revision(other) == otherBefore && stamp(other) == otherStamp)
	// a wikilink is derived from the body, whose own change advanced the page
	before, otherBefore = revision(page), revision(other)
	pageStamp, otherStamp := stamp(page), stamp(other)
	untouched := func() bool {
		return revision(page) == before && stamp(page) == pageStamp && revision(other) == otherBefore && stamp(other) == otherStamp
	}
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'wikilink',"+NOW+",'ui')", page, other)
	s.K("a wikilink insertion advances no page: it is derived from the body", untouched())
	c.must("DELETE FROM links WHERE from_id=? AND to_id=? AND kind='wikilink'", page, other)
	s.K("a wikilink deletion advances no page: it is derived from the body", untouched())
	c.must("UPDATE entities SET revision=9223372036854775807 WHERE id=?", page)
	s.K("revision exhaustion refuses edit", strings.HasPrefix(c.tryx("UPDATE entities SET body='overflow' WHERE id=?", page), "ERR") && c.str("SELECT body FROM entities WHERE id=?", page) == "committed" && revision(page) == 9223372036854775807)
}
