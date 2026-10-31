package api_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/internal/api/apitest"
	"lifelog/siren"
)

// TestSetCoverFromAnyPage: every page offers set-cover and a link to its picker, a file page and the lists do not. A
// client sends a new photo to the picker and the answer is the page; the photo is the page's cover, and the person's
// picture in People, kept with a title of the page and today. A tile of the picker sets a kept picture, which the
// picker then marks. A client names a kept picture by its title, as the CLI and an agent do.
func TestSetCoverFromAnyPage(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	ana := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Ana"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, ana, "save-body"), map[string]string{"body": "Met at school."}))
	lake := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Lake"}))
	trip := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Trip"}))
	scanPath, _ := jpegFile(t, "scan.jpg", 20, 20)
	scan := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "Scan.jpg", "original": scanPath}))
	for _, e := range []*siren.Entity{ana, lake, trip} {
		coverAction(t, e, apitest.Href(e, "self")+"/cover")
		if apitest.Href(e, "choose-cover") != apitest.Href(e, "self")+"/cover" {
			t.Errorf("%s has no link to its picker", e.Title)
		}
	}
	for _, e := range []*siren.Entity{scan, apitest.Must(t)(c.Get(ctx, "/browse?kind=person"))} {
		if hasAction(e, "set-cover") {
			t.Errorf("%s offers set-cover", e.Title)
		}
	}
	self := apitest.Href(ana, "self")
	// The picker takes a new photo (a file field that accepts pictures) or the title of a kept picture, and lists the
	// kept pictures with their previews.
	picker := apitest.Must(t)(c.Get(ctx, self+"/cover"))
	set := coverAction(t, picker, self+"/cover")
	if f := set.Field("original"); f == nil || f.Type != "file" || f.Accept != "image/*" || set.Field("title") == nil {
		t.Errorf("the picker's set-cover is %+v, want the fields original (a file of pictures) and title", set.Fields)
	}
	if rows, _ := props(picker)["rows"].([]any); len(rows) != 1 || str(rows[0].(map[string]any)["title"]) != "Scan.jpg" || str(rows[0].(map[string]any)["picture"]) != apitest.Href(scan, "preview") {
		t.Errorf("the picker lists %v, want Scan.jpg with its preview", props(picker)["rows"])
	}

	jpg, _ := jpegFile(t, "ana.jpg", 40, 30)
	rec := postPhoto(t, h, self+"/cover", jpg)
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != self {
		t.Fatalf("POST %s/cover: %d to %q %.300s", self, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	title := "Ana " + fixedToday + ".jpg"
	page := apitest.Must(t)(c.Get(ctx, self))
	if got := str(props(page)["body"]); got != "![["+title+"]]\n\nMet at school." {
		t.Errorf("Ana's body = %q", got)
	}
	cover := apitest.Href(page, "cover")
	if !strings.HasSuffix(cover, "/preview") {
		t.Fatalf("Ana's page has no cover link: %+v", page.Links)
	}
	rows, _ := props(apitest.Must(t)(c.Get(ctx, "/browse?kind=person")))["rows"].([]any)
	if len(rows) != 1 || str(rows[0].(map[string]any)["picture"]) != cover {
		t.Fatalf("rows = %v, want Ana with the cover %s", rows, cover)
	}

	// A tile of the picker is a form of the picture's title; the answer is the page.
	rec = request(h, "POST", self+"/cover", url.Values{"title": {"Scan.jpg"}}.Encode(), map[string]string{"Host": "localhost"})
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != self {
		t.Fatalf("the tile: %d to %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	choice := apitest.Must(t)(c.Get(ctx, self+"/cover"))
	marked := map[string]bool{}
	for _, r := range props(choice)["rows"].([]any) {
		m := r.(map[string]any)
		marked[str(m["title"])] = m["current"] == true
	}
	if want := map[string]bool{"Scan.jpg": true, title: false}; !reflect.DeepEqual(marked, want) {
		t.Errorf("the picker rows = %v, want %v", marked, want)
	}

	// A client names a kept picture by its title.
	apitest.Must(t)(c.Do(ctx, coverAction(t, apitest.Must(t)(c.Get(ctx, apitest.Href(lake, "self"))), apitest.Href(lake, "self")+"/cover"), map[string]string{"title": title}))
	pages, _ := props(apitest.Must(t)(c.Get(ctx, "/browse?kind=page")))["rows"].([]any)
	var lakePicture any
	for _, r := range pages {
		if row := r.(map[string]any); row["title"] == "Lake" {
			lakePicture = row["picture"]
		}
	}
	if str(lakePicture) != cover {
		t.Errorf("the pages rows = %v, want Lake with the cover %s", pages, cover)
	}
	if _, err := c.Do(ctx, coverAction(t, page, self+"/cover"), map[string]string{"title": "Nothing.jpg"}); err == nil {
		t.Errorf("a title that is no kept picture was taken")
	}
}

// TestTheCoverOfADay: the first picture of a day page is its cover, in the day page and in the day view. A day view
// whose day page has no cover leads to the picker; a day with no day page yet has no picker, since there is no page to
// take a cover.
func TestTheCoverOfADay(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/days/2026-01-02")), "capture"), map[string]string{"text": "Words."}))
	if plain := apitest.Must(t)(c.Get(ctx, "/days/2026-01-02")); apitest.Href(plain, "choose-cover") == "" {
		t.Errorf("a day with no cover does not lead to the picker")
	}
	if none := apitest.Must(t)(c.Get(ctx, "/days/2026-01-03")); apitest.Href(none, "choose-cover") != "" {
		t.Errorf("a day with no day page offers a cover")
	}
	jpg, _ := jpegFile(t, "lake.jpg", 40, 30)
	f := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "Lake.jpg", "day": fixedToday, "original": jpg}))
	day := apitest.Must(t)(c.Get(ctx, "/days/"+fixedToday))
	if got := props(day)["cover"]; got == nil || str(got.(map[string]any)["title"]) != "Lake.jpg" {
		t.Errorf("the day's cover = %v, want Lake.jpg", got)
	}
	if apitest.Href(day, "cover") != apitest.Href(f, "preview") {
		t.Errorf("the day's cover link = %q, want %q", apitest.Href(day, "cover"), apitest.Href(f, "preview"))
	}
	if page := apitest.Must(t)(c.Get(ctx, apitest.Href(day, "page"))); apitest.Href(page, "cover") != apitest.Href(f, "preview") {
		t.Errorf("the day page's cover link = %q, want %q", apitest.Href(page, "cover"), apitest.Href(f, "preview"))
	}
}

