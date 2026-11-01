package core

import (
	"strings"

	"lifelog/text"
)

// The conversation page of a day is the plain page that say makes (API.md, the conversation). Its title is the one
// way to find it, so the title is reserved: reservedTitle is the one refusal, and the writes below call it. Two writes
// are exempt:
//   - AppendParagraph, which is say, makes the page and writes its turns;
//   - a wikilink makes an empty plain page for a target that does not exist (the save contract of wikilinks, D19),
//     also for a conversation title. The page has no turns, so it is no conversation yet, and the first say adopts it.
//
// Every other write that makes a page, gives a page a title, or changes the type of a page refuses a conversation
// title: insertPage (create-page, add-file, create-person, register-metric, the category of file-pages and of
// create-page), rename, and retype (promote, add-file, register-metric on a page that exists).

// ConversationTitle is the title of the conversation page of a local day.
func ConversationTitle(day string) string { return "Conversation " + day }

// ConversationDay is the day of the conversation page that a title names; "" when it names none. The key of a title
// ignores case, so "conversation 2031-05-14" names the page of that day too.
func ConversationDay(title string) string {
	day, ok := strings.CutPrefix(text.TitleKey(title), "conversation ")
	if !ok || !text.IsDay(day) {
		return ""
	}
	return day
}

// reservedTitle refuses a conversation title: 409 with one sentence for every write and every writer.
func reservedTitle(title string) error {
	if ConversationDay(title) == "" {
		return nil
	}
	return Conflict("%q is a conversation title: only say makes that page, and a conversation page keeps its title: its day names it", title)
}
