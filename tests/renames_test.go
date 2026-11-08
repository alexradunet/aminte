package tests

import (
	"context"
	"strings"

	"aminte/internal/core"
	"aminte/text"
)

// renames executes the canonical recipe and compares independent storage outcomes
// with the writer. Rename retains identity rather than moving facts/edges to a stub.
func renames(s *S) {
	sts := statements(s.d.Block("rename-a-page"))
	s.K("rename recipe has BEGIN IMMEDIATE and seven identity/name statements", len(sts) == 7 && strings.HasPrefix(strings.ToUpper(code(sts[0])), "BEGIN IMMEDIATE"))
	if len(sts) != 7 {
		return
	}
	recipe := func(c *C, id int64, title string) (int64, error) {
		if !text.ValidTitle(title) {
			return 0, &core.Error{Status: 422, Msg: "invalid title"}
		}
		p := P{"old_id": id, "new_title": title, "new_key": text.TitleKey(title)}
		c.must(sts[0])
		rollback := func() (int64, error) {
			c.must("ROLLBACK")
			return 0, &core.Error{Status: 409, Msg: "reserved identity/name"}
		}
		old := c.rows(sts[1], p)
		if len(old) != 1 || old[0][2] == int64(1) || old[0][3] != nil || text.IsDay(title) {
			return rollback()
		}
		taken := c.rows(sts[2], p)
		if len(taken) > 0 && taken[0][0] != id {
			return rollback()
		}
		for _, st := range sts[3:] {
			if _, err := c.query(st, p); err != nil {
				c.must("ROLLBACK")
				return 0, err
			}
		}
		return id, nil
	}
	state := func(c *C) string {
		return c.tab("SELECT id,entity_type,preferred_name_key,coalesce(day,'-'),body,source,import_key FROM entities ORDER BY id") + " / " + c.tab("SELECT entity_id,title,name_key FROM entity_names ORDER BY name_key") + " / " + c.tab("SELECT id,from_id,to_id,kind,note,source,created_at FROM links ORDER BY id")
	}
	seed := func(c *C) (int64, int64) {
		id := c.pageW("Sourdogh", "2026-09-20", "Feed [[Baking]].")
		target := c.page("Baking")
		place := c.page("Japan")
		day := c.dayPage("2026-09-29", "See [[Sourdogh]].")
		c.link(id, target, "wikilink")
		c.link(id, target, "part-of")
		c.link(id, place, "wikilink")
		c.link(day, id, "wikilink")
		c.must("UPDATE links SET note='same flour' WHERE from_id=? AND kind='part-of'", id)
		return id, day
	}
	for _, writer := range []bool{false, true} {
		c := s.fresh()
		id, day := seed(c)
		edges := c.tab("SELECT id,from_id,to_id,kind,note,source,created_at FROM links ORDER BY id")
		rename := recipe
		if writer {
			rename = func(c *C, id int64, title string) (int64, error) {
				return c.store().Rename(context.Background(), "ui", id, title)
			}
		}
		for _, title := range []string{"Sourdough", "SOURDOUGH", "Sourdogh", "Sourdough"} {
			got, err := rename(c, id, title)
			s.K("rename recipe/writer keeps the id", err == nil && got == id, writer, title, err)
			s.K("rename keeps body/day/source", c.tab("SELECT body,day,source FROM entities WHERE id=?", id) == "Feed [[Baking]].|2026-09-20|ui")
			s.K("rename keeps exact incoming/outgoing link identities and notes", c.tab("SELECT id,from_id,to_id,kind,note,source,created_at FROM links ORDER BY id") == edges)
			s.K("old reference body and endpoint remain", c.str("SELECT body FROM entities WHERE id=?", day) == "See [[Sourdogh]]." && c.n("SELECT to_id FROM links WHERE from_id=?", day) == id)
		}
		s.K("aliases map directly to one owner", c.n("SELECT count(*) FROM entity_names WHERE name_key IN ('sourdogh','sourdough') AND entity_id=?", id) == 2 && c.n("SELECT count(DISTINCT entity_id) FROM entity_names WHERE name_key IN ('sourdogh','sourdough')") == 1)
		before := state(c)
		for _, title := range []string{"Baking", "2026-09-30", "Lab [unsafe]"} {
			_, err := rename(c, id, title)
			s.K("taken/date/grammar refusal has no side effects", err != nil && state(c) == before, title, err)
		}
		tomb := c.page("Reserved tomb")
		c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", tomb)
		ghost := c.page("Reserved ghost")
		for _, target := range []int64{tomb, ghost} {
			title := c.str("SELECT title FROM entity_names WHERE entity_id=?", target)
			before = state(c)
			_, err := rename(c, id, title)
			s.K("ghost/tombstone names do not merge", err != nil && state(c) == before, err)
		}
		before = state(c)
		_, err := rename(c, day, "Other day")
		s.K("journal identity cannot rename", err != nil && state(c) == before, err)
		before = state(c)
		_, err = rename(c, tomb, "Other tomb")
		s.K("deleted identity must revive before rename", err != nil && state(c) == before, err)
	}
	for _, typ := range []string{"person", "metric", "file"} {
		c := s.fresh()
		id := c.named(typ, "Old "+typ)
		before := c.tab("SELECT entity_type,body,day,source FROM entities WHERE id=?", id)
		got, err := recipe(c, id, "New "+typ)
		s.K("typed identity rename preserves type/prose/day/source", err == nil && got == id && c.tab("SELECT entity_type,body,day,source FROM entities WHERE id=?", id) == before, typ, err)
		table := map[string]string{"person": "people", "metric": "metrics", "file": "files"}[typ]
		s.K("typed extension still has the same owner", c.n("SELECT count(*) FROM "+table+" WHERE id=?", id) == 1)
	}
}
