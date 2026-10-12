# Load benchmark — 2026-10-12

Supersedes `BENCHMARK-2026-06-20.md` (schema 006, single queries only). This one
runs the real binary at `main` 9348b05 (schema through migration 015) under
concurrent HTTP load.

## Setup

- Data: `scripts/bench-seed.sql` — 100 users × 1000 notes = 100K notes, 294K
  note_tags (Zipf: `t0` on 33K notes, `t250` on a handful), 100K resolved links,
  100K likes, 1K follows. Bodies use a 5,000-word vocabulary, so a search term
  hits ~2% of notes (1,983 for `w1234`), unlike the 06-20 run where every note
  matched.
- Box: one 4-core / 15 GB container running **everything** — Go server, Postgres
  16 (default config, `shared_buffers` 128 MB), and the load generator. Absolute
  numbers are this box, not Render (Starter = 0.5 CPU, Postgres 17 Basic-1gb).
  Read them as ratios and as the shape of the curve.
- Load: [vegeta](https://github.com/tsenart/vegeta) v12.12.0, open-loop
  (fixed arrival rate). Per-query cost from `pg_stat_statements`.
- App pool: `SetMaxOpenConns(25)` (`internal/db/db.go`).

## 1. Per endpoint, uncontended (10 sequential requests, mean wall time)

| Endpoint | Wall | Dominant query (mean) | 判讀 |
| --- | --- | --- | --- |
| `/u005/note-5` note page | 6 ms | — | 快 |
| `/u005` profile | 8 ms | — | 快 |
| `/tag/t250` rare tag | 10 ms | — | 快 |
| `/feed?tab=following` | 19 ms | card query 14 ms | 可以 |
| `/api/graph/local` 1 or 2 hops | 4–5 ms | — | `maxLocalGraphNodes` 有效 |
| `/api/wiki/suggest?q=note-12` | **154 ms** | two `lower(n.title) LIKE '%…%'` seq scans, 75 + 73 ms | 每打一個字就一次（120 ms debounce）。沒有 trigram index |
| **`/feed` (recommended, anon)** | **177 ms** | `WITH ranked AS (ROW_NUMBER() OVER (PARTITION BY author_id …))` 171 ms | **最常被打的頁最慢**。per-author 上限用 window function 對**全部** 85K 篇公開筆記排序，每次載入都做 |
| **`/tag/t0` hot tag** | **280 ms** | related-tag cloud self-join 169 ms + card query (ordered over 33K tagged notes) 127 ms | 常見標籤才慢；冷門標籤 10 ms |
| **`/search?q=w1234`** | **293 ms** | `ts_headline` over every match before `LIMIT` 275 ms | 慢在摘要不是比對：2K 筆全做 headline |
| `/api/graph` (global) | 610 ms | — | **19 MB**（gzip 7 MB）。沒有上限，隨筆記數線性長 |

Tag chips (the 06-20 bottleneck, ~40 ms) no longer show up: `topTagChips` now
serves from a 5-minute in-process cache (`feed.go`).

## 2. Mixed read load (16-target round-robin: 3× feed, 6× note, 3× profile, rare tag, hot tag, search, following)

| Rate | p50 | p90 | p99 | Throughput | Max active DB conns |
| --- | --- | --- | --- | --- | --- |
| 5/s | 11 ms | 259 ms | 399 ms | 5.0/s | 3 |
| 10/s | 13 ms | 241 ms | 332 ms | 10.0/s | 4 |
| 20/s | 19 ms | 290 ms | 381 ms | 19.8/s | 6 |
| **40/s** | **118 ms** | **1.4 s** | **2.6 s** | **37.4/s** | **29** |

Knee between 20 and 40 req/s: at 40 the pool (25) is saturated, the
throughput falls behind the arrival rate, and a queue forms. The p90 at low
rates is just the three slow endpoints above, not contention.

Single endpoint at 20/s (10 s each) shows who saturates alone: `/feed` p50
1.6 s, `/tag/t0` p50 6.4 s (max 16 s), `/search` p50 2.6 s; every other
endpoint stayed under 30 ms p99.

## 3. Cheap pages only (30 random note pages + 10 profiles)

| Rate | p50 | p99 | Throughput |
| --- | --- | --- | --- |
| 100/s | 7 ms | 12 ms | 100/s |
| 200/s | 112 ms | 298 ms | 199/s |
| 400/s | 166 ms | 363 ms | 396/s |

Throughput keeps up to 400/s; latency rises past 100/s as the 4 shared cores
fill. CPU-bound, not pool-bound.

## 4. Write contention (10 users, 15 s each)

| Workload | Rate | p50 | p99 | Errors |
| --- | --- | --- | --- | --- |
| Autosave `PATCH /api/notes/{id}` (own draft; body with 2 tags + 2 wiki links, so tags/links recompute) | 50/s | 7 ms | 25 ms | 0 |
| same | 100/s | 7 ms | 13 ms | 0 |
| Like toggle, all 10 users on **one** note | 100/s | 2 ms | 4 ms | 0 |
| same | 300/s | 2 ms | 28 ms | 0 |

No deadlocks, no 5xx. Writes are not the problem; no write queue needed at this
scale.

## Conclusions

1. **The read path saturates on three queries, all O(table), not on writes or
   the pool size.** Raising the pool would only move the queue into Postgres.
2. `/feed` recommended is the one that matters: it's the front door and the
   most-requested page. The per-author cap ranks all public notes on every
   load. A fix that reads only the newest slice via `idx_notes_recommended`
   (e.g. rank within the last N×limit rows, or a `LATERAL` top-k per author)
   would make it scale with the page size, not the table.
3. `/search` should `LIMIT` first and `ts_headline` only the rows it shows.
4. `/api/wiki/suggest` wants a `pg_trgm` GIN index on `lower(title)` — it runs
   on every keystroke after `[[`.
5. Hot-tag related cloud: cache like tag chips, or sample.
6. `/api/graph` needs a node cap before the global graph is linked from
   anywhere popular.

None of this hurts today (three users, hundreds of notes). These are the
first things to fix when notes reach the tens of thousands.

## Reproduce

```sh
createdb benchmark
DATABASE_URL=postgres://…/benchmark go run ./cmd/server   # migrates; Ctrl-C
psql postgres://…/benchmark -v ON_ERROR_STOP=1 -f scripts/bench-seed.sql   # ~45 s
# pg_stat_statements: ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements';
#                     restart Postgres; CREATE EXTENSION pg_stat_statements;
ADDR=:8097 DEBUG=1 DATABASE_URL=postgres://…/benchmark go run ./cmd/server
curl -c jar 'localhost:8097/_dev/login?as=u001'          # session cookie for authed targets
go install github.com/tsenart/vegeta/v12@v12.12.0
vegeta attack -targets=mix.txt -rate=20 -duration=20s | vegeta report
```

Target files are plain vegeta format (`GET url` + optional header lines, blank
line between targets); writes use `@body.txt` for the form body.
