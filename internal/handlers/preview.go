package handlers

import (
	"fmt"
	"net/http"
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

	// Published-only, gated in SQL: the handler stays viewer-independent,
	// which is what makes the response cacheable.
	rows, err := s.DB.QueryContext(r.Context(), fmt.Sprintf(`
		SELECT %s
		FROM notes n JOIN users u ON u.id = n.author_id
		WHERE u.handle = $1 AND n.slug = $2
		  AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`, noteCardColumns),
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
