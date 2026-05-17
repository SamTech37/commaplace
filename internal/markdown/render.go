package markdown

import (
	"bytes"
	htmlpkg "html"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Resolver returns true if a wiki link points at an existing target.
// nil treats every link as unresolved.
type Resolver func(WikiLink) bool

// Render converts md to safe HTML. Wiki links are wrapped in <a class="wiki ...">
// with "wiki-resolved" or "wiki-unresolved" depending on resolver.
func Render(md, currentUser string, resolve Resolver) (template.HTML, error) {
	md = stripComments(md)
	g := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			&wikiExt{currentUser: currentUser, resolve: resolve},
			&headingIDExt{},
			&tagExt{},
			&highlightExt{},
			&calloutExt{},
			&mathExt{},
			&codeBlockExt{},
		),
	)
	var buf bytes.Buffer
	if err := g.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// ExternalLink represents a [[ ... ]] resolved by an external-mode caller.
type ExternalLink struct {
	URL   string
	Class string
	Label string
}

// ExternalResolver returns the rendered link for a raw [[token]] string.
// raw is the contents inside [[ ]] before any alias/anchor splitting.
type ExternalResolver func(raw string) ExternalLink

// RenderExternal converts md to HTML using a caller-supplied resolver for
// every [[wiki link]]. Used for external (Obsidian Publish) notes where
// commonplace's own /@user/slug URL scheme doesn't apply.
func RenderExternal(md string, resolve ExternalResolver) (template.HTML, error) {
	md = stripComments(md)
	g := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			&externalWikiExt{resolve: resolve},
			&headingIDExt{},
			&tagExt{},
			&highlightExt{},
			&calloutExt{},
			&mathExt{},
			&codeBlockExt{},
		),
	)
	var buf bytes.Buffer
	if err := g.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// stripLeadingMarker removes one line-leading markdown marker so card excerpts
// don't show raw "- ", "1. ", "# " etc.
func stripLeadingMarker(ln string) string {
	switch {
	case strings.HasPrefix(ln, "- "):
		return strings.TrimSpace(ln[2:])
	case strings.HasPrefix(ln, "> "):
		return strings.TrimSpace(ln[2:])
	}
	// numbered list: "12. text"
	for i, r := range ln {
		if r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && r == '.' && i+1 < len(ln) && ln[i+1] == ' ' {
			return strings.TrimSpace(ln[i+2:])
		}
		break
	}
	// heading: "#### text" — strip the run of #
	if strings.HasPrefix(ln, "#") {
		i := 0
		for i < len(ln) && ln[i] == '#' {
			i++
		}
		if i < len(ln) && ln[i] == ' ' {
			return strings.TrimSpace(ln[i+1:])
		}
	}
	return ln
}

// Excerpt strips the most common markdown syntax and returns up to n
// characters. Used for feed/profile cards. Not perfect; intentionally cheap.
func Excerpt(md string, n int) string {
	s := md
	// strip YAML frontmatter at the very top (--- ... ---)
	if strings.HasPrefix(s, "---\n") {
		if idx := strings.Index(s[4:], "\n---"); idx >= 0 {
			s = strings.TrimLeft(s[4+idx+4:], "\r\n")
		}
	}
	// strip code fences quickly
	for {
		i := strings.Index(s, "```")
		if i < 0 {
			break
		}
		j := strings.Index(s[i+3:], "```")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[i+3+j+3:]
	}
	// drop wiki links, keep their slug
	for {
		i := strings.Index(s, "[[")
		if i < 0 {
			break
		}
		rest := s[i+2:]
		j := strings.Index(rest, "]]")
		if j < 0 {
			break
		}
		inner := rest[:j]
		label := inner
		if k := strings.LastIndexByte(inner, '/'); k >= 0 {
			label = inner[k+1:]
		}
		s = s[:i] + label + rest[j+2:]
	}
	rep := strings.NewReplacer(
		"**", "", "__", "", "*", "", "_", "",
		"`", "", "\r", "",
	)
	s = rep.Replace(s)
	// per-line cleanup: trim, drop leading bullet/heading/number/quote markers
	{
		raw := strings.Split(s, "\n")
		out := make([]string, 0, len(raw))
		for _, ln := range raw {
			ln = strings.TrimSpace(ln)
			ln = stripLeadingMarker(ln)
			out = append(out, ln)
		}
		s = strings.Join(out, "\n")
	}
	// collapse runs of blank lines into a single blank line
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	// collapse runs of spaces/tabs inside each line (but keep newlines)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}

// ---------- external wiki-link extension ----------

type externalWikiExt struct {
	resolve ExternalResolver
}

func (e *externalWikiExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&externalWikiParser{}, 198),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&externalWikiRenderer{resolve: e.resolve}, 198),
	))
}

