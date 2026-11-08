package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"aminte/client"
	"aminte/internal/api/apitest"
	"aminte/internal/core"
	"aminte/internal/photo/phototest"
	"aminte/siren"
)

// TestParityOfTheSurfaces is the parity table of the surfaces (AGENTS.md: client parity is a first-class contract). A
// row is one scenario of the domain. It runs on each surface on a fresh file, and it holds the answer of the surface
// and the state of the file after it, read back through the HTTP API. The surfaces are the client in process (http),
// the client over a server (remote) and the command lines of this program (cli). Each runs once as an owner and once as
// an agent. The MCP server is the extension extensions/mcp: its own end-to-end test holds that surface. Where a surface
// has a shortcut of an action, the row runs the shortcut there: the shortcut wraps the action, so its answer is the
// action's. Each surface keeps the tests of what is its own: the flags, the output and snapshot --snapshots of the CLI; the transport of the client. The clock of every row is
// fixed at parityToday, so no row depends on the date of the run.
//
// A row stays for what a surface adapts: a CLI shortcut (page, rename, day, snapshot), the word today in a day field, a
// number or a flag, one kind of field (a hidden version, a password), the refusals 422, 409 and 403, the owner and the
// agent, a GET and a POST. The generic path, do and client.Do, is one path for every action. A row that only sends one
// more action through it is not here: the outcome of that action is the test of internal/api that holds it.
func TestParityOfTheSurfaces(t *testing.T) {
	for _, row := range parityRows() {
		for _, s := range paritySurfaces {
			t.Run(row.name+"/"+s.name+"/"+s.source, func(t *testing.T) {
				t.Parallel()
				read, h, store := apitest.FreshOn(t, "cli", parityToday)
				x := &parityRun{t: t, owner: !strings.HasPrefix(s.source, "agent:"), read: read, store: store}
				x.call = s.open(t, h, store.DB.Path(), s.source)
				row.run(x)
			})
		}
	}
}

// paritySurfaces are the columns of the table: a surface and the source it writes as. The three names of an owner
// (cli, api, ui) are each on one surface; the API decides by owner or agent only.
var paritySurfaces = []struct {
	name, source string
	open         func(t *testing.T, h http.Handler, path, source string) surfaceCall
}{
	{"http", "api", openHTTP}, {"http", "agent:parity", openHTTP},
	{"remote", "ui", openRemote}, {"remote", "agent:parity", openRemote},
	{"cli", "cli", openCLI}, {"cli", "agent:parity", openCLI},
}

// parityToday is today on the clock of every row: the clock of the store of the API, and of the store that a command
// line opens.
const parityToday = "2031-05-20"

// surfaceCall runs the catalog's action with the field values on one surface. It returns the entity of the answer
// (the error entity of a refusal too) and the error of a refusal.
type surfaceCall func(action string, args map[string]string) (*siren.Entity, error)

func openHTTP(t *testing.T, h http.Handler, _, source string) surfaceCall {
	return catalogCall(t, client.InProcess(h, source))
}

func openRemote(t *testing.T, h http.Handler, _, source string) surfaceCall {
	return catalogCall(t, apitest.Remote(t, h, source))
}

// catalogCall runs an action of GET /actions with the client.
func catalogCall(t *testing.T, c *client.Client) surfaceCall {
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	return func(action string, args map[string]string) (*siren.Entity, error) {
		t.Helper()
		return c.Do(t.Context(), apitest.Action(t, catalog, action), args)
	}
}

