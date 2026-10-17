package markdown

import (
	"bytes"
	"html"
	"html/template"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// blockRefs turns on block references: a block's ^id becomes its HTML id,
// [[note#^id]] scrolls to it and ![[note#^id]] embeds only that block. While
// off, markers are still hidden, block links land on the note and block
// embeds show the whole note. A var only so tests can flip it.
var blockRefs = false

// The subpath rules below follow Obsidian's reading view (1.14.4); see
// .claude/specs/obsidian-flavored-markdown-spec.md, "Behavior rules".

// splitSubpath splits the text after a link's first # into its parts.
func splitSubpath(anchor string) []string {
	var parts []string
	for _, p := range strings.Split(anchor, "#") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

var headingPunct = regexp.MustCompile("[!\"#$%&()*+,.:;<=>?@^`{|}~/\\[\\]\\\\\r\n]")

// normalizeHeading is how a link's heading path and a heading's text are
// compared: punctuation becomes space, whitespace collapses, case folds.
func normalizeHeading(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(headingPunct.ReplaceAllString(s, " ")), " "))
}

// Heading is one heading of a note, as Headings reports it.
type Heading struct {
	Level int
	Text  string // source text, as written after the #s
}

// Headings lists md's headings in order. Lines inside code fences are not
// headings.
func Headings(md string) []Heading {
	_, body := SplitFrontmatter(md)
	src := []byte(stripComments(body))
	doc := goldmark.New(goldmark.WithExtensions(staticExts...)).Parser().Parse(text.NewReader(src))
	var out []Heading
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			out = append(out, Heading{Level: h.Level, Text: strings.TrimSpace(string(h.Lines().Value(src)))})
		}
		return ast.WalkContinue, nil
	})
	return out
}

// NormalizeHeading is the comparison form of a heading or of one part of a
// link's heading path.
func NormalizeHeading(s string) string { return normalizeHeading(s) }

var subpathUnsafe = regexp.MustCompile(`[:#|^\\\r\n]|%%|\[\[|\]\]`)

// HeadingSubpath is heading text made safe to write inside [[note#...]]:
// characters that would end or split the link become spaces. It still
// matches its heading, since matching ignores them too.
func HeadingSubpath(s string) string {
	return strings.Join(strings.Fields(subpathUnsafe.ReplaceAllString(s, " ")), " ")
}

// fragment is the URL fragment a link's anchor scrolls to: the last heading
// of a heading path, or the block id while block refs are on.
func fragment(anchor string) string {
	parts := splitSubpath(anchor)
	if len(parts) == 0 {
		return ""
	}
	if last := parts[len(parts)-1]; strings.HasPrefix(last, "^") {
		if blockRefs && len(parts) == 1 {
			return last
		}
		return ""
	}
	return headingAnchor(parts[len(parts)-1])
}

// Section returns the part of md that anchor (a link's text after #) points
// at: a heading's section, or a block while block refs are on. An empty
// anchor, or a block anchor while block refs are off, returns md whole.
// ok is false when the heading or block does not exist.
func Section(md, anchor string) (string, bool) {
	parts := splitSubpath(anchor)
	if len(parts) == 0 {
		return md, true
	}
	isBlock := len(parts) == 1 && strings.HasPrefix(parts[0], "^")
	if isBlock && !blockRefs {
		return md, true
	}
	_, body := SplitFrontmatter(md)
	src := []byte(stripComments(body))
	doc := goldmark.New(goldmark.WithExtensions(staticExts...)).Parser().Parse(text.NewReader(src))

	if isBlock {
		want := strings.ToLower(parts[0])
		var hit ast.Node
		_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering && n.Kind() != ast.KindHeading {
				if v, ok := n.AttributeString("id"); ok {
					if b, ok := v.([]byte); ok && strings.ToLower(string(b)) == want {
						hit = n
						return ast.WalkStop, nil
					}
				}
			}
			return ast.WalkContinue, nil
		})
		if hit == nil {
			return "", false
		}
		start := lineStart(src, blockPos(hit))
		// A standalone marker naming hit was removed from the tree, so the
		// slice runs over it; it is the only thing that can follow hit there.
		slice := standaloneTail.ReplaceAllString(string(src[start:blockEnd(doc, src, hit, start)]), "")
		return dedent(slice), true
	}

	var headings []*ast.Heading
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			headings = append(headings, h)
		}
		return ast.WalkContinue, nil
	})
	var target, next *ast.Heading
	matched, level := 0, 0
	for _, h := range headings {
		if target != nil {
			if h.Level <= target.Level {
				next = h
				break
			}
			continue
		}
		if h.Level > level && normalizeHeading(string(h.Lines().Value(src))) == normalizeHeading(parts[matched]) {
			matched, level = matched+1, h.Level
			if matched == len(parts) {
				target = h
			}
		}
	}
	if target == nil {
		return "", false
	}
	start, end := lineStart(src, blockPos(target)), len(src)
	if next != nil {
		end = lineStart(src, blockPos(next))
	}
	return strings.TrimRight(string(src[start:end]), " \t\r\n") + "\n", true
}

