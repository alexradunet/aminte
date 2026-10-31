package text

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The vectors are read from the contract page itself, the language-neutral home of the rules.
const contract = "../docs/contract/titles-and-wikilinks.md"

// cells splits a markdown table row on the | that are not escaped as \|.
func cells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	var out []string
	var cur strings.Builder
	for i := 0; i < len(row); i++ {
		if row[i] == '\\' && i+1 < len(row) && row[i+1] == '|' {
			cur.WriteString(`\|`)
			i++
			continue
		}
		if row[i] == '|' {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(row[i])
	}
	return append(out, strings.TrimSpace(cur.String()))
}

// spans returns the code spans of a cell, with the page's escapes (\n \r \t \uXXXX \|) applied.
func spans(cell string) []string {
	var out []string
	for i := 0; i < len(cell); {
		if cell[i] != '`' {
			i++
			continue
		}
		ticks := 1
		for i+ticks < len(cell) && cell[i+ticks] == '`' {
			ticks++
		}
		fence := strings.Repeat("`", ticks)
		end := strings.Index(cell[i+ticks:], fence)
		s := cell[i+ticks : i+ticks+end]
		if ticks > 1 {
			s = strings.TrimPrefix(strings.TrimSuffix(s, " "), " ")
		}
		out = append(out, unescape(s))
		i += 2*ticks + end
	}
	return out
}

var uEsc = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)

func unescape(s string) string {
	s = strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t", `\|`, "|").Replace(s)
	return uEsc.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.ParseUint(m[2:], 16, 32)
		return string(rune(n))
	})
}

func tables(t *testing.T) (links, keys [][]string) {
	b, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	var cur *[][]string
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "| body |"):
			cur = &links
		case strings.HasPrefix(line, "| title | `title_key` |"):
			cur = &keys
		case strings.HasPrefix(line, "|---"):
		case strings.HasPrefix(line, "|") && cur != nil:
			*cur = append(*cur, cells(line))
		default:
			cur = nil
		}
	}
	if len(links) < 60 || len(keys) < 7 {
		t.Fatalf("vector tables not found: %d link rows, %d key rows", len(links), len(keys))
	}
	return
}

func TestWikilinkVectors(t *testing.T) {
	links, _ := tables(t)
	for _, row := range links {
		body := spans(row[0])[0]
		want := spans(row[1]) // "—" has no spans: no links
		_, got, _ := Targets(body, "")
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("body %q: got %q, want %q", body, got, want)
		}
	}
}

func TestLengthVectors(t *testing.T) {
	for _, c := range []struct {
		title string
		ok    bool
	}{
		{strings.Repeat("a", 240), true}, {strings.Repeat("a", 241), false},
		{strings.Repeat("日", 80), true}, {strings.Repeat("日", 81), false},
	} {
		_, got, _ := Targets("[["+c.title+"]]", "")
		if (len(got) == 1) != c.ok {
			t.Errorf("%d bytes: linked=%v, want %v", len(c.title), len(got) == 1, c.ok)
		}
	}
	esz, ss := strings.Repeat("ẞ", 81), strings.Repeat("ss", 81)
	if _, got, _ := Targets("[["+esz+"]] [["+ss+"]]", ""); len(got) != 1 || got[0] != ss {
		t.Errorf("ẞ×81 then ss×81: got %q, want the ss spelling", got)
	}
}

func TestTitleKeyVectors(t *testing.T) {
	_, keys := tables(t)
	for _, row := range keys {
		in, out := spans(row[0]), spans(row[1])
		for i, title := range in {
			want := out[0]
			if len(out) == len(in) {
				want = out[i]
			}
			if got := TitleKey(title); got != want {
				t.Errorf("TitleKey(%q) = %q, want %q", title, got, want)
			}
		}
	}
}

func TestUnassignedRejected(t *testing.T) {
	if ValidTitle("a\U000E0080b") || ValidTitle("x͸") {
		t.Error("a title with an unassigned code point was accepted")
	}
	page, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	if v := strings.TrimSuffix(AssignedVersion, ".0"); !strings.Contains(string(page), "**Unicode "+v+"**") {
		t.Errorf("the contract's Cn rule does not name Unicode %s, the version this writer pins", v)
	}
	if ValidTitle("a\U0001FAE9") || !ValidTitle("a\U0001FAE8") {
		t.Error("the Cn rule does not follow Unicode 15.0: U+1FAE9 (16.0) must be refused, U+1FAE8 (15.0) accepted")
	}
	if !ValidTitle("Lakeside") || !ValidTitle("2026-09-29") {
		t.Error("an ordinary title was rejected")
	}
}

