package tests

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// The bounds the DDL itself puts on stored text, on a tombstone's time and on the shape of the file: what a writer
// must not be trusted to remember. Every probe runs in a rollback-only savepoint, so a value a mutant lets through
// changes nothing for the next probe.
func schemaBounds(s *S) {
	importKeyBounds(s)
	emptyImportKeyIncident(s)
	nameUnitAndNoteBounds(s)
	tombstoneOrder(s)
	entityColumnOrder(s)
	seedIsAtomic(s)
	triggersAvoidConflictShortcuts(s)
}

const bounds0 = "'2026-01-01T00:00:00.000Z'"

// rolledBack runs one statement in a savepoint that is always rolled back and says what it returned: "OK" or "ERR …".
func (c *C) rolledBack(q string, args ...any) string {
	c.must("SAVEPOINT bounds_probe")
	r := c.tryx(q, args...)
	c.must("ROLLBACK TO bounds_probe")
	c.must("RELEASE bounds_probe")
	return r
}

func importKeyBounds(s *S) {
	c := s.freshWith(F{Path: filepath.Join(s.dir, "import-key-bounds.db"), Hardened: true})
	tables := []struct {
		name, insert string
		args         []any // before the key, which is always the last parameter
		notNull      bool  // the key is a required column: NULL is not a key it takes
	}{
		{"entities", "INSERT INTO entities(entity_type,preferred_name_key,source,created_at,updated_at,import_key) VALUES('page','import key witness','ui'," + bounds0 + "," + bounds0 + ",?)", nil, false},
		{"measurements", "INSERT INTO measurements(metric_id,day,value,source,created_at,import_key) VALUES(1,'2026-01-01',3,'ui'," + bounds0 + ",?)", nil, false},
		{"capture_keys", "INSERT INTO capture_keys(source,page_id,recorded_at,import_key) VALUES('ui',1," + bounds0 + ",?)", nil, true}, // page 1 is Mood's
	}
	accepted := []struct {
		desc string
		key  any
	}{
		{"NULL", nil},
		{"a short key", "k"},
		{"a 20-digit number as text", "18446744073709551615"},
		{"spaces around the key", " padded "},
		{"512 ASCII bytes", strings.Repeat("a", 512)},
		{"512 bytes of two-byte characters", strings.Repeat("é", 256)},
	}
	refused := []struct{ desc, key string }{
		{"the empty key", ""},
		{"a lone NUL", "\x00"},
		{"an embedded NUL", "k\x00hidden"},
		{"513 ASCII bytes", strings.Repeat("a", 513)},
		{"514 bytes of two-byte characters", strings.Repeat("é", 257)},
		{"100000 bytes", strings.Repeat("a", 100000)},
	}
	for _, t := range tables {
		for _, k := range accepted {
			if k.key == nil && t.notNull {
				continue
			}
			r := c.rolledBack(t.insert, append(append([]any{}, t.args...), k.key)...)
			s.K(t.name+"_import_key accepts "+k.desc, r == "OK", r)
		}
		for _, k := range refused {
			r := c.rolledBack(t.insert, append(append([]any{}, t.args...), k.key)...)
			s.K(t.name+"_import_key refuses "+k.desc, strings.Contains(r, t.name+"_import_key"), clip(r, 120))
		}
	}
}

// An importer that sent the empty string as a key lost every row after the first from its source without an error:
// NOT NULL puts the empty string into the partial unique index, and ON CONFLICT DO NOTHING reads the second row as
// "imported before".
func emptyImportKeyIncident(s *S) {
	c := s.fresh()
	insert := "INSERT INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES(1,?,?,'import:synthetic',?," + NOW + ") " +
		"ON CONFLICT(source,import_key,metric_id) WHERE import_key IS NOT NULL DO NOTHING"
	r1 := c.tryx(insert, "2026-01-01", 1, "")
	r2 := c.tryx(insert, "2026-01-02", 2, "")
	s.K("an empty import_key is refused, never accepted once and then read as imported before",
		strings.Contains(r1, "measurements_import_key") && strings.Contains(r2, "measurements_import_key") && c.n("SELECT count(*) FROM measurements") == 0, r1, r2, "stored:", c.n("SELECT count(*) FROM measurements"))

	c = s.fresh()
	for i, key := range []string{"a", "b", "a"} {
		if r := c.tryx(insert, "2026-01-0"+fmt.Sprint(i+1), i+1, key); r != "OK" {
			stop("keyed import: %s", r)
		}
	}
	s.K("distinct import keys of one source are all stored and a re-sent key is skipped", c.n("SELECT count(*) FROM measurements") == 2 && c.n("SELECT count(*) FROM measurements WHERE import_key='a'") == 1)
}

