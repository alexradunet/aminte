package core

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/photo/phototest"
)

// browseFixture writes, through the writer, one of each kind in and out of the categories Family, Cousins (under
// Family) and Work, and returns the ids by title. Hand-checked layout:
//
//	Family:  Beta, Cousins, Ana, photo.jpg, scan.pdf, Old (tombstoned)
//	Cousins: Gamma
//	Work:    Delta, Weight
//	nowhere: Alpha, Bo, memo.m4a, notes.txt, Walk, Mood (the metric every file has), the day page
//	         2031-05-01 (a day page is never filed: links_endpoint_types, D26)
func browseFixture(t *testing.T) (*Store, map[string]int64) {
	t.Helper()
	s := fresh(t)
	ids := map[string]int64{}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, title := range []string{"Alpha", "Beta", "Gamma", "Delta", "Old"} {
		id, _, err := s.CreatePage(ctx, "cli", title, "", "")
		check(err)
		ids[title] = id
	}
	for _, title := range []string{"Ana", "Bo"} {
		id, err := s.CreatePerson(ctx, "cli", title, "", "", "")
		check(err)
		ids[title] = id
	}
	for i, f := range []FileIn{{Title: "photo.jpg", MIME: "image/jpeg", Day: "2031-05-02"}, {Title: "memo.m4a", MIME: "audio/mp4", Day: "2031-05-01"},
		{Title: "scan.pdf", MIME: "application/pdf", Day: "2031-04-01"}, {Title: "notes.txt", MIME: "text/plain", Day: "2031-03-01"}} {
		f.SHA256 = strings.Repeat(fmt.Sprintf("%02x", i+1), 32)
		k, err := s.AddFile(ctx, "cli", f)
		check(err)
		ids[f.Title] = k.ID
	}
	for _, m := range [][2]string{{"Weight", "kg"}, {"Walk", ""}} {
		_, err := s.CreateMetric(ctx, "cli", NewMetric{Name: m[0], Unit: m[1]})
		check(err)
		id, err := s.PageID(ctx, m[0])
		check(err)
		ids[m[0]] = id
	}
	day, _, err := s.Capture(ctx, "cli", "2031-05-01", "Lunch with [[Ana]]", nil)
	check(err)
	ids["2031-05-01"] = day
	file := func(category string, titles ...string) {
		t.Helper()
		var list []int64
		for _, title := range titles {
			list = append(list, ids[title])
		}
		f, err := s.FilePages(ctx, "cli", list, category, "", false)
		check(err)
		ids[category] = f.Category.ID
	}
	file("Family", "Beta", "Ana", "photo.jpg", "scan.pdf", "Old")
	file("Cousins", "Gamma")
	file("Family", "Cousins")
	file("Work", "Delta", "Weight")
	check(s.Tombstone(ctx, "cli", ids["Old"]))
	return s, ids
}

func pageTitles(rows []PageRow) string {
	var out []string
	for _, r := range rows {
		title := r.Title
		if r.Category {
			title += "*"
		}
		out = append(out, title)
	}
	return strings.Join(out, ",")
}

// TestPlainPagesInEachScope: the plain pages by title, a category marked; in a category only what is filed directly
// in it; not filed, the pages in no category that are not categories. A day page and a tombstone are in no list.
func TestPlainPagesInEachScope(t *testing.T) {
	s, ids := browseFixture(t)
	for _, tc := range []struct {
		name string
		sc   Scope
		want string
	}{
		{"everything", Scope{}, "Alpha,Beta,Cousins*,Delta,Family*,Gamma,Work*"},
		{"in Family", Scope{In: ids["Family"]}, "Beta,Cousins*"},
		{"in Cousins", Scope{In: ids["Cousins"]}, "Gamma"},
	} {
		got, err := s.PlainPages(ctx, tc.sc, 50, 0)
		if err != nil || pageTitles(got) != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, pageTitles(got), err, tc.want)
		}
	}
	if got, err := s.PlainPages(ctx, Scope{}, 2, 2); err != nil || pageTitles(got) != "Cousins*,Delta" {
		t.Errorf("a page of two after two: %q %v", pageTitles(got), err)
	}
	unfiled, err := s.PlainPages(ctx, Scope{Unfiled: true}, 50, 0)
	if err != nil || pageTitles(unfiled) != "Alpha" || unfiled[0].ID != ids["Alpha"] {
		t.Errorf("not filed: %+v %v, want Alpha %d", unfiled, err, ids["Alpha"])
	}
}

