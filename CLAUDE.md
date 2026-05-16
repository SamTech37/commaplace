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

## Data model

- `users`: handle, email, created_at
- `notes`: author_id, folder_path, slug, title, body_md, created_at, updated_at (UNIQUE per author+path+slug)
- `links`: parsed from notes, resolves targets, computed on every save
- `backlinks`: just a reverse query on links table

## Conventions

- **No JS framework**; HTMX for when full reload is worse
- **Mobile-first** CSS; wider screens get whitespace, not sidebars
- **SQL next to handlers**; no ORM
- **File layout**: cmd/server/ → internal/{db,auth,markdown,handlers} → templates/ + static/
- **Before commit**: `go vet ./...` and `go test ./...`
- **Commit messages**: imperative, < 72 chars

## Commands

```bash
go run ./cmd/server                      # dev server
go test ./... && go vet ./...            # tests + lints
go build -o bin/commonplace ./cmd/server # production binary
```

## Ask me first

- New dependencies
- Changes to wiki link syntax
- Data model changes
