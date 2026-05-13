// Package markdown contains the goldmark pipeline and the wiki-link
// extension. The parser logic lives here (no goldmark deps) so it is
// easy to unit-test and re-use at save time for link extraction.
package markdown

import "strings"

// WikiLink is the parsed payload of [[...]].
//
//   [[slug]]                   -> {Slug: "slug"}
//   [[folder/slug]]            -> {Folder: "folder", Slug: "slug"}
//   [[@user/slug]]             -> {User: "user", Slug: "slug"}
//   [[@user/folder/slug]]      -> {User: "user", Folder: "folder", Slug: "slug"}
type WikiLink struct {
	User   string // empty -> same vault as the surrounding note
	Folder string // empty -> root of the vault
	Slug   string
	Raw    string // original payload between [[ and ]]
}

// URL builds the path the link points at. currentUser is used when the
// link is same-vault (User == "").
func (l WikiLink) URL(currentUser string) string {
	handle := l.User
	if handle == "" {
		handle = currentUser
	}
	parts := []string{handle}
	if l.Folder != "" {
		parts = append(parts, l.Folder)
	}
	parts = append(parts, l.Slug)
	return "/" + strings.Join(parts, "/")
}

// Label is what users see for the link text. Last path segment, like Obsidian.
func (l WikiLink) Label() string { return l.Slug }

// ParseLink parses the body between [[ and ]]. Returns (link, true) on
// success, ({}, false) for malformed input.
func ParseLink(body string) (WikiLink, bool) {
	raw := body
	s := strings.TrimSpace(body)
	if s == "" {
		return WikiLink{}, false
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

	if strings.HasPrefix(s, "/") {
		return WikiLink{}, false
	}
	parts := strings.Split(s, "/")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return WikiLink{}, false
		}
	}
	slug := parts[len(parts)-1]
	folder := strings.Join(parts[:len(parts)-1], "/")

	return WikiLink{
		User:   user,
		Folder: folder,
		Slug:   slug,
		Raw:    raw,
	}, true
}

// Extract scans body for every [[...]] occurrence and returns each
// parsable wiki link, deduplicated by (user, folder, slug). Used at save
// time to populate the links table.
func Extract(body string) []WikiLink {
	var out []WikiLink
	seen := map[string]bool{}
	src := body
	for {
		i := strings.Index(src, "[[")
		if i < 0 {
			break
		}
		rest := src[i+2:]
		j := strings.Index(rest, "]]")
		if j < 0 {
			break
		}
		inner := rest[:j]
		if !strings.ContainsAny(inner, "\n") && !strings.Contains(inner, "[[") {
			if l, ok := ParseLink(inner); ok {
				key := l.User + "\x00" + l.Folder + "\x00" + l.Slug
				if !seen[key] {
					seen[key] = true
					out = append(out, l)
				}
			}
		}
		src = rest[j+2:]
	}
	return out
}
