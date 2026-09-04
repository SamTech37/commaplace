package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"commonplace/internal/config"
	"commonplace/internal/markdown"
)

var pageCfg = config.DefaultPagination()


// feedCard powers every note-listing surface (feed/tag/search/profile/saved
// and the hover-preview endpoint): a meta-rich card with variant selection.
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
	IsDraft      bool     // profile's own drafts tab: shows a bulk-delete checkbox
	SnippetHTML  templ.Component // search's ts_headline <mark> highlight; overrides Excerpt when set
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
	cursor := parseFeedCursor(q.Get("older"), q.Get("older_id"))

	viewer, _ := s.Auth.CurrentUser(r)

	var (
		cards []feedCard
		err   error
	)
	if tab == "following" {
		if viewer == nil {
			cards = nil
		} else {
			cards, err = s.queryFollowingCards(r.Context(), viewer.ID, tagFilter, cursor.UpdatedAt, pageCfg.FeedPageSize)
		}
	} else {
		cards, err = s.queryRecommendedCards(r.Context(), tagFilter, cursor, pageCfg.FeedPageSize)
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	attachTagsToCards(r.Context(), s.DB, cards)

	// Cursor: the last card's (updated_at, id). The id half is what makes a
	// batch of notes sharing one timestamp paginable at all — see feedCursor.
	// A full page is no longer the test for "there is more": maxPerAuthor can
	// leave a page short while notes remain, so ask again whenever anything
	// came back and let the next request return nothing.
	var olderURL string
	if len(cards) > 0 {
		last := cards[len(cards)-1]
		v := r.URL.Query()
		v.Set("older", strconv.FormatInt(last.UpdatedAt, 10))
		v.Set("older_id", last.NoteID.String())
		olderURL = "/feed?" + v.Encode()
	}

	view := NoteListView{
		Cards:    cards,
		OlderURL: olderURL,
		Empty:    feedEmpty(tagFilter),
	}

	// HTMX request for the next batch → return only cards + a new sentinel.
	if r.Header.Get("HX-Request") == "true" {
		s.renderFragment(w, r, notesFragment(view))
		return
	}

	tagChips := s.topTagChips(r.Context(), tagFilter)
	userHandle := ""
	if viewer != nil {
		userHandle = viewer.Handle
	}

	s.renderPage(w, r, pageTitle("Feed"), "page-wide", nil, feedPage(FeedPageProps{
		View:           view,
		Tab:            tab,
		TagFilter:      tagFilter,
		TagChips:       tagChips,
		ViewerLoggedIn: viewer != nil,
		UserHandle:     userHandle,
	}))
}

func feedEmpty(tagFilter string) templ.Component {
	if tagFilter != "" {
		return emptyText("還沒有筆記使用 #" + tagFilter + "。")
	}
	return feedEmptyNoTag()
}

// maxPerAuthorPerPage caps how much of one page a single author may occupy.
// Without it the recommended feed is pure recency, so the first person to
// import a large vault owns every page of it — and because import publishes
// immediately, that is the first such person, not the thousandth.
const maxPerAuthorPerPage = 3

// feedCursor is the recommended feed's pagination key. updated_at alone is not
// enough: a bulk import writes one timestamp across every note it creates, and
// "updated_at < last" then steps over every sibling sharing that second — those
// notes become unreachable by scrolling rather than merely late. The note id
// breaks the tie, and the same (updated_at, id) pair orders the rows.
type feedCursor struct {
	UpdatedAt int64
	NoteID    uuid.UUID
}

func parseFeedCursor(older, olderID string) feedCursor {
	var c feedCursor
	c.UpdatedAt, _ = strconv.ParseInt(older, 10, 64)
	c.NoteID, _ = uuid.Parse(olderID)
	return c
}

func (c feedCursor) set() bool { return c.UpdatedAt > 0 && c.NoteID != uuid.Nil }

