package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/db"
	"commonplace/internal/feedrank"
)

func feedRankOf(t *testing.T, s *Server, id uuid.UUID) int64 {
	t.Helper()
	var r sql.NullInt64
	if err := s.DB.QueryRow(`SELECT feed_rank FROM notes WHERE id = $1`, id).Scan(&r); err != nil || !r.Valid {
		t.Fatalf("note %s has no rank (%v)", id, err)
	}
	return r.Int64
}

func publishedAtOf(t *testing.T, s *Server, id uuid.UUID) int64 {
	t.Helper()
	var p int64
	if err := s.DB.QueryRow(`SELECT published_at FROM notes WHERE id = $1`, id).Scan(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func manageAs(t *testing.T, s *Server, user uuid.UUID, body string) {
	t.Helper()
	w := httptest.NewRecorder()
	s.PostManageNotes(w, authedRequest(s, user, http.MethodPost, "/api/notes/manage", body))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("manage %s: %d %s", body, w.Code, w.Body)
	}
}

// insertUnranked writes a published note the way a binary from before
// migration 017 does during a deploy overlap: no feed_rank.
func insertUnranked(t *testing.T, s *Server, author uuid.UUID, slug string, at int64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.DB.QueryRow(`
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at)
		VALUES($1, $2, $2, $2, 'x', $3, $3, $3) RETURNING id`, author, slug, at).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A published note without a rank must not break the feed for everyone: it
// is left out until the boot step ranks it.
func TestFeedSkipsUnrankedNotes(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkPublishedAt(t, s, alice, "ranked", "Ranked", 1000)
	insertUnranked(t, s, alice, "straggler", 2000)

	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/feed", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Ranked") || strings.Contains(w.Body.String(), "straggler") {
		t.Fatalf("feed with an unranked note: %d\n%s", w.Code, w.Body)
	}
}

// The boot step ranks stragglers with the same formula, once.
func TestAssignUnrankedRanksStragglers(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	mkPublishedAt(t, s, alice, "ranked", "Ranked", 1000)
	id := insertUnranked(t, s, alice, "straggler", 2000)

	n, err := feedrank.AssignUnranked(ctx, s.DB, s.Feed)
	if err != nil || n != 1 {
		t.Fatalf("AssignUnranked: %d, %v", n, err)
	}
	if got, want := feedRankOf(t, s, id), int64(2000-7200); got != want {
		t.Fatalf("straggler rank %d, want %d (one earlier note in the window)", got, want)
	}
	if n, err := feedrank.AssignUnranked(ctx, s.DB, s.Feed); err != nil || n != 0 {
		t.Fatalf("second run: %d, %v", n, err)
	}
	if cards := walkFeed(t, s, "", 50); len(cards) != 2 {
		t.Fatalf("feed after ranking: %+v", cards)
	}
}

// Hiding earlier posts must not reset the burst tally: it counts notes by
// first publish, whatever their state now.
func TestFeedBurstCountsHiddenEarlierNotes(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	step := int64(s.Feed.BurstStep / time.Second)

	a, err := s.saveNote(ctx, alice, "alice", "a", "A", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	manageAs(t, s, alice, "action=hide&ids="+a.String())
	b, err := s.saveNote(ctx, alice, "alice", "b", "B", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := feedRankOf(t, s, b), publishedAtOf(t, s, b)-step; got != want {
		t.Fatalf("post after hiding the previous one: rank %d, want %d", got, want)
	}
	manageAs(t, s, alice, "action=hide&ids="+b.String())
	manageAs(t, s, alice, "action=publish&ids="+a.String())
	c, err := s.saveNote(ctx, alice, "alice", "c", "C", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := feedRankOf(t, s, c), publishedAtOf(t, s, c)-2*step; got != want {
		t.Fatalf("third post after hide/republish: rank %d, want %d", got, want)
	}
}

// Two publishes by one author in flight at once: the second waits for the
// first and is spread behind it; two notes of one import share one rank.
func TestAssignSerializesPerAuthor(t *testing.T) {
	for _, sameImport := range []bool{false, true} {
		t.Run(fmt.Sprintf("sameImport=%v", sameImport), func(t *testing.T) {
			s := newTestServer(t)
			ctx := context.Background()
			alice := mkUser(t, s, "alice")
			const at = 5000
			batch := uuid.NullUUID{}
			if sameImport {
				batch = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			}
			insert := func(tx *sql.Tx, slug string) uuid.UUID {
				var id uuid.UUID
				if err := tx.QueryRow(`
					INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at, import_batch_id)
					VALUES($1, $2, $2, $2, 'x', $3, $3, $3, $4) RETURNING id`, alice, slug, at, batch).Scan(&id); err != nil {
					t.Error(err)
				}
				return id
			}

			tx1, _ := s.DB.Begin()
			defer tx1.Rollback()
			first := insert(tx1, "one")
			if err := feedrank.Assign(ctx, tx1, s.Feed, first); err != nil {
				t.Fatal(err)
			}
			second := make(chan uuid.UUID, 1)
			go func() {
				tx2, _ := s.DB.Begin()
				defer tx2.Rollback()
				id := insert(tx2, "two")
				if err := feedrank.Assign(ctx, tx2, s.Feed, id); err != nil {
					t.Error(err)
				}
				tx2.Commit()
				second <- id
			}()
			select {
			case <-second:
				t.Fatal("second publish was ranked while the first was still uncommitted")
			case <-time.After(300 * time.Millisecond):
			}
			if err := tx1.Commit(); err != nil {
				t.Fatal(err)
			}
			id := <-second
			want := int64(at) - int64(s.Feed.BurstStep/time.Second)
			if sameImport {
				want = at
			}
			if got := feedRankOf(t, s, id); got != want {
				t.Fatalf("second rank %d, want %d", got, want)
			}
		})
	}
}

// 全部儲存 fires every save-one at once; the import still gets one rank.
func TestParallelSaveOneSharesOneRank(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	batch := uuid.New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			form := url.Values{"title": {fmt.Sprintf("n%d", i)}, "body_md": {"x"}, "batch": {batch.String()}}
			w := httptest.NewRecorder()
			s.PostImportSaveOne(w, authedRequest(s, alice, "POST", "/import/save-one", form.Encode()))
			if w.Code != 200 {
				t.Errorf("save-one %d: %d %s", i, w.Code, w.Body)
			}
		}(i)
	}
	wg.Wait()
	var ranks int
	if err := s.DB.QueryRow(`SELECT count(DISTINCT feed_rank) FROM notes WHERE import_batch_id = $1`, batch).Scan(&ranks); err != nil || ranks != 1 {
		t.Fatalf("one import, %d distinct ranks (%v)", ranks, err)
	}
	if cards := walkFeed(t, s, "", 50); len(cards) != 1 || cards[0].BatchCount != 8 {
		t.Fatalf("feed: %+v", cards)
	}
}

// Ranking many notes in one call (manager publish of a whole tab) is one
// pass, not a lookup per pair, and matches ranking them one by one.
func TestAssignManyNotesIsFast(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	for i := int64(0); i < 5; i++ {
		mkPublishedAt(t, s, alice, fmt.Sprintf("old%d", i), "Old", nowUnix()-3600+i)
	}
	// 2,000 drafts, published together from the note manager (scope=all):
	// one shared published_at, so each one's tally spans all the others.
	const n = 2000
	if _, err := s.DB.Exec(`
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at)
		SELECT $1, 'n' || i, 'n' || i, 'N', 'x', 1, 1 FROM generate_series(1, $2) i`, alice, n); err != nil {
		t.Fatal(err)
	}
	// A running server reuses Assign's prepared statement, and after five runs
	// Postgres may switch to a generic plan, where the ids array is an opaque
	// parameter (no hashed = ANY). Rank under that plan.
	dsn, sep := os.Getenv("TEST_DATABASE_URL"), "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	generic, err := db.Open(dsn + sep + "plan_cache_mode=force_generic_plan")
	if err != nil {
		t.Fatal(err)
	}
	defer generic.Close()
	s.DB = generic
	began := time.Now()
	manageAs(t, s, alice, "action=publish&scope=all&tab=drafts")
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("publishing %d notes took %v", n, took)
	}

	// The same ranks as publishing them one by one in id order.
	step := int64(s.Feed.BurstStep / time.Second)
	rows, err := s.DB.Query(`SELECT published_at, feed_rank FROM notes WHERE author_id = $1 AND slug LIKE 'n%' ORDER BY id`, alice)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for k := int64(0); rows.Next(); k++ {
		var p, r int64
		if err := rows.Scan(&p, &r); err != nil {
			t.Fatal(err)
		}
		if want := p - step*(5+k); r != want {
			t.Fatalf("note %d: rank %d, want %d", k, r, want)
		}
	}
}

// An import too small to be a card is its notes in the feed, so each of them
// counts in the burst tally; a card-sized import counts once.
func TestFeedBurstCountsSmallImportPerNote(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	step := int64(s.Feed.BurstStep / time.Second)
	for files, before := range map[int]int64{2: 2, 3: 1} {
		handle := fmt.Sprintf("u%d", files)
		user := mkUser(t, s, handle)
		in := map[string]string{}
		for i := 0; i < files; i++ {
			in[fmt.Sprintf("f%d.md", i)] = "x"
		}
		importFiles(t, s, user, in)
		id, err := s.saveNote(ctx, user, handle, "post", "Post", "x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := feedRankOf(t, s, id), publishedAtOf(t, s, id)-before*step; got != want {
			t.Errorf("post after a %d-note import: rank %d, want %d (%d earlier items)", files, got, want, before)
		}
	}
}

// The batch card and the page it links to agree: both are the import's
// recommendable notes, so a semi-public note is neither counted on the card
// nor listed behind it (semi-public notes stay out of recommendations).
func TestBatchCardMatchesItsProfileView(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	batch := uuid.New()
	var ids []uuid.UUID
	for i := 0; i < 4; i++ {
		ids = append(ids, mkImportedAt(t, s, alice, batch, fmt.Sprintf("n%d", i), fmt.Sprintf("Imported %d", i), 1000))
	}
	if _, err := s.DB.Exec(`UPDATE notes SET distribution = 'semi' WHERE id = $1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	cards := walkFeed(t, s, "", 50)
	if len(cards) != 1 || cards[0].BatchCount != 3 {
		t.Fatalf("card: %+v", cards)
	}
	for _, viewer := range []uuid.UUID{mkUser(t, s, "bob"), alice} {
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, authedRequest(s, viewer, "GET", cards[0].URL, ""))
		if got := strings.Count(w.Body.String(), "Imported "); w.Code != 200 || got != cards[0].BatchCount {
			t.Fatalf("%s lists %d notes, card says %d", cards[0].URL, got, cards[0].BatchCount)
		}
	}
}

// A malformed ?batch= is no filter at all: the profile renders as if it were
// absent, published space included.
func TestProfileIgnoresInvalidBatch(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkPublishedAt(t, s, alice, "post", "Post", 1000)
	if _, err := s.DB.Exec(`INSERT INTO user_spaces(user_id, published, updated_at)
		VALUES($1, '{"title":"Alice Space Title","body":"","blocks":[]}', 0)`, alice); err != nil {
		t.Fatal(err)
	}
	get := func(target string) string {
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", target, w.Code)
		}
		return w.Body.String()
	}
	if !strings.Contains(get("/alice"), "Alice Space Title") {
		t.Fatal("plain profile does not render the space")
	}
	if !strings.Contains(get("/alice?batch=not-a-uuid"), "Alice Space Title") {
		t.Fatal("invalid ?batch= changed the profile view")
	}
}

// notesRowsRead runs EXPLAIN ANALYZE on q with sequential scans off (as on a
// large table) and returns how many notes rows its scans produced in all.
func notesRowsRead(t *testing.T, s *Server, q string, args []any) float64 {
	t.Helper()
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := tx.QueryRow(`EXPLAIN (ANALYZE, FORMAT JSON) `+q, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []struct{ Plan map[string]any }
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	var walk func(n map[string]any) float64
	walk = func(n map[string]any) float64 {
		sum := 0.0
		if n["Relation Name"] == "notes" {
			sum += n["Actual Rows"].(float64) * n["Actual Loops"].(float64)
		}
		children, _ := n["Plans"].([]any)
		for _, c := range children {
			sum += walk(c.(map[string]any))
		}
		return sum
	}
	return walk(plan[0].Plan)
}

// A feed page costs what is on it: imports ranked far below the page are
// never read, however many there are.
func TestFeedPageCostIgnoresDistantImports(t *testing.T) {
	s := newTestServer(t)
	alice, bob := mkUser(t, s, "alice"), mkUser(t, s, "bob")
	// 400 imports of 5 notes each, all ranked long before any post below.
	if _, err := s.DB.Exec(`
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at,
		                  import_batch_id, feed_rank, first_published_at)
		SELECT $1, 'i' || b || '-' || k, 'i' || b || '-' || k, 'I', 'x', 1000 + b, 1000 + b, 1000 + b,
		       md5('batch' || b)::uuid, 1000 + b, 1000 + b
		FROM generate_series(1, 400) b, generate_series(1, 5) k`, alice); err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 40; i++ {
		mkPublishedAt(t, s, bob, fmt.Sprintf("p%d", i), "P", 10_000_000+i*200_000)
	}
	if _, err := s.DB.Exec(`ANALYZE notes`); err != nil {
		t.Fatal(err)
	}
	page1, err := s.queryRecommendedCards(context.Background(), "", rankCursor{}, 10)
	if err != nil || len(page1) != 10 {
		t.Fatalf("page 1: %d cards (%v)", len(page1), err)
	}
	for name, cursor := range map[string]rankCursor{"page 1": {}, "page 2": page1[9].rankCursor()} {
		q, args := s.recommendedSQL("", cursor, 10)
		got := notesRowsRead(t, s, q, args)
		t.Logf("%s: %.0f notes rows", name, got)
		if got > 100 {
			t.Errorf("%s read %.0f notes rows for 10 cards", name, got)
		}
	}
}
