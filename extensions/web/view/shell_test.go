package view

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog-web/pixelart"
	"lifelog/siren"
)

// TestNavMarksThePageTheBrowserIsOn says one navigation link is current: the one whose path is the page's, else the
// one with the longest path the page is under.
func TestNavMarksThePageTheBrowserIsOn(t *testing.T) {
	for path, want := range map[string]string{
		"/":                             "/",
		"/days":                         "/days",
		"/days/2026-01-01":              "/days",
		"/days/today":                   "/days/today",
		"/months/2026-10":               "/days",
		"/years/2026":                   "/days",
		"/integrity":                    "/integrity",
		"/browse":                       "/browse",
		"/metrics":                      "/metrics",
		"/metrics/TSH":                  "/metrics",
		"/settings":                     "/settings",
		"/days/2026-10-28/conversation": "/days/today/conversation",
		"/days/today/conversation":      "/days/today/conversation",
		"/pages/7":                      "",
		"/search":                       "",
		"/measurements/1":               "",
	} {
		var got string
		for _, it := range (&view{Path: path}).Nav() {
			if it.Current {
				if got != "" {
					t.Errorf("%s marks both %s and %s current", path, got, it.Href)
				}
				got = it.Href
			}
		}
		if got != want {
			t.Errorf("%s marks %q current, want %q", path, got, want)
		}
	}
}

// TestMoreIsMarkedOnAPageOfTheOwner: More is marked on a page of the owner's links at its foot, and on no other page.
// Today's day page marks Today, by the today of the view's words.
func TestMoreIsMarkedOnAPageOfTheOwner(t *testing.T) {
	shell := &Shell{Today: "2026-10-16", Zone: time.UTC}
	for path, want := range map[string]bool{
		"/integrity": true, "/actions": true, "/": false, "/days/2026-10-16": false, "/metrics": false, "/pages/7": false,
	} {
		if got := (&view{Path: path, Shell: shell}).InMore(); got != want {
			t.Errorf("InMore on %s = %v, want %v", path, got, want)
		}
	}
	for _, c := range []struct {
		shell *Shell
		want  string
	}{{shell, "/days/today"}, {nil, "/days"}, {&Shell{Today: "2026-10-15"}, "/days"}} {
		var got string
		for _, it := range (&view{Path: "/days/2026-10-16", Shell: c.shell}).Nav() {
			if it.Current {
				got = it.Href
			}
		}
		if got != c.want {
			t.Errorf("the shell %+v marks %q on /days/2026-10-16, want %q", c.shell, got, c.want)
		}
	}
}

// drawRail draws a page of a shell and returns its rail.
func drawRail(t *testing.T, shell *Shell) string {
	t.Helper()
	u, _ := url.Parse("/anything")
	b, err := Draw(&siren.Entity{Class: []string{"unknown"}, Title: "A page"}, Facts{URL: u, Shell: shell})
	if err != nil {
		t.Fatal(err)
	}
	return regexp.MustCompile(`(?s)<aside class="rail">.*?</aside>`).FindString(string(b))
}

func ptrBool(b bool) *bool { return &b }

// TestTheRailDrawsTheTodayStrip: two things done and two open: the line counts the done ones, every chip says its
// state, and an open chip leads to the form that does the thing (entry and mood: the capture of today; a habit: the
// reading form of Home, with the habit and the value 1 in the query).
func TestTheRailDrawsTheTodayStrip(t *testing.T) {
	rail := drawRail(t, &Shell{Today: "2026-10-19", Zone: time.UTC, Strip: &Strip{
		Written: true, Mood: false, Streak: 12,
		Habits: []Habit{{"Walk", nil}, {"No phone", ptrBool(true)}, {"Run & jump", nil}, {"Stretch", ptrBool(false)}},
	}})
	if rail == "" {
		t.Fatal("no rail")
	}
	strip := regexp.MustCompile(`(?s)<section class="rail-today".*?</section>`).FindString(rail)
	if strip == "" {
		t.Fatalf("no strip in the rail:\n%s", rail)
	}
	if !strings.Contains(strip, "2 of 6 done") { // entry and one habit done; the entry, the mood and four habits are counted
		t.Errorf("the line does not say 2 of 6 done:\n%s", strip)
	}
	for _, want := range []string{
		`<a class="chip done" href="/days/today#capture">✓ entry</a>`,
		`<a class="chip" href="/days/today#capture">mood</a>`,
		`<a class="chip" href="/?back=%2Fanything&amp;metric=Walk&amp;new=record&amp;value=1#record">Walk</a>`,
		`<span class="chip done">✓ No phone</span>`,
		`<a class="chip" href="/?back=%2Fanything&amp;metric=Run&#43;%26&#43;jump&amp;new=record&amp;value=1#record">Run &amp; jump</a>`,
		`<span class="chip">Stretch · no</span>`,
		`12 days in a row`,
	} {
		if !strings.Contains(strip, want) {
			t.Errorf("the strip lacks %s:\n%s", want, strip)
		}
	}
	// the strip is inside the sheet of More, before the owner's links, so a phone keeps it out of the bottom bar
	more := rail[strings.Index(rail, `<div class="rail-more"`):]
	if i, j := strings.Index(more, `class="rail-today"`), strings.Index(more, `class="rail-foot"`); i < 0 || j < i {
		t.Errorf("the strip is not in More before the owner's links:\n%s", more)
	}
}

