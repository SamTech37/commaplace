package handlers

import (
	"context"
	"fmt"
	htmlpkg "html"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// GetWikiSuggest returns an HTML <li> fragment for autocomplete in the editor.
//
// Query routing:
//   - ""          → nothing
//   - "foo"       → note title search (own vault first, followed, then global)
//   - "@"         → user handle prefix search
//   - "@bob"      → user handle prefix search for "bob"
//   - "@bob/"     → all of bob's notes
//   - "@bob/wiki" → bob's notes whose title contains "wiki"
func (s *Server) GetWikiSuggest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if strings.HasPrefix(q, "@") {
		rest := q[1:]
		if idx := strings.Index(rest, "/"); idx >= 0 {
			viewer, _ := s.Auth.CurrentUser(r)
			viewerHandle := ""
			if viewer != nil {
				viewerHandle = viewer.Handle
			}
			s.suggestNotesForUser(r.Context(), w, rest[:idx], rest[idx+1:], viewerHandle)
		} else {
			s.suggestUsers(r.Context(), w, rest)
		}
		return
	}
	user, _ := s.Auth.CurrentUser(r)
	if user != nil && r.URL.Query().Get("desk") == "1" {
		s.suggestDeskNotes(w, r, user.ID, user.Handle, q)
		return
	}
	var (
		myID     uuid.UUID
		myHandle string
	)
	if user != nil {
		myID, myHandle = user.ID, user.Handle
	}
	s.suggestNotes(r.Context(), w, myID, myHandle, q)
}

func (s *Server) suggestUsers(ctx context.Context, w http.ResponseWriter, prefix string) {
	// handle_ci, not handle: handle preserves signup casing, handle_ci is
	// the canonical lowercase form the rest of the app resolves against.
	rows, err := s.DB.QueryContext(ctx,
		`SELECT handle FROM users WHERE handle_ci LIKE $1 ORDER BY handle LIMIT 6`,
		strings.ToLower(prefix)+"%")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var handle string
		if err := rows.Scan(&handle); err != nil {
			return
		}
		insert := "@" + handle + "/"
		fmt.Fprintf(w, `<li class="ac-item" data-insert=%q><span class="ac-primary">@%s</span><span class="ac-secondary">user</span></li>`,
			insert, htmlpkg.EscapeString(handle))
	}
}

func (s *Server) suggestNotesForUser(ctx context.Context, w http.ResponseWriter, handle, q, viewerHandle string) {
	// handle_ci, not handle: a hand-typed "[[@Bob/..." (bypassing the
	// autocomplete dropdown) should still resolve regardless of casing.
	handleCI := strings.ToLower(handle)
	// Drafts surface only in the author's own vault.
	pub := " AND n.published_at IS NOT NULL"
	if handleCI == strings.ToLower(viewerHandle) {
		pub = ""
	}
	var (
		query string
		args  []any
	)
	if q == "" {
		query = `
			SELECT n.id, n.title, n.slug, u.handle
			FROM notes n
			JOIN users u ON u.id = n.author_id
			WHERE u.handle_ci = $1 AND n.hidden_at IS NULL AND n.deleted_at IS NULL` + pub + `
			ORDER BY n.updated_at DESC LIMIT 6`
		args = []any{handleCI}
	} else {
		clause, vargs := likeAnyVariant("n.title", q, 2)
		query = `
			SELECT n.id, n.title, n.slug, u.handle
			FROM notes n
			JOIN users u ON u.id = n.author_id
			WHERE u.handle_ci = $1 AND ` + clause + ` AND n.hidden_at IS NULL AND n.deleted_at IS NULL` + pub + `
			ORDER BY n.updated_at DESC LIMIT 6`
		args = append([]any{handleCI}, vargs...)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sg wikiSuggestion
		if err := rows.Scan(&sg.NoteID, &sg.Title, &sg.Slug, &sg.Handle); err != nil {
			return
		}
		insert := buildWikiInsert(sg, viewerHandle)
		secondary := "@" + sg.Handle
		fmt.Fprintf(w, `<li class="ac-item" data-insert=%q><span class="ac-primary">%s</span><span class="ac-secondary">%s</span></li>`,
			insert, htmlpkg.EscapeString(sg.Title), htmlpkg.EscapeString(secondary))
	}
}

type wikiSuggestion struct {
	NoteID uuid.UUID
	Title  string
	Slug   string
	Handle string
}

