package handlers

import (
	"net/http"
	"strings"
	"unicode"

	"commaplace/internal/markdown"
)

// normalizeTag lowercases ASCII, collapses runs of non-letter/digit into '-'
// (Unicode letters/digits survive so e.g. "混音" stays intact), and trims '-'.
// Returns "" for input that has no letters or digits at all.
func normalizeTag(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevDash := true
	for _, r := range s {
		switch {
		case r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// parseTags accepts a comma-separated tag list, returning normalized
// deduped tags in input order. Whitespace around commas is fine.
func parseTags(input string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(input, ",") {
		t := normalizeTag(raw)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// GetTagPage lists notes carrying a given tag, newest first.
func (s *Server) GetTagPage(w http.ResponseWriter, r *http.Request) {
	tag := normalizeTag(r.PathValue("tag"))
	if tag == "" {
		s.renderError(w, r, http.StatusNotFound, "no such tag")
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT n.title, n.folder_path, n.slug, n.body_md, n.updated_at, u.handle
		FROM note_tags nt
		JOIN notes n ON n.id = nt.note_id
		JOIN users u ON u.id = n.author_id
		WHERE nt.tag = ? AND n.hidden_at IS NULL
		ORDER BY n.updated_at DESC
		LIMIT 200`, tag)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var items []feedItem
	for rows.Next() {
		var (
			title, folderPath, slug, body, handle string
			updated                                int64
		)
		if err := rows.Scan(&title, &folderPath, &slug, &body, &updated, &handle); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, feedItem{
			Title:        title,
			URL:          noteURL(handle, folderPath, slug),
			Excerpt:      markdown.Excerpt(body, 150),
			AuthorHandle: handle,
			FolderPath:   folderPath,
			UpdatedRel:   relativeTime(updated),
		})
	}
	s.render(w, r, "tag", map[string]any{
		"Tag":   tag,
		"Items": items,
	})
}
