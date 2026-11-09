package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"aminte/client"
	"aminte/internal/api"
	"aminte/internal/api/apitest"
	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/siren"
)

// The owner-only actions are refused to an agent and to an import, with 403 and the code forbidden. The full-text
// check of the integrity checks (integrity-check) holds the write lock while it runs, so it is the owner's alone too:
// its GET route refuses as a POST route does. A refused action writes nothing: no metric, and no file in the snapshot
// folder. That they are never MCP tools is a test of the extension extensions/mcp (TestTheToolsAreTheActionsThatAreNotTheOwners).
func TestOwnerOnlyActionsPolicy(t *testing.T) {
	d, snapshots := dbtest.New(t), t.TempDir()
	h := api.NewWith(&core.Store{DB: d, SnapshotDir: snapshots}, nil)
	catalog := apitest.Must(t)(client.InProcess(h, "agent:policy-test").Get(t.Context(), "/actions"))
	// The catalog has owner reads and owner writes, and an ordinary action is not the owner's. The loop below
	// reads the catalog's own mark, so a new owner action needs no edit here.
	var ownerGets, ownerPosts int
	for _, a := range catalog.Actions {
		switch {
		case a.Owner && a.Method == "GET":
			ownerGets++
		case a.Owner && a.Method == "POST":
			ownerPosts++
		}
	}
	if ownerGets == 0 || ownerPosts == 0 {
		t.Fatalf("the catalog has %d owner reads and %d owner writes, want at least one of each", ownerGets, ownerPosts)
	}
	if record := apitest.Action(t, catalog, "record"); record.Owner {
		t.Fatalf("record is owner-only: %+v", record)
	}
	sent := map[string]map[string]string{"register-metric": {"name": "Forbidden direct"}}
	for _, source := range []string{"agent:policy-test", "import:policy-test"} {
		c := client.InProcess(h, source)
		for _, a := range catalog.Actions {
			if !a.Owner {
				continue
			}
			e, err := c.Do(t.Context(), a, sent[a.Name])
			var code any
			if e != nil {
				p, _ := e.Properties.(map[string]any)
				code = p["code"]
			}
			if apitest.Status(err) != 403 || code != "forbidden" {
				t.Errorf("%s: owner-only %s: %v, code %v; want 403 forbidden", source, a.Name, err, code)
			}
		}
	}
	var count int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE name_key = 'forbidden direct'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("a refused registration wrote a metric: %d %v", count, err)
	}
	if files, err := os.ReadDir(snapshots); err != nil || len(files) != 0 {
		t.Fatalf("a refused snapshot wrote in the snapshot folder: %v %v", files, err)
	}
}

// A writer name that is not well formed is refused where it is used. A write that inserts a row meets
// entities_source, so the 422 is SQLite's and names the constraint. An owner-only action may only read, and meets no
// CHECK, so the boundary refuses the name: "Agent:x" must not pass as the owner by missing the prefix "agent:".
func TestMalformedWriterIsRefusedByTheSchemaOrTheOwnerCheck(t *testing.T) {
	d := dbtest.New(t)
	h := api.NewWith(&core.Store{DB: d, SnapshotDir: t.TempDir()}, nil)
	catalog := apitest.Must(t)(client.InProcess(h, "cli").Get(t.Context(), "/actions"))
	for _, source := range []string{"Bad Source!", "Agent:x", strings.Repeat("a", 65)} {
		c := client.InProcess(h, source)
		_, err := c.Do(t.Context(), apitest.Action(t, catalog, "create-page"), map[string]string{"title": "Malformed " + source[:3], "body": "words"})
		if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "entities_source") {
			t.Errorf("create-page as %q: %v; want 422 that names entities_source", source, err)
		}
		_, err = c.Do(t.Context(), apitest.Action(t, catalog, "integrity-check"), nil)
		if apitest.Status(err) != 422 {
			t.Errorf("integrity-check as %q: %v; want 422", source, err)
		}
	}
	var count int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE title LIKE 'Malformed%'").Scan(&count); err != nil || count != 0 {
		t.Errorf("a refused write left pages: %d %v", count, err)
	}
}

