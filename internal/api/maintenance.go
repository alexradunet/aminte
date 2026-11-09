package api

// The database as a whole: the integrity checks, the owner's snapshot and the owner's repairs of derived data.

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"

	"aminte/internal/core"
	"aminte/siren"
)

// snapshot is the owner's action (docs/plans/082-snapshot-in-the-catalog.md): the file lands on this machine, in
// the store's folder (named life-DATE.failed.db when its restore check fails); the answer is the path and the
// restore check, with self at the root. Core reports a refusal as 422 snapshot_refused; a timeout and a failure are
// answered as on every route.
func (h *server) snapshot(r *http.Request, _ admitted) (*siren.Entity, error) {
	path, res, err := h.s.TakeSnapshot(r.Context(), h.s.Local())
	if err != nil {
		return nil, err
	}
	if !res.OK { // the file is kept, as the check does not delete, under a .failed.db name (D25): the owner reads it
		return nil, &core.Error{Status: http.StatusInternalServerError, Code: siren.CodeRestoreCheckFailed,
			Msg: fmt.Sprintf("%s failed its restore check: it is kept under this .failed.db name, and it is not one to restore", path)}
	}
	download := snapshotHref(filepath.Base(path))
	props := map[string]any{"snapshot": path, "download": download, "restore_check": res}
	return &siren.Entity{Class: []string{"snapshot"}, Title: "Snapshot", Properties: props, Result: props,
		Links: []siren.Link{link("self", "/", "Home"), link("index", "/", "Home"), link("download", download, "Download the snapshot")}}, nil
}

// snapshotHref is the address of the download of the snapshot named name.
func snapshotHref(name string) string { return "/snapshots/" + url.PathEscape(name) }

// snapshotFile is GET /snapshots/{name}: the owner's download of a snapshot that the action wrote, so a browser on
// another device keeps a copy of life.db. It is the copy, never the live file: a copy has no -wal beside it and passed
// its restore check. Like GET /integrity it is the owner's read: an agent or an import is refused.
func (h *server) snapshotFile(w http.ResponseWriter, r *http.Request) {
	if err := requireOwner(source(r)); err != nil {
		writeError(w, r, err)
		return
	}
	name := r.PathValue("name")
	f, err := h.s.OpenSnapshot(name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

func (h *server) integrity(r *http.Request, _ admitted) (*siren.Entity, error) {
	res, err := h.s.Integrity(r.Context())
	if err != nil {
		return nil, err
	}
	// The owner's repairs are offered where the owner reads what needs them.
	return &siren.Entity{Class: []string{"integrity"}, Title: "Integrity checks", Properties: res,
		Links:   []siren.Link{link("self", "/integrity", "Integrity"), link("index", "/", "Home")},
		Actions: []siren.Action{action("rebuild-search", nil, nil), action("resync-links", nil, nil)}}, nil
}

// rebuildSearch is the owner's repair of the full-text index (docs/contract/integrity-checks.md). The answer is the
// number of documents of the new index, with self at the root.
func (h *server) rebuildSearch(r *http.Request, _ admitted) (*siren.Entity, error) {
	documents, err := h.s.RebuildSearch(r.Context())
	if err != nil {
		return nil, err
	}
	props := map[string]any{"documents": documents}
	return &siren.Entity{Class: []string{"rebuilt"}, Title: "Search rebuilt", Properties: props, Result: props,
		Links: []siren.Link{link("self", "/", "Home"), link("index", "/", "Home")}}, nil
}

// resyncLinks is the owner's repair of the links of the bodies (docs/contract/integrity-checks.md). The answer lists
// the pages rewritten and what the rewrite made; self is the root, as for the other repairs.
func (h *server) resyncLinks(r *http.Request, a admitted) (*siren.Entity, error) {
	res, err := h.s.ResyncLinks(r.Context(), a.src)
	if err != nil {
		return nil, err
	}
	return &siren.Entity{Class: []string{"resynced"}, Title: "Links resynced", Properties: res, Result: res,
		Links: []siren.Link{link("self", "/", "Home"), link("index", "/", "Home")}}, nil
}
