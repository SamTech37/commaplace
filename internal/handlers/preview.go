package handlers

import (
	"database/sql"
	"net/http"

	"commonplace/internal/markdown"
)

// GetNotePreview serves the hover-preview card for an internal note link.
//
// It returns the same feedCard the feed renders, through the same
// masonry_card template — so a list-shaped note previews as a list, and the
// card follows the view layer whenever that moves.
func (s *Server) GetNotePreview(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")
	slug := r.PathValue("slug")

	// Columns match scanCards so the row lands straight in a feedCard.
	// Published-only, gated in SQL: the handler stays viewer-independent,
	// which is what makes the response cacheable.
	var (
		c    feedCard
		body string
	)
	err := s.DB.QueryRowContext(r.Context(), `
		SELECT n.id, n.title, n.body_md, n.updated_at,
		       (SELECT COUNT(*) FROM likes WHERE note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id AND target_user_id IS DISTINCT FROM u.id)
		FROM notes n JOIN users u ON u.id = n.author_id
		WHERE u.handle = $1 AND n.slug = $2
		  AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`,
		handle, slug,
	).Scan(&c.NoteID, &c.Title, &body, &c.UpdatedAt, &c.LikeCount, &c.LinkCount, &c.CrossCount)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	c.AuthorHandle = handle
	c.URL = noteURL(handle, slug)
	c.UpdatedRel = relativeTime(c.UpdatedAt)
	c.Variant, c.Excerpt, c.ListItems, c.Quote, c.LinkChips = analyzeCardBody(body)
	c.ImageURL = markdown.FirstImageURL(body)
	cards := []feedCard{c} // attachTagsToCards fills in place, so hand it a slice
	attachTagsToCards(r.Context(), s.DB, cards)

	w.Header().Set("Cache-Control", "private, max-age=60")
	s.renderFragment(w, r, masonryCard(cards[0]))
}
