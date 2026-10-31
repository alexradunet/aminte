package core

import (
	"context"
	"database/sql"
	"errors"
	"mime"
	"strings"

	"lifelog/internal/preview"
	"lifelog/text"
)

// FileIn is a file to keep (docs/cookbook/keep-a-file.md, D9): the hash and type of the original, which is never
// stored; the title and text of its page; its preview, a JPEG preview.Make made (nil for none). Its day is Day when
// the caller gives one, else Taken, else today for ordinary files. Unknown-capture mode follows
// docs/cookbook/keep-a-file.md: a photo with no day of its own has no day, and goes in no day page. dayOf finds that
// mode for a photo (an image/* type); setCover gives it to a file of any type (unknownCaptureDay). A photo's
// position is never read (D21).
type FileIn struct {
	unknownCaptureDay bool // no day of its own, whatever the type: only setCover sets it (dayOf)
	Title             string
	SHA256            string
	MIME              string
	Body              string
	Day               string
	Taken             string
	Preview           []byte
}

// Kept is what keeping a file did.
type Kept struct {
	StoredDay    string `json:"stored_day,omitempty"`    // Existing attribution, not new capture evidence.
	ID           int64  `json:"id,omitempty"`            // none in a dry run that would create the page
	Existing     bool   `json:"existing,omitempty"`      // the original was kept already: that page, nothing else written
	Deleted      bool   `json:"deleted,omitempty"`       // ...and it is tombstoned, so not even a preview was added
	PreviewAdded bool   `json:"preview_added,omitempty"` // a file kept without a preview got this one
	Promoted     bool   `json:"promoted,omitempty"`      // a plain page of the title (a ghost an embed made) became the file
	Sync         *Sync  `json:"sync,omitempty"`          // the links its text names; none when nothing was written
	DaySync      *Sync  `json:"day_sync,omitempty"`      // the links synchronized when appending to its day
	Day          string `json:"day,omitempty"`           // its own day: the caller's, or the one its metadata gives
	Embedded     bool   `json:"embedded,omitempty"`      // it was appended to its day's page
}

// dayKind says what the day of a file kept means.
type dayKind int

const (
	dayOwn  dayKind = iota // the day the caller gave, else the one the metadata gives: the file belongs to that day
	dayKept                // no day of its own: the file has the day it is kept, and belongs to no day page
	dayNone                // unknown capture: the file has no day, and goes in no day page
)

// fileDay is the day of a file kept, with what the day means. A dayNone file has no day: Day is "".
type fileDay struct {
	Day  string
	Kind dayKind
}

// own says whether the file belongs to its day: only then ownDay reports the day and embeds the picture.
func (d fileDay) own() bool { return d.Kind == dayOwn }

// dayOf finds the day of the file f. Unknown capture is explicit (unknownCaptureDay, set by setCover for a file of
// any type) or found here for a photo (an image/* type) with neither Day nor Taken.
func (t *Tx) dayOf(f FileIn) fileDay {
	photoWithNoDay := strings.HasPrefix(f.MIME, "image/") && f.Day == "" && f.Taken == ""
	switch {
	case f.unknownCaptureDay || photoWithNoDay:
		return fileDay{Kind: dayNone}
	case f.Day != "":
		return fileDay{Day: f.Day, Kind: dayOwn}
	case f.Taken != "":
		return fileDay{Day: f.Taken, Kind: dayOwn}
	}
	return fileDay{Day: t.today(), Kind: dayKept}
}

// AddFile keeps a file: the page the original's hash names already, or a new file page, or a plain page of the title
// promoted to the file (docs/cookbook/keep-a-file.md); then its own day (ownDay).
func (t *Tx) AddFile(f FileIn) (Kept, error) {
	// the hash and the type are normalized here and held by files_sha256 and files_mime; the days by entities_day
	f.SHA256 = strings.ToLower(strings.TrimSpace(f.SHA256))
	f.MIME = NormalMIME(f.MIME)
	if f.Preview != nil {
		if err := preview.Check(f.Preview); err != nil {
			return Kept{}, Invalid("%v", err)
		}
	}
	d := t.dayOf(f)
	if k, found, err := t.keepKept(f, d); found || err != nil {
		return k, err
	}
	return t.keepNew(f, d)
}

