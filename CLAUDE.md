# CLAUDE.md

## 1. Think Before Coding

State assumptions explicitly. If multiple interpretations exist, surface them — don't pick silently. If a simpler approach exists, say so. If something is unclear, stop and ask.

## 2. Simplicity First

Minimum code that solves the problem. No features, abstractions, configurability, or error handling beyond what was asked. If 200 lines could be 50, rewrite it. "Would a senior engineer call this overcomplicated?" — if yes, simplify.

## 3. Surgical Changes

Touch only what you must. Don't improve adjacent code, refactor what isn't broken, or reformat. Match existing style. Note unrelated dead code; don't delete it. Clean up only the orphans your own changes created. Every changed line should trace to the user's request.

## 4. Goal-Driven Execution

Define verifiable success criteria, then loop until met.
- "Add validation" → write tests for invalid inputs, then make them pass
- "Fix the bug" → write a test that reproduces it, then make it pass
- "Refactor X" → tests pass before and after

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.


## 5. Dev Commands

See Makefile. Prefer `make watch` for hot-reload.

## 6. Adding Features Should Not Cause Regression in Existing Ones

## 7. Design Tokens Are the Source of Truth

Every text/background pair must pass WCAG AA (4.5:1, or 3:1 for text ≥24px), every font-size uses a `--fs-*` token (never literal px), every radius uses a `--r-*` token, and token names describe role not value (`--ink`/`--paper`, never `--black`/`--white`).

---

## Project Context
See @.claude/htmx-rules.md for htmx guidelines
See @README.md for what the project does, where things live, and how work gets done.
See @docs/DESIGN_PROMPTS.md for visual design direction (colors, radii, spacing, animation) — read before writing any CSS
See @docs/DECISIONS.md for the why/when-to-break-it behind past architecture calls (build tooling, DB, htmx/Alpine/templ, fonts)
See @docs/RUNBOOK-db-purge.md before any destructive change to the live DB (wipes, migration squashes, 封測→公測) — dump first, purge via a migration, never ad-hoc psql

---

# Codebase Sitemap

Note: the `internal/external` package (Obsidian Publish/Quartz crawler, `/x/{vault}` routes,
`external_*` tables) described in older docs/specs no longer exists — removed entirely, not
just undocumented. Cross-vault linking is the `links` table below, nothing else.

## Entry & Internal Packages

```
cmd/server/main.go — Config, DB open+migrate, seed, auth init, routes, ListenAndServe
internal/config/config.go — Site/Nav/Email config, pagination, auth TTL, session cookie
internal/db/db.go — Postgres pool (pgx/v5), idempotent migration runner (go:embed migrations/*.sql)
internal/db/migrations/ — 001_init.sql … 005_note_tags_tag_lower_index.sql
internal/auth/auth.go — Magic-link tokens, HMAC session cookies, findOrCreateUser
internal/auth/oauth.go — Google OAuth (start/callback), account linking by email
internal/auth/smtp.go — SMTP Mailer impl
internal/markdown/render.go — Goldmark + wikilinks, callouts, math, tags, heading IDs, Excerpt()
internal/markdown/wikilink.go — [[...]] parser → WikiLink (user/folder/slug/anchor/alias)
internal/seed/demo_notes.go — CJK + English demo content, idempotent
internal/seed/dev.go — SEED_DEV multi-user fake data (alice/bob/carol/dave)
internal/seed/tour.go — Onboarding tour notes (SEED_TOUR=1)
```

## HTTP Handlers (internal/handlers/)

```
handlers.go — Routes() mux, gzip + cache-control middleware, GetCatchAll (profile/note/assets)
render.go — Server struct (DB/Auth/BaseURL/...), chrome()/renderPage()/renderFragment()/pageTitle()
auth.go — /login, /auth/{token}, /auth/google[+/callback], /logout, /me, /_dev/login
notes.go — CRUD + saveNote/recomputeLinks/loadBacklinksSplit/loadOutgoingSplit/absoluteNoteURL
note_image.go — GET/POST /api/notes/{id}/image, resolveOGImage (og:image fallback)
import.go — GET/POST /import, POST /import/save-one — single + batch .md upload
feed.go — GET /feed — cursor pagination (50/page, HTMX infinite scroll)
profile.go — GET /{user} — recent notes, follow counts
calendar.go — GET /me/calendar — month-grid note view
graph.go — /graph shell + /api/graph (all/local/per-tag/per-user)
wiki.go — GET /api/wiki/suggest, GET /api/tags/suggest — [[ and # autocomplete
search.go — /search (tsvector) + /api/search/palette (Cmd+K fuzzy title search)
tags.go — /tag/{tag} — paginated
palette.go — Cmd+K palette data helper
likes.go — POST /api/like, GET /me/saved
follow.go — POST /api/follow
admin.go — /admin dashboard, /admin/reports, POST /admin/hide
export.go — /api/notes/{id}/raw
onboarding.go — /onboarding, POST /onboarding/fork
settings.go — POST /settings/theme
reports.go — POST /api/report
avatar.go — GET/POST /me/avatar (builder), GET /u/{handle}/avatar.png
avatar_compose.go — PNG composition from part IDs + skin color
*_test.go — one per handler file above, plus link_regression_test.go, social_test.go
```

