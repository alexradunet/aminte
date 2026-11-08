package core

import (
	"context"
	"regexp"
	"slices"
	"strconv"

	"aminte/text"
)

// A page's cover is the first live file with a preview that its body embeds (docs/plans/127-a-cover-for-every-page.md).
// A reader draws it above the page and in the lists. Any page has one but a file page, whose own preview is its
// picture. SetCover and SetCoverOf make a file the cover: its embed goes first in the body, or trades places with the
// cover's when the body embeds it already (showFirst). The file is kept as the owner picked it, with no day and no
// place, so a cover is not put in a day page.

// Cover is the cover of a page: the file, its title, and the embed of the body that names it, as the body writes it
// (a reader that draws the cover above the body skips that embed, so the picture is not drawn twice).
type Cover struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Embed string `json:"embed"`
}

// embeddedPictures gives the pictures of each body, in the order of bodies: the live files with a preview that the
// body embeds, each once, in the order of their first embed. The first is the body's cover. One query serves all
// bodies. A title resolves through every name of a page, as GET /previews resolves it.
func embeddedPictures(ctx context.Context, q queryer, bodies []string) ([][]Cover, error) {
	keys, embeds := []string{}, make([][]string, len(bodies))
	for i, body := range bodies {
		embeds[i] = text.EmbeddedTitles(body)
		for _, title := range embeds[i] {
			keys = append(keys, text.TitleKey(title))
		}
	}
	out := make([][]Cover, len(bodies))
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT n.name_key, f.id, pn.title FROM entity_names n
		  JOIN entities e ON e.id = n.entity_id AND e.deleted_at IS NULL
		  JOIN entity_names pn ON pn.entity_id = e.id AND pn.name_key = e.preferred_name_key
		  JOIN files f ON f.id = e.id AND f.preview IS NOT NULL
		 WHERE n.name_key IN (SELECT value FROM json_each(?))`, jsonArray(keys))
	byKey := map[string]Cover{}
	var key string
	var c Cover
	if err := each(rows, err, []any{&key, &c.ID, &c.Title}, func() { byKey[key] = c }); err != nil {
		return nil, err
	}
	for i, titles := range embeds {
		seen := map[int64]bool{}
		for _, title := range titles {
			if c, ok := byKey[text.TitleKey(title)]; ok && !seen[c.ID] {
				seen[c.ID] = true
				c.Embed = title
				out[i] = append(out[i], c)
			}
		}
	}
	return out, nil
}

// firstCovers gives the cover of each body, in the order of bodies: its first picture (embeddedPictures), else a zero
// Cover.
func firstCovers(ctx context.Context, q queryer, bodies []string) ([]Cover, error) {
	pics, err := embeddedPictures(ctx, q, bodies)
	if err != nil {
		return nil, err
	}
	out := make([]Cover, len(bodies))
	for i, p := range pics {
		if len(p) > 0 {
			out[i] = p[0]
		}
	}
	return out, nil
}

// coverOf is the cover of one body, nil when it has none.
func coverOf(ctx context.Context, q queryer, body string) (*Cover, error) {
	pics, err := embeddedPictures(ctx, q, []string{body})
	if err != nil || len(pics[0]) == 0 {
		return nil, err
	}
	return &pics[0][0], nil
}

// setPictures sets the picture of each row of a list to its cover: of gives the body of a row and the field to set.
// One query serves the list.
func setPictures[T any](ctx context.Context, q queryer, rows []T, of func(*T) (string, *int64)) error {
	bodies := make([]string, len(rows))
	for i := range rows {
		bodies[i], _ = of(&rows[i])
	}
	covers, err := firstCovers(ctx, q, bodies)
	if err != nil {
		return err
	}
	for i := range rows {
		_, picture := of(&rows[i])
		*picture = covers[i].ID
	}
	return nil
}

// SetCover keeps a photo as a file and makes it the page's cover. day names a new file when f has no title:
// "<page> <day>.jpg", with a number when that title is taken.
func (s *Store) SetCover(ctx context.Context, source string, id int64, day string, f FileIn) (Kept, error) {
	return alone(s, ctx, source, func(t *Tx) (Kept, error) { return t.setCover(id, day, f) })
}

// SetCoverOf makes a kept file, named by its title, the page's cover.
func (s *Store) SetCoverOf(ctx context.Context, source string, id int64, title string) (Sync, error) {
	return alone(s, ctx, source, func(t *Tx) (Sync, error) { return t.setCoverOf(id, title) })
}

// setCover is Store.SetCover in t.
func (t *Tx) setCover(id int64, day string, f FileIn) (Kept, error) {
	page, err := t.coverPage(id)
	if err != nil {
		return Kept{}, err
	}
	if f.Preview == nil {
		return Kept{}, Invalid("a picture: send a JPEG, PNG or GIF, or a preview of it")
	}
	f.unknownCaptureDay, f.Day, f.Taken = true, "", ""
	if f.Title == "" {
		if f.Title, err = t.freeCoverTitle(page.Title, day, f.MIME); err != nil {
			return Kept{}, err
		}
	}
	k, err := t.AddFile(f)
	if err != nil {
		return Kept{}, err
	}
	kept, err := t.findPage(false, `e.id = ?`, k.ID)
	if err != nil {
		return Kept{}, err
	}
	sync, err := t.showFirst(id, k.ID, kept.Title)
	if err != nil {
		return Kept{}, err
	}
	if sync != nil {
		k.Sync = sync
	}
	return k, nil
}

// setCoverOf is Store.SetCoverOf in t.
func (t *Tx) setCoverOf(id int64, title string) (Sync, error) {
	if _, err := t.coverPage(id); err != nil {
		return Sync{}, err
	}
	p, err := t.lookup(title)
	if err != nil {
		return Sync{}, err
	}
	var preview bool
	if p != nil && p.Type == "file" && !p.Deleted {
		if err := t.tx.QueryRowContext(t.ctx, `SELECT preview IS NOT NULL FROM files WHERE id = ?`, p.ID).Scan(&preview); err != nil {
			return Sync{}, err
		}
	}
	if !preview {
		return Sync{}, Invalid("%q is not a kept picture: keep it first (New file, add-file)", title)
	}
	sync, err := t.showFirst(id, p.ID, p.Title)
	if err != nil || sync == nil {
		return Sync{}, err
	}
	return *sync, nil
}

// coverPage is the live page with the id when it may take a cover: any page but a file, whose own picture stands for
// it.
func (t *Tx) coverPage(id int64) (*pageRef, error) {
	p, err := t.livePage(id)
	if err == nil && p.Type == "file" {
		return nil, Invalid("%q is a file: its own picture is its cover", p.Title)
	}
	return p, err
}

// freeCoverTitle is "<page> <day><ext>", or "<page> <day> 2<ext>" and on when a page has that name. A day page's
// cover is named for its day alone, "2026-10-04.jpg": its title is the day, and a day title is a day page's only (D5).
func (t *Tx) freeCoverTitle(page, day, mime string) (string, error) {
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/heic": ".heic", "image/webp": ".webp"}[NormalMIME(mime)]
	base := page + " " + day
	if text.IsDay(page) {
		base = page
	}
	for n := 1; ; n++ {
		title := base
		if n > 1 {
			title += " " + strconv.Itoa(n)
		}
		title += ext
		p, err := t.lookup(title)
		if err != nil || p == nil {
			return title, err
		}
	}
}

// showFirst makes a file the cover of a page. A file the body embeds already trades places with the cover: the two
// embeds swap, each with its label, so the body keeps every picture once (swapEmbeds). Any other file's embed goes
// first. Nothing changes when the file is the cover already.
func (t *Tx) showFirst(id, file int64, title string) (*Sync, error) {
	var body string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ?`, id).Scan(&body); err != nil {
		return nil, err
	}
	pics, err := embeddedPictures(t.ctx, t.tx, []string{body})
	if err != nil {
		return nil, err
	}
	if len(pics[0]) > 0 && pics[0][0].ID == file {
		return nil, nil
	}
	if next, ok, err := t.swapped(body, pics[0], file); err != nil {
		return nil, err
	} else if ok {
		sync, err := t.SetBody(id, next)
		return &sync, err
	}
	if err := automaticEmbedTitle(title); err != nil {
		return nil, err
	}
	mark := "![[" + title + "]]"
	if body != "" {
		mark += "\n\n"
	}
	sync, err := t.SetBody(id, mark+body)
	return &sync, err
}

