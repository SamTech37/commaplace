CREATE TABLE users (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  handle      TEXT    NOT NULL UNIQUE,
  email       TEXT    NOT NULL UNIQUE,
  created_at  INTEGER NOT NULL,
  theme       TEXT    NOT NULL DEFAULT 'auto',
  onboarded_at INTEGER
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
  hidden_at    INTEGER,
  deleted_at   INTEGER,
  UNIQUE(author_id, slug)
);

ALTER TABLE users ADD COLUMN pinned_note_id INTEGER REFERENCES notes(id) ON DELETE SET NULL;

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
CREATE INDEX idx_links_target   ON links(target_user_handle, target_slug);
CREATE INDEX idx_links_source   ON links(source_note_id);

CREATE TABLE auth_tokens (
  token       TEXT    PRIMARY KEY,
  email       TEXT    NOT NULL,
  expires_at  INTEGER NOT NULL,
  used_at     INTEGER
);

CREATE INDEX idx_auth_tokens_expires ON auth_tokens(expires_at);

CREATE TABLE note_tags (
  note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag        TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (note_id, tag)
);

CREATE INDEX idx_note_tags_tag ON note_tags(tag);

CREATE TABLE likes (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, note_id)
);

CREATE INDEX idx_likes_note         ON likes(note_id);
CREATE INDEX idx_likes_user_created ON likes(user_id, created_at DESC);

CREATE TABLE follows (
  follower_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followed_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (follower_id, followed_id)
);

CREATE INDEX idx_follows_followed         ON follows(followed_id);
CREATE INDEX idx_follows_follower_created ON follows(follower_id, created_at DESC);

CREATE TABLE reports (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  note_id      INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  reporter_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason       TEXT    NOT NULL,
  created_at   INTEGER NOT NULL,
  status       TEXT    NOT NULL DEFAULT 'open'
);

CREATE INDEX idx_reports_status_created ON reports(status, created_at DESC);
CREATE INDEX idx_reports_note            ON reports(note_id);

CREATE TABLE external_vaults (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  source_url      TEXT    NOT NULL UNIQUE,
  site_id         TEXT    NOT NULL DEFAULT '',
  display_name    TEXT    NOT NULL DEFAULT '',
  slug            TEXT    NOT NULL,
  added_at        INTEGER NOT NULL,
  last_crawled_at INTEGER NOT NULL DEFAULT 0,
  status          TEXT    NOT NULL DEFAULT 'pending',
  error_message   TEXT    NOT NULL DEFAULT '',
  note_count      INTEGER NOT NULL DEFAULT 0,
  kind            TEXT    NOT NULL DEFAULT 'publish',
  UNIQUE(slug)
);

CREATE INDEX idx_external_vaults_status ON external_vaults(status, last_crawled_at);
CREATE INDEX idx_external_vaults_kind   ON external_vaults(kind);

CREATE TABLE external_notes (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  vault_id      INTEGER NOT NULL REFERENCES external_vaults(id) ON DELETE CASCADE,
  path          TEXT    NOT NULL,
  slug          TEXT    NOT NULL,
  title         TEXT    NOT NULL,
  body_md       TEXT    NOT NULL DEFAULT '',
  excerpt       TEXT    NOT NULL DEFAULT '',
  content_hash  TEXT    NOT NULL DEFAULT '',
  original_url  TEXT    NOT NULL DEFAULT '',
  fetched_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  UNIQUE(vault_id, path)
);

CREATE INDEX idx_external_notes_vault_updated ON external_notes(vault_id, updated_at DESC);
CREATE INDEX idx_external_notes_updated ON external_notes(updated_at DESC);

CREATE TABLE external_links (
  id                       INTEGER PRIMARY KEY AUTOINCREMENT,
  source_external_note_id  INTEGER NOT NULL REFERENCES external_notes(id) ON DELETE CASCADE,
  raw_link                 TEXT    NOT NULL,
  target_kind              TEXT    NOT NULL,
  target_vault_id          INTEGER          REFERENCES external_vaults(id) ON DELETE SET NULL,
  target_external_note_id  INTEGER          REFERENCES external_notes(id) ON DELETE SET NULL,
  target_user_handle       TEXT    NOT NULL DEFAULT '',
  target_slug              TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_external_links_source ON external_links(source_external_note_id);
CREATE INDEX idx_external_links_target_note ON external_links(target_external_note_id);
CREATE INDEX idx_external_links_target_user ON external_links(target_user_handle, target_slug);

CREATE VIRTUAL TABLE notes_fts USING fts5(
  title,
  body_md,
  tokenize='unicode61 remove_diacritics 2',
  content='notes',
  content_rowid='id'
);

INSERT INTO notes_fts(rowid, title, body_md)
SELECT id, title, body_md FROM notes;

CREATE TRIGGER notes_fts_ai AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
END;

CREATE TRIGGER notes_fts_ad AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body_md) VALUES('delete', old.id, old.title, old.body_md);
END;

CREATE TRIGGER notes_fts_au AFTER UPDATE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body_md) VALUES('delete', old.id, old.title, old.body_md);
  INSERT INTO notes_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
END;

CREATE VIRTUAL TABLE external_notes_fts USING fts5(
  title,
  body_md,
  tokenize='unicode61 remove_diacritics 2',
  content='external_notes',
  content_rowid='id'
);

CREATE TRIGGER external_notes_fts_ai AFTER INSERT ON external_notes BEGIN
  INSERT INTO external_notes_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
END;

CREATE TRIGGER external_notes_fts_ad AFTER DELETE ON external_notes BEGIN
  INSERT INTO external_notes_fts(external_notes_fts, rowid, title, body_md)
  VALUES('delete', old.id, old.title, old.body_md);
END;

CREATE TRIGGER external_notes_fts_au AFTER UPDATE ON external_notes BEGIN
  INSERT INTO external_notes_fts(external_notes_fts, rowid, title, body_md)
  VALUES('delete', old.id, old.title, old.body_md);
  INSERT INTO external_notes_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
END;
