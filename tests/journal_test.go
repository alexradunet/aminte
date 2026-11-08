package tests

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"aminte/text"
)

// journal: the journal (D5, D15, D22, D23) — the day page and its CHECK, capture that appends to it
// (cookbook/capture), the cookbook/day-view day view, the days that name someone or somewhere
// (cookbook/days-that-name), and what stands in for recurrence,
// events, with explicit planning kept separate from observed habits.
func journal(s *S) {
	journalDefinition(s)
	periods(s)

	// ---- the day page (pages_day_page)
	c := s.fresh()
	add := func(title string, day any) int64 { return c.addPage(title, day, "", text.TitleKey(title)) }
	journalID := add("2026-09-29", "2026-09-29")
	s.K("a page titled with a day, whose day is its title, is accepted", journalID != 0)
	cleared := c.tryx("UPDATE entities SET day=NULL WHERE id=?", journalID)
	s.K("journal day cannot be cleared", strings.HasPrefix(cleared, "ERR"), cleared)
	c.must("UPDATE entities SET day='2026-09-29' WHERE id=?", journalID)
	s.K("a page titled with a day but no day is refused (pages_day_page)", add("2026-09-28", nil) == 0)
	s.K("...and one whose day is another day", add("2026-09-27", "2026-09-26") == 0)
	s.K("a second page for the same day is refused (the title is unique)", add("2026-09-29", "2026-09-29") == 0)
	s.K("a title that only looks like a day is an ordinary page (2026-02-30, 2026-9-3)", add("2026-02-30", nil) != 0 && add("2026-9-3", nil) != 0)
	s.K("an ordinary page may have a day of its own", add("Trip report", "2026-09-29") != 0)
	cols := c.col("select name from pragma_table_info('entities')")
	s.K("pages have no kind and no inbox column (D5)", !slices.Contains(cols, "kind") && !slices.Contains(cols, "triaged_at"))
	s.K("there is no events table (D22)", c.n("select count(*) from sqlite_schema where name='events'") == 0)

	// ---- cookbook/capture: append to the day page, create it on the first write
	c = s.fresh()
	c.metric("weight", "kg")
	steps, ok := s.saveSteps()
	if !ok {
		stop("cookbook/save-a-body does not have one statement of each step: %v", steps.counts)
	}
	p := P{}
	sync := func(st string) { // the cookbook/save-a-body link sync, inside cookbook/capture's transaction
		if strings.HasPrefix(strings.ToUpper(st), "UPDATE ENTITIES SET BODY") {
			id := p["page_id"].(int64)
			c.docSave(steps, id, c.str("select body from entities where id=?", id), c.str("select preferred_name_key from entities where id=?", id), true)
		}
	}
	_, e := c.runBlock(s.d.Block("capture"), p, sync)
	row := c.tab("select n.title,e.day,e.body from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key where e.id=?", p["page_id"])
	s.K("cookbook/capture run literally on a new day creates the day page with the entry", e == nil && row == "2026-09-29|2026-09-29|Shipped the schema doc. Review pending. [[Aminte]]", e, row)
	s.K("...links it to what the entry names, and attaches the mood to it",
		c.n("select count(*) from links l join entity_names n on n.entity_id=l.to_id where l.from_id=? and n.name_key='aminte'", p["page_id"]) == 1 &&
			c.tab("select captured_with_id from measurements") == val(p["page_id"]))
	var moodInsert string
	for _, st := range statements(s.d.Block("capture")) {
		if strings.HasPrefix(strings.ToUpper(code(st)), "INSERT INTO MEASUREMENTS") {
			moodInsert = st
		}
	}
	mood := c.n("select entity_id from entity_names where name_key='mood'")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", mood)
	c.must(moodInsert, p)
	s.K("cookbook/capture does not write tombstoned Mood", c.n("select count(*) from measurements") == 1)
	c.must("UPDATE entities SET deleted_at=NULL WHERE id=?", mood)
	c.must(moodInsert, p)
	s.K("cookbook/capture writes revived Mood", c.n("select count(*) from measurements") == 2)

	var sel, upd []string
	for _, st := range statements(s.d.Block("capture")) {
		switch u := strings.ToUpper(code(st)); {
		case strings.HasPrefix(u, "SELECT"):
			sel = append(sel, st)
		case strings.HasPrefix(u, "UPDATE ENTITIES"):
			upd = append(upd, st)
		}
	}
	s.K("cookbook/capture finds the day page by its key, the day itself", len(sel) > 0 && c.tabOrErr(sel[0]) == val(p["page_id"])+"|None")
	plan := ""
	if len(sel) > 0 {
		plan = c.plan(sel[0])
	}
	s.K("...an indexed exact registry lookup", strings.Contains(strings.ToUpper(plan), "SEARCH") && strings.Contains(strings.ToUpper(plan), "INDEX"), plan)
	if len(upd) == 0 {
		stop("cookbook/capture has no UPDATE entities")
	}
	c.must(upd[0], P{"page_id": p["page_id"]})
	body := c.str("select body from entities where id=?", p["page_id"])
	s.K("a second capture that day appends after a blank line, in the same page", strings.Count(body, "Review pending.") == 2 &&
		strings.Contains(body, "[[Aminte]]\n\nShipped") && c.n("select count(*) from entities where day=?", "2026-09-29") == 1)
	c = s.fresh()
	a := c.capture("first", "2026-09-30")
	b := c.capture("met [[Ana]]", "2026-09-30")
	d := c.capture("next day", "2026-10-01")
	s.K("capture: one page per day, entries in order", a == b && b != d && c.str("select body from entities where id=?", a) == "first\n\nmet [[Ana]]")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", d)
	e2 := c.capture("again", "2026-10-01")
	s.K("capture on a tombstoned day page revives it, never a second page for the day", e2 == d && c.str("select deleted_at from entities where id=?", d) == "None")
	c.savePage("see [[2026-12-02]]", "")
	g := c.n("select entity_id from entity_names where name_key='2026-12-02'")
	s.K("a link that names a day before anything was written makes that day's page, with its day", c.tab("select day, body from entities where id=?", g) == "2026-12-02|")
	s.K("...and the first capture of that day writes into it", c.capture("it came", "2026-12-02") == g && c.str("select body from entities where id=?", g) == "it came")

	captureKeys(s)

	// ---- cookbook/day-view
	DV := s.d.Block("day-view")
	c = s.fresh()
	c.dayPage("2026-09-29", "a day of work")
	c.dayPage("2026-09-28", "yesterday")
	essay := c.pageW("Essay", "2026-09-29", "text")
	// a page whose creation wrote two different clocks (an import keeps the note's own creation time) was never edited
	imported, importErr := c.tryIdentity("page", "Imported note", M{"day": "2026-09-29", "body": "kept", "created_at": "2020-05-01T08:00:00.000Z", "updated_at": "2026-10-01T09:00:00.000Z", "source": "import:vault", "import_key": "notes/imported.md"})
	if importErr != nil {
		stop("imported fixture: %v", importErr)
	}
	c.page("Link target")
	c.pageW("Yesterday essay", "2026-09-28", "x")
	c.named("person", "Sam")
	wt := c.metric("weight", "kg")
	c.measure(wt, "2026-09-29", 71.2, M{"taken_at": "2026-09-29T06:00:00.000Z"})
	c.measure(wt, "2026-09-29", 70.0, M{"supersedes_id": 1})
	rows := c.rows(DV, P{"day": "2026-09-29"})
	var got, kinds, items []string
	for _, r := range rows {
		got = append(got, val(r[0])+"|"+val(r[2]))
		kinds = append(kinds, val(r[0]))
		items = append(items, val(r[2]))
	}
	s.K("cookbook/day-view shows the day page, the page written that day and the corrected reading",
		slices.Contains(got, "day page|a day of work") && slices.Contains(got, "page|Essay") && slices.Contains(got, "weight|70.0 kg"), tab(rows))
	nDay := 0
	for _, k := range kinds {
		if k == "day page" {
			nDay++
		}
	}
	s.K("...the day page once, as the day page and not again as a page written that day", nDay == 1 && !slices.Contains(got, "page|2026-09-29"), tab(rows))
	none := true
	for _, x := range []string{"yesterday", "Link target", "Sam", "Yesterday essay", "71.2 kg"} {
		none = none && !slices.Contains(items, x)
	}
	s.K("...not another day's page, a link target, a person's page, a page of another day or the superseded reading", none, tab(rows))
	order := len(rows) > 0 && rows[0][0] == "day page"
	seenDated := false
	for _, r := range rows {
		if r[1] != nil {
			seenDated = true
		} else if seenDated {
			order = false
		}
	}
	s.K("...undated items first, the day page leading", order, tab(rows))
	flagged := func() []string {
		var out []string
		for _, r := range c.rows(DV, P{"day": "2026-09-29"}) {
			if strings.Contains(val(r[0]), "(edited)") {
				out = append(out, val(r[2]))
			}
		}
		return out
	}
	s.K(`a page that was only created, with its owned name, is not flagged "(edited)", whatever its two clocks say`, len(flagged()) == 0 && c.n("select count(*) from entities where id in (?,?) and revision = 1", essay, imported) == 2, flagged())
	pid := c.n("select entity_id from entity_names where name_key='essay'")
	c.must("UPDATE entities SET body='text 2' WHERE id=?", pid)
	s.K(`an edited page is flagged "(edited)"`, slices.Equal(flagged(), []string{"Essay"}), flagged())

	// ---- cookbook/days-that-name: the days that name someone or somewhere
	c = s.fresh()
	ana := c.named("person", "Ana", M{"name": "Ana"})
	par := c.page("Lakeside")
	c.capture("with [[Ana]] at [[Lakeside]]", "2026-07-31")
	d2 := c.capture("called [[Ana]]", "2026-08-02")
	c.capture("alone", "2026-08-03")
	c.savePage("an essay about [[Ana]]", "Friends")
	Q := s.d.Block("days-that-name")
	s.K("cookbook/days-that-name lists the day pages that link Ana, newest first, and not an essay that names her", slices.Equal(c.col(Q, P{"entity_id": ana}), []string{"2026-08-02", "2026-07-31"}))
	s.K("cookbook/days-that-name lists the days that name a plain page the same way", slices.Equal(c.col(Q, P{"entity_id": par}), []string{"2026-07-31"}))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", d2)
	s.K("...and drops a tombstoned day", slices.Equal(c.col(Q, P{"entity_id": ana}), []string{"2026-07-31"}))
	plan = c.plan(Q, P{"entity_id": ana})
	s.K("cookbook/days-that-name is served by links_to", strings.Contains(plan, "links_to"), plan)

	// ---- observed habits and birthdays remain distinct from explicit tasks (D23)
	c = s.fresh()
	s.K("no task table and no task graph kinds (D23)", c.n("select count(*) from sqlite_schema where type='table' and name in ('tasks','task_occurrences')") == 0 &&
		c.n("select count(*) from link_kinds where kind in ('spawned','subtask')") == 0)
	mid := c.metric("rent_paid", "")
	for _, x := range [][2]any{{"2026-01-31", 1}, {"2026-02-28", 0}, {"2026-03-31", 1}} {
		c.measure(mid, x[0].(string), x[1])
	}
	s.K(`"did I do it each month" is a 0/1 habit metric`, c.str("select group_concat(value) from (select value from measurement_values where metric_id=? order by day)", mid) == "1.0,0.0,1.0")
	c.named("person", "Ada", M{"birth_day": "1815-12-10"})
	s.K("birthdays are a query over people.birth_day", c.tab("SELECT n.title FROM people pe JOIN entities e on e.id=pe.id JOIN entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key WHERE strftime('%m-%d',pe.birth_day)='12-10'") == "Ada")
	s.K("no event kinds are registered: attended and is-a are gone with events (D22)", c.n("select count(*) from link_kinds where kind in ('attended','is-a')") == 0)
}

