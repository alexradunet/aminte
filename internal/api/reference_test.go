package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"aminte/internal/core"
	"aminte/internal/db/dbtest"
)

// TestAPIReferenceMatchesTheCode keeps API.md true: every property table of a class names exactly the paths the
// Go property type carries, and every route's live JSON carries only the top-level keys its table names.
func TestAPIReferenceMatchesTheCode(t *testing.T) {
	documented := readReference(t, filepath.Join("..", "..", "API.md"))
	// The shape behind each section: a Go value whose JSON names, walked, are the paths the table must list.
	shapes := map[string]any{
		"root": map[string]any{"today": "", "zone": "", "utc_offset": 0, "recent": []recentDay{}},
		"days": map[string]any{"today": "", "limit": 0, "offset": 0, "cards": []dayCard{}},
		"browse": map[string]any{"kind": "", "in": browseIn{}, "unfiled": false, "ghosts": false, "deleted": false, "media": "", "counts": browseCounts{},
			"media_counts": mediaCounts{}, "categories": []core.Category{}, "rows": []browseRow{}, "limit": 0, "offset": 0},
		"search":                    map[string]any{"q": "", "order": "", "kind": "", "titles": []core.TitleMatch{}, "hits": []searchHit{}, "limit": 0, "offset": 0},
		"day":                       dayView{},
		"month":                     map[string]any{"month": "", "today": "", "days": []monthDay{}, "page_id": int64(0), "body": "", "version": "", "cover": core.Cover{}},
		"week":                      map[string]any{"week": "", "from": "", "through": "", "today": "", "days": []monthDay{}, "page_id": int64(0), "body": "", "version": "", "cover": core.Cover{}},
		"year":                      map[string]any{"year": "", "today": "", "written": 0, "readings": 0, "months": []yearMonth{}, "quarters": []yearQuarter{}, "page_id": int64(0), "body": "", "version": "", "cover": core.Cover{}},
		"page, person, file":        core.Page{},
		"result of tombstone-pages": map[string]any{"tombstoned": []int64{}, "count": 0, "revive": ""},
		"cover-choice":              coverChoiceProps{Rows: []coverRow{}},
		"measurement":               core.Measurement{},
		"series": map[string]any{"metric": "", "from": "", "to": "", "readings": []core.Reading{}, "unit": "", "daily": false,
			// and the page of the metric (plan 144): the properties of a page but its `metric` object
			"id": int64(0), "entity_type": "", "title": "", "body": "", "created_at": "", "version": "", "is_day_page": false,
			"links": []core.Edge{}, "backlinks": []core.Edge{}, "cover": core.Cover{}},
		"settings": map[string]any{"model_url": "", "model": "", "model_key": "", "owner_number": ""},
		"conversation": map[string]any{"day": "", "turns": []Turn{},
			// and the page of the conversation (plan 146): the properties of a page
			"id": int64(0), "entity_type": "", "title": "", "body": "", "created_at": "", "version": "", "is_day_page": false,
			"links": []core.Edge{}, "backlinks": []core.Edge{}, "cover": core.Cover{}},
		"metrics":    map[string]any{"metrics": []metricRow{}, "groups": []metricGroup{}, "categories": []core.Category{}, "today": ""},
		"snapshot":   map[string]any{"snapshot": "", "download": "", "restore_check": core.IntegrityResult{}},
		"rebuilt":    map[string]any{"documents": 0},
		"resynced":   core.Resync{Created: []string{}, Revived: []string{}, Skipped: []string{}},
		"tombstoned": map[string]any{"ids": []int64{}, "count": 0},
		"filed":      map[string]any{"category": core.Category{}, "created": false, "ids": []int64{}, "count": 0, "added": 0, "moved": 0, "removed": 0},
		"integrity":  core.IntegrityResult{},
		"result":     map[string]any{"title": "", "sha256": "", "mime": "", "picture": false},
		"result of capture, create-page, save-body": core.Sync{},
		"result of add-file":                        core.Kept{},
	}
	for section, shape := range shapes {
		want := map[string]bool{}
		walk("", reflect.ValueOf(shape), want)
		got, ok := documented[section]
		if !ok {
			t.Errorf("API.md has no section %q", section)
			continue
		}
		for p := range want {
			if !got[p] {
				t.Errorf("API.md %q lacks %s", section, p)
			}
		}
		for p := range got {
			if !want[p] {
				t.Errorf("API.md %q documents %s, which the code does not send", section, p)
			}
		}
	}
	for section := range documented {
		if _, ok := shapes[section]; !ok && section != "errors" && section != "actions" {
			t.Errorf("API.md section %q is not checked here: add its shape", section)
		}
	}
	if e := documented["errors"]; !e["status"] || !e["code"] || !e["message"] || len(e) != 3 {
		t.Errorf("errors section: %v", e)
	}

	// Live: every route's top-level keys are documented for its section.
	h, c, ids := fixture(t)
	if _, _, err := c.AppendParagraph(t.Context(), "cli", core.ConversationTitle("2031-05-01"), "2031-05-01", core.ConversationCategory, conversationFirstLine("2031-05-01"),
		func(body string) string { return body + "\n\n**Owner** (09.07): Hello" }); err != nil {
		t.Fatal(err)
	}
	live := []struct{ section, route string }{
		{"root", "/"}, {"days", "/days"}, {"search", "/search?q=word"},
		{"day", "/days/2031-05-01"}, {"page, person, file", ids["page"]}, {"page, person, file", ids["person"]},
		{"page, person, file", ids["file"]},
		{"measurement", ids["measurement"]}, {"series", "/metrics/Weight"}, {"metrics", "/metrics"},
		{"browse", "/browse?in=A+note"}, {"browse", "/browse?kind=person"}, {"browse", "/browse?kind=media"}, {"browse", "/browse?ghosts=1"}, {"browse", "/browse?deleted=1"},
		{"integrity", "/integrity"}, {"settings", "/settings"}, {"conversation", "/days/2031-05-01/conversation"}, {"conversation", "/days/2031-05-02/conversation"}, {"cover-choice", ids["page"] + "/cover"},
		{"month", "/months/2031-05"}, {"year", "/years/2031"}, {"week", "/weeks/2031-W20"},
	}
	named := func(section, key string) bool { // a list is documented as "name[]"
		return documented[section][key] || documented[section][key+"[]"]
	}
	for _, l := range live {
		props := properties(t, h, "GET", l.route, "")
		for k := range props {
			if !named(l.section, k) {
				t.Errorf("GET %s sends %q, which API.md %q does not name", l.route, k, l.section)
			}
		}
	}
	for k := range properties(t, h, "POST", "/snapshots", "") {
		if !named("snapshot", k) {
			t.Errorf("POST /snapshots sends %q, which API.md \"snapshot\" does not name", k)
		}
	}
	for k := range properties(t, h, "POST", "/search/rebuild", "") {
		if !named("rebuilt", k) {
			t.Errorf("POST /search/rebuild sends %q, which API.md \"rebuilt\" does not name", k)
		}
	}
	for k := range properties(t, h, "POST", "/links/resync", "") {
		if !named("resynced", k) {
			t.Errorf("POST /links/resync sends %q, which API.md \"resynced\" does not name", k)
		}
	}
	for k := range properties(t, h, "POST", "/pages/tombstone", "ids="+strings.TrimPrefix(ids["page"], "/pages/")) {
		if !named("tombstoned", k) {
			t.Errorf("POST /pages/tombstone sends %q, which API.md \"tombstoned\" does not name", k)
		}
	}
	for k := range properties(t, h, "POST", "/pages/file", "category=Filed+Things&ids="+strings.TrimPrefix(ids["person"], "/pages/")) {
		if !named("filed", k) {
			t.Errorf("POST /pages/file sends %q, which API.md \"filed\" does not name", k)
		}
	}
	for k := range properties(t, h, "GET", "/pages/999999", "") {
		if !documented["errors"][k] {
			t.Errorf("an error sends %q, which API.md \"errors\" does not name", k)
		}
	}
	_ = c
}