func (s *Server) queryRecommendedCards(ctx context.Context, tagFilter string, cursor feedCursor, limit int) ([]feedCard, error) {
	// The CTE holds the filters and the per-author ranking; the outer query
	// only adds the columns. Written this way the filters exist once, and
	// noteCardColumns needs no aliasing to survive a subquery.
	args := []any{}
	q := strings.Builder{}
	q.WriteString(`
		WITH ranked AS (
		  SELECT n.id,
		         ROW_NUMBER() OVER (PARTITION BY n.author_id
		                            ORDER BY n.updated_at DESC, n.id DESC) AS rn
		  FROM notes n
		  WHERE n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`)
	if tagFilter != "" {
		args = append(args, tagFilter)
		fmt.Fprintf(&q, ` AND EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag = $%d)`, len(args))
	}
	if cursor.set() {
		args = append(args, cursor.UpdatedAt, cursor.NoteID)
		fmt.Fprintf(&q, ` AND (n.updated_at, n.id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	args = append(args, maxPerAuthorPerPage)
	fmt.Fprintf(&q, `
		)
		SELECT %s
		FROM notes n
		JOIN users u ON u.id = n.author_id
		JOIN ranked r ON r.id = n.id AND r.rn <= $%d`, noteCardColumns, len(args))
	args = append(args, limit)
	fmt.Fprintf(&q, ` ORDER BY n.updated_at DESC, n.id DESC LIMIT $%d`, len(args))
	rows, err := s.DB.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, err
	}
	return scanCards(rows)
}

func (s *Server) queryFollowingCards(ctx context.Context, viewerID uuid.UUID, tagFilter string, older int64, limit int) ([]feedCard, error) {
	args := []any{viewerID}
	q := strings.Builder{}
	fmt.Fprintf(&q, `
		SELECT %s
		FROM notes n
		JOIN users u   ON u.id = n.author_id
		JOIN follows f ON f.followed_id = n.author_id
		WHERE f.follower_id = $1 AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`, noteCardColumns)
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

// noteCardColumns is the canonical column list every note-listing query
// selects (feed/tag/profile/hover-preview — everything except search, whose
// ts_headline snippet is a genuinely different shape). One list, so a query
// can't drift out of sync with what scanCards scans. Requires
// "FROM notes n JOIN users u ON u.id = n.author_id" in scope.
const noteCardColumns = `n.id, n.title, n.slug, n.body_md, n.updated_at, u.handle, n.published_at,
	(SELECT COUNT(*) FROM likes WHERE note_id = n.id),
	(SELECT COUNT(*) FROM links WHERE source_note_id = n.id),
	(SELECT COUNT(*) FROM links WHERE source_note_id = n.id AND target_user_id IS DISTINCT FROM u.id)`

// noteRow is the plain data one noteCardColumns row carries — no UI shaping.
// toCard is the one place that turns it into the feedCard the templ layer
// renders (variant classification, thumbnail pick): data and its view are
// fetched together but adapted in a separate, explicit step.
type noteRow struct {
	ID           uuid.UUID
	Title        string
	Slug         string
	Body         string
	UpdatedAt    int64
	AuthorHandle string
	PublishedAt  sql.NullInt64
	LikeCount    int
	LinkCount    int
	CrossCount   int
}

func scanNoteRows(rows *sql.Rows) ([]noteRow, error) {
	defer rows.Close()
	var out []noteRow
	for rows.Next() {
		var n noteRow
		if err := rows.Scan(&n.ID, &n.Title, &n.Slug, &n.Body, &n.UpdatedAt, &n.AuthorHandle, &n.PublishedAt,
			&n.LikeCount, &n.LinkCount, &n.CrossCount); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// toCard adapts one noteRow into a feedCard. This is the UI-shaping step
// (masonry variant/quote/list/thumbnail) — it never runs inside the SQL scan.
// untitledDraftLabel stands in for a draft's empty title everywhere a card
// renders one — only drafts can have an empty title (PublishNote requires
// one), so this never masks a real published note.
const untitledDraftLabel = "（空白）"

func (n noteRow) toCard() feedCard {
	title := n.Title
	if title == "" {
		title = untitledDraftLabel
	}
	c := feedCard{
		NoteID:       n.ID,
		Title:        title,
		URL:          noteURL(n.AuthorHandle, n.Slug),
		AuthorHandle: n.AuthorHandle,
		UpdatedAt:    n.UpdatedAt,
		UpdatedRel:   relativeTime(n.UpdatedAt),
		IsDraft:      !n.PublishedAt.Valid,
		LikeCount:    n.LikeCount,
		LinkCount:    n.LinkCount,
		CrossCount:   n.CrossCount,
	}
	c.Variant, c.Excerpt, c.ListItems, c.Quote, c.LinkChips = analyzeCardBody(n.Body)
	c.ImageURL = markdown.FirstImageURL(n.Body)
	return c
}

func scanCards(rows *sql.Rows) ([]feedCard, error) {
	noteRows, err := scanNoteRows(rows)
	if err != nil {
		return nil, err
	}
	out := make([]feedCard, len(noteRows))
	for i, n := range noteRows {
		out[i] = n.toCard()
	}
	return out, nil
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
		lines := strings.Split(s, "\n")
		rest := ""
		for i, line := range lines {
			ln := strings.TrimSpace(line)
			if strings.HasPrefix(ln, ">") {
				qb.WriteString(strings.TrimSpace(strings.TrimPrefix(ln, ">")))
				qb.WriteByte(' ')
			} else if qb.Len() > 0 {
				rest = strings.Join(lines[i:], "\n")
				break
			}
		}
		q := strings.TrimSpace(markdown.StripInline(markdown.StripMDLinks(qb.String())))
		if runes := []rune(q); len(runes) > 200 {
			q = string(runes[:200]) + "…"
		}
		// the quote renders in its own block above; excerpt picks up after it
		return "quote", markdown.Excerpt(rest, 160), nil, q, nil
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
		// the links render as chips above; dropping them from the excerpt keeps
		// the card from printing the same slugs twice
		return "links", markdown.Excerpt(markdown.StripWikiLinks(body), 160), nil, "", chips
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
		bullets = append(bullets, markdown.StripInline(markdown.StripMDLinks(item)))
		if len(bullets) >= 5 {
			break
		}
	}
	if len(bullets) >= 3 {
		return "list", "", bullets, "", nil
	}

	return "text", markdown.Excerpt(body, 160), nil, "", nil
}

// topTagChips returns the top tag chips with Active set for the current filter,
// served from a 5-min in-process cache (the underlying aggregate is ~40ms at
// scale). On a cache miss it refills from loadTopTagChips; on error it returns
// nil (chips are a non-critical feed adornment). See tagChipCache.
func (s *Server) topTagChips(ctx context.Context, active string) []tagChip {
	c := &s.tagChips
	c.mu.RLock()
	fresh := time.Now().Before(c.until)
	cached := c.data
	c.mu.RUnlock()

	if !fresh {
		chips, err := loadTopTagChips(ctx, s.DB, 8)
		if err != nil {
			log.Printf("topTagChips refill: %v", err)
			return nil
		}
		c.mu.Lock()
		c.data, c.until = chips, time.Now().Add(5*time.Minute)
		cached = c.data
		c.mu.Unlock()
	}

	// Copy + set the per-request Active flag (the cached list omits it).
	out := make([]tagChip, len(cached))
	for i, t := range cached {
		t.Active = t.Tag == active
		out[i] = t
	}
	return out
}

func loadTopTagChips(ctx context.Context, db *sql.DB, limit int) ([]tagChip, error) {
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
		out = append(out, t)
	}
	return out, rows.Err()
}
