package tests

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// cookbook: every SQL block of the cookbook (docs/cookbook/) prepares and runs on a seeded database, statement by
// statement with its ids carried by RETURNING — on a normal connection and on a hardened one
// (SQLITE_DBCONFIG_DEFENSIVE + trusted_schema=OFF, contract/connections); and every block that only reads runs on
// a reader (mode=ro + trusted_schema=OFF, contract/connections).
func cookbook(s *S) {
	cookbookMeasurementReads(s)
	bl := s.d.CookbookBlocks()
	first, order := s.d.Blocks()
	s.K("the cookbook has at least 20 SQL blocks, one or more per recipe, 18 recipes", len(bl) >= 20 && len(first) == 18 && slices.Equal(order, s.d.CookbookOrder()), order)

	seeded := func(hardened bool, path string) (*C, P) {
		c := s.freshWith(F{Hardened: hardened, Path: path})
		link := func(from, to int64, kind string) { // the seed's links: a refused one stops the suite
			if r := c.link(from, to, kind); r != "OK" {
				stop("cookbook seed, a link of the kind %s: %s", kind, r)
			}
		}
		dp := c.dayPage("2026-09-28", "Shipped the schema with [[Sam]] in [[Japan]]. [[Aminte]]")
		wp := c.page("Aminte")
		cat := c.page("Biomarkers")
		link(dp, wp, "wikilink")
		pe := c.named("person", "Sam", M{"name": "Sam"})
		gh := c.page("Ana")
		pl := c.page("Japan")
		link(dp, pe, "wikilink")
		link(dp, pl, "wikilink")
		w := c.metric("weight", "kg")
		c.metric("vitamin_d", "")
		c.measure(w, "2026-09-29", 71.2)
		c.measure(w, "2026-09-28", 70.9)
		old := c.pageW("Sourdogh", nil, "Feed the starter. [[Aminte]]")
		link(old, wp, "wikilink")
		return c, P{"include_deleted": 0, "old_id": old, "new_title": "Sourdough starter", "new_key": "sourdough starter", "found_id": wp, "target_id": wp, "target_ids": "[]", "day_page_id": dp, "mistaken_row_id": 2, "from_day": "2026-01-15", "to_day": "2026-09-10",
			"day": "2026-09-29", "today": "2026-09-29", "page_id": wp, "person_id": pe, "entity_id": pe, "handle_title": "Bob Sample", "handle_key": "bob sample", "ghost_id": gh,
			"query": "schema", "key": "newpage", "title": "Newpage", "metric_id": w, "wrong_row_id": 1, "source": "ui",
			"import_key": "notes/sourdough.md", "metric": "vitamin_d", "as_of": "2099-01-01T00:00:00.000Z", "parent_id": cat,
			"sha256": strings.Repeat("ab", 32), "mime": "image/heic", "preview": jpegBytes, "file_title": "2026-09-29 Lake.jpg", "append_embed": 1,
			"file_key": "2026-09-29 lake.jpg", "body": "The lake at dawn with [[Sam]].",
			"snapshot": filepath.ToSlash(filepath.Join(s.dir, fmt.Sprintf("cookbook-snapshot-%v.db", hardened)))}
	}

	for _, hardened := range []bool{false, true} {
		tag := map[bool]string{false: "plain", true: "hardened"}[hardened]
		c, p := seeded(hardened, "")
		nprep, nfail := 0, 0
		var why []string
		for _, b := range bl { // every statement prepares against the DDL
			for _, st := range statements(b.sql) {
				switch verb(st) {
				case "BEGIN", "COMMIT", "SAVEPOINT", "RELEASE", "ROLLBACK":
					continue
				}
				nprep++
				nulls := P{}
				for _, m := range paramName.FindAllStringSubmatch(code(st), -1) {
					nulls[m[1]] = nil
				}
				if _, e := c.query("EXPLAIN "+st, nulls); e != nil {
					nfail++
					why = append(why, b.key+": "+e.Error())
				}
			}
		}
		s.K(tag+": all statements of the cookbook prepare", nprep > 0 && nfail == 0, nprep, why)
		for _, b := range bl { // and every block runs, in document order, on one database
			r := "OK"
			if _, e := c.runBlock(b.sql, p, nil); e != nil {
				r = "ERR " + e.Error()
				c.tryx("ROLLBACK")
			}
			s.K(tag+": "+b.key+" runs", r == "OK", r)
		}
		s.K(tag+": the database is clean after the whole cookbook", c.integrityOK())
	}

	// a reader of contract/connections: mode=ro and trusted_schema=OFF, on a file a hardened writer seeded
	path := filepath.Join(s.dir, "cookbook-reader.db")
	_, p := seeded(true, path)
	r := s.connect(path, "mode=ro", "_pragma=trusted_schema(0)")
	nread := 0
	var bad []string
	for _, b := range bl {
		reads := true
		for _, st := range statements(b.sql) {
			reads = reads && (verb(st) == "SELECT" || verb(st) == "WITH")
		}
		if !reads {
			continue
		}
		nread++
		if _, e := r.runBlock(b.sql, p, nil); e != nil {
			bad = append(bad, b.key+": "+e.Error())
		}
	}
	s.K("reader: every block that only reads runs on a mode=ro connection with trusted_schema=OFF",
		nread >= 8 && len(bad) == 0 && r.n("PRAGMA trusted_schema") == 0, nread, bad)
}