// openCLI runs each action as a command line in process, on the file at path, and reads the JSON that the command
// writes. The source cli is the default of the CLI, so its line names none. The store that the line opens is in the
// zone of this machine, and its clock is at noon of parityToday in that zone: its today is parityToday in any zone.
func openCLI(t *testing.T, _ http.Handler, path, source string) surfaceCall {
	e, stdout, _ := testEnv(t)
	today, err := time.ParseInLocation(time.DateOnly, parityToday, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	e.clock = func() time.Time { return today.Add(12 * time.Hour) }
	return func(action string, args map[string]string) (*siren.Entity, error) {
		t.Helper()
		argv := []string{"--db", path}
		if source != "cli" {
			argv = append(argv, "--source", source)
		}
		stdout.Reset()
		err := runArgs(t.Context(), e, append(argv, cliLine(action, args)...))
		if stdout.Len() == 0 {
			return nil, err
		}
		return decodeEntity(t, stdout.Bytes()), err
	}
}

// cliLine is the command line of an action: the shortcut of the CLI when one takes these fields (page TITLE, rename
// ID TITLE, day [DAY]), else do ACTION field=value, with the fields in the order of their names.
func cliLine(action string, args map[string]string) []string {
	day, onlyDay := args["day"]
	switch {
	case action == "find" && len(args) == 1 && args["title"] != "":
		return []string{"page", args["title"]}
	case action == "rename" && len(args) == 2 && args["id"] != "" && args["title"] != "":
		return []string{"rename", args["id"], args["title"]}
	case action == "snapshot" && len(args) == 0:
		return []string{"snapshot"}
	case action == "open-day" && len(args) == 0:
		return []string{"day"}
	case action == "open-day" && len(args) == 1 && onlyDay:
		return []string{"day", day}
	}
	line := []string{"do", action}
	for _, k := range slices.Sorted(maps.Keys(args)) {
		line = append(line, k+"="+args[k])
	}
	return line
}

// decodeEntity reads the JSON of an entity with its numbers exact, as the client reads them.
func decodeEntity(t *testing.T, b []byte) *siren.Entity {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var e siren.Entity
	if err := d.Decode(&e); err != nil {
		t.Fatalf("not an entity: %v: %s", err, b)
	}
	return &e
}

// parityRun is one row on one surface.
type parityRun struct {
	t     *testing.T
	owner bool           // the source of the surface is an owner, not agent:*
	call  surfaceCall    // the surface under test
	read  *client.Client // the HTTP API in process, as the owner: the state of the file after a call
	store *core.Store    // the setup of a row, and a probe of the file
}

type parityRow struct {
	name string
	run  func(x *parityRun)
}

// ok is the entity of a call that must succeed.
func (x *parityRun) ok(action string, args map[string]string) *siren.Entity {
	x.t.Helper()
	e, err := x.call(action, args)
	if err != nil {
		x.t.Fatalf("%s %v: %v", action, args, err)
	}
	return e
}

// refused calls an action that must be refused with the status and the code of the error entity, and returns the
// message of the entity. The error has the status too (main writes the
// error of the CLI as "aminte: 409: ...").
func (x *parityRun) refused(action string, args map[string]string, status int, code string) string {
	x.t.Helper()
	e, err := x.call(action, args)
	if err == nil || e == nil {
		x.t.Fatalf("%s %v: %v, %v; want a refusal %d %s", action, args, e, err, status, code)
	}
	p := props(x.t, e)
	if fmt.Sprint(p["status"]) != strconv.Itoa(status) || p["code"] != code {
		x.t.Errorf("%s %v: %v %v (%v), want %d %s", action, args, p["status"], p["code"], p["message"], status, code)
	}
	if got := apitest.Status(err); got != status {
		x.t.Errorf("%s %v: the error %q has the status %d, want %d (-1: it is not a *client.Error)", action, args, err, got, status)
	}
	return fmt.Sprint(p["message"])
}

// page reads the page with the title through the HTTP API.
func (x *parityRun) page(title string) (*siren.Entity, error) {
	return x.read.Get(x.t.Context(), "/pages?title="+url.QueryEscape(title))
}

// pageByID reads the page with the id through the HTTP API, and returns its properties.
func (x *parityRun) pageByID(id int64) map[string]any {
	x.t.Helper()
	return props(x.t, apitest.Must(x.t)(x.read.Get(x.t.Context(), fmt.Sprintf("/pages/%d", id))))
}

// absent holds that no page has the title, through the HTTP API.
func (x *parityRun) absent(title string) {
	x.t.Helper()
	if e, err := x.page(title); apitest.Status(err) != http.StatusNotFound {
		x.t.Errorf("the page %s: %v %v, want none", title, e, err)
	}
}

// categories are the titles of the categories of a page (its part-of links), through the HTTP API.
func (x *parityRun) categories(id int64) string {
	x.t.Helper()
	var out []string
	links, _ := x.pageByID(id)["links"].([]any)
	for _, l := range links {
		if l := l.(map[string]any); l["kind"] == "part-of" {
			out = append(out, fmt.Sprint(l["title"]))
		}
	}
	return strings.Join(out, ",")
}

// rowField is a field of each row of a browse entity, in order.
func rowField(t *testing.T, e *siren.Entity, name string) string {
	t.Helper()
	var out []string
	rows, _ := props(t, e)["rows"].([]any)
	for _, r := range rows {
		out = append(out, fmt.Sprint(r.(map[string]any)[name]))
	}
	return strings.Join(out, ",")
}

// sql runs a statement on the file: the setup of a state that no action makes.
func (x *parityRun) sql(query string, args ...any) {
	x.t.Helper()
	if _, err := x.store.DB.W.ExecContext(x.t.Context(), query, args...); err != nil {
		x.t.Fatal(err)
	}
}

func props(t *testing.T, e *siren.Entity) map[string]any {
	t.Helper()
	p, ok := e.Properties.(map[string]any)
	if !ok {
		t.Fatalf("the entity %q has no properties: %#v", e.Title, e.Properties)
	}
	return p
}

// fieldValue is the value that the action carries for the field, nil for none.
func fieldValue(a siren.Action, name string) any {
	for _, f := range a.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func parityRows() []parityRow {
	rows := []parityRow{
		{"find", rowFind},
		{"open-day", rowOpenDay},
		{"open-day of today", rowOpenToday},
		{"the word today in a day field", rowTodayInADayField},
		{"browse", rowBrowse},
	}
	for _, kind := range []string{"page", "person", "metric", "file"} {
		rows = append(rows, parityRow{"rename a " + kind, func(x *parityRun) { rowRename(x, kind) }})
	}
	return append(rows,
		parityRow{"stale save-body", rowStaleSave},
		parityRow{"save-body at the largest version", rowLargestVersion},
		parityRow{"register-metric", rowRegisterMetric},
		parityRow{"settings", rowSettings},
		parityRow{"say", rowSay},
		parityRow{"reply", rowReply},
		parityRow{"promote", rowPromote},
		parityRow{"edit-metric", rowEditMetric},
		parityRow{"set-cover by a kept title", rowSetCover},
		parityRow{"random-page kind=day", rowRandomDay},
		parityRow{"random-page kind=person", rowRandomPage},
		parityRow{"search kind=person", rowSearchKind},
		parityRow{"create-person", rowCreatePerson},
		parityRow{"correct and retract", rowCorrectAndRetract},
		parityRow{"integrity-check", rowIntegrityCheck},
		parityRow{"snapshot", rowSnapshot},
		parityRow{"an unknown field", rowUnknownField},
		parityRow{"a value outside the options", rowAValueOutsideTheOptions},
	)
}

// A field that the action does not list is the 422 of the server on every surface, and the capture writes no day page.
func rowUnknownField(x *parityRun) {
	x.refused("capture", map[string]string{"day": "today", "text": "x", "nope": "1"}, http.StatusUnprocessableEntity, "invalid")
	x.absent(parityToday)
}

// A value outside the static options of a field is the 422 of the server on every surface.
func rowAValueOutsideTheOptions(x *parityRun) {
	x.refused("search", map[string]string{"q": "a", "kind": "planet"}, http.StatusUnprocessableEntity, "invalid")
}

// set-cover with the title of a kept picture puts its embed first in the body, so it is the page's cover; the
// page's picker lists it as the cover. A file page takes no cover, and a title that is no kept picture is refused.
// A new photo is a file field, which an MCP tool does not carry.
func rowSetCover(x *parityRun) {
	ctx := x.t.Context()
	k, err := x.store.AddFile(ctx, "cli", core.FileIn{Title: "Lake.jpg", SHA256: strings.Repeat("a", 64), MIME: "image/jpeg", Preview: phototest.JPEG(8, 8)})
	if err != nil {
		x.t.Fatal(err)
	}
	trip, _, err := x.store.CreatePage(ctx, "cli", "Trip", "Away.", "")
	if err != nil {
		x.t.Fatal(err)
	}
	x.ok("set-cover", map[string]string{"id": id(trip), "title": "Lake.jpg"})
	p := x.pageByID(trip)
	cover, _ := p["cover"].(map[string]any)
	if p["body"] != "![[Lake.jpg]]\n\nAway." || cover["id"] != json.Number(id(k.ID)) || cover["title"] != "Lake.jpg" {
		x.t.Errorf("Trip after set-cover: %q, cover %v", p["body"], p["cover"])
	}
	choice := props(x.t, apitest.Must(x.t)(x.read.Get(ctx, "/pages/"+id(trip)+"/cover")))
	rows, _ := choice["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["current"] != true {
		x.t.Errorf("the picker of Trip: %v, want Lake.jpg as the cover", rows)
	}
	x.refused("set-cover", map[string]string{"id": id(k.ID), "title": "Lake.jpg"}, http.StatusUnprocessableEntity, "invalid")
	x.refused("set-cover", map[string]string{"id": id(trip), "title": "Nothing.jpg"}, http.StatusUnprocessableEntity, "invalid")
}

// find opens the page with the title; a title that no page has is a 404.
func rowFind(x *parityRun) {
	ana, _, err := x.store.CreatePage(x.t.Context(), "cli", "Ana", "Synthetic page", "")
	if err != nil {
		x.t.Fatal(err)
	}
	e := x.ok("find", map[string]string{"title": "Ana"})
	if p := props(x.t, e); e.Title != "Ana" || apitest.Href(e, "self") != "/pages/"+id(ana) || p["id"] != json.Number(id(ana)) || p["body"] != "Synthetic page" {
		x.t.Errorf("find Ana: %q %v %v", e.Title, apitest.Href(e, "self"), p)
	}
	x.refused("find", map[string]string{"title": "Nobody"}, http.StatusNotFound, "not_found")
}

// random-page with the kind day is the day view of a live day page picked at random, up to today
// (docs/plans/130-the-explore-pass.md): with one live day page up to today, a tombstoned one and one after today, it is
// the live one. With none, a 404.
func rowRandomDay(x *parityRun) {
	ctx := x.t.Context()
	day := map[string]string{"kind": "day"}
	x.refused("random-page", day, http.StatusNotFound, "not_found")
	for _, day := range []string{"2021-05-05", "2022-06-06", "2098-01-01"} {
		if _, _, err := x.store.Capture(ctx, "cli", day, "A day.", nil); err != nil {
			x.t.Fatal(err)
		}
	}
	gone, err := x.store.PageID(ctx, "2022-06-06")
	if err == nil {
		err = x.store.Tombstone(ctx, "cli", gone)
	}
	if err != nil {
		x.t.Fatal(err)
	}
	for range 5 {
		if e := x.ok("random-page", day); e.Title != "2021-05-05" || e.Class[0] != "day" {
			x.t.Errorf("random-page kind=day: %v %q, want the day 2021-05-05", e.Class, e.Title)
		}
	}
}

// random-page is a live thing of the kind picked at random: never a tombstoned one. A kind with nothing is a 404, a
// kind that is not one a 422.
func rowRandomPage(x *parityRun) {
	ctx := x.t.Context()
	if _, err := x.store.CreatePerson(ctx, "cli", "Ana", "", "", ""); err != nil {
		x.t.Fatal(err)
	}
	bob, err := x.store.CreatePerson(ctx, "cli", "Bob", "", "", "")
	if err == nil {
		err = x.store.Tombstone(ctx, "cli", bob)
	}
	if err != nil {
		x.t.Fatal(err)
	}
	for range 5 {
		if e := x.ok("random-page", map[string]string{"kind": "person"}); e.Title != "Ana" || e.Class[0] != "person" {
			x.t.Errorf("random-page kind=person: %v %q, want Ana", e.Class, e.Title)
		}
	}
	x.refused("random-page", map[string]string{"kind": "place"}, http.StatusUnprocessableEntity, "invalid") // a place is no kind: it is a page
	x.refused("random-page", map[string]string{"kind": "year"}, http.StatusUnprocessableEntity, "invalid")
}

// open-day is the day view of a day; it is a read, so it makes no day page.
func rowOpenDay(x *parityRun) {
	e := x.ok("open-day", map[string]string{"day": "2031-05-14"})
	if e.Title != "2031-05-14" {
		x.t.Errorf("open-day 2031-05-14: %q", e.Title)
	}
	x.absent("2031-05-14")
}

// open-day with no day, with an empty one or with the word "today" is the day view of today, as GET /days/today is. A
// value that is not a day is refused. It is a read, so it makes no day page.
func rowOpenToday(x *parityRun) {
	for _, args := range []map[string]string{{}, {"day": ""}, {"day": "today"}} {
		if e := x.ok("open-day", args); e.Title != parityToday || apitest.Href(e, "self") != "/days/"+parityToday {
			x.t.Errorf("open-day %v: %q %q, want the day %s", args, e.Title, apitest.Href(e, "self"), parityToday)
		}
	}
	x.refused("open-day", map[string]string{"day": "x"}, http.StatusUnprocessableEntity, "invalid")
	x.absent(parityToday)
}

// The word "today" in a day field is the local day of the server's clock: a reading that record writes and a file
// that add-file keeps with the day today have the day parityToday, in the answer and in the file. A value that is
// neither a day nor the word is the 422 of the schema's CHECK (measurements_day, entities_day), and it writes nothing.
func rowTodayInADayField(x *parityRun) {
	ctx := x.t.Context()
	if _, err := x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: "Weight", Unit: "kg"}); err != nil {
		x.t.Fatal(err)
	}
	m := x.ok("record", map[string]string{"metric": "Weight", "day": "today", "value": "71"})
	if p := props(x.t, m); m.Class[0] != "measurement" || p["day"] != parityToday || p["value"] != json.Number("71") {
		x.t.Errorf("record with the day today: %v %v %v, want 71 on %s", m.Class, p["day"], p["value"], parityToday)
	}
	f := x.ok("add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf", "day": "today"})
	if p := props(x.t, f); f.Class[0] != "file" || p["day"] != parityToday {
		x.t.Errorf("add-file with the day today: %v %v, want the day %s", f.Class, p["day"], parityToday)
	}
	for _, tc := range []struct {
		action, check string
		args          map[string]string
	}{
		{"record", "measurements_day", map[string]string{"metric": "Weight", "day": "Today", "value": "72"}},
		{"add-file", "entities_day", map[string]string{"title": "Notes.pdf", "sha256": strings.Repeat("cd", 32), "mime": "application/pdf", "day": "yesterday"}},
	} {
		if msg := x.refused(tc.action, tc.args, http.StatusUnprocessableEntity, "invalid"); !strings.Contains(msg, tc.check) {
			x.t.Errorf("%s %v: %q, want the CHECK %s", tc.action, tc.args, msg, tc.check)
		}
	}
	x.absent("Notes.pdf")
	day := props(x.t, apitest.Must(x.t)(x.read.Get(ctx, "/days/"+parityToday)))
	var readings []string
	rows, _ := day["readings"].([]any)
	for _, r := range rows {
		r, _ := r.(map[string]any)
		readings = append(readings, fmt.Sprint(r["metric"], " ", r["value"]))
	}
	if strings.Join(readings, ",") != "Weight 71" {
		x.t.Errorf("the readings of %s: %v, want Weight 71 alone", parityToday, readings)
	}
	if e, err := x.page("Scan.pdf"); err != nil || props(x.t, e)["day"] != parityToday {
		x.t.Errorf("the page Scan.pdf: %v %v, want the day %s", e, err, parityToday)
	}
}

