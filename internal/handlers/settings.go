package handlers

import "net/http"

// PostThemeSetting persists a logged-in user's theme preference. Visitors
// get 204 — they store the choice in localStorage client-side.
func (s *Server) PostThemeSetting(w http.ResponseWriter, r *http.Request) {
	v := r.FormValue("theme")
	if v != "auto" && v != "light" && v != "dark" {
		http.Error(w, "bad theme", http.StatusBadRequest)
		return
	}
	u, _ := s.Auth.CurrentUser(r)
	if u == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET theme = $1 WHERE id = $2`, v, u.ID,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
