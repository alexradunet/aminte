package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
	"lifelog/siren"
)

// The owner's snapshot action, from any client; refused to an agent.
func TestSnapshotIsTheOwnersFromAnySurface(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	clock := func() time.Time { return time.Date(2026, 10, 2, 14, 30, 5, 0, time.UTC) }
	h := api.New(&core.Store{DB: d, SnapshotDir: dir, Clock: clock, Zone: time.UTC})
	owner := client.InProcess(h, "cli")
	agent := client.InProcess(h, "agent:lmstudio")
	root := apitest.Must(t)(owner.Get(t.Context(), "/"))
	if _, _, err := (&core.Store{DB: d}).Capture(t.Context(), "cli", "2031-08-01", "a row to copy", nil); err != nil {
		t.Fatal(err)
	}
	action := apitest.Action(t, root, "snapshot")
	if !action.Owner {
		t.Fatalf("the root offers the owner the snapshot action: %+v", action)
	}

	first := apitest.Must(t)(owner.Do(t.Context(), action, nil))
	props := first.Properties.(map[string]any)
	path, _ := props["snapshot"].(string)
	today := "2026-10-02" // the day of the store's clock
	if filepath.Dir(path) != dir || filepath.Base(path) != "life-"+today+".db" {
		t.Fatalf("snapshot path %q: want life-%s.db in %s", path, today, dir)
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		t.Fatalf("the file: %v", err)
	}
	if check, _ := props["restore_check"].(map[string]any); check["ok"] != true {
		t.Fatalf("restore check: %v", props["restore_check"])
	}
	if first.Result == nil || apitest.Href(first, "self") != "/" {
		t.Fatalf("the answer carries the result and points home: result=%v self=%q", first.Result, apitest.Href(first, "self"))
	}
	second := apitest.Must(t)(owner.Do(t.Context(), action, nil))
	again, _ := second.Properties.(map[string]any)["snapshot"].(string)
	if filepath.Base(again) != "life-"+today+"T143005.db" {
		t.Fatalf("a second snapshot the same day: %q, want life-%sT143005.db", again, today)
	}

	// An agent is refused and writes nothing.
	before, _ := os.ReadDir(dir)
	if _, err := agent.Do(t.Context(), action, nil); apitest.Status(err) != 403 {
		t.Fatalf("agent: %v", err)
	}
	after, _ := os.ReadDir(dir)
	if len(after) != len(before) {
		t.Fatalf("an agent's refusal wrote a file: %d -> %d", len(before), len(after))
	}

}

// A snapshot that fails its restore check is an error on every surface, not a 200 with ok false inside: the file is
// kept (the check does not delete), and the answer names it with the code restore_check_failed
// (docs/plans/098-the-codebase-made-cohesive.md).
func TestASnapshotThatFailsItsRestoreCheckIsAnError(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	clock := func() time.Time { return time.Date(2026, 10, 2, 14, 30, 5, 0, time.UTC) }
	h := api.New(&core.Store{DB: d, SnapshotDir: dir, Clock: clock, Zone: time.UTC})
	owner := client.InProcess(h, "cli")
	root := apitest.Must(t)(owner.Get(t.Context(), "/"))
	apitest.Must(t)(owner.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Walk"}))
	// Deliberate damage, past the writer and the DDL: a correction of a reading of another metric, written with its
	// trigger dropped. The semantic integrity check reports it, in the live file and in every copy of it.
	apitest.Must(t)(owner.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Walk", "day": "2026-10-01", "value": "1"}))
	withoutTrigger(t, d.W, "measurements_supersede_metric", `INSERT INTO measurements(metric_id, day, value, supersedes_id, source, created_at)
		SELECT 1, '2026-10-01', 3, (SELECT id FROM measurements LIMIT 1), 'cli', strftime('%Y-%m-%dT%H:%M:%fZ','now')`)
	e, err := owner.Do(t.Context(), apitest.Action(t, root, "snapshot"), nil)
	if apitest.Status(err) != 500 {
		t.Fatalf("a failed restore check answers 500, got %v", err)
	}
	props, _ := e.Properties.(map[string]any)
	if props["code"] != "restore_check_failed" {
		t.Errorf("code = %v, want restore_check_failed", props["code"])
	}
	if _, err := os.Stat(filepath.Join(dir, "life-2026-10-02.failed.db")); err != nil {
		t.Errorf("the snapshot is kept under the failed name although its check failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "life-2026-10-02.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a snapshot that failed its check keeps the good name: %v", err)
	}
	if msg, _ := props["message"].(string); !strings.Contains(msg, "life-2026-10-02.failed.db") {
		t.Errorf("the answer = %q, want it to name the failed file", msg)
	}
}

// TestOnlyARefusedSnapshotIsSnapshotRefused: 422 snapshot_refused is for a refusal that core reports, here a folder
// inside a git work tree. A snapshot whose time ran out is a 504 timeout, and one that fails for another cause (its
// request cancelled) is a 500 internal, as every other route answers them. A 504 keeps the text of its cause; a 500
// says only "internal error". None of them leaves a file.
func TestOnlyARefusedSnapshotIsSnapshotRefused(t *testing.T) {
	d := dbtest.New(t)
	snapshot := func(t *testing.T, ctx context.Context, dir string, status int, code string) (message string) {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, "POST", "/snapshots", nil)
		rec := httptest.NewRecorder()
		api.New(&core.Store{DB: d, SnapshotDir: dir}).ServeHTTP(rec, req)
		var e siren.Entity
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("the answer is not an entity: %v: %s", err, rec.Body.String())
		}
		if got := props(&e)["code"]; rec.Code != status || got != code {
			t.Errorf("answer %d %v, want %d %s: %s", rec.Code, got, status, code, rec.Body.String())
		}
		if files, err := os.ReadDir(dir); err != nil || len(files) != 0 {
			t.Errorf("the snapshot folder after the error: %d files, %v", len(files), err)
		}
		message, _ = props(&e)["message"].(string)
		return message
	}
	t.Run("a refusal", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(repo, "backups")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		snapshot(t, t.Context(), dir, 422, "snapshot_refused")
	})
	t.Run("a timeout", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer cancel()
		if msg := snapshot(t, ctx, t.TempDir(), 504, "timeout"); msg == "internal error" || !strings.Contains(msg, context.DeadlineExceeded.Error()) {
			t.Errorf("the message of a 504 is %q, want the text of its cause (%s)", msg, context.DeadlineExceeded)
		}
	})
	t.Run("a failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if msg := snapshot(t, ctx, t.TempDir(), 500, "internal"); msg != "internal error" {
			t.Errorf("the message of a 500 is %q, want internal error", msg)
		}
	})
}

