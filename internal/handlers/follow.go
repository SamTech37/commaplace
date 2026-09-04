package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
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

	// The button replaces itself; the count lives elsewhere on the page and
	// rides along out-of-band. Both render from the same templ components the
	// pages use, so the swapped-in markup matches what was there before.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = followForm(targetID, following).Render(r.Context(), w)
	_ = followCount("followers", followers, templ.Attributes{"hx-swap-oob": "outerHTML"}).Render(r.Context(), w)
}

// followCountText is the wording for a follow count. One function, so the
// page and the out-of-band swap can't word the same number differently.
func followCountText(rel string, n int) string {
	if rel == "following" {
		return strconv.Itoa(n) + " 追蹤中"
	}
	return strconv.Itoa(n) + " 追蹤者"
}

// GetFollowList renders one follow list as a fragment for the count dropdown.
// rel is "followers" (who follows this user) or "following" (who they follow).
func (s *Server) GetFollowList(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")
	rel := r.URL.Query().Get("rel")
	if rel != "followers" && rel != "following" {
		http.Error(w, "bad rel", http.StatusBadRequest)
		return
	}

	var profileID uuid.UUID
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id FROM users WHERE handle = $1`, handle).Scan(&profileID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "no such user", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	viewer, _ := s.Auth.CurrentUser(r)
	var viewerID uuid.UUID
	if viewer != nil {
		viewerID = viewer.ID
	}

	rows, err := followRows(r.Context(), s.DB, profileID, viewerID, rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, r, followList(rows, rel, viewer != nil, viewerID))
}

// followRows lists one side of the follow graph, newest first. Both directions
// hit an existing index (idx_follows_followed / idx_follows_follower_created).
// No pagination: a hard cap is the whole of it at this scale.
func followRows(ctx context.Context, db *sql.DB, profileID, viewerID uuid.UUID, rel string) ([]followRow, error) {
	join, filter := "f.follower_id", "f.followed_id"
	if rel == "following" {
		join, filter = "f.followed_id", "f.follower_id"
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT u.id, u.handle,
		       EXISTS (SELECT 1 FROM follows v WHERE v.follower_id = $2 AND v.followed_id = u.id)
		FROM follows f JOIN users u ON u.id = %s
		WHERE %s = $1
		ORDER BY f.created_at DESC LIMIT 100`, join, filter), profileID, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []followRow
	for rows.Next() {
		var fr followRow
		if err := rows.Scan(&fr.ID, &fr.Handle, &fr.Following); err != nil {
			return nil, err
		}
		out = append(out, fr)
	}
	return out, rows.Err()
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
