package core

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"lifelog/text"
)

// insertPage is the universal insert convention (cookbook/capture.md): entities first, RETURNING id, then the
// owned-name row with that id. The writer sets no import_key on an entity.
func (t *Tx) insertPage(typ, title, key string, day any, body string) (id int64, err error) {
	title = periodSpelling(title, key)
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO entities(entity_type, preferred_name_key, day, body, created_at, updated_at, source)
                         VALUES (?, ?, ?, ?, `+nowSQL+`, `+nowSQL+`, ?)
	                     RETURNING id`, typ, key, day, body, t.source).Scan(&id)
	if err != nil {
		return 0, err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, title, key)
	return id, err
}

// periodKey is the shape of a period key, folded (entities.period, D30): a year, a month, a quarter or an ISO week. The
// file decides whether the period exists (entities_period_exists).
var periodKey = regexp.MustCompile(`^[0-9]{4}(-[0-9]{2}|-q[0-9]|-w[0-9]{2})?$`)

// periodSpelling is the title that the file accepts for the key: a period page is spelled in upper case (D30), so
// 2026-w43 is written 2026-W43. Any other title stays as it is.
func periodSpelling(title, key string) string {
	if periodKey.MatchString(key) {
		return strings.ToUpper(key)
	}
	return title
}

// pageRef is what a lookup by title or a livePage finds. Version is the decimal entities.revision token (Page.Version).
// Body is empty unless lookupWithBody found the page: no other lookup reads the body.
type pageRef struct {
	ID      int64
	Type    string
	Title   string
	Body    string
	Version string
	Deleted bool
}

// byKey is the condition of findPage for the page that holds a title key.
const byKey = `e.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`

// lookup finds the page that holds a title's key; nil when none does. It does not read the body.
func (t *Tx) lookup(title string) (*pageRef, error) {
	return t.lookupKey(text.TitleKey(title))
}

func (t *Tx) lookupKey(key string) (*pageRef, error) {
	return t.findPage(false, byKey, key)
}

// lookupWithBody is lookup with the body of the page, for a write that keeps the text of the owner: keep and
// RegisterMetric write a body only where the page has none.
func (t *Tx) lookupWithBody(title string) (*pageRef, error) {
	return t.findPage(true, byKey, text.TitleKey(title))
}

// livePage is the page with the id when it is live. It holds the one rule of a write that needs a live page, with one
// message for each answer: a missing page is a 404, a tombstoned one a 409 that names its id and its title, so the
// owner reads which page. It does not read the body.
func (t *Tx) livePage(id int64) (*pageRef, error) {
	p, err := t.findPage(false, `e.id = ?`, id)
	switch {
	case err != nil:
		return nil, err
	case p == nil:
		return nil, NotFound("no page %d", id)
	case p.Deleted:
		return nil, Conflict("page %d %q is deleted: restore it first (Restore, revive)", id, p.Title)
	}
	return p, nil
}

// findPage reads the page that the condition on the entity e selects, with its preferred title; nil when none. It reads
// the body only when body is true: body is the last column of entities, so a read without it does not read the overflow
// pages of a long body.
func (t *Tx) findPage(body bool, cond string, arg any) (*pageRef, error) {
	column := `''`
	if body {
		column = `e.body`
	}
	p := &pageRef{}
	err := t.tx.QueryRowContext(t.ctx, `SELECT e.id, e.entity_type, n.title, `+column+`, e.deleted_at IS NOT NULL,
                               CAST(e.revision AS TEXT)
                          FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
                         WHERE `+cond, arg).
		Scan(&p.ID, &p.Type, &p.Title, &p.Body, &p.Deleted, &p.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ExistsError says the title is taken and by which page, so a client can follow it (promote it, edit it).
type ExistsError struct {
	ID    int64
	Title string
	Plain bool // the page is a live plain page, so promote can make it a person
}

func (e *ExistsError) Error() string { return "a page titled " + e.Title + " already exists" }

// ensurePage is the id of the page that holds the key: found (revived when tombstoned), or created as a plain page
// with the title and the day (nil for none) on its first mention (cookbook/capture.md, cookbook/save-a-body.md).
func (t *Tx) ensurePage(title, key string, day any) (id int64, created, revived bool, err error) {
	p, err := t.lookupKey(key)
	switch {
	case err != nil:
		return 0, false, false, err
	case p == nil:
		id, err = t.insertPage("page", title, key, day, "")
		return id, true, false, err
	case p.Deleted:
		_, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET deleted_at = NULL WHERE id = ?`, p.ID)
		return p.ID, false, err == nil, err
	}
	return p.ID, false, false, nil
}

