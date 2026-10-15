-- Recommended-feed ranking (see internal/feedrank). feed_rank is set once, at
-- a note's first publish, and never recomputed, so keyset pagination over
-- (feed_rank, id) stays stable. import_batch_id groups the notes one import
-- published; the feed shows a large enough batch as a single card.
ALTER TABLE notes ADD COLUMN feed_rank BIGINT;
ALTER TABLE notes ADD COLUMN import_batch_id UUID;

-- Backfill every published note with feedrank.Assign's formula at its default
-- parameters (FEED_BURST_STEP 2h = 7200 s, FEED_BURST_WINDOW 24h = 86400 s;
-- env overrides do not reach a migration):
--   feed_rank = published_at - 7200 * (the author's notes published within
--               86400 s before this one)
-- written as window functions so 100K notes cost one sort, not 100K lookups.
-- rn - 1 counts the author's notes ordered before this one by
-- (published_at, id); before_window counts those older than the window
-- (published_at <= p - 86401). Existing notes get no import_batch_id.
UPDATE notes n
SET feed_rank = r.published_at - 7200 * (r.rn - 1 - r.before_window)
FROM (
  SELECT id, published_at,
         row_number() OVER (PARTITION BY author_id ORDER BY published_at, id) AS rn,
         count(*) OVER (PARTITION BY author_id ORDER BY published_at
                        RANGE BETWEEN UNBOUNDED PRECEDING AND 86401 PRECEDING) AS before_window
  FROM notes
  WHERE published_at IS NOT NULL
) r
WHERE n.id = r.id;

-- Keyset for the recommended feed: same predicate as recommendableNote.
CREATE INDEX idx_notes_feed_rank ON notes(feed_rank DESC, id DESC)
  WHERE published_at IS NOT NULL AND hidden_at IS NULL AND deleted_at IS NULL AND distribution = 'public';

-- Batch lookups: the feed's batch cards, the profile ?batch= filter, and
-- feedrank.Assign finding an already-ranked batchmate.
CREATE INDEX idx_notes_import_batch ON notes(author_id, import_batch_id, feed_rank)
  WHERE import_batch_id IS NOT NULL;