func nameUnitAndNoteBounds(s *S) {
	c := s.freshWith(F{Path: filepath.Join(s.dir, "text-bounds.db"), Hardened: true})
	person := c.identity("person", "ui", "Name bound witness", nil, "")
	metric := c.identity("metric", "ui", "Unit bound witness", nil, "")
	pageA, pageB := c.page("Note bound witness A"), c.page("Note bound witness B")
	probe := func(constraint string, run func(v any) string, accepted []string, refused []struct{ desc, v string }) {
		for _, v := range accepted {
			r := run(v)
			s.K(fmt.Sprintf("%s accepts %q", constraint, clip(v, 20)), r == "OK", clip(r, 120))
		}
		for _, x := range refused {
			r := run(x.v)
			s.K(constraint+" refuses "+x.desc, strings.Contains(r, constraint), clip(r, 120))
		}
	}

	probe("people_name", func(v any) string {
		return c.rolledBack("INSERT INTO people(id,name) VALUES(?,?)", person, v)
	}, []string{"Ana Pop", "A", strings.Repeat("a", 240), strings.Repeat("é", 120)}, []struct{ desc, v string }{
		{"the empty name", ""},
		{"a leading space", " Ana"},
		{"a trailing space", "Ana "},
		{"only spaces", "   "},
		{"a trailing NUL", "Ana\x00"},
		{"an embedded NUL", "A\x00na"},
		{"241 ASCII bytes", strings.Repeat("a", 241)},
		{"242 bytes of two-byte characters", strings.Repeat("é", 121)},
	})
	c.must("INSERT INTO people(id,name) VALUES(?,'Ana Pop')", person)
	s.K("people_name refuses an edit to an untrimmed name", strings.Contains(c.rolledBack("UPDATE people SET name=' Ana' WHERE id=?", person), "people_name"))

	probe("metrics_unit", func(v any) string {
		return c.rolledBack("INSERT INTO metrics(id,unit) VALUES(?,?)", metric, v)
	}, []string{"", "kg", "mg/dL", "%", strings.Repeat("u", 32), strings.Repeat("é", 16)}, []struct{ desc, v string }{
		{"a leading space", " kg"},
		{"a trailing space", "kg "},
		{"only a space", " "},
		{"a trailing NUL", "kg\x00"},
		{"an embedded NUL", "k\x00g"},
		{"33 ASCII bytes", strings.Repeat("u", 33)},
		{"34 bytes of two-byte characters", strings.Repeat("é", 17)},
	})

	notes := []struct{ desc, v string }{{"a trailing NUL", "note\x00"}, {"an embedded NUL", "no\x00te"}}
	okNotes := []string{"", "a note", "two\nlines"}
	probe("links_note", func(v any) string {
		return c.rolledBack("INSERT INTO links(from_id,to_id,kind,note,created_at,source) VALUES(?,?,'wikilink',?,"+bounds0+",'ui')", pageA, pageB, v)
	}, okNotes, notes)
	s.K("links_note allows no note", c.rolledBack("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'wikilink',"+bounds0+",'ui')", pageA, pageB) == "OK")
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'wikilink',"+bounds0+",'ui')", pageA, pageB)
	s.K("links_note refuses an edit that adds a NUL", strings.Contains(c.rolledBack("UPDATE links SET note='no'||char(0)||'te' WHERE from_id=?", pageA), "links_note"))

	probe("link_kinds_note", func(v any) string {
		return c.rolledBack("INSERT INTO link_kinds(kind,note) VALUES('note-probe',?)", v)
	}, okNotes, notes)
	s.K("link_kinds_note allows no note", c.rolledBack("INSERT INTO link_kinds(kind) VALUES('note-probe')") == "OK")
}

// A tombstone follows creation: deleted_at is never earlier than created_at (the same instant is allowed).
func tombstoneOrder(s *S) {
	c := s.freshWith(F{Path: filepath.Join(s.dir, "tombstone-order.db"), Hardened: true})
	const created, before, after = "2026-01-01T00:00:00.000Z", "2025-12-31T23:59:59.999Z", "2026-01-01T00:00:00.001Z"
	tables := []struct {
		name, insert string
		args         []any // before the tombstone, which is always the last parameter
		base         int64
	}{
		{"entities", "INSERT INTO entities(entity_type,preferred_name_key,source,created_at,updated_at,deleted_at) VALUES('page','tombstone witness','ui'," + bounds0 + "," + bounds0 + ",?)", nil, c.page("Tombstone base")},
	}
	for _, t := range tables {
		name := t.name + "_deleted_after_created"
		insert := func(deleted any) string {
			return c.rolledBack(t.insert, append(append([]any{}, t.args...), deleted)...)
		}
		s.K(name+" refuses a tombstone before creation", strings.Contains(insert(before), name))
		s.K(name+" accepts a tombstone at the creation instant", insert(created) == "OK")
		s.K(name+" accepts a later tombstone", insert(after) == "OK")
		s.K(name+" accepts a live row", insert(nil) == "OK")

		row := func() string { return c.tab("SELECT * FROM " + t.name + " WHERE id=" + fmt.Sprint(t.base)) }
		state := row()
		r := c.rolledBack("UPDATE "+t.name+" SET deleted_at=? WHERE id=?", before, t.base)
		s.K(name+" refuses an update that tombstones before creation", strings.Contains(r, name) && row() == state, clip(r, 120))
		s.K(name+" accepts an update that tombstones at the creation instant", c.rolledBack("UPDATE "+t.name+" SET deleted_at=? WHERE id=?", created, t.base) == "OK")
		s.K(name+" accepts an update that tombstones later", c.rolledBack("UPDATE "+t.name+" SET deleted_at=? WHERE id=?", after, t.base) == "OK")
	}
}