// keepKept handles an original that is kept already: that page, nothing written but a missing preview and its embed
// in its own day. found is false when the original is not kept.
func (t *Tx) keepKept(f FileIn, d fileDay) (k Kept, found bool, err error) {
	var hasPreview bool
	var deleted sql.NullString
	var title string
	var storedDay sql.NullString
	err = t.tx.QueryRowContext(t.ctx, `SELECT f.id, f.preview IS NOT NULL, p.deleted_at, p_name.title,p.day FROM files f JOIN entities p ON p.id = f.id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key  WHERE f.sha256 = ?`, f.SHA256).
		Scan(&k.ID, &hasPreview, &deleted, &title, &storedDay)
	if errors.Is(err, sql.ErrNoRows) {
		return k, false, nil
	}
	if err != nil {
		return k, true, err
	}
	k.Existing, k.Deleted = true, deleted.Valid
	if d.Kind == dayNone {
		k.StoredDay = storedDay.String
	}
	if k.Deleted {
		return k, true, nil
	}
	if d.own() && (hasPreview || f.Preview != nil) {
		if err := t.preflightEmbed(d.Day, title); err != nil {
			return k, true, err
		}
	}
	if !hasPreview && f.Preview != nil {
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE files SET preview = ? WHERE id = ? AND preview IS NULL`, f.Preview, k.ID); err != nil {
			return k, true, err
		}
		k.PreviewAdded = true
	}
	return k, true, t.ownDay(&k, ownDayFacts{Title: title, Day: d, Picture: hasPreview || k.PreviewAdded})
}

// keepNew keeps an original that is not kept yet: its page is new, or a plain page of the title promoted; then the
// files row, the links its text names and its own day.
func (t *Tx) keepNew(f FileIn, d fileDay) (k Kept, err error) {
	if err := checkTitle("title", f.Title); err != nil {
		return k, err
	}
	p, err := t.lookupWithBody(f.Title)
	if err != nil {
		return k, err
	}
	title := f.Title
	if p != nil {
		title = p.Title
	}
	if d.own() && f.Preview != nil {
		if err := t.preflightEmbed(d.Day, title); err != nil {
			return k, err
		}
	}
	body := f.Body
	switch {
	case p == nil:
		if k.ID, err = t.insertPage("file", f.Title, text.TitleKey(f.Title), nullIfEmpty(d.Day), f.Body); err != nil {
			return k, err
		}
	case p.Type == "file": // another original under a title in use: the schema cannot see it, and the 409 names the page
		return k, &ExistsError{ID: p.ID, Title: p.Title}
	default: // a day page, a person or a metric is never a file: the schema refuses the update
		if body, err = t.promoteToFile(&k, p, f, d); err != nil {
			return k, err
		}
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO files(id, sha256, mime, preview) VALUES (?, ?, ?, ?)`, k.ID, f.SHA256, f.MIME, f.Preview); err != nil {
		return k, err
	}
	sync, err := t.syncWikilinks(k.ID, body)
	if err != nil {
		return k, err
	}
	k.Sync = &sync
	return k, t.ownDay(&k, ownDayFacts{Title: title, Day: d, Picture: f.Preview != nil})
}

