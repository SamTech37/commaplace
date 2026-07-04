-- Replaces 004's index: note_tags.tag isn't guaranteed lowercase at rest
-- (some seed paths insert raw casing, e.g. "UX"), so prefix matching must
-- go through lower(tag), which the plain-column index can't serve.
DROP INDEX IF EXISTS idx_note_tags_tag_pattern;
CREATE INDEX idx_note_tags_tag_lower_pattern ON note_tags (lower(tag) text_pattern_ops);