// journalDefinition: entities.is_journal is the one stored definition of a journal day page (D5): the preferred key
// is ten characters that round-trip through date(). The vectors are hand-checked, including a Julian day number that
// date() accepts but does not give back. The scratch table below executes what the DDL relies on in SQLite: a CHECK
// may read a VIRTUAL generated column, a trigger's NEW and OLD expose it, and it cannot be inserted into (so a writer
// names its columns).
func journalDefinition(s *S) {
	c := s.fresh()
	for _, v := range []struct {
		key  string
		want int64
	}{
		{"2026-01-01", 1}, {"0000-01-01", 1}, {"9999-12-31", 1}, {"2024-02-29", 1}, {"2026-12-31", 1},
		{"2026-02-30", 0}, {"2026-13-01", 0}, {"2026-00-10", 0}, {"2023-02-29", 0}, {"2026-1-011", 0}, {"2026-01-0x", 0},
		{"2026_01_01", 0}, {"abcdefghij", 0}, {"２０２６-01-01", 0}, {"2459000.50", 0}, {"1234567890", 0},
		{"2026-9-3", 0}, {"2026-01-011", 0}, {"20260101", 0}, {"Diet", 0},
	} {
		day := any(nil)
		if v.want == 1 {
			day = v.key // a journal day page is its own day (entities_day_page)
		}
		id, err := c.tryIdentity("page", v.key, M{"day": day})
		s.K(fmt.Sprintf("entities.is_journal of %q is %d", v.key, v.want), err == nil && c.n("SELECT is_journal FROM entities WHERE id=?", id) == v.want, err)
	}
	s.K("SELECT * returns the generated column, between day and created_at", c.col("SELECT name FROM pragma_table_xinfo('entities') ORDER BY cid")[4] == "is_journal" && c.n("SELECT count(*) FROM (SELECT * FROM entities) WHERE is_journal=1") == 5)

	g := s.connect("")
	g.must(`CREATE TABLE g(id INTEGER PRIMARY KEY, k TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'page',
	  is_journal INTEGER NOT NULL GENERATED ALWAYS AS (length(k) = 10 AND date(k) IS k) VIRTUAL,
	  CONSTRAINT g_plain CHECK (NOT is_journal OR kind = 'page')) STRICT;
	  CREATE TABLE seen(what TEXT);
	  CREATE TRIGGER g_insert BEFORE INSERT ON g BEGIN INSERT INTO seen VALUES ('insert ' || NEW.is_journal); END;
	  CREATE TRIGGER g_update BEFORE UPDATE OF k ON g BEGIN INSERT INTO seen VALUES ('update ' || OLD.is_journal || '>' || NEW.is_journal); END;`)
	s.K("a CHECK may read a VIRTUAL generated column: it refuses a journal key of another type", strings.Contains(g.tryx("INSERT INTO g(k,kind) VALUES ('2026-01-02','person')"), "g_plain"))
	s.K("...and accepts a key that is not a date", g.tryx("INSERT INTO g(k,kind) VALUES ('foo','person')") == "OK")
	s.K("...on UPDATE too", strings.Contains(g.tryx("UPDATE g SET k='2026-01-03' WHERE k='foo'"), "g_plain"))
	g.must("INSERT INTO g(k) VALUES ('2026-01-01')")
	s.K("a BEFORE INSERT trigger's NEW exposes the generated column", g.str("SELECT what FROM seen ORDER BY rowid DESC LIMIT 1") == "insert 1")
	g.must("INSERT INTO g(k) VALUES ('bar')")
	g.must("UPDATE g SET k='2026-03-03' WHERE k='bar'")
	s.K("a BEFORE UPDATE OF trigger's OLD and NEW expose it, NEW from the new value", g.str("SELECT what FROM seen ORDER BY rowid DESC LIMIT 1") == "update 0>1")
	s.K("a generated column cannot be inserted into, so a writer names its columns", strings.Contains(g.tryx("INSERT INTO g(k,is_journal) VALUES ('x',1)"), "generated"))
}

