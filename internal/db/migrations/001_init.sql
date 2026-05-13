CREATE TABLE users (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  handle      TEXT    NOT NULL UNIQUE,
  email       TEXT    NOT NULL UNIQUE,
  created_at  INTEGER NOT NULL
);

CREATE TABLE notes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  author_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  folder_path  TEXT    NOT NULL DEFAULT '',
  slug         TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  body_md      TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  UNIQUE(author_id, folder_path, slug)
);

CREATE INDEX idx_notes_author_updated ON notes(author_id, updated_at DESC);
CREATE INDEX idx_notes_updated        ON notes(updated_at DESC);

CREATE TABLE links (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  source_note_id      INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  target_user_handle  TEXT    NOT NULL,
  target_folder_path  TEXT    NOT NULL DEFAULT '',
  target_slug         TEXT    NOT NULL,
  resolved_target_id  INTEGER          REFERENCES notes(id) ON DELETE SET NULL
);

CREATE INDEX idx_links_resolved ON links(resolved_target_id);
CREATE INDEX idx_links_target   ON links(target_user_handle, target_folder_path, target_slug);
CREATE INDEX idx_links_source   ON links(source_note_id);

CREATE TABLE auth_tokens (
  token       TEXT    PRIMARY KEY,
  email       TEXT    NOT NULL,
  expires_at  INTEGER NOT NULL,
  used_at     INTEGER
);

CREATE INDEX idx_auth_tokens_expires ON auth_tokens(expires_at);