// rebuild-search repairs the full-text index, for the owner only. The index here holds a document that no page has,
// which the full-text check of Integrity finds. An agent is refused and the index stays as it was; the owner's call
// repairs it and answers the number of documents.
func TestRebuildSearchIsTheOwnersRepair(t *testing.T) {
	d := dbtest.New(t)
	s := &core.Store{DB: d}
	h := api.NewWith(s, nil)
	if _, err := d.W.ExecContext(t.Context(), `INSERT INTO entities_fts(rowid, preferred, all_names, body) VALUES (999, 'ghost', 'ghost', 'drifted')`); err != nil {
		t.Fatal(err)
	}
	indexOK := func() bool {
		t.Helper()
		r, err := s.Integrity(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return r.FullTextIndexOK
	}
	if indexOK() {
		t.Fatal("the fixture index must be drifted")
	}
	agent := client.InProcess(h, "agent:repair-test")
	catalog := apitest.Must(t)(agent.Get(t.Context(), "/actions"))
	rebuild := apitest.Action(t, catalog, "rebuild-search")
	e, err := agent.Do(t.Context(), rebuild, nil)
	if p, _ := e.Properties.(map[string]any); apitest.Status(err) != 403 || p["code"] != "forbidden" {
		t.Fatalf("an agent's rebuild-search: %v %v; want 403 forbidden", err, e)
	}
	if indexOK() {
		t.Fatal("a refused rebuild-search repaired the index")
	}
	e = apitest.Must(t)(client.InProcess(h, "cli").Do(t.Context(), rebuild, nil))
	if p, _ := e.Properties.(map[string]any); p["documents"] == nil {
		t.Errorf("the answer of rebuild-search has no document count: %+v", e.Properties)
	}
	if !indexOK() {
		t.Fatal("the owner's rebuild-search did not repair the index")
	}
}

// resync-links rewrites the links of the bodies, for the owner only. The body of a page here names a page, and no link
// row says so. An agent is refused and the link stays missing; the owner's call writes it and lists the page.
func TestResyncLinksIsTheOwnersRepair(t *testing.T) {
	d := dbtest.New(t)
	s := &core.Store{DB: d}
	h := api.NewWith(s, nil)
	to, _, err := s.CreatePage(t.Context(), "cli", "Bridge", "", "")
	if err != nil {
		t.Fatal(err)
	}
	from, _, err := s.CreatePage(t.Context(), "cli", "Anchor", "see [[Bridge]]", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.ExecContext(t.Context(), `DELETE FROM links WHERE from_id = ? AND to_id = ?`, from, to); err != nil {
		t.Fatal(err)
	}
	drift := func() []int64 {
		t.Helper()
		r, err := s.Integrity(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return r.LinkDrift
	}
	if got := drift(); len(got) != 1 || got[0] != from {
		t.Fatalf("the fixture must have the drift of the page %d: %v", from, got)
	}
	agent := client.InProcess(h, "agent:repair-test")
	catalog := apitest.Must(t)(agent.Get(t.Context(), "/actions"))
	resync := apitest.Action(t, catalog, "resync-links")
	e, err := agent.Do(t.Context(), resync, nil)
	if p, _ := e.Properties.(map[string]any); apitest.Status(err) != 403 || p["code"] != "forbidden" {
		t.Fatalf("an agent's resync-links: %v %v; want 403 forbidden", err, e)
	}
	if len(drift()) != 1 {
		t.Fatal("a refused resync-links repaired the links")
	}
	e = apitest.Must(t)(client.InProcess(h, "cli").Do(t.Context(), resync, nil))
	if p, _ := e.Properties.(map[string]any); fmt.Sprint(p["pages"]) != fmt.Sprint([]any{json.Number(fmt.Sprint(from))}) {
		t.Errorf("the answer of resync-links lists %v; want the page %d", p["pages"], from)
	}
	if len(drift()) != 0 {
		t.Fatal("the owner's resync-links did not repair the links")
	}
}

// TestAnAgentIsRefusedTheOwnersReads: the integrity check is the owner's read. The writer of an embedded read
// (agent:embed) and any other agent get 403, so an address that an agent wrote never makes the server run it.
func TestAnAgentIsRefusedTheOwnersReads(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	for _, source := range []string{"agent:embed", "agent:x", "import:x"} {
		for _, path := range []string{"/integrity"} {
			rec := request(h, "GET", path, "", map[string]string{siren.SourceHeader: source})
			if rec.Code != http.StatusForbidden {
				t.Errorf("GET %s as %s: %d, want 403", path, source, rec.Code)
			}
		}
	}
}
