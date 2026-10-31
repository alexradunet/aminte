package tests

import (
	"context"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"math/rand/v2"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"lifelog/text"
)

// The save contract (contract/titles-and-wikilinks, cookbook/save-a-body, D19). Its vectors live in one place, the
// table of contract/titles-and-wikilinks; the boundary cases under the table are computed here. The extraction is
// the writer's (text), every save is either the writer's own (internal/core) or the cookbook's SQL run
// literally, and both run against the DDL under test.

type vector struct {
	body string
	want []string
}

var (
	a240, a241 = strings.Repeat("a", 240), strings.Repeat("a", 241)
	j80, j81   = strings.Repeat("日", 80), strings.Repeat("日", 81)
	ss81, esz  = strings.Repeat("ss", 81), strings.Repeat("ẞ", 81)
)

// computed is the boundary vectors the line under the table states: 240 and 241 bytes, 80 and 81 CJK characters,
// and 81 x ẞ (243 bytes, invalid) beside 81 x ss, which links the ss spelling.
func computed() []vector {
	return []vector{
		{"[[" + a240 + "]]", []string{a240}}, {"[[" + a241 + "]]", nil},
		{"[[" + j80 + "]]", []string{j80}}, {"[[" + j81 + "]]", nil},
		{"[[" + esz + "]] [[" + ss81 + "]]", []string{ss81}},
	}
}

var (
	vectorTable = regexp.MustCompile(`\| body \| links to.*?\n\s*\|---\|---\|\n((?:\s*\|.*\n)+)`)
	vectorRow   = regexp.MustCompile("^\\| (``? .*? ``?|`[^`]*`) \\| (.*) \\|$")
	uEscape     = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)
	codeSpanRE  = regexp.MustCompile("`([^`]*)`")
)

// unesc applies the page's escapes: \| \n \r \t and \uXXXX.
func unesc(t string) string {
	t = strings.NewReplacer(`\|`, "|", `\n`, "\n", `\r`, "\r", `\t`, "\t").Replace(t)
	return uEscape.ReplaceAllStringFunc(t, func(m string) string {
		n, _ := strconv.ParseUint(m[2:], 16, 32)
		return string(rune(n))
	})
}

// printedVectors is the vector table of contract/titles-and-wikilinks, and the rows that do not parse.
func (s *S) printedVectors() (rows []vector, bad []string) {
	m := vectorTable.FindStringSubmatch(s.d.Page("contract/titles-and-wikilinks.md"))
	if m == nil {
		return nil, []string{"no vector table"}
	}
	for _, line := range strings.Split(strings.TrimRight(m[1], "\n"), "\n") {
		mm := vectorRow.FindStringSubmatch(strings.TrimSpace(line))
		if mm == nil {
			bad = append(bad, line)
			continue
		}
		body := mm[1]
		if strings.HasPrefix(body, "``") {
			body = body[3 : len(body)-3]
		} else {
			body = body[1 : len(body)-1]
		}
		var want []string
		if mm[2] != "—" {
			for _, t := range codeSpanRE.FindAllStringSubmatch(mm[2], -1) {
				want = append(want, unesc(t[1]))
			}
		}
		rows = append(rows, vector{unesc(body), want})
	}
	return
}

func targets(body string) []string { _, t, _ := text.Targets(body, ""); return t }