// readReference is the property paths of every "### section" of API.md, from the first column of its tables.
func readReference(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]map[string]bool{}
	row := regexp.MustCompile("^\\| `([^`]+)` \\|")
	section := ""
	for sc := bufio.NewScanner(f); sc.Scan(); {
		line := sc.Text()
		if strings.HasPrefix(line, "### ") {
			section = strings.TrimPrefix(line, "### ")
			out[section] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(line, "## Errors") {
			section = "errors"
			out[section] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			section = ""
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil && section != "" {
			if out[section][m[1]] {
				t.Errorf("API.md %q lists %s twice", section, m[1])
			}
			out[section][m[1]] = true
		}
	}
	return out
}

// walk adds the JSON paths of v: a struct's fields by their json names (embedded ones flattened), an object as
// "name" and "name.field", a list of objects as "name[]" and "name[].field", anything else as a leaf.
func walk(prefix string, v reflect.Value, out map[string]bool) {
	t := v.Type()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Map:
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, k := range keys {
			add(prefix+k.String(), v.MapIndex(k).Elem(), out)
		}
	case reflect.Struct:
		walkType(prefix, t, out)
	}
}

func add(path string, v reflect.Value, out map[string]bool) {
	t := v.Type()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t.Kind() == reflect.Struct:
		out[path] = true
		walkType(path+".", t, out)
	case t.Kind() == reflect.Slice && elemStruct(t) != nil:
		out[path+"[]"] = true
		walkType(path+"[].", elemStruct(t), out)
	default:
		out[path] = true
	}
}