var kindExternalWiki = ast.NewNodeKind("ExternalWikiLink")

type externalWikiNode struct {
	ast.BaseInline
	Raw string
}

func (n *externalWikiNode) Kind() ast.NodeKind            { return kindExternalWiki }
func (n *externalWikiNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type externalWikiParser struct{}

func (p *externalWikiParser) Trigger() []byte { return []byte{'['} }

func (p *externalWikiParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 5 || line[0] != '[' || line[1] != '[' {
		return nil
	}
	end := -1
	for i := 2; i+1 < len(line); i++ {
		if line[i] == '\n' {
			return nil
		}
		if line[i] == '[' && i+1 < len(line) && line[i+1] == '[' {
			return nil
		}
		if line[i] == ']' && line[i+1] == ']' {
			end = i
			break
		}
	}
	if end < 0 {
		return nil
	}
	inner := strings.TrimSpace(string(line[2:end]))
	if inner == "" {
		return nil
	}
	block.Advance(end + 2)
	return &externalWikiNode{Raw: inner}
}

type externalWikiRenderer struct {
	resolve ExternalResolver
}

func (r *externalWikiRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindExternalWiki, r.render)
}

func (r *externalWikiRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*externalWikiNode)
	link := ExternalLink{URL: "#", Class: "wiki wiki-unresolved", Label: n.Raw}
	if r.resolve != nil {
		link = r.resolve(n.Raw)
	}
	if link.Label == "" {
		link.Label = n.Raw
	}
	w.WriteString(`<a href="`)
	w.WriteString(htmlpkg.EscapeString(link.URL))
	w.WriteString(`" class="`)
	w.WriteString(htmlpkg.EscapeString(link.Class))
	w.WriteString(`">`)
	w.WriteString(htmlpkg.EscapeString(link.Label))
	w.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}

// ---------- comment pre-processing ----------

// stripComments removes %% ... %% spans (both inline and multiline) from
// markdown before it reaches goldmark.
func stripComments(md string) string {
	var b strings.Builder
	for {
		i := strings.Index(md, "%%")
		if i < 0 {
			b.WriteString(md)
			return b.String()
		}
		b.WriteString(md[:i])
		j := strings.Index(md[i+2:], "%%")
		if j < 0 {
			b.WriteString(md[i:]) // unclosed %%, keep as-is
			return b.String()
		}
		md = md[i+2+j+2:]
	}
}

// ---------- highlight extension ==text== ----------

type highlightExt struct{}

func (e *highlightExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&highlightParser{}, 170),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&highlightRenderer{}, 170),
	))
}

var kindHighlight = ast.NewNodeKind("Highlight")

type highlightNode struct {
	ast.BaseInline
	Content string
}

func (n *highlightNode) Kind() ast.NodeKind         { return kindHighlight }
func (n *highlightNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type highlightParser struct{}

func (p *highlightParser) Trigger() []byte { return []byte{'='} }

func (p *highlightParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 5 || line[0] != '=' || line[1] != '=' {
		return nil
	}
	if line[2] == ' ' { // no leading space inside ==
		return nil
	}
	rest := line[2:]
	end := -1
	for i := 0; i+1 < len(rest); i++ {
		if rest[i] == '\n' {
			return nil
		}
		if rest[i] == '=' && rest[i+1] == '=' {
			if i > 0 && rest[i-1] == ' ' { // no trailing space before ==
				return nil
			}
			end = i
			break
		}
	}
	if end <= 0 {
		return nil
	}
	block.Advance(2 + end + 2)
	return &highlightNode{Content: string(rest[:end])}
}

type highlightRenderer struct{}

func (r *highlightRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindHighlight, r.render)
}

