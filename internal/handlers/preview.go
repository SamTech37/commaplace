package handlers

import (
	"fmt"
	"net/http"

	"commonplace/internal/markdown"
)

// GetNotePreview serves the hover-preview card for an internal note link.
//
// It returns the same feedCard the feed renders, through the same
// masonry_card template — so a list-shaped note previews as a list, and the
// card follows the view layer whenever that moves. Uses the same
// noteCardColumns + scanCards path as every other note-listing surface, so
// this can't drift out of sync with what a feedCard needs.
func (s *Server) GetNotePreview(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")
	slug := r.PathValue("slug")

	if anchor := r.URL.Query().Get("h"); anchor != "" && s.renderSectionPreview(w, r, handle, slug, anchor) {
		return
	}

	// Published-only, gated in SQL: the handler stays viewer-independent,
	// which is what makes the response cacheable.
	rows, err := s.DB.QueryContext(r.Context(), fmt.Sprintf(`
		SELECT %s
		FROM notes n JOIN users u ON u.id = n.author_id
		WHERE u.handle = $1 AND n.slug = $2
		  AND `+readableNote, noteCardColumns),
		handle, slug,
	)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	cards, err := scanCards(rows)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if len(cards) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	attachTagsToCards(r.Context(), s.DB, cards)

	w.Header().Set("Cache-Control", "private, max-age=60")
	s.renderFragment(w, r, masonryCard(cards[0]))
}

// renderSectionPreview writes the hover card for one section of a note. It
// reports false, having written nothing, when the note or the section does
// not exist; the caller then falls back to the whole-note card, which is
// also where a click on such a link lands.
func (s *Server) renderSectionPreview(w http.ResponseWriter, r *http.Request, handle, slug, anchor string) bool {
	var title, body, author string
	err := s.DB.QueryRowContext(r.Context(), `
		SELECT n.title, n.body_md, u.handle
		FROM notes n JOIN users u ON u.id = n.author_id
		WHERE u.handle = $1 AND n.slug = $2
		  AND `+readableNote,
		handle, slug,
	).Scan(&title, &body, &author)
	if err != nil {
		return false
	}
	section, ok := markdown.Section(body, anchor)
	if !ok {
		return false
	}
	resolver := s.buildResolver(r.Context(), author, markdown.Extract(section))
	embed := s.buildEmbedResolver(r.Context(), author, section, 1)
	html, err := markdown.Render(section, author, resolver, embed)
	if err != nil {
		return false
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	s.renderFragment(w, r, sectionPreview(title, noteURL(author, slug), anchor, string(html)))
	return true
}
