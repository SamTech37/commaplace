-- FTS5 full-text index over notes.title + notes.body_md.
-- title weighted 2x via bm25(notes_fts, 2.0, 1.0) at query time.

CREATE VIRTUAL TABLE notes_fts USING fts5(
  title,
  body_md,
  tokenize='unicode61 remove_diacritics 2',
  content='notes',
  content_rowid='id'
);

-- Backfill from existing notes.
INSERT INTO notes_fts(rowid, title, body_md)
SELECT id, title, body_md FROM notes;

-- Keep the FTS index in sync with the base table.
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
