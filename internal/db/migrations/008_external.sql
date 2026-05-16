-- External Obsidian Publish vaults indexed into commonplace.
-- "vault" = one publish.obsidian.md site (or custom domain).
-- "note"  = one markdown file within that vault.
-- "link"  = one wiki-link extracted from a note's body.

CREATE TABLE external_vaults (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  source_url      TEXT    NOT NULL UNIQUE,     -- normalised public URL pasted by admin
  site_id         TEXT    NOT NULL DEFAULT '', -- Obsidian Publish UUID, filled after first crawl
  display_name    TEXT    NOT NULL DEFAULT '', -- vault title from <h1> / options
  slug            TEXT    NOT NULL,            -- url-safe handle (last path segment), used in our routes
  added_at        INTEGER NOT NULL,
  last_crawled_at INTEGER NOT NULL DEFAULT 0,
  status          TEXT    NOT NULL DEFAULT 'pending', -- pending | crawling | active | error
  error_message   TEXT    NOT NULL DEFAULT '',
  note_count      INTEGER NOT NULL DEFAULT 0,
  UNIQUE(slug)
);

CREATE INDEX idx_external_vaults_status ON external_vaults(status, last_crawled_at);

CREATE TABLE external_notes (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  vault_id      INTEGER NOT NULL REFERENCES external_vaults(id) ON DELETE CASCADE,
  path          TEXT    NOT NULL,            -- file path inside vault, e.g. "Folder/Note.md"
  slug          TEXT    NOT NULL,            -- url-safe slug used in our routes
  title         TEXT    NOT NULL,
  body_md       TEXT    NOT NULL DEFAULT '',
  excerpt       TEXT    NOT NULL DEFAULT '',
  content_hash  TEXT    NOT NULL DEFAULT '',
  original_url  TEXT    NOT NULL DEFAULT '', -- best-effort: where to point "read on author's site"
  fetched_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  UNIQUE(vault_id, path)
);

CREATE INDEX idx_external_notes_vault_updated ON external_notes(vault_id, updated_at DESC);
CREATE INDEX idx_external_notes_updated ON external_notes(updated_at DESC);

CREATE TABLE external_links (
  id                       INTEGER PRIMARY KEY AUTOINCREMENT,
  source_external_note_id  INTEGER NOT NULL REFERENCES external_notes(id) ON DELETE CASCADE,
  raw_link                 TEXT    NOT NULL,           -- text inside [[...]] as parsed
  target_kind              TEXT    NOT NULL,           -- 'external_same' | 'external_cross' | 'commonplace_user' | 'unresolved'
  target_vault_id          INTEGER          REFERENCES external_vaults(id) ON DELETE SET NULL,
  target_external_note_id  INTEGER          REFERENCES external_notes(id) ON DELETE SET NULL,
  target_user_handle       TEXT    NOT NULL DEFAULT '',
  target_slug              TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_external_links_source ON external_links(source_external_note_id);
CREATE INDEX idx_external_links_target_note ON external_links(target_external_note_id);
CREATE INDEX idx_external_links_target_user ON external_links(target_user_handle, target_slug);

-- Full-text search over external notes (mirror of notes_fts).
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
