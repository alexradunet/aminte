package api_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/siren"
)

// TestMediaListsPicturesVideosAndRecordings: the media list of Browse is the files of the three kinds, newest day
// first, with a kind, a picture where one is kept and a day; a file of another kind keeps its page and is a document.
// One kind alone is read by media=, and any other word is refused.
func TestMediaListsPicturesVideosAndRecordings(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	add := apitest.Action(t, root, "add-file")
	jpg := func(name string, w, h int) string { p, _ := jpegFile(t, name, w, h); return p }
	photo := apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": "Lake.jpg", "day": "2031-05-10", "original": jpg("lake.jpg", 40, 30)}))
	apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": "Walk.mp4", "sha256": strings.Repeat("ab", 32), "mime": "video/mp4", "day": "2031-05-12"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "Walked. [[Walk.mp4]]"}))
	for i, title := range []string{"Memo 1.m4a", "Memo 2.m4a"} {
		apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": title, "sha256": strings.Repeat(string(rune('c'+i))+"d", 32), "mime": "audio/mp4", "day": "2031-05-0" + string(rune('1'+i))}))
	}
	// A photo with no EXIF and no day has no day of its own: the day page that shows it gives it one, or it has none.
	apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": "Dawn.jpg", "original": jpg("dawn.jpg", 32, 24)}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "![[Dawn.jpg]]"}))
	apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": "Blank.jpg", "original": jpg("blank.jpg", 24, 16)}))
	apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf"}))

	e := apitest.Must(t)(c.Get(ctx, "/browse?kind=media"))
	rows, _ := props(e)["rows"].([]any)
	var got []string
	for _, r := range rows {
		m := r.(map[string]any)
		got = append(got, m["title"].(string)+" "+m["kind"].(string)+" "+m["file_kind"].(string)+" "+m["mime"].(string)+" day="+str(m["day"])+" picture="+str(m["picture"])+" mentions="+str(m["mentions"]))
	}
	want := []string{
		"Dawn.jpg media image image/jpeg day=" + fixedToday + " picture=/pages/8/preview mentions=1",
		"Walk.mp4 media video video/mp4 day=2031-05-12 picture= mentions=1",
		"Lake.jpg media image image/jpeg day=2031-05-10 picture=" + apitest.Href(photo, "preview") + " mentions=1", // its day page shows it
		"Memo 2.m4a media audio audio/mp4 day=2031-05-02 picture= mentions=0",
		"Memo 1.m4a media audio audio/mp4 day=2031-05-01 picture= mentions=0",
		"Blank.jpg media image image/jpeg day= picture=/pages/9/preview mentions=0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if counts, _ := props(e)["media_counts"].(map[string]any); str(counts["image"]) != "3" || str(counts["video"]) != "1" || str(counts["audio"]) != "2" {
		t.Errorf("the media counts: %v", props(e)["media_counts"])
	}
	if len(e.Entities) != 6 || e.Entities[0].Title != "Dawn.jpg" || e.Entities[0].Class[0] != "file" {
		t.Errorf("the items: %+v", e.Entities)
	}
	if apitest.Href(e, "pictures") != "/browse?kind=media&media=image" || apitest.Href(e, "videos") != "/browse?kind=media&media=video" ||
		apitest.Href(e, "recordings") != "/browse?kind=media&media=audio" || apitest.Href(root, "media") != "/browse?kind=media" {
		t.Errorf("the links: %+v", e.Links)
	}
	// The list's add-file takes a file of any kind, as the catalog's does.
	accept := func(a siren.Action) string {
		for _, f := range a.Fields {
			if f.Name == "original" {
				return f.Accept
			}
		}
		return "?"
	}
	if accept(apitest.Action(t, e, "add-file")) != "" || accept(add) != "" {
		t.Errorf("the actions: %+v", e.Actions)
	}

	// One kind alone, and its paging keeps the kind; the media counts stay those of every kind.
	audio := apitest.Must(t)(c.Get(ctx, "/browse?kind=media&media=audio&limit=1"))
	rows, _ = props(audio)["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "Memo 2.m4a" || props(audio)["media"] != "audio" ||
		apitest.Href(audio, "next") != "/browse?kind=media&limit=1&media=audio&offset=1" || apitest.Href(audio, "self") != "/browse?kind=media&media=audio" {
		t.Errorf("the recordings: %v %v %q", rows, props(audio)["media"], apitest.Href(audio, "next"))
	}
	if counts, _ := props(audio)["media_counts"].(map[string]any); str(counts["image"]) != "3" {
		t.Errorf("the media counts of the recordings: %v", props(audio)["media_counts"])
	}
	if rows, _ := props(apitest.Must(t)(c.Get(ctx, "/browse?kind=media&media=image")))["rows"].([]any); len(rows) != 3 || rows[1].(map[string]any)["title"] != "Lake.jpg" {
		t.Errorf("the pictures: %v", rows)
	}

	// The documents are every other file, as rows, with the same words.
	apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": "Notes.txt", "sha256": strings.Repeat("0f", 32), "mime": "text/plain", "day": "2031-05-03"}))
	files := apitest.Must(t)(c.Get(ctx, "/browse?kind=document"))
	got = nil
	for _, r := range props(files)["rows"].([]any) {
		m := r.(map[string]any)
		got = append(got, m["title"].(string)+" "+m["kind"].(string)+" "+m["file_kind"].(string)+" "+m["mime"].(string)+" day="+str(m["day"]))
	}
	// The PDF had no day: it took the day of the store's clock when it was kept, so it is the newest.
	if want := "Scan.pdf document application application/pdf day=" + fixedToday + "\nNotes.txt document text text/plain day=2031-05-03"; strings.Join(got, "\n") != want ||
		len(files.Entities) != 2 || apitest.Href(root, "files") != "/browse?kind=document" ||
		accept(apitest.Action(t, files, "add-file")) != "" || props(files)["media_counts"] != nil {
		t.Errorf("the documents: %v\n%+v %+v", got, files.Links, props(files))
	}
}

// TestTheMediaCountsFollowTheScope: inside a category the media list counts what is filed there, and Not filed what
// is in no category.
func TestTheMediaCountsFollowTheScope(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	browseFixture(t, s) // Family: photo.jpg and scan.pdf; nowhere: memo.m4a
	ctx := t.Context()
	for path, want := range map[string]string{
		"/browse?kind=media":            "image=1 video=0 audio=1",
		"/browse?kind=media&in=Family":  "image=1 video=0 audio=0",
		"/browse?kind=media&unfiled=1":  "image=0 video=0 audio=1",
		"/browse?kind=media&in=Cousins": "image=0 video=0 audio=0",
	} {
		m := props(apitest.Must(t)(c.Get(ctx, path)))["media_counts"].(map[string]any)
		if got := "image=" + str(m["image"]) + " video=" + str(m["video"]) + " audio=" + str(m["audio"]); got != want {
			t.Errorf("%s: %s, want %s", path, got, want)
		}
	}
}

// TestMediaIsEmptyUntilAFileOfItsKindsIsKept: a PDF alone leaves the media list empty, with its counts and its
// empty words, and a photo alone the documents.
func TestMediaIsEmptyUntilAFileOfItsKindsIsKept(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf"}))
	e := apitest.Must(t)(c.Get(ctx, "/browse?kind=media"))
	if rows, _ := props(e)["rows"].([]any); len(rows) != 0 || len(e.Entities) != 0 {
		t.Errorf("the rows: %v", props(e)["rows"])
	}
	// And the other way round: a photo alone leaves the documents empty.
	c, _, _ = apitest.Fresh(t, "cli")
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "add-file"), map[string]string{"title": "Lake.jpg", "original": path}))
	if e := apitest.Must(t)(c.Get(ctx, "/browse?kind=document")); len(e.Entities) != 0 {
		t.Errorf("the documents: %+v", e.Entities)
	}
}

// str is a property as a string: a JSON number without its exponent, nothing as "".
func str(x any) string {
	switch v := x.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return "?"
}
