package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A name key is derived from the title by the pinned algorithm of the writer. SQLite cannot compute it, so a key set
// by hand is found by Go (docs/contract/integrity-checks.md).
func TestIntegrityDetectsANameKeyThatIsNotTheKeyOfItsTitle(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Harbour", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := s.Integrity(ctx)
	if err != nil || !clean.OK {
		t.Fatalf("clean setup: %+v %v", clean, err)
	}
	var alias int64
	if err := s.DB.W.QueryRowContext(ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, 'Straße', 'strasse-by-hand') RETURNING id`, id).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	got, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.OK || !reflect.DeepEqual(got.NameKeyMismatches, []int64{alias}) || len(got.OrphanEntities) != 0 || len(got.InvalidJournalNames) != 0 {
		t.Fatalf("want only NameKeyMismatches=[%d]: %+v", alias, got)
	}
}

// pageQueries are the statements of the first sql block of docs/contract/integrity-checks.md, the home of the
// queries, with the comments cut and the white space folded.
func pageQueries(t *testing.T) []string {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "contract", "integrity-checks.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(page), "```sql\n")
	if !ok {
		t.Fatal("the page has no sql block")
	}
	block, _, _ = strings.Cut(block, "```")
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		line, _, _ = strings.Cut(line, " -- ")
		line, _, _ = strings.Cut(line, ";   --")
		lines = append(lines, line)
	}
	var out []string
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		if folded := strings.Join(strings.Fields(statement), " "); folded != "" {
			out = append(out, folded)
		}
	}
	return out
}

// The queries of idChecks are copies of the page that is their home. A query that changes in one place only fails
// here.
func TestIntegrityQueriesAreThoseOfTheContract(t *testing.T) {
	page := pageQueries(t)
	checks := (&IntegrityResult{}).idChecks()
	const before = 2 // integrity_check and foreign_key_check come first on the page
	if len(page) < before+len(checks) {
		t.Fatalf("the page has %d statements; want at least %d", len(page), before+len(checks))
	}
	for i, c := range checks {
		if got, want := strings.Join(strings.Fields(c.query), " "), page[before+i]; got != want {
			t.Errorf("%s differs from the page:\n code: %s\n page: %s", c.name, got, want)
		}
	}
}