// docSaveContract: the save contract as the DOCUMENT prints it.
// A  the vector table of contract/titles-and-wikilinks reproduces with the writer's extraction, row for row, and the
//
//	boundary cases under it hold;
//
// B  the SQL of cookbook/save-a-body, run literally statement by statement, gives the vector results, leaves no
//
//	orphan, carries ids by RETURNING, and equals the writer's own save after 400 random edits;
//
// C  cookbook/backlinks lists a day page's wikilink and not a stub's redirect row;
// D  a fault of SAVEPOINT or RELEASE fails the save (docSaveFault).
func docSaveContract(s *S) {
	// ---- A  the table of contract/titles-and-wikilinks
	rows, bad := s.printedVectors()
	s.K("A every row of the contract table parses, and there are at least 60", len(bad) == 0 && len(rows) >= 60, len(rows), bad)
	var wrong []string
	for _, v := range rows {
		if got := targets(v.body); !slices.Equal(got, v.want) {
			wrong = append(wrong, fmt.Sprintf("%q: got %q, want %q", v.body, got, v.want))
		}
	}
	s.K("A every printed vector reproduces with the writer's extraction", len(rows) > 0 && len(wrong) == 0, wrong)
	s.K("A 81 x ẞ beside 81 x ss (243 bytes, invalid) links ss, as the line under the table says", slices.Equal(targets("[["+esz+"]] [["+ss81+"]]"), []string{ss81}))
	s.K("A the 240/241-byte and 80/81-CJK boundary under the table holds", len(targets("[["+a240+"]]")) == 1 && len(targets("[["+a241+"]]")) == 0 &&
		len(targets("[["+j80+"]]")) == 1 && len(targets("[["+j81+"]]")) == 0)

	// ---- B  cookbook/save-a-body run literally
	st, ok := s.saveSteps()
	s.K("B cookbook/save-a-body has exactly one statement of each step", ok, st.counts)
	sts := statements(s.d.Block("save-a-body"))
	index := func(x string) int {
		for i, y := range sts {
			if y == x {
				return i
			}
		}
		return -1
	}
	s.K("B cookbook/save-a-body opens with BEGIN IMMEDIATE and resolves inside the transaction", ok && len(sts) > 0 && strings.HasPrefix(strings.ToUpper(code(sts[0])), "BEGIN IMMEDIATE") &&
		index(st.sel) < index(st.ent) && index(st.ent) < index(st.commit))
	s.K("B no cookbook/save-a-body statement uses last_insert_rowid()", !strings.Contains(s.d.Block("save-a-body"), "last_insert_rowid"))
	if ok {
		docPage := func(c *C, body, title string) int64 {
			c.must(st.begin)
			pid := c.pageW(title, nil, body)
			c.docSave(st, pid, body, text.TitleKey(title), true)
			c.must(st.commit)
			return pid
		}
		docEdit := func(c *C, pid int64, body string) {
			c.must(st.begin)
			c.must("UPDATE entities SET body=? WHERE id=?", body, pid)
			c.docSave(st, pid, body, c.str("SELECT preferred_name_key FROM entities WHERE id=?", pid), true)
			c.must(st.commit)
		}
		all := append(rows, computed()...)
		okv, orph := true, int64(0)
		var differs []string
		for _, v := range all {
			c := s.fresh()
			pid := docPage(c, v.body, "Vector body")
			orph += c.n("select count(*) from entities e where not exists (select 1 from entity_names n where n.entity_id=e.id AND n.name_key=e.preferred_name_key)")
			if got := c.linksOf(pid); !slices.Equal(got, slices.Sorted(slices.Values(v.want))) {
				okv = false
				differs = append(differs, fmt.Sprintf("%q: %q", v.body, got))
			}
			c.Close()
		}
		s.K(fmt.Sprintf("B the document's SQL, run literally, gives the vector result for all %d vectors", len(all)), okv, differs)
		s.K("B ...and leaves no orphan entities row", orph == 0)
		c := s.fresh()
		c.must("BEGIN IMMEDIATE")
		eid := c.rows(st.ent, P{"source": "ui", "key": "zed", "title": "Zed"})[0][0]
		c.must(st.pg, P{"title": "Zed", "key": "zed", "target_id": eid})
		c.must("COMMIT")
		s.K("B the id step 2b RETURNs is the new page's id", c.one("select entity_id from entity_names where title='Zed'") == eid)
		frags := []string{"[[Alpha]]", "[[alpha|a]]", "#beta", "`[[code]]`", "[[Bad/Name]]", "~~~\n[[fence]]\n~~~", "[[Ünï]]", "[[UNÏ]]", "text", "#Beta",
			"[[Gamma delta]]", "#12", "[[CON]]", "#REDIRECTED", "#REDIRECT [[Alpha]]"}
		rng := rand.New(rand.NewPCG(10, 10))
		ca, cb := s.fresh(), s.fresh()
		var pa, pb []int64
		for i := range 6 {
			pa = append(pa, docPage(ca, "seed", fmt.Sprintf("Seed %d", i)))
			id, _ := cb.savePage("seed", fmt.Sprintf("Seed %d", i))
			pb = append(pb, id)
		}
		for range 400 {
			i := rng.IntN(6)
			var parts []string
			for range rng.IntN(7) {
				parts = append(parts, frags[rng.IntN(len(frags))])
			}
			body := strings.Join(parts, " ")
			docEdit(ca, pa[i], body)
			cb.editBody(pb[i], body)
		}
		same := true
		for i := range pa {
			same = same && slices.Equal(ca.linksOf(pa[i]), cb.linksOf(pb[i]))
		}
		s.K("B the document's SQL equals the writer's own save after 400 random edits", same)
		keys := "select name_key from entity_names order by 1"
		s.K("B ...with the same set of pages", slices.Equal(ca.col(keys), cb.col(keys)))
	}

	// ---- C  retained names share one backlink endpoint.
	c := s.fresh()
	old := c.page("Diet")
	mm := c.dayPage("2026-09-30", "[[Diet]]")
	dd := c.dayPage("2026-09-29", "[[Diet]]")
	c.link(mm, old, "wikilink")
	c.link(dd, old, "wikilink")
	if _, err := c.store().Rename(context.Background(), "ui", old, "Diet plan"); err != nil {
		stop("rename backlink fixture: %v", err)
	}
	r := c.rows(s.d.Block("backlinks"), P{"page_id": old})
	var got []string
	for _, x := range r {
		got = append(got, val(x[0])+"|"+val(x[2]))
	}
	s.K("C cookbook/backlinks keeps old-name mentions on the same identity", slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values([]string{"wikilink|" + ids(mm), "wikilink|" + ids(dd)}))), tab(r))

	// ---- D  a fault of a savepoint fails the save
	s.K("D cookbook/save-a-body says that an error of SAVEPOINT, ROLLBACK TO or RELEASE fails the whole save",
		strings.Contains(strings.Join(strings.Fields(s.d.Page("cookbook/save-a-body.md")), " "), "An error of `SAVEPOINT`, of `ROLLBACK TO` or of `RELEASE` fails the whole save"))
	if ok {
		docSaveFault(s, st)
	}
}

