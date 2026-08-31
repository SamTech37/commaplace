package handlers

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/a-h/templ"
	"github.com/google/uuid"
	"github.com/longbridgeapp/opencc"
)

func (s *Server) GetSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	scope := r.URL.Query().Get("scope") // "" | "mine" | "following"
	viewer, _ := s.Auth.CurrentUser(r)

	var cards []feedCard
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
				SELECT n.id, n.title, n.slug, u.handle, n.updated_at,
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
					var id uuid.UUID
					var title, slug, handle, snip string
					var updated int64
					if err := rows.Scan(&id, &title, &slug, &handle, &updated, &snip); err != nil {
						break
					}
					cards = append(cards, feedCard{
						NoteID:       id,
						Title:        title,
						URL:          noteURL(handle, slug),
						AuthorHandle: handle,
						UpdatedAt:    updated,
						UpdatedRel:   relativeTime(updated),
						SnippetHTML:  searchSnippet(snip),
					})
				}
			}
		}
	}

	title := pageTitle("搜尋")
	if q != "" {
		title = pageTitle(q + " · 搜尋")
	}
	s.renderPageWithQuery(w, r, title, "", q, nil, searchPage(SearchPageProps{
		Query:          q,
		Scope:          scope,
		ViewerLoggedIn: viewer != nil,
		View: NoteListView{
			Cards: cards,
			Empty: emptyText("沒有符合的結果。"),
		},
	}))
}

// searchSnippet escapes ts_headline's raw output for safe HTML rendering.
// ts_headline returns the original note body text (arbitrary, user-authored
// markdown) with literal <mark>/</mark> inserted around matched terms — it
// does NOT escape the surrounding text. Rendering that unescaped (as the old
// search.html did via template.HTML) let anyone who authored a note with
// HTML/JS-looking plain text reproduce it live on the search-results page
// for every visitor whose query matched it: a stored XSS reachable by any
// searcher, not just the note's author. This escapes everything except the
// <mark>/</mark> delimiters themselves, which are exactly the StartSel/
// StopSel passed to ts_headline above, so no other literal "<mark>" can
// appear in the input.
func searchSnippet(raw string) templ.Component {
	const open, close = "<mark>", "</mark>"
	var b strings.Builder
	for {
		i := strings.Index(raw, open)
		if i < 0 {
			b.WriteString(html.EscapeString(raw))
			break
		}
		b.WriteString(html.EscapeString(raw[:i]))
		b.WriteString(open)
		raw = raw[i+len(open):]
		j := strings.Index(raw, close)
		if j < 0 {
			b.WriteString(html.EscapeString(raw))
			break
		}
		b.WriteString(html.EscapeString(raw[:j]))
		b.WriteString(close)
		raw = raw[j+len(close):]
	}
	return templ.Raw(b.String())
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

// searchScopeURL builds /search?q=...&scope=... with q properly escaped
// (unlike the old search.html, which interpolated .Query raw into the href
// and relied on html/template's contextual auto-escaping; templ.URL does not
// re-escape a pre-built string, so this does the escaping itself).
func searchScopeURL(q, scope string) string {
	href := "/search?q=" + url.QueryEscape(q)
	if scope != "" {
		href += "&scope=" + scope
	}
	return href
}
