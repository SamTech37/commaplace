package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/config"
	"commonplace/internal/markdown"
)

var pageCfg = config.DefaultPagination()


// feedItem powers list-style cards (/tag, /me/saved, profile recent).
type feedItem struct {
	Title        string
	URL          string
	Excerpt      string
	AuthorHandle string
	UpdatedRel   string
}

// feedCard powers the masonry feed: meta-rich card with variant selection.
type feedCard struct {
	NoteID       uuid.UUID
	Title        string
	URL          string
	AuthorHandle string
	UpdatedAt    int64
	UpdatedRel   string
	LikeCount    int
	LinkCount    int
	CrossCount   int
	Variant      string   // "text" | "list" | "quote" | "links"
	Excerpt      string   // text variant
	ListItems    []string // list variant
	Quote        string   // quote variant
	LinkChips    []string // links variant
	Tags         []string // shown as #hashtag row on the card
	ImageURL     string   // first body image, shown as masonry thumbnail
}

type tagChip struct {
	Tag    string
	Count  int
	Active bool
}

func (s *Server) GetFeed(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tab := q.Get("tab")
	if tab != "following" {
		tab = "recommended"
	}
	tagFilter := normalizeTag(q.Get("tag"))
	layout := q.Get("layout")
	if layout != "list" && layout != "masonry" && layout != "grid" {
		layout = "grid"
	}
	var older int64
	if v := q.Get("older"); v != "" {
		older, _ = strconv.ParseInt(v, 10, 64)
	}

	viewer, _ := s.Auth.CurrentUser(r)

	var (
		cards []feedCard
		err   error
	)
	if tab == "following" {
		if viewer == nil {
			cards = nil
		} else {
			cards, err = s.queryFollowingCards(r.Context(), viewer.ID, tagFilter, older, pageCfg.FeedPageSize)
		}
	} else {
		cards, err = s.queryRecommendedCards(r.Context(), tagFilter, older, pageCfg.FeedPageSize)
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	attachTagsToCards(r.Context(), s.DB, cards)

	// "older" cursor for pagination — last item's updated_at, or empty.
	var olderURL string
	if len(cards) == pageCfg.FeedPageSize {
		last := cards[len(cards)-1]
		v := r.URL.Query()
		v.Set("older", strconv.FormatInt(last.UpdatedAt, 10))
		v.Set("layout", layout)
		v.Del("partial")
		olderURL = "/feed?" + v.Encode()
	}

	// HTMX request for the next batch → return only cards + a new sentinel.
	if q.Get("partial") == "1" {
		s.Pages.RenderPartial(w, "feed_partial", "feed_partial", map[string]any{
			"Cards":    cards,
			"OlderURL": olderURL,
			"Layout":   layout,
		})
		return
	}

	tagChips, _ := loadTopTagChips(r.Context(), s.DB, 8, tagFilter)

	s.render(w, r, "feed", map[string]any{
		"Cards":          cards,
		"Tab":            tab,
		"TagFilter":      tagFilter,
		"TagChips":       tagChips,
		"ViewerLoggedIn": viewer != nil,
		"OlderURL":       olderURL,
		"Layout":         layout,
	})
}

func (s *Server) queryRecommendedCards(ctx context.Context, tagFilter string, older int64, limit int) ([]feedCard, error) {
	args := []any{}
	q := strings.Builder{}
	q.WriteString(`
		SELECT n.id, n.title, n.slug, n.body_md, n.updated_at, u.handle,
		       (SELECT COUNT(*) FROM likes WHERE note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id AND target_user_handle != u.handle)
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`)
	if tagFilter != "" {
		args = append(args, tagFilter)
		fmt.Fprintf(&q, ` AND EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag = $%d)`, len(args))
	}
	if older > 0 {
		args = append(args, older)
		fmt.Fprintf(&q, ` AND n.updated_at < $%d`, len(args))
	}
	args = append(args, limit)
	fmt.Fprintf(&q, ` ORDER BY n.updated_at DESC LIMIT $%d`, len(args))
	rows, err := s.DB.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, err
	}
	return scanCards(rows)
}

func (s *Server) queryFollowingCards(ctx context.Context, viewerID uuid.UUID, tagFilter string, older int64, limit int) ([]feedCard, error) {
	args := []any{viewerID}
	q := strings.Builder{}
	q.WriteString(`
		SELECT n.id, n.title, n.slug, n.body_md, n.updated_at, u.handle,
		       (SELECT COUNT(*) FROM likes WHERE note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id AND target_user_handle != u.handle)
		FROM notes n
		JOIN users u   ON u.id = n.author_id
		JOIN follows f ON f.followed_id = n.author_id
		WHERE f.follower_id = $1 AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`)
	if tagFilter != "" {
		args = append(args, tagFilter)
		fmt.Fprintf(&q, ` AND EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag = $%d)`, len(args))
	}
	if older > 0 {
		args = append(args, older)
		fmt.Fprintf(&q, ` AND n.updated_at < $%d`, len(args))
	}
	args = append(args, limit)
	fmt.Fprintf(&q, ` ORDER BY n.updated_at DESC LIMIT $%d`, len(args))
	rows, err := s.DB.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, err
	}
	return scanCards(rows)
}

