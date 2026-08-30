-- One-time prod cleanup: drop the fake users seed.ApplyDev installs
-- (alice/bob/carol/dave/…), keep every account belonging to a real person.
--
-- Matched on the @dev.local email domain rather than a hand-written list of
-- handles: ApplyDev owns that domain, so the rule stays correct if the seed
-- cast ever changes, and it can never take out a real signup by accident.
--
-- All user-owned data cascades via ON DELETE CASCADE from users(id):
-- notes, likes, saves, follows, links, note_tags, reports, note_images.
DELETE FROM users WHERE lower(email) LIKE '%@dev.local';

-- auth_tokens has no FK to users — it is keyed by email and issued before
-- signup — so the delete above orphans rows here.
DELETE FROM auth_tokens WHERE lower(email) NOT IN (SELECT lower(email) FROM users);

-- The demo vault keeps its 14 notes but stops squatting a name a real person
-- may want. seed.DemoHandle moves to 'shawn_demo' in lockstep: ApplyDemo's
-- guard is `SELECT 1 FROM users WHERE handle = DemoHandle`, so renaming only
-- the row would make that guard miss and re-seed a second demo vault on the
-- next boot. Links survive the rename — they resolve by UUID, not handle.
UPDATE users SET handle = 'shawn_demo', handle_ci = 'shawn_demo'
WHERE lower(handle) = 'shawn';

-- Stub links record the handle they were authored against, so the rename above
-- leaves them pointing at a name that no longer exists — harmless today, but it
-- would strand saveNote's stub backfill if that vault ever gains a new note.
UPDATE links SET target_user_handle = 'shawn_demo'
WHERE lower(target_user_handle) = 'shawn';