// docSaveFault: cookbook/save-a-body run literally on a full file. A fault of SAVEPOINT, and a fault of the RELEASE
// after a target that succeeded, each fail the save: SQLite has rolled the transaction back, and the page keeps its
// body and its links. A save that goes on after the fault writes outside a transaction and loses the page's links
// (D19). The method is the one of the writer's own test of the fault. VACUUM removes the free pages, and
// max_page_count then lets the file grow by 16 pages only. FTS5 keeps the terms of a write in memory. It writes them
// to the file when a savepoint opens, and when a savepoint closes after a statement in it opened its own savepoint.
// The terms of 10,000 different words need more than 16 pages. A row that keeps its size is written in place and
// needs no new page, so each page is saved once more in capitals first, and the save under test writes a body of
// the same length. In the RELEASE case, a TEMP trigger (deliberate damage) writes the long body of Big again with
// other terms when the save links New: the last statement of the target, so the terms wait for its RELEASE. The
// witness needs the FTS5 of the driver's SQLite (tests/README.md); each label also holds that the fault came.
func docSaveFault(s *S, st saveSteps) {
	var long strings.Builder // 10,000 different words
	for i := range 10000 {
		fmt.Fprintf(&long, " c%05d", i)
	}
	for _, tc := range []struct {
		name, home, save, trigger string // home is the body of Home before the save
	}{
		{"SAVEPOINT", "[[Old]]" + long.String(), "[[New]]" + strings.ReplaceAll(long.String(), "c", "b"), ""},
		{"RELEASE", "[[Old]]", "[[New]]", `CREATE TEMP TRIGGER rewrite_big AFTER INSERT ON links WHEN NEW.kind = 'wikilink'
		    BEGIN UPDATE entities SET body = replace(body, 'C', 'D') WHERE preferred_name_key = 'big'; END`},
	} {
		c := s.freshWith(F{Path: filepath.Join(s.dir, "fault-"+tc.name+".db")})
		// save is the recipe, literally: the body (step 0), the links (steps 1-4) and the COMMIT.
		save := func(id int64, body string) string {
			return try(func() {
				c.must(st.begin)
				c.must("UPDATE entities SET body=? WHERE id=?", body, id)
				c.docSave(st, id, body, c.str("SELECT preferred_name_key FROM entities WHERE id=?", id), true)
				c.must(st.commit)
			})
		}
		page := func(title, body string) int64 {
			c.must(st.begin)
			id := c.pageW(title, nil, "")
			c.must(st.commit)
			if out := save(id, body); out != "OK" {
				stop("fault fixture %s: %s", title, out)
			}
			if out := save(id, strings.ToUpper(body)); out != "OK" {
				stop("fault fixture %s: %s", title, out)
			}
			return id
		}
		page("Big", long.String())
		home := page("Home", tc.home)
		if tc.trigger != "" {
			c.must(tc.trigger)
		}
		limit := c.str("PRAGMA max_page_count")
		c.must("VACUUM")
		c.must(fmt.Sprintf("PRAGMA max_page_count = %d", c.n("PRAGMA page_count")+16))
		kept := func() bool {
			return c.str("SELECT body FROM entities WHERE id=?", home) == strings.ToUpper(tc.home) && slices.Equal(c.linksOf(home), []string{"Old"}) &&
				c.n("SELECT count(*) FROM entity_names WHERE name_key='new'") == 0
		}

		out := save(home, tc.save)
		ended := c.tryx("ROLLBACK") // the save fails: the writer rolls back, and SQLite has done it already
		s.K("D a full file at "+tc.name+": the save fails, and SQLite has rolled the transaction back",
			strings.Contains(out, "database or disk is full") && strings.Contains(ended, "no transaction is active"), clip(out, 120), ended)
		s.K("D ...and the page keeps its body and its link, and no page New is made ("+tc.name+")", kept(), c.linksOf(home))

		// a save that goes on after the fault, as if the target were only skipped: step 4 and COMMIT
		out = try(func() {
			c.must(st.begin)
			c.must("UPDATE entities SET body=? WHERE id=?", tc.save, home)
			c.docSave(st, home, tc.save, "home", true)
		})
		dele := c.tryx(st.dele, P{"page_id": home, "target_ids": "[]"})
		commit := c.tryx(st.commit)
		s.K("D a save that goes on after the fault at "+tc.name+" writes outside a transaction: step 4 commits alone, and the page loses its link",
			strings.Contains(out, "database or disk is full") && dele == "OK" && strings.Contains(commit, "no transaction is active") &&
				c.str("SELECT body FROM entities WHERE id=?", home) == strings.ToUpper(tc.home) && len(c.linksOf(home)) == 0,
			clip(out, 120), dele, commit, c.linksOf(home))
		c.must("PRAGMA max_page_count = " + limit)
		c.Close()
	}
}

