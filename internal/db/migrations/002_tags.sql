CREATE TABLE note_tags (
  note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag        TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (note_id, tag)
);

CREATE INDEX idx_note_tags_tag ON note_tags(tag);
