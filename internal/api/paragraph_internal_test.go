package api

import (
	"strings"
	"testing"
)

// TestParagraphMakesTheWordsSafe: the vectors are written by hand. The paragraph is the marker and the words; a line
// outside a fence that reads as a marker is escaped, a fence that is left open is closed, and the words of a block
// that opens on the first line start on a line of their own.
func TestParagraphMakesTheWordsSafe(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"plain words", "Hello.", "**Owner** (09:07): Hello."},
		{"CRLF and a blank edge", " one\r\ntwo \r\n\r\n", "**Owner** (09:07): one\ntwo"},
		{"a marker of the owner on a later line", "a\n**Owner** (09:31): b", "**Owner** (09:07): a\n\\*\\*Owner\\*\\* (09:31): b"},
		{"a marker of the agent on a later line", "a\n**aminte** (09:31): b", "**Owner** (09:07): a\n\\*\\*aminte\\*\\* (09:31): b"},
		{"a marker with via and the earlier separator", "a\n**Owner** (09.31, via x): b", "**Owner** (09:07): a\n\\*\\*Owner\\*\\* (09.31, via x): b"},
		{"a marker on the first line is on the marker line, so no turn of its own", "**Owner** (09:31): b", "**Owner** (09:07): **Owner** (09:31): b"},
		{"a line that only looks like a marker", "a\n**Owner** 09:31: b\n**Owner**: c", "**Owner** (09:07): a\n**Owner** 09:31: b\n**Owner**: c"},
		{"a balanced block is kept, a marker inside it too", "a\n```\n**Owner** (09:31): b\n```\nc", "**Owner** (09:07): a\n```\n**Owner** (09:31): b\n```\nc"},
		{"a fence that is left open is closed", "a\n```\nb", "**Owner** (09:07): a\n```\nb\n```"},
		{"a marker after a block that is closed is escaped", "```\nx\n```\n**aminte** (09:31): b", "**Owner** (09:07):\n```\nx\n```\n\\*\\*aminte\\*\\* (09:31): b"},
		{"a block that opens on the first line", "```go\nx\n```", "**Owner** (09:07):\n```go\nx\n```"},
		{"an inline block on the first line is a fence for ParseTurns", "```ls```", "**Owner** (09:07):\n```ls```\n```"},
		{"two backticks are no fence", "a\n``\n**Owner** (09:31): b", "**Owner** (09:07): a\n``\n\\*\\*Owner\\*\\* (09:31): b"},
		{"lines that read as tool lines are escaped", "Run:\n- `ls`\n- `rm` \u2192 /pages/1", "**Owner** (09:07): Run:\n\\- `ls`\n\\- `rm` \u2192 /pages/1"},
		{"the first line too, so a list shows alike", "- `ls`\n- `rm`", "**Owner** (09:07): \\- `ls`\n\\- `rm`"},
		{"a tool line inside a balanced block is kept", "a\n```\n- `ls`\n```\nb", "**Owner** (09:07): a\n```\n- `ls`\n```\nb"},
		{"a tool line after a block is escaped", "```\nx\n```\n- `ls`", "**Owner** (09:07):\n```\nx\n```\n\\- `ls`"},
		{"a list that has no code, and an indented line, are kept", "- one\n- two\n  - `three`", "**Owner** (09:07): - one\n- two\n  - `three`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := paragraph("Owner", "(09:07)", tc.text); got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

// FuzzAParagraphIsOneTurn: whatever the words, a paragraph of the owner followed by a paragraph of the agent reads as
// two turns, in this order: the words make no turn and hide none. The words stay in their turn: the owner's turn
// reads back as the paragraph wrote it, with at most one line more (the fence that closes a block), and neither turn
// takes a tool line or a ref from the words.
func FuzzAParagraphIsOneTurn(f *testing.F) {
	for _, seed := range []string{
		"Hello", "a\n```\nb", "```\n```\n```", "**Owner** (09:31): x", "a\n**aminte** (09:31): x", "a\n```x```\n**Owner** (09:31, via y): z",
		"\n\n- `get`\n- `put`", "```", "a\r\n**Owner** (09:31): b", "~~~\n**Owner** (09:31): b", " **Owner** (09:31): b", "a\n````\n**Owner** (09:31): b",
		"Run:\n- `ls -la`\n- `rm` \u2192 /pages/1", "- `x`", "a\n```\n- `x`\n```\n- `y`", "a\r\r\n- `x`",
		"- `\r\r\n0", "a\n- `x`\n```\n- `y`", "- `a`\n\n- `b`\n```go\n- `c`",
	} {
		f.Add(seed)
	}
	const day = "2031-05-14"
	f.Fuzz(func(t *testing.T, text string) {
		if words(text) == "" {
			return
		}
		owner := paragraph("Owner", "(09:07)", text)
		body := "[[" + day + "]]\n\n" + owner + "\n\n" + paragraph("aminte", "(09:08)", "Ok.")
		turns := ParseTurns(day, body)
		if len(turns) != 2 || turns[0].Who != "owner" || turns[1].Who != "aminte" {
			t.Fatalf("the turns of %q: %+v, want owner aminte", body, turns)
		}
		// The owner's turn reads back line by line as the paragraph wrote it, but for a tool line outside a fence: it
		// reads back as it was said, with no backslash. The turn has at most one line more (the fence that closes a block).
		stored := strings.Split(strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(owner, "**Owner** (09:07):"), "\r\n", "\n")), "\n")
		said := strings.Split(strings.ReplaceAll(words(text), "\r\n", "\n"), "\n") // the page is read with CRLF as LF
		got := strings.Split(turns[0].Text, "\n")
		if len(got) != len(said) && len(got) != len(said)+1 {
			t.Fatalf("the owner's turn of %q has %d lines, want %d or one more", body, len(got), len(said))
		}
		if len(got) == len(said)+1 && got[len(said)] != "```" {
			t.Errorf("the owner's turn of %q ends in the extra line %q, want the line that closes a block", body, got[len(said)])
		}
		fenced := false
		for i, l := range said {
			want := stored[i]
			switch {
			case fenceLine(l):
				fenced = !fenced
			case !fenced && strings.HasPrefix(l, "- `"):
				want = l
			}
			if got[i] != want {
				t.Errorf("line %d of the owner's turn of %q reads back as %q, want %q", i+1, body, got[i], want)
			}
		}
		if turns[1].Text != "Ok." {
			t.Errorf("the reply of %q reads back as %q", body, turns[1].Text)
		}
		for _, turn := range turns {
			if len(turn.Tools) != 0 || len(turn.Refs) != 0 {
				t.Errorf("the %s turn of %q took the tool lines %q and the refs %q from the words", turn.Who, body, turn.Tools, turn.Refs)
			}
		}
	})
}
