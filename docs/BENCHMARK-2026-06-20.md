# Query benchmark — 2026-06-20

Local `benchmark` DB, 100 users × 1000 notes = 100K notes / 300K note_tags /
100K resolved links. Single query, no concurrency — relative cost, not
absolute latency. Referenced by `docs/DECISIONS.md`'s GraphQL/GraphDB
rejection (backlinks via bitmap scan is already fast) and by `plan.md`'s
Scaling section (tag chips is the one real bottleneck found).

| 查詢 | @100K | 判讀 |
| --- | --- | --- |
| Feed（`idx_notes_feed`） | **0.07 ms** | 跟 200 列一樣快——partial index 掃到 LIMIT 就停，不會排序 90K。撐得住預估規模 |
| **Tag chips**（`loadTopTagChips`，**每次 feed 都跑**） | **~40 ms** | **唯一真瓶頸**。整個 note_tags⋈notes join 做 HashAggregate，隨表成長 |
| Tag page | ~21 ms | 最慢的頁，可接受，盯著 |
| Backlinks（`idx_links_resolved`） | 快 | bitmap scan，沒問題 |
| FTS | n/a | 測試無效（每篇 seed 內文都命中），要換多樣語料重測 |

DB 們：prod（Render）、`commaplace`（dev）、`benchmark`（壓測）、`commaplace_test`（測試）。

Stale flag: run against schema as of migration 006. Migrations 007-009
(seed purge, `links.target_user_id`) landed after — numbers not re-verified
against current schema.
