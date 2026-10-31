// Package text computes what every writer must compute identically: the title predicate, title_key, and the
// wikilinks and #tags a body names. The rules and their vectors live in docs/contract/titles-and-wikilinks.md;
// this package implements them and states none of its own. It also holds the plain forms that the writer and its
// view both read (plain.go): a day and an instant that round-trip, and the marks of a line and of a snippet.
package text

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/unicode/rangetable"
)

// AssignedVersion is the Unicode version whose unassigned code points (Cn) a title may not contain. It is pinned,
// not the newer tables Go ships, so the titles this writer accepts do not change with a Go upgrade.
const AssignedVersion = "15.0.0"

var (
	assigned = rangetable.Assigned(AssignedVersion)
	fold     = cases.Fold()
	md       = goldmark.New() // CommonMark, no extensions
	wiki     = regexp.MustCompile(`\[\[([^\[\]\n\r]*)\]\]`)
	devices  = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true}
	invisible = map[rune]bool{0xAD: true, 0x61C: true, 0x200B: true, 0x200E: true, 0x200F: true, 0xFEFF: true}
)

func init() {
	for i := '1'; i <= '9'; i++ {
		devices["COM"+string(i)] = true
		devices["LPT"+string(i)] = true
	}
	for r := rune(0x202A); r <= 0x202E; r++ {
		invisible[r] = true
	}
	for r := rune(0x2060); r <= 0x2069; r++ {
		invisible[r] = true
	}
}

// TitleKey is title_key = NFC(casefold(NFC(title))).
func TitleKey(t string) string {
	return norm.NFC.String(fold.String(norm.NFC.String(t)))
}

// ValidTitle implements the reference-name predicate in the titles-and-wikilinks contract.
// The extraction check uses Candidates, not Targets, to avoid recursive validation.
func ValidTitle(t string) bool {
	if !validFilename(t) || strings.ContainsAny(t, "[]") {
		return false
	}
	key := TitleKey(t)
	for _, spelling := range []string{t, norm.NFC.String(t)} {
		for _, body := range []string{"See [[" + spelling + "]].", "See ![[" + spelling + "]].", "See [[" + spelling + "|display]]."} {
			got := Candidates(body)
			if len(got) != 1 || !validFilename(got[0]) || strings.ContainsAny(got[0], "[]") || TitleKey(got[0]) != key {
				return false
			}
		}
	}
	return true
}

func validFilename(t string) bool {
	if t == "" || t != strings.Trim(t, " ") || len(t) > 240 {
		return false
	}
	if strings.ContainsAny(t, `/\:*?"<>|`) {
		return false
	}
	for _, r := range t {
		if r <= 31 || (r >= 127 && r <= 159) || invisible[r] || !unicode.Is(assigned, r) {
			return false
		}
	}
	if t[0] == '.' || t[len(t)-1] == '.' {
		return false
	}
	base, _, _ := strings.Cut(t, ".")
	return !devices[asciiUpper(base)]
}

func asciiUpper(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 32
		}
		return r
	}, s)
}

// Candidates returns the titles a body names, in order of appearance, valid or not and not de-duplicated.
func Candidates(body string) []string {
	var out []string
	for _, m := range mentions(body) {
		out = append(out, m.title)
	}
	return out
}

// mention is a title that a body names, and the words a reader sees for it: the text of a wikilink after any |, or a
// #tag with its #.
type mention struct{ title, shown string }

// mentions is Candidates with the words that show each title.
func mentions(body string) []mention {
	var out []mention
	for _, run := range textRuns(body, true) {
		links := wiki.FindAllStringSubmatchIndex(run, -1)
		// One walk over the run blanks the wikilinks, so that their text is not read again for tags. It also finds
		// the position of each wikilink in characters, which is the unit of a tag's position.
		blanked := make([]rune, 0, utf8.RuneCountInString(run))
		linkAt := make([]int, len(links))
		k := 0
		for b, r := range run {
			for k < len(links) && b >= links[k][1] {
				k++
			}
			if k < len(links) && b >= links[k][0] {
				if b == links[k][0] {
					linkAt[k] = len(blanked)
				}
				r = ' '
			}
			blanked = append(blanked, r)
		}
		// The wikilinks and the tags are each in order of position: a merge puts them in one order.
		k = 0
		for _, t := range scanTags(blanked) {
			if !namesPage(t.tag) {
				continue
			}
			for ; k < len(links) && linkAt[k] < t.pos; k++ {
				out = append(out, linkMention(run, links[k]))
			}
			out = append(out, mention{t.tag, "#" + t.tag})
		}
		for ; k < len(links); k++ {
			out = append(out, linkMention(run, links[k]))
		}
	}
	return out
}

// linkMention is the mention of the wikilink at m, a match of wiki in run.
func linkMention(run string, m []int) mention {
	return mention{linkTitle(run, m), wikiShown(run[m[2]:m[3]])}
}

// linkTitle is the title of the wikilink at m, a match of wiki in run: the text before a |, without the spaces at
// its ends.
func linkTitle(run string, m []int) string {
	title, _, _ := strings.Cut(run[m[2]:m[3]], "|")
	return strings.Trim(title, " ")
}