// TestTheKindsInEachScope: people with their mentions, media and documents newest day first, metrics by
// title with their unit, each in the three scopes.
func TestTheKindsInEachScope(t *testing.T) {
	s, ids := browseFixture(t)
	named := func(sc Scope) string {
		t.Helper()
		rows, err := s.NamedMentions(ctx, sc, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s:%d:%s", r.Title, r.Mentions, r.LastDay))
		}
		return strings.Join(out, ",")
	}
	files := func(media bool, kind string, sc Scope) string {
		t.Helper()
		var rows []FileRow
		var err error
		if media {
			rows, err = s.Media(ctx, kind, sc, 50, 0)
		} else {
			rows, err = s.OtherFiles(ctx, sc, 50, 0)
		}
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Title+":"+r.Day)
		}
		return strings.Join(out, ",")
	}
	metrics := func(sc Scope) string {
		t.Helper()
		rows, err := s.MetricRows(ctx, sc, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Title+":"+r.Unit)
		}
		return strings.Join(out, ",")
	}
	family, unfiled := Scope{In: ids["Family"]}, Scope{Unfiled: true}
	for _, tc := range []struct{ name, got, want string }{
		{"people", named(Scope{}), "Ana:1:2031-05-01,Bo:0:"},
		{"people in Family", named(family), "Ana:1:2031-05-01"},
		{"people not filed", named(unfiled), "Bo:0:"},
		{"media", files(true, "", Scope{}), "photo.jpg:2031-05-02,memo.m4a:2031-05-01"},
		{"recordings", files(true, "audio", Scope{}), "memo.m4a:2031-05-01"},
		{"media in Family", files(true, "", family), "photo.jpg:2031-05-02"},
		{"media not filed", files(true, "", unfiled), "memo.m4a:2031-05-01"},
		{"pictures not filed", files(true, "image", unfiled), ""},
		{"documents", files(false, "", Scope{}), "scan.pdf:2031-04-01,notes.txt:2031-03-01"},
		{"documents in Family", files(false, "", family), "scan.pdf:2031-04-01"},
		{"documents not filed", files(false, "", unfiled), "notes.txt:2031-03-01"},
		{"metrics", metrics(Scope{}), "Mood:1-5,Walk:,Weight:kg"},
		{"metrics in Work", metrics(Scope{In: ids["Work"]}), "Weight:kg"},
		{"metrics not filed", metrics(unfiled), "Mood:1-5,Walk:"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestFiledInListsTheCategoriesFirst: All in a category is every live thing filed directly in it, the categories
// first, then by title, each with its kind; a tombstone is not in it, nor what a sub-category holds.
func TestFiledInListsTheCategoriesFirst(t *testing.T) {
	s, ids := browseFixture(t)
	rows, err := s.FiledIn(ctx, ids["Family"], 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s:%s:%s:%v:%s", r.Title, r.Type, r.Kind, r.Category, r.MIME))
	}
	want := "Cousins:page:page:true:,Ana:person:person:false:,Beta:page:page:false:,photo.jpg:file:media:false:image/jpeg,scan.pdf:file:document:false:application/pdf"
	if strings.Join(got, ",") != want {
		t.Errorf("FiledIn(Family):\n got %s\nwant %s", strings.Join(got, ","), want)
	}
	if rows[0].ID != ids["Cousins"] || rows[1].ID != ids["Ana"] {
		t.Errorf("ids: %+v", rows[:2])
	}
	// The tree counts the same things: the tombstone filed in Family is not counted.
	cats, err := s.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.ID == ids["Family"] && c.Count != len(rows) {
			t.Errorf("Family counts %d, All lists %d", c.Count, len(rows))
		}
	}
	if page, err := s.FiledIn(ctx, ids["Family"], 2, 3); err != nil || len(page) != 2 || page[0].Title != "photo.jpg" || page[1].Title != "scan.pdf" {
		t.Errorf("the last page of two: %+v %v", page, err)
	}
}

// TestBrowseCountsEachKindInEachScope: one count a kind in the scope, all their sum, and the ghost pages.
func TestBrowseCountsEachKindInEachScope(t *testing.T) {
	s, ids := browseFixture(t)
	for _, tc := range []struct {
		name string
		sc   Scope
		want map[string]int
	}{
		{"everything", Scope{}, map[string]int{"page": 7, "person": 2, "media": 2, "document": 2, "metric": 3, "all": 16, "ghosts": 0, "deleted": 1}},
		{"in Family", Scope{In: ids["Family"]}, map[string]int{"page": 2, "person": 1, "media": 1, "document": 1, "metric": 0, "all": 5, "ghosts": 0, "deleted": 1}},
		{"in Work", Scope{In: ids["Work"]}, map[string]int{"page": 1, "person": 0, "media": 0, "document": 0, "metric": 1, "all": 2, "ghosts": 0, "deleted": 1}},
		{"not filed", Scope{Unfiled: true}, map[string]int{"page": 1, "person": 1, "media": 1, "document": 1, "metric": 2, "all": 6, "ghosts": 0, "deleted": 1}},
	} {
		got, err := s.BrowseCounts(ctx, tc.sc)
		if err != nil || !maps.Equal(got, tc.want) {
			t.Errorf("%s: %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
}

// TestBrowseCountsTheGhostPages: the count of the ghost_pages view, a day page left out (a day page is never in
// Browse). The ghosts are inserted by direct SQL with an old created_at (bulk setup, as the hygiene test does:
// ghost_pages wants them older than 30 days, and created_at never changes); a page written now is not a ghost. The
// writer makes an empty day page with no links too (CreatePage with a date as the title); the old one here has that
// shape.
func TestBrowseCountsTheGhostPages(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "cli", "Fresh", "", ""); err != nil {
		t.Fatal(err)
	}
	err := s.Do(ctx, "cli", func(tx *Tx) error {
		for i, title := range []string{"Ghost A", "Ghost B", "2000-02-01"} {
			var id int64
			at := fmt.Sprintf("2000-01-01T00:00:0%d.000Z", i+1)
			var day any
			if title == "2000-02-01" {
				day = title
			}
			if err := tx.tx.QueryRowContext(ctx, `INSERT INTO entities(entity_type, preferred_name_key, day, body, created_at, updated_at, source)
			    VALUES ('page', ?, ?, '', ?, ?, 'cli') RETURNING id`, strings.ToLower(title), day, at, at).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.tx.ExecContext(ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, title, strings.ToLower(title)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var inView int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM ghost_pages`).Scan(&inView); err != nil || inView != 3 {
		t.Fatalf("the view has %d rows, %v: the setup wants the old day page in it", inView, err)
	}
	got, err := s.BrowseCounts(ctx, Scope{})
	if err != nil || got["ghosts"] != 2 || got["page"] != 3 {
		t.Errorf("counts: %v %v, want 2 ghosts among 3 pages", got, err)
	}
	ghosts, err := s.Ghosts(ctx, 10, 0)
	if err != nil || len(ghosts) != 2 || ghosts[0].Title != "Ghost A" || ghosts[1].Title != "Ghost B" {
		t.Errorf("Ghosts: %+v %v, want Ghost A, Ghost B", ghosts, err)
	}
}

// TestNotFiledCountsOnlyALiveCategory: a tombstoned category is not in the tree, so a thing filed only in it is in no
// category. It is in Not filed, and the chip counts it.
func TestNotFiledCountsOnlyALiveCategory(t *testing.T) {
	s, ids := browseFixture(t)
	cy, err := s.CreatePerson(ctx, "cli", "Cy", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	eps, _, err := s.CreatePage(ctx, "cli", "Eps", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.FilePages(ctx, "cli", []int64{cy, eps, ids["Bo"]}, "Gone", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FilePages(ctx, "cli", []int64{ids["Bo"]}, "Work", "", false); err != nil { // Bo stays in a live one
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone.Category.ID); err != nil {
		t.Fatal(err)
	}
	cats, err := s.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.ID == gone.Category.ID {
			t.Errorf("the tree draws the tombstoned category: %+v", c)
		}
	}
	unfiled := Scope{Unfiled: true}
	people, err := s.NamedMentions(ctx, unfiled, 50, 0)
	if err != nil || len(people) != 1 || people[0].ID != cy {
		t.Errorf("people not filed: %+v %v, want Cy", people, err)
	}
	if pages, err := s.PlainPages(ctx, unfiled, 50, 0); err != nil || pageTitles(pages) != "Alpha,Eps" || pages[1].ID != eps {
		t.Errorf("pages not filed: %+v %v, want Alpha,Eps (%d)", pages, err, eps)
	}
	if got, err := s.BrowseCounts(ctx, unfiled); err != nil || got["person"] != 1 || got["page"] != 2 {
		t.Errorf("counts not filed: %v %v, want 1 person and 2 pages", got, err)
	}
}

// TestBrowseOrdersTitlesWithoutCase: a list by title is in the order of the folded title, as the tree and the
// metrics board are: a lowercase title is not put after every capital.
func TestBrowseOrdersTitlesWithoutCase(t *testing.T) {
	s := fresh(t)
	var pages []int64
	for _, title := range []string{"reading", "Alpha", "Zeta"} {
		id, _, err := s.CreatePage(ctx, "cli", title, "", "")
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, id)
	}
	for _, title := range []string{"bo", "Ana", "Cy"} {
		if _, err := s.CreatePerson(ctx, "cli", title, "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []string{"commits", "Weight"} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: m}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.FilePages(ctx, "cli", pages, "Shelf", "", false); err != nil {
		t.Fatal(err)
	}
	shelf, err := s.PageID(ctx, "Shelf")
	if err != nil {
		t.Fatal(err)
	}
	titles := func(rows any, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		switch rs := rows.(type) {
		case []PageRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		case []NamedRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		case []MetricRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		case []FiledRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct{ name, got, want string }{
		{"pages", titles(s.PlainPages(ctx, Scope{}, 50, 0)), "Alpha,reading,Shelf,Zeta"},
		{"people", titles(s.NamedMentions(ctx, Scope{}, 50, 0)), "Ana,bo,Cy"},
		{"metrics", titles(s.MetricRows(ctx, Scope{}, 50, 0)), "commits,Mood,Weight"},
		{"in Shelf", titles(s.FiledIn(ctx, shelf, 50, 0)), "Alpha,reading,Zeta"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	for _, title := range []string{"Delta", "beta"} {
		if _, _, err := s.CreatePage(ctx, "cli", title, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := titles(s.PlainPages(ctx, Scope{Unfiled: true}, 50, 0)); got != "beta,Delta" {
		t.Errorf("not filed: %q", got)
	}
}

// TestBrowseKindsGivesACopy: a caller that changes the list BrowseKinds gives does not change the next list, which
// is the order of the chips of Browse.
func TestBrowseKindsGivesACopy(t *testing.T) {
	kinds := BrowseKinds()
	kinds[0] = "task"
	if got := strings.Join(BrowseKinds(), ","); got != "page,person,media,document,metric" {
		t.Errorf("BrowseKinds() = %s after a caller changed its copy, want page,person,media,document,metric", got)
	}
}

// TestMediaKindsGivesACopy: a caller that changes the list MediaKinds gives does not change the next list, or the
// files that the media list of Browse shows.
func TestMediaKindsGivesACopy(t *testing.T) {
	kinds := MediaKinds()
	kinds[0] = "text"
	if got := strings.Join(MediaKinds(), ","); got != "image,video,audio" {
		t.Errorf("MediaKinds() = %s after a caller changed its copy, want image,video,audio", got)
	}
	s := fresh(t)
	for i, f := range []FileIn{
		{Title: "A photo.jpg", MIME: "image/jpeg"},
		{Title: "A clip.mp4", MIME: "video/mp4"},
		{Title: "A memo.m4a", MIME: "audio/mp4"},
		{Title: "A note.txt", MIME: "text/plain"},
		{Title: "A scan.pdf", MIME: "application/pdf"},
	} {
		f.SHA256, f.Day = sha(byte('a'+i)), "2026-10-01"
		if _, err := s.AddFile(ctx, "cli", f); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(rows []FileRow, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Title)
		}
		slices.Sort(out)
		return strings.Join(out, ",")
	}
	if got, want := titles(s.Media(ctx, "", Scope{}, 10, 0)), "A clip.mp4,A memo.m4a,A photo.jpg"; got != want {
		t.Errorf("Media = %s, want %s", got, want)
	}
	if got, want := titles(s.OtherFiles(ctx, Scope{}, 10, 0)), "A note.txt,A scan.pdf"; got != want {
		t.Errorf("OtherFiles = %s, want %s", got, want)
	}
}

func TestReadersPage(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		if _, err := s.CreatePerson(ctx, "cli", fmt.Sprintf("Person %d", i), "", "", ""); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Capture(ctx, "cli", fmt.Sprintf("2031-04-%02d", i), fmt.Sprintf("pageword [[Missing %d]]", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	// Hand-counted: five people by title, five ghosts oldest first, five hits.
	for _, tc := range []struct {
		name          string
		limit, offset int
		want          string
	}{
		{"first page", 2, 0, "Person 1,Person 2"},
		{"second page", 2, 2, "Person 3,Person 4"},
		{"last page", 2, 4, "Person 5"},
		{"past the end", 2, 5, ""},
		{"all", 50, 0, "Person 1,Person 2,Person 3,Person 4,Person 5"},
	} {
		got, err := s.NamedMentions(ctx, Scope{}, tc.limit, tc.offset)
		var list []string
		for _, r := range got {
			list = append(list, r.Title)
		}
		if err != nil || strings.Join(list, ",") != tc.want {
			t.Errorf("%s: %q %v", tc.name, list, err)
		}
	}
	// The linked targets are not ghosts (a ghost has no link). Five empty pages of their own, inserted by direct SQL
	// with an old created_at (bulk setup: ghost_pages wants them older than 30 days, and created_at never changes),
	// are.
	err := s.Do(ctx, "cli", func(tx *Tx) error {
		for i := 1; i <= 5; i++ {
			var id int64
			at := fmt.Sprintf("2000-01-01T00:00:0%d.000Z", i)
			if err := tx.tx.QueryRowContext(ctx, `INSERT INTO entities(entity_type, preferred_name_key, body, created_at, updated_at, source)
			    VALUES ('page', ?, '', ?, ?, 'cli') RETURNING id`, fmt.Sprintf("ghost %d", i), at, at).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.tx.ExecContext(ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, fmt.Sprintf("Ghost %d", i), fmt.Sprintf("ghost %d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if none, err := s.Ghosts(ctx, 10, 0); err != nil || len(none) != 5 {
		t.Fatalf("ghosts: %+v %v", none, err)
	}
	ghosts, err := s.Ghosts(ctx, 2, 3)
	if err != nil || len(ghosts) != 2 || ghosts[0].Title != "Ghost 4" || ghosts[1].Title != "Ghost 5" {
		t.Errorf("ghosts page: %+v %v", ghosts, err)
	}
	hits, err := s.SearchWith(ctx, "pageword", SearchOptions{}, 2, 4)
	if err != nil || len(hits) != 1 {
		t.Errorf("search: %+v %v", hits, err)
	}
	all, err := s.SearchWith(ctx, "pageword", SearchOptions{}, 10, 0)
	if err != nil || len(all) != 5 {
		t.Errorf("search all: %+v %v", all, err)
	}
}

// TestNamedMentionsCountLivePagesAndTheNewestDay: a person is mentioned once by each live page that links to it.
// A tombstoned page does not count, and only a day page gives the last day. The order is by title.
func TestNamedMentionsCountLivePagesAndTheNewestDay(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"Sam", "Ana", "Zed"} {
		if _, err := s.CreatePerson(ctx, "cli", name, "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-05", "Lunch with [[Ana]]", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-09", "Walk with [[Ana]] and [[Sam]]", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Trip", "[[Ana]] came", ""); err != nil {
		t.Fatal(err)
	}
	gone, _, err := s.CreatePage(ctx, "cli", "Old note", "[[Sam]] was there", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}

	got, err := s.NamedMentions(ctx, Scope{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Ana: two day pages and Trip. Sam: one day page (the tombstoned page does not count). Zed: none.
	want := []struct {
		title    string
		mentions int
		lastDay  string
	}{{"Ana", 3, "2026-10-09"}, {"Sam", 1, "2026-10-09"}, {"Zed", 0, ""}}
	if len(got) != len(want) {
		t.Fatalf("NamedMentions = %+v, want %d rows", got, len(want))
	}
	for i, w := range want {
		if got[i].Title != w.title || got[i].Mentions != w.mentions || got[i].LastDay != w.lastDay {
			t.Errorf("row %d = %+v, want %+v", i, got[i], w)
		}
	}

	second, err := s.NamedMentions(ctx, Scope{}, 1, 1)
	if err != nil || len(second) != 1 || second[0].Title != "Sam" {
		t.Errorf("NamedMentions(1, 1) = %+v, %v, want Sam", second, err)
	}
}

// TestNamedMentionsGiveAPersonTheFirstPictureItsPageEmbeds: a person's picture is the first file its body embeds
// that keeps a preview and is live. A file with no preview, a tombstoned file and a plain [[link]] are passed over.
func TestNamedMentionsGiveAPersonTheFirstPictureItsPageEmbeds(t *testing.T) {
	s := fresh(t)
	pic := phototest.JPEG(8, 8)
	keep := func(title, mime string, c byte, preview []byte) int64 {
		t.Helper()
		k, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: sha(c), MIME: mime, Day: "2026-10-01", Preview: preview})
		if err != nil {
			t.Fatal(err)
		}
		return k.ID
	}
	keep("Notes.pdf", "application/pdf", 'a', nil)
	face := keep("Ana face.jpg", "image/jpeg", 'b', pic)
	keep("Beach.jpg", "image/jpeg", 'c', pic)
	gone := keep("Old face.jpg", "image/jpeg", 'd', pic)
	bodies := map[string]string{
		"Ana": "![[Notes.pdf]] Met at school.\n\n![[Ana face.jpg]]\n\n![[Beach.jpg]]",
		"Sam": "![[Old face.jpg]] and [[Beach.jpg]], a link and not an embed",
		"Zed": "",
	}
	for _, name := range []string{"Ana", "Sam", "Zed"} {
		id, err := s.CreatePerson(ctx, "cli", name, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if bodies[name] != "" {
			if _, err := s.SaveBody(ctx, "cli", id, bodies[name], "1"); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := s.Tombstone(ctx, "cli", gone); err != nil { // after the save, which revives a page it links
		t.Fatal(err)
	}

	got, err := s.NamedMentions(ctx, Scope{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"Ana": face, "Sam": 0, "Zed": 0}
	if len(got) != len(want) {
		t.Fatalf("NamedMentions = %+v, want %d rows", got, len(want))
	}
	for _, r := range got {
		if r.Picture != want[r.Title] {
			t.Errorf("%s: picture %d, want %d", r.Title, r.Picture, want[r.Title])
		}
	}
}

// TestARandomPickIsLiveAndOfItsKind: a random day is a live day page up to today; a random thing is a live thing of
// its kind, never a day page and never a ghost page. With one live and one tombstoned candidate, every pick is the
// live one. A kind with nothing is a 404, a kind that is not one a 422.
func TestARandomPickIsLiveAndOfItsKind(t *testing.T) {
	s := streamStore(t) // today is 2026-10-17
	if _, err := s.RandomDay(ctx); status(err) != 404 {
		t.Errorf("a random day of an empty record: %v, want a 404", err)
	}
	captured(t, s, "2026-10-01", "Kept.", nil)
	gone := captured(t, s, "2026-10-02", "Deleted.", nil)
	captured(t, s, "2026-11-01", "A plan.", nil)
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	ana := must[int64](t)(s.CreatePerson(ctx, "cli", "Ana", "", "", ""))
	bob := must[int64](t)(s.CreatePerson(ctx, "cli", "Bob", "", "", ""))
	if err := s.Tombstone(ctx, "cli", bob); err != nil {
		t.Fatal(err)
	}
	// A ghost page: empty, unlinked and older than 30 days (bulk setup by direct SQL, as TestBrowseCountsTheGhostPages
	// does: created_at never changes).
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var id int64
		if err := tx.tx.QueryRowContext(ctx, `INSERT INTO entities(entity_type, preferred_name_key, body, created_at, updated_at, source)
		    VALUES ('page', 'ghost', '', '2000-01-01T00:00:00.000Z', '2000-01-01T00:00:00.000Z', 'cli') RETURNING id`).Scan(&id); err != nil {
			return err
		}
		_, err := tx.tx.ExecContext(ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, 'Ghost', 'ghost')`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if day, err := s.RandomDay(ctx); err != nil || day != "2026-10-01" {
			t.Fatalf("a random day: %q, %v; want 2026-10-01, the one live day page up to today", day, err)
		}
		if id, err := s.RandomThing(ctx, "person"); err != nil || id != ana {
			t.Fatalf("a random person: %d, %v; want Ana %d", id, err, ana)
		}
		// all: Ana, the Mood metric, and no day page, no ghost
		id, err := s.RandomThing(ctx, "all")
		if err != nil {
			t.Fatal(err)
		}
		if p, err := s.PageByID(ctx, id); err != nil || p.IsDayPage || p.Title == "Ghost" || p.DeletedAt != "" {
			t.Fatalf("a random thing of all: %+v, %v", p, err)
		}
	}
	if _, err := s.RandomThing(ctx, "page"); status(err) != 404 {
		t.Errorf("a random page with only a ghost and day pages: %v, want a 404", err)
	}
}
