package tests

import (
	"fmt"
	"maps"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The reference stores one directed edge per link. NULL and empty notes remain different.
type graphEdge struct {
	kind string
	a, b int64
}

func graphKey(kind string, a, b int64) graphEdge {
	return graphEdge{kind, a, b}
}

func graphModel(s *S) {
	for _, seed := range []int64{2075, 20261007, 73} {
		rng := rand.New(rand.NewSource(seed))
		operations := make([]byte, 4*160)
		if _, err := rng.Read(operations); err != nil {
			stop("graph seed %d: %v", seed, err)
		}
		graphSequence(s, operations, fmt.Sprintf("seed-%d", seed))
	}
}

func graphSequence(s *S, operations []byte, label string) {
	path := filepath.Join(s.dir, fmt.Sprintf("graph-%d.db", s.next()))
	c := s.freshWith(F{Path: path, Hardened: true})
	c.must("PRAGMA synchronous=FULL")
	pages := []int64{c.page("Graph Ada"), c.page("Graph Bea"), c.page("Graph Cy"), c.page("Graph Dee")}
	journal := c.dayPage("2026-10-07", "an invalid part-of target")
	want := map[graphEdge]any{}
	const selectEdges = "SELECT kind,from_id,to_id,note FROM links ORDER BY kind,from_id,to_id"
	const selectVersions = "SELECT id,revision,updated_at FROM entities ORDER BY id"
	check := func(step string) {
		var expected [][]any
		for edge, note := range want {
			expected = append(expected, []any{edge.kind, edge.a, edge.b, note})
		}
		sort.Slice(expected, func(i, j int) bool {
			a, b := expected[i], expected[j]
			if a[0] != b[0] {
				return a[0].(string) < b[0].(string)
			}
			if a[1] != b[1] {
				return a[1].(int64) < b[1].(int64)
			}
			return a[2].(int64) < b[2].(int64)
		})
		got := c.tab(selectEdges)
		s.K(step+" agrees with independent graph", got == tab(expected), got, tab(expected))
	}
	for pos := 0; pos+3 < len(operations); pos += 4 {
		op, a, b := operations[pos]%8, pages[int(operations[pos+1])%len(pages)], pages[int(operations[pos+2])%len(pages)]
		// a kind derived from bodies, and a journal directed one
		kind := [...]string{"wikilink", "part-of"}[operations[pos+3]%2]
		var note any
		switch (operations[pos+3] >> 1) % 3 {
		case 1:
			note = ""
		case 2:
			note = fmt.Sprintf("note-%d", operations[pos+3]%5)
		}
		key := graphKey(kind, a, b)
		step := fmt.Sprintf("%s step %d op %d %v", label, pos/4, op, key)
		before, oldVersions := c.tab(selectEdges), c.rows(selectVersions)
		versions := tab(oldVersions)
		saved := maps.Clone(want)
		c.must("BEGIN IMMEDIATE")
		c.must("SAVEPOINT graph_step")
		switch op {
		case 0, 1, 6:
			c.must("INSERT INTO links(from_id,to_id,kind,note,created_at,source) VALUES(?,?,?,?, '2026-10-07T00:00:00.000Z','ui') ON CONFLICT(from_id,to_id,kind) DO NOTHING", a, b, kind, note)
			if _, exists := want[key]; !exists {
				want[key] = note
			}
		case 2:
			c.must("UPDATE links SET note=? WHERE kind=? AND from_id=? AND to_id=?", note, kind, a, b)
			if _, exists := want[key]; exists {
				want[key] = note
			}
		case 3:
			c.must("DELETE FROM links WHERE kind=? AND from_id=? AND to_id=?", kind, a, b)
			delete(want, key)
		case 4:
			// The first row is valid (or an explicit duplicate); the second is typed wrong.
			// ABORT must restore the revisions generated for the first row.
			result := c.tryx("INSERT INTO links(from_id,to_id,kind,note,created_at,source) VALUES(?,?,'part-of',?,'2026-10-07T00:00:00.000Z','ui'),(?,?,'part-of',?,'2026-10-07T00:00:00.000Z','ui') ON CONFLICT(from_id,to_id,kind) DO NOTHING", a, b, note, a, journal, note)
			s.K(step+" rejects typed invalid batch atomically", strings.HasPrefix(result, "ERR") && c.tab(selectEdges) == before && c.tab(selectVersions) == versions, result)
		case 5:
			result := c.tryx("UPDATE links SET note='forbidden',created_at='2026-10-08T00:00:00.000Z' WHERE kind=? AND from_id=? AND to_id=?", kind, a, b)
			_, exists := want[key]
			s.K(step+" refuses immutable edit without side effects", (exists && strings.HasPrefix(result, "ERR") || !exists && result == "OK") && c.tab(selectEdges) == before && c.tab(selectVersions) == versions, result)
		case 7:
			c.must("UPDATE links SET note=note WHERE kind=? AND from_id=? AND to_id=?", kind, a, b)
			s.K(step+" no-op preserves revisions", c.tab(selectVersions) == versions)
		}
		check(step + " inside transaction")
		// Which pages a graph change advances (aminte_meta.edit_revisions), from the rule and not from the triggers:
		// a link is an edit of the page it leaves; a wikilink
		// is derived from the body, so only editing its note is an edit; no page is advanced by a link it receives.
		advanced := map[int64]bool{}
		if !maps.Equal(want, saved) {
			switch {
			case kind == "wikilink" && op != 2:
			default:
				advanced[a] = true
			}
		}
		for i, row := range c.rows(selectVersions) {
			id, revision, updated := row[0].(int64), row[1].(int64), row[2]
			old, oldUpdated := oldVersions[i][1].(int64), oldVersions[i][2]
			if advanced[id] {
				s.K(step+" changed endpoint revision advances", revision > old, id, revision, old)
			} else {
				s.K(step+" unchanged endpoint revision stays fixed", revision == old && updated == oldUpdated, id, revision, old, updated, oldUpdated)
			}
		}
		if op == 6 {
			c.must("ROLLBACK TO graph_step")
			want = saved
			s.K(step+" savepoint rollback restores edge and revision state", c.tab(selectEdges) == before && c.tab(selectVersions) == versions)
		}
		c.must("RELEASE graph_step")
		if operations[pos+3]&128 != 0 {
			c.must("ROLLBACK")
			want = saved
			s.K(step+" transaction rollback restores edge and revision state", c.tab(selectEdges) == before && c.tab(selectVersions) == versions)
		} else {
			c.must("COMMIT")
		}
		check(step + " after transaction")
	}
	check(label + " final")
	s.K(label+" structural and FTS integrity", c.integrityOK() && c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK")
	if err := c.Close(); err != nil {
		stop("close graph: %v", err)
	}
	c = s.readOnly(path)
	c.must("PRAGMA trusted_schema=OFF")
	c.must("PRAGMA reverse_unordered_selects=ON")
	check(label + " read-only reopen")
}

// Byte operations give Go's minimizer control of individual operations and endpoints.
// Each input is isolated and capped at 64 transitions, including synthetic file I/O.
func FuzzGraphTransitions(f *testing.F) {
	f.Add([]byte{0, 0, 1, 0, 2, 1, 0, 4, 7, 0, 1, 0, 3, 1, 0, 0})
	f.Add([]byte{0, 0, 0, 2, 6, 0, 1, 4, 4, 2, 3, 0, 5, 0, 0, 0})
	f.Add([]byte{0, 0, 1, 1, 0, 1, 0, 3, 2, 0, 1, 133, 3, 0, 1, 1})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 4*64 {
			operations = operations[:4*64]
		}
		d := realDocs()
		s := &S{name: "graph-fuzz", d: d, ddl: d.DDL(), dir: t.TempDir()}
		defer s.close()
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("operations %x stopped: %v", operations, r)
			}
		}()
		graphSequence(s, operations, "fuzz")
		for _, failure := range s.fails {
			t.Error(failure)
		}
	})
}
