package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// GetAvatarBuilder renders the part picker page, pre-populated from the
// user's saved choice if any.
func (s *Server) GetAvatarBuilder(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	choice := loadAvatarChoice(r, s.DB, u.ID)
	data := map[string]any{
		"User":   u,
		"Parts":  AvatarParts,
		"Choice": choice,
	}
	s.render(w, r, "avatar_builder", data)
}

// PostAvatarBuilder validates the four part IDs + skin color, composes the
// PNG, and writes it back to the users row.
func (s *Server) PostAvatarBuilder(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	c := AvatarChoice{
		Face:  r.FormValue("face"),
		Eyes:  r.FormValue("eyes"),
		Mouth: r.FormValue("mouth"),
		Acc:   r.FormValue("acc"),
	}
	if !validPart(c.Face, AvatarParts.Face) ||
		!validPart(c.Eyes, AvatarParts.Eyes) ||
		!validPart(c.Mouth, AvatarParts.Mouth) ||
		!validPart(c.Acc, AvatarParts.Acc) {
		s.renderError(w, r, http.StatusBadRequest, "invalid avatar selection")
		return
	}

	png, err := s.ComposeAvatar(c)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	choiceJSON, _ := json.Marshal(c)
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET avatar = $1, avatar_choice = $2 WHERE id = $3`,
		png, string(choiceJSON), u.ID,
	); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// If we came in mid-onboarding, continue that flow; otherwise return to
	// the user's own profile (which now doubles as /me).
	next := "/" + u.Handle
	if onb, _ := userOnboarded(r.Context(), s.DB, u.ID); !onb {
		next = "/onboarding"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// GetAvatarPNG serves the stored PNG blob for {handle}. Returns 404 when the
// user has not built one yet, so callers can fall back to the letter-circle.
func (s *Server) GetAvatarPNG(w http.ResponseWriter, r *http.Request) {
	handle := strings.ToLower(strings.TrimSuffix(r.PathValue("handle"), ".png"))
	if handle == "" {
		http.NotFound(w, r)
		return
	}
	var blob []byte
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT avatar FROM users WHERE handle = $1`, handle,
	).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) || len(blob) == 0 {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Never cache — users re-edit and expect to see the change immediately.
	// Files are small (~20KB) so re-fetching per page load is fine.
	w.Header().Set("Cache-Control", "no-store")
	w.Write(blob)
}

// loadAvatarChoice reads the saved JSON, or returns a sensible default
// (first option in each category, mid-skin) for the initial builder render.
func loadAvatarChoice(r *http.Request, db *sql.DB, userID uuid.UUID) AvatarChoice {
	var raw string
	db.QueryRowContext(r.Context(),
		`SELECT avatar_choice FROM users WHERE id = $1`, userID,
	).Scan(&raw)
	if raw != "" {
		var c AvatarChoice
		if json.Unmarshal([]byte(raw), &c) == nil &&
			validPart(c.Face, AvatarParts.Face) {
			return c
		}
	}
	return AvatarChoice{
		Face:  AvatarParts.Face[0],
		Eyes:  AvatarParts.Eyes[0],
		Mouth: AvatarParts.Mouth[0],
		Acc:   AvatarParts.Acc[0],
	}
}

// userHasAvatar is the cheap predicate used by gates (e.g. onboarding).
func userHasAvatar(r *http.Request, db *sql.DB, userID uuid.UUID) bool {
	var n int
	db.QueryRowContext(r.Context(),
		`SELECT length(avatar) FROM users WHERE id = $1`, userID,
	).Scan(&n)
	return n > 0
}