// captureKeys: a capture sent again changes nothing (issue 0085, D28). The second block of cookbook/capture is run
// literally; the writer's own capture runs on the DDL under test; and the table's rules are probed on a hardened file.
func captureKeys(s *S) {
	blocks := sqlBlocks(s.d.Page("cookbook/capture.md"))
	if len(blocks) != 2 {
		stop("cookbook/capture has %d sql blocks, want the capture and the capture sent again", len(blocks))
	}
	c := s.freshWith(F{Hardened: true})
	page := c.capture("Walked the dog", "2026-09-29")
	p := P{"import_key": "send-1", "page_id": page}
	var lookup, keyInsert, moodInsert string
	for _, st := range statements(blocks[1]) {
		switch u := strings.ToUpper(code(st)); {
		case strings.HasPrefix(u, "SELECT PAGE_ID FROM CAPTURE_KEYS"):
			lookup = st
		case strings.HasPrefix(u, "INSERT INTO CAPTURE_KEYS"):
			keyInsert = st
		case strings.HasPrefix(u, "INSERT INTO MEASUREMENTS"):
			moodInsert = st
		}
	}
	if lookup == "" || keyInsert == "" || moodInsert == "" {
		stop("the capture sent again lacks its lookup, its key or its Mood reading: %q %q %q", lookup, keyInsert, moodInsert)
	}
	s.K("cookbook/capture, sent again: a key the file does not hold has no row", len(c.rows(lookup, p)) == 0)
	_, e := c.runBlock(blocks[1], p, nil)
	s.K("...and the block adds the key and the Mood reading of the capture in one transaction", e == nil && c.n("SELECT count(*) FROM capture_keys") == 1 &&
		c.tab("SELECT source, import_key, page_id FROM capture_keys") == "ui|send-1|"+val(page), e)
	s.K("...the Mood reading of that capture carries the same key", c.tab("SELECT source, import_key, captured_with_id, value FROM measurements") == "ui|send-1|"+val(page)+"|4")
	s.K("...a key the file holds has a row, and it names the day page of the capture", c.tabOrErr(lookup) != "" && val(c.rows(lookup, p)[0][0]) == val(page))
	again := c.tryx(keyInsert, p)
	s.K("...the unique pair stops a second row for the same key even when a writer forgets to read it first", strings.Contains(again, "UNIQUE") && c.n("SELECT count(*) FROM capture_keys") == 1, again)

	// the writer's capture, on the DDL under test
	c = s.freshWith(F{Hardened: true})
	mood := 4.0
	st := c.store()
	capture := func(source, key, day, entry string) (int64, bool) {
		id, _, already, err := st.CaptureWithKey(context.Background(), source, key, day, entry, &mood)
		if err != nil {
			stop("capture %s %q: %v", source, key, err)
		}
		return id, already
	}
	first, already := capture("ui", "k1", "2026-10-17", "Walked the dog")
	s.K("the writer: a capture with a key the file does not hold is added", !already && c.str("SELECT body FROM entities WHERE id=?", first) == "Walked the dog")
	again1, already := capture("ui", "k1", "2026-10-17", "Walked the dog")
	s.K("the writer: the same key again adds no paragraph and no Mood reading, and names the same day page",
		already && again1 == first && c.str("SELECT body FROM entities WHERE id=?", first) == "Walked the dog" && c.n("SELECT count(*) FROM measurements") == 1 && c.n("SELECT count(*) FROM capture_keys") == 1)
	capture("ui", "k2", "2026-10-17", "Walked the dog")
	s.K("the writer: another key is another capture, with its own paragraph and Mood reading", c.n("SELECT count(*) FROM measurements") == 2 &&
		c.str("SELECT body FROM entities WHERE id=?", first) == "Walked the dog\n\nWalked the dog")
	_, _, otherSource, err := st.CaptureWithKey(context.Background(), "cli", "k1", "2026-10-17", "From the CLI", &mood)
	s.K("the writer: one key under another source is another capture", err == nil && !otherSource && c.n("SELECT count(*) FROM capture_keys") == 3 && c.n("SELECT count(*) FROM measurements") == 3, err)
	capture("ui", "", "2026-10-17", "No key")
	capture("ui", "", "2026-10-17", "No key")
	s.K("the writer: two captures with no key still append twice, and keep no key", strings.HasSuffix(c.str("SELECT body FROM entities WHERE id=?", first), "No key\n\nNo key") &&
		c.n("SELECT count(*) FROM capture_keys") == 3 && c.n("SELECT count(*) FROM measurements") == 5)

	// the table's own rules, one probe each; a probe that a mutant lets through is rolled back
	insert := "INSERT INTO capture_keys(source,import_key,page_id,recorded_at) VALUES(?,?,?," + bounds0 + ")"
	s.K("capture_keys refuses a source that is not a writer's name (capture_keys_source)", strings.Contains(c.rolledBack(insert, "UI", "z", first), "capture_keys_source"))
	s.K("capture_keys refuses an instant without milliseconds (capture_keys_recorded_at)", strings.Contains(c.rolledBack("INSERT INTO capture_keys(source,import_key,page_id,recorded_at) VALUES('ui','z',?,'2026-10-17T10:00:00Z')", first), "capture_keys_recorded_at"))
	s.K("capture_keys refuses a key for a page that does not exist", strings.Contains(c.rolledBack(insert, "ui", "z", 999999), "FOREIGN KEY"))
	s.K("a capture key is unique per source: the same key twice under one source is refused", strings.Contains(c.rolledBack(insert, "ui", "k1", first), "UNIQUE"))
	other := c.capture("another day", "2026-10-18")
	s.K("...even for another page: the key names one capture", strings.Contains(c.rolledBack(insert, "ui", "k1", other), "UNIQUE"))
	s.K("...and one key under another source is another row", c.rolledBack(insert, "agent:probe", "k1", first) == "OK")
	s.K("an UPDATE of a capture key is refused (capture_keys_no_update)", strings.Contains(c.rolledBack("UPDATE capture_keys SET import_key='moved' WHERE import_key='k2'"), "append-only"))
	s.K("a DELETE of a capture key is refused (capture_keys_no_delete)", strings.Contains(c.rolledBack("DELETE FROM capture_keys WHERE import_key='k2'"), "never deleted"))
}

