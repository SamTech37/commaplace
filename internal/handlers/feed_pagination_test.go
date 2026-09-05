package handlers

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// mkPublishedAt inserts a published note with an exact updated_at, so a test
// can reproduce the one-timestamp-per-batch shape a bulk import writes.
func mkPublishedAt(t *testing.T, s *Server, author uuid.UUID, slug, title string, at int64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.DB.QueryRow(`
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at)
		VALUES($1, $2, $2, $3, 'body', $4, $4, $4) RETURNING id`,
		author, slug, title, at,
	).Scan(&id); err != nil {
		t.Fatalf("mkPublishedAt: %v", err)
	}
	return id
}

// One prolific author must not own a whole page. Recency alone gave the first
// bulk importer every slot, and import publishes immediately, so this is the
// first such user rather than a scale problem.
func TestFeedCapsOneAuthorPerPage(t *testing.T) {
	s := newTestServer(t)
	loud := mkUser(t, s, "loud")
	quiet := mkUser(t, s, "quiet")

	// The loud author's notes are all newer, so pure recency would bury quiet.
	for i := 0; i < 10; i++ {
		mkPublishedAt(t, s, loud, "loud-"+string(rune('a'+i)), "Loud", int64(2000+i))
	}
	mkPublishedAt(t, s, quiet, "quiet-1", "Quiet", 1000)

	cards, err := s.queryRecommendedCards(context.Background(), "", feedCursor{}, 16)
	if err != nil {
		t.Fatalf("queryRecommendedCards: %v", err)
	}

	byAuthor := map[string]int{}
	for _, c := range cards {
		byAuthor[c.AuthorHandle]++
	}
	if byAuthor["loud"] > maxPerAuthorPerPage {
		t.Errorf("loud took %d of one page, cap is %d", byAuthor["loud"], maxPerAuthorPerPage)
	}
	if byAuthor["quiet"] != 1 {
		t.Errorf("quiet's only note should reach the first page, got %d", byAuthor["quiet"])
	}
}

// Every note of a batch shares one updated_at. A cursor of "updated_at < last"
// steps over the rest of that second, so those notes cannot be scrolled to at
// all. Walking the whole feed must still yield each note exactly once.
func TestFeedPaginatesThroughIdenticalTimestamps(t *testing.T) {
	s := newTestServer(t)
	const sameSecond = 5000

	// Spread across authors so the per-author cap does not hide the point.
	want := map[string]bool{}
	for a := 0; a < 4; a++ {
		author := mkUser(t, s, "batch"+string(rune('a'+a)))
		for i := 0; i < 5; i++ {
			slug := "n-" + string(rune('a'+a)) + "-" + string(rune('0'+i))
			mkPublishedAt(t, s, author, slug, "N", sameSecond)
			want[slug] = false
		}
	}

	seen := map[string]int{}
	cursor := feedCursor{}
	for page := 0; page < 40; page++ {
		cards, err := s.queryRecommendedCards(context.Background(), "", cursor, 4)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(cards) == 0 {
			break
		}
		for _, c := range cards {
			seen[c.URL]++
		}
		last := cards[len(cards)-1]
		cursor = feedCursor{UpdatedAt: last.UpdatedAt, NoteID: last.NoteID}
	}

	if len(seen) != len(want) {
		t.Errorf("scrolled to %d of %d notes sharing one timestamp", len(seen), len(want))
	}
	for url, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times", url, n)
		}
	}
}
