package markdown

import "strings"

// SplitFrontmatter retains the exact YAML prefix, including delimiters. An
// unclosed block is ordinary Markdown, never a reason to discard the body.
func SplitFrontmatter(md string) (prefix, body string) {
	md = strings.TrimPrefix(md, "\ufeff")
	first, rest, ok := strings.Cut(md, "\n")
	if !ok || strings.TrimSpace(first) != "---" {
		return "", md
	}
	offset := len(first) + 1
	for _, line := range strings.SplitAfter(rest, "\n") {
		offset += len(line)
		if strings.TrimSpace(line) == "---" {
			return md[:offset], md[offset:]
		}
	}
	return "", md
}
