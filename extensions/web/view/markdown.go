package view

import (
	"bytes"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/text/unicode/norm"

	"lifelog/text"
)

// CommonMark owns links and images before eligible plain text is enhanced. The links panel,
// not this rendering, is what the save contract wrote.
var bodyMD = goldmark.New()

func markdown(src string) template.HTML { return markdownCover(src, "") }

// markdownCover is markdown with the page's cover drawn apart, above the body (docs/plans/127-a-cover-for-every-page.md):
// the first embed that names the cover (embed, its title as the body writes it) is left out, and so is its paragraph
// when nothing else is in it. An empty embed leaves out nothing.
func markdownCover(src, embed string) template.HTML {
	var b bytes.Buffer
	source := []byte(src)
	doc := bodyMD.Parser().Parse(gtext.NewReader(source))
	enhanceText(doc, source)
	if embed != "" {
		leaveOutEmbed(doc, source, text.TitleKey(embed))
	}
	if err := bodyMD.Renderer().Render(&b, source, doc); err != nil {
		return template.HTML("<pre>" + template.HTMLEscapeString(src) + "</pre>")
	}
	return template.HTML(b.String())
}

// leaveOutEmbed removes the first embed whose title has the key, in the order of the text, and its paragraph when
// nothing but blanks and line breaks is left in it.
func leaveOutEmbed(doc ast.Node, source []byte, key string) {
	var found ast.Node
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found != nil {
			return ast.WalkContinue, nil
		}
		if l, ok := n.(*ast.Link); ok {
			if c, ok := l.Attribute([]byte("class")); ok && string(c.([]byte)) == "wikilink embed" {
				if title, ok := embedTitle(l); ok && text.TitleKey(title) == key {
					found = l
					return ast.WalkStop, nil
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if found == nil {
		return
	}
	para := found.Parent()
	para.RemoveChild(para, found)
	if _, ok := para.(*ast.Paragraph); !ok {
		return
	}
	for c := para.FirstChild(); c != nil; c = c.NextSibling() {
		if s, ok := c.(*ast.String); ok && strings.TrimSpace(string(s.Value)) == "" {
			continue
		}
		if t, ok := c.(*ast.Text); ok && strings.TrimSpace(string(t.Segment.Value(source))) == "" {
			continue
		}
		return
	}
	para.Parent().RemoveChild(para.Parent(), para)
}

// embedTitle is the title of the file an embed link of embedLink draws, read back from its destination.
func embedTitle(l *ast.Link) (string, bool) {
	title, ok := strings.CutPrefix(string(l.Destination), "/pages?title=")
	if !ok {
		return "", false
	}
	title, err := url.QueryUnescape(title)
	return title, err == nil
}

// titleHref opens the page a title names, through GET /pages?title= (any case or normalisation).
func titleHref(title string) string { return "/pages?title=" + url.QueryEscape(title) }

func pageLink(title, label string) ast.Node {
	l := ast.NewLink()
	l.Destination = []byte(titleHref(title))
	l.SetAttributeString("class", []byte("wikilink"))
	s := ast.NewString([]byte(label))
	s.SetRaw(true) // escaped when written, never re-read as markdown
	l.AppendChild(l, s)
	return l
}

// embedLink is ![[title]]: the picture of the file page the title names, linking the page; a title with no picture
// shows its alt text, the label.
func embedLink(title, label string) ast.Node {
	img := ast.NewImage(ast.NewLink())
	img.Destination = []byte("/previews?title=" + url.QueryEscape(title))
	img.SetAttributeString("loading", []byte("lazy"))
	alt := ast.NewString([]byte(label))
	alt.SetRaw(true)
	img.AppendChild(img, alt)
	l := ast.NewLink()
	l.Destination = []byte(titleHref(title))
	l.SetAttributeString("class", []byte("wikilink embed"))
	l.AppendChild(l, img)
	return l
}

var wikiAtStart = regexp.MustCompile(`^(!?)\[\[([^\[\]\n\r]*)\]\]`)

// enhanceText joins adjacent text segments before decoding, but never crosses an inline
// container or line break. Existing anchors, images, code and HTML are left to CommonMark.
// It decodes the joined run, not each node as text does: after a hard line break, goldmark can put the
// backslash of an escape and the next character in two nodes, and only the joined run decodes that escape.
func enhanceText(parent ast.Node, source []byte) {
	for c := parent.FirstChild(); c != nil; {
		switch node := c.(type) {
		case *ast.Link, *ast.Image, *ast.AutoLink, *ast.CodeSpan, *ast.RawHTML,
			*ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
			c = c.NextSibling()
		case *ast.Text:
			var raw bytes.Buffer
			first := c
			var last *ast.Text
			for t := node; t != nil; t, _ = c.(*ast.Text) {
				raw.Write(t.Segment.Value(source))
				last = t
				c = c.NextSibling()
				if t.SoftLineBreak() || t.HardLineBreak() {
					break
				}
			}
			decoded := util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(raw.Bytes())))
			for _, node := range enhancedRun(norm.NFC.String(string(decoded))) {
				parent.InsertBefore(parent, first, node)
			}
			if last.SoftLineBreak() || last.HardLineBreak() {
				br := ast.NewTextSegment(gtext.NewSegment(last.Segment.Stop, last.Segment.Stop))
				br.SetSoftLineBreak(last.SoftLineBreak())
				br.SetHardLineBreak(last.HardLineBreak())
				parent.InsertBefore(parent, first, br)
			}
			for first != c {
				next := first.NextSibling()
				parent.RemoveChild(parent, first)
				first = next
			}
		default:
			enhanceText(c, source)
			c = c.NextSibling()
		}
	}
}

func enhancedRun(run string) []ast.Node {
	var nodes []ast.Node
	start := 0
	plain := func(end int) {
		if end > start {
			s := ast.NewString([]byte(run[start:end]))
			s.SetRaw(true)
			nodes = append(nodes, s)
		}
	}
	for i := 0; i < len(run); {
		var node ast.Node
		n := 0
		if run[i] == '[' || run[i] == '!' {
			if m := wikiAtStart.FindStringSubmatch(run[i:]); m != nil {
				title, label, piped := strings.Cut(m[2], "|")
				title = strings.Trim(title, " ")
				if text.ValidTitle(title) {
					if !piped || strings.TrimSpace(label) == "" {
						label = title
					}
					if m[1] != "" {
						node = embedLink(title, label)
					} else {
						node = pageLink(title, label)
					}
					n = len(m[0])
				}
			}
		} else if run[i] == '#' {
			prev := rune(10)
			if i > 0 {
				prev, _ = utf8.DecodeLastRuneInString(run[:i])
			}
			tag, size := text.TagAt(prev, run[i:])
			if size > 0 && text.ValidTitle(tag) {
				node = pageLink(tag, "#"+tag)
				n = size
			}
		}
		if node != nil {
			plain(i)
			nodes = append(nodes, node)
			i += n
			start = i
		} else {
			_, size := utf8.DecodeRuneInString(run[i:])
			i += size
		}
	}
	plain(len(run))
	return nodes
}

// mark is a search snippet as plain text with its matched words marked: the search puts text.SnippetOpen and
// text.SnippetClose around them. The text is escaped first, so a snippet can never carry HTML of its own.
func mark(snippet string) template.HTML {
	return template.HTML(markRE.ReplaceAllString(template.HTMLEscapeString(plainSnippet(snippet)), "<mark>$1</mark>"))
}

// plainSnippet is the words of a snippet without its CommonMark, on one line: the marker of a heading, a quote, a
// list item or a task box at the start of a line is cut, the brackets of a wikilink or an embed, and the inline marks
// (text.CutInlineMarks; a snippet is a fragment, so goldmark does not read it). It is for a list of hits, not a
// rendering; the JSON keeps the text as it is.
func plainSnippet(s string) string {
	var words []string
	for line := range strings.SplitSeq(s, "\n") {
		line = text.BlockMark.ReplaceAllString(strings.TrimSpace(line), "")
		if line != "" {
			words = append(words, line)
		}
	}
	return strings.NewReplacer("![[", "", "[[", "", "]]", "").Replace(text.CutInlineMarks(strings.Join(words, " ")))
}

var markRE = regexp.MustCompile(text.SnippetOpen + `([^` + text.SnippetOpen + text.SnippetClose + `]+)` + text.SnippetClose)
