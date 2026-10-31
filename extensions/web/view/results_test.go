package view

import (
	"strings"
	"testing"
)

// TestAResultIsSaidInSentences: each result shape of API.md in sentences; a key no sentence knows as a fact; a title
// with markup escaped.
func TestAResultIsSaidInSentences(t *testing.T) {
	c := clock{today: "2026-10-12"}
	say := func(m map[string]any) string {
		var out []string
		for _, l := range resultSentences(m, c, "") {
			out = append(out, string(l.HTML))
		}
		return strings.Join(out, " | ")
	}
	for _, tc := range []struct {
		name string
		in   map[string]any
		want string
	}{
		{"sync", map[string]any{"linked": []any{"B", "C", "A"}, "added": []any{"B", "A"}, "created": []any{"<b>"}, "revived": []any{"D"}, "skipped": []any{"x/y"}},
			`Linked <a href="/pages?title=A">A</a> and <a href="/pages?title=B">B</a>. | Created <a href="/pages?title=%3Cb%3E">&lt;b&gt;</a>. | ` +
				`Revived <a href="/pages?title=D">D</a>. | Not linked, not a valid title: <code>[[x/y]]</code>.`},
		{"nothing to say", map[string]any{"linked": []any{}}, "Done."},
		{"no link added", map[string]any{"linked": []any{"A", "B"}}, "Done."},
		{"add-file", map[string]any{"id": 7.0, "existing": true, "day": "2026-10-11",
			"embedded": true, "sync": map[string]any{"linked": []any{"A"}, "added": []any{"A"}}, "day_sync": map[string]any{"revived": []any{"B"}}},
			`In its page: Linked <a href="/pages?title=A">A</a>. | In its day page: Revived <a href="/pages?title=B">B</a>. | ` +
				`Kept the file: <a href="/pages/7">its page</a>. | The same original was kept before. | ` +
				`Its own day is <time datetime="2026-10-11">yesterday · Sun 11 Oct</time>. | Shown on its day page.`},
		{"add-file of a photo with no day, kept before", map[string]any{"id": 7.0, "existing": true, "stored_day": "2026-10-11"},
			`Kept the file: <a href="/pages/7">its page</a>. | The same original was kept before. | ` +
				`Its page keeps the day <time datetime="2026-10-11">yesterday · Sun 11 Oct</time>.`},
		{"a capture sent again", map[string]any{"already": true}, "Already added. Nothing changed."},
		{"snapshot", map[string]any{"snapshot": "snaps/life-2026-10-12.db", "restore_check": "ok"},
			`Took a snapshot: <code>snaps/life-2026-10-12.db</code>. | <dl class="facts"><dt>restore check</dt><dd>ok</dd></dl>`},
		{"unknown", map[string]any{"foo_bar": 1.0}, `<dl class="facts"><dt>foo bar</dt><dd>1</dd></dl>`},
	} {
		if got := say(tc.in); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}
