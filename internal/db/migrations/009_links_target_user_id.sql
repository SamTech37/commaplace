-- Links recorded the target's handle, which is a mutable label, not identity.
-- Renaming a user therefore stranded every inbound stub addressed to the old
-- name: backfillStubLinks matched on the string and could never match again.
-- Identity is users.id; store that instead.
--
-- What stays textual and why:
--   target_slug — the target note may not exist yet, so it has no uuid.
--   raw_target  — the link exactly as typed in the body. It is not a pointer;
--                 it is what the renderer matches against when re-parsing the
--                 body, so it must NOT follow renames.
ALTER TABLE links ADD COLUMN IF NOT EXISTS target_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

UPDATE links l SET target_user_id = u.id
FROM users u WHERE u.handle_ci = lower(l.target_user_handle);

CREATE INDEX IF NOT EXISTS idx_links_target_uid ON links(target_user_id, target_slug);
DROP INDEX IF EXISTS idx_links_target;
ALTER TABLE links DROP COLUMN IF EXISTS target_user_handle;
