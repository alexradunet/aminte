package web_test

// The chat of the shell as web draws it over the real core: the column on every page, the chat screen that is the
// conversation view, and the cards of the things that the agent touched. The file is made through the API, as an owner
// and an agent make it: the owner says, and the agent answers with reply. A test that needs a turn with a stamp of its own
// writes it into the page as the owner's edit (chatAppend), because the core stamps a reply with its own clock.

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/siren"
)

// chatScriptTag is the one script element that a page of the shell carries.
const chatScriptTag = `<script src="/assets/chat.js" defer></script>`

// chatColumnRE is the chat column of a page: the popover, up to the end of its form and its hint.
var chatColumnRE = regexp.MustCompile(`(?s)<div class="chat compact" id="chat".*?</form>\s*<p class="hint">.*?</p>\s*</div>`)

// chatCardRE is a card of a thread: group 1 is its kind, group 2 is its address, group 3 is the whole card.
var chatCardRE = regexp.MustCompile(`(?s)<a class="card card-([a-z]+)" href="([^"]*)">(.*?)</a>`)

// chatCardsOf are the cards of a page, in the order that they are drawn.
func chatCardsOf(page string) [][]string { return chatCardRE.FindAllStringSubmatch(page, -1) }

// cardKinds are the kinds of the cards of a page, in the order they are drawn, with the address of each: "day /days/2031-05-01".
func cardKinds(page string) []string {
	var out []string
	for _, card := range chatCardsOf(page) {
		out = append(out, card[1]+" "+card[2])
	}
	return out
}

// chatFormOf is the first chat form of page, up to its end. It is empty when the page has none.
func chatFormOf(page string) string {
	i := strings.Index(page, `class="chat-form" data-action="say">`)
	if i < 0 {
		return ""
	}
	return page[i : i+strings.Index(page[i:], "</form>")]
}

// chatScreenOf is the start of the chat screen of a page, for a failure message.
func chatScreenOf(page string) string {
	i := strings.Index(page, `<div class="chat-screen"`)
	if i < 0 {
		return page[:min(len(page), 300)]
	}
	return page[i:min(len(page), i+6000)]
}

// chatPageOf is the page that holds the conversation of today, and its body.
func chatPageOf(t *testing.T, l *life) (*siren.Entity, string) {
	t.Helper()
	conv := must(t)(l.api.Get(t.Context(), "/days/"+today+"/conversation"))
	page := must(t)(l.api.Get(t.Context(), href(conv, "page")))
	body, _ := page.Properties.(map[string]any)["body"].(string)
	return page, body
}

// chatAppend writes more at the end of the page of the conversation of today, as the owner does with save-body. The core
// refuses save-body on a conversation page to an agent: the agent writes its turn with reply.
func chatAppend(t *testing.T, l *life, more string) {
	t.Helper()
	page, body := chatPageOf(t, l)
	must(t)(l.api.Do(t.Context(), act(t, page, "save-body"), map[string]string{"body": body + more}))
}

// chatVersionOf is the version of the conversation of today, as the core says it.
func chatVersionOf(t *testing.T, l *life) string {
	t.Helper()
	conv := must(t)(l.api.Get(t.Context(), "/days/today/conversation"))
	version, _ := conv.Properties.(map[string]any)["version"].(string)
	return version
}