// saveContract: the save contract through the writer's own save, against the DDL under test. Expected outcome in each label.
func saveContract(s *S) {
	orphans := func(c *C) int64 {
		return c.n("SELECT count(*) FROM entities e WHERE entity_type='page' AND NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id=e.id AND n.name_key=e.preferred_name_key)")
	}

	// P1 valid and invalid targets in one page: the page IS saved, valid links made, invalid ones skipped
	c := s.fresh()
	pid, r := c.savePage("Diet: [[Health/Diet]], [[Re: plan]], [[Target|alias]], [[Good page]], #health #con #C", "")
	s.K("P1a the page is saved although two targets are invalid", c.n("select count(*) from entities where id=?", pid) == 1)
	s.K("P1b links = C, Good page, Target, health (#C is a valid one-letter tag)", slices.Equal(c.linksOf(pid), []string{"C", "Good page", "Target", "health"}), c.linksOf(pid))
	s.K("P1c skipped (reported, not stored): Health/Diet, Re: plan, con", slices.Equal(slices.Sorted(slices.Values(r.Skipped)), []string{"Health/Diet", "Re: plan", "con"}), r.Skipped)
	s.K("P1d no orphan entities row, DB consistent", orphans(c) == 0 && c.integrityOK())
	s.K("P1e the body keeps the text verbatim", strings.HasPrefix(c.str("select body from entities where id=?", pid), "Diet: [[Health/Diet]]"))

	// P2 backstop: with the title predicate skipped the save survives, and leaves no orphan (cookbook/save-a-body's SAVEPOINT)
	if st, ok := s.saveSteps(); ok {
		c = s.fresh()
		var skipped []string
		var p2 int64
		out := try(func() {
			c.must(st.begin)
			p2 = c.pageW("Backstop", nil, "x [[Health/Diet]] [[fine]]")
			_, skipped = c.docSave(st, p2, "x [[Health/Diet]] [[fine]]", "backstop", false)
			c.must(st.commit)
		})
		s.K("P2a predicate off: the page is still committed", out == "OK" && c.n("select count(*) from entities where id=?", p2) == 1, out)
		s.K("P2b predicate off: bad target skipped, good target linked", slices.Equal(c.linksOf(p2), []string{"fine"}) && slices.Contains(skipped, "Health/Diet"), c.linksOf(p2), skipped)
		s.K("P2c predicate off: no orphan entities row", orphans(c) == 0, orphans(c))
	} else {
		s.K("P2 cookbook/save-a-body has one statement of each step", false, st.counts)
	}

	// P3 sync = set equality: adding, removing, re-saving
	c = s.fresh()
	pid, _ = c.savePage("[[A]] [[B]] #t", "")
	s.K("P3a initial links A, B, t", slices.Equal(c.linksOf(pid), []string{"A", "B", "t"}))
	c.editBody(pid, "[[A]] [[C]]")
	s.K("P3b edit: B and t dropped, C added", slices.Equal(c.linksOf(pid), []string{"A", "C"}), c.linksOf(pid))
	n1 := c.tab("select count(*), max(id) from links")
	c.editBody(pid, "[[A]] [[C]]")
	n2 := c.tab("select count(*), max(id) from links")
	s.K("P3c re-saving the same body changes nothing (idempotent, no new rows)", n1 == n2, n1, n2)
	c.editBody(pid, "no links at all")
	s.K("P3d body without links: all wikilinks removed", len(c.linksOf(pid)) == 0)
	s.K("P3e dropped targets survive as pages (and may become ghosts, cookbook/ghost-pages)", c.n("select count(*) from entities where id<>? and entity_type = 'page'", pid) == 4)

	// P4 self link, stub
	c = s.fresh()
	c.must("BEGIN IMMEDIATE")
	wid := c.page("Diet")
	c.must("COMMIT")
	c.editBody(wid, "About [[Diet]] and [[diet]] and [[Food]]")
	s.K("P4a a page never links to itself", slices.Equal(c.linksOf(wid), []string{"Food"}), c.linksOf(wid))
	c.must("BEGIN IMMEDIATE")
	old := c.pageW("Old diet", nil, "[[Food]] [[Stuff]]")
	c.must("COMMIT")
	c.editBody(old, "[[Food]] [[Stuff]]")
	nBefore := len(c.linksOf(old))
	c.editBody(old, "#REDIRECT [[Diet]]")
	s.K("P4b REDIRECT is literal tag/text, not a stub", nBefore == 2 && slices.Equal(c.linksOf(old), []string{"Diet", "REDIRECT"}), c.linksOf(old))
	s.K("P4c redirect edges are not registered", strings.HasPrefix(c.link(old, wid, "redirect"), "ERR"))

	// P5 tombstoned target is revived, not duplicated
	c = s.fresh()
	pid, _ = c.savePage("[[Junk]]", "")
	jid := c.n("select entity_id from entity_names where title='Junk'")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", jid)
	pid2, _ := c.savePage("again [[JUNK]]", "")
	s.K("P5a one page, revived", c.n("select count(*) from entity_names where name_key='junk'") == 1 && c.str("select deleted_at from entities where id=?", jid) == "None")
	s.K("P5b both pages link to it", slices.Equal(c.linksOf(pid), []string{"Junk"}) && slices.Equal(c.linksOf(pid2), []string{"Junk"}))

	// P6 incremental == rebuild, over random edit sequences
	frags := []string{"[[Alpha]]", "[[alpha|a]]", "#beta", "`[[code]]`", "[[Bad/Name]]", "```\n[[fence]]\n```", "[[Ünï]]", "[[UNÏ]]", "text", "#Beta", "[[Gamma delta]]", "#12", "[[CON]]", "#REDIRECTED"}
	rng := rand.New(rand.NewPCG(6, 6))
	c = s.fresh()
	var pgs []int64
	for range 6 {
		id, _ := c.savePage("seed", "")
		pgs = append(pgs, id)
	}
	for range 400 {
		var parts []string
		for range rng.IntN(7) {
			parts = append(parts, frags[rng.IntN(len(frags))])
		}
		c.editBody(pgs[rng.IntN(len(pgs))], strings.Join(parts, " "))
	}
	c2 := s.fresh()
	same := true
	for _, p := range pgs {
		q, _ := c2.savePage(c.str("select body from entities where id=?", p), "")
		same = same && slices.Equal(c.linksOf(p), c2.linksOf(q))
	}
	s.K("P6a incremental maintenance == rebuild from bodies (6 pages, 400 random edits)", same)
	s.K("P6b no orphans, integrity ok after 400 edits", orphans(c) == 0 && c.integrityOK())
	s.K("P6c at most one page per title_key", c.n("select count(*) from (select name_key from entity_names group by 1 having count(*)>1)") == 0)

	// P7 four real writers save pages with the same new tag and target at the same time
	path := filepath.Join(s.dir, "p7.db")
	c0 := s.freshWith(F{Path: path})
	c0.must("PRAGMA journal_mode=WAL")
	c0.Close()
	var mu sync.Mutex
	var errs []string
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out := try(func() { s.writerConn(path).savePage(fmt.Sprintf("writer %d about [[Shared topic]] #shared", i), "") }); out != "OK" {
				mu.Lock()
				errs = append(errs, out)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	cv := s.writerConn(path)
	s.K("P7a four concurrent saves: no errors", len(errs) == 0, errs)
	s.K("P7b one page per shared target, four pages linked to each", cv.n("select count(*) from entity_names where name_key in ('shared topic','shared')") == 2 &&
		cv.n("select count(*) from links where kind='wikilink'") == 8)

	// P8 the vectors, end to end through the database (links == vector result); P9 the extraction alone
	rows, _ := s.printedVectors()
	all := append(rows, computed()...)
	okDB, okText := len(rows) > 0, len(rows) > 0
	var differs []string
	for _, v := range all {
		c := s.fresh()
		pid, _ := c.savePage(v.body, "")
		if got := c.linksOf(pid); !slices.Equal(got, slices.Sorted(slices.Values(v.want))) {
			okDB = false
			differs = append(differs, fmt.Sprintf("%q: %q", v.body, got))
		}
		c.Close()
		okText = okText && slices.Equal(targets(v.body), v.want)
	}
	s.K(fmt.Sprintf("P8 all %d vectors give the same links through a real save", len(all)), okDB, differs)
	s.K("P9 extraction vectors", okText)
}

// titleFuzz: the writer's title predicate against the DDL's own CHECKs on the same strings. No string the writer
// accepts may be refused by the DB (that would block a save); and, because the predicate mirrors the CHECKs one for
// one, none the other way either, except unassigned code points (category Cn), which only the writer can recognise
// — the alphabet below holds none, and the pages suite tests that difference on its own.
func titleFuzz(s *S) {
	alpha := []string{"a", "b", "c", "X", "Y", "Z", "0", "1", "9", " ", ".", "-", "_", "#", "/", "\\", ":", "*", "?", "\"", "<", ">", "|", "\u0001", "\u0009", "\u000a", "\u001f", "\u007f",
		"\u00a0", "\u3000", "\u2003", "é", "É", "ß", "日", "\U0001f600", "İ", "ſ", "K", "\u0301", "\u200b", "\u202e", "\u00ad", "\ufeff", "\u0085", "\u009f",
		"\u00a0", "\u200c", "\u200d", "\u2066", "\u061c", "\u2060", "\ufe0f"}
	words := []string{"COM¹", "LPT³", "LPT².txt", "con.backup", "Nul.tar.gz", "COM1.x", "CONX.txt", "a.CON", "a.con.b", ".con", "CON.", "COM10.txt", "CON .txt",
		"CON\u00a0.txt", "CON", "con", "Nul", "PRN", "aux", "COM1", "com9", "LPT1", "lpt9", "COM0", "LPT10", "CONSOLE", "NUL.txt", "CON.backup", ".", "..", "...", "a.", ".a",
		"a b", "a  b", " a", "a ", "a\u00a0", "\u00a0a"}
	rng := rand.New(rand.NewPCG(8, 8))
	pick := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(alpha[rng.IntN(len(alpha))])
		}
		return b.String()
	}
	rnd := func() string {
		switch r := rng.Float64(); {
		case r < .15:
			return words[rng.IntN(len(words))]
		case r < .30:
			return pick(1 + rng.IntN(6))
		case r < .40: // the length boundary, mixed byte widths
			n, t := 236+rng.IntN(9), ""
			for len(t) < n {
				t += []string{"a", "é", "日", "😀"}[rng.IntN(4)]
			}
			return t
		}
		return pick(1 + rng.IntN(12))
	}
	cases := append([]string{}, words...)
	cases = append(cases, a240, a241, j80, j81, strings.Repeat("😀", 60), strings.Repeat("😀", 61), strings.Repeat("é", 120), strings.Repeat("é", 121))
	for range 60000 {
		cases = append(cases, rnd())
	}
	c := s.fresh()
	ins, err := c.Prepare("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,?,?)")
	if err != nil {
		stop("prepare: %v", err)
	}
	defer ins.Close()
	seen := map[string]bool{}
	var block, loose []string
	unexplained := 0
	accepted := 0
	for _, t := range cases {
		if seen[t] {
			continue
		}
		seen[t] = true
		app := text.ValidTitle(t)
		c.must("SAVEPOINT f")
		key := text.TitleKey(t)
		owners, e := c.query("INSERT INTO entities(entity_type,preferred_name_key,day,created_at,updated_at,source) VALUES ('page',?,CASE WHEN date(?) IS ? THEN ? END,"+NOW+","+NOW+",'ui') RETURNING id", key, t, t, t)
		if e == nil {
			_, e = ins.Exec(owners[0][0], t, key)
		}
		c.must("ROLLBACK TO f")
		c.must("RELEASE f")
		db := e == nil
		if db {
			accepted++
		}
		if app && !db {
			block = append(block, fmt.Sprintf("%q (%v)", clip(t, 30), e))
		}
		if db && !app {
			loose = append(loose, fmt.Sprintf("%q", clip(t, 30)))
			addressable := true
			for _, spelling := range []string{t, norm.NFC.String(t)} {
				for _, body := range []string{"See [[" + spelling + "]].", "See ![[" + spelling + "]].", "See [[" + spelling + "|display]]."} {
					candidates := text.Candidates(body)
					if len(candidates) != 1 || text.TitleKey(candidates[0]) != text.TitleKey(t) {
						addressable = false
					}
				}
			}
			if addressable {
				unexplained++
			}
		}
	}
	s.K("more than 40 000 distinct strings, some accepted by the DB and some refused", len(seen) > 40000 && accepted > 0 && accepted < len(seen), len(seen), accepted)
	s.K("no string the writer accepts is refused by the DB (that would block a save)", len(block) == 0, len(block), block[:min(5, len(block))])
	s.K("DB-only generated names fail the contract addressability forms, not an unexplained filename rule", unexplained == 0, len(loose), loose[:min(8, len(loose))])
}
