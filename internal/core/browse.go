package core

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// The reads of Browse (docs/plans/117-one-browse-view.md): the lists of each kind in one scope, what is filed
// directly in a category, and the count of each kind in the scope.

// browseKinds are the kinds Browse lists, in the order of its chips. A file is media when the first part of its type
// is one of MediaKinds, else a document. BrowseCounts counts each of them, so only BrowseKinds gives them out, as a
// copy.
var browseKinds = []string{"page", "person", "media", "document", "metric"}

// BrowseKinds are the kinds Browse lists (browseKinds). Each call gives a new copy: a caller that changes it does not
// change the kinds that BrowseCounts counts, or the list of the next caller.
func BrowseKinds() []string { return slices.Clone(browseKinds) }

// Scope is where a Browse list looks: everything (the zero Scope), the things filed directly in the category page In,
// or the things in no category (Unfiled).
type Scope struct {
	In      int64
	Unfiled bool
}

// where is the condition of the scope on the entity p, with its arguments. A thing is in no category when no part-of
// link starts at it and ends at a live page. A tombstone keeps its links (D11), but the tree does not draw a
// tombstoned category, so what is filed only there is in no category. A plain page is also not in that list when a
// part-of link ends at it: it is a category itself, as the tree draws it.
func (sc Scope) where() (string, []any) {
	switch {
	case sc.In != 0:
		return `EXISTS (SELECT 1 FROM links fl WHERE fl.kind = 'part-of' AND fl.from_id = p.id AND fl.to_id = ?)`, []any{sc.In}
	case sc.Unfiled:
		return `NOT EXISTS (SELECT 1 FROM links fl JOIN entities fc ON fc.id = fl.to_id AND fc.deleted_at IS NULL
		                     WHERE fl.kind = 'part-of' AND fl.from_id = p.id)
		    AND (p.entity_type <> 'page' OR NOT EXISTS (SELECT 1 FROM links fl WHERE fl.kind = 'part-of' AND fl.to_id = p.id))`, nil
	}
	return `1`, nil
}

// plainPage is the condition on p of a live plain page: a page that is not a day page.
const plainPage = `p.entity_type = 'page' AND NOT p.is_journal AND p.deleted_at IS NULL`

// isCategory is the condition on p of a category: a part-of link ends at it, as Categories reads them.
const isCategory = `EXISTS (SELECT 1 FROM links c WHERE c.kind = 'part-of' AND c.to_id = p.id)`

// browseKind is the Browse kind of the entity p, with its file f (a LEFT JOIN): its type, or media or document for a
// file.
var browseKind = `CASE WHEN p.entity_type <> 'file' THEN p.entity_type WHEN ` + isMedia + ` THEN 'media' ELSE 'document' END`

// PageRow is a live plain page as Browse lists it. Category says that it is a category: a part-of link ends at it.
// Picture is its cover (0 when it has none).
type PageRow struct {
	ID       int64
	Title    string
	Category bool
	Picture  int64
	body     string
}

// PlainPages is one page of the live plain pages in the scope, by title (the folded title, preferred_name_key, as the
// tree orders the categories). A day page is never in it: the Calendar has
// the days.
func (s *Store) PlainPages(ctx context.Context, sc Scope, limit, offset int) ([]PageRow, error) {
	where, args := sc.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, `+isCategory+`, p.body
		  FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		 WHERE `+plainPage+` AND `+where+`
		 ORDER BY p.preferred_name_key LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	out, err := list(rows, err, func(r *PageRow) []any { return []any{&r.ID, &r.Title, &r.Category, &r.body} })
	if err != nil {
		return nil, err
	}
	return out, setPictures(ctx, s.DB.R, out, func(r *PageRow) (string, *int64) { return r.body, &r.Picture })
}

// MetricRow is a live metric as Browse lists it: its page's id, its name, its unit ("" for a count or a habit) and
// its cover (Picture, 0 when it has none).
type MetricRow struct {
	ID      int64
	Title   string
	Unit    string
	Picture int64
	body    string
}