// promoteToFile makes the plain page p the page of the file f, and sets k to say so. It returns the text of the
// page: the text of p, or that of f when p has none. Two different texts are never merged.
func (t *Tx) promoteToFile(k *Kept, p *pageRef, f FileIn, d fileDay) (body string, err error) {
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = 'file', deleted_at = NULL WHERE id = ?`, p.ID); err != nil {
		return "", err
	}
	if p.Body != "" && f.Body != "" && p.Body != f.Body {
		return "", Conflict("%s already has text of its own: give the file another title (two texts are never merged)", p.Title)
	}
	body = f.Body
	if p.Body == "" && f.Body != "" {
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET body = ? WHERE id = ?`, f.Body, p.ID); err != nil {
			return "", err
		}
	} else {
		body = p.Body
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET day = ? WHERE id = ? AND day IS NULL`, nullIfEmpty(d.Day), p.ID); err != nil {
		return "", err
	}
	k.ID, k.Promoted = p.ID, true
	if d.Kind == dayNone {
		if err := t.tx.QueryRowContext(t.ctx, "SELECT coalesce(day,'') FROM entities WHERE id=?", p.ID).Scan(&k.StoredDay); err != nil {
			return "", err
		}
	}
	return body, nil
}

// ownDayFacts are what ownDay needs to know about a file kept.
type ownDayFacts struct {
	Title   string // the title of its page
	Day     fileDay
	Picture bool // the file has a preview
}

// ownDay reports the own day of a file kept in Kept.Day, when the file has one. When the file also has a picture,
// the page of that day shows it (embed). A file with no day of its own goes in no day page.
func (t *Tx) ownDay(k *Kept, f ownDayFacts) error {
	if !f.Day.own() {
		return nil
	}
	k.Day = f.Day.Day
	if !f.Picture {
		return nil
	}
	dayID, err := t.dayPage(f.Day.Day)
	if err != nil {
		return err
	}
	k.Embedded, k.DaySync, err = t.embed(dayID, f.Title)
	return err
}

func (s *Store) AddFile(ctx context.Context, source string, f FileIn) (Kept, error) {
	return alone(s, ctx, source, func(t *Tx) (Kept, error) { return t.AddFile(f) })
}

// File is what a file page has beyond its page.
type File struct {
	SHA256  string `json:"sha256"`
	MIME    string `json:"mime"`
	Preview bool   `json:"preview"`
}

// Preview is the picture of a file page by its id (tombstoned or not); nil when it has none.
func (s *Store) Preview(ctx context.Context, id int64) ([]byte, error) {
	return s.preview(ctx, `SELECT preview FROM files WHERE id = ?`, id)
}

// PreviewByTitle is the picture of the live file page a title names (an embed); nil when there is none.
func (s *Store) PreviewByTitle(ctx context.Context, title string) ([]byte, error) {
	return s.preview(ctx, `SELECT f.preview FROM files f JOIN entities p ON p.id = f.id
		WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND p.deleted_at IS NULL`, text.TitleKey(title))
}

func (s *Store) preview(ctx context.Context, q string, arg any) ([]byte, error) {
	var b []byte
	err := s.DB.R.QueryRowContext(ctx, q, arg).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

// NormalMIME is a type as files.mime keeps it: lowercase type/subtype, parameters dropped; "" when it does not parse.
// Its grammar is files_mime.
func NormalMIME(s string) string {
	t, _, err := mime.ParseMediaType(s)
	if err != nil {
		return ""
	}
	return t
}

// TryAddFile is AddFile in a transaction that is rolled back: what keeping the file would do, writing nothing. A page
// it would create has no id, and neither has a category it would create. A refusal gives no result, as alone does.
func (s *Store) TryAddFile(ctx context.Context, source string, f FileIn) (Kept, error) {
	var k Kept
	if err := s.dryRun(ctx, source, func(t *Tx) (err error) { k, err = t.AddFile(f); return err }); err != nil {
		return Kept{}, err
	}
	if !k.Existing {
		k.ID = 0
	}
	return k, nil
}

// embed shows a file in a day's page once: `![[title]]` appended after a blank line unless the page shows it
// already, then the page's links synced.
func (t *Tx) embed(dayID int64, title string) (bool, *Sync, error) {
	var body string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ?`, dayID).Scan(&body); err != nil {
		return false, nil, err
	}
	shown, err := t.hasEmbed(body, title)
	if err != nil {
		return false, nil, err
	}
	if shown {
		return false, nil, nil
	}
	body += func() string {
		if body == "" {
			return ""
		}
		return "\n\n"
	}() + "![[" + title + "]]"
	keys, _, _ := text.Targets(body, "")
	named := false
	for _, key := range keys {
		if key == text.TitleKey(title) {
			named = true
		}
	}
	if !text.HasEmbed(body, title) || !named {
		return false, nil, Invalid("automatic photo embed cannot render and link here; edit the day's Markdown before keeping the photo")
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET body = ? WHERE id = ?`, body, dayID)
	if err != nil {
		return false, nil, err
	}

	sync, err := t.syncWikilinks(dayID, body)
	return true, &sync, err
}

func automaticEmbedTitle(title string) error {
	mark := "![[" + title + "]]"
	keys, _, _ := text.Targets(mark, "")
	if !text.HasEmbed(mark, title) || len(keys) != 1 || keys[0] != text.TitleKey(title) {
		return Invalid("cannot automatically embed %q: give a new file an explicit representable title; an existing file's handle is not renamed", title)
	}
	return nil
}

// dayPageBody is the body of the page whose preferred key is the day: the journal day page of that day (D5). It
// finds the name by the unique index of entity_names.name_key: no index starts with entities.preferred_name_key.
const dayPageBody = `SELECT e.body FROM entity_names n JOIN entities e ON e.id = n.entity_id AND e.preferred_name_key = n.name_key WHERE n.name_key = ?`

func (t *Tx) preflightEmbed(day, title string) error {
	var body string
	err := t.tx.QueryRowContext(t.ctx, dayPageBody, day).Scan(&body)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		shown, err := t.hasEmbed(body, title)
		if err != nil {
			return err
		}
		if shown {
			return nil
		}
	}
	return automaticEmbedTitle(title)
}

func (t *Tx) hasEmbed(body, title string) (bool, error) {
	owner, err := t.lookup(title)
	if err != nil {
		return false, err
	}
	for _, target := range text.EmbeddedTitles(body) {
		if owner == nil {
			if text.TitleKey(target) == text.TitleKey(title) {
				return true, nil
			}
			continue
		}
		p, err := t.lookup(target)
		if err != nil {
			return false, err
		}
		if p != nil && p.ID == owner.ID {
			return true, nil
		}
	}
	return false, nil
}