// TestTheConversationViewIsTheChatScreen: the conversation is a screen and not a page. The day bar names the day with
// "today", has the chips of the last three days that have a conversation page and Today (the current one marked), and a
// menu with the own things of the page. There is no page head, no margin and no row of categories outside the menu. The
// thread has the turns in order, the owner's turns and the agent's turns told apart by class, the relay of a turn, the
// tool lines as chips, markdown and wikilinks in the words, and a free paragraph as it is. The composer is the form of
// say and returns to the screen. Three dots wait while the last turn is the owner's. The column is not drawn.
func TestTheConversationViewIsTheChatScreen(t *testing.T) {
	l := emptyLife(t)
	l.earlierConversations(t, "2031-05-01", "2031-05-10", "2031-05-12", "2031-05-13") // the oldest is not one of the last three
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "How did I **sleep**? See [[Sleep notes]]."})
	chatAppend(t, l, "\n\n- `search_pages` words=sleep\n- `read_day` failed: not_found\n\n**lifelog** (09:30): You slept *well*.\n")
	l.do(t, "/days/today/conversation", "say", map[string]string{"text": "And last night?", "via": "whatsapp"})
	page, body := chatPageOf(t, l)
	body = strings.Replace(body, "**Owner**", "A note the owner typed.\n\n**Owner**", 1) // text before the first marker is a text turn
	must(t)(l.api.Do(t.Context(), act(t, page, "save-body"), map[string]string{"body": body}))
	l.do(t, "/", "create-page", map[string]string{"title": "Sleep log", "body": "I talked it over in [[Conversation " + today + "]].\n"}) // a page that links the conversation

	view := pageAt(t, l.web, "/days/"+today+"/conversation")
	order := []string{`<h1>Chat</h1><span class="day">Wed 14 May · today</span>`,
		`<nav class="days" aria-label="Days"><a href="/days/2031-05-10/conversation">Sat 10</a><a href="/days/2031-05-12/conversation">Mon 12</a><a href="/days/2031-05-13/conversation">Tue 13</a><a href="/days/today/conversation" aria-current="page">Today</a></nav>`,
		`<button type="button" class="more" popovertarget="chat-menu"`, `<div class="more-body" id="chat-menu" popover>`, `popovertargetaction="hide">Close</button>`,
		`the day view</a>`, `aria-label="Categories"`, `<summary>Edit</summary>`, `aria-label="Links"`, `aria-label="Backlinks"`, `class="danger">Delete`, `<section class="thread"`,
		`<li class="turn text">`, "A note the owner typed.",
		`<li class="turn owner">`, `<span class="from">You</span><time datetime="`, `How did I <strong>sleep</strong>?`, `<a href="/pages?title=Sleep+notes" class="wikilink">Sleep notes</a>`,
		`<li class="turn agent">`, `<span class="from">lifelog</span>`, `<li><b>search_pages</b> words=sleep</li>`, `<li><b>read_day</b> failed: not_found</li>`, `You slept <em>well</em>.`,
		`<li class="turn owner">`, `<span class="from">You</span><span class="via">· via whatsapp</span><time datetime="`, "And last night?",
		`<div class="thinking" role="status"`, `<footer class="composer">`}
	at := 0
	for _, want := range order {
		i := strings.Index(view[at:], want)
		if i < 0 {
			t.Fatalf("the chat screen lacks %q after byte %d:\n%s", want, at, chatScreenOf(view))
		}
		at += i
	}
	for _, gone := range []string{`class="page-head"`, `page-margin`, `aria-label="Facts"`, `id="chat"`, `<details class="more-menu"`, `class="page-body"`, `class="waiting"`} {
		if strings.Contains(view, gone) {
			t.Errorf("the chat screen draws %s", gone)
		}
	}
	if i, menu := strings.Index(view, `aria-label="Categories"`), strings.Index(view, `class="more-body"`); i < menu || menu < 0 {
		t.Errorf("the categories are at %d, before the menu at %d, or the menu is missing: a row of categories outside the menu", i, menu)
	}
	version := chatVersionOf(t, l)
	if version == "" || strings.Count(view, "<script") != 1 || !strings.Contains(view, chatScriptTag) {
		t.Errorf("the chat screen lacks its one script, or the day has no version %q", version)
	}
	if want := `<div class="chat-screen" id="conversation" data-day="` + today + `" data-version="` + version + `">`; !strings.Contains(view, want) {
		t.Errorf("the chat screen lacks %s", want)
	}
	// The composer is the plain chat form: no Preview in it (the form of the body in the menu keeps its own), and it
	// returns to the screen.
	composer := `<form method="POST" action="/days/` + today + `/say" class="chat-form" data-action="say">`
	if i := strings.Index(view, composer); i < 0 {
		t.Errorf("the chat screen lacks the composer %s", composer)
	} else if form := view[i:]; strings.Contains(form[:strings.Index(form, "</form>")], "Preview") ||
		!strings.Contains(form[:strings.Index(form, "</form>")], `<input type="hidden" name="back" value="/days/`+today+`/conversation">`) {
		t.Error("the composer offers Preview, or does not return to the screen")
	}
	if !regexp.MustCompile(`<time datetime="\d\d:\d\d">\d\d:\d\d</time>`).MatchString(view) {
		t.Error("the time of a turn has no datetime attribute in the form of HTML")
	}
	if !strings.Contains(chatFormOf(view), ` enterkeyhint="send"`) {
		t.Error("the text area of the composer does not say that Enter sends")
	}
	if !strings.Contains(view, `<kbd>Enter</kbd> sends · <kbd>Shift</kbd>+<kbd>Enter</kbd> a new line`) {
		t.Error("the composer lacks its keyboard rule")
	}
	// The agent has answered last: the three dots are gone.
	chatAppend(t, l, "\n\n**lifelog** (09:40): Last night you slept 7 hours.\n")
	if strings.Contains(pageAt(t, l.web, "/days/"+today+"/conversation"), `class="thinking"`) {
		t.Error("the screen waits for an agent that has answered")
	}
}

