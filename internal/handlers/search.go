package handlers

import (
	"html/template"
	"net/http"
	"strings"
	"unicode"
)

type searchHit struct {
	Title        string
	URL          string
	AuthorHandle string
	UpdatedRel   string
	Snippet      template.HTML // contains <mark>…</mark>
}

func (s *Server) GetSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	scope := r.URL.Query().Get("scope") // "" | "mine" | "following"
	viewer, _ := s.Auth.CurrentUser(r)

	var hits []searchHit
	if q != "" {
		fts := buildFTSQuery(q)
		if fts != "" {
			args := []any{fts}
			where := ""
			switch scope {
			case "mine":
				if viewer != nil {
					where = " AND n.author_id = ?"
					args = append(args, viewer.ID)
				}
			case "following":
				if viewer != nil {
					where = " AND n.author_id IN (SELECT followed_id FROM follows WHERE follower_id = ?)"
					args = append(args, viewer.ID)
				}
			}

			rows, err := s.DB.QueryContext(r.Context(), `
				SELECT n.title, n.slug, u.handle, n.updated_at,
				       snippet(notes_fts, 1, '<mark>', '</mark>', '…', 16) AS snip
				FROM notes_fts
				JOIN notes n ON n.id = notes_fts.rowid
				JOIN users u ON u.id = n.author_id
				WHERE notes_fts MATCH ? AND n.hidden_at IS NULL AND n.deleted_at IS NULL`+where+`
				ORDER BY bm25(notes_fts, 2.0, 1.0)
				LIMIT 50`, args...)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var title, slug, handle, snip string
					var updated int64
					if err := rows.Scan(&title, &slug, &handle, &updated, &snip); err != nil {
						break
					}
					hits = append(hits, searchHit{
						Title:        title,
						URL:          noteURL(handle, slug),
						AuthorHandle: handle,
						UpdatedRel:   relativeTime(updated),
						Snippet:      template.HTML(snip),
					})
				}
			}
		}
	}

	s.render(w, r, "search", map[string]any{
		"Query":          q,
		"Scope":          scope,
		"Hits":           hits,
		"ViewerLoggedIn": viewer != nil,
		"SearchQuery":    q,
	})
}

// buildFTSQuery turns a free-text query into an FTS5 expression. Strips
// punctuation that FTS5 treats as syntax, then quotes each term and adds
// '*' for prefix matching: "foo bar!" -> `"foo"* "bar"*`.
func buildFTSQuery(q string) string {
	var b strings.Builder
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	parts := strings.Fields(b.String())
	if len(parts) == 0 {
		return ""
	}
	for i, p := range parts {
		parts[i] = `"` + p + `"*`
	}
	return strings.Join(parts, " ")
}