func elemStruct(t reflect.Type) reflect.Type {
	e := t.Elem()
	for e.Kind() == reflect.Pointer {
		e = e.Elem()
	}
	if e.Kind() == reflect.Struct {
		return e
	}
	return nil
}

func walkType(prefix string, t reflect.Type, out map[string]bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			walkType(prefix, f.Type, out)
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		add(prefix+name, reflect.New(f.Type).Elem(), out)
	}
}

// fixture is a database with one of everything the routes show, and the hrefs of the pages made.
func fixture(t *testing.T) (http.Handler, *core.Store, map[string]string) {
	t.Helper()
	d := dbtest.New(t)
	s := &core.Store{DB: d, SettingsPath: core.SettingsPathFor(d.Path())}
	ctx := t.Context()
	ids := map[string]string{}
	personID, err := s.CreatePerson(ctx, "cli", "Sam", "Sam Synthetic", "2001-01-01", "")
	if err != nil {
		t.Fatal(err)
	}
	ids["person"] = fmt.Sprintf("/pages/%d", personID)
	pageID, _, err := s.CreatePage(ctx, "cli", "A note", "word [[Sam]] #tag", "")
	if err != nil {
		t.Fatal(err)
	}
	ids["page"] = fmt.Sprintf("/pages/%d", pageID)
	if _, _, err := s.Capture(ctx, "cli", "2031-05-01", "word at [[Lakeside]]", nil); err != nil {
		t.Fatal(err)
	}
	kept, err := s.AddFile(ctx, "cli", core.FileIn{Title: "memo.txt", SHA256: strings.Repeat("b", 64), MIME: "text/plain", Body: "word", Day: "2031-05-01"})
	if err != nil {
		t.Fatal(err)
	}
	ids["file"] = fmt.Sprintf("/pages/%d", kept.ID)
	if _, err := s.CreateMetric(ctx, "cli", core.NewMetric{Name: "Weight", Unit: "kg", Note: "morning"}); err != nil {
		t.Fatal(err)
	}
	mid, err := s.Record(ctx, "cli", core.Reading{Metric: "Weight", Day: "2031-05-01", Value: 70})
	if err != nil {
		t.Fatal(err)
	}
	ids["measurement"] = fmt.Sprintf("/measurements/%d", mid)
	if _, err := s.CreateMetric(ctx, "cli", core.NewMetric{Name: "Walk", Note: "1 = done"}); err != nil {
		t.Fatal(err)
	}
	h := NewWith(s, nil)
	return h, s, ids
}

// properties is the top-level property keys of a route's JSON answer.
func properties(t *testing.T, h http.Handler, method, route, body string) map[string]bool {
	t.Helper()
	req := httptest.NewRequest(method, route, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var e struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("%s %s: %d %v %.200s", method, route, rec.Code, err, rec.Body.String())
	}
	if rec.Code >= 300 && route != "/pages/999999" {
		t.Fatalf("%s %s: %d %.300s", method, route, rec.Code, rec.Body.String())
	}
	out := map[string]bool{}
	for k := range e.Properties {
		out[k] = true
	}
	return out
}