// TestADayWithNoTurnsSaysWhereTheAgentAnswers: the empty state names the way forward.
func TestADayWithNoTurnsSaysWhereTheAgentAnswers(t *testing.T) {
	l := emptyLife(t)
	view := pageAt(t, l.web, "/days/"+today+"/conversation")
	for _, want := range []string{`class="empty"`, "It answers here when it runs", "<code>lifelog serve --with agent</code>", `<textarea name="text"`} {
		if !strings.Contains(view, want) {
			t.Errorf("the empty conversation lacks %q", want)
		}
	}
	// The transport of WhatsApp answers nothing: the agent is an extension of its own (plan 162).
	if strings.Contains(view, "lifelog whatsapp") {
		t.Error("the empty conversation tells the owner to start the transport of WhatsApp, which answers nothing")
	}
	if strings.Contains(view, `class="turn `) || strings.Contains(view, `aria-label="Facts"`) || strings.Contains(view, "<summary>Edit</summary>") {
		t.Error("a day with no page draws a turn, the margin of a page or the editor of one")
	}
}

// TestADayThatOffersNoSayIsToldNothingWasSaid: say is the action of today alone, so a past day has no composer and no
// invitation.
func TestADayThatOffersNoSayIsToldNothingWasSaid(t *testing.T) {
	l := emptyLife(t)
	view := pageAt(t, l.web, "/days/2020-01-01/conversation")
	if !strings.Contains(view, "Nothing was said on this day.") || strings.Contains(view, "Write to the agent") || strings.Contains(view, `name="text"`) {
		t.Errorf("the conversation of a past day invites a write, or lacks its empty state:\n%s", view)
	}
	if !strings.Contains(view, `id="conversation" data-day="2020-01-01" data-version=""`) {
		t.Error("the view of a day with no page lacks its empty data-version")
	}
}

