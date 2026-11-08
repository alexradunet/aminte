package tests

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"aminte/internal/core"
)

// files: a file the owner keeps is a page (schema.sql, D9) — one identity with owned names and a
// files row: the hash and type of the original, never stored, and a small JPEG.
// A  the keys and CHECKs: sha256, mime, preview (and why IS, not =), what never changes, never deleted;
// B  the graph: an embed lands on a file, a caption links out, part-of, refused redirect; a ghost becomes a file;
// C  cookbook/keep-a-file as a writer runs it: create, the same original again, a missing preview, a tombstone,
//
//	another source, a promoted ghost, the refusals; the pictures a day shows.
func files(s *S) {
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }
	hash := func(b byte) string { return strings.Repeat(string("0123456789abcdef"[b%16]), 64) }
	file := func(c *C, title string, cols M) string { // a file page (title "" = a fresh one) and its files row: OK or ERR
		if title == "" {
			title = "File " + val(c.s.next())
		}
		id := c.identity("file", "ui", title, nil, "")
		m := M{"id": id, "sha256": fmt.Sprintf("%064x", 1000+id), "mime": "image/jpeg"}
		for k, v := range cols {
			m[k] = v
		}
		cs, marks, vals := m.split()
		return c.tryx("INSERT INTO files("+strings.Join(cs, ",")+") VALUES ("+marks+")", vals...)
	}

	// ---- A  structure
	c := s.fresh()
	f := c.named("file", "Lake.jpg")
	s.K("a file is one id: entities, a page of type file and a files row",
		c.tab("select e.entity_type, f.entity_type, n.title from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key join files f on f.id=e.id where e.id=?", f) == "file|file|Lake.jpg")
	s.K("a files row needs a page of type file", err(c.tryx("INSERT INTO files(id,sha256,mime) VALUES (?,?,?)", c.page("Plain"), hash(1), "image/jpeg")))
	s.K("...and cannot claim another type to hang off a person's page",
		err(c.tryx("INSERT INTO files(id,entity_type,sha256,mime) VALUES (?,'person',?,?)", c.named("person", "Ann"), hash(2), "image/jpeg")))
	s.K("files_entity_type: a files row is of type file", strings.Contains(c.tryx("INSERT INTO files(id,entity_type,sha256,mime) VALUES (?,'page',?,?)", c.page("P2"), hash(3), "image/jpeg"), "files_entity_type"))
	cols := c.col("select name from pragma_table_info('files')")
	s.K("files holds the hash, the type and the preview, and no path, size or name of the original",
		slices.Equal(cols, []string{"id", "entity_type", "sha256", "mime", "preview"}), cols)

	// sha256
	c = s.fresh()
	for _, v := range []string{hash(4), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		s.K("sha256 "+clip(v, 12)+": 64 lowercase hex digits accepted", file(c, "A"+v[:8], M{"sha256": v}) == "OK")
	}
	for _, v := range []any{strings.ToUpper(hash(10)), hash(5)[:63], hash(5) + "0", strings.Repeat("g", 64), "", nil} {
		s.K("sha256 "+clip(val(v), 12)+" refused", err(file(c, "", M{"sha256": v})))
	}
	s.K("one page per original: a second files row with the same sha256 is refused (files_sha256)",
		strings.Contains(file(c, "Twice", M{"sha256": hash(4)}), "UNIQUE"))

	// mime
	for _, v := range []string{"audio/mp4", "image/heic", "application/pdf", "audio/x-m4a", "image/svg+xml",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document"} {
		s.K("mime "+v+" accepted", file(c, "", M{"mime": v}) == "OK")
	}
	for _, v := range []string{"Image/JPEG", "jpeg", "text/plain; charset=utf-8", "a/b/c", "image/", "/jpeg", "",
		"image/" + strings.Repeat("x", 122)} {
		s.K("mime "+clip(v, 24)+" refused", err(file(c, "", M{"mime": v})))
	}

	// preview
	big := func(n int) []byte { return append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, n-3)...) }
	s.K("preview: a JPEG accepted", file(c, "P1", M{"preview": jpegBytes}) == "OK")
	s.K("preview: none (a recording, a PDF) accepted", file(c, "P2", M{"preview": nil}) == "OK")
	s.K("preview: a PNG refused", err(file(c, "P3", M{"preview": []byte{0x89, 'P', 'N', 'G', 13, 10, 26, 10}})))
	s.K("preview: text refused by the STRICT column, even text with the JPEG bytes", err(file(c, "P4", M{"preview": "\xff\xd8\xffhello"})))
	s.K("preview: 1 MB accepted, 1 MB and a byte refused", file(c, "P5", M{"preview": big(1048576)}) == "OK" && err(file(c, "P6", M{"preview": big(1048577)})))
	s.K("preview: an empty blob refused", err(file(c, "P7", M{"preview": []byte{}})))
	s.K("why IS, not =: substr of an empty blob is NULL, so = makes the CHECK NULL (a pass) and IS makes it 0",
		c.tab("select typeof(substr(x'', 1, 3)), typeof(substr(x'', 1, 3) = x'FFD8FF'), substr(x'', 1, 3) IS x'FFD8FF'") == "null|null|0")

	// what never changes, never deleted
	c = s.fresh()
	file(c, "Memo.m4a", M{"mime": "audio/mp4"})
	id := c.n("select id from files")
	s.K("files.sha256 cannot change", strings.Contains(c.tryx("UPDATE files SET sha256=? WHERE id=?", hash(9), id), "never change"))
	s.K("files.mime cannot change", strings.Contains(c.tryx("UPDATE files SET mime='audio/mpeg' WHERE id=?", id), "never change"))
	s.K("a no-op full-row update passes", c.tryx("UPDATE files SET sha256=sha256, mime=mime WHERE id=?", id) == "OK")
	c.must("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z' WHERE id=?", id)
	s.K("a missing preview may be added", c.tryx("UPDATE files SET preview=? WHERE id=?", jpegBytes, id) == "OK")
	s.K("adding it bumps entities.updated_at (files_touch)", c.n("select updated_at > created_at from entities where id=?", id) == 1)
	s.K("a file is never deleted", err(c.tryx("DELETE FROM files WHERE id=?", id)) && err(c.tryx("DELETE FROM entity_names WHERE entity_id=?", id)))

	renamed, renameErr := c.store().Rename(context.Background(), "ui", id, "Memo 2.m4a")
	s.K("a file rename retains its identity and owned old handle", renameErr == nil && renamed == id && c.n("SELECT count(*) FROM entity_names WHERE entity_id=?", id) == 2, renameErr)
	s.K("...it is tombstoned", c.tryx("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", id) == "OK")

	s.K("integrity and foreign keys clean", c.integrityOK())

	// ---- B  the graph
	c = s.fresh()
	f = c.named("file", "2026-06-01 Lake.jpg")
	day := c.dayPage("2026-06-01", "![[2026-06-01 Lake.jpg]]")
	bob := c.named("person", "Bob Sample")
	s.K("an embed in a day page lands on the file page (a wikilink)", c.link(day, f, "wikilink") == "OK")
	s.K("a caption that names a person links it from the file page", c.link(f, bob, "wikilink") == "OK")
	s.K("a file may be filed in a category", c.link(f, c.page("Scans"), "part-of") == "OK")
	s.K("a file may be filed in a category page (an album)", c.link(f, c.page("Holidays"), "part-of") == "OK")
	s.K("redirect is not a registered kind", err(c.link(c.page("Lake photo"), f, "redirect")))
	sd, _ := c.savePage("![[2026-06-01 Lake.jpg|the lake]] and ![[Unseen.png]]", "")
	s.K("the writer's save links an embed to the file page, and makes a ghost of a file not kept yet",
		slices.Equal(c.linksOf(sd), []string{"2026-06-01 Lake.jpg", "Unseen.png"}) && c.str("select e.entity_type from entities e join entity_names n on n.entity_id=e.id where n.name_key='unseen.png'") == "page", c.linksOf(sd))
	c.must("UPDATE entities SET body='The lake at dawn' WHERE id=?", f)
	s.K("search finds a file's text", c.n("select count(*) from entities_fts where entities_fts match 'dawn' and rowid=?", f) == 1)

	// promotion: a ghost an embed made becomes the file
	g := c.n("select entity_id from entity_names where name_key='unseen.png'")
	r1 := c.tryx("UPDATE entities SET entity_type='file' WHERE id=? AND entity_type='page'", g)
	r2 := c.tryx("INSERT INTO files(id,sha256,mime) VALUES (?,?,'image/png')", g, hash(11))
	s.K("a ghost page becomes a file: the UPDATE cascades to its page, then the files row", r1 == "OK" && r2 == "OK" &&
		c.tab("select e.entity_type, f.entity_type from entities e join files f using(id) where id=?", g) == "file|file", r1, r2)
	s.K("...and the embed that made it now lands on the file (the id did not change)", slices.Contains(c.linksOf(sd), "Unseen.png") &&
		c.n("select count(*) from links where from_id=? and to_id=?", sd, g) == 1)
	s.K("a file cannot be turned back into a plain page (entities_promote_from_page)", strings.Contains(c.tryx("UPDATE entities SET entity_type='page' WHERE id=?", g), "only a plain page is promoted"))
	s.K("a day page cannot become a file (entities_day_page_plain)", strings.Contains(c.tryx("UPDATE entities SET entity_type='file' WHERE id=?", day), "entities_day_page_plain"))
	empty := c.named("file", "Empty.pdf")
	s.K("ghost_pages never lists a file page, however empty and alone", c.n("select count(*) from ghost_pages where id=?", empty) == 0)
	s.K("integrity and foreign keys clean", c.integrityOK())

	// ---- C  cookbook/keep-a-file as a writer runs it
	blocks := regexp.MustCompile("(?s)```sql\n(.*?)```").FindAllStringSubmatch(s.d.Page("cookbook/keep-a-file.md"), -1)
	if len(blocks) != 2 {
		stop("cookbook/keep-a-file has %d SQL blocks, not 2", len(blocks))
	}
	sts := statements(blocks[0][1])
	firstWord := regexp.MustCompile(`\w+`)
	var words []string
	for _, st := range sts {
		words = append(words, strings.ToUpper(firstWord.FindString(code(st))))
	}
	if strings.Join(words, " ") != "BEGIN SELECT UPDATE SELECT INSERT INSERT UPDATE UPDATE UPDATE INSERT COMMIT" {
		stop("cookbook/keep-a-file: the write is not BEGIN, the two steps of 0, the resolve, 1a, 1b, the files row, COMMIT: %v", words)
	}
	begin, look, fill, resolve, commit := sts[0], sts[1], sts[2], sts[3], sts[10]
	promote, row := sts[6:9], sts[9]
	run := func(c *C, p P, st ...string) error {
		for _, x := range st {
			if _, e := c.runBlock(x, p, nil); e != nil {
				c.tryx("ROLLBACK")
				return e
			}
		}
		return nil
	}
	params := func(sha, title string, preview any) P {
		return P{"sha256": sha, "mime": "image/heic", "preview": preview, "file_title": title, "file_key": strings.ToLower(title),
			"day": "2026-06-02", "body": "The lake at dawn.", "source": "ui"}
	}
	count := func(c *C) string {
		return c.tab("select (select count(*) from entities where source <> 'schema'), (select count(*) from files)")
	}

	c = s.fresh()
	p := params(hash(1), "2026-06-02 Lake.jpg", jpegBytes)
	e := run(c, p, sts...)
	s.K("cookbook/keep-a-file run literally keeps a new file: its entity, its page with the text and the day, its files row",
		e == nil && c.tab("select e.entity_type, n.title, e.day, e.body, f.mime, f.preview = ?, e.source from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key join files f on f.id=e.id", jpegBytes) ==
			"file|2026-06-02 Lake.jpg|2026-06-02|The lake at dawn.|image/heic|1|ui", e)
	shape := func(c *C) string {
		return c.tab(`select e.entity_type, e.source, n.title, n.name_key, e.day, e.body, f.sha256, f.mime, hex(f.preview)
 from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key join files f on f.id=e.id`)
	}
	cw := s.fresh()
	k, ew := cw.store().AddFile(context.Background(), "ui", core.FileIn{Title: "2026-06-02 Lake.jpg", SHA256: hash(1), MIME: "image/heic",
		Body: "The lake at dawn.", Day: "2026-06-02", Preview: jpegBytes})
	s.K("the writer's own keep writes the rows the recipe writes", ew == nil && shape(cw) == shape(c), ew, shape(cw))
	beforeAgain := count(cw)
	k2, ew := cw.store().AddFile(context.Background(), "import:photos", core.FileIn{Title: "IMG_0001.HEIC", SHA256: hash(1), MIME: "image/heic"})
	s.K("...and finds the same original again, from another source, writing nothing", ew == nil && k2.Existing && k2.ID == k.ID && count(cw) == beforeAgain, ew, k2)
	before := count(c)
	again := params(hash(1), "IMG_0001.HEIC", jpegBytes)
	again["source"] = "import:photos"
	e = run(c, again, begin)
	found := c.rows(look, again)
	e2 := run(c, again, fill, commit)
	s.K("the same original again, from another source: step 0 finds the page, and nothing is written",
		e == nil && e2 == nil && len(found) == 1 && found[0][0] == p["file_id"] && count(c) == before, found, count(c))

	c.must("UPDATE files SET preview = NULL WHERE id = ?", p["file_id"]) // a file kept before its picture was made
	if setupErr := run(c, again, begin, look, fill, commit); setupErr != nil {
		stop("file recipe setup: %v", setupErr)
	}
	s.K("a missing preview is filled by a re-send that has one", c.n("select preview IS NOT NULL from files") == 1)
	other := append([]byte{}, jpegBytes...)
	other = append(other[:len(other)-2], 0, 0xFF, 0xD9)
	if setupErr := run(c, params(hash(1), "x", other), begin, look, fill, commit); setupErr != nil {
		stop("file recipe setup: %v", setupErr)
	}
	s.K("...and an existing preview is never replaced", c.n("select preview = ? from files", jpegBytes) == 1)
	c.must("UPDATE files SET preview = NULL WHERE id = ?", p["file_id"])
	c.must("UPDATE entities SET deleted_at = "+NOW+" WHERE id = ?", p["file_id"])
	if setupErr := run(c, again, begin, look, fill, commit); setupErr != nil {
		stop("file recipe setup: %v", setupErr)
	}
	s.K("a tombstoned file is left alone: no preview is filled", c.n("select preview IS NULL from files") == 1)
	e = run(c, params(hash(1), "Third.jpg", nil), sts...)
	s.K("a writer that skips step 0 is refused by files_sha256, and the transaction rolls back whole",
		e != nil && strings.Contains(e.Error(), "UNIQUE") && c.n("select count(*) from entity_names where name_key='third.jpg'") == 0, e)

	// a ghost an earlier embed made is promoted
	c = s.fresh()
	d, _ := c.savePage("Dawn at the lake: ![[Lake.jpg]]", "2026-06-03")
	ghost := c.n("select entity_id from entity_names where name_key='lake.jpg'")
	p = params(hash(2), "Lake.jpg", nil)
	if setupErr := run(c, p, begin); setupErr != nil {
		stop("file recipe setup: %v", setupErr)
	}
	held := c.rows(resolve, p)
	s.K("step 1 finds the ghost: a plain page, empty, not a day page, not a stub",
		len(held) == 1 && tab(held) == ids(ghost)+"|page|0|1", tab(held))
	p["file_id"] = ghost
	e = run(c, p, []string{promote[0], promote[1], promote[2], row, commit}...)
	s.K("1b promotes it: one id, a file page with the text, a files row; the day's embed lands on the file",
		e == nil && c.tab("select e.entity_type, e.body, f.sha256 = ? from entities e join files f using(id) where id=?", hash(2), ghost) == "file|The lake at dawn.|1" &&
			c.n("select count(*) from links where from_id=? and to_id=? and kind='wikilink'", d, ghost) == 1, e)
	read, e := c.query(blocks[1][1], P{"day": "2026-06-03"})
	s.K("the pictures a day shows: the file pages its day page links", e == nil && tab(read) == ids(ghost)+"|Lake.jpg|image/heic|0", tab(read), e)
	written := c.pageW("Notes.pdf", nil, "my own words")
	p = params(hash(3), "Notes.pdf", nil)
	p["file_id"] = written
	if setupErr := run(c, p, begin, promote[0], promote[1], promote[2], row, commit); setupErr != nil {
		stop("file recipe setup: %v", setupErr)
	}
	s.K("a page with text becomes the file with its text kept; :body fills only an empty body",
		c.str("select body from entities where id=?", written) == "my own words" && c.str("select entity_type from entities where id=?", written) == "file")
	// Promotion row parity: date filling is independent of whether the page has text.
	for _, tc := range []struct{ name, day, body, incoming string }{
		{"missing day", "", "", "The lake at dawn."},
		{"existing day", "2026-06-01", "", "The lake at dawn."},
		{"existing text", "", "my own words", ""},
	} {
		cr, cw := s.fresh(), s.fresh()
		seed := func(c *C) (int64, int64) {
			d, _ := c.savePage("![[Promotion.jpg]]", "2026-06-03")
			id := c.n("select entity_id from entity_names where name_key='promotion.jpg'")
			c.must("UPDATE entities SET day=NULLIF(?, ''), body=? WHERE id=?", tc.day, tc.body, id)
			return id, d
		}
		rid, rd := seed(cr)
		wid, wd := seed(cw)
		pp := params(hash(6), "Promotion.jpg", nil)
		pp["file_id"], pp["body"] = rid, tc.incoming
		er := run(cr, pp, begin, promote[0], promote[1], promote[2], row, commit)
		kw, ew := cw.store().AddFile(context.Background(), "ui", core.FileIn{
			Title: "Promotion.jpg", SHA256: hash(6), MIME: "image/heic", Day: "2026-06-02", Body: tc.incoming,
		})
		wantDay := tc.day
		if wantDay == "" {
			wantDay = "2026-06-02"
		}
		s.K("promotion "+tc.name+": recipe and writer agree, filling only a missing day",
			er == nil && ew == nil && shape(cr) == shape(cw) && cr.str("select day from entities where id=?", rid) == wantDay,
			er, ew, shape(cr), shape(cw))
		s.K("promotion "+tc.name+": identity, text and backlinks survive without inferred photo links",
			kw.Promoted && kw.ID == wid && cr.n("select id from files") == rid &&
				cr.n("select count(*) from links where from_id=? and to_id=? and kind='wikilink'", rd, rid) == 1 &&
				cw.n("select count(*) from links where from_id=? and to_id=? and kind='wikilink'", wd, wid) == 1 &&
				cr.n("select count(*) from links") == 1 && cw.n("select count(*) from links") == 1 &&
				cr.str("select body from entities where id=?", rid) == tc.body+tc.incoming)
	}
	for _, x := range []struct{ what, title string }{{"a person", "Bob Sample"}, {"a day page", "2026-06-03"}} {
		var held int64
		if x.what == "a person" {
			held = c.named("person", x.title)
		} else {
			held = d
		}
		p = params(hash(4), x.title, nil)
		p["file_id"] = held
		e = run(c, p, []string{promote[0], promote[1], promote[2], row, commit}...)
		s.K("a title held by "+x.what+": 1b changes nothing and the files row is refused, so the whole write rolls back",
			e != nil && c.str("select entity_type from entities where id=?", held) != "file" && c.n("select count(*) from files where sha256=?", hash(4)) == 0, e)
	}

	prose := c.pageW("Old Lake", nil, "#REDIRECT [[Lake.jpg]]")
	p = params(hash(5), "Old Lake", nil)
	p["file_id"] = prose
	p["body"] = ""
	e = run(c, p, begin, promote[0], promote[1], promote[2], row, commit)
	s.K("REDIRECT prose has no special promotion state", e == nil && c.str("SELECT body FROM entities WHERE id=?", prose) == "#REDIRECT [[Lake.jpg]]", e)

	s.K("integrity and foreign keys clean after the refusals", c.integrityOK())
}
