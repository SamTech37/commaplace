-- Keep the currently published text stable while its author keeps writing.
ALTER TABLE notes ADD COLUMN draft_title TEXT;
ALTER TABLE notes ADD COLUMN draft_body_md TEXT;
ALTER TABLE notes ADD COLUMN distribution TEXT NOT NULL DEFAULT 'public'
  CHECK (distribution IN ('public', 'semi'));
ALTER TABLE notes ADD COLUMN draft_distribution TEXT
  CHECK (draft_distribution IN ('public', 'semi'));

CREATE TABLE user_spaces (
  user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  revision BIGINT NOT NULL DEFAULT 0,
  document JSONB NOT NULL DEFAULT '{"title":"","body":"","blocks":[]}',
  published JSONB,
  updated_at BIGINT NOT NULL
);
CREATE INDEX idx_notes_recommended ON notes(updated_at DESC, id DESC)
  WHERE published_at IS NOT NULL AND hidden_at IS NULL AND deleted_at IS NULL AND distribution = 'public';
