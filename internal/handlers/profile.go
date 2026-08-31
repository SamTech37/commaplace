package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"commonplace/internal/auth"
	"commonplace/internal/markdown"
)

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

	view := NoteListView{
		Cards:    recent,
		OlderURL: profileOlderURL(profile.Handle, nextCursor, tab),
		Empty:    profileEmpty(tab),
	}
	if r.Header.Get("HX-Request") == "true" {
		s.renderFragment(w, r, notesFragment(view))
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

	isSelf := viewer != nil && viewer.ID == profile.ID
	var pinned *pinnedNote
	if isSelf {
		pinned, _ = pinnedNoteForUser(r.Context(), s.DB, profile.ID)
	}

	s.renderPage(w, r, pageTitle("@"+profile.Handle), "", nil, profilePage(ProfilePageProps{
		Handle:         profile.Handle,
		ProfileID:      profile.ID,
		View:           view,
		Tab:            tab,
		IsSelf:         isSelf,
		ViewerLoggedIn: viewer != nil,
		Following:      following,
		FollowerCount:  followers,
		FollowingCount: followingN,
		NoteCount:      noteCount,
		EstYear:        estYear,
		Pinned:         pinned,
		BulkDelete:     isSelf && tab == "drafts",
	}))
}

// profileOlderURL builds the infinite-scroll cursor URL, or "" when there's
// no next page (nextCursor == 0, same sentinel loadRecentNotes already uses).
func profileOlderURL(handle string, nextCursor int64, tab string) string {
	if nextCursor == 0 {
		return ""
	}
	url := "/" + handle + "?older=" + strconv.FormatInt(nextCursor, 10)
	if tab != "" {
		url += "&tab=" + tab
	}
	return url
}

func profileEmpty(tab string) templ.Component {
	if tab == "drafts" {
		return emptyText("還沒有草稿。")
	}
	return emptyText("還沒有筆記。")
}

// loadRecentNotes lists a user's notes as feedCards (the shared card model —
// see notes_view.templ). Unpublished drafts are included only when the
// viewer is the author themselves.
func loadRecentNotes(r *http.Request, db *sql.DB, authorID uuid.UUID, handle string, viewerID uuid.UUID, tab string, olderThan int64) ([]feedCard, int64, error) {
	query := `SELECT n.id, n.title, n.slug, n.body_md, n.updated_at, n.published_at,
		       (SELECT COUNT(*) FROM likes WHERE note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id AND target_user_id IS DISTINCT FROM n.author_id)
		FROM notes n
		WHERE n.author_id = $1 AND n.hidden_at IS NULL AND n.deleted_at IS NULL`
	args := []any{authorID}
	isSelf := viewerID == authorID
	if !isSelf {
		query += ` AND n.published_at IS NOT NULL`
	} else if tab == "drafts" {
		query += ` AND n.published_at IS NULL`
	} else {
		query += ` AND n.published_at IS NOT NULL`
	}
	if olderThan > 0 {
		args = append(args, olderThan)
		query += ` AND n.updated_at < $2`
	}
	query += ` ORDER BY n.updated_at DESC LIMIT 20`
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []feedCard
	for rows.Next() {
		var (
			c           feedCard
			slug, body  string
			publishedAt sql.NullInt64
		)
		if err := rows.Scan(&c.NoteID, &c.Title, &slug, &body, &c.UpdatedAt, &publishedAt,
			&c.LikeCount, &c.LinkCount, &c.CrossCount); err != nil {
			return nil, 0, err
		}
		c.AuthorHandle = handle
		c.URL = noteURL(handle, slug)
		c.UpdatedRel = relativeTime(c.UpdatedAt)
		c.IsDraft = !publishedAt.Valid
		c.Variant, c.Excerpt, c.ListItems, c.Quote, c.LinkChips = analyzeCardBody(body)
		c.ImageURL = markdown.FirstImageURL(body)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	attachTagsToCards(r.Context(), db, out)
	var nextCursor int64
	if len(out) == 20 {
		nextCursor = out[len(out)-1].UpdatedAt
	}
	return out, nextCursor, nil
}
