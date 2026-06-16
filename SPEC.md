# SPEC: Postgres + Railway Rebuild

## Goal

Rip out SQLite and rebuild commonplace's backend on Postgres + Railway, with
the core schema (users, notes, tags, links, likes, follows) redesigned
around UUID primary keys and link/pointer tables resolved by ID rather than
by name. **There are no real deployed instances and no production data** —
this is not a live migration with rollback/back-compat concerns, it's a
clean rip-and-replace: drop the SQLite driver, write a fresh Postgres
schema, point the app at it. The goal is to land the current feature set on
infra that scales horizontally, bills pay-as-you-go, and doesn't depend on
a single long-lived VM — not to ship anything new.

## Scope

**In:**
- Postgres as the database engine (Railway-hosted, single provider for both
  DB + compute — see Design decisions).
- Schema overhaul: `users`, `notes`, `tags`/`note_tags`, `links`, `likes`,
  `follows` rebuilt with UUID PKs and join/pointer tables.
- Drop `external_vaults` / `external_notes` / `external_links` entirely
  (external vault crawling becomes a manual/offline one-off process if
  needed later, not a deployed feature).
- Drop the dead `folder_path` column on `notes` (folders were already
  removed from the product).
- Note images: max 1 per note, stored as `bytea` in a side table, served via
  a dedicated route (same pattern as the existing avatar PNG endpoint) — not
  inlined into the `notes` row, not base64.
- Docker Compose local Postgres for `make dev` (dev/prod parity — same
  engine locally and in production, no SQLite-vs-Postgres adapter gap).
- Graceful shutdown (`SIGTERM` → drain → exit) — **done**,
  `cmd/server/main.go`.

**Out (explicit non-goals for this pass):**
- No new product features. Unchecked Must-Haves in `plan.md` (full search,
  permissions, paywall, meta-app views) wait for a separate pass.
- Tag picker UX (usage-sorted autocomplete, friction on creating new tags)
  — schema supports it (usage count is a derived `COUNT(*)` over
  `note_tags`), but the picker behavior itself is deferred; brief tracked in
  `plan.md` under the tag-merging item.
- Search engine replacement. SQLite FTS5 has no direct Postgres equivalent
  — this needs its own decision (built-in `tsvector` vs `pg_trgm` vs
  deferring to a future vector-search pass) and is **not resolved yet** (see
  Open questions).

## Design decisions

**Deploy model: scale-to-zero container, not function-per-request.**
The app is one Go binary on `net/http` with an in-process background
worker. A container platform (Railway) runs it unchanged. True
function-per-request serverless (Vercel/Lambda) would require splitting
into stateless per-route handlers and killing the in-process worker outright
— not worth the rewrite for this app's shape. Cloudflare Workers ruled out:
no native Go runtime.

**Provider: Railway, single provider for DB + compute.**
Railway bills per vCPU-second/RAM-second (idle ≈ free), vs. Render's flat
per-plan pricing regardless of utilization. Single provider keeps DB and
compute in the same region by default, avoiding cross-provider network
latency on every query — this app does many small synchronous DB round
trips per page render, so that latency matters more than picking
best-in-class per category.

**Identity resolution: UUID pointers, not name resolution.**
`users.handle` and `notes.slug`/`title` are mutable display strings.
Renaming either must never break an existing `[[@handle/note]]` link. So:
- `links.target_note_id` (and similarly any future user-pointer column)
  stores the resolved UUID, set once at link-recompute time.