// attachTagsToCards batch-loads tags for the given cards in one query and
// fills c.Tags in place.
func attachTagsToCards(ctx context.Context, db *sql.DB, cards []feedCard) {
	if len(cards) == 0 {
		return
	}
	ids := make([]any, len(cards))
	idx := make(map[uuid.UUID]int, len(cards))
	placeholders := make([]string, len(cards))
	for i, c := range cards {
		ids[i] = c.NoteID
		idx[c.NoteID] = i
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	q := "SELECT note_id, tag FROM note_tags WHERE note_id IN (" + strings.Join(placeholders, ",") + ") ORDER BY note_id, tag"
	rows, err := db.QueryContext(ctx, q, ids...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return
		}
		if i, ok := idx[id]; ok {
			cards[i].Tags = append(cards[i].Tags, tag)
		}
	}
}

func scanCards(rows *sql.Rows) ([]feedCard, error) {
	defer rows.Close()
	var out []feedCard
	for rows.Next() {
		var (
			c      feedCard
			slug   string
			body   string
			handle string
		)
		if err := rows.Scan(&c.NoteID, &c.Title, &slug, &body, &c.UpdatedAt, &handle,
			&c.LikeCount, &c.LinkCount, &c.CrossCount); err != nil {
			return nil, err
		}
		c.AuthorHandle = handle
		c.URL = noteURL(handle, slug)
		c.UpdatedRel = relativeTime(c.UpdatedAt)
		c.Variant, c.Excerpt, c.ListItems, c.Quote, c.LinkChips = analyzeCardBody(body)
		c.ImageURL = markdown.FirstImageURL(body)
		out = append(out, c)
	}
	return out, rows.Err()
}

// analyzeCardBody picks a card variant by the body's structural signal.
// Rules (first match wins):
//   - starts with ">"           -> quote
//   - 3+ wiki links             -> links
//   - top of body is bullet list (3+ bullets) -> list
//   - else                      -> text excerpt
// matchBullet returns (text, true) if ln is "- ...", "* ...", or "12. ...".
func matchBullet(ln string) (string, bool) {
	if strings.HasPrefix(ln, "- ") {
		return strings.TrimSpace(ln[2:]), true
	}
	if strings.HasPrefix(ln, "* ") {
		return strings.TrimSpace(ln[2:]), true
	}
	for i, r := range ln {
		if r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && r == '.' && i+1 < len(ln) && ln[i+1] == ' ' {
			return strings.TrimSpace(ln[i+2:]), true
		}
		break
	}
	return "", false
}

func analyzeCardBody(body string) (variant, excerpt string, listItems []string, quote string, linkChips []string) {
	s := strings.TrimSpace(body)

	if strings.HasPrefix(s, ">") {
		var qb strings.Builder
		for _, line := range strings.SplitN(s, "\n", 30) {
			ln := strings.TrimSpace(line)
			if strings.HasPrefix(ln, ">") {
				qb.WriteString(strings.TrimSpace(strings.TrimPrefix(ln, ">")))
				qb.WriteByte(' ')
			} else if qb.Len() > 0 {
				break
			}
		}
		q := strings.TrimSpace(markdown.StripMDLinks(qb.String()))
		if runes := []rune(q); len(runes) > 200 {
			q = string(runes[:200]) + "…"
		}
		return "quote", "", nil, q, nil
	}

	links := markdown.Extract(body)
	if len(links) >= 3 {
		seen := map[string]bool{}
		chips := make([]string, 0, 6)
		for _, l := range links {
			label := l.Slug
			if l.User != "" {
				label = "@" + l.User + "/" + l.Slug
			}
			if seen[label] {
				continue
			}
			seen[label] = true
			chips = append(chips, label)
			if len(chips) >= 6 {
				break
			}
		}
		return "links", markdown.Excerpt(body, 160), nil, "", chips
	}

	var bullets []string
	started := false
	for _, line := range strings.SplitN(s, "\n", 60) {
		ln := strings.TrimSpace(line)
		if ln == "" {
			if started {
				break
			}
			continue
		}
		// before we've started collecting, skip leading headings
		if !started && strings.HasPrefix(ln, "#") {
			continue
		}
		item, ok := matchBullet(ln)
		if !ok {
			if started {
				break
			}
			continue
		}
		started = true
		bullets = append(bullets, markdown.StripMDLinks(item))
		if len(bullets) >= 5 {
			break
		}
	}
	if len(bullets) >= 3 {
		return "list", "", bullets, "", nil
	}

	return "text", markdown.Excerpt(body, 160), nil, "", nil
}

func loadTopTagChips(ctx context.Context, db *sql.DB, limit int, active string) ([]tagChip, error) {
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	rows, err := db.QueryContext(ctx, `
		SELECT nt.tag, COUNT(*) c
		FROM note_tags nt
		JOIN notes n ON n.id = nt.note_id
		WHERE n.updated_at > $1 AND n.published_at IS NOT NULL
		GROUP BY nt.tag
		ORDER BY c DESC, nt.tag
		LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tagChip
	for rows.Next() {
		var t tagChip
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		t.Active = t.Tag == active
		out = append(out, t)
	}
	return out, rows.Err()
}
