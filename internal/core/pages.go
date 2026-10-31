package core

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"lifelog/text"
)

// saveBody is Store.SaveBody in t. A stale version is a conflict unless the body sent is the body the page holds:
// that is a save sent again after its reply was lost, and it succeeds as the no-op it is. SetBody writes nothing
// when the body is equal, so the page keeps its version.
func (t *Tx) saveBody(id int64, body, version string) (Sync, error) {
	p, err := t.livePage(id)
	if err != nil {
		return Sync{}, err
	}
	if version != p.Version {
		var same bool
		if err := t.tx.QueryRowContext(t.ctx, `SELECT body IS ? FROM entities WHERE id = ?`, body, id).Scan(&same); err != nil {
			return Sync{}, err
		}
		if !same {
			return Sync{}, conflictCode(CodeStaleVersion, "page %d changed since version %q (now %q): read it again", id, version, p.Version)
		}
	}
	return t.SetBody(id, body)
}

// SetBody is SaveBody without the version check, for a write that has the body it sets: SaveBody after its check,
// the note of a new metric on an empty body, and the picture mark of a page.
func (t *Tx) SetBody(id int64, body string) (Sync, error) {
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET body = ? WHERE id = ? AND body IS NOT ?`, body, id, body); err != nil {
		return Sync{}, err
	}
	return t.syncWikilinks(id, body)
}

// CreatePage creates a page written on purpose; day is the day it was written ("" = today). A title that is a
// day makes that day's page. A title that is a period key makes a period page, which has no day (D30).
func (t *Tx) CreatePage(title, body, day string) (id int64, r Sync, err error) {
	var stored any = day
	switch {
	case text.IsDay(title):
		stored = title
	case periodKey.MatchString(text.TitleKey(title)):
		stored = nil
	case day == "":
		stored = t.today()
	}
	if id, err = t.createNamed("page", title, body, stored, nil); err != nil {
		return id, r, err
	}
	r, err = t.syncWikilinks(id, body)
	return id, r, err
}

// CreatePerson is cookbook/person.md's create: one id, a page and a people row.
func (t *Tx) CreatePerson(title, name, birth, death string) (int64, error) {
	if name == "" {
		name = title
	}
	return t.createNamed("person", title, "", nil, func(id int64) error {
		_, err := t.tx.ExecContext(t.ctx, `INSERT INTO people(id, name, birth_day, death_day) VALUES (?, ?, ?, ?)`,
			id, name, nullIfEmpty(birth), nullIfEmpty(death))
		return err
	})
}

// createNamed creates one entity: its row in entities with the whole body, its preferred name in entity_names, and
// then extra for its typed row. It is the one check of the title for CreatePage and CreatePerson: the
// title must be valid, and no page, live or deleted, may hold its key (the ExistsError names that page). day is the
// day of a page (for a day page, its date), nil for a person. Creation is one write, so the new entity is
// at revision 1 (lifelog_meta.edit_revisions). The caller syncs the wikilinks of the body after the typed row exists.
func (t *Tx) createNamed(typ, title, body string, day any, extra func(int64) error) (int64, error) {
	if err := checkTitle("title", title); err != nil {
		return 0, err
	}
	p, err := t.lookup(title)
	if err != nil {
		return 0, err
	}
	if p != nil {
		plain, err := t.promotable(p.ID)
		if err != nil {
			return 0, err
		}
		return 0, &ExistsError{ID: p.ID, Title: title, Plain: plain}
	}
	id, err := t.insertPage(typ, title, text.TitleKey(title), day, body)
	if err != nil || extra == nil {
		return id, err
	}
	return id, extra(id)
}

// promotable says whether a page may be offered to become a person or a metric: a live plain page that is
// not a day page or a period page (entities_day_page_plain, entities_period_page) and that nothing is filed in (a
// category: its part-of links need a plain page, entities_endpoint_types). It reads the stored columns. It is an
// offer, not a rule: a promotion that the schema refuses for another reason is still the schema's 422.
func (t *Tx) promotable(id int64) (bool, error) {
	var ok bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT e.entity_type = 'page' AND e.deleted_at IS NULL AND NOT e.is_journal AND e.period IS NULL
		   AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = e.id AND l.kind = 'part-of')
		  FROM entities e WHERE e.id = ?`, id).Scan(&ok)
	return ok, err
}

