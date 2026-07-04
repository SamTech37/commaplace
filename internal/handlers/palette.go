package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

type paletteItem struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Handle string `json:"handle"`
}

// GetPalette returns up to 20 notes whose title matches q for the
// Cmd+K command palette. Title-LIKE only — content-search lives in
// the existing /search page.
func (s *Server) GetPalette(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := []paletteItem{}
	if q == "" {
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	clause, args := likeAnyVariant("n.title", q, 1)
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT n.title, n.slug, u.handle
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE `+clause+`
		  AND n.hidden_at IS NULL AND n.deleted_at IS NULL
		  AND n.published_at IS NOT NULL
		ORDER BY n.updated_at DESC
		LIMIT 20`, args...)
	if err != nil {
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var it paletteItem
		var slug string
		if err := rows.Scan(&it.Title, &slug, &it.Handle); err != nil {
			continue
		}
		it.URL = noteURL(it.Handle, slug)
		out = append(out, it)
	}
	_ = json.NewEncoder(w).Encode(out)
}
