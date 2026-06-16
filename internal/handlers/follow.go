package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// PostFollow toggles a follow row from the current user to target user_id.
// Returns the follow button + count fragment for HTMX swap.
func (s *Server) PostFollow(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	targetID, err := uuid.Parse(r.PostFormValue("user_id"))
	if err != nil || targetID == u.ID {
		http.Error(w, "bad target", http.StatusBadRequest)
		return
	}

	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO follows(follower_id, followed_id, created_at) VALUES($1, $2, $3) ON CONFLICT (follower_id, followed_id) DO NOTHING`,
		u.ID, targetID, nowUnix())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inserted, _ := res.RowsAffected()
	following := inserted == 1
	if !following {
		if _, err := s.DB.ExecContext(r.Context(),
			`DELETE FROM follows WHERE follower_id = $1 AND followed_id = $2`,
			u.ID, targetID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	followers, _ := followerCount(r.Context(), s.DB, targetID)
	writeFollowFragment(w, targetID, following, followers)
}

func writeFollowFragment(w http.ResponseWriter, targetID uuid.UUID, following bool, followers int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cls := "follow-btn"
	label := "Follow"
	if following {
		cls += " following"
		label = "Following"
	}
	fmt.Fprintf(w, `<form class="inline-form follow-form" hx-post="/api/follow" hx-target="this" hx-swap="outerHTML">
  <input type="hidden" name="user_id" value="%s">
  <button type="submit" class="%s" aria-pressed="%t">%s</button>
  <span class="follower-count"> · %d follower%s</span>
</form>`, targetID, cls, following, label, followers, plural(followers))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func followerCount(ctx context.Context, db *sql.DB, userID uuid.UUID) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM follows WHERE followed_id = $1`, userID).Scan(&n)
	return n, err
}

func followingCount(ctx context.Context, db *sql.DB, userID uuid.UUID) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM follows WHERE follower_id = $1`, userID).Scan(&n)
	return n, err
}

func userFollows(ctx context.Context, db *sql.DB, follower, followed uuid.UUID) (bool, error) {
	if follower == uuid.Nil {
		return false, nil
	}
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM follows WHERE follower_id = $1 AND followed_id = $2`,
		follower, followed,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
