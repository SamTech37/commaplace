# Load benchmark — 2026-10-12 (re-measured 2026-10-14)

Supersedes `BENCHMARK-2026-06-20.md` (schema 006, single queries only). This one
runs the real binary under concurrent HTTP load. The first 10-12 run used a
seed that gave every note the same timestamp; every number below is from the
fixed seed, re-measured on 10-14.

## Setup

- Binary: built from `7db233e` — this branch merged with `main` `5188fb9`
  (schema through migration 015; no app code differs from `main`).
- Data: `scripts/bench-seed.sql` — 100 users × 1000 notes = 100K notes
  (94,140 readable, 84,607 recommendable), 294K note_tags, 100K resolved
  links, 100K likes, 1K follows. Timestamps are spread over the year ending at
  seed time (8,191 notes in the last 30 days).
- Tags are `floor(500·U³)`, not Zipf: a steep head and a flat tail. `t0` is on
  33,291 notes, `t10` on 2,595, `t100` on 619; the median tag has 316 and the
  rarest 176. The "rare tag" below is `t499` (199 notes).
- Search term `w1234` matches 1,982 notes (5,000-word vocabulary), unlike the
  06-20 run where every note matched.
- Box: one 4-core / 15 GB container running **everything** — Go server, Postgres
  16 (default config, `shared_buffers` 128 MB), and the load generator. Absolute
  numbers are this box, not Render (Starter = 0.5 CPU, Postgres 17 Basic-1gb).
  Read them as ratios and as the shape of the curve. Nothing else was running.
- Load: [vegeta](https://github.com/tsenart/vegeta) v12.12.0, open-loop (fixed
  arrival rate), driven by `scripts/bench-load.sh`. Per-query cost from
  `pg_stat_statements`. During each run the script samples `pg_stat_activity`
  (client backends of this DB, excluding its own) every 0.5 s, counts new
  Postgres sessions (`pg_stat_database.sessions`), and reads whole-box CPU from
  `/proc/stat`.
- App pool: `SetMaxOpenConns(25)`, `MaxIdleConns` unset — `database/sql`'s
  default of 2 (`internal/db/db.go`).

## 1. Per endpoint, uncontended (mean of 10 sequential requests; range over 4 runs)

| Endpoint | Wall | Dominant query (mean) | 判讀 |
| --- | --- | --- | --- |
| `/u005/note-5` note page | 5–7 ms | — | 快 |
| `/u005` profile | 6–7 ms | — | 快 |
| `/tag/t499` rare tag | 9–14 ms | related-tag cloud 2–3 ms | 快 |
| `/feed?tab=following` | 8–10 ms | card query ~2 ms | 快 |
| `/api/graph/local` 1 or 2 hops | 2–3 ms | — | `maxLocalGraphNodes` 有效 |
| `/api/wiki/suggest?q=w1234` (16 titles match) | 77–95 ms | `lower(n.title) LIKE '%…%'` over all notes 57–71 ms + the followed-authors variant 15–19 ms | 每打一個字就一次。沒有 trigram index；字越少見越慢 |
| `/api/wiki/suggest?q=note-12` (no match) | 116–151 ms | same, 96–129 ms | 最壞情況：沒命中就掃完全表 |
| **`/feed` (recommended, anon)** | **195–206 ms** | `WITH ranked AS (ROW_NUMBER() OVER (PARTITION BY author_id …))` 188–197 ms | **最常被打的頁最慢**。每次載入都把 84,607 篇公開筆記全部排序（external merge sort） |
| **`/tag/t0` hot tag** | **135–267 ms** | related-tag cloud self-join 126–260 ms; card query ~2 ms | 只有熱門標籤慢：雲要 join 該標籤的全部 33K 篇 |
| **`/search?q=w1234`** | **268–299 ms** | the search query 266–297 ms | 見下：OR 讓 GIN index 用不上 |
| `/api/graph` (global) | 460–506 ms | node list 176–191 ms + edge list 71–82 ms | **19.1 MB**（gzip 7.0 MB）。沒有上限，隨筆記數線性長 |

**Search.** `EXPLAIN ANALYZE` of the shipped query (`search.go`), warm: 258–349
ms. Its `WHERE (n.search_tsv @@ query OR u.handle ILIKE $2)` spans both tables,
so Postgres cannot use `idx_notes_search_tsv`: it seq-scans the 94,140 readable
notes, joins users, and evaluates the OR on every row (`Rows Removed by Join
Filter: 92282`). `ts_headline` is not the cost — it runs above the top-N sort,
on the 50 returned rows only. The same query with just the FTS predicate uses
a bitmap scan on the GIN index and runs in 13–23 ms.

