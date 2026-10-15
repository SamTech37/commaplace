// Package feedrank computes notes.feed_rank, the key the recommended feed
// pages by. It is the one place rank is computed; every path that publishes a
// note calls Assign in the same transaction that sets published_at.
package feedrank

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/config"
)

// Assign sets feed_rank on each of ids that is published and not yet ranked:
//
//	feed_rank = published_at − BurstStep × (the author's notes published
//	            within BurstWindow before this one)
//
// so a burst of posts spreads back in time instead of stacking at the top.
// An import batch counts once in that tally, and a note whose batch already
// has a rank takes it, so a whole batch shares one rank — its card's place.
// "Before this one" is every note already ranked, plus, among ids themselves,
// those earlier by (published_at, id): ranking several notes in one call
// gives the same answer as ranking them one by one.
//
// Rank is set once and never recomputed — not on edit, not on republish after
// hiding (the note returns to its original place) — so a reader's cursor
// never sees an item move.
//
// Engagement is the expected next term (e.g. + k × likes in the last day). It
// would make rank change after publish, which this immutability rules out
// as-is; adding it means a scheduled recompute plus a per-session snapshot of
// the ranks a reader is paging through, so their cursor stays consistent.
func Assign(ctx context.Context, tx *sql.Tx, cfg config.Feed, ids ...uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, assignSQL, ids,
		int64(cfg.BurstStep/time.Second), int64(cfg.BurstWindow/time.Second))
	return err
}

// $1 ids, $2 BurstStep in seconds, $3 BurstWindow in seconds. Subqueries read
// the statement's snapshot, so m.feed_rank IS NOT NULL means "ranked before
// this call".
const assignSQL = `
UPDATE notes n SET feed_rank = COALESCE(
  (SELECT b.feed_rank FROM notes b
    WHERE b.author_id = n.author_id AND b.import_batch_id = n.import_batch_id
      AND b.feed_rank IS NOT NULL
    LIMIT 1),
  n.published_at - $2 * (
    SELECT COUNT(DISTINCT COALESCE(m.import_batch_id, m.id)) FROM notes m
    WHERE m.author_id = n.author_id
      AND m.published_at BETWEEN n.published_at - $3 AND n.published_at
      AND (m.feed_rank IS NOT NULL
           OR (m.id = ANY($1) AND (m.published_at, m.id) < (n.published_at, n.id)))
      AND (n.import_batch_id IS NULL OR m.import_batch_id IS DISTINCT FROM n.import_batch_id)))
WHERE n.id = ANY($1) AND n.feed_rank IS NULL AND n.published_at IS NOT NULL`