// promote is Store.Promote in t.
func (t *Tx) promote(id int64, name, birth, death string) error {
	var have string
	err := t.tx.QueryRowContext(t.ctx, `SELECT entity_type FROM entities WHERE id = ?`, id).Scan(&have)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no page %d", id)
	}
	if err != nil {
		return err
	}
	// The schema refuses a person, a metric or a file promoted to another type (entities_promote_from_page). A person
	// promoted to a person changes no type, so no rule of the schema sees it: the people insert would fail on its key.
	// The writer refuses it with this 409.
	if have == "person" {
		return Conflict("page %d is a %s, not a plain page to promote", id, have)
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = 'person', deleted_at = NULL WHERE id = ?`, id); err != nil {
		return err
	}
	if name == "" {
		p, err := t.findPage(false, `e.id = ?`, id)
		if err != nil {
			return err
		}
		name = p.Title
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO people(id, name, birth_day, death_day) VALUES (?, ?, ?, ?)`,
		id, name, nullIfEmpty(birth), nullIfEmpty(death))
	return err
}

// PersonEdit is the change that EditPerson makes. A nil field stays as it is. An empty Birth or Death removes the
// date. An empty Name is written as it is: the constraint people_name refuses it.
type PersonEdit struct{ Name, Birth, Death *string }

// editPerson is Store.EditPerson in t.
func (t *Tx) editPerson(id int64, e PersonEdit) error {
	if e.Name == nil && e.Birth == nil && e.Death == nil {
		return Invalid("send name, birth_day or death_day: an edit with no field changes nothing")
	}
	p, err := t.livePage(id)
	if err != nil {
		return err
	}
	if p.Type != "person" {
		return Conflict("page %d is a %s, not a person", id, p.Type)
	}
	var name string
	var birth, death sql.NullString
	if err := t.tx.QueryRowContext(t.ctx, `SELECT name, birth_day, death_day FROM people WHERE id = ?`, id).Scan(&name, &birth, &death); err != nil {
		return err
	}
	if e.Name != nil {
		name = *e.Name
	}
	if e.Birth != nil {
		birth = sql.NullString{String: *e.Birth, Valid: *e.Birth != ""}
	}
	if e.Death != nil {
		death = sql.NullString{String: *e.Death, Valid: *e.Death != ""}
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE people SET name = ?, birth_day = ?, death_day = ? WHERE id = ?`, name, birth, death, id)
	return err
}

// setDeleted tombstones or revives the entity (Store.Tombstone and Store.Revive in t). The same state again changes nothing.
func (t *Tx) setDeleted(id int64, deleted bool) error {
	var tombstoned bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT deleted_at IS NOT NULL FROM entities WHERE id = ?`, id).Scan(&tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no entity %d", id)
	}
	if err != nil {
		return err
	}
	if tombstoned == deleted {
		return nil
	}
	q := `UPDATE entities SET deleted_at = NULL WHERE id = ?`
	if deleted {
		q = `UPDATE entities SET deleted_at = ` + nowSQL + ` WHERE id = ?`
	}
	_, err = t.tx.ExecContext(t.ctx, q, id)
	return err
}

// SaveBody replaces a page's body and syncs its wikilinks (cookbook/save-a-body.md). version is the
// entities.revision token the caller read; the clock is audit metadata, not a version.
func (s *Store) SaveBody(ctx context.Context, source string, id int64, body, version string) (Sync, error) {
	return alone(s, ctx, source, func(t *Tx) (Sync, error) { return t.saveBody(id, body, version) })
}

// made is what a write that makes or writes a page gives, as one value for alone: the page's id and what the save of
// its body did to its wikilinks.
type made struct {
	id   int64
	sync Sync
}

