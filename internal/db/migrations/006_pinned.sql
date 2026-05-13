-- Pinned note shown at the top of /me. Set by onboarding "fork the tour".
ALTER TABLE users ADD COLUMN pinned_note_id INTEGER REFERENCES notes(id) ON DELETE SET NULL;

-- Timestamp at which the user completed onboarding (forked or skipped).
-- NULL means we should still show the onboarding choice on next sign-in.
ALTER TABLE users ADD COLUMN onboarded_at INTEGER;
