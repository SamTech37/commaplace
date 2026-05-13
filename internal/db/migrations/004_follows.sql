CREATE TABLE follows (
  follower_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followed_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (follower_id, followed_id)
);

CREATE INDEX idx_follows_followed         ON follows(followed_id);
CREATE INDEX idx_follows_follower_created ON follows(follower_id, created_at DESC);
