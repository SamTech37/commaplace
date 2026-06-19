CREATE INDEX IF NOT EXISTS idx_notes_feed ON notes(updated_at DESC)
WHERE hidden_at IS NULL AND deleted_at IS NULL AND published_at IS NOT NULL;
