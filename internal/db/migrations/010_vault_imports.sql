-- Completed receipts make retrying a lost response safe. No uploaded files persist.
CREATE TABLE vault_imports (
  author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  id UUID NOT NULL,
  report JSONB NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (author_id, id)
);
