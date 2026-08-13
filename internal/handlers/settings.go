package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"commonplace/internal/auth"
)

// handleRE mirrors the URL-safe handle shape: lowercase alphanumerics and
// interior hyphens, 2–30 chars. Matches how handles are minted from emails
// (auth.handleFromEmail) so a changed handle is never something the router
// or profile URLs can't represent.
var handleRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,28}[a-z0-9])?$`)

// PostHandleSetting lets a logged-in user rename their own @handle. Links
// survive because edges resolve by UUID (links.resolved_target_id), not by
// handle — see plan.md. Uniqueness is case-folded via handle_ci.
func (s *Server) PostHandleSetting(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	next := strings.ToLower(strings.TrimSpace(r.FormValue("handle")))
	if next == u.Handle {
		http.Redirect(w, r, "/"+u.Handle, http.StatusSeeOther)
		return
	}
	if !handleRE.MatchString(next) {
		s.renderError(w, r, http.StatusBadRequest, "用戶名只能用小寫英數字與連字號（2–30 字），不能以連字號開頭或結尾。")
		return
	}
	if auth.IsReservedHandle(next) {
		s.renderError(w, r, http.StatusBadRequest, "這個用戶名是系統保留字，換一個吧。")
		return
	}

	var taken bool
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE handle_ci = $1 AND id <> $2)`,
		next, u.ID,
	).Scan(&taken); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if taken {
		s.renderError(w, r, http.StatusConflict, "這個用戶名已經有人用了。")
		return
	}

	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET handle = $1, handle_ci = $1 WHERE id = $2`, next, u.ID,
	); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/"+next, http.StatusSeeOther)
}

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