// Large text is the last column, so the small ones before it (deleted_at, entity_type, …) are read without walking
// the overflow pages of the body.
func entityColumnOrder(s *S) {
	c := s.fresh()
	cols := c.col("SELECT name FROM pragma_table_info('entities') ORDER BY cid")
	s.K("entities.body is the last column, behind every small column", len(cols) > 1 && cols[len(cols)-1] == "body", cols)
}

// The seed is one transaction: a seed statement that fails leaves no half-seeded Mood behind.
func seedIsAtomic(s *S) {
	const seed = "INSERT INTO metrics(id, unit) VALUES (1, '1-5');"
	if !strings.Contains(s.ddl, seed) {
		stop("the DDL no longer seeds the Mood metric with %q", seed)
	}
	c := s.connect("")
	_, err := c.Exec(strings.Replace(s.ddl, seed, "INSERT INTO metrics(id, unit) VALUES (1, NULL);", 1))
	s.K("a failing last seed statement is an error of the DDL script", err != nil)
	c.tryx("ROLLBACK") // the script stopped inside the seed transaction when the seed is one
	s.K("a failing last seed statement leaves no half-seeded Mood entity or name", c.n("SELECT (SELECT count(*) FROM entities)+(SELECT count(*) FROM entity_names)") == 0)

	ok := s.fresh()
	s.K("the seed makes Mood: an entity, its name and its metrics row", ok.n("SELECT count(*) FROM entities e JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key JOIN metrics m ON m.id=e.id WHERE e.id=1 AND n.title='Mood'") == 1)
}

var conflictShortcut = regexp.MustCompile(`(?i)\bOR\s+(IGNORE|REPLACE)\b`)

// aminte_meta.writers forbids OR IGNORE (it skips CHECK and NOT NULL violations silently) and OR REPLACE (a delete);
// the file's own triggers follow it.
func triggersAvoidConflictShortcuts(s *S) {
	triggers, offenders := 0, []string{}
	for _, st := range statements(s.ddl) {
		if !strings.HasPrefix(strings.ToUpper(strings.Join(strings.Fields(code(st)), " ")), "CREATE TRIGGER") {
			continue
		}
		triggers++
		if conflictShortcut.MatchString(code(st)) {
			offenders = append(offenders, clip(strings.Join(strings.Fields(code(st)), " "), 60))
		}
	}
	s.K("the DDL has triggers to read", triggers > 30, triggers)
	s.K("no trigger uses OR IGNORE or OR REPLACE", len(offenders) == 0, offenders)

}

// The two lookups that were widened: a day is found by entities_day (only pages with a day are in it) and a backlink
// of one kind by links_to on both columns. The plans are read for the access path, not compared as text.
func lookupPlans(s *S) {
	c := s.fresh()
	day := c.dayPage("2026-09-29", "")
	target := c.page("Link target without a day")
	c.link(day, target, "wikilink")
	plan := c.plan("SELECT id FROM entities WHERE day = ?", "2026-09-29")
	s.K("entities_day serves a lookup by day", strings.Contains(plan, "SEARCH") && strings.Contains(plan, "entities_day"), plan)
	plan = c.plan("SELECT id FROM entities WHERE day IS NULL")
	s.K("entities_day holds only pages with a day: a lookup of pages without one does not use it", !strings.Contains(plan, "entities_day"), plan)
	plan = c.plan("SELECT from_id FROM links WHERE to_id = ? AND kind = ?", target, "wikilink")
	s.K("links_to serves the backlinks of one kind by both columns", strings.Contains(plan, "SEARCH") && strings.Contains(plan, "links_to") && strings.Contains(plan, "kind=?"), plan)
	plan = c.plan("SELECT from_id FROM links WHERE to_id = ?", target)
	s.K("links_to still serves the backlinks of every kind", strings.Contains(plan, "SEARCH") && strings.Contains(plan, "links_to"), plan)

	plan = c.plan(s.d.Block("day-view"), P{"day": "2026-09-29"})
	s.K("cookbook/day-view reads the pages of a day through entities_day", strings.Contains(plan, "entities_day"), plan)
	plan = c.plan(s.d.Block("backlinks"), P{"page_id": target})
	s.K("cookbook/backlinks is served by links_to", strings.Contains(plan, "SEARCH") && strings.Contains(plan, "links_to"), plan)
}
