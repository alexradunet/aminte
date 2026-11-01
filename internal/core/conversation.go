package core

import (
	"net/http"
	"regexp"
	"strings"

	"lifelog/siren"
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

// The body of a conversation page holds the turns of the owner, and a line that reads as one would be a turn that the
// owner never said. So a writer that is not the owner's does not write that body: only the two writes of turns do
// (AppendParagraph for say, EditBody for reply), and they make the lines safe. SetBody is the one write of a body
// for every other caller (the save of a body, set-cover), and it calls keepTurns. This is the one home of the rule.

// writerRE is the grammar of a writer name: the CHECKs entities_source, measurements_source and links_source.
var writerRE = regexp.MustCompile(`^[a-z0-9_:.-]{1,64}$`)

// WriterName says that name is a writer name. A surface that reads a writer name and meets no CHECK of it (an
// owner-only action, a write that only changes a body) asks it here.
func WriterName(name string) bool { return writerRE.MatchString(name) }

// AgentWriter says that a writer is an agent (agent:*) or an import (import:*): the writers that the owner's rules bind.
func AgentWriter(source string) bool {
	return strings.HasPrefix(source, siren.AgentPrefix) || strings.HasPrefix(source, siren.ImportPrefix)
}

// ownerWriter says that a writer may speak as the owner: its name is a writer name, and it is not an agent or an
// import. A name that is no writer name ("Agent:x") is not the owner's: the prefix tests of AgentWriter would pass
// it, and a write that only changes a body meets no CHECK of the name.
func ownerWriter(source string) bool { return WriterName(source) && !AgentWriter(source) }

// keepTurns refuses (403) the write of the body of the page id when the page is a conversation page and the writer of
// the transaction is not the owner's. A page that does not exist is no conversation page: the write finds it.
func (t *Tx) keepTurns(id int64) error {
	if ownerWriter(t.source) {
		return nil
	}
	p, err := t.findPage(false, `e.id = ?`, id)
	if err != nil || p == nil || ConversationDay(p.Title) == "" {
		return err
	}
	return &Error{Status: http.StatusForbidden, Msg: "an agent or an import may not write a conversation page: the agent writes its turn with reply"}
}