// TestEveryPageOfTheShellDrawsTheChatColumn: the column holds the turns of today, the three dots when the last turn is
// the owner's, a form of say to today whose back is the page, and the version of the conversation page. The script is
// loaded once, with defer. The composer posts, and the page comes back to where it was.
func TestEveryPageOfTheShellDrawsTheChatColumn(t *testing.T) {
	l := emptyLife(t)
	day := "/days/" + today
	empty := pageAt(t, l.web, day)
	for _, want := range []string{`<div class="chat compact" id="chat" popover role="complementary" data-day="today" data-version="" aria-label="Chat">`, "Nothing is said today yet",
		`<form method="POST" action="/days/today/say" class="chat-form" data-action="say">`, `<input type="hidden" name="back" value="` + day + `">`,
		`popovertarget="chat"`, chatScriptTag} {
		if !strings.Contains(empty, want) {
			t.Errorf("the page of a day with no talk lacks %s", want)
		}
	}
	if !strings.Contains(chatColumnRE.FindString(empty), `<textarea name="text"`) || !strings.Contains(chatFormOf(chatColumnRE.FindString(empty)), ` enterkeyhint="send"`) {
		t.Error("the text area of the column does not say that Enter sends")
	}
	if strings.Count(empty, `<script`) != 1 || strings.Contains(empty, `class="thinking"`) || strings.Contains(chatColumnRE.FindString(empty), "Preview") {
		t.Error("the column draws more than one script, the three dots with no turn, or a Preview button")
	}

	rec := postForm(l.web, "/days/today/say", url.Values{"text": {"Anyone there?"}, "back": {day}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), day+"?feedback=") {
		t.Fatalf("POST /days/today/say with back: %d to %q, want 303 to the page with a receipt", rec.Code, rec.Header().Get("Location"))
	}
	column := chatColumnRE.FindString(pageAt(t, l.web, day))
	for _, want := range []string{`data-version="` + chatVersionOf(t, l) + `"`, `<li class="turn owner">`, "Anyone there?", `<div class="thinking" role="status"`} {
		if !strings.Contains(column, want) {
			t.Errorf("the column after a turn lacks %s:\n%s", want, column)
		}
	}
	if !strings.Contains(pageAt(t, l.web, "/browse"), `<input type="hidden" name="back" value="/browse">`) {
		t.Error("the form of the column on /browse does not return to /browse")
	}
	if strings.Contains(pageAt(t, l.web, "/days/today/conversation"), `id="chat"`) {
		t.Error("the conversation view draws the column")
	}
}

// chatCardsLife is an empty file with the things that an agent touches, each made through the API: a day with a mood, a
// page, a person, a metric with two readings, and a word to search. The addresses are in the order of the tool lines that
// name them. Today is 2031-05-14, so the reading of the day before is "yesterday". The word heron is in the entry of
// today, in the page River walk and in the conversation itself, which says it in a tool line: three hits.
func chatCardsLife(t *testing.T) (*life, []string) {
	t.Helper()
	l := emptyLife(t)
	l.do(t, "/", "capture", map[string]string{"text": "Saw a heron at the river", "mood": "4"})
	river := l.do(t, "/", "create-page", map[string]string{"title": "River walk", "body": "A long path along the water.\nA heron lives there."})
	sam := l.do(t, "/", "create-person", map[string]string{"title": "Sam", "name": "Samuel Ortiz"})
	l.do(t, "/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": "2031-05-12", "value": "70.4"})
	l.do(t, "/", "record", map[string]string{"metric": "Weight", "day": "2031-05-13", "value": "71.2"})
	return l, []string{"/days/" + today, href(river, "self"), href(sam, "self"), "/metrics/Weight", "/search?q=heron"}
}