// browse lists the things of one kind, in a category or in none, a page of rows at a time; a combination with no
// meaning is a 422.
func rowBrowse(x *parityRun) {
	ctx := x.t.Context()
	var people []int64
	for _, title := range []string{"Ana", "Bo", "Cy"} {
		p, err := x.store.CreatePerson(ctx, "cli", title, "", "", "")
		if err != nil {
			x.t.Fatal(err)
		}
		people = append(people, p)
	}
	if _, err := x.store.FilePages(ctx, "cli", []int64{people[2], people[0]}, "Family", "", false); err != nil {
		x.t.Fatal(err)
	}
	for _, title := range []string{"Pia", "Bea", "Cal"} {
		category := "Notes"
		if title == "Pia" {
			category = ""
		}
		if _, _, err := x.store.CreatePage(ctx, "cli", title, "", category); err != nil {
			x.t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		args           map[string]string
		titles, counts string // counts: the mentions of each person
	}{
		{map[string]string{}, "Bea,Cal,Family,Notes,Pia", ""},
		{map[string]string{"in": "Notes"}, "Bea,Cal", ""},
		{map[string]string{"unfiled": "1"}, "Pia", ""}, // a category is not in no category
		{map[string]string{"limit": "1", "offset": "1"}, "Cal", ""},
		{map[string]string{"in": "Family", "kind": "person"}, "Ana,Cy", "0,0"},
		{map[string]string{"kind": "person", "unfiled": "1"}, "Bo", "0"},
	} {
		e := x.ok("browse", tc.args)
		if titles, counts := rowField(x.t, e, "title"), rowField(x.t, e, "mentions"); e.Class[0] != "browse" || titles != tc.titles || (tc.counts != "" && counts != tc.counts) {
			x.t.Errorf("browse %v: %v %s (mentions %s), want %s (mentions %s)", tc.args, e.Class, titles, counts, tc.titles, tc.counts)
		}
	}
	x.refused("browse", map[string]string{"kind": "all"}, http.StatusUnprocessableEntity, "invalid")
}

// search with a kind lists only the hits of that kind (docs/plans/130-the-explore-pass.md); a kind that is not one is
// a 422.
func rowSearchKind(x *parityRun) {
	ctx := x.t.Context()
	if _, _, err := x.store.Capture(ctx, "cli", "2021-05-05", "A lantern.", nil); err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.store.CreatePerson(ctx, "cli", "Keeper of the lantern", "", "", ""); err != nil {
		x.t.Fatal(err)
	}
	e := x.ok("search", map[string]string{"q": "lantern", "kind": "person"})
	p := props(x.t, e)
	var hits []string
	for _, h := range p["hits"].([]any) {
		h := h.(map[string]any)
		hits = append(hits, fmt.Sprint(h["kind"], " ", h["title"]))
	}
	if strings.Join(hits, ",") != "person Keeper of the lantern" || p["kind"] != "person" {
		x.t.Errorf("search kind=person: %v, kind %v", hits, p["kind"])
	}
	x.refused("search", map[string]string{"q": "lantern", "kind": "metric"}, http.StatusUnprocessableEntity, "invalid")
}

// rename gives a page of each type a new title and keeps the old one as an alias: find by the old title and by the
// new one finds the same page, and its other fields and the links to it stay. A title that another page holds, a date, or a title that
// is not a valid name is refused, and nothing is written, not even a name.
func rowRename(x *parityRun, kind string) {
	ctx := x.t.Context()
	old := "Old " + kind
	var p int64
	var err error
	switch kind {
	case "page":
		p, _, err = x.store.CreatePage(ctx, "cli", old, "original prose", "")
	case "person":
		p, err = x.store.CreatePerson(ctx, "cli", old, "Synthetic Person", "", "2031-01-01")
	case "metric":
		if _, err = x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: old, Unit: "kg", Note: "original metric"}); err == nil {
			p, err = x.store.PageID(ctx, old)
		}
	case "file":
		var kept core.Kept
		kept, err = x.store.AddFile(ctx, "cli", core.FileIn{Title: old, SHA256: strings.Repeat("a", 64), MIME: "text/plain", Body: "original file", Day: "2031-01-01"})
		p = kept.ID
	}
	if err != nil {
		x.t.Fatal(err)
	}
	ref, _, err := x.store.CreatePage(ctx, "cli", "Reference", "See [["+old+"]].", "")
	if err != nil {
		x.t.Fatal(err)
	}
	same := func(m map[string]any) map[string]any {
		return map[string]any{"id": m["id"], "type": m["entity_type"], "body": m["body"], "day": m["day"], "person": m["person"], "file": m["file"]}
	}
	initial := same(x.pageByID(p))
	for _, title := range []string{"New " + kind, "NEW " + strings.ToUpper(kind), old, "Final " + kind} {
		if e := x.ok("rename", map[string]string{"id": id(p), "title": title}); e.Title != title {
			x.t.Fatalf("rename to %q: %q", title, e.Title)
		}
		for _, name := range []string{old, title} {
			found := x.ok("find", map[string]string{"title": name})
			apitest.Action(x.t, found, "rename")
			if found.Title != title || props(x.t, found)["id"] != json.Number(id(p)) {
				x.t.Fatalf("find %q after the rename to %q: %q %v", name, title, found.Title, props(x.t, found)["id"])
			}
		}
		if now := same(x.pageByID(p)); !reflect.DeepEqual(now, initial) {
			x.t.Fatalf("the rename to %q changed the page: %v, was %v", title, now, initial)
		}
		r := x.pageByID(ref)
		links, _ := r["links"].([]any)
		if r["body"] != "See [["+old+"]]." || len(links) != 1 || links[0].(map[string]any)["id"] != json.Number(id(p)) || links[0].(map[string]any)["title"] != title {
			x.t.Fatalf("the page that links %q after the rename to %q: %v %v", old, title, r["body"], links)
		}
	}
	if _, _, err := x.store.CreatePage(ctx, "cli", "Ghost ref", "[[Reserved ghost]]", ""); err != nil {
		x.t.Fatal(err)
	}
	tomb, _, err := x.store.CreatePage(ctx, "cli", "Reserved tombstone", "", "")
	if err != nil {
		x.t.Fatal(err)
	}
	if err := x.store.Tombstone(ctx, "cli", tomb); err != nil {
		x.t.Fatal(err)
	}
	before := x.pageByID(p)
	names := func() (n int) {
		if err := x.store.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM entity_names").Scan(&n); err != nil {
			x.t.Fatal(err)
		}
		return n
	}
	count := names()
	for _, tc := range []struct {
		title  string
		status int
		code   string
		says   string // a part of the message; "" for any
	}{
		{"Reserved ghost", http.StatusConflict, "exists", ""},
		{"Reserved tombstone", http.StatusConflict, "exists", ""},
		{"2031-02-01", http.StatusUnprocessableEntity, "invalid", "a journal day has only its canonical date name"}, // entity_names_day_insert
		{"Lab [unsafe]", http.StatusUnprocessableEntity, "invalid", ""},
	} {
		if msg := x.refused("rename", map[string]string{"id": id(p), "title": tc.title}, tc.status, tc.code); !strings.Contains(msg, tc.says) {
			x.t.Errorf("the refused rename to %q says %q, want %q in it", tc.title, msg, tc.says)
		}
		if after := x.pageByID(p); !reflect.DeepEqual(after, before) {
			x.t.Fatalf("the refused rename to %q changed the page: %v, was %v", tc.title, after, before)
		}
	}
	if n := names(); n != count {
		x.t.Errorf("the refused renames left %d names, were %d", n, count)
	}
}

