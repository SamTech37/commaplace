# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A markdown-based social knowledge platform — Pinterest × Obsidian × 小紅書. Users publish folders of markdown notes; readers browse a feed and follow [[wiki links]] into same vault or [[@user/note]] into another person's vault.

## v0 scope

- Magic link auth, magic link emails printed to stdout in dev
- Users: one vault with folders of markdown notes
- Note editor with live preview
- Wiki links: `[[note]]` (same-vault) and `[[@user/note]]` (cross-vault)
- Public profiles at `/[user]` with folder tree + recent notes
- Note view at `/[user]/[...path]` with clickable wiki links and backlinks
- `/feed` showing recent notes site-wide, `/tag/{tag}` for tags

## Tech stack

- **Go 1.22+** (http enhanced routing), server-rendered HTML
- **SQLite** via modernc.org/sqlite (pure Go, no CGO)
- **Auth**: hand-rolled magic links (stdlib smtp + hmac-signed cookies)
- **Markdown**: goldmark + custom wiki-link extension
- **Frontend**: HTMX for incremental UX, vanilla CSS (no build tools)
- Single binary, deploy anywhere

## Commands

```bash
go run ./cmd/server                      # dev server
go test ./... && go vet ./...            # tests + lints
go test ./internal/markdown/...          # single package tests
go build -o bin/commonplace ./cmd/server # production binary

SEED_TOUR=1 go run ./cmd/server          # also seed legacy onboarding content
DEBUG=1 go run ./cmd/server              # enable /_test/markdown debug routes
```

## Architecture

### Request flow

`main.go` wires `db.Open` → `db.Migrate` → `seed.ApplyDemo` → `auth.Auth` → `handlers.Server`. The `Server.Routes()` method in `internal/handlers/handlers.go` registers all URL patterns using Go 1.22 method+path syntax (e.g. `"GET /feed"`). Profile and note views (`/{user}`, `/{user}/{path...}`) are registered last to avoid shadowing other routes.

### Template pipeline

Templates live in `internal/handlers/templates/`. `render.go:LoadPages` pre-parses each page template alongside `_base.html` so `{{block "content"}}` overrides apply. HTMX partial responses use `RenderPartial`, which wraps a fragment template alongside `feed.html` (to inherit the `card` template definition). Adding a new page requires: new `.html` file + entry in the `pageNames` slice in `LoadPages`.

### Wiki link system

`internal/markdown/wikilink.go` parses `[[slug]]` and `[[@user/slug]]` with no goldmark dependency — pure string scanning so it can be called at save time for link extraction (`Extract`). `internal/markdown/render.go` wires this into goldmark as an AST extension. On every note save, `handlers.RecomputeLinks` re-extracts links and upserts the `links` table; backlinks are a reverse query on that table.

### External vaults

`internal/external/` crawls Obsidian Publish and Quartz sites. Flow: admin pastes a URL → `external_vaults` row (status=pending) → background `Worker` calls `Crawler.Crawl` → populates `external_notes` + `external_links` → served at `/x/<vault-slug>/<note-slug>`. The `Fetcher` interface in `types.go` is the swap point for new upstream formats.

### Database migrations

`internal/db/db.go` embeds `migrations/*.sql` and applies them in numeric order via `schema_migrations` version tracking. To add a schema change: create `NNN_description.sql` (next number in sequence). Migrations run in a transaction and are idempotent across restarts.

### Configuration

All config is read from environment variables in `cmd/server/main.go:loadConfig`. No config file. Key vars: `DB_PATH`, `ADDR`, `SESSION_SECRET`, `BASE_URL`, `SMTP_HOST`/`SMTP_PORT`/`SMTP_USER`/`SMTP_PASS`/`SMTP_FROM`, `ADMIN_HANDLE`, `DEBUG`. Copy `.env.example` for local dev. When `SMTP_HOST` is empty, magic links print to stdout.

## Data model

- `users`: handle, email, created_at
- `notes`: author_id, folder_path, slug, title, body_md, tags, pinned, hidden, created_at, updated_at (UNIQUE per author+path+slug)
- `links`: parsed from notes at save time, resolves targets; backlinks are reverse queries
- `tags`, `note_tags`: tag taxonomy
- `likes`: user↔note join table
- `follows`: user↔user join table
- `external_vaults`, `external_notes`, `external_links`: crawled third-party vaults
- `reports`, `schema_migrations`: moderation + migration tracking

## Conventions

- **No JS framework**; HTMX for when full reload is worse
- **Mobile-first** CSS; wider screens get whitespace, not sidebars
- **SQL next to handlers**; no ORM
- **File layout**: `cmd/server/` → `internal/{db,auth,markdown,handlers,external,config,seed}` → `templates/` + `static/`
- **Before commit**: `go vet ./...` and `go test ./...`
- **Commit messages**: imperative, < 72 chars

## Ask me first

- New dependencies
- Changes to wiki link syntax
- Data model changes