// CreatePage makes the page and, with a category, files it there in the same write, as FilePages does
// (docs/plans/115-categories-to-browse.md): a day as the category refuses the whole write.
func (s *Store) CreatePage(ctx context.Context, source, title, body, category string) (int64, Sync, error) {
	m, err := alone(s, ctx, source, func(t *Tx) (made, error) {
		id, r, err := t.CreatePage(title, body, "")
		if err != nil {
			return made{}, err
		}
		return made{id, r}, t.fileNew(id, category)
	})
	return m.id, m.sync, err
}

// CreatePerson makes the person. file-pages files it in a category (docs/plans/131-the-pareto-pass.md, part 2).
func (s *Store) CreatePerson(ctx context.Context, source, title, name, birth, death string) (int64, error) {
	return alone(s, ctx, source, func(t *Tx) (int64, error) { return t.CreatePerson(title, name, birth, death) })
}

// fileNew files a page made in this write in the category titled category ("" for none), as FilePages files it:
// a missing category is made, and a day as the category refuses the whole write: entities_day_page refuses a plain
// page with a day's title, and links_endpoint_types a part-of link to a day page that exists.
func (t *Tx) fileNew(id int64, category string) error {
	if category == "" {
		return nil
	}
	_, err := t.filePages([]int64{id}, category, "", false)
	return err
}

// Promote makes a plain page a person (cookbook/person.md): the id and its links stay, a tombstoned page is
// revived. A missing id is 404; a page of another type is the schema's 422 (entities_promote_from_page), and a
// person is a 409. name, birth and death are empty when not given.
func (s *Store) Promote(ctx context.Context, source string, id int64, name, birth, death string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.promote(id, name, birth, death) })
}

// EditPerson changes the full name and the dates of a live person (cookbook/person.md). The handle, which
// is the page's title, changes with rename. A page of another type is a 409, a missing page a 404, a tombstoned one a
// 409; a bad date, an empty name or a death before the birth is the 422 of the schema (people_name,
// people_birth_day, people_death_day, people_death_day_order).
func (s *Store) EditPerson(ctx context.Context, source string, id int64, e PersonEdit) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.editPerson(id, e) })
}

// Revive clears entities.deleted_at (D11). A missing id is 404; the same state again changes nothing.
func (s *Store) Revive(ctx context.Context, source string, id int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.setDeleted(id, false) })
}

// tombstonePages is Store.Tombstone in t.
func (t *Tx) tombstonePages(ids []int64) error {
	for _, id := range ids {
		if err := t.setDeleted(id, true); err != nil {
			return err
		}
	}
	return nil
}

// Tombstone sets entities.deleted_at of every page named, in one write (D11): nothing is ever deleted. A page that
// is tombstoned already is left as it is, and an id no entity has is a 404 that writes nothing.
func (s *Store) Tombstone(ctx context.Context, source string, ids ...int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.tombstonePages(ids) })
}

// Page is one page with what surrounds it. Version is the decimal entities.revision token: pass it back to SaveBody.
type Page struct {
	ID        int64       `json:"id"`
	Type      string      `json:"entity_type"`
	Title     string      `json:"title"`
	Day       string      `json:"day,omitempty"`
	Period    string      `json:"period,omitempty"` // year, month, quarter or week for a period page (D30), read from entities.period
	Body      string      `json:"body"`
	CreatedAt string      `json:"created_at"`
	Version   string      `json:"version"`
	DeletedAt string      `json:"deleted_at,omitempty"`
	IsDayPage bool        `json:"is_day_page"`
	Person    *Person     `json:"person,omitempty"`
	Metric    *MetricInfo `json:"metric,omitempty"`
	File      *File       `json:"file,omitempty"`
	Cover     *Cover      `json:"cover,omitempty"` // nil for a file page, and for a page with no cover
	Out       []Edge      `json:"links"`
	In        []Edge      `json:"backlinks"`
}

type Person struct {
	Name  string `json:"name"`
	Birth string `json:"birth_day,omitempty"`
	Death string `json:"death_day,omitempty"`
}

// MetricInfo is what a metric's page holds beside the page: its unit and its daily mark (D31).
type MetricInfo struct {
	Unit  string `json:"unit"`
	Daily bool   `json:"daily"`
}

