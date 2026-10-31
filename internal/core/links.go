package core

import (
	"context"
	"database/sql"
	"errors"

	"lifelog/text"
)

// Sync is what a body save did to the page's wikilinks (docs/contract/titles-and-wikilinks.md: the UI reports
// skipped targets and tells the owner about a revived page).
type Sync struct {
	Linked  []string `json:"linked"`
	Added   []string `json:"added,omitempty"` // the linked titles the page did not link before this save
	Created []string `json:"created,omitempty"`
	Revived []string `json:"revived,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
}

// wikilinkIDs are the ids of the pages that the wikilink rows of the page id point to.
func wikilinkIDs(ctx context.Context, q queryer, id int64) (map[int64]bool, error) {
	targets := map[int64]bool{}
	var to int64
	rows, err := q.QueryContext(ctx, `SELECT to_id FROM links WHERE from_id = ? AND kind = 'wikilink'`, id)
	if err := each(rows, err, []any{&to}, func() { targets[to] = true }); err != nil {
		return nil, err
	}
	return targets, nil
}

// syncWikilinks is cookbook/save-a-body.md steps 1-4, after the body is written. A target that fails in steps 1-3 is
// skipped when its savepoint rolls it back alone. A fault of the savepoint itself fails the save: the SAVEPOINT or the
// RELEASE failed, or the savepoint could not roll back. The state of the transaction is then not known.
func (t *Tx) syncWikilinks(pageID int64, body string) (Sync, error) {
	var own string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT preferred_name_key FROM entities WHERE id = ?`, pageID).Scan(&own); err != nil {
		return Sync{}, err
	}
	keys, titles, rejected := text.Targets(body, own)
	r := Sync{Linked: []string{}, Skipped: rejected}
	before, err := wikilinkIDs(t.ctx, t.tx, pageID)
	if err != nil {
		return Sync{}, err
	}
	ids := []int64{}
	seen := map[int64]bool{pageID: true}
	for i, key := range keys {
		title := titles[i]
		var id int64
		var created, revived bool
		skipped, fault := t.savepoint(func() (err error) {
			id, created, revived, err = t.linkTarget(pageID, key, title)
			return err
		})
		if fault != nil {
			return Sync{}, fault
		}
		if skipped != nil {
			r.Skipped = append(r.Skipped, title)
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		r.Linked = append(r.Linked, title)
		if !before[id] {
			r.Added = append(r.Added, title)
		}
		if created {
			r.Created = append(r.Created, title)
		}
		if revived {
			r.Revived = append(r.Revived, title)
		}
	}
	_, err = t.tx.ExecContext(t.ctx, `DELETE FROM links WHERE from_id = ? AND kind = 'wikilink'
	                       AND to_id NOT IN (SELECT value FROM json_each(?))`, pageID, jsonArray(ids))
	return r, err
}

func (t *Tx) linkTarget(pageID int64, key, title string) (id int64, created, revived bool, err error) {
	if id, created, revived, err = t.ensurePage(title, key, dayOfTitle(title)); err != nil || id == pageID {
		return id, false, false, err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'wikilink', `+nowSQL+`, ?)
	                    ON CONFLICT(from_id, to_id, kind) DO NOTHING`, pageID, id, t.source)
	return id, created, revived, err
}

// dayOfTitle is a link target's day: none, except a day page (entities_day_page).
func dayOfTitle(title string) any {
	if text.IsDay(title) {
		return title
	}
	return nil
}

// link adds an edge of a registered kind in t (D8); added reports whether the link is new.
func (t *Tx) link(from, to int64, kind, note string) (added bool, err error) {
	if kind == "wikilink" {
		return false, Invalid("wikilink links are written by saving a body, not directly")
	}
	var one int
	err = t.tx.QueryRowContext(t.ctx, `SELECT 1 FROM entities WHERE id = ?`, from).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, NotFound("no page %d", from) // a missing row is a 404, not the foreign key's 422
	}
	if err != nil {
		return false, err
	}
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO links(from_id, to_id, kind, note, created_at, source) VALUES (?, ?, ?, ?, `+nowSQL+`, ?)
	                       ON CONFLICT(from_id, to_id, kind) DO NOTHING`, from, to, kind, nullIfEmpty(note), t.source)
	if err != nil {
		return false, err // Do turns a refusal of the registry's endpoint types into its 422
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Edge is a link seen from one end; ID and Title are the other end.
type Edge struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"entity_type"`
	Note  string `json:"note,omitempty"`
	// Context is the sentence of the other page that names this one, for a wikilink backlink (text.Mention).
	Context string `json:"context,omitempty"`
}

func (s *Store) edges(ctx context.Context, q string, args ...any) ([]Edge, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	return list(rows, err, func(e *Edge) []any { return []any{&e.Kind, &e.ID, &e.Title, &e.Type, &e.Note} })
}

// addContexts sets Edge.Context of each wikilink edge in in, the backlinks of page id: the sentence of the source that
// names the page. The names are every name that the page has kept, as a link by an old name still ends at the page.
// The bodies of all the sources come in one query.
func (s *Store) addContexts(ctx context.Context, id int64, in []Edge) error {
	var sources []int64
	for _, e := range in {
		if e.Kind == "wikilink" {
			sources = append(sources, e.ID)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT name_key FROM entity_names WHERE entity_id = ?`, id)
	keys, err := list(rows, err, func(k *string) []any { return []any{k} })
	if err != nil {
		return err
	}
	rows, err = s.DB.R.QueryContext(ctx, `SELECT id, body FROM entities WHERE id IN (SELECT value FROM json_each(?))`, jsonArray(sources))
	contexts := make(map[int64]string, len(sources))
	var from int64
	var body string
	if err := each(rows, err, []any{&from, &body}, func() { contexts[from] = text.Mention(body, keys) }); err != nil {
		return err
	}
	for i := range in {
		if in[i].Kind == "wikilink" {
			in[i].Context = contexts[in[i].ID]
		}
	}
	return nil
}

// LinkKindsFrom are the registered kinds a page of this type may start (link_kinds.from_types, NULL for any),
// the save contract's wikilink excluded, in kind order.
func (s *Store) LinkKindsFrom(ctx context.Context, typ string) ([]string, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind FROM link_kinds WHERE kind <> 'wikilink'
	    AND (from_types IS NULL OR instr(',' || from_types || ',', ',' || ? || ',') > 0) ORDER BY kind`, typ)
	return list(rows, err, func(k *string) []any { return []any{k} })
}
