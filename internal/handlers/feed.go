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
	Variant      string          // "text" | "list" | "quote" | "links"
	Excerpt      string          // text variant
	ListItems    []string        // list variant
	Quote        string          // quote variant
	LinkChips    []string        // links variant
	Tags         []string        // shown as #hashtag row on the card
	ImageURL     string          // first body image, shown as masonry thumbnail
	IsDraft      bool            // unpublished note; displayed as hidden/draft to its owner
	SnippetHTML  templ.Component // search's ts_headline <mark> highlight; overrides Excerpt when set
	FeedRank     int64           // recommended feed only: the card's keyset rank (internal/feedrank)
	BatchID      uuid.UUID       // non-nil: an import-batch card (recommended feed); ListItems holds sample titles
	BatchCount   int             // batch card: the batch's visible note count
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

	viewer, _ := s.Auth.CurrentUser(r)

	var (
		cards []feedCard
		err   error
	)
	if tab == "following" {
		if viewer == nil {
			cards = nil
		} else {
			cursor := parseFeedCursor(q.Get("older"), q.Get("older_id"))
			cards, err = s.queryFollowingCards(r.Context(), viewer.ID, tagFilter, cursor, pageCfg.FeedPageSize)
		}
	} else {
		cursor := parseRankCursor(q.Get("older"), q.Get("older_id"))
		cards, err = s.queryRecommendedCards(r.Context(), tagFilter, cursor, pageCfg.FeedPageSize)
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	attachTagsToCards(r.Context(), s.DB, cards)

	// Cursor: the last card's key — (updated_at, id) on following, (feed_rank,
	// note or batch id) on recommended. The id half is what makes notes
	// sharing one timestamp paginable at all — see feedCursor. Ask again
	// whenever anything came back and let the next request return nothing.
	var olderURL string
	if len(cards) > 0 {
		last := cards[len(cards)-1]
		v := r.URL.Query()
		if tab == "following" {
			v.Set("older", strconv.FormatInt(last.UpdatedAt, 10))
			v.Set("older_id", last.NoteID.String())
		} else {
			c := last.rankCursor()
			v.Set("older", strconv.FormatInt(c.Rank, 10))
			v.Set("older_id", c.ID.String())
		}
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

// feedCursor is the (updated_at, id) pagination key of the following feed and
// the other updated_at-ordered lists. updated_at alone is not enough: a bulk
// import writes one timestamp across every note it creates, and
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

// rankCursor is the recommended feed's pagination key: (feed_rank, id), where
// id is a note's id or, for a batch card, the batch id. feed_rank never
// changes after publish (internal/feedrank), so the key is stable.
type rankCursor struct {
	Rank int64
	ID   uuid.UUID
}

func parseRankCursor(older, olderID string) rankCursor {
	var c rankCursor
	c.Rank, _ = strconv.ParseInt(older, 10, 64)
	c.ID, _ = uuid.Parse(olderID)
	return c
}

func (c rankCursor) set() bool { return c.ID != uuid.Nil }

func (c feedCard) rankCursor() rankCursor {
	if c.BatchID != uuid.Nil {
		return rankCursor{c.FeedRank, c.BatchID}
	}
	return rankCursor{c.FeedRank, c.NoteID}
}

// batchCardTitles is how many of a batch's note titles its card previews.
const batchCardTitles = 3

// queryRecommendedCards pages the recommended feed: public notes and import
// batch cards, by (feed_rank, id) descending — a plain keyset, so nothing is
// dropped or repeated, and notes published mid-scroll rank above the cursor.
// An import of at least Feed.BatchMin notes is one card at the rank its notes
// share, and its notes are not listed on their own; a smaller import is just
// its notes. The size test counts every note the import created, so hiding
// some later does not break the card back into notes; the card's N counts
// only the recommendable (visible, public) ones, and it disappears when none
// are left. Unranked notes (see feedrank.AssignUnranked) are skipped.
func (s *Server) queryRecommendedCards(ctx context.Context, tagFilter string, cursor rankCursor, limit int) ([]feedCard, error) {
	q, args := s.recommendedSQL(tagFilter, cursor, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var (
		cards        []feedCard
		cardAuthors  []uuid.UUID // batch cards' author ids, aligned with cards
		noteIDs      []uuid.UUID
		batchAuthors []uuid.UUID
		batchIDs     []uuid.UUID
	)
	for rows.Next() {
		var (
			c         feedCard
			id        uuid.UUID
			published int64
			author    uuid.NullUUID
		)
		if err := rows.Scan(&c.FeedRank, &id, &c.BatchCount, &published, &author, &c.AuthorHandle); err != nil {
			rows.Close()
			return nil, err
		}
		if author.Valid {
			c.BatchID = id
			c.URL = "/" + c.AuthorHandle + "?batch=" + id.String()
			c.UpdatedAt, c.UpdatedRel = published, relativeTime(published)
			batchAuthors = append(batchAuthors, author.UUID)
			batchIDs = append(batchIDs, id)
		} else {
			c.NoteID = id
			noteIDs = append(noteIDs, id)
		}
		cards = append(cards, c)
		cardAuthors = append(cardAuthors, author.UUID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(noteIDs) > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT `+noteCardColumns+`
			FROM notes n JOIN users u ON u.id = n.author_id
			WHERE n.id = ANY($1)`, noteIDs)
		if err != nil {
			return nil, err
		}
		full, err := scanCards(rows)
		if err != nil {
			return nil, err
		}
		byID := make(map[uuid.UUID]feedCard, len(full))
		for _, c := range full {
			byID[c.NoteID] = c
		}
		for i, c := range cards {
			if c.BatchID == uuid.Nil {
				f := byID[c.NoteID] // zero if deleted since the first query; dropped below
				f.FeedRank = c.FeedRank
				cards[i] = f
			}
		}
	}

	if len(batchIDs) > 0 {
		rows, err := s.DB.QueryContext(ctx, `
			SELECT t.author_id, t.import_batch_id, t.title FROM (
			  SELECT n.author_id, n.import_batch_id, n.title,
			         row_number() OVER (PARTITION BY n.author_id, n.import_batch_id
			                            ORDER BY n.published_at, n.slug) AS rn
			  FROM notes n
			  WHERE `+recommendableNote+`
			    AND (n.author_id, n.import_batch_id) IN (SELECT * FROM unnest($1::uuid[], $2::uuid[]))
			) t
			WHERE t.rn <= $3
			ORDER BY t.rn`, batchAuthors, batchIDs, batchCardTitles)
		if err != nil {
			return nil, err
		}
		titles := map[[2]uuid.UUID][]string{}
		for rows.Next() {
			var key [2]uuid.UUID
			var title string
			if err := rows.Scan(&key[0], &key[1], &title); err != nil {
				rows.Close()
				return nil, err
			}
			titles[key] = append(titles[key], title)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i := range cards {
			if cards[i].BatchID != uuid.Nil {
				cards[i].ListItems = titles[[2]uuid.UUID{cardAuthors[i], cards[i].BatchID}]
			}
		}
	}
	out := cards[:0]
	for _, c := range cards {
		if c.NoteID != uuid.Nil || c.BatchID != uuid.Nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// recommendedSQL builds queryRecommendedCards' item query: rank, id, batch
// count, batch published_at, batch author (NULL for a note), handle. Its
// cost follows the page, not the table:
//   - the note arm reads the top of idx_notes_feed_rank (notes outside
//     imports) below the cursor, LIMIT items;
//   - the batch arm walks idx_notes_batch_rank down from the cursor, but no
//     lower than the note arm's last rank when that arm is full (nothing
//     lower can make the page), aggregates only the imports it meets, and
//     stops once it has LIMIT items: a card for an import of at least
//     BatchMin, otherwise that import's notes. It stops in index order,
//     by rank alone (WITH TIES keeps every item sharing the last rank), so
//     nothing is sorted before the LIMIT and the plan stays cheap enough
//     that the planner does not JIT-compile it.
//
// Every note of an import shares one rank (feedrank.Assign), which is what
// lets (feed_rank, import_batch_id) stand for the batch card's key.
func (s *Server) recommendedSQL(tagFilter string, cursor rankCursor, limit int) (string, []any) {
	args := []any{s.Feed.BatchMin, limit}
	noteFilter, batchTag, itemFilter, candFilter := "", "TRUE", "", ""
	if tagFilter != "" {
		args = append(args, tagFilter)
		batchTag = fmt.Sprintf(`EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag = $%d)`, len(args))
		noteFilter += ` AND ` + batchTag
	}
	if cursor.set() {
		args = append(args, cursor.Rank, cursor.ID)
		noteFilter += fmt.Sprintf(` AND (n.feed_rank, n.id) < ($%d, $%d)`, len(args)-1, len(args))
		itemFilter = fmt.Sprintf(` WHERE (x.rank, x.id) < ($%d, $%d)`, len(args)-1, len(args))
		candFilter = fmt.Sprintf(` AND n.feed_rank <= $%d`, len(args)-1)
	}
	return fmt.Sprintf(`
		WITH note_arm AS MATERIALIZED (
		  SELECT n.feed_rank AS rank, n.id FROM notes n
		  WHERE %[1]s AND n.import_batch_id IS NULL AND n.feed_rank IS NOT NULL%[2]s
		  ORDER BY n.feed_rank DESC, n.id DESC LIMIT $2
		), batch_arm AS (
		  SELECT x.* FROM (
		    SELECT DISTINCT n.feed_rank AS rank, n.import_batch_id AS id, n.author_id
		    FROM notes n
		    WHERE n.import_batch_id IS NOT NULL AND n.feed_rank IS NOT NULL%[5]s
		      AND n.feed_rank >= COALESCE((SELECT min(rank) FROM note_arm HAVING count(*) = $2),
		                                  '-9223372036854775808'::bigint)
		    ORDER BY 1 DESC, 2 DESC, 3
		  ) c
		  CROSS JOIN LATERAL (
		    SELECT count(*) AS size,
		           count(*) FILTER (WHERE %[1]s) AS notes,
		           count(*) FILTER (WHERE %[1]s AND %[3]s) AS tagged,
		           min(n.published_at) FILTER (WHERE %[1]s) AS published_at
		    FROM notes n WHERE n.author_id = c.author_id AND n.import_batch_id = c.id
		  ) b
		  CROSS JOIN LATERAL (
		    SELECT c.rank, c.id, b.notes, b.published_at, c.author_id
		    WHERE b.size >= $1 AND b.tagged > 0
		    UNION ALL
		    SELECT n.feed_rank, n.id, 0, 0, NULL::uuid FROM notes n
		    WHERE b.size < $1 AND n.author_id = c.author_id AND n.import_batch_id = c.id
		      AND n.feed_rank = c.rank AND %[1]s AND %[3]s
		  ) x%[4]s
		  ORDER BY c.rank DESC FETCH FIRST ($2) ROWS WITH TIES
		)
		SELECT i.rank, i.id, i.notes, i.published_at, i.author_id, COALESCE(u.handle, '')
		FROM (
		  SELECT rank, id, 0 AS notes, 0::bigint AS published_at, NULL::uuid AS author_id FROM note_arm
		  UNION ALL
		  SELECT * FROM batch_arm
		) i
		LEFT JOIN users u ON u.id = i.author_id
		ORDER BY i.rank DESC, i.id DESC LIMIT $2`,
		recommendableNote, noteFilter, batchTag, itemFilter, candFilter), args
}

func (s *Server) queryFollowingCards(ctx context.Context, viewerID uuid.UUID, tagFilter string, older feedCursor, limit int) ([]feedCard, error) {
	args := []any{viewerID}
	q := strings.Builder{}
	fmt.Fprintf(&q, `
		SELECT %s
		FROM notes n
		JOIN users u   ON u.id = n.author_id
		JOIN follows f ON f.followed_id = n.author_id
		WHERE f.follower_id = $1 AND `+readableNote, noteCardColumns)
	if tagFilter != "" {
		args = append(args, tagFilter)
		fmt.Fprintf(&q, ` AND EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag = $%d)`, len(args))
	}
	if older.set() {
		args = append(args, older.UpdatedAt, older.NoteID)
		fmt.Fprintf(&q, ` AND (n.updated_at, n.id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	args = append(args, limit)
	fmt.Fprintf(&q, ` ORDER BY n.updated_at DESC, n.id DESC LIMIT $%d`, len(args))
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

// readableNote is the one definition of "any reader may open note n"; every
// public list, count and suggestion filters on it, so a count can't advertise
// notes its link then refuses to show. recommendableNote narrows it to what
// the recommended feed (and its tag chips) may surface. Requires alias n.
const (
	readableNote      = `n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`
	recommendableNote = readableNote + ` AND n.distribution = 'public'`
)

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
//
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
	_, body = markdown.SplitFrontmatter(body)
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
		WHERE n.updated_at > $1 AND `+recommendableNote+`
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