// MetricRows is one page of the live metrics in the scope, by title. The values are on the board of /metrics.
func (s *Store) MetricRows(ctx context.Context, sc Scope, limit, offset int) ([]MetricRow, error) {
	where, args := sc.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, m.unit, p.body
		  FROM metrics m JOIN entities p ON p.id = m.id
		  JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		 WHERE p.deleted_at IS NULL AND `+where+`
		 ORDER BY p.preferred_name_key LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	out, err := list(rows, err, func(r *MetricRow) []any { return []any{&r.ID, &r.Title, &r.Unit, &r.body} })
	if err != nil {
		return nil, err
	}
	return out, setPictures(ctx, s.DB.R, out, func(r *MetricRow) (string, *int64) { return r.body, &r.Picture })
}

// FiledRow is a live thing filed in a category: its id, title and type, its Browse kind (BrowseKinds), whether it
// is a category itself, the type of a file's original ("" for any other thing) and the file whose preview stands
// for it (Picture): its cover, or a file's own preview; 0 when there is none.
type FiledRow struct {
	ID       int64
	Title    string
	Type     string
	Kind     string
	Category bool
	MIME     string
	Picture  int64
	body     string
	preview  bool
}

// FiledIn is one page of every live thing filed directly in the category page with that id: the categories under
// it first, then the rest, each by title. A day page is never in it: links_endpoint_types refuses to file one (D26).
func (s *Store) FiledIn(ctx context.Context, category int64, limit, offset int) ([]FiledRow, error) {
	where, args := Scope{In: category}.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, p.entity_type, `+browseKind+`, `+isCategory+` AS category, coalesce(f.mime, ''),
		       p.body, f.preview IS NOT NULL
		  FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		  LEFT JOIN files f ON f.id = p.id
		 WHERE p.deleted_at IS NULL AND `+where+`
		 ORDER BY category DESC, p.preferred_name_key LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	out, err := list(rows, err, func(r *FiledRow) []any {
		return []any{&r.ID, &r.Title, &r.Type, &r.Kind, &r.Category, &r.MIME, &r.body, &r.preview}
	})
	if err != nil {
		return nil, err
	}
	// A file has no cover: its own preview stands for it.
	if err := setPictures(ctx, s.DB.R, out, func(r *FiledRow) (string, *int64) {
		if r.Type == "file" {
			return "", &r.Picture
		}
		return r.body, &r.Picture
	}); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].preview {
			out[i].Picture = out[i].ID
		}
	}
	return out, nil
}

// BrowseCounts counts the live things of each Browse kind in the scope, by kind (a kind with none is 0), with "all",
// the sum of the kinds, "ghosts", the rows of Ghosts, and "deleted", the tombstones (neither is in a category, so
// their counts do not change with the scope). A day page is not counted.
func (s *Store) BrowseCounts(ctx context.Context, sc Scope) (map[string]int, error) {
	out := map[string]int{"all": 0}
	for _, k := range browseKinds {
		out[k] = 0
	}
	where, args := sc.where()
	kinds, err := groupCounts(s.DB.R.QueryContext(ctx, `
		SELECT `+browseKind+`, count(*)
		  FROM entities p LEFT JOIN files f ON f.id = p.id
		 WHERE p.deleted_at IS NULL AND NOT p.is_journal AND `+where+`
		 GROUP BY 1`, args...))
	if err != nil {
		return nil, err
	}
	for k, n := range kinds {
		out[k], out["all"] = n, out["all"]+n
	}
	var ghosts int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM (`+plainGhosts+`)`).Scan(&ghosts); err != nil {
		return nil, err
	}
	out["ghosts"] = ghosts
	var deleted int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM (`+deletedRows+`)`).Scan(&deleted); err != nil {
		return nil, err
	}
	out["deleted"] = deleted
	return out, nil
}

// NamedRow is a live person with how often the log mentions it.
type NamedRow struct {
	ID    int64
	Title string
	// Mentions is the number of live pages that link to this one, of any kind. A page counts once.
	Mentions int
	// LastDay is the day of the newest live day page among those pages. It is "" when there is none.
	LastDay string
	// Picture is its cover (0 when it has none).
	Picture int64
	body    string
}