**Tag chips** (the 06-20 bottleneck). The uncached `loadTopTagChips` aggregate
runs in 36–49 ms: it reads only the 6,937 recent recommendable notes through
`idx_notes_recommended`, but seq-scans all 294K note_tags to join them.
`topTagChips` (`feed.go`) serves it from a 5-minute in-process cache, so that
cost is paid once per 5 minutes per process. A refill is not deduplicated:
requests that arrive while the cache is stale each run it.

## 2. Mixed read load (16-target round robin: 3× feed, 6× note, 3× profile, rare tag, hot tag, search, following; 20 s per rate)

| Rate | p50 | p90 | p99 | Throughput | DB conns: max active / max open | New PG sessions | CPU |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 5/s | 14 ms | 271 ms | 412 ms | 5.0/s | 3 / 3 | 2 | 16% |
| 10/s | 16 ms | 268 ms | 352 ms | 10.0/s | 2 / 3 | 12 | 28% |
| 20/s | 26 ms | 287 ms | 429 ms | 19.7/s | 5 / 6 | 87 | 53% |
| **40/s** | **109 ms** | **1.4 s** | **2.1 s** | **38.6/s** | **23 / 23** | **234** | **97%** |

40/s sits right at this box's limit and is not stable run to run. A second
full run matched the table (p50 106 ms, p99 2.1 s, 24 of 25 active, CPU 97%),
but the verification review's five repeats of the 40/s step alone gave p50
31 / 127 / 35 / 30 ms and p99 0.54 / 2.8 / 0.76 / 0.45 s: only one tipped into
the 23-active, 97%-CPU, falling-behind state; the others ran at 88–93% CPU
with 7–8 active connections. So the ceiling is the CPU, reached somewhere
around 40 req/s of this mix, not a clean knee between 20 and 40.
The pool does fill, but with CPU-bound queries queueing in it, not as the
limit — in the single-endpoint runs below all 25 connections are active and
the box is already at 97–98%. The p90 at low rates is the three slow
endpoints above.

Single endpoint at 20/s (10 s each):

| Endpoint | p50 | p99 | Throughput | DB conns active / open | CPU |
| --- | --- | --- | --- | --- | --- |
| `/feed` | 2.2 s | 4.8 s | 17.0/s | 25 / 25 | 97% |
| `/tag/t0` | 2.4 s | 7.1 s | 14.6/s | 25 / 25 | 98% |
| `/search?q=w1234` | 3.6 s | 12.2 s | 12.1/s | 25 / 25 | 97% |
| `/api/wiki/suggest?q=note-12` | 96 ms | 138 ms | 19.8/s | 3 / 3 | 53% |
| `/tag/t499` | 16 ms | 23 ms | 20.1/s | 1 / 2 | 15% |
| `/u005` | 9 ms | 15 ms | 20.1/s | 1 / 2 | 10% |
| `/u005/note-5` | 9 ms | 13 ms | 20.1/s | 1 / 2 | 10% |

Each of `/feed`, `/tag/t0` and `/search` alone saturates this box below 20
req/s.

## 3. Cheap pages only (30 note pages + 10 profiles; 15 s per rate)

| Rate | p50 | p99 | Throughput | DB conns active / open | New PG sessions | CPU |
| --- | --- | --- | --- | --- | --- | --- |
| 100/s | 8 ms | 14 ms | 100/s | 1 / 2 | 0 | 32% |
| 200/s | 147 ms | 380 ms | 198/s | 8 / 25 | 1,528 | 90% |
| 400/s | 286 ms | 818 ms | 395/s | 4 / 25 | 57 | 84% |

The jump at 200/s is not query cost (at most 8 connections active) but
reconnecting: with `MaxIdleConns` at its default of 2, `database/sql` closes
every released connection beyond two, so each burst re-dials Postgres (TCP,
SCRAM auth, a new backend process) — 1,528 new sessions for 3,000 requests,
with CPU at 90%.

**Experiment, not in this PR:** the same binary plus one line,
`d.SetMaxIdleConns(25)`:

| Load | p50 | p99 | New PG sessions | CPU |
| --- | --- | --- | --- | --- |
| cheap 100/s (2 runs) | 7 ms | 12–13 ms | 0–2 | 27–29% |
| cheap 200/s (2 runs) | 7 ms | 17–22 ms | 0–3 | 49–50% |
| cheap 400/s (2 runs) | 20–37 ms | 224–236 ms | 0–19 | 80–82% |
| mixed 20/s | 15 ms | 400 ms | 0 | 48% |
| mixed 40/s (5 runs incl. review) | 17–18 ms | 0.46–0.96 s | 0 | 90% |

