package api

import (
	"errors"
	"testing"

	"lifelog/internal/core"
)

// TestInsertReplyPlacesTheBlockByTurns: the vectors are written by hand. The block goes right after the owner turn that
// is counted, before the next marker line, whatever the bytes of the page are; a turn that follows keeps its place.
func TestInsertReplyPlacesTheBlockByTurns(t *testing.T) {
	const day = "2031-05-14"
	const block = "**lifelog** (09:08): R"
	for _, tc := range []struct {
		name, body string
		turn       int
		want       string
	}{
		{"the last turn", "[[2031-05-14]]\n\n**Owner** (09:07): A", 1, "[[2031-05-14]]\n\n**Owner** (09:07): A\n\n" + block},
		{"a blank edge of the page is dropped", "[[2031-05-14]]\n\n**Owner** (09:07): A\n\n\n", 1, "[[2031-05-14]]\n\n**Owner** (09:07): A\n\n" + block},
		{"a turn that follows keeps its place", "[[2031-05-14]]\n\n**Owner** (09:07): A\n\n**Owner** (09:08): B", 1,
			"[[2031-05-14]]\n\n**Owner** (09:07): A\n\n" + block + "\n\n**Owner** (09:08): B"},
		{"the second turn after a reply to the first", "[[2031-05-14]]\n\n**Owner** (09:07): A\n\n**lifelog** (09:07): R1\n\n**Owner** (09:08): B", 2,
			"[[2031-05-14]]\n\n**Owner** (09:07): A\n\n**lifelog** (09:07): R1\n\n**Owner** (09:08): B\n\n" + block},
		{"an edit of an earlier turn does not move the block", "[[2031-05-14]]\n\n**Owner** (09:07): A, edited\n\n\n\n**Owner** (09:08): B", 1,
			"[[2031-05-14]]\n\n**Owner** (09:07): A, edited\n\n" + block + "\n\n**Owner** (09:08): B"},
		{"the note of the owner after a turn stays in the turn", "[[2031-05-14]]\n\n**Owner** (09:07): A\n\nA note.\n\n**Owner** (09:08): B", 1,
			"[[2031-05-14]]\n\n**Owner** (09:07): A\n\nA note.\n\n" + block + "\n\n**Owner** (09:08): B"},
		{"CRLF becomes LF", "[[2031-05-14]]\r\n\r\n**Owner** (09:07): A\r\n\r\n**Owner** (09:08): B", 1,
			"[[2031-05-14]]\n\n**Owner** (09:07): A\n\n" + block + "\n\n**Owner** (09:08): B"},
		{"a page without the first line", "**Owner** (09:07): A", 1, "**Owner** (09:07): A\n\n" + block},
		{"text before the first marker", "[[2031-05-14]]\n\nA free note.\n\n**Owner** (09:07): A", 1, "[[2031-05-14]]\n\nA free note.\n\n**Owner** (09:07): A\n\n" + block},
		{"a fake marker in a block is no turn", "[[2031-05-14]]\n\n**Owner** (09:07): A\n```\n**Owner** (09:08): fake\n```\n\n**Owner** (09:09): B", 2,
			"[[2031-05-14]]\n\n**Owner** (09:07): A\n```\n**Owner** (09:08): fake\n```\n\n**Owner** (09:09): B\n\n" + block},
		{"a fence that the page leaves open is closed", "[[2031-05-14]]\n\n**Owner** (09:07): A\n```\nx", 1, "[[2031-05-14]]\n\n**Owner** (09:07): A\n```\nx\n```\n\n" + block},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := insertReply(day, tc.body, tc.turn, block)
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v\nwant %q", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name, body string
		turn       int
		status     int
	}{
		{"a turn the page does not have", "[[2031-05-14]]\n\n**Owner** (09:07): A", 2, 404},
		{"a page with no turn", "[[2031-05-14]]", 1, 404},
		{"a turn that has a reply", "[[2031-05-14]]\n\n**Owner** (09:07): A\n\n**lifelog** (09:07): R", 1, 409},
		{"the reply is a turn of the agent, the second owner turn has none", "[[2031-05-14]]\n\n**Owner** (09:07): A\n\n**lifelog** (09:07): R", 2, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := insertReply(day, tc.body, tc.turn, block)
			var ce *core.Error
			if !errors.As(err, &ce) || ce.Status != tc.status {
				t.Errorf("got %v, want the status %d", err, tc.status)
			}
		})
	}
}
