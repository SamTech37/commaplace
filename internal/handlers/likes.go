package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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
	noteID, err := strconv.ParseInt(r.PostFormValue("note_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad note id", http.StatusBadRequest)
		return
	}

	// Toggle: try insert; if already there, delete.
	res, err := s.DB.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO likes(user_id, note_id, created_at) VALUES(?, ?, ?)`,
		u.ID, noteID, nowUnix())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inserted, _ := res.RowsAffected()
	liked := inserted == 1
	if !liked {
		if _, err := s.DB.ExecContext(r.Context(),
			`DELETE FROM likes WHERE user_id = ? AND note_id = ?`, u.ID, noteID,
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
func writeHeartFragment(w http.ResponseWriter, noteID int64, liked bool, count int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cls := "heart"
	icon := "♡"
	if liked {
		cls += " liked"
		icon = "♥"
	}
	fmt.Fprintf(w, `<form class="inline-form" hx-post="/api/like" hx-target="this" hx-swap="outerHTML">
  <input type="hidden" name="note_id" value="%d">
  <button type="submit" class="%s" aria-pressed="%t">%s <span class="count">%d</span></button>
</form>`, noteID, cls, liked, icon, count)
}

func likeCount(ctx context.Context, db *sql.DB, noteID int64) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM likes WHERE note_id = ?`, noteID,
	).Scan(&n)
	return n, err
}

func userHasLiked(ctx context.Context, db *sql.DB, userID, noteID int64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM likes WHERE user_id = ? AND note_id = ?`, userID, noteID,
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
		SELECT n.title, n.folder_path, n.slug, n.body_md, n.updated_at, u2.handle
		FROM likes l
		JOIN notes n  ON n.id = l.note_id
		JOIN users u2 ON u2.id = n.author_id
		WHERE l.user_id = ? AND n.hidden_at IS NULL
		ORDER BY l.created_at DESC
		LIMIT 200`, u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var items []feedItem
	for rows.Next() {
		var (
			title, folderPath, slug, body, handle string
			updated                                int64
		)
		if err := rows.Scan(&title, &folderPath, &slug, &body, &updated, &handle); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, feedItem{
			Title:        title,
			URL:          noteURL(handle, folderPath, slug),
			Excerpt:      markdown.Excerpt(body, 150),
			AuthorHandle: handle,
			FolderPath:   folderPath,
			UpdatedRel:   relativeTime(updated),
		})
	}
	s.render(w, r, "saved", map[string]any{"Items": items})
}
