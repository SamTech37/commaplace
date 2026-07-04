package handlers

import (
	"fmt"
	htmlpkg "html"
	"net/http"
	"strings"
	"unicode"

	"commonplace/internal/markdown"
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
	// Match by script variant (Simplified/Traditional) and case, not exact
	// bytes — note_tags isn't guaranteed lowercase at rest (some seed paths
	// insert raw casing, e.g. "UX"), and a tag typed in one script should
	// find notes tagged in the other.
	variants := searchVariants(tag)
	clauses := make([]string, len(variants))
	args := make([]any, len(variants))
	for i, v := range variants {
		clauses[i] = fmt.Sprintf("lower(nt.tag) = $%d", i+1)
		args[i] = v
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT n.title, n.slug, n.body_md, n.updated_at, u.handle
		FROM note_tags nt
		JOIN notes n ON n.id = nt.note_id
		JOIN users u ON u.id = n.author_id
		WHERE (`+strings.Join(clauses, " OR ")+`) AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
		ORDER BY n.updated_at DESC
		LIMIT 200`, args...)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var items []feedItem
	for rows.Next() {
		var title, slug, body, handle string
		var updated int64
		if err := rows.Scan(&title, &slug, &body, &updated, &handle); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, feedItem{
			Title:        title,
			URL:          noteURL(handle, slug),
			Excerpt:      markdown.Excerpt(body, 150),
			AuthorHandle: handle,
			UpdatedRel:   relativeTime(updated),
		})
	}
	s.render(w, r, "tag", map[string]any{
		"Tag":   tag,
		"Items": items,
	})
}

// GetTagSuggest returns an HTML <li> fragment list of existing tags whose
// name starts with q, most-used first. Shared by the editor's "#"
// autocomplete (cmeditor.js) and the /feed tag-search picker (tagsearch.js).
func (s *Server) GetTagSuggest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	q := normalizeTag(r.URL.Query().Get("q"))
	// Match by script variant (Simplified/Traditional) as a prefix, not
	// substring — unlike likeAnyVariant, which is built for title search.
	variants := searchVariants(q)
	clauses := make([]string, len(variants))
	args := make([]any, len(variants))
	for i, v := range variants {
		// lower(tag), not tag: note_tags isn't guaranteed lowercase at rest
		// (some seed paths insert raw casing), and q is already lowercased.
		clauses[i] = fmt.Sprintf("lower(tag) LIKE $%d", i+1)
		args[i] = v + "%"
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT tag, COUNT(*) AS uses FROM note_tags WHERE (`+strings.Join(clauses, " OR ")+`)
		 GROUP BY tag ORDER BY uses DESC, tag LIMIT 20`,
		args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		var uses int
		if err := rows.Scan(&tag, &uses); err != nil {
			return
		}
		fmt.Fprintf(w, `<li class="ac-item" data-insert=%q><span class="ac-primary">#%s</span><span class="ac-secondary">%d</span></li>`,
			tag, htmlpkg.EscapeString(tag), uses)
	}
}
