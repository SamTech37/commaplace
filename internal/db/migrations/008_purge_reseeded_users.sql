-- 007 deleted these, then seed.ApplyDev re-created them later in the same boot:
-- SEED_DEV was still "1" in Render's dashboard, and a Blueprint does not
-- re-apply render.yaml env vars on a code push. ApplyDev is now gated on DEBUG
-- too (cmd/server/main.go), which no dashboard value can switch back on, so
-- this delete is the last one needed.
--
-- 007 is already recorded in schema_migrations and never runs again, hence a
-- second file rather than an edit to the first.
DELETE FROM users WHERE lower(email) LIKE '%@dev.local';

DELETE FROM auth_tokens WHERE lower(email) NOT IN (SELECT lower(email) FROM users);
