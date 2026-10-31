package web_test

// The rail of every page that web draws: the sidebar of a desktop, the bottom bar and the More sheet of a phone, the
// search dialog and the access keys. The pages come from web over the real core, so the entries are the ones that the
// owner's browser gets. The view tests hold the rail drawn from a hand-made shell; these hold it from a real file.

import (
	"regexp"
	"strings"
	"testing"
)

var (
	// railAsideRE is the rail of a page: the aside, with the bottom bar and the More sheet inside it.
	railAsideRE = regexp.MustCompile(`(?s)<aside class="rail">.*?</aside>`)
	// railBarRE is the bottom bar of a phone: the sections of every day, Chat and More. Group 1 is its content.
	railBarRE = regexp.MustCompile(`(?s)<nav aria-label="Sections">(.*?)</nav>`)
	// railSheetRE is the sheet that More opens. Group 1 is its content.
	railSheetRE = regexp.MustCompile(`(?s)<div class="rail-more" id="sections" popover>(.*?)</aside>`)
	// railEntryRE is one section of the bar: group 1 is the address, group 2 is the mark of the current page, group 3 is
	// the words.
	railEntryRE = regexp.MustCompile(`<a href="([^"]+)"( aria-current="page")?[^>]*><span class="icon icon-[a-z]+" aria-hidden="true"></span>([^<]+)</a>`)
	// railLinkRE is one link of the bar or of the sheet, or the button that opens the sheet. The address of a link stops
	// before a fragment, so the links of the Today strip are not in the list.
	railLinkRE = regexp.MustCompile(`<a href="([^"#]+)"|<button type="button" (popovertarget="sections")`)
)

// railLinksOf are the addresses of the links of a part of the rail, in order, and the button that opens the sheet.
func railLinksOf(part string) string {
	var out []string
	for _, m := range railLinkRE.FindAllStringSubmatch(part, -1) {
		out = append(out, m[1]+m[2])
	}
	return strings.Join(out, " ")
}

// TestTheRailDrawsNoMonth: this month is on Calendar, so the rail and the sheet of a phone do not draw it a second
// time.
func TestTheRailDrawsNoMonth(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/days/"+today, "capture", map[string]string{"text": "Synthetic entry"})
	for _, path := range []string{"/days/" + today, "/browse", "/integrity"} {
		rail := railAsideRE.FindString(pageAt(t, l.web, path))
		if rail == "" {
			t.Fatalf("%s: no rail", path)
		}
		for _, bad := range []string{`class="month-box"`, `href="/months/`, `href="/days/2031-05-`} {
			if strings.Contains(rail, bad) {
				t.Errorf("%s: the rail draws the month (%s):\n%s", path, bad, rail)
			}
		}
	}
}

// TestTheRailMarksTodayOnTodaysDay: the day of today is Today, and any other day is in the Calendar.
func TestTheRailMarksTodayOnTodaysDay(t *testing.T) {
	l := emptyLife(t)
	if body := pageAt(t, l.web, "/days/"+today); !strings.Contains(body, `<a href="/days/today" aria-current="page" accesskey="t">`) {
		t.Errorf("the day of today does not mark Today:\n%s", railAsideRE.FindString(body))
	}
	if body := pageAt(t, l.web, "/days/2031-05-13"); !strings.Contains(body, `<a href="/days" aria-current="page">`) {
		t.Errorf("another day does not mark Calendar:\n%s", railAsideRE.FindString(body))
	}
}

// TestTheBottomBarAndTheMoreSheetOfAPhone: the sections of every day (Today, This week, Calendar, Metrics, Browse), Chat
// and More are one list, the bottom bar of a phone. The owner's links are the popover sheet that More opens and Close
// hides, with no script. Each link is drawn once, and More is marked when the page is in the sheet.
func TestTheBottomBarAndTheMoreSheetOfAPhone(t *testing.T) {
	l := emptyLife(t)
	body := pageAt(t, l.web, "/browse?kind=person")
	bar, sheet := railBarRE.FindStringSubmatch(body), railSheetRE.FindStringSubmatch(body)
	if bar == nil || sheet == nil {
		t.Fatalf("no bottom bar or no sheet:\n%s", railAsideRE.FindString(body))
	}
	if got, want := railLinksOf(bar[1]), `/days/today /weeks/today /days /metrics /browse /days/today/conversation popovertarget="sections"`; got != want {
		t.Errorf("the bottom bar: %s, want %s", got, want)
	}
	if !strings.Contains(bar[1], `<a href="/browse" aria-current="page">`) || strings.Contains(bar[1], `class="here"`) {
		t.Errorf("the bottom bar does not mark Browse, or marks More:\n%s", bar[1])
	}
	if got, want := railLinksOf(sheet[1]), "/actions /integrity /settings /actions?new=snapshot"; got != want || !strings.Contains(sheet[1], `<a href="/actions?new=snapshot">Snapshot</a>`) {
		t.Errorf("the sheet: %s, want %s", got, want)
	}
	if !strings.Contains(sheet[1], `<button type="button" class="button small" popovertarget="sections" popovertargetaction="hide">Close</button>`) {
		t.Error("the sheet has no Close")
	}
	if !strings.Contains(pageAt(t, l.web, "/integrity"), `<button type="button" popovertarget="sections" class="here">`) {
		t.Error("More is not marked on a page of the sheet")
	}
	if strings.Contains(pageAt(t, l.web, "/days/"+today), `popovertarget="sections" class="here">`) {
		t.Error("More is marked on Today")
	}
	if !strings.Contains(body, `content="width=device-width, initial-scale=1, viewport-fit=cover"`) {
		t.Error("the page does not reach the edges of the phone (viewport-fit)")
	}
}

