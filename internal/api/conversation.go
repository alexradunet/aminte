package api

// The conversation of a day: a plain page, as API.md describes it. This file is the one home of its convention in
// the program: the category, the first line, the owner's paragraph, the parse of the turns and their keys. Its title,
// and the rule that only say makes a page under that title, are in internal/core (conversation.go). The core calls no
// model. An agent (an extension) reads the page and writes its own paragraphs in the same format.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"lifelog/internal/core"
	"lifelog/siren"
	"lifelog/text"
)

// conversationCategory is the category page that every conversation page is filed in.
const conversationCategory = "Conversations"

// conversationFirstLine is the first line of the page: a link to the day it is about.
func conversationFirstLine(day string) string { return "[[" + day + "]]" }

// stamp is the brackets that begin an entry: the time in the server's zone, HH:MM, and the name of the relay when
// there is one: `(HH:MM)` or `(HH:MM, via NAME)`. say and capture both begin with it, so every surface stamps an
// entry alike. say takes the name from its field via, capture from its source (relayOf).
func (h *server) stamp(relay string) string {
	if relay != "" {
		relay = ", via " + relay
	}
	return "(" + h.s.Local().Format("15:04") + relay + ")"
}

// relayOf is the writer that relayed an entry when an agent or an import wrote it (its prefix cut), else "".
func relayOf(source string) string {
	for _, prefix := range []string{siren.AgentPrefix, siren.ImportPrefix} {
		if name, ok := strings.CutPrefix(source, prefix); ok {
			return name
		}
	}
	return ""
}

