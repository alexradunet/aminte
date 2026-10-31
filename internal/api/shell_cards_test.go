package api_test

import (
	"fmt"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheColumnDrawsSixCardsAndTheScreenDrawsEveryOne: two turns name six days each. The conversation view draws a card
// for each of the twelve. The column of any other page draws a card for the six of the newest turn, and the plain link
// for the six of the older turn.
func TestTheColumnDrawsSixCardsAndTheScreenDrawsEveryOne(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	day := func(n int) string { return fmt.Sprintf("/days/2031-05-%02d", n) }
	var older, newer []string
	for n := 1; n <= 6; n++ {
		older, newer = append(older, day(n)), append(newer, day(n+6))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")), "say"), map[string]string{"text": "Show me"}))
	conversationBody(t, c, "\n\n"+toolLines(older...)+"\n**lifelog** (09:30): First.\n\n**Owner** (09:31): More.\n\n"+toolLines(newer...)+"\n**lifelog** (09:32): Second.\n")

	kinds := func(page string) string {
		var out []string
		for _, card := range cardsOf(page) {
			out = append(out, card[1]+" "+card[2])
		}
		return strings.Join(out, ", ")
	}
	var every, column []string
	for _, ref := range append(older, newer...) { // the page shows the turns in order, the older first
		every = append(every, "day "+ref)
		column = append(column, "link "+ref)
	}
	for i := range 6 {
		column[6+i] = every[6+i] // the newer turn
	}
	if got, want := kinds(browse(t, h, "/days/"+fixedToday+"/conversation")), strings.Join(every, ", "); got != want {
		t.Errorf("the conversation view:\n%s\nwant\n%s", got, want)
	}
	if got, want := kinds(chatColumn.FindString(browse(t, h, "/browse"))), strings.Join(column, ", "); got != want {
		t.Errorf("the column:\n%s\nwant\n%s", got, want)
	}
}
