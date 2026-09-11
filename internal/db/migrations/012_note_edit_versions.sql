-- Detect concurrent editor saves (including two devices on one desk).
ALTER TABLE notes ADD COLUMN edit_version BIGINT NOT NULL DEFAULT 0;
