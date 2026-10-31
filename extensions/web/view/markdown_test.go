package view

import (
	"runtime"
	"strings"
	"testing"

	"lifelog/text"
)

func TestMarkdownLinksWhatNamesAPage(t *testing.T) {
	for _, c := range []struct{ in, want, not string }{
		{"with [[Ana]]", `<a href="/pages?title=Ana" class="wikilink">Ana</a>`, ""},
		{"[[Ana Popescu|Ana]]", `<a href="/pages?title=Ana+Popescu" class="wikilink">Ana</a>`, ""},
		{"[[Café Lume]]", `href="/pages?title=Caf%C3%A9+Lume"`, ""},
		{"`[[Ana]]`", "<code>[[Ana]]</code>", "wikilink"},
		{"[[a/b]]", "[[a/b]]", "wikilink"},
		{"ran #run-club", `<a href="/pages?title=run-club" class="wikilink">#run-club</a>`, ""},
		{"a#b #123 #redirect", `<a href="/pages?title=redirect" class="wikilink">#redirect</a>`, ""},
		{"[[<b>x]]", "", "<b>"},
		{"<script>alert(1)</script>", "raw HTML omitted", "<script>"},
		{"[x](javascript:alert(1))", `<a href="">x</a>`, "javascript"},
		{"# Heading\n\n*em* **strong**", "<h1>Heading</h1>", ""},
		{"see ![[Lake.jpg|the lake]] here", `<a href="/pages?title=Lake.jpg" class="wikilink embed"><img src="/previews?title=Lake.jpg" alt="the lake" loading="lazy"></a>`, "!"},
		{"![[Lake.jpg]]", `alt="Lake.jpg"`, ""},
		{"![[a/b]] ![x](u.png)", `![[a/b]] <img src="u.png" alt="x">`, "embed"},
	} {
		got := string(markdown(c.in))
		if !strings.Contains(got, c.want) || (c.not != "" && strings.Contains(got, c.not)) {
			t.Errorf("%q renders %q", c.in, got)
		}
	}
}

// Whole-document expectations catch stolen destinations, nested anchors and leftover delimiters.
func TestMarkdownLinkPrecedence(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"[[Ana]](https://example.test)", `<p><a href="https://example.test">[Ana]</a></p>`},
		{"[label [[Ana]] #run](https://example.test)", `<p><a href="https://example.test">label [[Ana]] #run</a></p>`},
		{"[[Ana]][ref]\n\n[ref]: https://example.test", `<p><a href="https://example.test">[Ana]</a></p>`},
		{"![[Ana]](photo.png)", `<p><img src="photo.png" alt="[Ana]"></p>`},
		{"![[Ana]][pic]\n\n[pic]: photo.png", `<p><img src="photo.png" alt="[Ana]"></p>`},
		{`[\[Ana\] &amp; Café](u)`, `<p><a href="u">[Ana] &amp; Café</a></p>`},
		{"[[Ana *x*]]", `<p>[[Ana <em>x</em>]]</p>`},
		{"[[Ana\nPopescu]]", "<p>[[Ana\nPopescu]]</p>"},
		{"&#35;run ![[Café|A &amp; B]]", `<p><a href="/pages?title=run" class="wikilink">#run</a> <a href="/pages?title=Caf%C3%A9" class="wikilink embed"><img src="/previews?title=Caf%C3%A9" alt="A &amp; B" loading="lazy"></a></p>`},
		{"![label [[Ana]] #run](photo.png)", `<p><img src="photo.png" alt="label [[Ana]] #run"></p>`},
		{"[x](https://example.test/[[Ana]])", `<p><a href="https://example.test/%5B%5BAna%5D%5D">x</a></p>`},
		{"[[Café|A &amp; B]]", `<p><a href="/pages?title=Caf%C3%A9" class="wikilink">A &amp; B</a></p>`},
		{`\[\[Ana\]\]`, `<p><a href="/pages?title=Ana" class="wikilink">Ana</a></p>`},
		{"&#91;&#91;Ana&#93;&#93;", `<p><a href="/pages?title=Ana" class="wikilink">Ana</a></p>`},
		{"[[Ana|&lt;script&gt;]]", `<p><a href="/pages?title=Ana" class="wikilink">&lt;script&gt;</a></p>`},
		{"[[Café]](u) [[Ana]]", `<p><a href="u">[Café]</a> <a href="/pages?title=Ana" class="wikilink">Ana</a></p>`},
		{"[[Ana]](broken", `<p><a href="/pages?title=Ana" class="wikilink">Ana</a>(broken</p>`},
		{"[[Ana] text](u)", `<p><a href="u">[Ana] text</a></p>`},
		{"`[[Ana]]` [#run](javascript:bad)", `<p><code>[[Ana]]</code> <a href="">#run</a></p>`},
		{"[[Ana]]  \n#run", "<p><a href=\"/pages?title=Ana\" class=\"wikilink\">Ana</a><br>\n<a href=\"/pages?title=run\" class=\"wikilink\">#run</a></p>"},
		// After a hard line break, goldmark can put the backslash of an escape and the next character in two text
		// nodes. The renderer decodes the joined run, so "\[" is "[": a decode of each node shows "\[" (the first case).
		{"a\\  \n\\[x", "<p>a\\<br>\n[x</p>"},
		{"a\\  \n\\[[Ana]]", "<p>a\\<br>\n<a href=\"/pages?title=Ana\" class=\"wikilink\">Ana</a></p>"},
	} {
		if got := strings.TrimSpace(string(markdown(c.in))); got != c.want {
			t.Errorf("%q: got %s; want %s", c.in, got, c.want)
		}
	}
}