// swapped is body with the embeds of its cover and of file traded (swapEmbeds), when the body embeds file. ok is
// false when it does not, or when the swap would not give the same pictures with file first: an embed written in a
// way the plain text does not show (an escape, a code span before it) is not moved.
func (t *Tx) swapped(body string, pics []Cover, file int64) (string, bool, error) {
	i := slices.IndexFunc(pics, func(c Cover) bool { return c.ID == file })
	if i < 1 {
		return "", false, nil
	}
	next, ok := swapEmbeds(body, pics[0].Embed, pics[i].Embed)
	if !ok {
		return "", false, nil
	}
	after, err := embeddedPictures(t.ctx, t.tx, []string{next})
	if err != nil {
		return "", false, err
	}
	ids := func(cs []Cover) []int64 {
		out := []int64{}
		for _, c := range cs {
			out = append(out, c.ID)
		}
		slices.Sort(out)
		return out
	}
	return next, len(after[0]) > 0 && after[0][0].ID == file && slices.Equal(ids(after[0]), ids(pics)), nil
}

// swapEmbeds trades the first embed of the title a with the first embed of the title b in body, each as written,
// with its label. ok is false when either is missing.
func swapEmbeds(body, a, b string) (string, bool) {
	find := func(title string) []int {
		return regexp.MustCompile(`!\[\[ *` + regexp.QuoteMeta(title) + ` *(?:\|[^\[\]\n\r]*)?\]\]`).FindStringIndex(body)
	}
	x, y := find(a), find(b)
	if x == nil || y == nil || x[0] == y[0] {
		return "", false
	}
	if x[0] > y[0] {
		x, y = y, x
	}
	return body[:x[0]] + body[y[0]:y[1]] + body[x[1]:y[0]] + body[x[0]:x[1]] + body[y[1]:], true
}