func cookbookMeasurementReads(s *S) {
	c := s.fresh()
	weight := c.metric("weight", "kg")
	for _, metric := range []int64{1, weight} {
		c.must("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES (?,'2026-10-01',1,'ui','2026-10-01T12:00:00.000Z')", metric)
	}
	series, mood, day := s.d.Block("metric-series"), s.d.Block("mood-over-time"), s.d.Block("day-view")
	params := P{"day": "2026-10-01", "metric": "weight", "include_deleted": 0}
	s.K("active measurement recipes expose live metrics", len(c.rows(series, params)) == 1 && len(c.rows(mood)) == 1 && len(c.rows(day, params)) == 2)
	// Mood is seeded when the DDL is applied, so its tombstone is "now": a fixed date would precede its creation.
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id IN (1,?)", weight)
	s.K("active measurement recipes hide tombstoned metrics", len(c.rows(series, params)) == 0 && len(c.rows(mood)) == 0 && len(c.rows(day, params)) == 0)
	s.K("a metric tombstone retains its facts", c.n("SELECT count(*) FROM measurement_values") == 2)
	c.must("UPDATE entities SET deleted_at=NULL WHERE id IN (1,?)", weight)
	s.K("active measurement recipes restore readings on metric revival", len(c.rows(series, params)) == 1 && len(c.rows(mood)) == 1 && len(c.rows(day, params)) == 2)

	// the readings asked for on a day (D31): the live daily metrics in name order, a NULL value where the day has none
	asked := sqlBlocks(s.d.Page("cookbook/day-view.md"))
	if s.K("the day view has the block of the readings asked for on this day", len(asked) == 2) {
		c = s.fresh()
		sleep, meditate := c.metric("Sleep", "h"), c.metric("Meditate", "")
		count, gone := c.metric("Count", ""), c.metric("Zeta", "")
		for _, id := range []int64{sleep, meditate, gone} {
			c.must("UPDATE metrics SET daily=1 WHERE id=?", id)
		}
		c.measure(sleep, "2026-10-01", 7.5)
		c.measure(count, "2026-10-01", 3)
		c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", gone)
		s.K("the readings asked for on a day: the live daily metrics in name order, a metric with no reading has a NULL value, a metric that is not daily or is tombstoned is absent",
			c.tab(asked[1], P{"day": "2026-10-01"}) == "Meditate||None; Sleep|h|7.5", c.tab(asked[1], P{"day": "2026-10-01"}))
	}

	blocks := sqlBlocks(s.d.Page("cookbook/metric-series.md"))
	if !s.K("metric series includes a recorded-time cutoff recipe", len(blocks) == 2) {
		return
	}
	asOf := blocks[1]
	c = s.fresh()
	weight = c.metric("weight", "kg")
	put := func(value any, at string, prior any) int64 {
		return c.n("INSERT INTO measurements(metric_id,day,value,source,created_at,supersedes_id) VALUES (?,'2026-10-01',?,'ui',?,?) RETURNING id", weight, value, at, prior)
	}
	read := func(at string) string { return c.tab(asOf, P{"metric": "weight", "as_of": at}) }
	a := put(71.25, "2026-10-01T10:00:00.000Z", nil)
	b := put(72.25, "2026-10-01T12:00:00.000Z", a)
	put(73.25, "2026-10-01T11:00:00.000Z", b)   // the clock regressed across a correction
	put(80.25, "2026-10-01T10:00:00.000Z", nil) // an independent same-day reading
	s.K("recorded-time cutoff excludes later recorded facts", read("2026-10-01T09:59:59.999Z") == "")
	s.K("recorded-time cutoff retains original before a later correction", read("2026-10-01T10:30:00.000Z") == "1|2026-10-01|71.25|None; 4|2026-10-01|80.25|None", read("2026-10-01T10:30:00.000Z"))
	s.K("recorded-time cutoff follows eligible descendants across a clock regression", read("2026-10-01T11:30:00.000Z") == "3|2026-10-01|73.25|None; 4|2026-10-01|80.25|None", read("2026-10-01T11:30:00.000Z"))
	r := put(nil, "2026-10-01T13:00:00.000Z", int64(3))
	s.K("recorded-time cutoff applies retractions at the cutoff", read("2026-10-01T13:00:00.000Z") == "4|2026-10-01|80.25|None", read("2026-10-01T13:00:00.000Z"))
	put(74.25, "2026-10-01T13:00:00.000Z", r)
	s.K("recorded-time cutoff resolves equal timestamps by correction ancestry", read("2026-10-01T13:00:00.000Z") == "4|2026-10-01|80.25|None; 6|2026-10-01|74.25|None", read("2026-10-01T13:00:00.000Z"))
	c.must("UPDATE entities SET deleted_at='2026-10-02T00:00:00.000Z' WHERE id=?", weight)
	s.K("recorded-time history labels present metric lifecycle rather than hiding facts", read("2026-10-01T13:00:00.000Z") == "4|2026-10-01|80.25|2026-10-02T00:00:00.000Z; 6|2026-10-01|74.25|2026-10-02T00:00:00.000Z", read("2026-10-01T13:00:00.000Z"))
}