// words is the text of an entry as the page keeps it: LF line ends (a browser sends CRLF) and no blank edge.
func words(text string) string { return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")) }

// fenceLine says that a line opens or closes a fenced code block: it starts with three backticks. ParseTurns and
// paragraph read a fence by this one rule.
func fenceLine(l string) bool { return strings.HasPrefix(l, "```") }

// toolMark starts a tool line: ParseTurns reads the lines that start with it, right before a marker of lifelog, as the
// tool lines of that marker.
const toolMark = "- `"

// paragraph is one turn as the page keeps it: the marker of who (Owner or lifelog) with its stamp, then the words. The
// owner or the model wrote the words, and ParseTurns must read exactly one turn from the paragraph, so the words are
// made safe first:
//   - a line outside a fence that reads as a marker is escaped (`\*\*Owner\*\* (09:31): ...`): it shows the same
//     in a Markdown view, and it is text and not a turn;
//   - a line outside a fence that starts with a dash, a space and a backtick (toolMark) has a backslash before the
//     dash, and it is no tool line. ParseTurns gives the tool lines right before a marker of lifelog to that
//     marker, so a turn of the owner that ends in such lines would lose them to the reply that follows. ParseTurns
//     takes the backslash off again (turnWords), so the turn has the words that were said;
//   - a fence that the words open and do not close is closed by a line of three backticks at the end, so the next
//     marker is not hidden inside it;
//   - the words start on a line of their own when their first line is a fence line: after the marker, on its line, it
//     would open no fence for ParseTurns, and the line that closes the block would open one.
//
// A block that is balanced stays as it is, also with a marker line inside it.
func paragraph(who, stamp, text string) string {
	marker := "**" + who + "** " + stamp + ":"
	lines := strings.Split(words(text), "\n")
	sep, from := " ", 1 // lines[0] follows the marker on its line: ParseTurns reads no fence and no marker there
	if fenceLine(lines[0]) {
		sep, from = "\n", 0
	}
	fenced := false
	for i, l := range lines {
		switch {
		case fenceLine(l):
			fenced = !fenced
		case fenced:
		case strings.HasPrefix(l, toolMark):
			lines[i] = `\` + l
		case i >= from:
			if m := turnStart.FindStringSubmatch(l); m != nil {
				lines[i] = `\*\*` + m[1] + `\*\*` + l[len(m[1])+4:]
			}
		}
	}
	if fenced {
		lines = append(lines, "```")
	}
	return marker + sep + strings.Join(lines, "\n")
}

// Turn is one turn of a conversation, as ParseTurns reads it. Who is owner, lifelog, or text for a paragraph that no
// turn starts (the owner edited the page): At is then empty. Key names the turn (withKeys). Tools are the tool lines of
// a lifelog turn. Refs are the addresses that the tool lines name, in order, each once. Via is the writer that relayed
// the turn.
type Turn struct {
	Who   string   `json:"who"`
	Key   string   `json:"key"`
	At    string   `json:"at,omitempty"`
	Via   string   `json:"via,omitempty"`
	Text  string   `json:"text"`
	Tools []string `json:"tools,omitempty"`
	Refs  []string `json:"refs,omitempty"`
}

var turnStart = regexp.MustCompile(`^\*\*(Owner|lifelog)\*\* \((\d\d[:.]\d\d)(?:, via ([a-z0-9_:.-]+))?\): ?`)

// withKeys gives each turn its key and gives the turns back. A key is 16 hex digits of a SHA-256 hash of the who, the
// time, the relay and the words of the turn, and of the number of turns before it that have the same four: so no two
// turns of a page have one key, and a key does not move when a reply or a say is written, or when an earlier turn
// that is different is removed. When the owner removes an earlier turn that is equal (the same four), the later equal
// turn has the key that the removed turn had, and a reply with the key that the agent read for it is 409. A key
// changes when the owner edits the words, the time or the relay of the turn; reply then refuses it (409) and the
// agent reads the page again. A turn is named by what it is, not by its place, because the page changes while an agent
// works on a turn.
func withKeys(turns []Turn) []Turn {
	before := map[string]int{}
	for i := range turns {
		t := &turns[i]
		same := fmt.Sprintf("%q %q %q %q", t.Who, t.At, t.Via, t.Text) // quoted: no two different turns write one string
		sum := sha256.Sum256([]byte(same + " " + strconv.Itoa(before[same])))
		before[same]++
		t.Key = hex.EncodeToString(sum[:8])
	}
	return turns
}

// toolRefs are the addresses that the tool lines name after an arrow (siren.ToolLine), in order, each once.
func toolRefs(tools []string) []string {
	var refs []string
	for _, l := range tools {
		if ref := siren.ToolLine(l).Href; ref != "" && !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	return refs
}

// refRel is the rel of the sub-entities of a conversation that name what its agent touched, and refsPerTurn is how
// many addresses of one turn they name: a turn that names more shows the others as plain links.
const (
	refRel      = "ref"
	refsPerTurn = 6
)

// refsOf are the addresses that the conversation names as sub-entities with the rel ref: the addresses of each turn,
// the newest turn first, each address once, at most refsPerTurn of a turn. A client that asks for ?embed=ref gets the
// things that were touched last, within the bound of one embedding.
func refsOf(turns []Turn) []string {
	var refs []string
	for _, t := range slices.Backward(turns) {
		for _, ref := range t.Refs[:min(len(t.Refs), refsPerTurn)] {
			if !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

// earlierDays is how many earlier days the day bar of the chat names, and earlierSpan is how far back it looks.
const (
	earlierDays = 3
	earlierSpan = 14
)

// earlierConversations are the days before today that have a live conversation page, the newest earlierDays of them,
// oldest first. It looks back over earlierSpan days: a day further back is a link from the day view.
func (h *server) earlierConversations(ctx context.Context, today string) ([]string, error) {
	t, ok := text.ParseDay(today)
	if !ok {
		return nil, core.Invalid("today %q is not a day", today)
	}
	var days []string
	for i := 1; i <= earlierSpan && len(days) < earlierDays; i++ {
		day := t.AddDate(0, 0, -i).Format(time.DateOnly)
		id, err := h.s.PageID(ctx, core.ConversationTitle(day))
		if err != nil {
			return nil, err
		}
		if id == 0 {
			continue
		}
		p, err := h.s.PageByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if isConversation(p) {
			days = append([]string{day}, days...)
		}
	}
	return days, nil
}

// bodyLines are the lines of a body (CRLF is read as LF), and skip is how many of them the first line [[day]] takes: 1
// when the body starts with it, else 0. The turns are read from lines[skip:].
func bodyLines(day, body string) (lines []string, skip int) {
	lines = strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if strings.TrimRight(lines[0], " \t") == conversationFirstLine(day) {
		skip = 1
	}
	return lines, skip
}

// scanLines finds the marker lines of lines: their indexes, in order. A marker line is outside a fenced code block. It
// says too whether the lines end inside a fence.
func scanLines(lines []string) (marks []int, fenced bool) {
	for i, l := range lines {
		if fenceLine(l) {
			fenced = !fenced
		} else if !fenced && turnStart.MatchString(l) {
			marks = append(marks, i)
		}
	}
	return marks, fenced
}

// settled is the text of lines with its blank edge dropped, and a line of three backticks added when the lines end
// inside a fence that an edit of the owner left open. It is the head that a block follows (joinBlock): the end of the
// page for say, and the place after the answered turn for a reply. The marker of the block is then read as a marker
// and the block is not text in the fence. This is the one rule that a paragraph starts outside a fence, which
// paragraph assumes. ParseTurns reads the last turn of an open page as closed already, so the closing line changes
// neither the words nor the key of a turn.
func settled(lines []string) string {
	head := strings.TrimRightFunc(strings.Join(lines, "\n"), unicode.IsSpace) // the blank edge that ParseTurns drops
	if _, fenced := scanLines(lines); fenced {
		head += "\n```"
	}
	return head
}

// joinBlock is head and then block, after a blank line.
func joinBlock(head, block string) string {
	if head == "" {
		return block
	}
	return head + "\n\n" + block
}

// escapeToolTail puts a backslash before each line of the tool-line block that ends head: the lines that start with
// toolMark right before its end. A reply with no tool lines of its own follows head after one blank line, and
// ParseTurns gives such a block to the marker of the reply. With the backslash the lines stay in the turn that was
// answered, and ParseTurns gives their words back (turnWords), so the turn keeps its key. A head that ends in a fence
// line ends in no such block, so a line inside a fence is never changed.
func escapeToolTail(head string) string {
	lines := strings.Split(head, "\n")
	for i := len(lines); i > 0 && strings.HasPrefix(lines[i-1], toolMark); i-- {
		lines[i-1] = `\` + lines[i-1]
	}
	return strings.Join(lines, "\n")
}

// ParseTurns reads the turns of the body of the conversation page of day (CRLF is read as LF). A marker line starts
// with `**Owner** (HH:MM): ` or `**lifelog** (HH:MM): `, and may name the relaying writer inside the brackets,
// `**Owner** (HH:MM, via NAME): ` (NAME is [a-z0-9_:.-]+), and is outside a fenced code block (a line that starts with
// three backticks opens or closes a fence). A page written with the earlier separator, `(HH.MM)`, is read the same,
// and its turn's At is HH:MM. A turn runs from its marker line to the line before the next marker line,
// or to the end of the page; it keeps its inner blank lines and loses its blank edge. The tool lines of a lifelog
// turn are the lines that start with "- `" right before its marker: one block, with at most one blank line between
// it and the marker. They are not part of the turn before. The text before the first marker, after the first line
// [[day]], is one turn of Who text when it is not blank. The words of a turn are the words that were said: a
// backslash that paragraph put before a tool-like line is taken off (turnWords), and a fence that the page leaves open
// at its end is read as closed, as the next write closes it (settled). So a write that adds a turn, or a reply that
// closes the fence, changes neither the words nor the key of a turn that was there.
func ParseTurns(day, body string) []Turn {
	all, skip := bodyLines(day, body)
	lines := all[skip:]
	marks, fenced := scanLines(lines)
	turns := []Turn{}
	// segment is lines[from:to]; it ends where the next marker begins, and the tool lines of that marker leave it.
	segment := func(from, to int) (seg []string, tools []string) {
		seg = lines[from:to]
		if to == len(lines) || !strings.HasPrefix(lines[to], "**lifelog**") {
			return seg, nil
		}
		end := len(seg)
		if end > 0 && strings.TrimSpace(seg[end-1]) == "" {
			end--
		}
		start := end
		for start > 0 && strings.HasPrefix(seg[start-1], toolMark) {
			start--
		}
		for _, l := range seg[start:end] {
			tools = append(tools, strings.TrimRight(l, " \t"))
		}
		return seg[:start], tools
	}
	first := len(lines)
	if len(marks) > 0 {
		first = marks[0]
	}
	pre, tools := segment(0, first)
	if t := strings.TrimSpace(strings.Join(pre, "\n")); t != "" {
		turns = append(turns, Turn{Who: "text", Text: t})
	}
	for k, at := range marks {
		to := len(lines)
		if k+1 < len(marks) {
			to = marks[k+1]
		}
		m := turnStart.FindStringSubmatch(lines[at])
		seg, next := segment(at+1, to)
		turns = append(turns, Turn{Who: strings.ToLower(m[1]), At: strings.Replace(m[2], ".", ":", 1), Via: m[3], Text: turnWords(lines[at][len(m[0]):], seg), Tools: tools, Refs: toolRefs(tools)})
		tools = next
	}
	if fenced && len(turns) > 0 { // the page ends in a fence: the last turn is read as the next write closes it (settled)
		turns[len(turns)-1].Text += "\n```"
	}
	return turns
}

// turnWords are the words of a turn that a marker line starts: first, the rest of that line, and then the lines after
// it, with the blank edge dropped. paragraph put a backslash before each line outside a fence that starts with
// toolMark; it is taken off here, in the rest of the marker line too, and a line inside a fenced block keeps its
// backslash. The rest of the marker line opens no fence.
func turnWords(first string, rest []string) string {
	lines := append([]string{first}, rest...)
	fenced := false
	for i, l := range lines {
		switch {
		case i > 0 && fenceLine(l):
			fenced = !fenced
		case !fenced && strings.HasPrefix(l, `\`+toolMark):
			lines[i] = l[1:]
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// say is the owner's turn: it adds the paragraph to the conversation page of the day, made on the first turn.
func (h *server) say(r *http.Request, a admitted) (*siren.Entity, error) {
	day, err := h.conversationDayOf(r)
	if err != nil {
		return nil, err
	}
	if day != h.s.Today() {
		return nil, core.Invalid("say is for today: the agent answers today's conversation")
	}
	entry := words(a.v.Get("text"))
	if entry == "" {
		return nil, core.Invalid("missing field text")
	}
	via := a.v.Get("via")
	if via != "" && !core.WriterName(via) {
		return nil, core.Invalid("via %q: a name of letters, digits, _ : . -", via)
	}
	block := paragraph("Owner", h.stamp(via), entry)
	_, sync, err := h.s.AppendParagraph(r.Context(), a.src, core.ConversationTitle(day), day, conversationCategory,
		conversationFirstLine(day), func(body string) string {
			lines, _ := bodyLines(day, body)
			return joinBlock(settled(lines), block)
		})
	if err != nil {
		return nil, err
	}
	return withResult(h.conversationEntity(r.Context(), day))(sync)
}

// reply is the agent's turn: the paragraph **lifelog** (HH:MM): text, with the tool lines before it, written right after
// the owner's turn that it answers (the field turn is the key of that turn, from the conversation). The place is found
// and the write is made in one transaction, so a turn that the owner said meanwhile stays after the reply and waits
// for the next one. A turn that has a reply is 409, and so is a key that no owner turn of the page has now: the owner
// edited the turn, or removed it, and the agent reads the page again.
func (h *server) reply(r *http.Request, a admitted) (*siren.Entity, error) {
	day, err := h.conversationDayOf(r)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(a.v.Get("turn"))
	if key == "" {
		return nil, core.Invalid("missing field turn")
	}
	text := words(a.v.Get("text"))
	if text == "" {
		return nil, core.Invalid("missing field text")
	}
	tools, err := toolLines(a.v.Get("tools"))
	if err != nil {
		return nil, err
	}
	block := paragraph("lifelog", h.stamp(""), text)
	if tools != "" {
		block = tools + "\n\n" + block
	}
	_, sync, err := h.s.EditBody(r.Context(), a.src, core.ConversationTitle(day), func(body string) (string, error) {
		return insertReply(day, body, key, block)
	})
	if err != nil {
		return nil, err
	}
	return withResult(h.conversationEntity(r.Context(), day))(sync)
}

// toolLines are the tool lines of the field tools, one for each line of it, as the page keeps them (LF line ends, no
// blank line, no blank edge). A line must start with "- `", the mark that ParseTurns reads a tool line by.
func toolLines(field string) (string, error) {
	var lines []string
	for l := range strings.SplitSeq(strings.ReplaceAll(field, "\r\n", "\n"), "\n") {
		if l = strings.TrimRight(l, " \t\r"); l == "" {
			continue
		}
		if !strings.HasPrefix(l, toolMark) {
			return "", core.Invalid("tools: a tool line starts with - ` (a dash, a space and a backtick): %q", l)
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n"), nil
}

// insertReply puts block, the paragraph of a reply with its tool lines, right after the turn of the owner that has the
// key, and gives the new body. The turn after the answered turn is the next marker line, so the block goes before it: a
// turn that follows stays after the reply. A turn whose next turn is a turn of lifelog has its reply (409). A key that
// no owner turn has is 409 too: the page changed since the agent read it.
func insertReply(day, body, key, block string) (string, error) {
	all, skip := bodyLines(day, body)
	lines := all[skip:]
	marks, _ := scanLines(lines)
	var owners []int // the positions in marks of the owner's turns
	for k, m := range marks {
		if strings.HasPrefix(lines[m], "**Owner**") {
			owners = append(owners, k)
		}
	}
	n, seen := -1, 0 // n is the place of the key among the owner turns
	for _, t := range withKeys(ParseTurns(day, body)) {
		if t.Who == "owner" && t.Key == key {
			n = seen
			break
		}
		if t.Who == "owner" {
			seen++
		}
	}
	if n < 0 {
		return "", core.Conflict("no owner turn of the conversation of %s has the key %q: the page changed, read it again", day, key)
	}
	at, end := owners[n], len(lines)
	if at+1 < len(marks) {
		end = marks[at+1]
		if strings.HasPrefix(lines[end], "**lifelog**") {
			return "", core.Conflict("the owner turn %q of the conversation of %s has a reply already", key, day)
		}
	}
	out := joinBlock(escapeToolTail(settled(all[:skip+end])), block)
	if tail := strings.TrimLeft(strings.Join(all[skip+end:], "\n"), "\n"); tail != "" {
		out += "\n\n" + tail
	}
	return out, nil
}

// conversation is GET /days/{day}/conversation.
func (h *server) conversation(r *http.Request) (*siren.Entity, error) {
	day, err := h.conversationDayOf(r)
	if err != nil {
		return nil, err
	}
	return h.conversationEntity(r.Context(), day)
}

// conversationDayOf is the {day} of the path: a day, or the word today.
func (h *server) conversationDayOf(r *http.Request) (string, error) {
	day := h.dayNamed(r.PathValue("day"))
	if !text.IsDay(day) {
		return "", core.Invalid("day %q is not a local day: YYYY-MM-DD or today", day)
	}
	return day, nil
}

// conversationHref is the address of the conversation of a day.
func conversationHref(day string) string { return siren.DayHref(day) + "/conversation" }

// isConversation says that the page p is a conversation: a plain page that is live. A tombstoned page is none (say makes it
// live again), and a page of another type that holds the title (a file, a person or a metric: no write makes one now) holds
// no turns, whatever its body says.
func isConversation(p *core.Page) bool { return p.Type == "page" && p.DeletedAt == "" }

// conversationEntity is the conversation of a day: its turns, say, the links earlier to the conversations of the
// days before today (the day bar of a chat client) and, when a turn names addresses, the sub-entities ref. When the
// page exists it carries the page as a metric's series does (plan 144): its properties, links and actions, so one view
// draws both.
func (h *server) conversationEntity(ctx context.Context, day string) (*siren.Entity, error) {
	title := core.ConversationTitle(day)
	today := h.s.Today()
	e := &siren.Entity{Class: []string{"conversation"}, Title: title,
		Links:   []siren.Link{link("self", conversationHref(day), title), link("day", siren.DayHref(day), day), link("index", "/", "Home")},
		Actions: []siren.Action{}}
	earlier, err := h.earlierConversations(ctx, today)
	if err != nil {
		return nil, err
	}
	for _, d := range earlier {
		e.Links = append(e.Links, link("earlier", conversationHref(d), d))
	}
	if day == today { // say is for today: the agent answers today's conversation
		e.Actions = append(e.Actions, action("say", map[string]string{"day": day}, nil))
	}
	props := map[string]any{"day": day, "turns": []Turn{}}
	e.Properties = props
	id, err := h.s.PageID(ctx, title)
	if err != nil || id == 0 {
		return e, err
	}
	p, err := h.s.PageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isConversation(p) {
		return e, nil
	}
	if err := h.carryPage(ctx, e, id); err != nil {
		return nil, err
	}
	e.Class = append(e.Class, "page")
	e.Links = append(e.Links, link("page", siren.PageHref(id), title))
	turns := withKeys(ParseTurns(day, fmt.Sprint(props["body"])))
	props["turns"] = turns
	for _, ref := range refsOf(turns) {
		e.Entities = append(e.Entities, link(refRel, ref, ""))
	}
	return e, nil
}
