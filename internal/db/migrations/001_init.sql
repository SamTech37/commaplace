CREATE TABLE users (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  handle         TEXT    NOT NULL UNIQUE,
  handle_ci      TEXT    NOT NULL UNIQUE,
  email          TEXT    NOT NULL UNIQUE,
  created_at     BIGINT  NOT NULL,
  theme          TEXT    NOT NULL DEFAULT 'auto',
  onboarded_at   BIGINT,
  avatar         BYTEA,
  avatar_choice  TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE notes (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  author_id    UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  slug         TEXT    NOT NULL,
  slug_ci      TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  body_md      TEXT    NOT NULL DEFAULT '',
  search_tsv   TSVECTOR,
  created_at   BIGINT  NOT NULL,
  updated_at   BIGINT  NOT NULL,
  hidden_at    BIGINT,
  deleted_at   BIGINT,
  UNIQUE(author_id, slug_ci)
);

ALTER TABLE users ADD COLUMN pinned_note_id UUID REFERENCES notes(id) ON DELETE SET NULL;

CREATE INDEX idx_notes_author_updated ON notes(author_id, updated_at DESC);
CREATE INDEX idx_notes_updated        ON notes(updated_at DESC);
CREATE INDEX idx_notes_search_tsv     ON notes USING GIN(search_tsv);

CREATE FUNCTION notes_search_tsv_update() RETURNS trigger AS $$
BEGIN
  NEW.search_tsv :=
    setweight(to_tsvector('simple', coalesce(NEW.title, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(NEW.body_md, '')), 'B');
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notes_search_tsv_trigger
  BEFORE INSERT OR UPDATE OF title, body_md ON notes
  FOR EACH ROW EXECUTE FUNCTION notes_search_tsv_update();

CREATE TABLE links (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_note_id      UUID    NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  target_user_handle  TEXT    NOT NULL,
  target_slug         TEXT    NOT NULL,
  raw_target          TEXT    NOT NULL DEFAULT '',
  resolved_target_id  UUID             REFERENCES notes(id) ON DELETE SET NULL
);

CREATE INDEX idx_links_resolved ON links(resolved_target_id);
CREATE INDEX idx_links_target   ON links(target_user_handle, target_slug);
CREATE INDEX idx_links_source   ON links(source_note_id);

CREATE TABLE auth_tokens (
  token       TEXT    PRIMARY KEY,
  email       TEXT    NOT NULL,
  expires_at  BIGINT  NOT NULL,
  used_at     BIGINT
);

CREATE INDEX idx_auth_tokens_expires ON auth_tokens(expires_at);

CREATE TABLE note_tags (
  note_id    UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag        TEXT    NOT NULL,
  created_at BIGINT  NOT NULL,
  PRIMARY KEY (note_id, tag)
);

CREATE INDEX idx_note_tags_tag ON note_tags(tag);

CREATE TABLE likes (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note_id    UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  created_at BIGINT  NOT NULL,
  PRIMARY KEY (user_id, note_id)
);

CREATE INDEX idx_likes_note         ON likes(note_id);
CREATE INDEX idx_likes_user_created ON likes(user_id, created_at DESC);

CREATE TABLE follows (
  follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followed_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  BIGINT  NOT NULL,
  PRIMARY KEY (follower_id, followed_id)
);

CREATE INDEX idx_follows_followed         ON follows(followed_id);
CREATE INDEX idx_follows_follower_created ON follows(follower_id, created_at DESC);

CREATE TABLE reports (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  note_id      UUID    NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  reporter_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason       TEXT    NOT NULL,
  created_at   BIGINT  NOT NULL,
  status       TEXT    NOT NULL DEFAULT 'open'
);

CREATE INDEX idx_reports_status_created ON reports(status, created_at DESC);
CREATE INDEX idx_reports_note            ON reports(note_id);

CREATE TABLE note_images (
  note_id      UUID PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
  image        BYTEA   NOT NULL,
  content_type TEXT    NOT NULL,
  created_at   BIGINT  NOT NULL
);