// Targets returns the distinct valid targets as (key, first valid spelling) pairs in order, skipping ownKey,
// and the rejected titles.
func Targets(body, ownKey string) (keys, titles, rejected []string) {
	seen := map[string]bool{}
	for _, t := range Candidates(body) {
		if !ValidTitle(t) {
			rejected = append(rejected, t)
			continue
		}
		k := TitleKey(t)
		if k == ownKey || seen[k] {
			continue
		}
		seen[k] = true
		keys, titles = append(keys, k), append(titles, t)
	}
	return
}

// HasEmbed reports a rendered wiki embed naming title, using CommonMark's inline precedence.
func HasEmbed(body, title string) bool {
	key := TitleKey(title)
	for _, target := range EmbeddedTitles(body) {
		if TitleKey(target) == key {
			return true
		}
	}
	return false
}

// EmbeddedTitles returns valid rendered embed names before identity resolution. The text of a link is not read: a
// reader draws no embed inside a link, as HTML puts no link inside a link.
func EmbeddedTitles(body string) []string {
	var titles []string
	for _, run := range textRuns(body, false) {
		for _, m := range wiki.FindAllStringSubmatchIndex(run, -1) {
			if m[0] == 0 || run[m[0]-1] != '!' {
				continue
			}
			target, _, _ := strings.Cut(run[m[2]:m[3]], "|")
			target = strings.Trim(target, " ")
			if ValidTitle(target) {
				titles = append(titles, target)
			}
		}
	}
	return titles
}

// TagAt reads the #tag that starts s, where prev is the character before it ('\n' at the start of a line), by the
// rules Candidates applies; n is its length in bytes, '#' included, and 0 when no tag naming a page starts there.
func TagAt(prev rune, s string) (tag string, n int) {
	if !strings.HasPrefix(s, "#") {
		return "", 0
	}
	// A tag has only tag characters and '-', so the scan reads s only to the first other character after the '#'. A
	// renderer calls TagAt at each '#' of a line: a scan to the end of the line makes the work grow with the square
	// of the number of tags.
	end := len(s)
	if i := strings.IndexFunc(s[1:], func(r rune) bool { return !tagChar(r) && r != '-' }); i >= 0 {
		end = 1 + i
	}
	for _, t := range scanTags(append([]rune{prev}, []rune(s[:end])...)) {
		if t.pos == 1 && namesPage(t.tag) {
			return t.tag, 1 + len(t.tag)
		}
	}
	return "", 0
}

// namesPage reports a scanned #tag that names a page: not all decimal digits.
func namesPage(tag string) bool {
	return !allDigits(tag)
}

func tagChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || r == '_'
}

type tag struct {
	pos int
	tag string
}

func scanTags(run []rune) []tag {
	var out []tag
	n := len(run)
	for i := 0; i < n; {
		if run[i] == '#' && (i == 0 || !(tagChar(run[i-1]) || run[i-1] == '/' || run[i-1] == '#')) {
			j := i + 1
			for {
				k := j
				for k < n && tagChar(run[k]) {
					k++
				}
				j = k
				if j+1 < n && run[j] == '-' && tagChar(run[j+1]) && k > i+1 {
					j++
					continue
				}
				break
			}
			if j > i+1 {
				out = append(out, tag{i, string(run[i+1 : j])})
			}
			i = max(j, i+1)
		} else {
			i++
		}
	}
	return out
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Nd, r) {
			return false
		}
	}
	return true
}

// textRuns returns the CommonMark text of a body as runs of plain text, each NFC-normalised. A run is a maximal
// sequence of adjacent text nodes inside one inline container, broken at line breaks and at any other inline
// (emphasis, code span, link, raw HTML). Code spans, raw HTML, link destinations and image alt text are not read.
// The text of a link is read only with linkText: a wikilink in the text of a link is a link of the graph (the
// contract's vector `[see [[Diet]]](http://x)`), and an embed there is not drawn (EmbeddedTitles).
func textRuns(body string, linkText bool) []string {
	src := []byte(body)
	doc := md.Parser().Parse(gtext.NewReader(src))
	var runs []string
	var cur bytes.Buffer
	open := false
	flush := func() {
		if open {
			runs = append(runs, norm.NFC.String(cur.String()))
			cur.Reset()
			open = false
		}
	}
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				cur.Write(decode(c, src))
				open = true
				if c.SoftLineBreak() || c.HardLineBreak() {
					flush()
				}
			case *ast.String:
				cur.Write(c.Value)
				open = true
			case *ast.CodeSpan, *ast.Image, *ast.RawHTML, *ast.AutoLink,
				*ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
				flush()
			case *ast.Link:
				flush()
				if linkText {
					walk(c)
					flush()
				}
			default:
				flush()
				walk(c)
				flush()
			}
		}
	}
	walk(doc)
	flush()
	return runs
}

// decode is the text a text node stands for: backslash escapes and character references resolved.
func decode(t *ast.Text, src []byte) []byte {
	v := t.Segment.Value(src)
	if t.IsRaw() {
		return v
	}
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
}
