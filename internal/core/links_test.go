package core

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSaveBodyAddsAndDeletesLinks(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Diet", "See [[Plan]] and [[Health/Diet]] and [[Diet]]", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, id)
	if titles(p.Out, "wikilink") != "Plan" {
		t.Fatalf("links %v", p.Out)
	}
	r, err := s.SaveBody(ctx, "cli", id, "nowSQL [[Other]] only", p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skipped) != 0 {
		t.Errorf("skipped %v", r.Skipped)
	}
	p2, _ := s.PageByID(ctx, id)
	if titles(p2.Out, "wikilink") != "Other" {
		t.Errorf("after the re-save: %v", p2.Out)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale", p.Version); status(err) != 409 {
		t.Errorf("a save with an old version: %v", err)
	}
}

func TestSaveRevivesATombstonedTarget(t *testing.T) {
	s := fresh(t)
	old, _, _ := s.CreatePage(ctx, "cli", "Old", "", "")
	if err := s.Tombstone(ctx, "cli", old); err != nil {
		t.Fatal(err)
	}
	_, r, err := s.CreatePage(ctx, "cli", "New", "[[old]]", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Revived, ",") != "old" || len(r.Created) != 0 {
		t.Errorf("sync %+v", r)
	}
}

// TestALinkTargetIsSkippedOnlyWhenItsSavepointRollsBack: a body save skips a wikilink target that fails, when the
// savepoint of the target rolls it back alone (cookbook/save-a-body.md, "any error in 1-3"), and the rest of the save
// commits. When the savepoint cannot roll back, the save fails as a fault, and the page keeps its body and its links.
// A trigger of the test (deliberate damage) makes the target fail when the save inserts its name: RAISE(ABORT) is a
// refusal; a function that does not exist is an error that is not a refusal; RAISE(ROLLBACK) rolls the whole
// transaction back, so the savepoint cannot roll back to its start.
func TestALinkTargetIsSkippedOnlyWhenItsSavepointRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name, trigger string
		skipped       bool
	}{
		{"a refusal of the DDL", `SELECT RAISE(ABORT, 'the test refuses the target')`, true},
		{"an error that is not a refusal", `SELECT no_such_function()`, true},
		{"a rollback of the whole write", `SELECT RAISE(ROLLBACK, 'the test rolls the write back')`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fresh(t)
			home, _, err := s.CreatePage(ctx, "cli", "Home", "[[Old]]", "")
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.PageByID(ctx, home)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.W.ExecContext(ctx, `CREATE TEMP TRIGGER fail_the_target BEFORE INSERT ON entity_names
			    WHEN NEW.title = 'New' BEGIN `+tc.trigger+`; END`); err != nil {
				t.Fatal(err)
			}
			r, err := s.SaveBody(ctx, "cli", home, "[[New]]", p.Version)
			body, links := "[[Old]]", "Old"
			if tc.skipped {
				body, links = "[[New]]", ""
				if err != nil || strings.Join(r.Skipped, ",") != "New" || len(r.Linked) != 0 {
					t.Errorf("SaveBody = %+v, %v; want New skipped and nothing linked", r, err)
				}
			} else if err == nil || status(err) != 0 || !reflect.DeepEqual(r, Sync{}) {
				t.Errorf("SaveBody = %+v, %v (status %d); want a fault, not a refusal or a skipped target", r, err, status(err))
			}
			got, err := s.PageByID(ctx, home)
			if err != nil {
				t.Fatal(err)
			}
			if got.Body != body || titles(got.Out, "wikilink") != links {
				t.Errorf("Home: body %q, links %q; want body %q, links %q", got.Body, titles(got.Out, "wikilink"), body, links)
			}
			if id, err := s.PageID(ctx, "New"); id != 0 || err != nil {
				t.Errorf("PageID(New) = %d, %v; want no page", id, err)
			}
		})
	}
}

