package handlers

import "net/http"

// GetRandom redirects to a random published note — the "I'm feeling lucky" /
// surprise-me jump. ORDER BY random() is fine at this scale; revisit if the
// notes table ever gets large enough for it to matter. Falls back to the feed
// when there's nothing to jump to.
func (s *Server) GetRandom(w http.ResponseWriter, r *http.Request) {
	var handle, slug string
	err := s.DB.QueryRowContext(r.Context(), `
		SELECT u.handle, n.slug
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE n.published_at IS NOT NULL
		  AND n.hidden_at IS NULL
		  AND n.deleted_at IS NULL
		ORDER BY random()
		LIMIT 1`).Scan(&handle, &slug)
	if err != nil {
		http.Redirect(w, r, "/feed", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, noteURL(handle, slug), http.StatusSeeOther)
}
