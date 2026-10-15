-- Substring title match for [[ autocomplete and the Cmd+K palette:
-- likeAnyVariant emits lower(n.title) LIKE '%…%', which a btree can't serve.
-- Patterns without 3 consecutive word characters yield no usable trigram;
-- trgmIndexable keeps the planner off this index for those.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_notes_title_lower_trgm ON notes USING gin (lower(title) gin_trgm_ops);