// MissingSection is the embed body shown when an embed's #heading or #^block
// is not in the target note.
func MissingSection(anchor string) template.HTML {
	return template.HTML(`<p class="embed-missing">找不到段落 <code>#` + html.EscapeString(anchor) + `</code></p>`)
}

// blockPos is the source offset where n starts, or -1.
func blockPos(n ast.Node) int {
	if p := n.Pos(); p >= 0 {
		return p
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if p := blockPos(c); p >= 0 {
			return p
		}
	}
	return -1
}

func lineStart(src []byte, pos int) int {
	if pos < 0 {
		return 0
	}
	return bytes.LastIndexByte(src[:pos], '\n') + 1
}

// blockEnd is where n's source ends: the first block outside n that starts
// after it. Siblings alone are not enough — footnote definitions are moved to
// the end of the tree, out of source order.
func blockEnd(doc ast.Node, src []byte, n ast.Node, start int) int {
	end := len(src)
	_ = ast.Walk(doc, func(m ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || m.Type() != ast.TypeBlock {
			return ast.WalkContinue, nil
		}
		if m == n {
			return ast.WalkSkipChildren, nil
		}
		if p := blockPos(m); p >= 0 {
			if s := lineStart(src, p); s > start && s < end {
				end = s
			}
		}
		return ast.WalkContinue, nil
	})
	return end
}

// dedent strips the first line's indentation from every line, so a nested
// list item sliced out of its list does not turn into an indented code block.
func dedent(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	indent := s[:len(s)-len(strings.TrimLeft(s, " \t"))]
	if indent == "" {
		return s + "\n"
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimPrefix(ln, indent)
	}
	return strings.Join(lines, "\n") + "\n"
}

// ---------- block reference extension ^id ----------
// Strips ^id markers from reading view and, while blockRefs is on, puts the
// id on the block it names.

type blockRefExt struct{}

func (e *blockRefExt) Extend(m goldmark.Markdown) {
	// After callouts (90), so a callout keeps the id its blockquote carried.
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(&blockRefTransformer{}, 80),
	))
}

type blockRefTransformer struct{}

var standaloneTail = regexp.MustCompile(`\n[ \t]*\^[A-Za-z0-9-]+[ \t]*\s*$`)

var blockIDRe = regexp.MustCompile(`(?:^|[ \t]+)\^([A-Za-z0-9-]+)[ \t]*$`)

func (t *blockRefTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	src := reader.Source()
	var paras []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (n.Kind() == ast.KindParagraph || n.Kind() == ast.KindTextBlock) {
			paras = append(paras, n)
		}
		return ast.WalkContinue, nil
	})
	for _, p := range paras {
		lines := p.Lines()
		if lines.Len() == 0 {
			continue
		}
		last := lines.At(lines.Len() - 1)
		line := bytes.TrimRight(src[last.Start:last.Stop], "\r\n")
		m := blockIDRe.FindSubmatchIndex(line)
		if m == nil {
			continue
		}
		id := string(line[m[2]:m[3]])
		var owner ast.Node
		if lines.Len() == 1 && m[0] == 0 {
			// A paragraph that is only ^id names the block before it.
			if p.Parent() == ast.Node(doc) {
				owner = p.PreviousSibling()
			} else {
				owner = enclosingBlock(doc, p)
			}
			p.Parent().RemoveChild(p.Parent(), p)
		} else {
			if !cutInlineTail(p, last.Start+m[0]) {
				continue
			}
			owner = enclosingBlock(doc, p)
		}
		if blockRefs && owner != nil {
			owner.SetAttributeString("id", []byte("^"+id))
		}
	}
}

// enclosingBlock is the block an ^id inside p names: the nearest list item,
// otherwise the top-level block.
func enclosingBlock(doc *ast.Document, p ast.Node) ast.Node {
	for n := p.Parent(); n != nil; n = n.Parent() {
		if n.Kind() == ast.KindListItem {
			return n
		}
	}
	n := p
	for n.Parent() != nil && n.Parent() != ast.Node(doc) {
		n = n.Parent()
	}
	return n
}

// cutInlineTail drops p's inline text from source offset cut onward. It
// changes nothing and reports false unless that tail is plain text.
func cutInlineTail(p ast.Node, cut int) bool {
	var drop []ast.Node
	var trim *ast.Text
	for c := p.LastChild(); c != nil; c = c.PreviousSibling() {
		t, ok := c.(*ast.Text)
		if !ok {
			return false
		}
		if t.Segment.Stop <= cut {
			break
		}
		if t.Segment.Start >= cut {
			drop = append(drop, t)
			continue
		}
		trim = t
		break
	}
	for _, n := range drop {
		p.RemoveChild(p, n)
	}
	if trim != nil {
		trim.Segment = trim.Segment.WithStop(cut)
	} else if t, ok := p.LastChild().(*ast.Text); ok {
		// The marker had a line to itself; the line before it now ends p.
		t.SetSoftLineBreak(false)
		t.SetHardLineBreak(false)
	}
	return true
}