// TestTheStripSaysTwoOfTwoAndHidesAShortStreak: the count is over the entry, the mood and the habits; a streak of 0
// or 1 draws no line.
func TestTheStripSaysTwoOfTwoAndHidesAShortStreak(t *testing.T) {
	for _, streak := range []int{0, 1} {
		rail := drawRail(t, &Shell{Today: "2026-10-19", Zone: time.UTC, Strip: &Strip{Written: true, Mood: true, Streak: streak}})
		if !strings.Contains(rail, "2 of 2 done") || strings.Contains(rail, "in a row") {
			t.Errorf("streak %d:\n%s", streak, rail)
		}
	}
}

// TestNoStripWithoutState: a shell with no state draws the rail with no strip, and a page with no shell no rail.
func TestNoStripWithoutState(t *testing.T) {
	rail := drawRail(t, &Shell{Today: "2026-10-19", Zone: time.UTC})
	if rail == "" || strings.Contains(rail, "rail-today") {
		t.Errorf("a shell with no state:\n%s", rail)
	}
	if rail := drawRail(t, nil); rail != "" {
		t.Errorf("a page with no shell has a rail:\n%s", rail)
	}
}

// TestEveryIconNameIsACellOfTheSheet: the names that templates, the rail and the search hits pass to icon are cells of
// the icon sheet, and a name that is not a cell draws nothing. A template that passes a name from its data (the file
// kind of a tile) is checked by its values: the kinds of media.
func TestEveryIconNameIsACellOfTheSheet(t *testing.T) {
	names := map[string]string{} // name to where it is used
	literal := regexp.MustCompile(`\{\{icon "([a-z]+)"\}\}`)
	entries, err := Files.ReadDir("html")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		for _, m := range literal.FindAllStringSubmatch(string(MustFile(e.Name())), -1) {
			names[m[1]] = e.Name()
		}
	}
	for _, it := range navItems {
		if it.Icon != "" {
			names[it.Icon] = "navItems"
		}
	}
	for _, k := range newKinds {
		names[k.Icon] = "newKinds"
	}
	for kind, name := range kindIcons {
		names[name] = "kindIcons " + kind
	}
	for _, name := range []string{"image", "video", "audio"} { // the file_kind of a media tile
		names[name] = "the kinds of media"
	}
	if len(names) < 10 {
		t.Fatalf("found %d icon names, which is too few: the search for them is broken", len(names))
	}
	for name, where := range names {
		if _, ok := pixelart.IconCell(name); !ok {
			t.Errorf("%s uses the icon %q, which is not a cell of the icon sheet", where, name)
		}
		want := `<span class="icon icon-` + name + `" aria-hidden="true"></span>`
		if got := string(icon(name)); got != want {
			t.Errorf("icon(%q) is %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"", "nope", `x" onload="y`} {
		if got := icon(name); got != "" {
			t.Errorf("icon(%q) is %q, want nothing", name, got)
		}
	}
}

// TestTheColumnIsTheShellsAndTheConversationHasNone: a page with a shell and a chat has the column; the conversation
// view (class conversation) has none, and a page with no shell has none. The turns of the conversation view are read
// from its property turns, and the agent has not answered when the last turn is the owner's.
func TestTheColumnIsTheShellsAndTheConversationHasNone(t *testing.T) {
	chat := &Chat{Version: "v1"}
	shell := &Shell{Today: "2026-10-28", Chat: chat}
	if got := (&view{Shell: shell, Entity: &siren.Entity{}}).Chat(); got != chat {
		t.Errorf("a page with a shell has the chat %v, want the shell's", got)
	}
	if got := (&view{Shell: shell, Entity: &siren.Entity{Class: []string{"conversation", "page"}}}).Chat(); got != nil {
		t.Errorf("the conversation view has the chat %v, want none", got)
	}
	if got := (&view{}).Chat(); got != nil {
		t.Errorf("a page with no shell has the chat %v, want none", got)
	}
	v := &view{Props: map[string]any{"turns": []any{
		map[string]any{"who": "owner", "at": "09.07", "via": "whatsapp", "text": "Hello"},
		map[string]any{"who": "lifelog", "at": "09.08", "text": "Hi", "tools": []any{"- `read_day`"}},
		map[string]any{"who": "owner", "at": "09.09", "text": "Thanks"},
	}}}
	turns := v.Turns()
	if len(turns) != 3 || turns[1].Who != "lifelog" || turns[1].At != "09.08" || turns[1].Text != "Hi" || len(turns[1].Tools) != 1 || siren.ToolLine(turns[1].Tools[0]) != (siren.Tool{Name: "read_day"}) {
		t.Errorf("the turns: %+v", turns)
	}
	if turns[0].Via != "whatsapp" || turns[1].Via != "" {
		t.Errorf("the relay of the turns: %+v", turns)
	}
	if !waiting(turns) || waiting(turns[:2]) || waiting(nil) {
		t.Error("the agent waits only when the last turn is the owner's")
	}
}

// TestTheDayBarMarksTheDayShown: the chips are the days before today that have a conversation, oldest first, and
// Today last; a day that is not among them (a link from the day view) is a chip too; the day shown is marked.
func TestTheDayBarMarksTheDayShown(t *testing.T) {
	shell := &Shell{Today: "2026-10-28"}
	chips := func(shown string) string {
		v := &view{Shell: shell, Props: map[string]any{"day": shown}, chatDays: []string{"2026-10-25", "2026-10-27"}}
		var out []string
		for _, c := range v.ChatDays() {
			mark := ""
			if c.Current {
				mark = "*"
			}
			out = append(out, c.Text+mark+" "+c.Href)
		}
		return strings.Join(out, ", ")
	}
	if got, want := chips("2026-10-28"), "Sun 25 /days/2026-10-25/conversation, Tue 27 /days/2026-10-27/conversation, Today* /days/today/conversation"; got != want {
		t.Errorf("today: %s, want %s", got, want)
	}
	if got, want := chips("2026-10-27"), "Sun 25 /days/2026-10-25/conversation, Tue 27* /days/2026-10-27/conversation, Today /days/today/conversation"; got != want {
		t.Errorf("a day of the list: %s, want %s", got, want)
	}
	if got, want := chips("2026-10-01"), "Thu 1* /days/2026-10-01/conversation, Sun 25 /days/2026-10-25/conversation, Tue 27 /days/2026-10-27/conversation, Today /days/today/conversation"; got != want {
		t.Errorf("another day: %s, want %s", got, want)
	}
}

// TestACardIsDrawnOnlyForAnAddressTheServerResolved: an address that is not in the facts (an older turn) and one that
// is there with no entity are the plain link, cut at 80 characters with its href whole; the entity picks the card by
// its class.
func TestACardIsDrawnOnlyForAnAddressTheServerResolved(t *testing.T) {
	v := &view{Shell: &Shell{Today: "2026-10-28"}, cards: map[string]*siren.Entity{
		"/gone":    nil,
		"/pages/3": {Class: []string{"person", "page"}, Title: "Sam", Properties: map[string]any{"body": "# Sam\n\nMy **friend** from [[Lisbon]].", "cover": map[string]any{"id": 9}}},
		"/odd":     {Class: []string{"settings"}, Title: "Settings"},
	}}
	long := "/search?q=" + strings.Repeat("a", 100)
	for _, href := range []string{"/unasked", "/gone"} {
		if c := v.CardOf(href); c.Kind != "link" || c.Title != href || c.Href != href {
			t.Errorf("the plain link of %s: %+v", href, c)
		}
	}
	if c := v.CardOf(long); c.Kind != "link" || c.Href != long || c.Title != long[:80]+"…" {
		t.Errorf("a long plain link: %+v", c)
	}
	if c := v.CardOf("/pages/3"); c.Kind != "person" || c.Title != "Sam" || c.Icon != "people" || c.Picture != "/pages/9/preview" || c.Value != "" {
		t.Errorf("the person: %+v", c)
	}
	if c := v.CardOf("/odd"); c.Kind != "other" || c.Title != "Settings" || c.Value != "settings" {
		t.Errorf("a class without a card: %+v", c)
	}
}

// TestTheDateAboveAThreadIsInFullWords: the date above a thread names the weekday, and the year when it is not this year.
func TestTheDateAboveAThreadIsInFullWords(t *testing.T) {
	v := &view{Shell: &Shell{Today: "2026-10-28"}}
	if got := v.LongDate("2026-10-28"); got != "Wednesday 28 October" {
		t.Errorf("LongDate: %q", got)
	}
	if got := v.LongDate("2025-01-05"); got != "Sunday 5 January 2025" {
		t.Errorf("LongDate of another year: %q", got)
	}
}
