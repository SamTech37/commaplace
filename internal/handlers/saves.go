package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// PostSave toggles a private "save for later" bookmark. Distinct from a like:
// likes are a public signal + count; saves are the reader's private reading
// list, surfaced at /me/saved.
func (s *Server) PostSave(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	noteID, err := uuid.Parse(r.PostFormValue("note_id"))
	if err != nil {
		http.Error(w, "bad note id", http.StatusBadRequest)
		return
	}

	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO saves(user_id, note_id, created_at) VALUES($1, $2, $3) ON CONFLICT (user_id, note_id) DO NOTHING`,
		u.ID, noteID, nowUnix())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inserted, _ := res.RowsAffected()
	saved := inserted == 1
	if !saved {
		if _, err := s.DB.ExecContext(r.Context(),
			`DELETE FROM saves WHERE user_id = $1 AND note_id = $2`, u.ID, noteID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeSaveFragment(w, noteID, saved)
}

// writeSaveFragment renders the bookmark button; it self-replaces on toggle.
func writeSaveFragment(w http.ResponseWriter, noteID uuid.UUID, saved bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cls := "action-btn save-btn"
	label, icon := "收藏", "＋"
	if saved {
		cls += " saved"
		label, icon = "已收藏", "✓"
	}
	fmt.Fprintf(w, `<form class="inline-form" hx-post="/api/save" hx-target="this" hx-swap="outerHTML">
  <input type="hidden" name="note_id" value="%s">
  <button type="submit" class="%s" aria-pressed="%t">%s %s</button>
</form>`, noteID, cls, saved, icon, label)
}

func userHasSaved(ctx context.Context, db *sql.DB, userID, noteID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM saves WHERE user_id = $1 AND note_id = $2)`,
		userID, noteID,
	).Scan(&exists)
	return exists, err
}