## Templates & Static (internal/handlers/)

`html/template` is gone entirely — every page is `templ` (github.com/a-h/templ).
`go tool templ generate` (pinned as a Go tool dependency in go.mod, no global
install) turns `*.templ` into `*_templ.go`; the generated `.go` is committed
(not gitignored) so the deploy path stays pure `go build` — see
`docs/DECISIONS.md` 5. `make watch` runs `templ generate` before each rebuild
(`.air.toml`).

```
layout.templ — Layout component: nav, head/meta defaults, footer, theme toggle (was _base.html)
notes_view.templ — NoteListView + feedCard + cardRenderers registry (one entry today: "masonry").
                    Shared by feed/tag/search/profile/saved/preview — one data source, one rendering.
                    Adding a layout later = one func(NoteListView) templ.Component + one registry line.
{feed,tag,search,profile,saved}_page.templ — one page per list surface; each builds
                                              its own header chrome, then embeds @notesView(view)
notes_pages.templ — write, note, note_stub
{admin,avatar_builder,calendar_page,graph_page,import_page,import_batch_page,
 login,legal,onboarding,error_content}.templ — the rest, one page (or two, for
 import/import_batch and admin's two views) per file
static/style.css — Mobile-first, dark/light, design tokens in :root
static/{cmeditor,graph,copy,share,tagsearch,palette,reveal,opencc-toggle,import-batch}.js
static/htmx.min.js, d3-force.min.js, easymde.min.{js,css}, opencc.min.js — vendored
static/og-default.png — fallback og:image (generated from comma_mascot_logo.svg)
```

## Routes

```
GET  /                              → redirect /feed
GET  /login  POST /login            Login form / issue magic-link
GET  /auth/{token}                  Consume token, set session
GET  /auth/google[+/callback]       Google OAuth
POST /logout
GET  /me   GET /me/calendar
GET  /feed                          Social feed (HTMX ?older=)
GET  /write   POST /write           Create;  POST /preview  Live preview fragment
PATCH /api/notes/{id}   POST /api/notes/{id}/publish   Autosave + publish (draft model)
GET  /edit/{id}   POST /delete/{id}   POST /api/notes/bulk-delete
GET  /import  POST /import   POST /import/save-one
GET  /tag/{tag}   GET /tag/{tag}/graph
GET  /search?q=   GET /api/search/palette?q=
POST /api/like   GET /me/saved
POST /api/follow
GET  /api/wiki/suggest?q=   GET /api/tags/suggest?q=
GET  /graph   GET /api/graph   GET /api/graph/local   GET /u/{user}/graph
POST /settings/theme
GET  /me/avatar   POST /me/avatar   GET /u/{handle}/avatar.png
GET  /onboarding   POST /onboarding/fork
POST /api/report
GET  /admin[/{$}]   /admin/reports   POST /admin/hide
GET  /api/notes/{id}/raw
GET  /api/notes/{id}/image   POST /api/notes/{id}/image
GET  /assets/...                    Static (embedded, served by catch-all)
GET  /_dev/login                    Skip-login; needs Debug or ?key=PLAYTEST_LOGIN_KEY
GET  /{user}   GET /{user}/{slug}    Catch-all: profile or note view
```

## Database Schema (Postgres, `internal/db/migrations/001_init.sql` + later migrations)

```
users        — id, handle, handle_ci, email, theme, pinned_note_id, onboarded_at, avatar, avatar_choice
notes        — id, author_id, slug, slug_ci, title, body_md, search_tsv, *_at, hidden_at, deleted_at, published_at
note_tags    — note_id, tag, created_at
links        — source_note_id → target_user_handle/target_slug/resolved_target_id
likes        — user_id, note_id, created_at
follows      — follower_id, followed_id, created_at
auth_tokens  — token, email, expires_at, used_at
reports      — note_id, reporter_id, reason, status, created_at
note_images  — note_id (PK), image (bytea), content_type, created_at
schema_migrations — version, applied_at
```
Full-text search: `notes.search_tsv` (tsvector, GIN-indexed), kept in sync by a
`BEFORE INSERT OR UPDATE` trigger — not a separate FTS virtual table.
