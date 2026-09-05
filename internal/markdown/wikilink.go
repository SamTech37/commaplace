// Package markdown contains the goldmark pipeline and the wiki-link
// extension. Link payload parsing and renderer-aligned extraction live here
// so they can be reused when saving notes.
package markdown

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// WikiLink is the parsed payload of [[...]].
//
//	[[slug]]           -> {Slug: "slug"}
//	[[@user/slug]]     -> {User: "user", Slug: "slug"}
//	[[slug#Heading]]   -> {Slug: "slug", Anchor: "Heading"}
//	[[slug|Alias]]     -> {Slug: "slug", Alias: "Alias"}
//	[[#Heading]]       -> {Anchor: "Heading"} (same-note link)
//
// Paths with slashes (legacy folder syntax like [[folder/slug]] or
// [[@user/folder/slug]]) use only the last segment as the slug.
type WikiLink struct {
	User   string // empty -> same vault as the surrounding note
	Slug   string // empty only for same-note heading links ([[#Heading]])
	Anchor string // heading text after #; empty if no anchor
	Alias  string // display label after |; empty -> use Label() default
	Raw    string // original payload between [[ and ]]
}

// URL builds the path the link points at. currentUser is used when the
// link is same-vault (User == "").
func (l WikiLink) URL(currentUser string) string {
	if l.Slug == "" {
		// Same-note heading link: [[#Heading]]
		return "#" + headingAnchor(l.Anchor)
	}
	handle := l.User
	if handle == "" {
		handle = currentUser
	}
	u := "/" + handle + "/" + l.Slug
	if l.Anchor != "" {
		u += "#" + headingAnchor(l.Anchor)
	}
	return u
}

// headingAnchor converts heading text to a URL fragment. Unicode letters
// are preserved (so CJK headings produce usable anchors); ASCII punctuation
// other than hyphen/underscore is dropped; spaces become hyphens.
// This must match the algorithm in headingIDTransformer (render.go).
func headingAnchor(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		case r > 127:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

// Label is what users see for the link text. Falls back: alias > slug > anchor.
func (l WikiLink) Label() string {
	if l.Alias != "" {
		return l.Alias
	}
	if l.Slug != "" {
		return l.Slug
	}
	return l.Anchor
}

// ParseLink parses the body between [[ and ]]. Returns (link, true) on
// success, ({}, false) for malformed input.
func ParseLink(body string) (WikiLink, bool) {
	raw := body
	s := strings.TrimSpace(body)
	if s == "" {
		return WikiLink{}, false
	}

	// Extract display alias: everything after the last '|'.
	var alias string
	if idx := strings.LastIndexByte(s, '|'); idx >= 0 {
		alias = strings.TrimSpace(s[idx+1:])
		s = strings.TrimSpace(s[:idx])
		if s == "" && alias == "" {
			return WikiLink{}, false
		}
	}

	var user string
	if strings.HasPrefix(s, "@") {
		rest := s[1:]
		i := strings.IndexByte(rest, '/')
		if i <= 0 || i == len(rest)-1 {
			// "@", "@user", "@user/" — all invalid.
			return WikiLink{}, false
		}
		user = rest[:i]
		s = rest[i+1:]
		if strings.TrimSpace(user) == "" {
			return WikiLink{}, false
		}
	}

	// Extract heading anchor: part after the first '#'.
	var anchor string
	if strings.HasPrefix(s, "#") {
		// Same-note heading link: [[#Heading]] or [[#Heading|Alias]]
		anchor = strings.TrimSpace(s[1:])
		s = ""
		if anchor == "" {
			return WikiLink{}, false
		}
	} else if idx := strings.IndexByte(s, '#'); idx >= 0 {
		anchor = strings.TrimSpace(s[idx+1:])
		s = strings.TrimSpace(s[:idx])
	}

	if s == "" && anchor == "" {
		return WikiLink{}, false
	}

	var slug string
	if s != "" {
		if strings.HasPrefix(s, "/") {
			return WikiLink{}, false
		}
		parts := strings.Split(s, "/")
		for _, p := range parts {
			if strings.TrimSpace(p) == "" {
				return WikiLink{}, false
			}
		}
		// Use only the last segment; legacy folder prefix is silently ignored.
		slug = parts[len(parts)-1]
	}

	return WikiLink{
		User:   user,
		Slug:   slug,
		Anchor: anchor,
		Alias:  alias,
		Raw:    raw,
	}, true
}

// Extract uses the renderer's Markdown parser so code examples, comments and
// YAML do not create phantom graph edges. Deduplicate by (user, slug).
func Extract(body string) []WikiLink {
	var out []WikiLink
	seen := map[string]bool{}
	_, body = SplitFrontmatter(body)
	src := []byte(stripComments(body))
	exts := append([]goldmark.Extender{}, staticExts...)
	exts = append(exts, &wikiExt{}, &embedExt{})
	doc := goldmark.New(goldmark.WithExtensions(exts...)).Parser().Parse(text.NewReader(src))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var l WikiLink
		switch n := node.(type) {
		case *wikiNode:
			l = n.Link
		case *embedNode:
			l = n.Link
		default:
			return ast.WalkContinue, nil
		}
		key := l.User + "\x00" + l.Slug
		if l.Slug != "" && !seen[key] {
			seen[key] = true
			out = append(out, l)
		}
		return ast.WalkContinue, nil
	})
	return out
}

// ExtractInlineTags scans body for inline #tag occurrences and returns raw
// tag names (without leading #), deduplicated, in encounter order.
// Frontmatter and [[wikilinks]] are skipped. The caller is responsible for normalization.
func ExtractInlineTags(body string) []string {
	_, s := SplitFrontmatter(body)
	// blank out [[wikilinks]] so #anchors inside them aren't picked up
	s = StripWikiLinks(s)
	// blank out URLs so #fragments aren't picked up
	s = urlRe.ReplaceAllString(s, " ")
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '#')
		if j < 0 {
			break
		}
		pos := i + j
		// Word boundary: # must not be preceded by alphanumeric. A second
		// consecutive # is heading-like punctuation, never a new tag opener.
		if pos > 0 && (isAlnumByte(s[pos-1]) || s[pos-1] == '#') {
			i = pos + 1
			continue
		}
		rest := s[pos+1:]
		end := 0
		for end < len(rest) && isTagByte(rest[end]) {
			end++
		}
		if end == 0 {
			i = pos + 1
			continue
		}
		tag := rest[:end]
		if !allDigitBytes(tag) && !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
		i = pos + 1 + end
	}
	return out
}

// StripWikiLinks replaces every [[...]] token with a space, dropping the link
// text along with the brackets. Callers that want the label kept use
// Excerpt instead.
func StripWikiLinks(s string) string {
	for {
		i := strings.Index(s, "[[")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+2:], "]]")
		if j < 0 {
			return s
		}
		start := i
		if start > 0 && s[start-1] == '!' {
			start--
		}
		s = s[:start] + " " + s[i+2+j+2:]
	}
}

// urlRe matches a bare http(s) URL up to the next whitespace.
var urlRe = regexp.MustCompile(`https?://\S+`)

// isAlnumByte reports whether c is an ASCII alphanumeric character.
func isAlnumByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// isTagByte reports whether c is valid inside an Obsidian tag name.
func isTagByte(c byte) bool {
	return isAlnumByte(c) || c == '_' || c == '-' || c == '/' || c > 127
}

// allDigitBytes reports whether every byte in s is an ASCII digit.
func allDigitBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