// rename is Store.Rename in t. A day page keeps its date, and a date is the name of its day page alone: the
// triggers entity_names_day_insert and entities_day_identity refuse the write. A day page renamed to its own date
// writes nothing, so no trigger sees it: rename refuses it, as step 0 of cookbook/rename-a-page.md says.
func (t *Tx) rename(id int64, newTitle string) (int64, error) {
	if _, err := t.livePage(id); err != nil {
		return 0, err
	}
	if err := checkTitle("title", newTitle); err != nil {
		return 0, err
	}
	key := text.TitleKey(newTitle)
	target, err := t.lookupKey(key)
	if err != nil {
		return 0, err
	}
	if target != nil && target.ID != id {
		return 0, &ExistsError{ID: target.ID, Title: target.Title}
	}
	// Only the day page of a date holds the date's key (entity_names_day_insert, entities_day_identity): here id is
	// that day page.
	if target != nil && text.IsDay(key) {
		return 0, Conflict("a day page's canonical date is not renamed")
	}
	if target == nil {
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, newTitle, key); err != nil {
			return 0, err
		}
	} else {
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE entity_names SET title = ? WHERE entity_id = ? AND name_key = ? AND title IS NOT ?`, newTitle, id, key, newTitle); err != nil {
			return 0, err
		}
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET preferred_name_key = ? WHERE id = ? AND preferred_name_key IS NOT ?`, key, id, key); err != nil {
		return 0, err
	}
	return id, nil
}

// Rename selects an owned preferred name, retaining previous names and the entity's identity.
// See contract/titles-and-wikilinks.md and cookbook/rename-a-page.md.
func (s *Store) Rename(ctx context.Context, source string, id int64, newTitle string) (to int64, err error) {
	return alone(s, ctx, source, func(t *Tx) (int64, error) { return t.rename(id, newTitle) })
}

// PageID resolves an owned name through the registry; 0 when no entity owns it.
func (s *Store) PageID(ctx context.Context, title string) (int64, error) {
	var id int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT entity_id FROM entity_names WHERE name_key = ?`, text.TitleKey(title)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// TitleMatch is a name of a live page that starts with a prefix: the name, and the preferred title, id and type of
// its page. For an alias the name and the title differ.
type TitleMatch struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	PageID int64  `json:"page_id"`
	Type   string `json:"entity_type"`
}

// Titles are the preferred titles of the pages of ids, in the order of ids. It reads the names only, for a sentence
// that names a few pages. An id that no page has is an error.
func (s *Store) Titles(ctx context.Context, ids []int64) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		var title string
		err := s.DB.R.QueryRowContext(ctx, `SELECT n.title FROM entities e
			JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key WHERE e.id = ?`, id).Scan(&title)
		if err != nil {
			return nil, err
		}
		out = append(out, title)
	}
	return out, nil
}

// TitlesByPrefix are the names of live pages whose name key starts with the title key of prefix, in key order, and
// at most limit of them (limit is 1 or more): what a search lists first. The read is a range on the unique index of
// name_key: every key with the prefix is at or after it, and before it followed by the byte 0xFF, which no UTF-8
// text holds.
func (s *Store) TitlesByPrefix(ctx context.Context, prefix string, limit int) ([]TitleMatch, error) {
	key := text.TitleKey(strings.TrimSpace(prefix))
	if key == "" {
		return nil, Invalid("a title prefix needs text")
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT n.title, p.title, e.id, e.entity_type
		  FROM entity_names n
		  JOIN entities e ON e.id = n.entity_id AND e.deleted_at IS NULL
		  JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
		 WHERE n.name_key >= ? AND n.name_key < ?
		 ORDER BY n.name_key LIMIT ?`, key, key+"\xff", limit)
	return list(rows, err, func(m *TitleMatch) []any { return []any{&m.Name, &m.Title, &m.PageID, &m.Type} })
}