// PageByID reads a page, tombstoned or not (the caller decides what a tombstone shows).
func (s *Store) PageByID(ctx context.Context, id int64) (*Page, error) {
	p := &Page{ID: id}
	var day, deleted, period sql.NullString
	err := s.DB.R.QueryRowContext(ctx, `
		SELECT e.entity_type, n.title, e.day, e.period, e.body, e.created_at, CAST(e.revision AS TEXT), e.deleted_at, e.is_journal
		  FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key WHERE e.id = ?`, id).
		Scan(&p.Type, &p.Title, &day, &period, &p.Body, &p.CreatedAt, &p.Version, &deleted, &p.IsDayPage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no page %d", id)
	}
	if err != nil {
		return nil, err
	}
	p.Day, p.Period, p.DeletedAt = day.String, period.String, deleted.String
	if p.Type == "person" {
		var b, d sql.NullString
		p.Person = &Person{}
		if err := s.DB.R.QueryRowContext(ctx, `SELECT name, birth_day, death_day FROM people WHERE id = ?`, id).
			Scan(&p.Person.Name, &b, &d); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		p.Person.Birth, p.Person.Death = b.String, d.String
	}
	if p.Type == "metric" {
		p.Metric = &MetricInfo{}
		if err := s.DB.R.QueryRowContext(ctx, `SELECT unit, daily FROM metrics WHERE id = ?`, id).
			Scan(&p.Metric.Unit, &p.Metric.Daily); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if p.Type == "file" {
		p.File = &File{}
		if err := s.DB.R.QueryRowContext(ctx, `SELECT sha256, mime, preview IS NOT NULL FROM files WHERE id = ?`, id).
			Scan(&p.File.SHA256, &p.File.MIME, &p.File.Preview); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	} else if p.Cover, err = coverOf(ctx, s.DB.R, p.Body); err != nil {
		return nil, err
	}
	// outgoing edges to live entities
	if p.Out, err = s.edges(ctx, `
		SELECT l.kind, l.to_id, pg_name.title, e.entity_type, coalesce(l.note, '')
		  FROM links l JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL JOIN entity_names pg_name ON pg_name.entity_id = e.id AND pg_name.name_key = e.preferred_name_key
		 WHERE l.from_id = ? ORDER BY l.kind, pg_name.title`, id); err != nil {
		return nil, err
	}
	// cookbook/backlinks.md: owned names resolve to this id. The day pages come first, newest first.
	if p.In, err = s.edges(ctx, `
		SELECT l.kind, l.from_id, pg_name.title, e.entity_type, coalesce(l.note, '')
		  FROM links l
		  JOIN entities e  ON e.id = l.from_id AND e.deleted_at IS NULL
		  JOIN entity_names pg_name ON pg_name.entity_id = e.id AND pg_name.name_key = e.preferred_name_key
		 WHERE l.to_id = ?
		 ORDER BY e.is_journal DESC, CASE WHEN e.is_journal THEN e.day END DESC, pg_name.title`, id); err != nil {
		return nil, err
	}
	if err := s.addContexts(ctx, id, p.In); err != nil {
		return nil, err
	}
	return p, nil
}

// Hit is one full-text match (cookbook/full-text-search.md): its page, the snippet, its kind (SearchKinds) and the
// file whose preview stands for it (Picture): its cover, or a file's own preview; 0 when there is none.
type Hit struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Kind    string `json:"kind"`
	Picture int64  `json:"-"`
	body    string // read for Picture
	preview bool
}

// searchKinds are the kinds a search groups its hits by (docs/plans/130-the-explore-pass.md, part 6), in the order of
// the groups. A metric is a page: its page is words, as a page is.
var searchKinds = []string{"day", "page", "person", "file"}

// SearchKinds are the kinds of a hit (searchKinds), as a copy.
func SearchKinds() []string { return slices.Clone(searchKinds) }

// searchKind is the kind of the hit p.
const searchKind = `CASE WHEN p.is_journal THEN 'day' WHEN p.entity_type = 'metric' THEN 'page' ELSE p.entity_type END`

