-- Separate "save for later" from "like". Until now /me/saved read from the
-- likes table, conflating the two. saves mirrors likes structurally but is a
-- private bookmark list (no public count).
CREATE TABLE saves (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note_id    UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (user_id, note_id)
);
CREATE INDEX idx_saves_user_created ON saves(user_id, created_at DESC);