// A save with an old version is a 409 stale_version and writes nothing: the version of the last read, and a clock
// token equal to updated_at (the token of an old program). The same save with the body the page holds is a no-op
// that succeeds. The version is a string exact above 2^53.
func rowStaleSave(x *parityRun) {
	p, _, err := x.store.CreatePage(x.t.Context(), "cli", "Version parity", "first", "")
	if err != nil {
		x.t.Fatal(err)
	}
	x.sql(`UPDATE entities SET revision = 9007199254740993 WHERE id = ?`, p)
	var clock string
	if err := x.store.DB.R.QueryRowContext(x.t.Context(), `SELECT updated_at FROM entities WHERE id = ?`, p).Scan(&clock); err != nil {
		x.t.Fatal(err)
	}
	read := x.ok("find", map[string]string{"title": "Version parity"})
	if v, filled := props(x.t, read)["version"], fieldValue(apitest.Action(x.t, read, "save-body"), "version"); v != "9007199254740993" || filled != "9007199254740993" {
		x.t.Fatalf("the version %#v and the save-body version %#v, want 9007199254740993 as a string", v, filled)
	}
	saved := x.ok("save-body", map[string]string{"id": id(p), "version": "9007199254740993", "body": "committed [[Kept]]"})
	if v := props(x.t, saved)["version"]; v != "9007199254740994" {
		x.t.Fatalf("the version after the save: %#v, want 9007199254740994", v)
	}
	x.sql(`UPDATE entities SET updated_at = ? WHERE id = ?`, clock, p)
	for _, stale := range []string{"9007199254740993", clock, "2026-01-01T00:00:00.000Z"} {
		x.refused("save-body", map[string]string{"id": id(p), "version": stale, "body": "stale [[Forbidden]]"}, http.StatusConflict, "stale_version")
	}
	if now := x.pageByID(p); now["body"] != "committed [[Kept]]" || now["version"] != "9007199254740994" {
		x.t.Errorf("after the stale saves: %v %v, want the committed body at 9007199254740994", now["body"], now["version"])
	}
	x.absent("Forbidden")
	// a save sent again after its reply was lost: the old version, the body the page holds
	x.ok("save-body", map[string]string{"id": id(p), "version": "9007199254740993", "body": "committed [[Kept]]"})
	if now := x.pageByID(p); now["body"] != "committed [[Kept]]" || now["version"] != "9007199254740994" {
		x.t.Errorf("after the resent save: %v %v, want the committed body still at 9007199254740994", now["body"], now["version"])
	}
}

