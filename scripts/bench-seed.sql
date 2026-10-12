-- Synthetic load for docs/BENCHMARK-*.md: 100 users × 1000 notes = 100K notes,
-- ~300K note_tags (Zipf-skewed over 500 tags), 100K resolved links, 100K likes,
-- 1K follows. Run against an empty, migrated DB — never dev or prod:
--
--   createdb benchmark
--   DATABASE_URL=postgres://…/benchmark go run ./cmd/server   # migrates; Ctrl-C once listening
--   psql postgres://…/benchmark -v ON_ERROR_STOP=1 -f scripts/bench-seed.sql
--
-- Bodies draw from a 5,000-word vocabulary, so a search term hits ~1-2% of
-- notes instead of all of them (the 2026-06-20 FTS number was void for that).

SELECT setseed(0.42);

INSERT INTO users (handle, handle_ci, email, created_at)
SELECT format('u%s', lpad(i::text, 3, '0')), format('u%s', lpad(i::text, 3, '0')),
       format('u%s@bench.local', lpad(i::text, 3, '0')), 1700000000
FROM generate_series(0, 99) i;

-- Notes: author round-robin, timestamps spread over ~1 year, 95% published,
-- 10% of those semi-public, 1% hidden.
INSERT INTO notes (author_id, slug, slug_ci, title, body_md, created_at, updated_at,
                   published_at, hidden_at, distribution)
SELECT u.id, format('note-%s', n), format('note-%s', n),
       format('Note %s about w%s', n, (random() * 4999)::int),
       b.body, t, t,
       CASE WHEN random() < 0.95 THEN t END,
       CASE WHEN random() < 0.01 THEN t END,
       CASE WHEN random() < 0.10 THEN 'semi' ELSE 'public' END
FROM generate_series(0, 99999) n
JOIN users u ON u.handle = format('u%s', lpad((n % 100)::text, 3, '0'))
CROSS JOIN LATERAL (SELECT 1700000000 + (random() * 31536000)::bigint AS t) ts
CROSS JOIN LATERAL (SELECT string_agg('w' || (random() * 4999)::int, ' ') AS body
                    FROM generate_series(1, 80 + n % 40)) b;

-- Three distinct tags per note, Zipf-ish: t0 is common, t499 rare.
INSERT INTO note_tags (note_id, tag, created_at)
SELECT DISTINCT ON (id, tag) id, tag, created_at FROM (
  SELECT n.id, n.created_at, 't' || floor(500 * power(random(), 3))::int AS tag
  FROM notes n CROSS JOIN generate_series(1, 3)
) x;

-- One resolved link per note to a random note.
INSERT INTO links (source_note_id, target_user_id, target_slug, raw_target, resolved_target_id)
SELECT s.id, t.author_id, t.slug, t.slug, t.id
FROM (SELECT id, row_number() OVER () AS rn FROM notes) s
JOIN (SELECT id, author_id, slug, row_number() OVER (ORDER BY random()) AS rn FROM notes) t
  ON t.rn = s.rn;

INSERT INTO likes (user_id, note_id, created_at)
SELECT DISTINCT ON (u.id, n.id) u.id, n.id, 1700000000
FROM (SELECT id, row_number() OVER (ORDER BY random()) % 100 AS k FROM notes) n
JOIN (SELECT id, row_number() OVER (ORDER BY handle) - 1 AS k FROM users) u ON u.k = n.k;

-- Each user follows the next ten.
INSERT INTO follows (follower_id, followed_id, created_at)
SELECT a.id, b.id, 1700000000
FROM (SELECT id, row_number() OVER (ORDER BY handle) - 1 AS k FROM users) a
JOIN (SELECT id, row_number() OVER (ORDER BY handle) - 1 AS k FROM users) b
  ON b.k IN (SELECT (a.k + d) % 100 FROM generate_series(1, 10) d);

ANALYZE;
