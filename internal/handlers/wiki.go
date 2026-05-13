package handlers

import (
	"context"
	"database/sql"
	"fmt"
	htmlpkg "html"
	"net/http"
	"strings"
)

// GetWikiSuggest returns an HTML <li> fragment for autocomplete in the editor.
//
// Ranking when q has no @ prefix:
//  1. notes I authored
//  2. notes by users I follow
//  3. site-wide, most recently updated
//
// When q starts with @, suggests user handles instead.
func (s *Server) GetWikiSuggest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		return
	}
	if strings.HasPrefix(q, "@") {
		s.suggestUsers(r.Context(), w, q[1:])
		return
	}
	user, _ := s.Auth.CurrentUser(r)
	var (
		myID     int64
		myHandle string
	)
	if user != nil {
		myID, myHandle = user.ID, user.Handle
	}
	s.suggestNotes(r.Context(), w, myID, myHandle, q)
}

func (s *Server) suggestUsers(ctx context.Context, w http.ResponseWriter, prefix string) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT handle FROM users WHERE handle LIKE ? ORDER BY handle LIMIT 10`,
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

type wikiSuggestion struct {
	NoteID int64
	Title  string
	Folder string
	Slug   string
	Handle string
}

func (s *Server) suggestNotes(ctx context.Context, w http.ResponseWriter, myID int64, myHandle, q string) {
	pattern := "%" + strings.ToLower(q) + "%"
	seen := map[int64]bool{}
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
			if err := rows.Scan(&sg.NoteID, &sg.Title, &sg.Folder, &sg.Slug, &sg.Handle); err != nil {
				return
			}
			if seen[sg.NoteID] {
				continue
			}
			seen[sg.NoteID] = true
			results = append(results, sg)
		}
	}

	if myID != 0 {
		collect(`
			SELECT n.id, n.title, n.folder_path, n.slug, ? AS handle
			FROM notes n
			WHERE n.author_id = ? AND lower(n.title) LIKE ? AND n.hidden_at IS NULL
			ORDER BY n.updated_at DESC LIMIT 10`,
			myHandle, myID, pattern)

		collect(`
			SELECT n.id, n.title, n.folder_path, n.slug, u.handle
			FROM notes n
			JOIN users u   ON u.id = n.author_id
			JOIN follows f ON f.followed_id = n.author_id
			WHERE f.follower_id = ? AND lower(n.title) LIKE ? AND n.hidden_at IS NULL
			ORDER BY n.updated_at DESC LIMIT 10`,
			myID, pattern)
	}

	collect(`
		SELECT n.id, n.title, n.folder_path, n.slug, u.handle
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE lower(n.title) LIKE ? AND n.hidden_at IS NULL
		ORDER BY n.updated_at DESC LIMIT 20`,
		pattern)

	for _, sg := range results {
		insert := buildWikiInsert(sg, myHandle)
		secondary := "@" + sg.Handle
		if sg.Folder != "" {
			secondary += "/" + sg.Folder
		}
		fmt.Fprintf(w, `<li class="ac-item" data-insert=%q><span class="ac-primary">%s</span><span class="ac-secondary">%s</span></li>`,
			insert, htmlpkg.EscapeString(sg.Title), htmlpkg.EscapeString(secondary))
	}
}

// buildWikiInsert produces the payload between [[ and ]] when this suggestion
// is picked: own notes are same-vault, others get the @user prefix.
func buildWikiInsert(s wikiSuggestion, myHandle string) string {
	if s.Handle == myHandle {
		if s.Folder != "" {
			return s.Folder + "/" + s.Slug
		}
		return s.Slug
	}
	if s.Folder != "" {
		return "@" + s.Handle + "/" + s.Folder + "/" + s.Slug
	}
	return "@" + s.Handle + "/" + s.Slug
}

// satisfy `import "database/sql"` if all consumers are inlined above.
var _ = sql.ErrNoRows