func (r *highlightRenderer) render(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	w.WriteString("<mark>")
	w.WriteString(htmlpkg.EscapeString(node.(*highlightNode).Content))
	w.WriteString("</mark>")
	return ast.WalkSkipChildren, nil
}

// ---------- inline math extension $...$ ----------

type mathExt struct{}

func (e *mathExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&mathInlineParser{}, 175),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&mathInlineRenderer{}, 175),
	))
}

var kindMathInline = ast.NewNodeKind("MathInline")

type mathInlineNode struct {
	ast.BaseInline
	Content string
}

func (n *mathInlineNode) Kind() ast.NodeKind         { return kindMathInline }
func (n *mathInlineNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type mathInlineParser struct{}

func (p *mathInlineParser) Trigger() []byte { return []byte{'$'} }

func (p *mathInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 3 || line[0] != '$' || line[1] == '$' {
		return nil // too short or $$ (block math, handled by code fence)
	}
	if line[1] == ' ' || line[1] == '\t' {
		return nil // space after $ → not math (e.g. "$100")
	}
	end := -1
	for i := 1; i < len(line); i++ {
		if line[i] == '\n' {
			return nil
		}
		if line[i] == '$' && line[i-1] != ' ' {
			end = i
			break
		}
	}
	if end <= 0 {
		return nil
	}
	block.Advance(end + 1)
	return &mathInlineNode{Content: string(line[1:end])}
}

type mathInlineRenderer struct{}

func (r *mathInlineRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMathInline, r.render)
}

func (r *mathInlineRenderer) render(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	w.WriteString(`<span class="math-inline">$`)
	w.WriteString(htmlpkg.EscapeString(node.(*mathInlineNode).Content))
	w.WriteString(`$</span>`)
	return ast.WalkSkipChildren, nil
}

// ---------- code block extension (mermaid + math fences) ----------

type codeBlockExt struct{}

func (e *codeBlockExt) Extend(m goldmark.Markdown) {
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&codeBlockRenderer{}, 500),
	))
}

type codeBlockRenderer struct{}

func (r *codeBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.render)
}

func (r *codeBlockRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.FencedCodeBlock)
	lang := string(n.Language(source))

	if entering {
		switch lang {
		case "mermaid":
			w.WriteString(`<pre class="mermaid">`)
		case "math":
			w.WriteString(`<div class="math-block">`)
		default:
			if lang != "" {
				w.WriteString(`<pre><code class="language-` + htmlpkg.EscapeString(lang) + `">`)
			} else {
				w.WriteString("<pre><code>")
			}
		}
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			w.WriteString(htmlpkg.EscapeString(string(line.Value(source))))
		}
		return ast.WalkSkipChildren, nil
	}
	switch lang {
	case "mermaid":
		w.WriteString("</pre>\n")
	case "math":
		w.WriteString("</div>\n")
	default:
		w.WriteString("</code></pre>\n")
	}
	return ast.WalkContinue, nil
}

// ---------- callout extension > [!type] ----------

type calloutExt struct{}

func (e *calloutExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(&calloutTransformer{}, 90),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&calloutRenderer{}, 200),
	))
}

type calloutTransformer struct{}

