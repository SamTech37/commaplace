package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"commonplace/internal/auth"
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

	viewer, _ := s.Auth.CurrentUser(r)
	var viewerID uuid.UUID
	if viewer != nil {
		viewerID = viewer.ID
	}
	isSelf := viewer != nil && viewer.ID == profile.ID

	mode := r.URL.Query().Get("view")
	if mode != "calendar" {
		mode = "timeline"
	}

	var (
		view       NoteListView
		tab        string
		bulkDelete bool
		calendar   *calendarGridProps
	)
	if mode == "calendar" {
		grid, err := s.buildCalendarGrid(r.Context(), profile.ID, r.URL.Query().Get("m"), isSelf)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		calendar = &grid
	} else {
		var olderThan int64
		if s := r.URL.Query().Get("older"); s != "" {
			olderThan, _ = strconv.ParseInt(s, 10, 64)
		}
		tab = r.URL.Query().Get("tab")

		recent, nextCursor, err := loadRecentNotes(r, s.DB, profile.ID, profile.Handle, viewerID, tab, olderThan)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}

		view = NoteListView{
			Cards:       recent,
			Layout:      "list",
			OlderURL:    profileOlderURL(profile.Handle, nextCursor, tab),
			Empty:       profileEmpty(tab),
			GroupByDate: true,
		}
		bulkDelete = isSelf && tab == "drafts"
		if r.Header.Get("HX-Request") == "true" {
			s.renderFragment(w, r, notesFragment(view))
			return
		}
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

	var pinned *pinnedNote
	if isSelf {
		pinned, _ = pinnedNoteForUser(r.Context(), s.DB, profile.ID)
	}

	s.renderPage(w, r, pageTitle("@"+profile.Handle), "", nil, profilePage(ProfilePageProps{
		Handle:         profile.Handle,
		ProfileID:      profile.ID,
		Mode:           mode,
		View:           view,
		Calendar:       calendar,
		Tab:            tab,
		IsSelf:         isSelf,
		ViewerLoggedIn: viewer != nil,
		Following:      following,
		FollowerCount:  followers,
		FollowingCount: followingN,
		NoteCount:      noteCount,
		EstYear:        estYear,
		Pinned:         pinned,
		BulkDelete:     bulkDelete,
	}))
}

// profileOlderURL builds the infinite-scroll cursor URL, or "" when there's
// no next page (nextCursor == 0, same sentinel loadRecentNotes already uses).
func profileOlderURL(handle string, nextCursor int64, tab string) string {
	if nextCursor == 0 {
		return ""
	}
	href := "/" + handle + "?older=" + strconv.FormatInt(nextCursor, 10)
	if tab != "" {
		href += "&tab=" + url.QueryEscape(tab)
	}
	return href
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
	query := `SELECT ` + noteCardColumns + `
		FROM notes n
		JOIN users u ON u.id = n.author_id
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
	out, err := scanCards(rows)
	if err != nil {
		return nil, 0, err
	}
	// AuthorHandle blanked: it's always this profile's own handle, redundant
	// on your own vault page (listCard omits it when empty).
	for i := range out {
		out[i].AuthorHandle = ""
	}
	var nextCursor int64
	if len(out) == 20 {
		nextCursor = out[len(out)-1].UpdatedAt
	}
	return out, nextCursor, nil
}
