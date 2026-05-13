CREATE TABLE likes (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, note_id)
);

CREATE INDEX idx_likes_note         ON likes(note_id);
CREATE INDEX idx_likes_user_created ON likes(user_id, created_at DESC);