func (t *calloutTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	src := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindBlockquote {
			return ast.WalkContinue, nil
		}
		firstChild := n.FirstChild()
		if firstChild == nil || firstChild.Kind() != ast.KindParagraph {
			return ast.WalkContinue, nil
		}
		// Goldmark splits the line across several ast.Text nodes (e.g. "[",
		// "!note", "]"). Collect them all until the first SoftLineBreak.
		var sb strings.Builder
		for child := firstChild.FirstChild(); child != nil; child = child.NextSibling() {
			if child.Kind() != ast.KindText {
				break
			}
			txt := child.(*ast.Text)
			sb.Write(txt.Value(src))
			if txt.SoftLineBreak() {
				break
			}
		}
		ct, title, foldable, expanded, ok := parseCalloutLine(strings.TrimSpace(sb.String()))
		if !ok {
			return ast.WalkContinue, nil
		}
		// Remove the text nodes that make up the [!type] line.
		for child := firstChild.FirstChild(); child != nil; {
			if child.Kind() != ast.KindText {
				break
			}
			txt := child.(*ast.Text)
			softLB := txt.SoftLineBreak()
			next := child.NextSibling()
			firstChild.RemoveChild(firstChild, child)
			if softLB {
				break
			}
			child = next
		}
		// Drop the first paragraph if it is now empty.
		if firstChild.ChildCount() == 0 {
			n.RemoveChild(n, firstChild)
		}
		// Tag the blockquote so the renderer can identify it.
		n.SetAttribute([]byte("data-callout"), []byte(ct))
		n.SetAttribute([]byte("data-callout-title"), []byte(title))
		if foldable {
			fold := "closed"
			if expanded {
				fold = "open"
			}
			n.SetAttribute([]byte("data-callout-fold"), []byte(fold))
		}
		return ast.WalkContinue, nil
	})
}

// parseCalloutLine parses the first line of a blockquote for callout syntax.
// Accepted forms: [!type], [!type] Title, [!type]-, [!type]+, [!type]- Title.
func parseCalloutLine(line string) (ct, title string, foldable, expanded, ok bool) {
	if !strings.HasPrefix(line, "[!") {
		return
	}
	end := strings.IndexByte(line, ']')
	if end < 0 {
		return
	}
	ct = strings.ToLower(strings.TrimSpace(line[2:end]))
	if ct == "" {
		return
	}
	rest := line[end+1:]
	if strings.HasPrefix(rest, "-") {
		foldable, expanded = true, false
		rest = rest[1:]
	} else if strings.HasPrefix(rest, "+") {
		foldable, expanded = true, true
		rest = rest[1:]
	}
	title = strings.TrimSpace(rest)
	if title == "" {
		title = strings.ToUpper(ct[:1]) + ct[1:]
	}
	ok = true
	return
}

type calloutRenderer struct{}

func (r *calloutRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindBlockquote, r.render)
}

func (r *calloutRenderer) render(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	ctVal, isCallout := node.Attribute([]byte("data-callout"))
	if !isCallout {
		if entering {
			w.WriteString("<blockquote>\n")
		} else {
			w.WriteString("</blockquote>\n")
		}
		return ast.WalkContinue, nil
	}
	ct := htmlpkg.EscapeString(string(ctVal.([]byte)))
	_, foldable := node.Attribute([]byte("data-callout-fold"))

	if entering {
		titleVal, _ := node.Attribute([]byte("data-callout-title"))
		title := htmlpkg.EscapeString(string(titleVal.([]byte)))
		if foldable {
			foldState, _ := node.Attribute([]byte("data-callout-fold"))
			open := ""
			if string(foldState.([]byte)) == "open" {
				open = " open"
			}
			w.WriteString(`<details class="callout callout-` + ct + `"` + open + ">\n")
			w.WriteString(`<summary class="callout-title">` + title + "</summary>\n")
			w.WriteString("<div class=\"callout-body\">\n")
		} else {
			w.WriteString(`<div class="callout callout-` + ct + "\">\n")
			w.WriteString(`<div class="callout-title">` + title + "</div>\n")
			w.WriteString("<div class=\"callout-body\">\n")
		}
	} else {
		if foldable {
			w.WriteString("</div></details>\n")
		} else {
			w.WriteString("</div></div>\n")
		}
	}
	return ast.WalkContinue, nil
}

// ---------- heading ID extension ----------
// Assigns id="..." to every heading using a Unicode-preserving algorithm
// that matches headingAnchor() in wikilink.go, so [[Note#Heading]] fragments
// resolve correctly even for CJK headings.

type headingIDExt struct{}

func (e *headingIDExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(&headingIDTransformer{}, 100),
	))
}

type headingIDTransformer struct{}

func (t *headingIDTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindHeading {
			return ast.WalkContinue, nil
		}
		id := headingAnchor(collectText(n, reader.Source()))
		if id == "" {
			id = "heading"
		}
		n.SetAttribute([]byte("id"), []byte(id))
		return ast.WalkContinue, nil
	})
}