- `links.raw_target` keeps the literal `[[...]]` text alongside the
  resolved ID. This lets an unresolved link (target doesn't exist yet)
  render as a stub, and re-resolve automatically once that note is created
  — without re-scanning markdown.
- Uniqueness is enforced on a case-folded shadow column (`handle_ci`,
  `slug_ci`), not the raw display column, so "Sam" and "sam" can't both
  exist as distinct handles.
- `notes` uniqueness is `(author_id, slug_ci)` — no dup within one vault,
  duplicates across vaults are fine (that's how multi-tenant note titles are
  supposed to work).

**Tags: usage count is derived, not denormalized.**
`tags.uses` is `COUNT(*) FROM note_tags GROUP BY tag_id`, computed live, not
a maintained counter column — avoids a write-amplification bug class
(forgetting to decrement on untag) for a number that isn't hot-path
performance-critical yet. Denormalize later only if the picker query proves
slow at real scale.

Schema can't stop "#SOBEAUTIFUL"-style misuse (X/FB-style tag spam for
emphasis instead of categorization) — that's a UX nudge (sort existing tags
by usage, make picking one the path of least resistance, make creating a
new tag a deliberate separate step), not a constraint. Schema's job here is
just to expose the count; behavior is a follow-up (tracked in `plan.md`).

**Composability pattern: skinny join tables, nothing embedded.**
Every relationship (`note_tags`, `links`, `likes`, `follows`, and any future
one) is its own table keyed by the two UUIDs it connects, with
`ON DELETE CASCADE`/`SET NULL` chosen per semantics — never a foreign key or
array embedded directly on `users`/`notes`. Adding a new relationship (e.g.
collections, canvases) later means one new table, zero migration risk to
the core three tables, because nothing else has a dangling FK into a table
that doesn't exist yet at the time it's added.

**`likes`/`follows` carry over from the current schema** (already the
correct join-table shape, `(user_id, note_id)` / `(follower_id,
followed_id)` composite PKs) — just re-keyed from `INTEGER` to `UUID` to
match the rest.

**External vault tables: dropped, not deprecated-in-place.** Clean
migration over carrying 3 unused tables forward. If the crawler feature
returns, it gets fresh tables built against whatever the schema looks like
at that point.

## Search (resolved)

Three asks in `plan.md`'s Must Have list: exact match, fuzzy (仿 Obsidian
Ctrl+O), vector semantic.

- **Exact + ranked full-text** → Postgres `tsvector`/`tsquery` with a GIN
  index. Direct FTS5 replacement — the current FTS5 setup weights title×2
  over body; `tsvector` does the same via `setweight('A', ...)` /
  `setweight('B', ...)` + `ts_rank`. No extension required.
- **Ctrl+O-style quick switcher** → this is fuzzy *subsequence matching over
  note titles* (jump-to-note by typing fragments), not full-text search —
  same shape as fzf/Sublime's go-to-file. One user's title list is tiny
  (hundreds, not millions), so this runs in-memory in Go, not in Postgres.
  `pg_trgm` is unnecessary for this specific feature (it solves
  typo-tolerant *body* search, a different problem).
- **Vector semantic search** → stays deferred (already future/Could-have
  scope). Moving to Postgres does settle the candidate debate though:
  sqlite-vector is dead once SQLite is gone, `pgvector` is the only
  contender — not deciding *when* to build it, just noting there's no
  longer a choice to make later.

## Open questions

- **Avatar PNG**: currently composed on the fly from part IDs
  (`avatar_compose.go`), not stored — assumed this stays a pure function
  and is unaffected by the DB migration. Not explicitly confirmed.
- **Connection pooling**: Railway Postgres + a long-lived container means
  the app's existing `database/sql` pool should work as-is (no serverless
  cold-connection-per-request problem) — but exact pool size tuning
  (`SetMaxOpenConns` etc.) for Postgres vs. SQLite's single-writer model
  hasn't been worked out.

## Out of scope for v0

- Tag picker UX implementation (brief written, not built — see `plan.md`).
- Search engine swap (FTS5 → Postgres) — separate decision + implementation
  pass.
- Any unchecked Must-Have from `plan.md` not already shipped (permissions,
  paywall, meta-app views beyond masonry/graph, 繁簡轉換, etc.) — this
  migration does not expand product scope.
- External vault crawling as a deployed/scheduled feature — dropped to
  manual/offline only, with no commitment to bring it back automated.