// TagAt, read at each '#', finds the tags of a line. The lists are written by hand from the tag rules: a tag follows
// the start of a line or a character that is no letter, digit or '#'; it has tag characters and '-'; a trailing '-'
// is not part of it; digits alone name no page.
func TestTagAtFindsTheTagsOfALine(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"#run", []string{"run"}},
		{"a#b", nil},
		{"x #run-club! and #ünï", []string{"run-club", "ünï"}},
		{"#123 #2026a", []string{"2026a"}},
		{"#REDIRECT", []string{"REDIRECT"}},
		{"##x", nil},
		{"/#x", nil},
		{"(#x) #a_b-", []string{"x", "a_b"}},
	} {
		var got []string
		prev := '\n'
		for i, r := range c.in {
			if r == '#' {
				if tag, n := TagAt(prev, c.in[i:]); n > 0 {
					got = append(got, tag)
				}
			}
			prev = r
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: TagAt finds %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCandidatesAllocatesAFewBytesForEachByteOfALongRun: a save takes a body of up to 20 MiB, and a body with no
// break is one run of text. Candidates holds the run as characters (4 bytes each), and the parse copies the text a
// few times. No other value can grow with each character of the run: the bound is 16 bytes for each byte of the body.
func TestCandidatesAllocatesAFewBytesForEachByteOfALongRun(t *testing.T) {
	const half = 2 << 20
	body := "#start " + strings.Repeat("x", half) + " #middle " + strings.Repeat("y", half) + " #end"
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := Candidates(body)
	runtime.ReadMemStats(&after)
	if want := []string{"start", "middle", "end"}; !slices.Equal(got, want) {
		t.Fatalf("Candidates of the long run: %q, want %q", got, want)
	}
	if n, limit := after.TotalAlloc-before.TotalAlloc, uint64(16*len(body)); n > limit {
		t.Errorf("Candidates of a run of %d bytes allocated %d bytes (%.1f for each byte), want at most %d", len(body), n, float64(n)/float64(len(body)), limit)
	}
}

// TestCandidatesKeepsTheOrderOfManyWikilinksAndTagsInOneRun: the wikilinks and the tags of a run come out in the
// order of the text. The titles have characters of two and three bytes, so a position in bytes and a position in
// characters are not the same. A tag can touch a wikilink on each side. A tag of digits and a # just before a
// wikilink name no page. A # in a wikilink is a part of its title.
func TestCandidatesKeepsTheOrderOfManyWikilinksAndTagsInOneRun(t *testing.T) {
	var body strings.Builder
	var want []string
	for i := range 100 {
		fmt.Fprintf(&body, "[[Lé %d|日本]] #tü%d #%d [[Ök #%d]]#ä%d[[Ü%d]] #[[Ö%d]] ", i, i, i, i, i, i, i)
		want = append(want, fmt.Sprint("Lé ", i), fmt.Sprint("tü", i), fmt.Sprint("Ök #", i), fmt.Sprint("ä", i),
			fmt.Sprint("Ü", i), fmt.Sprint("Ö", i))
	}
	if got := Candidates(body.String()); !slices.Equal(got, want) {
		t.Errorf("Candidates of one run of %d wikilinks and tags:\n got %q\nwant %q", len(want), got, want)
	}
}

func TestPhotoEmbedContexts(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{"![[PHOTO.jpg|alias]]", true}, {"`![[Photo.jpg]]`", false},
		{"~~~\n![[Photo.jpg]]\n~~~", false}, {"[![[Photo.jpg]]](url)", false},
		{"![![[Photo.jpg]]](url)", false}, {"[[Photo.jpg]]", false},
		{"![[Photo.jpg]]", true}, {"![[Lake [1].jpg]]", false},
		{"a\\  \n\\![[Photo.jpg]]", true}, // an escaped '!' is a '!': the embed after a hard line break
	} {
		title := "Photo.jpg"
		if strings.Contains(tc.body, "Lake") {
			title = "Lake [1].jpg"
		}
		if got := HasEmbed(tc.body, title); got != tc.want {
			t.Errorf("%q: %v", tc.body, got)
		}
	}
}

// TestTheTextOfALinkIsReadForWikilinksOnly: one walk reads the text of a body, and its parameter says whether it
// reads the text of a Markdown link. A wikilink there is a link of the graph (the contract's vector
// `[see [[Diet]]](http://x)`). An embed there is not drawn, because HTML puts no link inside a link, so it is not
// an embed.
func TestTheTextOfALinkIsReadForWikilinksOnly(t *testing.T) {
	body := "[see ![[Photo.jpg]]](http://x) and ![[Lake.jpg]]"
	for _, tc := range []struct {
		name      string
		got, want []string
	}{
		{"the runs with the text of a link", textRuns(body, true), []string{"see ![[Photo.jpg]]", " and ![[Lake.jpg]]"}},
		{"the runs without it", textRuns(body, false), []string{" and ![[Lake.jpg]]"}},
		{"the wikilinks", Candidates(body), []string{"Photo.jpg", "Lake.jpg"}},
		{"the embeds", EmbeddedTitles(body), []string{"Lake.jpg"}},
	} {
		if !slices.Equal(tc.got, tc.want) {
			t.Errorf("%s of %q: %q, want %q", tc.name, body, tc.got, tc.want)
		}
	}
}
