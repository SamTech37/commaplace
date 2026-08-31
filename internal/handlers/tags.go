package handlers

import (
	"context"
	"fmt"
	htmlpkg "html"
	"log"
	"net/http"
	"net/url"
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
		"Tag":     tag,
		"Items":   items,
		"Related": s.relatedTags(r.Context(), variants, args),
	})
}

// cloudTag is one entry in the related-tag cloud. Size is a 1..5 bucket the
// template maps to a --fs-* step; the raw count only reaches the title text.
type cloudTag struct {
	Tag   string
	Count int
	Size  int
	URL   string
}

// relatedTags returns the tags that co-occur with the current one on the same
// note, most-shared first, sized into five buckets.
//
// Scoped to the current tag's own notes on purpose: a cloud of every tag in
// the database would grow without bound and stops being readable long before
// it stops being expensive (the global aggregate is already the slowest query
// we have — see plan.md's benchmark table). "What goes with this?" is also the
// more useful question, and following one lands on that tag's own cloud.
// Anything outside this view stays reachable through the tag search.
func (s *Server) relatedTags(ctx context.Context, variants []string, args []any) []cloudTag {
	inc := make([]string, len(variants))
	exc := make([]string, len(variants))
	for i := range variants {
		inc[i] = fmt.Sprintf("lower(nt1.tag) = $%d", i+1)
		exc[i] = fmt.Sprintf("lower(nt2.tag) <> $%d", i+1)
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT nt2.tag, COUNT(*) c
		FROM note_tags nt1
		JOIN notes n ON n.id = nt1.note_id
		JOIN note_tags nt2 ON nt2.note_id = nt1.note_id
		WHERE (`+strings.Join(inc, " OR ")+`) AND `+strings.Join(exc, " AND ")+`
		  AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
		GROUP BY nt2.tag
		ORDER BY c DESC, nt2.tag
		LIMIT 40`, args...)
	if err != nil {
		log.Printf("relatedTags: %v", err)
		return nil // a non-critical adornment; the note list still renders
	}
	defer rows.Close()

	var out []cloudTag
	hi, lo := 0, 0
	for rows.Next() {
		var t cloudTag
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil
		}
		hi = max(hi, t.Count)
		if lo == 0 || t.Count < lo {
			lo = t.Count
		}
		t.URL = "/tag/" + url.PathEscape(t.Tag)
		out = append(out, t)
	}
	for i := range out {
		// Spread counts over five steps, rounding to nearest. When every tag
		// co-occurs equally often there is no ranking to show, so they all sit
		// in the middle rather than all shouting at size 5.
		if hi == lo {
			out[i].Size = 3
			continue
		}
		// Use no more steps than the range has room for: with counts of only
		// 1 and 2, jumping fs-xs to fs-lg would dramatize a difference of one.
		span := hi - lo
		steps := min(4, span)
		out[i].Size = 1 + ((out[i].Count-lo)*steps+span/2)/span
	}
	return out
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
