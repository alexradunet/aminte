package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"

	"lifelog/text"
)

// Resync is what ResyncLinks did: the live pages whose links it rewrote, by id, and what rewriting them made, revived
// or skipped (Sync says each).
type Resync struct {
	Pages   []int64  `json:"pages"`
	Created []string `json:"created,omitempty"`
	Revived []string `json:"revived,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
}

// ResyncLinks rewrites the wikilinks of every live page whose links differ from its body (the link drift of
// Integrity), by the code of a body save (syncWikilinks): the body is the truth (D19). A page whose links are right is
// not written. A target page that the body names and no page has is made, and a deleted one is revived, as a save
// does. The drift is read first, outside the write transaction, and the rewrite reads each body again, so a page that
// changed meanwhile is synced with its new body. source is the writer of the rows made.
func (s *Store) ResyncLinks(ctx context.Context, source string) (*Resync, error) {
	ctx, cancel := context.WithTimeout(ctx, integrityDeadline)
	defer cancel()
	tx, err := s.DB.R.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("resync links: %w", err)
	}
	defer tx.Rollback() // a read transaction has nothing to keep
	drifted, err := driftedLinkPages(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("resync links: %w", err)
	}
	r := &Resync{Pages: []int64{}}
	if len(drifted) == 0 {
		return r, nil
	}
	err = s.Do(ctx, source, func(t *Tx) error {
		for _, id := range drifted {
			var body string
			err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ? AND deleted_at IS NULL`, id).Scan(&body)
			if errors.Is(err, sql.ErrNoRows) {
				continue // deleted since the read: a deleted page keeps its links
			}
			if err != nil {
				return err
			}
			sync, err := t.syncWikilinks(id, body)
			if err != nil {
				return err
			}
			r.Pages = append(r.Pages, id)
			r.Created = append(r.Created, sync.Created...)
			r.Revived = append(r.Revived, sync.Revived...)
			r.Skipped = append(r.Skipped, sync.Skipped...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// linkDriftBatch is the number of bodies read at once: a body can be long, and the pages of a lifetime are many.
const linkDriftBatch = 500

// driftedLinkPages lists, by id, the live pages whose wikilink rows are not the links of their bodies. The links of a
// body are the distinct targets that text.Targets extracts, each resolved to the page that holds its name (a deleted
// page too), the page itself left out: the set that syncWikilinks writes when no target is skipped. A target that no
// page has is a difference, since a save would make the page. A page that a save skips as a target stays in the list
// until its body changes. Go holds this check because the extraction of wikilinks is the writer's algorithm
// (docs/contract/titles-and-wikilinks.md); SQL cannot extract them.
func driftedLinkPages(ctx context.Context, q *sql.Tx) ([]int64, error) {
	drifted := []int64{}
	var after int64
	for {
		type page struct {
			id        int64
			body, own string
		}
		var batch []page
		var p page
		rows, err := q.QueryContext(ctx, `SELECT id, body, preferred_name_key FROM entities WHERE deleted_at IS NULL AND id > ? ORDER BY id LIMIT ?`, after, linkDriftBatch)
		if err := each(rows, err, []any{&p.id, &p.body, &p.own}, func() { batch = append(batch, p) }); err != nil {
			return nil, fmt.Errorf("link drift: %w", err)
		}
		for _, p := range batch {
			differs, err := linksDiffer(ctx, q, p.id, p.body, p.own)
			if err != nil {
				return nil, fmt.Errorf("link drift: %w", err)
			}
			if differs {
				drifted = append(drifted, p.id)
			}
		}
		if len(batch) < linkDriftBatch {
			return drifted, nil
		}
		after = batch[len(batch)-1].id
	}
}

// linksDiffer says whether the wikilink rows of the page differ from the links of its body (driftedLinkPages).
func linksDiffer(ctx context.Context, q *sql.Tx, id int64, body, own string) (bool, error) {
	keys, _, _ := text.Targets(body, own)
	want := map[int64]bool{}
	for _, key := range keys {
		var target int64
		err := q.QueryRowContext(ctx, `SELECT entity_id FROM entity_names WHERE name_key = ?`, key).Scan(&target)
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if target != id {
			want[target] = true
		}
	}
	have, err := wikilinkIDs(ctx, q, id)
	if err != nil {
		return false, err
	}
	return !maps.Equal(want, have), nil
}