// hitColumns are the columns of a hit, in the order of hitFields.
const hitColumns = `p.id, p_name.title, snippet(entities_fts, 2, '⟦', '⟧', '…', 24), ` + searchKind + `, p.body, f.preview IS NOT NULL`

func hitFields(h *Hit) []any { return []any{&h.ID, &h.Title, &h.Snippet, &h.Kind, &h.body, &h.preview} }

// withPictures sets the picture of each hit: the cover of its body, or a file's own preview. One query serves the
// hits.
func (s *Store) withPictures(ctx context.Context, hits []Hit) ([]Hit, error) {
	err := setPictures(ctx, s.DB.R, hits, func(h *Hit) (string, *int64) {
		if h.Kind == "file" {
			return "", &h.Picture
		}
		return h.body, &h.Picture
	})
	for i := range hits {
		if hits[i].preview {
			hits[i].Picture = hits[i].ID
		}
	}
	return hits, err
}

// The orders of a search: best match first (the default), or newest first.
const (
	SearchBest   = "best"
	SearchNewest = "newest"
)

// SearchOrders are the values of the order of a search.
func SearchOrders() []string { return []string{SearchBest, SearchNewest} }

// SearchOrderOf is the order a request names: an empty order is the best match, and an order that is not one of the
// SearchOrders is a 422.
func SearchOrderOf(order string) (string, error) {
	switch order {
	case "":
		return SearchBest, nil
	case SearchBest, SearchNewest:
		return order, nil
	}
	return "", Invalid("order must be one of %s", strings.Join(SearchOrders(), ", "))
}

// SearchOptions are what a search may ask besides the words. Order is one of the SearchOrders. Omit are pages that
// are not hits, so the caller can list them in a group of their own and the paging counts only the pages that are left.
// Kind is one of the SearchKinds, or "" for every kind.
type SearchOptions struct {
	Order string
	Omit  []int64
	Kind  string
}

// where is the condition of the options on a hit p after the match, with its arguments: Omit and Kind.
func (o SearchOptions) where() (string, []any, error) {
	var cond string
	var args []any
	if len(o.Omit) > 0 {
		cond = " AND p.id NOT IN (?" + strings.Repeat(", ?", len(o.Omit)-1) + ")"
		for _, id := range o.Omit {
			args = append(args, id)
		}
	}
	switch {
	case o.Kind == "":
	case slices.Contains(searchKinds, o.Kind):
		cond += " AND " + searchKind + " = ?"
		args = append(args, o.Kind)
	default:
		return "", nil, Invalid("kind must be one of %s", strings.Join(searchKinds, ", "))
	}
	return cond, args, nil
}

// searchFrom is the live pages that the FTS5 query ? matches: the one definition of a hit.
const searchFrom = `
		  FROM entities_fts
		  JOIN entities p ON p.id = entities_fts.rowid JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
		  LEFT JOIN files f ON f.id = p.id
		 WHERE entities_fts MATCH ?`

// SearchWith is the hits of an FTS5 query, best first; limit and offset select one page of them. With the order newest, the hits go by the day of their page, newest first: a day
// page has the day of its title, any other page the day of its created_at in the zone of the store. Pages of one
// day keep the order of the best match, then of the id.
func (s *Store) SearchWith(ctx context.Context, q string, o SearchOptions, limit, offset int) ([]Hit, error) {
	order, err := SearchOrderOf(o.Order)
	if err != nil {
		return nil, err
	}
	omit, rest, err := o.where()
	if err != nil {
		return nil, err
	}
	args := append([]any{q}, rest...)
	if order == SearchNewest {
		return s.searchNewest(ctx, q, omit, args, limit, offset)
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT `+hitColumns+searchFrom+omit+`
		 ORDER BY rank, p.id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, invalidCause(err, "search %q: %v", q, err)
	}
	hits, err := list(rows, nil, hitFields)
	if err != nil {
		return nil, err
	}
	return s.withPictures(ctx, hits)
}

