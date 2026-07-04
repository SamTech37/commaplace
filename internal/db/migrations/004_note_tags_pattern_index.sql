CREATE INDEX idx_note_tags_tag_pattern ON note_tags (tag text_pattern_ops);
