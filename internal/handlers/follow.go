package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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
	targetID, err := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
	if err != nil || targetID == u.ID {
		http.Error(w, "bad target", http.StatusBadRequest)
		return
	}

	res, err := s.DB.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO follows(follower_id, followed_id, created_at) VALUES(?, ?, ?)`,
		u.ID, targetID, nowUnix())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inserted, _ := res.RowsAffected()
	following := inserted == 1
	if !following {
		if _, err := s.DB.ExecContext(r.Context(),
			`DELETE FROM follows WHERE follower_id = ? AND followed_id = ?`,
			u.ID, targetID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	followers, _ := followerCount(r.Context(), s.DB, targetID)
	writeFollowFragment(w, targetID, following, followers)
}

func writeFollowFragment(w http.ResponseWriter, targetID int64, following bool, followers int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cls := "follow-btn"
	label := "Follow"
	if following {
		cls += " following"
		label = "Following"
	}
	fmt.Fprintf(w, `<form class="inline-form follow-form" hx-post="/api/follow" hx-target="this" hx-swap="outerHTML">
  <input type="hidden" name="user_id" value="%d">
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

func followerCount(ctx context.Context, db *sql.DB, userID int64) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM follows WHERE followed_id = ?`, userID).Scan(&n)
	return n, err
}

func followingCount(ctx context.Context, db *sql.DB, userID int64) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM follows WHERE follower_id = ?`, userID).Scan(&n)
	return n, err
}

func userFollows(ctx context.Context, db *sql.DB, follower, followed int64) (bool, error) {
	if follower == 0 {
		return false, nil
	}
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM follows WHERE follower_id = ? AND followed_id = ?`,
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