// At the largest version, 2^63-1, the version is exact on the surface and in the action's field. A save of the same
// body writes nothing and is accepted; a save of a new body is refused, as the version cannot grow, and nothing is
// written.
func rowLargestVersion(x *parityRun) {
	const largest = "9223372036854775807"
	p, _, err := x.store.CreatePage(x.t.Context(), "cli", "Version max", "committed [[Kept]]", "")
	if err != nil {
		x.t.Fatal(err)
	}
	x.sql(`UPDATE entities SET revision = `+largest+` WHERE id = ?`, p)
	read := x.ok("find", map[string]string{"title": "Version max"})
	if v, filled := props(x.t, read)["version"], fieldValue(apitest.Action(x.t, read, "save-body"), "version"); v != largest || filled != largest {
		x.t.Fatalf("the version %#v and the save-body version %#v, want %s as a string", v, filled, largest)
	}
	x.ok("save-body", map[string]string{"id": id(p), "version": largest, "body": "committed [[Kept]]"})
	x.refused("save-body", map[string]string{"id": id(p), "version": largest, "body": "overflow [[Forbidden]]"}, http.StatusUnprocessableEntity, "invalid")
	if now := x.pageByID(p); now["body"] != "committed [[Kept]]" || now["version"] != largest {
		x.t.Errorf("after the refused save: %v %v, want the committed body at %s", now["body"], now["version"], largest)
	}
	x.absent("Forbidden")
}

