package api

// The conversation of a day: a plain page, as API.md describes it. This file is the one home of its convention in
// the program: the title, the category, the first line, the owner's paragraph and the parse of the turns. The core
// calls no model. An agent (an extension) reads the page and writes its own paragraphs in the same format.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"lifelog/internal/core"
	"lifelog/siren"
	"lifelog/text"
)

// conversationCategory is the category page that every conversation page is filed in.
const conversationCategory = "Conversations"

// ConversationTitle is the title of the conversation page of a local day.
func ConversationTitle(day string) string { return "Conversation " + day }

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

// ownerParagraph is the paragraph of one turn of the owner: `**Owner** (HH:MM): words`, with the stamp of the writer.
func ownerParagraph(stamp, text string) string { return "**Owner** " + stamp + ": " + words(text) }

// conversationDay is the day of a conversation page, from its title; "" when the title is not one.
func conversationDay(title string) string {
	day, ok := strings.CutPrefix(title, "Conversation ")
	if !ok || !text.IsDay(day) || ConversationTitle(day) != title {
		return ""
	}
	return day
}

// Turn is one turn of a conversation, as ParseTurns reads it. Who is owner, lifelog, or text for a paragraph that no
// turn starts (the owner edited the page): At is then empty. Tools are the tool lines of a lifelog turn.
// Refs are the addresses that the tool lines name, in order, each once. Via is the writer that relayed the turn.
type Turn struct {
	Who   string   `json:"who"`
	At    string   `json:"at,omitempty"`
	Via   string   `json:"via,omitempty"`
	Text  string   `json:"text"`
	Tools []string `json:"tools,omitempty"`
	Refs  []string `json:"refs,omitempty"`
}

var turnStart = regexp.MustCompile(`^\*\*(Owner|lifelog)\*\* \((\d\d[:.]\d\d)(?:, via ([a-z0-9_:.-]+))?\): ?`)

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
		id, err := h.s.PageID(ctx, ConversationTitle(day))
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
		if p.DeletedAt == "" {
			days = append([]string{day}, days...)
		}
	}
	return days, nil
}

// ParseTurns reads the turns of the body of the conversation page of day (CRLF is read as LF). A marker line starts
// with `**Owner** (HH:MM): ` or `**lifelog** (HH:MM): `, and may name the relaying writer inside the brackets,
// `**Owner** (HH:MM, via NAME): ` (NAME is [a-z0-9_:.-]+), and is outside a fenced code block (a line that starts with
// three backticks opens or closes a fence). A page written with the earlier separator, `(HH.MM)`, is read the same,
// and its turn's At is HH:MM. A turn runs from its marker line to the line before the next marker line,
// or to the end of the page; it keeps its inner blank lines and loses its blank edge. The tool lines of a lifelog
// turn are the lines that start with "- `" right before its marker: one block, with at most one blank line between
// it and the marker. They are not part of the turn before. The text before the first marker, after the first line
// [[day]], is one turn of Who text when it is not blank.
func ParseTurns(day, body string) []Turn {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if len(lines) > 0 && strings.TrimRight(lines[0], " \t") == conversationFirstLine(day) {
		lines = lines[1:]
	}
	var marks []int // the index of each marker line
	fenced := false
	for i, l := range lines {
		if strings.HasPrefix(l, "```") {
			fenced = !fenced
		} else if !fenced && turnStart.MatchString(l) {
			marks = append(marks, i)
		}
	}
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
		for start > 0 && strings.HasPrefix(seg[start-1], "- `") {
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
		words := strings.TrimSpace(lines[at][len(m[0]):] + "\n" + strings.Join(seg, "\n"))
		turns = append(turns, Turn{Who: strings.ToLower(m[1]), At: strings.Replace(m[2], ".", ":", 1), Via: m[3], Text: words, Tools: tools, Refs: toolRefs(tools)})
		tools = next
	}
	return turns
}

// say is the owner's turn: it adds the paragraph to the conversation page of the day, made on the first turn.
func (h *server) say(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	day, err := h.conversationDayOf(r)
	if err != nil {
		return nil, err
	}
	if day != h.s.Today() {
		return nil, core.Invalid("say is for today: the agent answers today's conversation")
	}
	entry := words(v.Get("text"))
	if entry == "" {
		return nil, core.Invalid("missing field text")
	}
	via := v.Get("via")
	if via != "" && !writerRE.MatchString(via) {
		return nil, core.Invalid("via %q: a name of letters, digits, _ : . -", via)
	}
	_, sync, err := h.s.AppendParagraph(r.Context(), src, ConversationTitle(day), day, conversationCategory,
		conversationFirstLine(day), ownerParagraph(h.stamp(via), entry))
	if err != nil {
		return nil, err
	}
	return withResult(h.conversationEntity(r.Context(), day))(sync)
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

// conversationEntity is the conversation of a day: its turns, say, the links earlier to the conversations of the
// days before today (the day bar of a chat client) and, when a turn names addresses, the sub-entities ref. When the
// page exists it carries the page as a metric's series does (plan 144): its properties, links and actions, so one view
// draws both.
func (h *server) conversationEntity(ctx context.Context, day string) (*siren.Entity, error) {
	title := ConversationTitle(day)
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
	pe, err := h.pageEntity(ctx, id)
	if err != nil {
		return nil, err
	}
	if slices.Contains(pe.Class, "deleted") { // a tombstoned page is no conversation: say revives it
		return e, nil
	}
	if err := h.carryPage(ctx, e, id); err != nil {
		return nil, err
	}
	e.Class = append(e.Class, "page")
	e.Links = append(e.Links, link("page", siren.PageHref(id), title))
	turns := ParseTurns(day, fmt.Sprint(props["body"]))
	props["turns"] = turns
	for _, ref := range refsOf(turns) {
		e.Entities = append(e.Entities, link(refRel, ref, ""))
	}
	return e, nil
}
