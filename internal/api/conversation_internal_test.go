package api

import (
	"reflect"
	"testing"
)

// TestParseTurnsReadsAHandWrittenBody: the vectors are written by hand, as an agent in another language would write
// them, and not made by the program's own formatter. A turn runs from its marker line to the next marker line. They
// hold an owner turn of two paragraphs, a lifelog turn with tool lines, a note that the owner added after a reply
// (it is part of the reply), a fenced block with a fake marker inside, CRLF line ends, tool lines before an owner
// turn and the first line of another day.
func TestParseTurnsReadsAHandWrittenBody(t *testing.T) {
	const day = "2031-05-14"
	for _, tc := range []struct {
		name, body string
		want       []Turn
	}{
		{"an empty body", "", []Turn{}},
		{"the first line only", "[[2031-05-14]]", []Turn{}},
		{"an owner turn of two paragraphs", "[[2031-05-14]]\n\n**Owner** (09:07): First.\n\nSecond,\non two lines.\n",
			[]Turn{{Who: "owner", At: "09:07", Text: "First.\n\nSecond,\non two lines."}}},
		{"a lifelog turn with tool lines", "[[2031-05-14]]\n\n**Owner** (09:07): How did I sleep?\n\n" +
			"- `get`\n- `search`: no hits\n\n**lifelog** (09:08): Seven hours.\n\nA second paragraph.\n",
			[]Turn{
				{Who: "owner", At: "09:07", Text: "How did I sleep?"},
				{Who: "lifelog", At: "09:08", Text: "Seven hours.\n\nA second paragraph.", Tools: []string{"- `get`", "- `search`: no hits"}},
			}},
		{"a note after a reply is part of the reply", "[[2031-05-14]]\n\n**lifelog** (09:08): Seven hours.\n\nA note the owner typed.\n\n**Owner** (10:00): Thanks.",
			[]Turn{
				{Who: "lifelog", At: "09:08", Text: "Seven hours.\n\nA note the owner typed."},
				{Who: "owner", At: "10:00", Text: "Thanks."},
			}},
		{"a fake marker in a fenced block", "[[2031-05-14]]\n\n**lifelog** (09:08): Look:\n\n```\n**Owner** (09:09): fake\n```\n\nDone.",
			[]Turn{{Who: "lifelog", At: "09:08", Text: "Look:\n\n```\n**Owner** (09:09): fake\n```\n\nDone."}}},
		{"CRLF line ends", "[[2031-05-14]]\r\n\r\n**Owner** (09:07): Hello\r\n\r\n- `get`\r\n\r\n**lifelog** (09:08): Hi\r\n",
			[]Turn{
				{Who: "owner", At: "09:07", Text: "Hello"},
				{Who: "lifelog", At: "09:08", Text: "Hi", Tools: []string{"- `get`"}},
			}},
		{"tool lines before an owner turn are part of the text before it", "[[2031-05-14]]\n\nA note.\n\n- `get`\n\n**Owner** (09:07): Hello",
			[]Turn{{Who: "text", Text: "A note.\n\n- `get`"}, {Who: "owner", At: "09:07", Text: "Hello"}}},
		{"tool lines before the first lifelog turn", "[[2031-05-14]]\n- `get`\n**lifelog** (09:08): Hi",
			[]Turn{{Who: "lifelog", At: "09:08", Text: "Hi", Tools: []string{"- `get`"}}}},
		{"a mark that is not a turn", "**Owner** 09.07: no brackets\n\n**lifelog** (9.7): bad time",
			[]Turn{{Who: "text", Text: "**Owner** 09.07: no brackets\n\n**lifelog** (9.7): bad time"}}},
		{"the earlier separator is read, and at is HH:MM", "**Owner** (14.37, via whatsapp): Hi\n\n**lifelog** (14.38): Hello",
			[]Turn{{Who: "owner", At: "14:37", Via: "whatsapp", Text: "Hi"}, {Who: "lifelog", At: "14:38", Text: "Hello"}}},
		{"a marker with via", "**Owner** (14:37, via whatsapp): Hi\n\n**lifelog** (14:38): Hello",
			[]Turn{{Who: "owner", At: "14:37", Via: "whatsapp", Text: "Hi"}, {Who: "lifelog", At: "14:38", Text: "Hello"}}},
		{"a via with a dot and a dash", "**Owner** (14:37, via a.b-c_1): Hi",
			[]Turn{{Who: "owner", At: "14:37", Via: "a.b-c_1", Text: "Hi"}}},
		{"a via with a colon (the relay agent:a:b)", "**Owner** (14:37, via a:b): Hi\n\n**lifelog** (14:38): Hello",
			[]Turn{{Who: "owner", At: "14:37", Via: "a:b", Text: "Hi"}, {Who: "lifelog", At: "14:38", Text: "Hello"}}},
		{"a bracket part that is not via", "**Owner** (14:37, from whatsapp): Hi\n\n**Owner** (14:37, via WhatsApp): Hi\n\n**Owner** (14:37, via ): Hi",
			[]Turn{{Who: "text", Text: "**Owner** (14:37, from whatsapp): Hi\n\n**Owner** (14:37, via WhatsApp): Hi\n\n**Owner** (14:37, via ): Hi"}}},
		{"the refs of the tool lines", "**Owner** (09:07): Q\n\n" +
			"- `get` \u2192 [/days/2031-05-14](/days/2031-05-14)\n- `search` \u2192 /search?q=x\n- `get` failed: not_found\n" +
			"- `get` \u2192 [/days/2031-05-14](/days/2031-05-14)\n\n**lifelog** (09:08): A",
			[]Turn{{Who: "owner", At: "09:07", Text: "Q"}, {Who: "lifelog", At: "09:08", Text: "A", Tools: []string{
				"- `get` \u2192 [/days/2031-05-14](/days/2031-05-14)", "- `search` \u2192 /search?q=x", "- `get` failed: not_found",
				"- `get` \u2192 [/days/2031-05-14](/days/2031-05-14)"},
				Refs: []string{"/days/2031-05-14", "/search?q=x"}}}},
		{"a ref that is not a path", "**lifelog** (09:08): A\n\n- `get` \u2192 [http://x.test/a](http://x.test/a)\n- `get` \u2192 days/1\n- `get` \u2192 [broken\n\n**lifelog** (09:09): B",
			[]Turn{{Who: "lifelog", At: "09:08", Text: "A"}, {Who: "lifelog", At: "09:09", Text: "B", Tools: []string{
				"- `get` \u2192 [http://x.test/a](http://x.test/a)", "- `get` \u2192 days/1", "- `get` \u2192 [broken"}}}},
		{"a first line that names another day is text", "[[2031-05-13]]", []Turn{{Who: "text", Text: "[[2031-05-13]]"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseTurns(day, tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

// TestParseTurnsGivesTheWordsAsTheyWereSaid: the vectors are written by hand. The page keeps a backslash before a line
// outside a fenced block that starts with a dash, a space and a backtick (paragraph put it there, so the line is no
// tool line), and ParseTurns takes it off again: the words of a turn are the words that were said, and a client that
// draws or sends them shows a list. A block keeps what it holds, and the text before the first marker is not a turn
// that paragraph wrote. A code fence that the last turn leaves open is read as closed, so the words and the key of
// the turn are the same before and after the line of three backticks that closes it.
func TestParseTurnsGivesTheWordsAsTheyWereSaid(t *testing.T) {
	const day = "2031-05-14"
	for _, tc := range []struct {
		name, body string
		want       []Turn
	}{
		{"the escape of a tool line is taken off", "[[2031-05-14]]\n\n**Owner** (09:07): Files:\n\\- `main.go`\n\\- `util.go`\n\n**lifelog** (09:08): Ok",
			[]Turn{{Who: "owner", At: "09:07", Text: "Files:\n- `main.go`\n- `util.go`"}, {Who: "lifelog", At: "09:08", Text: "Ok"}}},
		{"also on the line of the marker", "**Owner** (09:07): \\- `ls`\n\\- `rm`",
			[]Turn{{Who: "owner", At: "09:07", Text: "- `ls`\n- `rm`"}}},
		{"an escaped line is no tool line of the reply that follows", "**Owner** (09:07): Run:\n\\- `ls`\n\n**lifelog** (09:08): Done",
			[]Turn{{Who: "owner", At: "09:07", Text: "Run:\n- `ls`"}, {Who: "lifelog", At: "09:08", Text: "Done"}}},
		{"the reply keeps its list too, and the tool lines stay tool lines", "**Owner** (09:07): Q\n\n- `get`\n\n**lifelog** (09:08): Files:\n\\- `a.go`",
			[]Turn{{Who: "owner", At: "09:07", Text: "Q"}, {Who: "lifelog", At: "09:08", Text: "Files:\n- `a.go`", Tools: []string{"- `get`"}}}},
		{"a block keeps what it holds, a line after it is read", "**Owner** (09:07): a\n```\n\\- `x`\n```\n\\- `y`",
			[]Turn{{Who: "owner", At: "09:07", Text: "a\n```\n\\- `x`\n```\n- `y`"}}},
		{"the text before the first marker is kept as it is", "\\- `x`\n\n**Owner** (09:07): a",
			[]Turn{{Who: "text", Text: "\\- `x`"}, {Who: "owner", At: "09:07", Text: "a"}}},
		{"a backslash that is not before a tool line stays", "**Owner** (09:07): \\- one\n\\-`two`\n \\- `three`\n\\\\- `four`",
			[]Turn{{Who: "owner", At: "09:07", Text: "\\- one\n\\-`two`\n \\- `three`\n\\\\- `four`"}}},
		{"a fence that the last turn leaves open is read as closed", "**Owner** (09:07): a\n```\nx",
			[]Turn{{Who: "owner", At: "09:07", Text: "a\n```\nx\n```"}}},
		{"the same turn with its fence closed", "**Owner** (09:07): a\n```\nx\n```",
			[]Turn{{Who: "owner", At: "09:07", Text: "a\n```\nx\n```"}}},
		{"an open fence after a blank line, and the turn before it", "**Owner** (09:07): a\n\n**lifelog** (09:08): b\n```\n- `x`\n\n",
			[]Turn{{Who: "owner", At: "09:07", Text: "a"}, {Who: "lifelog", At: "09:08", Text: "b\n```\n- `x`\n```"}}},
		{"text with a fence left open", "```\nx", []Turn{{Who: "text", Text: "```\nx\n```"}}},
		{"a block that opens and closes is not changed", "**Owner** (09:07): a\n```\nx\n```\n\n**lifelog** (09:08): b",
			[]Turn{{Who: "owner", At: "09:07", Text: "a\n```\nx\n```"}, {Who: "lifelog", At: "09:08", Text: "b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseTurns(day, tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
	open := withKeys(ParseTurns(day, "**Owner** (09:07): a\n```\nx"))
	closed := withKeys(ParseTurns(day, "**Owner** (09:07): a\n```\nx\n```"))
	if open[0].Key != closed[0].Key {
		t.Errorf("the key of a turn with its fence open: %s, closed: %s, want the same key", open[0].Key, closed[0].Key)
	}
}
