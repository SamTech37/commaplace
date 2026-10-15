package handlers

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/config"
	"commonplace/internal/feedrank"
)

// mkPublishedAt inserts a note published at exactly `at` and ranks it the way
// every publish path does, so a test can reproduce the one-timestamp-per-batch
// shape a bulk import writes.
func mkPublishedAt(t *testing.T, s *Server, author uuid.UUID, slug, title string, at int64) uuid.UUID {
	t.Helper()
	return mkImportedAt(t, s, author, uuid.Nil, slug, title, at)
}

// mkImportedAt is mkPublishedAt for a note of import batch (uuid.Nil: none).
func mkImportedAt(t *testing.T, s *Server, author, batch uuid.UUID, slug, title string, at int64) uuid.UUID {
	t.Helper()
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id uuid.UUID
	if err := tx.QueryRow(`
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at, import_batch_id)
		VALUES($1, $2, $2, $3, 'body', $4, $4, $4, $5) RETURNING id`,
		author, slug, title, at, uuid.NullUUID{UUID: batch, Valid: batch != uuid.Nil},
	).Scan(&id); err != nil {
		t.Fatalf("mkImportedAt: %v", err)
	}
	if err := feedrank.Assign(context.Background(), tx, s.Feed, id); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

// walkFeed scrolls the recommended feed to its end, size cards a page.
func walkFeed(t *testing.T, s *Server, tag string, size int) []feedCard {
	t.Helper()
	var all []feedCard
	cursor := rankCursor{}
	for page := 0; page < 5000; page++ {
		cards, err := s.queryRecommendedCards(context.Background(), tag, cursor, size)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(cards) == 0 {
			return all
		}
		all = append(all, cards...)
		cursor = cards[len(cards)-1].rankCursor()
	}
	t.Fatal("feed never ended")
	return nil
}

// cardKey names a feed card: a note by URL, a batch card by its batch id.
func cardKey(c feedCard) string {
	if c.BatchID != uuid.Nil {
		return "batch:" + c.BatchID.String()
	}
	return c.URL
}

// Every note of a batch shares one published_at. A cursor on the timestamp
// alone steps over the rest of that second, so those notes cannot be scrolled
// to at all. Walking the whole feed must still yield each note exactly once.
func TestFeedPaginatesThroughIdenticalTimestamps(t *testing.T) {
	s := newTestServer(t)
	const sameSecond = 5000

	want := map[string]bool{}
	for a := 0; a < 4; a++ {
		author := mkUser(t, s, "batch"+string(rune('a'+a)))
		for i := 0; i < 5; i++ {
			slug := "n-" + string(rune('a'+a)) + "-" + string(rune('0'+i))
			mkPublishedAt(t, s, author, slug, "N", sameSecond)
			want["/batch"+string(rune('a'+a))+"/"+slug] = true
		}
	}

	seen := map[string]int{}
	for _, c := range walkFeed(t, s, "", 4) {
		seen[c.URL]++
	}
	if len(seen) != len(want) {
		t.Errorf("scrolled to %d of %d notes sharing one timestamp", len(seen), len(want))
	}
	for url, n := range seen {
		if n != 1 || !want[url] {
			t.Errorf("%s appeared %d times", url, n)
		}
	}
}

// Five posts in a row from one author must not stack at the top: each earlier
// post in the burst pushes the next back FEED_BURST_STEP, so other authors'
// notes from the same hours land between them.
func TestFeedBurstInterleavesWithOtherAuthors(t *testing.T) {
	s := newTestServer(t)
	const T = int64(1_800_000_000)
	alice := mkUser(t, s, "alice")
	for i := int64(0); i < 5; i++ {
		mkPublishedAt(t, s, alice, fmt.Sprintf("a%d", i), "A", T+60*i)
	}
	for i, h := range []string{"carol", "dave", "erin", "frank"} {
		mkPublishedAt(t, s, mkUser(t, s, h), "n", "N", T-int64(2*i+1)*3600)
	}

	var got []string
	for _, c := range walkFeed(t, s, "", 50) {
		got = append(got, c.AuthorHandle)
	}
	want := "alice carol alice dave alice erin alice frank alice"
	if strings.Join(got, " ") != want {
		t.Fatalf("feed order\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}

// importFiles runs a real multi-file /import and saves every item of the
// batch editor through /import/save-one, the way the page's buttons do.
func importFiles(t *testing.T, s *Server, user uuid.UUID, files map[string]string) uuid.UUID {
	t.Helper()
	body, ctype := multipartUpload(t, files)
	req := httptest.NewRequest("POST", "/import", body)
	req.Header.Set("Content-Type", ctype)
	req.AddCookie(sessionCookie(t, s, user))
	rec := httptest.NewRecorder()
	s.PostImport(rec, req)
	m := regexp.MustCompile(`name="batch" value="([0-9a-f-]{36})"`).FindAllStringSubmatch(rec.Body.String(), -1)
	if len(m) != len(files) {
		t.Fatalf("batch editor carries %d batch ids for %d files:\n%s", len(m), len(files), rec.Body)
	}
	batch := uuid.MustParse(m[0][1])
	for name, content := range files {
		form := url.Values{"title": {strings.TrimSuffix(name, ".md")}, "body_md": {content}, "tags": {"imported"}, "batch": {batch.String()}}
		w := httptest.NewRecorder()
		s.PostImportSaveOne(w, authedRequest(s, user, "POST", "/import/save-one", form.Encode()))
		if w.Code != 200 {
			t.Fatalf("save-one %s: %d %s", name, w.Code, w.Body)
		}
	}
	return batch
}

func TestFeedImportBecomesOneCard(t *testing.T) {
	s := newTestServer(t)
	alice, bob := mkUser(t, s, "alice"), mkUser(t, s, "bob")
	batch := importFiles(t, s, alice, map[string]string{"one.md": "1", "two.md": "2", "three.md": "3"})
	if _, err := s.saveNote(context.Background(), bob, "bob", "hello", "Hello", "hi", nil); err != nil {
		t.Fatal(err)
	}

	cards := walkFeed(t, s, "", 50)
	if len(cards) != 2 {
		t.Fatalf("want one batch card + bob's note, got %d cards: %+v", len(cards), cards)
	}
	var card *feedCard
	for i := range cards {
		if cards[i].BatchID != uuid.Nil {
			card = &cards[i]
		} else if cards[i].AuthorHandle != "bob" {
			t.Fatalf("batch note listed on its own: %s", cards[i].URL)
		}
	}
	if card == nil || card.BatchID != batch || card.BatchCount != 3 || card.AuthorHandle != "alice" ||
		card.URL != "/alice?batch="+batch.String() || len(card.ListItems) != batchCardTitles {
		t.Fatalf("bad batch card: %+v", card)
	}

	// The rendered feed shows it; the following tab, profile and tag pages
	// still list the notes one by one.
	page := httptest.NewRecorder()
	s.Routes().ServeHTTP(page, httptest.NewRequest("GET", "/feed", nil))
	for _, want := range []string{"@alice 匯入了 3 篇筆記", `href="/alice?batch=` + batch.String() + `"`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("feed page missing %q", want)
		}
	}
	if _, err := s.DB.Exec(`INSERT INTO follows(follower_id, followed_id, created_at) VALUES($1, $2, 0)`, bob, alice); err != nil {
		t.Fatal(err)
	}
	following, err := s.queryFollowingCards(context.Background(), bob, "", feedCursor{}, 50)
	if err != nil || len(following) != 3 {
		t.Fatalf("following lists %d of 3 imported notes (%v)", len(following), err)
	}
	profile, _, err := loadRecentNotes(httptest.NewRequest("GET", "/alice", nil), s.DB, alice, "alice", bob, "", uuid.Nil, feedCursor{})
	if err != nil || len(profile) != 3 {
		t.Fatalf("profile lists %d of 3 imported notes (%v)", len(profile), err)
	}
	tag := httptest.NewRecorder()
	treq := httptest.NewRequest("GET", "/tag/imported", nil)
	treq.SetPathValue("tag", "imported")
	s.GetTagPage(tag, treq)
	for _, slug := range []string{"one", "two", "three"} {
		if !strings.Contains(tag.Body.String(), `href="/alice/`+slug+`"`) {
			t.Errorf("tag page missing imported note %s", slug)
		}
	}
	// The tag-filtered feed keeps the card: its notes carry the tag.
	if tagged := walkFeed(t, s, "imported", 50); len(tagged) != 1 || tagged[0].BatchID != batch {
		t.Errorf("tag-filtered feed: %+v", tagged)
	}
}

func TestFeedSmallImportStaysNotes(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	importFiles(t, s, alice, map[string]string{"one.md": "1", "two.md": "2"})
	cards := walkFeed(t, s, "", 50)
	if len(cards) != 2 || cards[0].BatchID != uuid.Nil || cards[1].BatchID != uuid.Nil {
		t.Fatalf("a 2-note import should be 2 plain cards, got %+v", cards)
	}
}

func TestFeedVaultImportIsOneCard(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	batch := uuid.New()
	r := vaultRequest(t, map[string]string{"a.md": "a", "b.md": "b", "c/d.md": "d", "e.md": "e"}, batch)
	r.AddCookie(sessionCookie(t, s, alice))
	w := httptest.NewRecorder()
	s.PostVaultImport(w, r)
	if !strings.Contains(w.Body.String(), `"type":"complete"`) {
		t.Fatal(w.Body.String())
	}
	cards := walkFeed(t, s, "", 50)
	if len(cards) != 1 || cards[0].BatchID != batch || cards[0].BatchCount != 4 {
		t.Fatalf("vault import of 4: %+v", cards)
	}
}

// Hidden and deleted notes leave the card's count but not the import's size:
// the card stays one card until nothing in it is visible, then disappears.
func TestFeedBatchCountDropsHiddenNotes(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	batch := uuid.New()
	var ids []uuid.UUID
	for i := 0; i < 4; i++ {
		ids = append(ids, mkImportedAt(t, s, alice, batch, fmt.Sprintf("n%d", i), fmt.Sprintf("Note %d", i), 1000))
	}
	s.DB.Exec(`UPDATE notes SET hidden_at = 1 WHERE id = $1`, ids[0])
	s.DB.Exec(`UPDATE notes SET deleted_at = 1 WHERE id = $1`, ids[1])
	cards := walkFeed(t, s, "", 50)
	if len(cards) != 1 || cards[0].BatchCount != 2 || strings.Join(cards[0].ListItems, ",") != "Note 2,Note 3" {
		t.Fatalf("after hiding 2 of 4: %+v", cards)
	}
	s.DB.Exec(`UPDATE notes SET published_at = NULL WHERE id = ANY($1)`, ids[2:])
	if cards := walkFeed(t, s, "", 50); len(cards) != 0 {
		t.Fatalf("a batch with nothing visible left should vanish: %+v", cards)
	}
}

// Random authors, bursts, same-second runs, imports of every size, hidden,
// deleted and semi-public notes: scrolling to the end at any page size shows
// every visible item exactly once, in strictly descending (rank, id) order.
func TestFeedScrollsEveryItemExactlyOnce(t *testing.T) {
	s := newTestServer(t)
	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	t.Logf("seed %d", seed)

	type note struct {
		key, author string
		batch       uuid.UUID
		visible     bool
		tagged      bool
	}
	var notes []note
	batchSize := map[uuid.UUID]int{}
	at := int64(1_700_000_000)
	authors := map[string]uuid.UUID{}
	for _, h := range []string{"a", "b", "c", "d", "e", "f"} {
		authors[h] = mkUser(t, s, h)
	}
	handles := []string{"a", "b", "c", "d", "e", "f"}
	for i := 0; i < 160; i++ {
		at += int64(rng.Intn(3) * rng.Intn(7200)) // a third of steps land in the same second
		h := handles[rng.Intn(len(handles))]
		size, batch := 1, uuid.Nil
		if rng.Intn(5) == 0 {
			size, batch = 1+rng.Intn(6), uuid.New()
		}
		for j := 0; j < size; j++ {
			slug := fmt.Sprintf("n%d-%d", i, j)
			id := mkImportedAt(t, s, authors[h], batch, slug, slug, at)
			n := note{key: "/" + h + "/" + slug, author: h, batch: batch, visible: true}
			switch rng.Intn(10) {
			case 0:
				s.DB.Exec(`UPDATE notes SET hidden_at = 1 WHERE id = $1`, id)
				n.visible = false
			case 1:
				s.DB.Exec(`UPDATE notes SET deleted_at = 1 WHERE id = $1`, id)
				n.visible = false
			case 2:
				s.DB.Exec(`UPDATE notes SET distribution = 'semi' WHERE id = $1`, id)
				n.visible = false
			}
			if rng.Intn(2) == 0 {
				s.DB.Exec(`INSERT INTO note_tags(note_id, tag, created_at) VALUES($1, 't', 0)`, id)
				n.tagged = true
			}
			if batch != uuid.Nil {
				batchSize[batch]++
			}
			notes = append(notes, n)
		}
	}

	expect := func(tag bool) map[string]int {
		visible := map[uuid.UUID]int{}
		hasTag := map[uuid.UUID]bool{}
		for _, n := range notes {
			if n.batch != uuid.Nil && n.visible {
				visible[n.batch]++
				hasTag[n.batch] = hasTag[n.batch] || n.tagged
			}
		}
		want := map[string]int{}
		for _, n := range notes {
			isCard := n.batch != uuid.Nil && batchSize[n.batch] >= s.Feed.BatchMin
			switch {
			case isCard && visible[n.batch] > 0 && (!tag || hasTag[n.batch]):
				want["batch:"+n.batch.String()] = visible[n.batch]
			case !isCard && n.visible && (!tag || n.tagged):
				want[n.key] = 0
			}
		}
		return want
	}

	for _, tag := range []string{"", "t"} {
		want := expect(tag != "")
		for _, size := range []int{1, 2, 3, 7, 50} {
			cards := walkFeed(t, s, tag, size)
			seen := map[string]int{}
			for i, c := range cards {
				k := cardKey(c)
				seen[k]++
				if count, ok := want[k]; !ok {
					t.Errorf("tag %q size %d: unexpected %s", tag, size, k)
				} else if c.BatchID != uuid.Nil && c.BatchCount != count {
					t.Errorf("tag %q size %d: %s counts %d, want %d", tag, size, k, c.BatchCount, count)
				}
				if i > 0 {
					p, q := cards[i-1].rankCursor(), c.rankCursor()
					if p.Rank < q.Rank || (p.Rank == q.Rank && p.ID.String() <= q.ID.String()) {
						t.Errorf("tag %q size %d: order breaks at %d", tag, size, i)
					}
				}
			}
			for k := range want {
				if seen[k] != 1 {
					t.Errorf("tag %q size %d: %s seen %d times", tag, size, k, seen[k])
				}
			}
		}
	}
}

// Whatever is published while a reader scrolls — a post, a burst, a whole
// import — never repeats or skips anything still ahead of their cursor.
func TestFeedPublishMidScrollChangesNothingPaged(t *testing.T) {
	s := newTestServer(t)
	alice, bob := mkUser(t, s, "alice"), mkUser(t, s, "bob")
	start := nowUnix() - 3*86400
	for i := 0; i < 30; i++ {
		author := alice
		if i%3 == 0 {
			author = bob
		}
		mkPublishedAt(t, s, author, fmt.Sprintf("old%d", i), "Old", start+int64(i)*3600)
	}
	before := walkFeed(t, s, "", 4)

	ctx := context.Background()
	paged, err := s.queryRecommendedCards(ctx, "", rankCursor{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := s.queryRecommendedCards(ctx, "", paged[len(paged)-1].rankCursor(), 4)
	paged = append(paged, next...)

	// Mid-scroll: a burst of new posts and an import, by the same authors.
	for i := 0; i < 5; i++ {
		if _, err := s.saveNote(ctx, alice, "alice", fmt.Sprintf("new%d", i), "New", "x", nil); err != nil {
			t.Fatal(err)
		}
	}
	importFiles(t, s, bob, map[string]string{"x.md": "x", "y.md": "y", "z.md": "z"})
	isNew := map[string]bool{}
	for _, c := range walkFeed(t, s, "", 50) {
		isNew[cardKey(c)] = true
	}
	for _, c := range before {
		delete(isNew, cardKey(c))
	}

	cursor := paged[len(paged)-1].rankCursor()
	var rest []string
	for {
		cards, err := s.queryRecommendedCards(ctx, "", cursor, 4)
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) == 0 {
			break
		}
		for _, c := range cards {
			if !isNew[cardKey(c)] {
				rest = append(rest, cardKey(c))
			}
		}
		cursor = cards[len(cards)-1].rankCursor()
	}
	var want []string
	for _, c := range before[len(paged):] {
		want = append(want, cardKey(c))
	}
	if strings.Join(rest, " ") != strings.Join(want, " ") {
		t.Fatalf("scroll changed after mid-scroll publishes\n got %v\nwant %v", rest, want)
	}
}

// The tuning parameters come from Server.Feed (config.FeedFromEnv in main),
// not constants: changing them changes the feed.
func TestFeedParamsComeFromConfig(t *testing.T) {
	s := newTestServer(t)
	s.Feed = config.Feed{BatchMin: 2, BurstStep: 0, BurstWindow: 24 * time.Hour}
	alice := mkUser(t, s, "alice")
	importFiles(t, s, alice, map[string]string{"one.md": "1", "two.md": "2"})
	for i := int64(0); i < 3; i++ {
		mkPublishedAt(t, s, alice, fmt.Sprintf("p%d", i), "P", nowUnix()+60*(i+1))
	}
	mkPublishedAt(t, s, mkUser(t, s, "bob"), "b", "B", nowUnix()+30)

	var got []string
	for _, c := range walkFeed(t, s, "", 50) {
		if c.BatchID != uuid.Nil {
			got = append(got, "batch")
		} else {
			got = append(got, c.AuthorHandle)
		}
	}
	// BatchMin 2 makes the 2-note import a card; BurstStep 0 leaves the burst
	// stacked by plain recency.
	if want := "alice alice alice bob batch"; strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

// Every path that publishes ranks the note once; edits and republishing keep
// the first rank.
func TestEveryPublishPathRanksOnce(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	rank := func(id uuid.UUID) int64 {
		t.Helper()
		var r *int64
		if err := s.DB.QueryRow(`SELECT feed_rank FROM notes WHERE id = $1`, id).Scan(&r); err != nil || r == nil {
			t.Fatalf("note %s has no rank (%v)", id, err)
		}
		return *r
	}
	publish := func(id uuid.UUID) {
		t.Helper()
		w := httptest.NewRecorder()
		req := authedRequest(s, alice, http.MethodPost, "/api/notes/"+id.String()+"/publish", "")
		req.SetPathValue("id", id.String())
		s.PublishNote(w, req)
		if w.Code != 200 {
			t.Fatalf("publish: %d %s", w.Code, w.Body)
		}
	}
	manage := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		s.PostManageNotes(w, authedRequest(s, alice, http.MethodPost, "/api/notes/manage", body))
		if w.Code != 303 {
			t.Fatalf("manage: %d %s", w.Code, w.Body)
		}
	}

	written, err := s.saveNote(ctx, alice, "alice", "written", "Written", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := s.createDraft(ctx, alice)
	if err := s.autosaveNote(ctx, draft, "alice", "drafted", "Drafted", "x", nil); err != nil {
		t.Fatal(err)
	}
	publish(draft)
	first := rank(draft)

	// Edit + publish again, and hide + publish again: same rank.
	timeNow = func() time.Time { return time.Now().Add(time.Hour) }
	defer func() { timeNow = time.Now }()
	if err := s.autosaveNote(ctx, draft, "alice", "drafted", "Drafted v2", "y", nil); err != nil {
		t.Fatal(err)
	}
	publish(draft)
	manage("action=hide&ids=" + draft.String())
	manage("action=publish&ids=" + draft.String())
	if got := rank(draft); got != first {
		t.Fatalf("republish moved rank %d -> %d", first, got)
	}

	// A draft first made public from the manager is ranked there.
	other, _ := s.createDraft(ctx, alice)
	if err := s.autosaveNote(ctx, other, "alice", "managed", "Managed", "x", nil); err != nil {
		t.Fatal(err)
	}
	manage("action=publish&ids=" + other.String())
	rank(other)
	rank(written)

	var unranked int
	if err := s.DB.QueryRow(`SELECT count(*) FROM notes WHERE published_at IS NOT NULL AND feed_rank IS NULL`).Scan(&unranked); err != nil || unranked != 0 {
		t.Fatalf("%d published notes without rank (%v)", unranked, err)
	}
}

// The batch card's link: /{user}?batch=<id> is the profile timeline narrowed
// to that import, infinite scroll included.
func TestProfileBatchFilter(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	batch := uuid.New()
	for i := 0; i < 21; i++ {
		mkImportedAt(t, s, alice, batch, fmt.Sprintf("imp%02d", i), fmt.Sprintf("Imported %02d", i), 1000)
	}
	mkPublishedAt(t, s, alice, "own", "Own Post", 2000)
	mkImportedAt(t, s, mkUser(t, s, "bob"), batch, "forged", "Forged", 1000) // same id, other author

	get := func(target string, htmx bool) string {
		t.Helper()
		req := httptest.NewRequest("GET", target, nil)
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d", target, w.Code)
		}
		return w.Body.String()
	}
	first := get("/alice?batch="+batch.String(), false)
	if strings.Contains(first, "Own Post") || strings.Contains(first, "Forged") || !strings.Contains(first, "一次匯入的筆記") {
		t.Fatalf("filtered profile shows notes outside the batch:\n%s", first)
	}
	m := regexp.MustCompile(`id="notes-sentinel"[^>]*hx-get="([^"]+)"`).FindStringSubmatch(first)
	if strings.Count(first, "Imported ") != 20 || m == nil || !strings.Contains(m[1], "batch="+batch.String()) {
		t.Fatalf("first page: %d notes, sentinel %v", strings.Count(first, "Imported "), m)
	}
	rest := get(strings.ReplaceAll(m[1], "&amp;", "&"), true)
	if strings.Count(rest, "Imported ") != 1 || strings.Contains(rest, "Own Post") {
		t.Fatalf("second page:\n%s", rest)
	}
	if all := get("/alice?all=1", false); !strings.Contains(all, "Own Post") {
		t.Fatal("unfiltered profile lost the plain post")
	}
}