// register-metric is the owner's: an owner registers a metric, an agent gets a 403 and nothing is written.
func rowRegisterMetric(x *parityRun) {
	args := map[string]string{"name": "Weight", "unit": "kg"}
	switch {
	case x.owner:
		if e := x.ok("register-metric", args); e.Title != "Weight" {
			x.t.Errorf("register-metric Weight: %q", e.Title)
		}
		if e, err := x.page("Weight"); err != nil || props(x.t, e)["entity_type"] != "metric" {
			x.t.Errorf("the page Weight: %v %v, want a metric", e, err)
		}
		// The mark: Weight has none, Run is registered with daily 1. The page and the board both say so.
		x.ok("register-metric", map[string]string{"name": "Run", "daily": "1"})
		// Sent again with another mark it is refused; sent with no mark it is returned as it is.
		x.refused("register-metric", map[string]string{"name": "Run", "daily": "0"}, http.StatusConflict, "conflict")
		x.ok("register-metric", map[string]string{"name": "Run"})
		for name, want := range map[string]bool{"Weight": false, "Run": true} {
			pid, err := x.store.PageID(x.t.Context(), name)
			if err != nil {
				x.t.Fatal(err)
			}
			if m, _ := x.pageByID(pid)["metric"].(map[string]any); m["daily"] != want {
				x.t.Errorf("the page %s: metric %v, want daily %v", name, m, want)
			}
			if got := x.boardDaily(name); got != want {
				x.t.Errorf("GET /metrics: daily of %s is %v, want %v", name, got, want)
			}
		}
		return
	default:
		x.refused("register-metric", args, http.StatusForbidden, "forbidden")
	}
	x.absent("Weight")
}

// boardDaily is the daily mark that GET /metrics says of the metric name.
func (x *parityRun) boardDaily(name string) bool {
	x.t.Helper()
	rows, _ := props(x.t, apitest.Must(x.t)(x.read.Get(x.t.Context(), "/metrics")))["metrics"].([]any)
	for _, r := range rows {
		if m, _ := r.(map[string]any); m["name"] == name {
			d, _ := m["daily"].(bool)
			return d
		}
	}
	x.t.Fatalf("GET /metrics lists no metric %s", name)
	return false
}

// promote makes a plain page a person on every surface, with the page's title as its name when none is sent. A person
// promoted again is a 409. A metric is never promoted: the schema refuses it (entities_promote_from_page) as a 422
// with the sentence of the trigger, and so is register-metric with the title of a person, which only an owner may
// call. A refused call writes nothing.
func rowPromote(x *parityRun) {
	ctx := x.t.Context()
	p, _, err := x.store.CreatePage(ctx, "cli", "Lakeside", "a plain page", "")
	if err != nil {
		x.t.Fatal(err)
	}
	e := x.ok("promote", map[string]string{"id": id(p)})
	if person, _ := props(x.t, e)["person"].(map[string]any); e.Class[0] != "person" || person["name"] != "Lakeside" {
		x.t.Errorf("promote of a plain page: class %v, person %v, want a person named Lakeside", e.Class, person)
	}
	x.refused("promote", map[string]string{"id": id(p)}, http.StatusConflict, "conflict")
	if _, err := x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: "Distance", Unit: "km"}); err != nil {
		x.t.Fatal(err)
	}
	metric, err := x.store.PageID(ctx, "Distance")
	if err != nil {
		x.t.Fatal(err)
	}
	same := func(m map[string]any) map[string]any {
		return map[string]any{"type": m["entity_type"], "version": m["version"], "body": m["body"], "person": m["person"]}
	}
	before := same(x.pageByID(metric))
	const trigger = "only a plain page is promoted: a person, a metric or a file keeps its type (D20)"
	if msg := x.refused("promote", map[string]string{"id": id(metric)}, http.StatusUnprocessableEntity, "invalid"); !strings.Contains(msg, trigger) {
		x.t.Errorf("promote of a metric: %q, want the sentence of the trigger", msg)
	}
	if x.owner {
		if msg := x.refused("register-metric", map[string]string{"name": "Lakeside", "unit": "km"}, http.StatusUnprocessableEntity, "invalid"); !strings.Contains(msg, trigger) {
			x.t.Errorf("register-metric with the title of a person: %q, want the sentence of the trigger", msg)
		}
	}
	if after := same(x.pageByID(metric)); !reflect.DeepEqual(after, before) || before["type"] != "metric" {
		x.t.Errorf("the metric after the refusals: %v, want %v, a metric", after, before)
	}
}

// edit-metric sets the daily mark of a metric, on every surface, for an owner and an agent alike: 1 sets it, and a call
// with no daily (an unticked checkbox) sets 0. A plain page and a tombstoned metric are a 409. A refused call writes
// nothing, and the readings stay.
func rowEditMetric(x *parityRun) {
	ctx := x.t.Context()
	if _, err := x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: "Meditate"}); err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: "Retired"}); err != nil {
		x.t.Fatal(err)
	}
	plain, _, err := x.store.CreatePage(ctx, "cli", "Plain page", "", "")
	if err != nil {
		x.t.Fatal(err)
	}
	m, err := x.store.PageID(ctx, "Meditate")
	if err != nil {
		x.t.Fatal(err)
	}
	retired, err := x.store.PageID(ctx, "Retired")
	if err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.store.Record(ctx, "cli", core.Reading{Metric: "Meditate", Day: "2026-10-01", Value: 1}); err != nil {
		x.t.Fatal(err)
	}
	daily := func() any {
		info, _ := x.pageByID(m)["metric"].(map[string]any)
		return info["daily"]
	}
	if got := daily(); got != false {
		x.t.Fatalf("a new metric: daily %v, want false", got)
	}
	x.ok("edit-metric", map[string]string{"id": id(m), "daily": "1"})
	if got := daily(); got != true {
		x.t.Errorf("after daily 1: %v", got)
	}
	x.ok("edit-metric", map[string]string{"id": id(m)}) // an unticked checkbox sends nothing
	if got := daily(); got != false {
		x.t.Errorf("after no daily: %v", got)
	}
	x.refused("edit-metric", map[string]string{"id": id(m), "daily": "2"}, http.StatusUnprocessableEntity, "invalid")
	x.refused("edit-metric", map[string]string{"id": id(plain), "daily": "1"}, http.StatusConflict, "conflict")
	if err := x.store.Tombstone(ctx, "cli", retired); err != nil {
		x.t.Fatal(err)
	}
	x.refused("edit-metric", map[string]string{"id": id(retired), "daily": "1"}, http.StatusConflict, "conflict")
	if got := daily(); got != false {
		x.t.Errorf("after refused edits: %v", got)
	}
	if n := len(props(x.t, apitest.Must(x.t)(x.read.Get(ctx, "/metrics/Meditate")))["readings"].([]any)); n != 1 {
		x.t.Errorf("the readings of Meditate: %d, want 1", n)
	}
}

