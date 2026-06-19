package handlers

import (
	"html/template"
	"log"
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
		tsq := buildTSQuery(q)
		if tsq != "" {
			args := []any{tsq, "%" + q + "%"}
			where := ""
			switch scope {
			case "mine":
				if viewer != nil {
					where = " AND n.author_id = $3"
					args = append(args, viewer.ID)
				}
			case "following":
				if viewer != nil {
					where = " AND n.author_id IN (SELECT followed_id FROM follows WHERE follower_id = $3)"
					args = append(args, viewer.ID)
				}
			}

			rows, err := s.DB.QueryContext(r.Context(), `
				SELECT n.title, n.slug, u.handle, n.updated_at,
				       ts_headline('simple', n.body_md, query,
				         'StartSel=<mark>, StopSel=</mark>, MaxFragments=1, MaxWords=16, MinWords=8') AS snip
				FROM notes n
				JOIN users u ON u.id = n.author_id
				CROSS JOIN to_tsquery('simple', $1) query
				WHERE (n.search_tsv @@ query OR u.handle ILIKE $2) AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`+where+`
				ORDER BY ts_rank(n.search_tsv, query) DESC
				LIMIT 50`, args...)
			if err != nil {
				log.Printf("search query: %v", err)
			} else {
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

// buildTSQuery turns a free-text query into a Postgres tsquery expression.
// Strips punctuation, then emits prefix-match terms joined by '&':
// "foo bar!" -> "foo:* & bar:*".
func buildTSQuery(q string) string {
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
		parts[i] = p + ":*"
	}
	return strings.Join(parts, " & ")
}