func TestMarkdownPostParseNFC(t *testing.T) {
	for _, n := range []int{100, 118, 119} {
		title := strings.Repeat("é", n) + ".jpg"
		for _, spelling := range []string{title, strings.Repeat("e\u0301", n) + ".jpg"} {
			body := "![[" + spelling + "|&lt;label&gt;&amp;]]"
			got := string(markdown(body))
			keys, _, _ := text.Targets(body, "")
			want := len(title) <= 240
			if strings.Contains(got, `class="wikilink embed"`) != want || text.HasEmbed(body, title) != want || (len(keys) == 1) != want {
				t.Errorf("n=%d: %s", n, got)
			}
			if want && (!strings.Contains(got, `/previews?title=`+strings.TrimPrefix(titleHref(title), "/pages?title=")) || !strings.Contains(got, `alt="&lt;label&gt;&amp;"`)) {
				t.Errorf("target/escaping: %s", got)
			}
		}
	}
	tag := strings.Repeat("e\u0301", 100)
	if got := string(markdown("#" + tag)); !strings.Contains(got, `class="wikilink"`) {
		t.Errorf("tag: %s", got)
	}
	if got := string(markdown("\u1fef![[Cafe\u0301.jpg]]\u1fef")); !strings.Contains(got, `class="wikilink embed"`) || strings.Contains(got, "<code>") {
		t.Errorf("post-parse: %s", got)
	}
}

// TestALineOfManyTagsIsDrawnWithWorkInProportionToIt: the renderer reads a tag at each '#' of a line. A read to the
// end of the line at each '#' makes the work grow with the square of the number of tags. The bytes that the drawing
// allocates count the work. Each tag is checked as a title (text.ValidTitle) and becomes a link: that is about 20 KB.
// The bound is 64 KB for each tag.
func TestALineOfManyTagsIsDrawnWithWorkInProportionToIt(t *testing.T) {
	if raceDetector {
		t.Skip("the race detector makes every allocation larger, so the bytes do not measure the work")
	}
	const n = 10000
	line := strings.Repeat("#a ", n)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := string(markdown(line))
	runtime.ReadMemStats(&after)
	if links := strings.Count(got, `<a href="/pages?title=a" class="wikilink">#a</a>`); links != n {
		t.Fatalf("a line of %d tags draws %d links to a tag", n, links)
	}
	if allocated, limit := after.TotalAlloc-before.TotalAlloc, uint64(n*64<<10); allocated > limit {
		t.Errorf("a line of %d tags allocated %d bytes (%d for each tag), want at most %d", n, allocated, allocated/n, limit)
	}
}

