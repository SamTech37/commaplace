# commaplace

## About the user
我不會任何程式語言，只會寫 markdown。當你要跟我解釋功能、或說明你改了什麼時：
- 不要預設我看得懂任何技術字眼（function、handler、migration、schema、HTMX、CSS class、FTS、bm25、cookie、HMAC… 等等通通算）。
- 用日常語言描述「使用者會看到 / 體驗到什麼變化」，而不是「我改了哪個檔案、加了什麼欄位」。
- 如果非得提到技術名詞，請當場用一句話翻譯成白話。
- 程式碼層的細節（檔名、行號、SQL）我看不懂，除非我問，否則不必貼給我。

## What this is
A markdown-based social knowledge platform — Pinterest × Obsidian × 小紅書.
Users publish folders of markdown notes; readers browse a feed, dive into 
someone's note, follow internal `[[wiki links]]` into the same vault or 
`[[@user/note]]` into another person's vault. The killer feature is 
cross-vault rabbit holes.

## v0 scope (everything else is out)
- Auth (magic link)
- User has one vault, vault contains folders, folders contain markdown notes
- Note editor: textarea + live preview
- Wiki link syntax: `[[note]]` (same vault) and `[[@user/note]]` (cross-vault)
- Public profile page at /[user] showing folder tree + recent notes
- Note view at /[user]/[...path] rendering markdown + clickable wiki links
- Backlinks panel on note view (computed from a links table)
- /feed showing 50 most recent notes site-wide

## Tech stack
- Go (≥1.22, for `net/http` enhanced pattern matching), server-rendered HTML
- `net/http` stdlib for routing
- `html/template` stdlib for templates
- SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- Auth: hand-rolled magic link via stdlib `net/smtp` + signed cookies (`crypto/hmac`)
- `goldmark` for markdown + a custom wiki-link extension
- HTMX vendored as `static/htmx.min.js` for incremental interactivity
- Hand-written CSS in `static/style.css` — no build tooling
- Single static binary; deploy to Fly.io or any VPS

## Data model
The three application tables. (Auth uses an additional `auth_tokens` table; it's plumbing, not part of the domain model.)

- users: id INTEGER PK, handle TEXT UNIQUE, email TEXT UNIQUE, created_at
- notes: id INTEGER PK, author_id, folder_path, slug, title, body_md, created_at, updated_at
  - UNIQUE(author_id, folder_path, slug)
- links: id INTEGER PK, source_note_id, target_user_handle, target_folder_path, target_slug, resolved_target_id (nullable)
  - recomputed every time a note is saved
- backlinks are just a query: links where resolved_target_id = me

## Conventions
- Server-rendered HTML, no client-side JS framework. Reach for HTMX only when a full page reload would feel worse than the alternative.
- **Mobile-first UI.** Design and write CSS for phone width first; on wider viewports the content column stays the same width and the extra space becomes symmetric whitespace on both sides. No multi-column or sidebar layouts in v0.
- All DB access via `database/sql` + `modernc.org/sqlite`. SQL strings live next to the handler that uses them; no ORM, no query builder.
- File layout:
  - `cmd/server/main.go` — entry point + route registration
  - `internal/db/` — connection, migrations runner, shared query helpers
  - `internal/auth/` — magic link, sessions, current-user helper
  - `internal/markdown/` — goldmark extension + rendering
  - `internal/handlers/` — HTTP handlers grouped by feature
  - `templates/` — `html/template` files
  - `static/` — `htmx.min.js`, `style.css`, anything served directly
  - `migrations/` — numbered `.sql` files, applied at startup
- Commit messages: imperative mood, < 72 chars
- Run `go vet ./...` and `go test ./...` before considering anything done

## Commands
- `go run ./cmd/server` — start dev server
- `go test ./...` — run tests
- `go vet ./...` — static checks
- `go build -o bin/commaplace ./cmd/server` — production binary

## Things to ask me before doing
- Adding any new dependency
- Changing the wiki link syntax
- Anything that changes the data model