// TestThePickerRefuses: a file page has no picker, its own picture is its cover; a deleted page is revived first; a
// page that does not exist is not found.
func TestThePickerRefuses(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	jpg, _ := jpegFile(t, "lake.jpg", 40, 30)
	f := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "Lake.jpg", "original": jpg}))
	gone := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Gone"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, gone, "tombstone-pages"), nil))
	if apitest.Href(f, "choose-cover") != "" {
		t.Errorf("a file page links a picker")
	}
	for path, want := range map[string]int{apitest.Href(f, "self") + "/cover": 404, apitest.Href(gone, "self") + "/cover": 409, "/pages/999/cover": 404} {
		_, err := c.Get(ctx, path)
		var e *client.Error
		if !errors.As(err, &e) || e.Status != want {
			t.Errorf("GET %s: %v, want %d", path, err, want)
		}
	}
}

func coverAction(t *testing.T, e *siren.Entity, href string) siren.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == "set-cover" && a.Href == href {
			return a
		}
	}
	t.Fatalf("%s: no set-cover to %s among %+v", e.Title, href, e.Actions)
	return siren.Action{}
}

// postPhoto sends a photo as a multipart form.
func postPhoto(t *testing.T, h http.Handler, path, file string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mw.WriteField("title", "")
	p, err := mw.CreateFormFile("original", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.Copy(p, f); err != nil {
		t.Fatal(err)
	}
	mw.CreateFormFile("preview", "") // the folded preview input, left empty
	mw.Close()
	req := httptest.NewRequest("POST", path, &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAPictureTheBodyEmbedsBecomesTheCover: the picker's form of a kept picture that the body embeds makes it the
// cover by trading places with the old cover's embed (docs/plans/131-the-pareto-pass.md, part 2).
func TestAPictureTheBodyEmbedsBecomesTheCover(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	for i, title := range []string{"A.jpg", "B.jpg"} {
		jpg, _ := jpegFile(t, strings.ToLower(title), 40, 30+i) // another size: another original
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": title, "original": jpg}))
	}
	trip := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Trip", "body": "![[A.jpg]]\n\nSwam. ![[B.jpg]]"}))
	self := apitest.Href(trip, "self")
	if first := str(props(apitest.Must(t)(c.Get(ctx, self)))["cover"].(map[string]any)["title"]); first != "A.jpg" {
		t.Fatalf("the cover before the pick = %q, want A.jpg: the first picture of the body", first)
	}
	rec := request(h, "POST", self+"/cover", url.Values{"title": {"B.jpg"}}.Encode(), map[string]string{"Host": "localhost"})
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != self {
		t.Fatalf("the picker's B.jpg: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	p := props(apitest.Must(t)(c.Get(ctx, self)))
	if got := str(p["body"]); got != "![[B.jpg]]\n\nSwam. ![[A.jpg]]" {
		t.Errorf("the body after B.jpg = %q", got)
	}
	if str(p["cover"].(map[string]any)["title"]) != "B.jpg" {
		t.Errorf("the cover = %v, want B.jpg", p["cover"])
	}
}

// TestTheCalendarShowsTheCovers: a day page with a cover gives the month its picture; a day page without one, and a
// day with no page, have none.
func TestTheCalendarShowsTheCovers(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	jpg, _ := jpegFile(t, "lake.jpg", 40, 30)
	f := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "Lake.jpg", "day": "2031-05-02", "original": jpg}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/days/2031-05-01")), "capture"), map[string]string{"text": "No photo."}))
	pictures := func(path string) map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, d := range props(apitest.Must(t)(c.Get(ctx, path)))["days"].([]any) {
			m := d.(map[string]any)
			if m["page"] == true {
				out[str(m["day"])] = str(m["picture"])
			}
		}
		return out
	}
	want := map[string]string{"2031-05-01": "", "2031-05-02": apitest.Href(f, "preview")}
	if got := pictures("/months/2031-05"); !reflect.DeepEqual(got, want) {
		t.Errorf("/months/2031-05: pictures = %v, want %v", got, want)
	}
}