// periods: a period page (D30) is a page whose canonical key is a week, a month, a quarter or a year: entities.period
// names its shape, entities_period_page and entities_period_exists hold it, and it has the identity rules of a day page.
func periods(s *S) {
	c := s.fresh()
	for _, v := range []struct{ title, want string }{
		{"2026-W43", "week"}, {"2026-10", "month"}, {"2026-Q4", "quarter"}, {"2026", "year"}, {"2026-W53", "week"}, {"2020-W53", "week"},
		{"0000", "year"}, {"9999-12", "month"}, {"2026-W01", "week"}, {"2026-W52", "week"},
	} {
		id, err := c.tryIdentity("page", v.title, M{})
		s.K(fmt.Sprintf("the page %q is accepted and its period is %s", v.title, v.want), err == nil && c.str("SELECT period FROM entities WHERE id=?", id) == v.want && c.n("SELECT is_journal FROM entities WHERE id=?", id) == 0, err)
		if err == nil {
			s.K(fmt.Sprintf("...its key is %q and its day is NULL", text.TitleKey(v.title)), c.n("SELECT day IS NULL FROM entities WHERE id=?", id) == 1 && c.str("SELECT preferred_name_key FROM entities WHERE id=?", id) == text.TitleKey(v.title))
		}
	}
	s.K("a day page and a plain page have no period", c.n("SELECT period IS NULL FROM entities WHERE id=?", c.dayPage("2026-10-19", "")) == 1 && c.n("SELECT period IS NULL FROM entities WHERE id=?", c.page("Ferritin")) == 1)
	s.K("SELECT * returns the generated column period right after is_journal", c.col("SELECT name FROM pragma_table_xinfo('entities') ORDER BY cid")[5] == "period")

	// ---- a period that does not exist
	for _, bad := range []string{"2027-W53", "2021-W53", "2026-W00", "2026-W54", "2026-13", "2026-00", "2026-Q5", "2026-Q0", "2026-Q9", "2026-W99"} {
		_, err := c.tryIdentity("page", bad, M{})
		s.K(fmt.Sprintf("the period %q does not exist: entities_period_exists refuses it", bad), err != nil && strings.Contains(err.Error(), "entities_period_exists"), err)
	}
	// every year from 1990 to 2060: week 53 is accepted exactly when the Monday of that week is on or before 28 December
	wrong := []string{}
	w := s.fresh()
	for y := 1990; y <= 2060; y++ {
		key := fmt.Sprintf("%d-W53", y)
		_, err := w.tryIdentity("page", key, M{})
		if (err == nil) != (c.str("SELECT date(?||'-01-04','-6 days','weekday 1','+364 days') <= ?||'-12-28'", fmt.Sprint(y), fmt.Sprint(y)) == "1") {
			wrong = append(wrong, key)
		}
	}
	s.K("week 53 exists in a year exactly when its Monday is on or before 28 December (1990 to 2060)", len(wrong) == 0, wrong)

	// ---- a period page is a plain page without a day
	_, err := c.tryIdentity("page", "2026-W44", M{"day": "2026-10-26"})
	s.K("a period page with a day is refused (entities_period_page)", err != nil && strings.Contains(err.Error(), "entities_period_page"), err)
	_, err = c.tryIdentity("person", "2026", M{})
	s.K("a person titled 2026 is refused (entities_period_page)", err != nil && strings.Contains(err.Error(), "entities_period_page"), err)
	_, err = c.tryIdentity("metric", "2026-Q3", M{})
	s.K("a metric titled 2026-Q3 is refused (entities_period_page)", err != nil && strings.Contains(err.Error(), "entities_period_page"), err)
	year := c.str("SELECT id FROM entities WHERE preferred_name_key='2026'")
	s.K("a year page is not promoted to a person (entities_period_page)", strings.Contains(c.tryx("UPDATE entities SET entity_type='person' WHERE id=?", year), "entities_period_page"))

	// ---- one spelling, no alias, no rename, never filed
	month := c.str("SELECT id FROM entities WHERE preferred_name_key='2026-10'")
	week := c.str("SELECT id FROM entities WHERE preferred_name_key='2026-w43'")
	_, err = c.tryIdentity("page", "2026-w43b", M{})
	s.K("a title that only looks like a period is a plain page (2026-w43b)", err == nil)
	s.K("a lower-case spelling of a week is refused: the one spelling is 2026-W43", strings.Contains(func() string { _, e := c.tryIdentity("page", "2026-w45", M{}); return fmt.Sprint(e) }(), "a period page has only its canonical key name"))
	s.K("an alias on a month page is refused", strings.Contains(c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'October 2026','october 2026')", month), "a period page has only its canonical key name"))
	s.K("an alias that is another period key on a plain page is refused", strings.Contains(c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'2026-Q1','2026-q1')", c.page("Plain page")), "a period page has only its canonical key name"))
	s.K("a period key is not an alias of a period page", strings.Contains(c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'2026-Q1','2026-q1')", year), "a period page has only its canonical key name"))
	s.K("a week spelled in lower case cannot be set by an update", strings.Contains(c.tryx("UPDATE entity_names SET title='2026-w43' WHERE entity_id=?", week), "a period spelling is its canonical key"))
	s.K("a year page cannot be renamed", func() bool {
		other := c.page("Rename target")
		return strings.Contains(c.tryx("UPDATE entities SET preferred_name_key='rename target' WHERE id=?", year), "period identity cannot be renamed") && other != 0
	}())
	plain := c.page("Becomes a period")
	s.K("a plain page does not acquire a period key through its aliases", func() bool {
		c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Alias of it','alias of it')", plain)
		_, e := c.query("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'2028-W10','2028-w10')", plain)
		return e != nil && strings.Contains(c.tryx("UPDATE entities SET preferred_name_key='2028-w10' WHERE id=?", plain), "period identity cannot be renamed or acquired")
	}())
	filed, category := c.page("Filed page"), c.page("A category")
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'part-of',"+NOW+",'ui')", filed, category)
	// the entity row comes before its name row (the key's FK is deferred), so a page with no name yet is filed, or made
	// a category, and then selects a period key: only its part-of links can refuse that
	for _, x := range []struct {
		lbl  string
		link func(e int64) string
	}{
		{"a page with no name yet, filed in a category, cannot become a period page", func(e int64) string { return c.link(e, category, "part-of") }},
		{"a category with no name yet cannot become a period page", func(e int64) string { return c.link(filed, e, "part-of") }},
	} {
		c.must("BEGIN IMMEDIATE")
		e := c.n("INSERT INTO entities(entity_type,preferred_name_key,source,created_at,updated_at) VALUES ('page','plans 3','ui'," + NOW + "," + NOW + ") RETURNING id")
		linked := x.link(e)
		r := c.tryx("UPDATE entities SET preferred_name_key='2029-q1' WHERE id=?", e)
		s.K(x.lbl+" (D30)", linked == "OK" && strings.Contains(r, "never becomes a period page"), linked, r)
		c.must("ROLLBACK")
	}
	c.must("BEGIN IMMEDIATE")
	e := c.n("INSERT INTO entities(entity_type,preferred_name_key,source,created_at,updated_at) VALUES ('page','plans 4','ui'," + NOW + "," + NOW + ") RETURNING id")
	s.K("a page with no name yet and no part-of link becomes a period page, and its name follows",
		c.tryx("UPDATE entities SET preferred_name_key='2029-q1' WHERE id=?", e) == "OK" &&
			c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'2029-Q1','2029-q1')", e) == "OK" && c.str("SELECT period FROM entities WHERE id=?", e) == "quarter")
	c.must("ROLLBACK")

	s.K("a part-of link from a week page is refused (a period page is never filed)", strings.Contains(c.link(week, category, "part-of"), "a period page is never filed in a category"))
	s.K("a part-of link to a month page is refused (a category is never a period page)", strings.Contains(c.link(filed, month, "part-of"), "a category is a plain page, never a period page"))
	s.K("a part-of link to a year page is refused", strings.HasPrefix(c.link(filed, year, "part-of"), "ERR"))
	s.K("a wikilink from and to a period page works", c.link(week, month, "wikilink") == "OK" && c.link(c.page("Notes"), week, "wikilink") == "OK")

	// ---- a day page is as before
	_, err = c.tryIdentity("page", "2026-10-21", M{"day": "2026-10-21"})
	s.K("a day page is accepted, with its day, and has no period", err == nil && c.n("SELECT count(*) FROM entities WHERE preferred_name_key='2026-10-21' AND period IS NULL AND is_journal=1") == 1)
	s.K("a day page cannot hold a period spelling or key as its alias", strings.HasPrefix(c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'2026-W43','2026-w43x')", c.dayPage("2026-10-22", "")), "ERR"))
	s.K("the integrity query of the link day rules finds no period page in a part-of link", c.n(`SELECT count(*) FROM links l LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id
	 WHERE l.kind='part-of' AND (t.period IS NOT NULL OR f.period IS NOT NULL)`) == 0)
}