// settings is the owner's: an owner saves the model, reads it back with GET /settings, and clears a setting with an empty
// value while a field left out stays; an agent gets a 403 and no file is written. The file has the mode 0600.
func rowSettings(x *parityRun) {
	if !x.owner {
		x.refused("settings", map[string]string{"model": "x"}, http.StatusForbidden, "forbidden")
		if _, err := os.Stat(x.store.SettingsPath); !os.IsNotExist(err) {
			x.t.Errorf("a refused settings wrote the file: %v", err)
		}
		return
	}
	read := func() map[string]any { return props(x.t, apitest.Must(x.t)(x.read.Get(x.t.Context(), "/settings"))) }
	x.ok("settings", map[string]string{"model_url": "http://127.0.0.1:1234/v1", "model": "synthetic-model", "model_key": "synthetic-key"})
	if p := read(); p["model_url"] != "http://127.0.0.1:1234/v1" || p["model"] != "synthetic-model" || p["model_key"] != "synthetic-key" {
		x.t.Errorf("GET /settings after a save: %v", p)
	}
	if fi, err := os.Stat(x.store.SettingsPath); err != nil {
		x.t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		x.t.Errorf("the settings file has the mode %v, want 0600", fi.Mode().Perm())
	}
	x.ok("settings", map[string]string{"model": "other-model"}) // the fields left out stay
	if p := read(); p["model"] != "other-model" || p["model_key"] != "synthetic-key" {
		x.t.Errorf("GET /settings after a save of one field: %v", p)
	}
	x.ok("settings", map[string]string{"model_key": ""}) // an empty field clears
	if p := read(); p["model_key"] != nil || p["model"] != "other-model" {
		x.t.Errorf("GET /settings after clearing the key: %v", p)
	}
}

// say is the owner's: an agent or an import is refused (403) and writes nothing. The first turn makes the page in the
// category Conversations with the first line; a second turn is appended after a blank line; an empty text is 422 and
// writes nothing. The stamp is the time of the surface's clock, so the row checks its shape only.
func rowSay(x *parityRun) {
	if !x.owner {
		x.refused("say", map[string]string{"day": "today", "text": "Hello."}, http.StatusForbidden, "forbidden")
		x.absent("Conversation " + parityToday)
		return
	}
	x.refused("say", map[string]string{"day": "today", "text": " \n "}, http.StatusUnprocessableEntity, "invalid")
	x.absent("Conversation " + parityToday)
	e := x.ok("say", map[string]string{"day": "today", "text": "Hello."})
	if !slices.Contains(e.Class, "conversation") || len(props(x.t, e)["turns"].([]any)) != 1 {
		x.t.Errorf("say: %v %v, want the conversation with one turn", e.Class, e.Properties)
	}
	x.ok("say", map[string]string{"day": parityToday, "text": "Again."})
	pe, err := x.page("Conversation " + parityToday)
	if err != nil {
		x.t.Fatal(err)
	}
	p := props(x.t, pe)
	want := regexp.MustCompile(`^\[\[` + parityToday + `\]\]\n\n\*\*Owner\*\* \(\d\d:\d\d\): Hello\.\n\n\*\*Owner\*\* \(\d\d:\d\d\): Again\.$`)
	if body, _ := p["body"].(string); !want.MatchString(body) {
		x.t.Errorf("the body after two turns: %q", body)
	}
	id, _ := p["id"].(json.Number)
	n, _ := id.Int64()
	if got := x.categories(n); got != "Conversations" {
		x.t.Errorf("the categories of the conversation: %q, want Conversations", got)
	}
	got := apitest.Must(x.t)(x.read.Get(x.t.Context(), "/days/today/conversation"))
	if turns, _ := props(x.t, got)["turns"].([]any); len(turns) != 2 {
		x.t.Errorf("GET /days/today/conversation: %v, want two turns", turns)
	}
}

// reply is no owner action: an agent runs it as the owner does. The owner's turn is set up with the store (an agent
// cannot say). A day with no page is 404; the turn is the key of the owner's turn, and a key that no owner turn has is
// 409; a turn that has a reply is 409 and the page stays as it was; the tool lines come in one field, one for each line. The one
// difference between the writers is the save of the page: the owner edits it, an agent or an import is refused (403).
func rowReply(x *parityRun) {
	title := "Conversation " + parityToday
	args := map[string]string{"day": "today", "turn": "0123456789abcdef", "text": "Hi.", "tools": "- `search`: no hits\n- `get` → [/days/2031-05-20](/days/2031-05-20)"}
	x.refused("reply", args, http.StatusNotFound, "not_found")
	x.absent(title)
	if _, _, err := x.store.AppendParagraph(x.t.Context(), "api", title, parityToday, "Conversations", "[["+parityToday+"]]",
		func(body string) string { return body + "\n\n**Owner** (09:00): Hello." }); err != nil {
		x.t.Fatal(err)
	}
	conv := props(x.t, apitest.Must(x.t)(x.read.Get(x.t.Context(), "/days/today/conversation")))
	first, _ := conv["turns"].([]any)
	key, _ := first[0].(map[string]any)["key"].(string)
	if key == "" {
		x.t.Fatalf("the owner's turn has no key: %v", first)
	}
	x.refused("reply", map[string]string{"day": "today", "text": "Hi."}, http.StatusUnprocessableEntity, "invalid")
	x.refused("reply", map[string]string{"day": "today", "turn": key, "text": " \n "}, http.StatusUnprocessableEntity, "invalid")
	x.refused("reply", map[string]string{"day": "today", "turn": key, "text": "Hi.", "tools": "not a tool line"}, http.StatusUnprocessableEntity, "invalid")
	x.refused("reply", map[string]string{"day": "today", "turn": "1", "text": "Hi."}, http.StatusConflict, "conflict")
	args["turn"] = key
	e := x.ok("reply", args)
	p := props(x.t, e)
	turns, _ := p["turns"].([]any)
	if len(turns) != 2 {
		x.t.Fatalf("reply: %v, want the owner's turn and the reply", turns)
	}
	reply, _ := turns[1].(map[string]any)
	if reply["who"] != "aminte" || reply["text"] != "Hi." || fmt.Sprint(reply["tools"]) != "[- `search`: no hits - `get` → [/days/2031-05-20](/days/2031-05-20)]" ||
		fmt.Sprint(reply["refs"]) != "[/days/2031-05-20]" {
		x.t.Errorf("the reply: %v, want the words, two tool lines and the ref of the second", reply)
	}
	before, _ := p["body"].(string)
	x.refused("reply", args, http.StatusConflict, "conflict")
	if got := x.pageByID(mustInt(x.t, p["id"]))["body"]; got != before {
		x.t.Errorf("the page after the refused second reply: %q, want %q", got, before)
	}
	save := map[string]string{"id": fmt.Sprint(p["id"]), "body": before + "\n\nA note.", "version": fmt.Sprint(p["version"])}
	if x.owner {
		x.ok("save-body", save)
		return
	}
	x.refused("save-body", save, http.StatusForbidden, "forbidden")
}

