# commonplace

A markdown-based social knowledge platform — Pinterest × Obsidian × 小紅書.

Users publish a vault of markdown notes; readers browse a feed, dive into
someone's note, follow internal `[[wiki links]]` into the same vault or
`[[@user/note]]` into another person's vault. The killer feature is
**cross-vault rabbit holes**.

## Features (v0)

- **Magic-link auth** — no passwords, just an emailed link
- **One vault per user** — a flat collection of markdown notes
- **Wiki links** with two flavours:
  - `[[note]]` — same vault
  - `[[@user/note]]` — into someone else's vault
- **Live preview** editor (textarea + rendered HTML side-by-side)
- **Profile pages** at `/[user]` showing recent notes
- **Note view** at `/[user]/[...path]` with clickable wiki links + backlinks
- **Feed** (`/feed`) — recommended + following tabs, masonry cards
- **Graph view** (`/graph`) — Obsidian-style force-directed map of every note and its links
- **Tags, likes, follows, search, reports** — the small social loop
- **Markdown export** — download or copy any note as raw `.md`
- **Avatar builder** (`/me/avatar`) — pick face/eyes/mouth/accessory + skin color; served as PNG at `/u/{handle}/avatar.png`

## Stack

- Go ≥ 1.22 (uses `net/http` enhanced pattern matching)
- Postgres via `github.com/jackc/pgx/v5` (full-text search via `tsvector` + GIN index)
- `goldmark` for markdown + a custom wiki-link extension
- HTMX (vendored) for incremental interactivity
- Hand-written CSS, no build tooling
- Ships as a **single static binary**

## Prerequisites

- [Go ≥ 1.22](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) — runs Postgres locally via Compose
- [air](https://github.com/air-verse/air) for live reload: `go install github.com/air-verse/air@latest`
- **Windows users**: requires [Git Bash](https://gitforwindows.org/) or WSL — native cmd/PowerShell is not supported

## Running

**Day-to-day development** (hot reload + seed data + auto-opens browser):

```sh
make watch
```

**First time or after a schema change** (one-shot, no reload):

```sh
make dev-full
```

Both commands start Postgres automatically via Docker and log in as `alice` at `/_dev/login`.

**Other commands:**

```sh
make dev          # dev server, no seed data, no browser open
make dev-oauth    # dev + Google OAuth (set GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET first)
make test         # run all tests against a separate test DB
make db-down      # stop Postgres
```

**Production build:**

```sh
go build -o bin/commonplace ./cmd/server
```

## Environment variables

| Var | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:8080` | Listen address |
| `DATABASE_URL` | `postgres://commaplace:commaplace@localhost:5432/commaplace?sslmode=disable` | Postgres connection string |
| `BASE_URL` | `http://localhost:<port>` | Used in magic-link emails |
| `SESSION_SECRET` | auto-generated (`.session_secret`) | Hex-encoded HMAC key for session cookies |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASS` / `SMTP_FROM` | unset | If unset, magic links are printed to stdout (dev mode) |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | unset | Enables the "Continue with Google" button on `/login`; redirect URI is `${BASE_URL}/auth/google/callback` |
| `ADMIN_HANDLE` | unset | Handle granted access to `/admin/*` |
| `DEBUG` | `0` | Verbose error pages + unlocks `/_dev/login` unconditionally. Leave unset on any deploy reachable outside the team |
| `PLAYTEST_LOGIN_KEY` | unset | Unlocks `/_dev/login?as=<handle>&key=<this>` without needing `DEBUG` — shared-secret login for playtests when SMTP/OAuth aren't set up |
| `SEED_DEV` | `0` | Install multi-user fake data (alice/bob/carol/dave) on startup |
| `SEED_TOUR` | `0` | Also install the legacy English onboarding-fork seed |

## Layout

```
cmd/server/        entry point + config
internal/db/       connection, migrations runner
internal/auth/     magic-link sessions
internal/markdown/ goldmark + wiki-link extension
internal/handlers/ HTTP handlers, templates, static assets
internal/seed/     demo / onboarding seed content
migrations/        numbered .sql files, applied at startup
```

## Conventions

- Server-rendered HTML; HTMX only when a full reload would feel worse
- Mobile-first CSS; on wide screens the column stays narrow with symmetric margins
- All DB access via `database/sql` — no ORM, no query builder
- SQL strings live next to the handler that uses them
- Commit messages: imperative mood, < 72 chars
