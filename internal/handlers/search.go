package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"github.com/longbridgeapp/opencc"
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
		tsq := buildTSQueryVariants(q)
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

var (
	ccOnce sync.Once
	ccS2T  *opencc.OpenCC
	ccT2S  *opencc.OpenCC
)

// searchVariants returns the query plus its Simplified↔Traditional Chinese
// conversions, deduplicated, so a query in either script matches content in
// the other. Queries without Han characters are returned as-is.
func searchVariants(q string) []string {
	if !strings.ContainsFunc(q, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
		return []string{q}
	}
	ccOnce.Do(func() {
		var err error
		if ccS2T, err = opencc.New("s2t"); err != nil {
			log.Printf("opencc s2t init: %v", err)
		}
		if ccT2S, err = opencc.New("t2s"); err != nil {
			log.Printf("opencc t2s init: %v", err)
		}
	})
	variants := []string{q}
	for _, cc := range []*opencc.OpenCC{ccS2T, ccT2S} {
		if cc == nil {
			continue
		}
		v, err := cc.Convert(q)
		if err != nil || v == q || v == variants[len(variants)-1] {
			continue
		}
		variants = append(variants, v)
	}
	return variants
}

// likeAnyVariant builds a SQL clause ORing "lower(col) LIKE $n" for each
// script variant of q (Simplified/Traditional Chinese conversions), with
// placeholders starting at argOffset. Returns the clause and the args to
// append in order (each wrapped in "%...%").
func likeAnyVariant(col, q string, argOffset int) (string, []any) {
	variants := searchVariants(strings.ToLower(q))
	clauses := make([]string, len(variants))
	args := make([]any, len(variants))
	for i, v := range variants {
		clauses[i] = fmt.Sprintf("lower(%s) LIKE $%d", col, argOffset+i)
		args[i] = "%" + v + "%"
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

// buildTSQueryVariants builds a tsquery that ORs the script variants of q:
// "数论" -> "(数论:*) | (數論:*)".
func buildTSQueryVariants(q string) string {
	var exprs []string
	for _, v := range searchVariants(q) {
		if e := buildTSQuery(v); e != "" {
			exprs = append(exprs, "("+e+")")
		}
	}
	return strings.Join(exprs, " | ")
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