// searchNewest reads the id, the day-page day and the created_at of every match, in the order of the best match, sorts
// them by day (the sort is stable) and cuts the page. Only then does it read the title and the snippet of the
// pages of the cut. The day of a page in the zone of the store is not a SQL expression: SQLite knows the zone of the
// process only, and a daylight-saving offset changes with the instant.
func (s *Store) searchNewest(ctx context.Context, q, omit string, args []any, limit, offset int) ([]Hit, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, CASE WHEN p.is_journal THEN p.preferred_name_key ELSE '' END, p.created_at`+searchFrom+omit+`
		 ORDER BY rank, p.id`, args...)
	if err != nil {
		return nil, invalidCause(err, "search %q: %v", q, err)
	}
	type match struct {
		id  int64
		day string
	}
	var m match
	var journalDay, created string
	var matches []match
	zone := s.Local().Location()
	if err := each(rows, nil, []any{&m.id, &journalDay, &created}, func() {
		m.day = journalDay
		if t, err := time.Parse(time.RFC3339Nano, created); m.day == "" && err == nil {
			m.day = t.In(zone).Format(time.DateOnly)
		}
		matches = append(matches, m)
	}); err != nil {
		return nil, err
	}
	slices.SortStableFunc(matches, func(a, b match) int { return -strings.Compare(a.day, b.day) })
	offset = max(offset, 0)
	if limit <= 0 || offset >= len(matches) {
		return []Hit{}, nil
	}
	matches = matches[offset:min(offset+limit, len(matches))]
	ids := make([]any, len(matches))
	for i, x := range matches {
		ids[i] = x.id
	}
	rows, err = s.DB.R.QueryContext(ctx, `
		SELECT `+hitColumns+searchFrom+`
		   AND p.id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, append([]any{q}, ids...)...)
	if err != nil {
		return nil, invalidCause(err, "search %q: %v", q, err)
	}
	found, err := list(rows, nil, hitFields)
	if err != nil {
		return nil, err
	}
	if found, err = s.withPictures(ctx, found); err != nil {
		return nil, err
	}
	byID := make(map[int64]Hit, len(found))
	for _, h := range found {
		byID[h.ID] = h
	}
	out := make([]Hit, 0, len(matches))
	for _, x := range matches {
		if h, ok := byID[x.id]; ok { // a page tombstoned between the two reads is not a hit
			out = append(out, h)
		}
	}
	return out, nil
}

// AppendParagraph adds paragraph to the end of the page titled title, after a blank line, in one write. The page is
// made on the first call, written on day, with head as its first line, and filed in category when that is not empty.
// A tombstoned page is revived, as a capture revives a day page. A title that a page of another type holds is a 409.
// It returns the page's id and what the save did to the wikilinks of the body.
func (s *Store) AppendParagraph(ctx context.Context, source, title, day, category, head, paragraph string) (int64, Sync, error) {
	m, err := alone(s, ctx, source, func(t *Tx) (made, error) {
		p, err := t.lookupWithBody(title)
		if err != nil {
			return made{}, err
		}
		if p == nil {
			id, r, err := t.CreatePage(title, head+"\n\n"+paragraph, day)
			if err != nil {
				return made{}, err
			}
			// The category is the writer's own convention: a deleted one (the owner's file keeps one from an earlier
			// design) is revived, as a deleted page of the conversation is below, so the first turn is not refused.
			if c, err := t.lookup(category); err != nil {
				return made{}, err
			} else if c != nil && c.Deleted {
				if err := t.setDeleted(c.ID, false); err != nil {
					return made{}, err
				}
			}
			return made{id, r}, t.fileNew(id, category)
		}
		if p.Type != "page" {
			return made{}, Conflict("%q is a %s, not a plain page: a paragraph cannot be added to it", title, p.Type)
		}
		if p.Deleted {
			if err := t.setDeleted(p.ID, false); err != nil {
				return made{}, err
			}
		}
		body := paragraph
		if strings.TrimSpace(p.Body) != "" {
			body = strings.TrimRight(p.Body, "\r\n") + "\n\n" + paragraph
		}
		r, err := t.SetBody(p.ID, body)
		return made{p.ID, r}, err
	})
	return m.id, m.sync, err
}