func collectText(n ast.Node, src []byte) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := child.(*ast.Text); ok {
				sb.Write(t.Value(src))
			}
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

// ---------- inline tag extension ----------
// Renders #tag occurrences in body text as <a href="/tag/name"> links.

type tagExt struct{}

func (e *tagExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&tagInlineParser{}, 180),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&tagRenderer{}, 180),
	))
}

var kindTag = ast.NewNodeKind("Tag")

type tagNode struct {
	ast.BaseInline
	Name string
}

func (n *tagNode) Kind() ast.NodeKind         { return kindTag }
func (n *tagNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type tagInlineParser struct{}

func (p *tagInlineParser) Trigger() []byte { return []byte{'#'} }

func (p *tagInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if len(line) < 2 || line[0] != '#' {
		return nil
	}
	// word boundary: # must not be preceded by an alphanumeric character
	if seg.Start > 0 && isAlnumByte(block.Source()[seg.Start-1]) {
		return nil
	}
	rest := line[1:]
	end := 0
	for end < len(rest) && isTagByte(rest[end]) {
		end++
	}
	if end == 0 || allDigitBytes(string(rest[:end])) {
		return nil
	}
	block.Advance(1 + end)
	return &tagNode{Name: string(rest[:end])}
}

type tagRenderer struct{}

func (r *tagRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindTag, r.render)
}

func (r *tagRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*tagNode)
	w.WriteString(`<a href="/tag/`)
	w.WriteString(htmlpkg.EscapeString(n.Name))
	w.WriteString(`" class="tag-chip">#`)
	w.WriteString(htmlpkg.EscapeString(n.Name))
	w.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}

// ---------- goldmark wiki-link extension ----------

type wikiExt struct {
	currentUser string
	resolve     Resolver
}

func (e *wikiExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&wikiInlineParser{}, 199),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&wikiRenderer{currentUser: e.currentUser, resolve: e.resolve}, 199),
	))
}

var kindWikiLink = ast.NewNodeKind("WikiLink")

type wikiNode struct {
	ast.BaseInline
	Link WikiLink
}

func (n *wikiNode) Kind() ast.NodeKind                  { return kindWikiLink }
func (n *wikiNode) Dump(source []byte, level int)       { ast.DumpHelper(n, source, level, nil, nil) }

type wikiInlineParser struct{}

func (p *wikiInlineParser) Trigger() []byte { return []byte{'['} }

func (p *wikiInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 5 || line[0] != '[' || line[1] != '[' {
		return nil
	}
	end := -1
	for i := 2; i+1 < len(line); i++ {
		if line[i] == '\n' {
			return nil
		}
		// don't allow nested [[
		if line[i] == '[' && i+1 < len(line) && line[i+1] == '[' {
			return nil
		}
		if line[i] == ']' && line[i+1] == ']' {
			end = i
			break
		}
	}
	if end < 0 {
		return nil
	}
	inner := string(line[2:end])
	link, ok := ParseLink(inner)
	if !ok {
		return nil
	}
	block.Advance(end + 2)
	return &wikiNode{Link: link}
}

type wikiRenderer struct {
	currentUser string
	resolve     Resolver
}

func (r *wikiRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindWikiLink, r.render)
}

func (r *wikiRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*wikiNode)
	resolved := r.resolve != nil && r.resolve(n.Link)
	crossVault := n.Link.User != "" && n.Link.User != r.currentUser

	var cls string
	switch {
	case crossVault && resolved:
		cls = "wiki wiki-cross-resolved"
	case crossVault && !resolved:
		cls = "wiki wiki-cross-unresolved"
	case !crossVault && resolved:
		cls = "wiki wiki-resolved"
	default:
		cls = "wiki wiki-unresolved"
	}
	url := n.Link.URL(r.currentUser)
	w.WriteString(`<a href="`)
	w.WriteString(htmlpkg.EscapeString(url))
	w.WriteString(`" class="`)
	w.WriteString(cls)
	w.WriteString(`">`)
	w.WriteString(htmlpkg.EscapeString(n.Link.Label()))
	w.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}
