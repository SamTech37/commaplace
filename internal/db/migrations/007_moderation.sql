-- Reports filed by signed-in users on notes they think break norms.
CREATE TABLE reports (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  note_id      INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  reporter_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason       TEXT    NOT NULL,
  created_at   INTEGER NOT NULL,
  status       TEXT    NOT NULL DEFAULT 'open' -- 'open' | 'resolved'
);

CREATE INDEX idx_reports_status_created ON reports(status, created_at DESC);
CREATE INDEX idx_reports_note            ON reports(note_id);

-- Hidden notes 404 for everyone except the author and the admin. Feed,
-- search, tag pages, profile recents all exclude rows where this is set.
ALTER TABLE notes ADD COLUMN hidden_at INTEGER;