// TestAFaultOfSavepointOrReleaseFailsTheSave: when the SAVEPOINT of a link target fails, or the RELEASE after a target
// that succeeded fails, the save fails as a fault. The page keeps its body and its links, and the target is not made.
//
// The fault is a full file. VACUUM removes the free pages, and PRAGMA max_page_count then lets the file grow by 16
// pages only. FTS5 keeps the terms of a write in memory. It writes them to the file when a savepoint opens, and when a
// savepoint closes after a statement in it opened its own savepoint. The terms of a body of 10,000 words need more than
// 16 pages. When SAVEPOINT or RELEASE fails because the file is full, SQLite rolls back the whole transaction, and the
// statements after it then run outside a transaction. A row that keeps its size is written in place and needs no new
// page. So the test saves each page once in capitals first (the same length, and a revision that keeps its size), and
// the save under test writes a body of the same length.
//
// In the SAVEPOINT case, the terms are those of the long body that the save writes. In the RELEASE case, a trigger of
// the test (deliberate damage) writes the long body of Big again with other terms when the save links New. That is the
// last statement of the target, so the terms stay in memory until the RELEASE. The test needs the FTS5 and the b-tree
// of the SQLite that go.mod names.
func TestAFaultOfSavepointOrReleaseFailsTheSave(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			s := fresh(t)
			// page makes a page and saves its body once more in capitals: the same length, and the revision 2.
			page := func(title, body string) (int64, string) {
				t.Helper()
				id, _, err := s.CreatePage(ctx, "cli", title, body, "")
				if err != nil {
					t.Fatal(err)
				}
				p, err := s.PageByID(ctx, id)
				if err == nil {
					_, err = s.SaveBody(ctx, "cli", id, strings.ToUpper(body), p.Version)
				}
				if err == nil {
					p, err = s.PageByID(ctx, id)
				}
				if err != nil {
					t.Fatal(err)
				}
				return id, p.Version
			}
			page("Big", long.String())
			home, version := page("Home", tc.home)
			if tc.trigger != "" {
				if _, err := s.DB.W.ExecContext(ctx, tc.trigger); err != nil {
					t.Fatal(err)
				}
			}
			var limit, pages int64
			if err := s.DB.W.QueryRowContext(ctx, `PRAGMA max_page_count`).Scan(&limit); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.W.ExecContext(ctx, `VACUUM`); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.W.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.W.ExecContext(ctx, fmt.Sprintf(`PRAGMA max_page_count = %d`, pages+16)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { // before the Close of fresh, whose PRAGMA optimize needs room
				if _, err := s.DB.W.ExecContext(ctx, fmt.Sprintf(`PRAGMA max_page_count = %d`, limit)); err != nil {
					t.Error(err)
				}
			})
			r, err := s.SaveBody(ctx, "cli", home, tc.save, version)
			if err == nil || status(err) != 0 || !reflect.DeepEqual(r, Sync{}) {
				t.Errorf("SaveBody = %+v, %v (status %d); want a fault, not a refusal or a skipped target", r, err, status(err))
			}
			got, err := s.PageByID(ctx, home)
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.ToUpper(tc.home); got.Body != want || titles(got.Out, "wikilink") != "Old" {
				t.Errorf("Home: body %.20q, links %q; want body %.20q, links %q", got.Body, titles(got.Out, "wikilink"), want, "Old")
			}
			if id, err := s.PageID(ctx, "New"); id != 0 || err != nil {
				t.Errorf("PageID(New) = %d, %v; want no page", id, err)
			}
		})
	}
}

// TestALinkOfAKindThatTheRegistryDoesNotHave: redirect is not a kind of link_kinds (D8). The trigger
// links_endpoint_types refuses a link of a kind that the registry does not have: a 422 with its sentence. A link from
// a page that does not exist is a 404, whatever its kind. No such link exists, so a request to remove one is a 404.
// Each refusal writes nothing.
func TestALinkOfAKindThatTheRegistryDoesNotHave(t *testing.T) {
	s := fresh(t)
	a, _, err := s.CreatePage(ctx, "cli", "Page A", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreatePage(ctx, "cli", "Page B", "", "")
	if err != nil {
		t.Fatal(err)
	}
	const unregistered = "link kind is not registered in link_kinds"
	for _, c := range []struct {
		name   string
		write  func() error
		status int
		says   string // a part of the message
	}{
		{"link redirect", func() error { return addLink(s, "cli", a, b, "redirect", "") }, 422, unregistered},
		{"link a kind with no row", func() error { return addLink(s, "cli", a, b, "no-such-kind", "") }, 422, unregistered},
		{"link redirect from no page", func() error { return addLink(s, "cli", 9999, b, "redirect", "") }, 404, "no page 9999"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := everyRow(t, s)
			if err := c.write(); status(err) != c.status || !strings.Contains(fmt.Sprint(err), c.says) {
				t.Errorf("got %v (status %d), want %d with %q", err, status(err), c.status, c.says)
			}
			if after := everyRow(t, s); after != before {
				t.Errorf("the refusal wrote:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestLinkKindsFromMatchesTheRegistry(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind, from_types FROM link_kinds WHERE kind <> 'wikilink' ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string][]string{}
	for rows.Next() {
		var kind string
		var from *string
		if err := rows.Scan(&kind, &from); err != nil {
			t.Fatal(err)
		}
		for _, typ := range []string{"page", "person", "file", "metric", "category", "no-such-type"} {
			if from == nil || strings.Contains(","+*from+",", ","+typ+",") {
				want[typ] = append(want[typ], kind)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want["page"]) == 0 {
		t.Fatal("the registry offers a page no kind")
	}
	for typ, kinds := range want {
		got, err := s.LinkKindsFrom(ctx, typ)
		if err != nil || strings.Join(got, ",") != strings.Join(kinds, ",") {
			t.Errorf("%s: got %v want %v (%v)", typ, got, kinds, err)
		}
	}
	// A type written as an injection is a value: it matches only the kinds open to any type, like an unknown type.
	if got, err := s.LinkKindsFrom(ctx, "page' OR '1'='1"); err != nil || strings.Join(got, ",") != strings.Join(want["no-such-type"], ",") {
		t.Errorf("injection: %v %v want %v", got, err, want["no-such-type"])
	}
}

// addLink writes one edge through the writer's own link (the part-of rows of file-pages use it).
func addLink(s *Store, source string, from, to int64, kind, note string) error {
	return s.Do(ctx, source, func(t *Tx) error { _, err := t.link(from, to, kind, note); return err })
}

// dropLink deletes one edge by direct SQL: no operation of the writer takes an edge out but file-pages with from.
func dropLink(s *Store, source string, from, to int64, kind string) error {
	_, err := s.DB.W.ExecContext(ctx, `DELETE FROM links WHERE from_id = ? AND to_id = ? AND kind = ?`, from, to, kind)
	return err
}