// TestASnapshotIsDownloadedByTheOwner: the answer of a snapshot links to its download, which sends the file as it was
// written. Only a good snapshot's name in the snapshot folder is sent: not the live file, not a failed copy, not a
// path. An agent is refused.
func TestASnapshotIsDownloadedByTheOwner(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	clock := func() time.Time { return time.Date(2026, 10, 2, 14, 30, 5, 0, time.UTC) }
	h := api.New(&core.Store{DB: d, SnapshotDir: dir, Clock: clock, Zone: time.UTC, SettingsPath: filepath.Join(t.TempDir(), "life.settings.json")})
	owner := client.InProcess(h, "cli")
	e := apitest.Must(t)(owner.Do(t.Context(), apitest.Action(t, apitest.Must(t)(owner.Get(t.Context(), "/")), "snapshot"), nil))
	if got := apitest.Href(e, "download"); got != "/snapshots/life-2026-10-02.db" {
		t.Fatalf("the download link: %q", got)
	}
	if got := e.Properties.(map[string]any)["download"]; got != "/snapshots/life-2026-10-02.db" {
		t.Fatalf("the download property: %v", got)
	}
	want, err := os.ReadFile(filepath.Join(dir, "life-2026-10-02.db"))
	if err != nil {
		t.Fatal(err)
	}
	get := func(path, src string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if src != "" {
			req.Header.Set(siren.SourceHeader, src)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := get("/snapshots/life-2026-10-02.db", "")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/vnd.sqlite3" ||
		rec.Header().Get("Content-Disposition") != `attachment; filename=life-2026-10-02.db` {
		t.Fatalf("the download: %d %v", rec.Code, rec.Header())
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("the download sent %d bytes, the file has %d", rec.Body.Len(), len(want))
	}
	if rec := get("/snapshots/life-2026-10-02.db", "agent:lmstudio"); rec.Code != 403 {
		t.Errorf("an agent's download: %d, want 403", rec.Code)
	}

	// Files beside the snapshots that are not good snapshots, and names that are not snapshot names, are 404.
	for _, name := range []string{"life.db", "life-2026-10-01.failed.db", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "life-2026-10-03.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/snapshots/life.db", "/snapshots/life-2026-10-01.failed.db", "/snapshots/notes.txt",
		"/snapshots/life-2026-10-03.db", "/snapshots/life-2026-10-04.db", "/snapshots/..%2Flife-2026-10-02.db"} {
		if rec := get(path, ""); rec.Code != 404 {
			t.Errorf("GET %s: %d, want 404", path, rec.Code)
		}
	}
}