// NamedMentions is one page of the live people in the scope, by title (the folded title,
// preferred_name_key). It reads the mentions in the same query, so the cost does not grow with the number of rows.
func (s *Store) NamedMentions(ctx context.Context, sc Scope, limit, offset int) ([]NamedRow, error) {
	where, args := sc.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, count(DISTINCT lp.id),
		       coalesce(max(CASE WHEN lp.is_journal THEN lp.day END), ''), p.body
		  FROM entities p
		  JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		  LEFT JOIN links l ON l.to_id = p.id
		  LEFT JOIN entities lp ON lp.id = l.from_id AND lp.deleted_at IS NULL
		 WHERE p.entity_type = 'person' AND p.deleted_at IS NULL AND `+where+`
		 GROUP BY p.id, p_name.title
		 ORDER BY p.preferred_name_key LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	out, err := list(rows, err, func(r *NamedRow) []any { return []any{&r.ID, &r.Title, &r.Mentions, &r.LastDay, &r.body} })
	if err != nil {
		return nil, err
	}
	return out, setPictures(ctx, s.DB.R, out, func(r *NamedRow) (string, *int64) { return r.body, &r.Picture })
}

// mediaKinds are the kinds of file the media list of Browse shows, named by the first part of files.mime, in the
// order the list counts them. A file of any other kind (a PDF, a scan's text) is a document (OtherFiles). isMedia is
// built from them once, so only MediaKinds gives them out, as a copy.
var mediaKinds = []string{"image", "video", "audio"}

// MediaKinds are the kinds of file the media list of Browse shows (mediaKinds). Each call gives a new copy: a caller
// that changes it does not change the list of isMedia.
func MediaKinds() []string { return slices.Clone(mediaKinds) }

// FileRow is a live file the owner keeps (docs/cookbook/keep-a-file.md), as the media and the documents of Browse
// show it.
type FileRow struct {
	ID    int64
	Title string
	MIME  string
	// Picture says the file keeps a preview: a photo's own, or the frame a video was sent with.
	Picture bool
	// Day is the file's day (entities.day: the day given, else a JPEG's EXIF day; a photo with neither has none; any
	// other file has the day it was kept), else the day of the newest live day page that links to it (a photo kept with
	// its capture day unknown, then shown in a day page); "" when it has neither.
	Day string
	// Mentions is the number of live pages that link to this one, of any kind. A page counts once.
	Mentions int
}

// mediaKind is the first part of files.mime: 'image' of 'image/jpeg'. files_mime guarantees the slash.
const mediaKind = `substr(f.mime, 1, instr(f.mime, '/') - 1)`

// isMedia is the condition on the file f of a file of the media kinds (mediaKinds).
var isMedia = mediaKind + ` IN ('` + strings.Join(mediaKinds, `', '`) + `')`

// Media is one page of the live files of the media kinds in the scope: all of them, or the one kind named. Newest
// day first (FileRow.Day), then the newest kept.
func (s *Store) Media(ctx context.Context, kind string, sc Scope, limit, offset int) ([]FileRow, error) {
	return s.filesWhere(ctx, `(`+isMedia+`) AND (? = '' OR `+mediaKind+` = ?)`, []any{kind, kind}, sc, limit, offset)
}

// OtherFiles is one page of the live files of every other kind (a PDF, a text) in the scope: the documents, in the
// order of Media.
func (s *Store) OtherFiles(ctx context.Context, sc Scope, limit, offset int) ([]FileRow, error) {
	return s.filesWhere(ctx, `NOT (`+isMedia+`)`, nil, sc, limit, offset)
}

// Pictures is one page of the live files that keep a picture, of any kind, in the order of Media: the covers a page
// may take (SetCoverOf).
func (s *Store) Pictures(ctx context.Context, limit, offset int) ([]FileRow, error) {
	return s.filesWhere(ctx, `f.preview IS NOT NULL`, nil, Scope{}, limit, offset)
}

