-- Presentation state only. Notes, saves and links remain their own sources of truth.
CREATE TABLE desk_states (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 0,
    state JSONB NOT NULL DEFAULT '{"windows":[],"active":"","scrollLeft":0}',
    updated_at BIGINT NOT NULL
);
