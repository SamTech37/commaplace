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

// Assign sets feed_rank (and first_published_at, its time) on each of ids that
// is published and not yet ranked:
//
//	feed_rank = published_at − BurstStep × (the author's notes first
//	            published within BurstWindow before this one)
//
// so a burst of posts spreads back in time instead of stacking at the top.
// The tally goes by first publish and includes notes hidden or deleted since,
// so hiding and republishing cannot reset it. An import of at least BatchMin
// notes is one card in the feed and counts once; a smaller import's notes are
// separate cards and count one each. All notes of one import share one rank:
// a note whose import already has a rank takes it, and an import's notes
// ranked in one call get one rank between them.
//
// "Before this one" is every note already ranked, plus, among ids, those
// earlier by (published_at, id): ranking several notes in one call gives the
// same answer as ranking them one by one. Assign takes a per-author lock for
// the rest of tx, so concurrent publishes by one author see each other.
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
	// Same key as vault import's per-author lock, so an import in progress
	// and a publish by its author are ranked one after the other.
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended(a::text, 0))
		FROM (SELECT DISTINCT author_id AS a FROM notes WHERE id = ANY($1) ORDER BY 1) s`, ids); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, assignSQL, ids,
		int64(cfg.BurstStep/time.Second), int64(cfg.BurstWindow/time.Second), cfg.BatchMin)
	return err
}

// AssignUnranked ranks every published note that has no rank: notes an older
// binary published during a deploy overlap (the feed skips them until then),
// or rows written by hand. Idempotent; run at boot after migrating.
func AssignUnranked(ctx context.Context, db *sql.DB, cfg config.Feed) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var ids []uuid.UUID
	rows, err := tx.QueryContext(ctx, `SELECT id FROM notes WHERE feed_rank IS NULL AND published_at IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := Assign(ctx, tx, cfg, ids...); err != nil {
		return 0, err
	}
	return int64(len(ids)), tx.Commit()
}

// $1 ids, $2 BurstStep and $3 BurstWindow in seconds, $4 BatchMin.
//
// One pass of window functions over each author's events, O(n log n) in the
// notes involved: prior events are the author's ranked notes first published
// in the span the targets need, target events are the notes being ranked.
// pw and tw are an event's weight in the tally (prior, target): 1 per note,
// except that an import of at least $4 weighs 1 in all, on its earliest note
// (0 when an earlier call already ranked part of it). A target's tally is
// every prior event in its window plus the target events before it by
// (published_at, id) in its window. Targets of one import take the rank of
// the import's earliest target, or its existing rank.
const assignSQL = `
WITH t AS (
  SELECT id, author_id, import_batch_id AS batch, published_at AS p
  FROM notes WHERE id = ANY($1) AND feed_rank IS NULL AND published_at IS NOT NULL
), span AS (
  SELECT author_id, min(p) - $3::bigint AS lo, max(p) AS hi FROM t GROUP BY author_id
), prior AS (
  SELECT m.author_id, m.import_batch_id AS batch, m.id, m.first_published_at AS at
  FROM span JOIN notes m ON m.author_id = span.author_id
   AND m.first_published_at BETWEEN span.lo AND span.hi
), sizes AS (
  SELECT b.author_id, b.import_batch_id AS batch, count(*) AS size, max(b.feed_rank) AS rank
  FROM notes b
  WHERE b.import_batch_id IS NOT NULL
    AND (b.author_id, b.import_batch_id) IN (
      SELECT author_id, batch FROM t WHERE batch IS NOT NULL
      UNION SELECT author_id, batch FROM prior WHERE batch IS NOT NULL)
  GROUP BY 1, 2
), events AS (
  SELECT p.author_id, p.at, p.id, NULL::uuid AS unit, NULL::bigint AS inherited, 0 AS tw,
         CASE WHEN s.size >= $4
              THEN (row_number() OVER (PARTITION BY p.author_id, p.batch ORDER BY p.at, p.id) = 1)::int
              ELSE 1 END AS pw
  FROM prior p LEFT JOIN sizes s ON s.author_id = p.author_id AND s.batch = p.batch
  UNION ALL
  SELECT t.author_id, t.p, t.id, COALESCE(t.batch, t.id), s.rank,
         CASE WHEN s.size >= $4
              THEN (s.rank IS NULL AND row_number() OVER (PARTITION BY t.author_id, t.batch ORDER BY t.p, t.id) = 1)::int
              ELSE 1 END,
         0
  FROM t LEFT JOIN sizes s ON s.author_id = t.author_id AND s.batch = t.batch
), tallied AS (
  SELECT id, author_id, unit, inherited, at,
         at - $2::bigint * (
           sum(pw) OVER (PARTITION BY author_id ORDER BY at RANGE BETWEEN $3::bigint PRECEDING AND CURRENT ROW)
           + COALESCE(sum(tw) OVER (PARTITION BY author_id ORDER BY at, id
                                    ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING), 0)
           - COALESCE(sum(tw) OVER (PARTITION BY author_id ORDER BY at
                                    RANGE BETWEEN UNBOUNDED PRECEDING AND $3::bigint + 1 PRECEDING), 0)) AS computed
  FROM events
), ranked AS (
  SELECT id, at, COALESCE(inherited, first_value(computed) OVER (PARTITION BY author_id, unit ORDER BY at, id)) AS rank
  FROM tallied WHERE unit IS NOT NULL
)
UPDATE notes n SET feed_rank = r.rank, first_published_at = r.at
FROM ranked r WHERE n.id = r.id`