At 400/s the baseline opened only 57 sessions yet stayed slow (p50 286 ms with
at most 4 connections active); these runs do not isolate why, only that keeping
idle connections removes it. The single-endpoint `/feed`, `/tag/t0` and
`/search` runs did not change with it (17.2, 15.2, 12.7 req/s): those are
CPU-bound in Postgres.

## 4. Write contention (u001..u010, 15 s per rate)

| Workload | Rate | p50 | p99 | Status |
| --- | --- | --- | --- | --- |
| Autosave `PATCH /api/notes/{id}`, each user on their own draft (body with 2 tags + 2 wiki links, so tags/links recompute) | 50/s | 9 ms | 16 ms | 750 × 200 |
| same | 100/s | 8 ms | 13 ms | 1,500 × 200 |
| Like toggle, all 10 users on **one** note | 100/s | 3 ms | 10 ms | 1,500 × 200 |
| same | 300/s | 3 ms | 16 ms | 4,500 × 200 |

At most 2 connections active; no deadlock in the Postgres log. Writes are not
the problem; no write queue needed at this scale.

## Conclusions

1. **The read path tops out on CPU, around 40 mixed req/s on 4 cores.**
   The pool fills at 40/s only because CPU-bound queries queue in it; with all
   25 connections active the box is already at 97%, so a bigger pool would
   only move the queue into Postgres.
2. **The pool's idle setting is the cheapest win.** `MaxIdleConns` is unset
   (default 2), so bursts reconnect to Postgres. Setting it to the open cap
   took cheap pages at 200/s from p50 147 ms to 7 ms, and mixed 40/s from a
   baseline p50 of 30–127 ms (unstable, see §2) to a steady 17–18 ms.
   One line in `internal/db/db.go`.
3. Three reads are O(table), and each saturates the box alone below 20 req/s:
   - `/feed` recommended (the front door): the per-author cap ranks all 84,607
     public notes on every load. Untested direction: rank only the newest
     slice via `idx_notes_recommended` (e.g. within the last N×limit rows, or a
     `LATERAL` top-k per author), so cost follows page size, not table size.
   - `/search`: the `OR u.handle ILIKE` turns an index search into a full
     scan. Match handles in a separate query (or a `UNION` branch) so the FTS
     branch uses the GIN index — that predicate alone measured 13–23 ms.
   - Hot-tag related cloud: cost follows the tag's note count (`t0`, 33K
     notes: 126–260 ms; `t499`: 2–3 ms). Cache it like tag chips, or sample.
4. `/api/wiki/suggest` spends 57–129 ms in a seq scan per keystroke after
   `[[`, worst when nothing matches; 20/s of it takes half the box. A
   `pg_trgm` GIN index on `lower(title)` is the usual fix (not tested here).
5. `/api/graph` returns 19 MB at 100K notes with no cap; it needs one before
   the global graph is linked from anywhere popular.
6. Tag chips are fine as they are: 36–49 ms per refill, at most once per 5
   minutes per process. The hot-tag card query and the following feed are
   ~2 ms; the 127 ms and 14 ms in the first 10-12 run came from the seed bug
   (one shared timestamp, so `ORDER BY updated_at … LIMIT` sorted every match).

All of these grow with the table; at 100K notes they are the first things to
fix.

## Reproduce

```sh
DB=postgres://commaplace:commaplace@localhost:5432/benchmark
psql "${DB%/*}/postgres" -c 'CREATE DATABASE benchmark'
psql "$DB" -c 'CREATE EXTENSION pg_stat_statements'  # needs it in shared_preload_libraries
go build -o /tmp/bench-srv ./cmd/server
DATABASE_URL="$DB?sslmode=disable" timeout 15 /tmp/bench-srv           # migrates
psql "$DB" -v ON_ERROR_STOP=1 -f scripts/bench-seed.sql                 # ~40 s
ADDR=:8297 DEBUG=1 DATABASE_URL="$DB?sslmode=disable" /tmp/bench-srv &
go install github.com/tsenart/vegeta/v12@v12.12.0
DB=$DB scripts/bench-load.sh        # every section; or name some of:
                                    # single diag mix cheap writes
```

The script mints session cookies via `/_dev/login` (hence `DEBUG=1`), looks up
each user's draft id with psql, writes the vegeta target files, and prints
each batch's latency summary with its DB-connection, session and CPU samples.
The `writes` section edits the first draft of u001..u010 and toggles likes on
`u005/note-5` an even number of times.