// TestMarkShowsTheMatchedWords says a search snippet marks what matched, and that the snippet's own text can
// never become markup.
func TestMarkShowsTheMatchedWords(t *testing.T) {
	for src, want := range map[string]string{
		"…ran ⟦5k⟧ with…":                  "…ran <mark>5k</mark> with…",
		"<b>⟦x⟧</b>":                       "&lt;b&gt;<mark>x</mark>&lt;/b&gt;",
		"<script>alert(1)</script>":        "&lt;script&gt;alert(1)&lt;/script&gt;",
		"no match here":                    "no match here",
		"⟦⟧":                               "⟦⟧",
		"the [[⟦Garden⟧ project]] - [x] a": "the <mark>Garden</mark> project - [x] a",
	} {
		if got := string(mark(src)); got != want {
			t.Errorf("mark(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestASnippetIsPlainText says a search snippet shows the words of a body without its CommonMark: the markers of a
// heading, a quote, a list item and a task box at the start of a line, the brackets of a wikilink and of an embed, and
// the marks of emphasis and code are cut, a link shows its label, and the lines are joined with a space; a #tag keeps
// its sign, and a mark keeps its place.
func TestASnippetIsPlainText(t *testing.T) {
	for src, want := range map[string]string{
		"Raised beds.\n\n- [x] Order cedar boards\n- [ ] Fill with ⟦soil⟧": "Raised beds. Order cedar boards Fill with <mark>soil</mark>",
		"Coffee with [[Ana]]; the [[⟦Garden⟧ project]].":                   "Coffee with Ana; the <mark>Garden</mark> project.",
		"## A ⟦heading⟧\n> a quote\n* one\n+ two":                          "A <mark>heading</mark> a quote one two",
		"#running at the lake\n![[sunset.png]]":                            "#running at the lake sunset.png",
		"a [link](x) and [x] in a line":                                    "a link and [x] in a line",
		"Read *⟦Piranesi⟧*, **well** `done`, _calm_ and file_name.txt":     "Read <mark>Piranesi</mark>, well done, calm and file_name.txt",
	} {
		if got := string(mark(src)); got != want {
			t.Errorf("mark(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestMarkdownCoverLeavesOutTheCoverOnce: the cover is drawn above the body, so the first embed that names it (by any
// spelling of its title key) is left out, with its paragraph when nothing else is in it. Another embed of the same
// file, the other embeds and the words stay. An empty embed leaves out nothing.
func TestMarkdownCoverLeavesOutTheCoverOnce(t *testing.T) {
	img := func(title string) string { return `<img src="/previews?title=` + title + `"` }
	for _, c := range []struct {
		name, body, embed string
		want              []string // each with the number of times it is drawn
		count             []int
	}{
		{"first, alone in its paragraph", "![[Lake.jpg]]\n\nWords.", "Lake.jpg",
			[]string{img("Lake.jpg"), "<p></p>", "<p>Words.</p>"}, []int{0, 0, 1}},
		{"a later embed of the same file stays", "![[Lake.jpg]]\n\nWords ![[Lake.jpg]]", "Lake.jpg",
			[]string{img("Lake.jpg"), "<p>Words"}, []int{1, 1}},
		{"in a line of words", "Before ![[Sea.jpg]] then ![[lake.JPG]] after.", "Lake.jpg",
			[]string{img("Sea.jpg"), img("lake.JPG"), "after."}, []int{1, 0, 1}},
		{"no cover", "![[Lake.jpg]]", "",
			[]string{img("Lake.jpg")}, []int{1}},
		{"an embed inside a link is not one", "[![[Lake.jpg]]](https://example.org) ![[Lake.jpg]]", "Lake.jpg",
			[]string{img("Lake.jpg"), "example.org"}, []int{0, 1}},
	} {
		got := string(markdownCover(c.body, c.embed))
		for i, w := range c.want {
			if n := strings.Count(got, w); n != c.count[i] {
				t.Errorf("%s: %q drawn %d times, want %d, in %s", c.name, w, n, c.count[i], got)
			}
		}
	}
}
