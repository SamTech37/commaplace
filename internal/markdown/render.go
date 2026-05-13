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
	g := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			&wikiExt{currentUser: currentUser, resolve: resolve},
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
