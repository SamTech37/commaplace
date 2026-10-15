-- Follow-up to 017 (review of PR #42).
--
-- first_published_at is when a note was ranked, i.e. its first publish. It is
-- set together with feed_rank and, like it, never changed. The burst tally in
-- feedrank.Assign counts an author's earlier notes by it: counting by the
-- current published_at let publish → hide → publish … reset the tally, since a
-- hidden note has no published_at and a republished one gets a new one.
-- 017 ranked every published note by its published_at, so that is its first
-- publish as far as ranking is concerned.
ALTER TABLE notes ADD COLUMN first_published_at BIGINT;
UPDATE notes SET first_published_at = COALESCE(published_at, feed_rank) WHERE feed_rank IS NOT NULL;
CREATE INDEX idx_notes_author_first_published ON notes(author_id, first_published_at)
  WHERE first_published_at IS NOT NULL;

-- The feed's note arm now lists only notes outside imports (import notes are
-- reached through their batch, idx_notes_batch_rank), and never an unranked
-- note: one published by an older binary during a deploy overlap has no rank
-- until the next boot ranks it.
DROP INDEX idx_notes_feed_rank;
CREATE INDEX idx_notes_feed_rank ON notes(feed_rank DESC, id DESC)
  WHERE published_at IS NOT NULL AND hidden_at IS NULL AND deleted_at IS NULL AND distribution = 'public'
    AND import_batch_id IS NULL AND feed_rank IS NOT NULL;

-- Imports in rank order, for the feed's batch arm. Every note of an import
-- shares one rank, so (feed_rank, import_batch_id) is the batch card's key.
CREATE INDEX idx_notes_batch_rank ON notes(feed_rank DESC, import_batch_id DESC, author_id)
  WHERE import_batch_id IS NOT NULL AND feed_rank IS NOT NULL;