func (s *Server) suggestNotes(ctx context.Context, w http.ResponseWriter, myID uuid.UUID, myHandle, q string) {
	seen := map[uuid.UUID]bool{}
	var results []wikiSuggestion

	collect := func(query string, args ...any) {
		if len(results) >= 10 {
			return
		}
		rows, err := s.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			if len(results) >= 10 {
				return
			}
			var sg wikiSuggestion
			if err := rows.Scan(&sg.NoteID, &sg.Title, &sg.Slug, &sg.Handle); err != nil {
				return
			}
			if seen[sg.NoteID] {
				continue
			}
			seen[sg.NoteID] = true
			results = append(results, sg)
		}
	}

	if myID != uuid.Nil {
		clause, vargs := likeAnyVariant("n.title", q, 3)
		collect(`
			SELECT n.id, n.title, n.slug, $1::text AS handle
			FROM notes n
			WHERE n.author_id = $2 AND `+clause+` AND n.hidden_at IS NULL AND n.deleted_at IS NULL
			ORDER BY n.updated_at DESC LIMIT 6`,
			append([]any{myHandle, myID}, vargs...)...)

		clause2, vargs2 := likeAnyVariant("n.title", q, 2)
		collect(`
			SELECT n.id, n.title, n.slug, u.handle
			FROM notes n
			JOIN users u   ON u.id = n.author_id
			JOIN follows f ON f.followed_id = n.author_id
			WHERE f.follower_id = $1 AND `+clause2+` AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
			ORDER BY n.updated_at DESC LIMIT 6`,
			append([]any{myID}, vargs2...)...)
	}

	clause3, vargs3 := likeAnyVariant("n.title", q, 1)
	collect(`
		SELECT n.id, n.title, n.slug, u.handle
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE `+clause3+` AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
		ORDER BY n.updated_at DESC LIMIT 6`,
		vargs3...)

	for _, sg := range results {
		insert := buildWikiInsert(sg, myHandle)
		secondary := "@" + sg.Handle
		fmt.Fprintf(w, `<li class="ac-item" data-insert=%q><span class="ac-primary">%s</span><span class="ac-secondary">%s</span></li>`,
			insert, htmlpkg.EscapeString(sg.Title), htmlpkg.EscapeString(secondary))
	}
}

func buildWikiInsert(s wikiSuggestion, myHandle string) string {
	if s.Handle == myHandle {
		return s.Slug
	}
	return "@" + s.Handle + "/" + s.Slug
}

// One bounded query: visible desk materials, then saved articles, own notes,
// followed authors, and other public notes. Private notes never leak across vaults.
func (s *Server) suggestDeskNotes(w http.ResponseWriter, r *http.Request, userID uuid.UUID, handle, q string) {
	if len(q) > 300 {
		http.Error(w, "查詢過長", 400)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	ids := []string{}
	for _, raw := range strings.Split(r.URL.Query().Get("notes"), ",") {
		if len(ids) >= 64 {
			break
		}
		if id, err := uuid.Parse(raw); err == nil {
			ids = append(ids, id.String())
		}
	}
	clause, args := likeAnyVariant("n.title", q, 3)
	rows, err := s.DB.QueryContext(r.Context(), `SELECT n.id,n.title,n.slug,u.handle FROM notes n JOIN users u ON u.id=n.author_id WHERE n.deleted_at IS NULL AND n.hidden_at IS NULL AND (n.author_id=$1 OR n.published_at IS NOT NULL) AND `+clause+` ORDER BY (n.id=ANY($2::uuid[])) DESC, EXISTS(SELECT 1 FROM saves WHERE user_id=$1 AND note_id=n.id) DESC,(n.author_id=$1) DESC,EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND followed_id=n.author_id) DESC,n.updated_at DESC,n.id DESC LIMIT 10`, append([]any{userID, ids}, args...)...)
	if err != nil {
		http.Error(w, "無法取得 Wiki 建議", 500)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sg wikiSuggestion
		if err := rows.Scan(&sg.NoteID, &sg.Title, &sg.Slug, &sg.Handle); err != nil {
			return
		}
		fmt.Fprintf(w, `<li class="ac-item" data-insert="%s"><span class="ac-primary">%s</span><span class="ac-secondary">%s</span></li>`, htmlpkg.EscapeString(buildWikiInsert(sg, handle)), htmlpkg.EscapeString(sg.Title), htmlpkg.EscapeString("@"+sg.Handle))
	}
}
