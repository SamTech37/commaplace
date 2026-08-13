package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/auth"
	"commonplace/internal/markdown"
)

type profileNote struct {
	ID         uuid.UUID
	Title      string
	URL        string
	Excerpt    string
	UpdatedRel string
	UpdatedAt  int64
	IsDraft    bool
}

func (s *Server) GetProfile(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")

	if auth.IsReservedHandle(strings.ToLower(handle)) {
		s.renderError(w, r, http.StatusNotFound, "not found")
		return
	}

	var profile struct {
		ID     uuid.UUID
		Handle string
	}
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id, handle FROM users WHERE handle = $1`, handle,
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

	tab := r.URL.Query().Get("tab")

	viewer, _ := s.Auth.CurrentUser(r)
	var viewerID uuid.UUID
	if viewer != nil {
		viewerID = viewer.ID
	}

	recent, nextCursor, err := loadRecentNotes(r, s.DB, profile.ID, profile.Handle, viewerID, tab, olderThan)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	data := map[string]any{
		"Handle":     profile.Handle,
		"Recent":     recent,
		"NextCursor": nextCursor,
		"Tab":        tab,
	}
	if r.Header.Get("HX-Request") == "true" {
		s.Pages.RenderPartial(w, "profile", "profile-notes", data)
		return
	}

	following, _ := userFollows(r.Context(), s.DB, viewerID, profile.ID)
	followers, _ := followerCount(r.Context(), s.DB, profile.ID)
	followingN, _ := followingCount(r.Context(), s.DB, profile.ID)

	var noteCount int
	_ = s.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM notes WHERE author_id = $1 AND hidden_at IS NULL AND deleted_at IS NULL`,
		profile.ID,
	).Scan(&noteCount)

	var createdAt int64
	_ = s.DB.QueryRowContext(r.Context(),
		`SELECT created_at FROM users WHERE id = $1`,
		profile.ID,
	).Scan(&createdAt)
	estYear := 0
	if createdAt > 0 {
		estYear = time.Unix(createdAt, 0).Year()
	}

	data["Profile"] = profile
	data["Following"] = following
	data["FollowerCount"] = followers
	data["FollowingCount"] = followingN
	data["NoteCount"] = noteCount
	data["EstYear"] = estYear
	isSelf := viewer != nil && viewer.ID == profile.ID
	data["IsSelf"] = isSelf
	data["ViewerLoggedIn"] = viewer != nil
	data["Tab"] = tab
	if isSelf {
		data["Pinned"], _ = pinnedNoteForUser(r.Context(), s.DB, profile.ID)
	}
	s.render(w, r, "profile", data)
}

// loadRecentNotes lists a user's notes. Unpublished drafts are included only
// when the viewer is the author themselves.
func loadRecentNotes(r *http.Request, db *sql.DB, authorID uuid.UUID, handle string, viewerID uuid.UUID, tab string, olderThan int64) ([]profileNote, int64, error) {
	query := `SELECT id, title, slug, body_md, updated_at, published_at
		FROM notes
		WHERE author_id = $1 AND hidden_at IS NULL AND deleted_at IS NULL`
	args := []any{authorID}
	isSelf := viewerID == authorID
	if !isSelf {
		query += ` AND published_at IS NOT NULL`
	} else if tab == "drafts" {
		query += ` AND published_at IS NULL`
	} else {
		query += ` AND published_at IS NOT NULL`
	}
	if olderThan > 0 {
		args = append(args, olderThan)
		query += ` AND updated_at < $2`
	}
	query += ` ORDER BY updated_at DESC LIMIT 20`
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []profileNote
	for rows.Next() {
		var id uuid.UUID
		var title, slug, body string
		var updated int64
		var publishedAt sql.NullInt64
		if err := rows.Scan(&id, &title, &slug, &body, &updated, &publishedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, profileNote{
			ID:         id,
			Title:      title,
			URL:        noteURL(handle, slug),
			Excerpt:    markdown.Excerpt(body, 150),
			UpdatedRel: relativeTime(updated),
			UpdatedAt:  updated,
			IsDraft:    !publishedAt.Valid,
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