// TestTheSearchDialogIsInThePageOnce: every page of the shell carries the search dialog once, a modal with the search
// form inside, which the Search button of the top bar opens by its command, and Escape or a tap outside closes
// (closedby). The button is drawn once too.
func TestTheSearchDialogIsInThePageOnce(t *testing.T) {
	l := emptyLife(t)
	body := pageAt(t, l.web, "/days/"+today)
	for _, want := range []string{
		`<dialog class="palette" id="palette" closedby="any" aria-label="Search">` + "\n" + `<search><form method="GET" action="/search"><input type="search" name="q" autocomplete="off" aria-label="Search" placeholder="A page's title, or words of a body"></form></search>`,
		`<button type="button" class="rail-search-link button" commandfor="palette" command="show-modal" aria-label="Search">`,
	} {
		if n := strings.Count(body, want); n != 1 {
			t.Errorf("the day carries %s %d times, want once", want, n)
		}
	}
}

// TestTheAccessKeysAreInThePage: the keys are the access keys of the browser, on the search, New, Today, the capture and
// the previous and next day, and the footer says so. There is no script to take any other key.
func TestTheAccessKeysAreInThePage(t *testing.T) {
	l := emptyLife(t)
	day := pageAt(t, l.web, "/days/"+today)
	for _, want := range []string{
		`<button type="button" class="rail-search button" commandfor="palette" command="show-modal" accesskey="k">`,
		`<button type="button" class="button primary rail-new" popovertarget="new-menu" accesskey="n">`,
		`<a href="/days/today" aria-current="page" accesskey="t">`,
		`placeholder="What happened? Who was there?" accesskey="c">`,
		`<a class="button step" accesskey="[" href="/days/2031-05-13" rel="prev"`, `<a class="button step" accesskey="]" href="/days/2031-05-15" rel="next"`,
		`<p class="keys"><small>Access keys, with the browser's modifier: <kbd>k</kbd> search · <kbd>n</kbd> new · <kbd>c</kbd> capture · <kbd>t</kbd> today · <kbd>[</kbd> <kbd>]</kbd> previous, next</small></p>`,
	} {
		if !strings.Contains(day, want) {
			t.Errorf("the day lacks %s", want)
		}
	}
}

// TestTheSectionsAreTheSameOnEveryPageAndTheRightOneIsMarked: the sections of every day are Today, This week, Calendar,
// Metrics, Browse and Chat, in the rail of a desktop and in the bottom bar of a phone (one list, which the sheet does
// not repeat). There is no group of the record. Browse is marked on every list of Browse, Metrics on the board and
// under it.
func TestTheSectionsAreTheSameOnEveryPageAndTheRightOneIsMarked(t *testing.T) {
	l := emptyLife(t)
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	for _, c := range []struct{ path, current string }{
		{"/browse", "Browse"}, {"/browse?kind=person", "Browse"}, {"/browse?ghosts=1", "Browse"}, {"/metrics", "Metrics"},
		{"/metrics/Weight", "Metrics"}, {"/days/" + today, "Today"}, {"/months/2031-05", "Calendar"}, {"/search", ""},
	} {
		body := pageAt(t, l.web, c.path)
		bar := railBarRE.FindStringSubmatch(body)
		if bar == nil {
			t.Errorf("%s: no sections:\n%s", c.path, railAsideRE.FindString(body))
			continue
		}
		var got, marked []string
		for _, m := range railEntryRE.FindAllStringSubmatch(bar[1], -1) {
			got = append(got, m[3]+"="+m[1])
			if m[2] != "" {
				marked = append(marked, m[3])
			}
		}
		if want := "Today=/days/today This week=/weeks/today Calendar=/days Metrics=/metrics Browse=/browse Chat=/days/today/conversation"; strings.Join(got, " ") != want {
			t.Errorf("%s: the sections are %v, want %s", c.path, got, want)
		}
		if strings.Join(marked, " ") != c.current {
			t.Errorf("%s marks %v, want %q", c.path, marked, c.current)
		}
		if strings.Contains(body, `aria-label="The record"`) || strings.Count(railAsideRE.FindString(body), `href="/browse"`) != 1 {
			t.Errorf("%s: the rail has a group of the record, or a second link to Browse", c.path)
		}
	}
}

// TestTheRailHasChatAndSettings: Chat is a section of the rail and goes to the conversation of today. Settings is
// beside Snapshot in the line of the owner. Each is marked on its own page.
func TestTheRailHasChatAndSettings(t *testing.T) {
	l := emptyLife(t)
	body := pageAt(t, l.web, "/browse")
	for _, want := range []string{`<a href="/days/today/conversation"><span class="icon icon-chat" aria-hidden="true"></span>Chat</a>`,
		` · <a href="/settings">Settings</a> · <a href="/actions?new=snapshot">Snapshot</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the rail lacks %s", want)
		}
	}
	if !strings.Contains(pageAt(t, l.web, "/days/today/conversation"), `<a href="/days/today/conversation" aria-current="page">`) {
		t.Error("Chat is not marked on the conversation")
	}
	if !strings.Contains(pageAt(t, l.web, "/days/"+today+"/conversation"), `<a href="/days/today/conversation" aria-current="page">`) {
		t.Error("Chat is not marked on the conversation of a day by its date")
	}
	if !strings.Contains(pageAt(t, l.web, "/settings"), `<a href="/settings" aria-current="page">Settings</a>`) {
		t.Error("Settings is not marked on /settings")
	}
}
