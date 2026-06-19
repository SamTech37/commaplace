package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"commonplace/internal/markdown"
)

// PostLike toggles a like row for the current user on the given note.
// Returns the heart+count fragment so HTMX can swap it inline.
func (s *Server) PostLike(w http.ResponseWriter, r *http.Request) {
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

	// Toggle: try insert; if already there, delete.
	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO likes(user_id, note_id, created_at) VALUES($1, $2, $3) ON CONFLICT (user_id, note_id) DO NOTHING`,
		u.ID, noteID, nowUnix())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inserted, _ := res.RowsAffected()
	liked := inserted == 1
	if !liked {
		if _, err := s.DB.ExecContext(r.Context(),
			`DELETE FROM likes WHERE user_id = $1 AND note_id = $2`, u.ID, noteID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	count, _ := likeCount(r.Context(), s.DB, noteID)
	writeHeartFragment(w, noteID, liked, count)
}

// writeHeartFragment renders the heart button + count. The button toggles
// itself when clicked.
func writeHeartFragment(w http.ResponseWriter, noteID uuid.UUID, liked bool, count int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cls := "heart"
	icon := "♡"
	if liked {
		cls += " liked"
		icon = "♥"
	}
	fmt.Fprintf(w, `<form class="inline-form" hx-post="/api/like" hx-target="this" hx-swap="outerHTML">
  <input type="hidden" name="note_id" value="%s">
  <button type="submit" class="%s" aria-pressed="%t">%s <span class="count">%d</span></button>
</form>`, noteID, cls, liked, icon, count)
}

func likeCount(ctx context.Context, db *sql.DB, noteID uuid.UUID) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM likes WHERE note_id = $1`, noteID,
	).Scan(&n)
	return n, err
}

func userHasLiked(ctx context.Context, db *sql.DB, userID, noteID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM likes WHERE user_id = $1 AND note_id = $2`, userID, noteID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetSaved lists notes the current user has liked, newest like first.
func (s *Server) GetSaved(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT n.title, n.slug, n.body_md, n.updated_at, u2.handle
		FROM likes l
		JOIN notes n  ON n.id = l.note_id
		JOIN users u2 ON u2.id = n.author_id
		WHERE l.user_id = $1 AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
		ORDER BY l.created_at DESC
		LIMIT 200`, u.ID)
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
	s.render(w, r, "saved", map[string]any{"Items": items})
}
