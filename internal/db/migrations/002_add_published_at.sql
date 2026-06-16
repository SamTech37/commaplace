-- Draft model: published_at NULL = unpublished draft, non-NULL = published.
-- Backfill runs once (migrations are version-tracked): every existing note is
-- already public, so stamp it published at its creation time.
ALTER TABLE notes ADD COLUMN published_at BIGINT;
UPDATE notes SET published_at = created_at WHERE published_at IS NULL;
CREATE INDEX idx_notes_published ON notes(published_at);