// mustInt reads the id of an entity that the client decoded with exact numbers.
func mustInt(t *testing.T, v any) int64 {
	t.Helper()
	n, ok := v.(json.Number)
	if !ok {
		t.Fatalf("%v is not a number", v)
	}
	i, err := n.Int64()
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// create-person makes a person with a name and two dates. A death before the birth is 422 and writes no page.
func rowCreatePerson(x *parityRun) {
	args := map[string]string{"title": "Sam (barber)", "name": "Samuel Ortiz", "birth_day": "1980-02-03", "death_day": "2020-01-01"}
	e := x.ok("create-person", args)
	if p := props(x.t, e); e.Title != "Sam (barber)" || p["entity_type"] != "person" {
		x.t.Errorf("create-person: %q %v", e.Title, p["entity_type"])
	}
	pid, err := x.store.PageID(x.t.Context(), "Sam (barber)")
	if err != nil || pid == 0 {
		x.t.Fatalf("Sam (barber): %d %v", pid, err)
	}
	person, _ := x.pageByID(pid)["person"].(map[string]any)
	if person["name"] != "Samuel Ortiz" || person["birth_day"] != "1980-02-03" || person["death_day"] != "2020-01-01" {
		x.t.Errorf("the person: %v, want Samuel Ortiz, 1980-02-03, 2020-01-01", person)
	}
	x.refused("create-person", map[string]string{"title": "Lee", "birth_day": "2000-01-01", "death_day": "1999-01-01"}, http.StatusUnprocessableEntity, "invalid")
	x.absent("Lee")
}

// readings are the values of the readings that the series of the metric shows, oldest first.
func (x *parityRun) readings(metric string) string {
	x.t.Helper()
	var out []string
	series := props(x.t, apitest.Must(x.t)(x.read.Get(x.t.Context(), "/metrics/"+url.PathEscape(metric)+"?from=2031-05-01")))
	rows, _ := series["readings"].([]any)
	for _, r := range rows {
		out = append(out, fmt.Sprint(r.(map[string]any)["value"]))
	}
	return strings.Join(out, ",")
}

// correct gives a reading a new value, once; retract removes a reading from the series.
func rowCorrectAndRetract(x *parityRun) {
	ctx := x.t.Context()
	if _, err := x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: "Weight", Unit: "kg"}); err != nil {
		x.t.Fatal(err)
	}
	first, err := x.store.Record(ctx, "cli", core.Reading{Metric: "Weight", Day: "2031-05-10", Value: 80})
	if err != nil {
		x.t.Fatal(err)
	}
	second, err := x.store.Record(ctx, "cli", core.Reading{Metric: "Weight", Day: "2031-05-11", Value: 90})
	if err != nil {
		x.t.Fatal(err)
	}
	if got := x.readings("Weight"); got != "80,90" {
		x.t.Fatalf("the readings at the start: %q, want 80,90", got)
	}
	x.ok("correct", map[string]string{"id": id(first), "value": "81"})
	if got := x.readings("Weight"); got != "81,90" {
		x.t.Errorf("the readings after correct: %q, want 81,90", got)
	}
	m := props(x.t, apitest.Must(x.t)(x.read.Get(ctx, "/measurements/"+id(first))))
	if m["current"] != false || m["superseded_by"] == nil {
		x.t.Errorf("the corrected reading: %v, want not current and superseded", m)
	}
	x.refused("correct", map[string]string{"id": id(first), "value": "82"}, http.StatusUnprocessableEntity, "invalid") // the UNIQUE of supersedes_id refuses it
	if got := x.readings("Weight"); got != "81,90" {
		x.t.Errorf("the readings after a second correct: %q, want 81,90", got)
	}
	x.ok("retract", map[string]string{"id": id(second)})
	if got := x.readings("Weight"); got != "81" {
		x.t.Errorf("the readings after retract: %q, want 81", got)
	}
}

// integrity-check is the owner's: an agent is refused (403).
func rowIntegrityCheck(x *parityRun) {
	if !x.owner {
		x.refused("integrity-check", nil, http.StatusForbidden, "forbidden")
		return
	}
	e := x.ok("integrity-check", nil)
	if p := props(x.t, e); !slices.Contains(e.Class, "integrity") || p["ok"] != true {
		x.t.Errorf("integrity-check: %v %v, want the class integrity and ok", e.Class, p["ok"])
	}
}

// snapshot is the owner's: the copy is a file beside the database, named by the answer. An agent is refused (403)
// by the API, and no file is written.
func rowSnapshot(x *parityRun) {
	const name = "life-" + parityToday + ".db"
	beside := filepath.Join(filepath.Dir(x.store.DB.Path()), name)
	if !x.owner {
		x.refused("snapshot", nil, http.StatusForbidden, "forbidden")
		if _, err := os.Stat(beside); err == nil {
			x.t.Errorf("a refused snapshot wrote %s", beside)
		}
		return
	}
	e := x.ok("snapshot", nil)
	p := props(x.t, e)
	if !slices.Contains(e.Class, "snapshot") || p["snapshot"] != beside {
		x.t.Errorf("snapshot: %v %v, want the class snapshot and %s", e.Class, p["snapshot"], beside)
	}
	if st, err := os.Stat(beside); err != nil || st.Size() == 0 {
		x.t.Errorf("the snapshot file: %v %v, want a file", st, err)
	}
	if rc, _ := p["restore_check"].(map[string]any); rc["ok"] != true {
		x.t.Errorf("the restore check: %v, want ok", rc)
	}
}
