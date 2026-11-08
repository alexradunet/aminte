package api

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestAReplyAndASayMoveNoTurnThatWasThere: a property over random pages. The owner says a turn, edits the page by hand
// (the text is added as it is, with no escape: markers, fences, tool lines, CR, a backslash), and the agent replies to
// an owner turn that waits, with tool lines or without. Then the owner says again. The reply writes one turn of the
// agent right after the turn that it answers, with exactly the tool lines that the agent sent. Every turn that was on
// the page keeps its who, time, words, tool lines and key, so a client that tracks turns by their keys sees only the
// new turns. The seed is fixed; a failure prints the page.
func TestAReplyAndASayMoveNoTurnThatWasThere(t *testing.T) {
	const day = "2031-05-14"
	pieces := []string{"```", "````", "~~~", "**Owner** (09:31): x", "**aminte** (09:31): y", "**Owner** (09.31, via w): z", " **Owner** (09:31): s",
		"- `get`", "- `x` \u2192 /pages/1", "", "text", "\r", "\r\n", "  ```", "```go", "[[" + day + "]]", "\t", "``", "\\- `y`", "\\\\- `y`", "\u00a0", "- `"}
	rng := rand.New(rand.NewPCG(162, 3))
	made := func(pieces []string, seps ...string) string {
		var b []string
		for range 1 + rng.IntN(6) {
			b = append(b, pieces[rng.IntN(len(pieces))])
		}
		return strings.Join(b, seps[rng.IntN(len(seps))])
	}
	gen := func() string { return made(pieces, "\n", "\r\n", "\n\n", " ") }
	// A browser sends CRLF or LF, never a CR that stands alone before a line end, so the edit has none: CR, LF, then
	// CRLF is read as LF once, when the page is read, and again when a write has made it CRLF.
	editPieces := slices.DeleteFunc(slices.Clone(pieces), func(p string) bool { return p == "\r" })
	genEdit := func() string { return made(editPieces, "\n", "\r\n", "\n\n", " ") }
	sayOn := func(body, block string) string {
		lines, _ := bodyLines(day, body)
		return joinBlock(settled(lines), block) // what say writes
	}
	describe := func(turns []Turn) string {
		return fmt.Sprintf("%+v", turns)
	}
	ran := 0
	for n := range 30000 {
		t1, edit, t2, t3 := gen(), genEdit(), gen(), gen()
		if words(t1) == "" || words(t2) == "" || words(t3) == "" {
			continue
		}
		body := sayOn("[["+day+"]]", paragraph("Owner", "(09:07)", t1))
		if rng.IntN(4) != 0 {
			body += []string{"\n", "\n\n", "\r\n"}[rng.IntN(3)] + edit // the owner's edit, as it is
		}
		if rng.IntN(3) == 0 {
			body = sayOn(body, paragraph("Owner", "(09:08)", t3)) // a turn that waits behind the edit
		}
		before := withKeys(ParseTurns(day, body))
		var waiting []int // the owner turns that no turn of the agent follows
		for i, turn := range before {
			if turn.Who == "owner" && (i+1 == len(before) || before[i+1].Who != "aminte") {
				waiting = append(waiting, i)
			}
		}
		if len(waiting) == 0 {
			continue
		}
		ran++
		at := waiting[rng.IntN(len(waiting))]
		tools := ""
		if rng.IntN(2) == 0 {
			tools = "- `search`: no hits"
		}
		block := paragraph("aminte", "(09:09)", t2)
		if tools != "" {
			block = tools + "\n\n" + block
		}
		replied, err := insertReply(day, body, before[at].Key, block)
		if err != nil {
			t.Errorf("case %d: the reply to the waiting turn %d of %q: %v", n, at, body, err)
			continue
		}
		after := withKeys(ParseTurns(day, replied))
		if len(after) != len(before)+1 {
			t.Errorf("case %d: %d turns before the reply and %d after, want one more, for %q:\n%s\n%s", n, len(before), len(after), body, describe(before), describe(after))
			continue
		}
		got := after[at+1]
		if got.Who != "aminte" || strings.Join(got.Tools, "\n") != tools {
			t.Errorf("case %d: the turn after the answered turn is %+v, want aminte with the tool lines %q, for %q", n, got, tools, body)
		}
		same := append(slicesOf(after[:at+1]), slicesOf(after[at+2:])...)
		if !reflect.DeepEqual(same, slicesOf(before)) {
			t.Errorf("case %d: a turn that was there moved or changed, for %q:\nbefore %s\nafter  %s", n, body, describe(before), describe(after))
			continue
		}
		next := withKeys(ParseTurns(day, sayOn(replied, paragraph("Owner", "(09:10)", t3))))
		if len(next) != len(after)+1 || !reflect.DeepEqual(slicesOf(next[:len(after)]), slicesOf(after)) {
			t.Errorf("case %d: the say after the reply moved a turn, for %q:\nbefore %s\nafter  %s", n, replied, describe(after), describe(next))
		}
	}
	if ran < 1000 {
		t.Errorf("only %d random cases had a turn that waits; the generator is too narrow", ran)
	}
}

// slicesOf is turns as a value that DeepEqual compares by who, key, time, relay, words, tool lines and refs: the
// slice itself, with a nil slice and an empty one alike.
func slicesOf(turns []Turn) []Turn {
	out := make([]Turn, len(turns))
	for i, turn := range turns {
		if len(turn.Tools) == 0 {
			turn.Tools = nil
		}
		if len(turn.Refs) == 0 {
			turn.Refs = nil
		}
		out[i] = turn
	}
	return out
}