// filesWhere is one page of the live files in the scope that meet cond, a condition on the file f with its
// arguments, in the order of Media.
func (s *Store) filesWhere(ctx context.Context, cond string, condArgs []any, sc Scope, limit, offset int) ([]FileRow, error) {
	where, args := sc.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, f.mime, f.preview IS NOT NULL,
		       coalesce(p.day, max(CASE WHEN lp.is_journal THEN lp.day END), '') AS day,
		       count(DISTINCT lp.id)
		  FROM entities p
		  JOIN files f ON f.id = p.id
		  JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		  LEFT JOIN links l ON l.to_id = p.id
		  LEFT JOIN entities lp ON lp.id = l.from_id AND lp.deleted_at IS NULL
		 WHERE p.deleted_at IS NULL AND `+cond+` AND `+where+`
		 GROUP BY p.id, p_name.title, f.mime, f.preview IS NOT NULL
		 ORDER BY day DESC, p.created_at DESC, p.id DESC LIMIT ? OFFSET ?`, append(append(condArgs, args...), limit, offset)...)
	return list(rows, err, func(r *FileRow) []any { return []any{&r.ID, &r.Title, &r.MIME, &r.Picture, &r.Day, &r.Mentions} })
}

// MediaCounts counts the live files of each media kind in the scope, by kind; a kind with none is absent.
func (s *Store) MediaCounts(ctx context.Context, sc Scope) (map[string]int, error) {
	where, args := sc.where()
	return groupCounts(s.DB.R.QueryContext(ctx, `SELECT `+mediaKind+`, count(*) FROM files f JOIN entities p ON p.id = f.id
		WHERE p.deleted_at IS NULL AND `+isMedia+` AND `+where+` GROUP BY 1`, args...))
}

// Ghost is a row of the ghost_pages view (cookbook/ghost-pages.md).
type Ghost struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// plainGhosts are the rows of the ghost_pages view that are not day pages. The view has an empty old day page with no
// links too, but a day page is never in Browse: the Calendar has the days (docs/plans/117-one-browse-view.md).
const plainGhosts = `SELECT g.id, g.title, g.created_at FROM ghost_pages g JOIN entities e ON e.id = g.id WHERE NOT e.is_journal`

// Ghosts is one page of the ghost_pages view, a day page left out (plainGhosts), oldest first.
func (s *Store) Ghosts(ctx context.Context, limit, offset int) ([]Ghost, error) {
	rows, err := s.DB.R.QueryContext(ctx, plainGhosts+` ORDER BY g.created_at, g.id LIMIT ? OFFSET ?`, limit, offset)
	return list(rows, err, func(g *Ghost) []any { return []any{&g.ID, &g.Title, &g.CreatedAt} })
}

// Deleted is a tombstone as Browse lists it, so it can be restored from its page: its id, its title, its type
// (entities.entity_type) and the instant it was deleted.
type Deleted struct {
	ID        int64
	Title     string
	Type      string
	DeletedAt string
}

// deletedRows are the tombstones, a day page among them: a deleted day page is restored from its page as any other.
const deletedRows = `SELECT p.id, p_name.title, p.entity_type, p.deleted_at
	FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
	WHERE p.deleted_at IS NOT NULL`

// DeletedPages is one page of the tombstones, the newest deletion first.
func (s *Store) DeletedPages(ctx context.Context, limit, offset int) ([]Deleted, error) {
	rows, err := s.DB.R.QueryContext(ctx, deletedRows+` ORDER BY p.deleted_at DESC, p.id DESC LIMIT ? OFFSET ?`, limit, offset)
	return list(rows, err, func(d *Deleted) []any { return []any{&d.ID, &d.Title, &d.Type, &d.DeletedAt} })
}

// RandomDay is the day of a live day page picked at random, up to today: a future day page is a plan, not a day to
// rediscover (docs/plans/130-the-explore-pass.md). A 404 when there is none.
func (s *Store) RandomDay(ctx context.Context) (string, error) {
	var day string
	err := s.DB.R.QueryRowContext(ctx, `SELECT p.day FROM entities p WHERE p.is_journal AND p.deleted_at IS NULL AND p.day <= ?
		ORDER BY random() LIMIT 1`, s.Today()).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return "", NotFound("no day page yet")
	}
	return day, err
}

// RandomThing is the id of a live thing of a Browse kind (BrowseKinds, or "all" for any of them) picked at random. It
// is never a day page, as Browse holds none, and never a ghost page, which has nothing to show. A kind with nothing in
// it, or that is not one, is a 404: the options of the catalog of the API refuse a kind that is not one, once for every
// surface.
func (s *Store) RandomThing(ctx context.Context, kind string) (int64, error) {
	var id int64
	err := s.DB.R.QueryRowContext(ctx, `
		SELECT p.id FROM entities p LEFT JOIN files f ON f.id = p.id
		 WHERE p.deleted_at IS NULL AND NOT p.is_journal AND (?1 = 'all' OR `+browseKind+` = ?1)
		   AND p.id NOT IN (SELECT id FROM (`+plainGhosts+`))
		 ORDER BY random() LIMIT 1`, kind).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, NotFound("nothing of the kind %s yet", kind)
	}
	return id, err
}
