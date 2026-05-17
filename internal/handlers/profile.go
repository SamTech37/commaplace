package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"commonplace/internal/auth"
	"commonplace/internal/markdown"
)

type profileNote struct {
	Title      string
	URL        string
	Excerpt    string
	UpdatedRel string
	UpdatedAt  int64
}

func (s *Server) GetProfile(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")

	if auth.IsReservedHandle(strings.ToLower(handle)) {
		s.renderError(w, r, http.StatusNotFound, "not found")
		return
	}

	var profile struct {
		ID     int64
		Handle string
	}
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id, handle FROM users WHERE handle = ?`, handle,
	).Scan(&profile.ID, &profile.Handle)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "no such user")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var olderThan int64
	if s := r.URL.Query().Get("older"); s != "" {
		olderThan, _ = strconv.ParseInt(s, 10, 64)
	}

	recent, nextCursor, err := loadRecentNotes(r, s.DB, profile.ID, olderThan)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	data := map[string]any{
		"Handle":     profile.Handle,
		"Recent":     recent,
		"NextCursor": nextCursor,
	}
	if r.URL.Query().Get("partial") == "1" {
		s.Pages.RenderPartial(w, "profile", "profile-notes", data)
		return
	}

	viewer, _ := s.Auth.CurrentUser(r)
	var viewerID int64
	if viewer != nil {
		viewerID = viewer.ID
	}
	following, _ := userFollows(r.Context(), s.DB, viewerID, profile.ID)
	followers, _ := followerCount(r.Context(), s.DB, profile.ID)
	followingN, _ := followingCount(r.Context(), s.DB, profile.ID)

	data["Profile"] = profile
	data["Following"] = following
	data["FollowerCount"] = followers
	data["FollowingCount"] = followingN
	isSelf := viewer != nil && viewer.ID == profile.ID
	data["IsSelf"] = isSelf
	data["ViewerLoggedIn"] = viewer != nil
	if isSelf {
		data["Email"] = viewer.Email
		data["Pinned"], _ = pinnedNoteForUser(r.Context(), s.DB, profile.ID)
	}
	s.render(w, r, "profile", data)
}

func loadRecentNotes(r *http.Request, db *sql.DB, authorID, olderThan int64) ([]profileNote, int64, error) {
	var (
		query string
		args  []any
	)
	if olderThan > 0 {
		query = `SELECT title, slug, body_md, updated_at
		FROM notes
		WHERE author_id = ? AND hidden_at IS NULL AND deleted_at IS NULL AND updated_at < ?
		ORDER BY updated_at DESC LIMIT 20`
		args = []any{authorID, olderThan}
	} else {
		query = `SELECT title, slug, body_md, updated_at
		FROM notes
		WHERE author_id = ? AND hidden_at IS NULL AND deleted_at IS NULL
		ORDER BY updated_at DESC LIMIT 20`
		args = []any{authorID}
	}
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var handle string
	if err := db.QueryRowContext(r.Context(),
		`SELECT handle FROM users WHERE id = ?`, authorID,
	).Scan(&handle); err != nil {
		return nil, 0, err
	}

	var out []profileNote
	for rows.Next() {
		var title, slug, body string
		var updated int64
		if err := rows.Scan(&title, &slug, &body, &updated); err != nil {
			return nil, 0, err
		}
		out = append(out, profileNote{
			Title:      title,
			URL:        noteURL(handle, slug),
			Excerpt:    markdown.Excerpt(body, 150),
			UpdatedRel: relativeTime(updated),
			UpdatedAt:  updated,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var nextCursor int64
	if len(out) == 20 {
		nextCursor = out[len(out)-1].UpdatedAt
	}
	return out, nextCursor, nil
}