// TestTheConversationDrawsACardForEachThingTheAgentTouched: the tool lines of an agent's turn name a day, a page, a
// person, a metric and a search, each made through the API first. The turn draws one card for each, in that order. Each
// card is a link to the thing, with the icon, the title and the value of its class, on the badge of its tool line above
// the words. An address that answers 404 draws the plain link, and so does the seventh address of a turn. The word
// today alone names the day of today.
func TestTheConversationDrawsACardForEachThingTheAgentTouched(t *testing.T) {
	l, refs := chatCardsLife(t)
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "What did I touch?"})
	seven := []string{"/pages/999999", "/days/2031-05-02", "/days/2031-05-03", "/days/2031-05-04", "/days/2031-05-05", "/days/2031-05-06", "/days/2031-05-07"}
	chatAppend(t, l, "\n\n"+toolLines(refs...)+"\n**lifelog** (09:30): Here they are.\n\n"+toolLines(seven...)+"\n**lifelog** (09:31): And a week.\n\n"+toolLines("/days/today")+"\n**lifelog** (09:32): And today, by the word.\n")

	page := pageAt(t, l.web, "/days/"+today+"/conversation")
	cards := chatCardsOf(page)
	if len(cards) != 13 {
		t.Fatalf("the page draws %d cards, want 13 (five, a 404 and six days, the seventh as a plain link, and today)", len(cards))
	}
	var kinds, hrefs []string
	for _, card := range cards[:5] {
		kinds, hrefs = append(kinds, card[1]), append(hrefs, card[2])
	}
	if got, want := strings.Join(kinds, " "), "day page person series search"; got != want {
		t.Fatalf("the cards of the first turn are %s, want %s", got, want)
	}
	if got, want := strings.Join(hrefs, " "), strings.Join(refs, " "); got != want {
		t.Errorf("the cards link to %s, want %s", got, want)
	}
	for i, want := range [][]string{
		{`<span class="icon icon-calendar"`, `<b class="card-title">Wed 14 May</b>`, `<span class="card-value">mood 4</span>`},
		{`<span class="icon icon-files"`, `<b class="card-title">River walk</b>`},
		{`<span class="icon icon-people"`, `<b class="card-title">Sam</b>`},
		{`<span class="icon icon-metrics"`, `<b class="card-title">Weight</b>`, `<span class="card-value">71.2 kg · yesterday</span>`},
		{`<span class="icon icon-search"`, `<b class="card-title">Search: heron</b>`, `<span class="card-value">3 hits</span>`}, // the entry of today, the page and the conversation itself, which says heron in its tool line
	} {
		for _, part := range want {
			if !strings.Contains(cards[i][3], part) {
				t.Errorf("the %s card lacks %q:\n%s", cards[i][1], part, cards[i][3])
			}
		}
	}
	// The second turn names a 404 and six days: the plain link, five day cards, and the seventh address is the plain link
	// too, since the server resolves six a turn.
	if cards[5][1] != "link" || cards[5][2] != "/pages/999999" || cards[5][3] != "/pages/999999" {
		t.Errorf("the card of a 404 is %q, want the plain link", cards[5][0])
	}
	for _, card := range cards[6:11] {
		if card[1] != "day" {
			t.Errorf("the card of %s is %s, want a day", card[2], card[1])
		}
	}
	if cards[11][1] != "link" || cards[11][2] != seven[6] || cards[11][3] != seven[6] {
		t.Errorf("the seventh address of a turn is %v, want the plain link", cards[11][:3])
	}
	// The third turn names today by the word: the redirect is followed, and the card is the card of the day.
	if cards[12][1] != "day" || cards[12][2] != "/days/today" || !strings.Contains(cards[12][3], `<b class="card-title">Wed 14 May</b>`) {
		t.Errorf("the card of /days/today is %v, want the card of the day of the 14th", cards[12][:3])
	}
	// The badges stand above the words of the turn, on the badge of the tool line that named the thing.
	turn := page[strings.Index(page, `<li class="turn agent">`):]
	turn = turn[:strings.Index(turn, `<div class="words prose">`)] // the turn up to its words
	if !strings.Contains(turn, `<ul class="tools">`) || !strings.Contains(turn, "<li><b>tool1</b><a class=\"card card-day\"") {
		t.Errorf("the badges, each with its card, are not above the words of the turn:\n%s", turn)
	}
}

// TestTheColumnDrawsTheCards: the column of any page draws the newest cards, with the same markup as the screen: one card
// for each kind of thing that the tool lines name, and the plain link for an address that answers 404.
func TestTheColumnDrawsTheCards(t *testing.T) {
	l, refs := chatCardsLife(t)
	l.do(t, "/days/"+today+"/conversation", "say", map[string]string{"text": "Show me"})
	chatAppend(t, l, "\n\n"+toolLines(append(slices.Clone(refs), "/pages/999999")...)+"\n**lifelog** (09:30): Here.\n")
	column := chatColumnRE.FindString(pageAt(t, l.web, "/browse"))
	cards := chatCardsOf(column)
	var kinds []string
	for _, card := range cards {
		kinds = append(kinds, card[1])
	}
	if got, want := strings.Join(kinds, " "), "day page person series search link"; got != want {
		t.Fatalf("the column draws the cards %s, want %s:\n%s", got, want, column)
	}
	for _, want := range []string{`class="chat compact"`, "Weight", "71.2 kg · yesterday", "mood 4"} {
		if !strings.Contains(column, want) {
			t.Errorf("the column lacks %q", want)
		}
	}
}
