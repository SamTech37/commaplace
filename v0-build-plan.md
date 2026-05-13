# v0 build plan

13 ordered tasks. Originally written for one-Plan-Mode-session-per-task pacing; now executed in a single sweep to MVP. Tasks remain useful as a checklist and as the structure of the codebase.

Stack: Go ≥1.22 (`net/http`, `html/template`) + SQLite (`modernc.org/sqlite`) + `goldmark` + HTMX. No ORM, no JS framework, no build tooling. See [CLAUDE.md](CLAUDE.md).

---

## 1. Scaffold

Init the project skeleton. After this, `go run ./cmd/server` shows a "commaplace" page.

- `go mod init commaplace`
- `cmd/server/main.go`: a `net/http` server on `:8080` with one handler returning a minimal HTML5 doc (`<!doctype html>` + `<meta name="viewport" content="width=device-width, initial-scale=1">` + `<h1>commaplace</h1>`). Viewport meta belongs from day one so mobile testing is meaningful even before CSS exists.
- Empty dirs: `internal/{db,auth,markdown,handlers}`, `templates`, `static`, `migrations`
- `.env.example` with `DB_PATH=./commaplace.db`, `SESSION_SECRET=`, `SMTP_HOST=`, `SMTP_PORT=`, `SMTP_USER=`, `SMTP_PASS=`, `SMTP_FROM=`
- `.gitignore`: `bin/`, `*.db`, `.env`
- First commit: "scaffold: go + net/http + sqlite-ready"

**Done when:** `go run ./cmd/server` serves a page that says "commaplace" at localhost:8080. `go vet ./...` passes clean.

---

## 2. DB schema

Create the three application tables (plus an `auth_tokens` table for task 3) and a tiny migrations runner.

- Add `modernc.org/sqlite` dependency
- `migrations/001_init.sql`: `users`, `notes`, `links`, `auth_tokens`. Use `INTEGER PRIMARY KEY`, `TEXT`, `INTEGER` for timestamps (unix seconds). Indexes on `notes(author_id, folder_path, slug)` UNIQUE, `links(resolved_target_id)`, `links(target_user_handle, target_folder_path, target_slug)`.
- `internal/db/db.go`:
  - `Open(path string) (*sql.DB, error)` opens the SQLite file with `?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)`
  - `Migrate(db *sql.DB) error` reads embedded `migrations/*.sql` (via `embed.FS`), tracks applied versions in a `schema_migrations` table, runs new ones in order
- Wire `cmd/server/main.go` to call `Open` then `Migrate` before listening

**Done when:** `go run ./cmd/server` boots clean. `sqlite3 commaplace.db ".schema"` shows all four tables. Re-running boot doesn't re-apply migrations.

---

## 3. Auth

Magic-link login. `/login` accepts an email; clicking the email link logs you in. `/me` shows your handle.

- `GET /login` renders a form (email field). `POST /login` inserts a row into `auth_tokens` (random 32-byte hex token, email, expires_at = now+15min) and emails a link `https://host/auth/<token>` via stdlib `net/smtp`. **Dev mode** (when `SMTP_HOST` is empty): write the link to stdout instead.
- `GET /auth/<token>`: validate (exists, not expired, not used). Find-or-create a `users` row using the email's local-part (slugified, ASCII-fold, dedup by suffix `-2`, `-3`, …) as the `handle`. Set a signed cookie `commaplace_session` containing `user_id` HMAC'd with `SESSION_SECRET`. Mark the token used. Redirect to `/me`.
- `internal/auth/session.go`: `Sign(userID int64) string`, `Verify(cookie string) (int64, error)` using `crypto/hmac` + `sha256`.
- `internal/auth/current.go`: `CurrentUser(r *http.Request) (*User, error)` reads the cookie and looks up the user.
- `GET /me` (auth required) renders the user's handle.

**Done when:** Submit email at `/login`, follow link from stdout, see `/me` with your handle. Refresh — still logged in. Restart the server — still logged in.

---

## 4. Note write path

Logged-in user hits `/write`, sees a form (title + folder_path + textarea), submits, a row lands in `notes`.

- `GET /write` renders the form (auth required)
- `POST /write` server-side handler:
  - Validate non-empty title; folder_path may be empty (root)
  - Auto-generate slug from title (kebab-case, ASCII-fold)
  - INSERT into `notes`. On UNIQUE(author_id, folder_path, slug) violation, re-render the form with an inline error
  - Redirect to the new note's URL `/{handle}/{folder_path}/{slug}` (will 404 until task 6)

**Done when:** I can write a note from `/write` and see the row via `sqlite3 commaplace.db "select id,folder_path,slug,title from notes;"`.

---

## 5. Markdown rendering

Wire up `goldmark` with sane defaults. No wiki links yet — that's task 7.

- Add `github.com/yuin/goldmark` dependency
- `internal/markdown/render.go`: a single `Render(md string) (template.HTML, error)` function using `goldmark.New(goldmark.WithExtensions(extension.GFM))`
- A test page `/_test/markdown` (only registered when `DEBUG=1`) renders a hard-coded markdown string
- `static/style.css`: **mobile-first.** Base layout is a centered single column with `max-width` around phone width (~640px) and auto horizontal margins, so desktop viewers get symmetric whitespace on both sides. Style `h1-h3, p, ul/ol, code, pre, blockquote, hr` for readable mobile typography (≥16px base).
- Serve `static/` via `http.FileServer`

**Done when:** `/_test/markdown?DEBUG=1` (or with the env flag) renders a hard-coded markdown string with proper styling. `go test ./internal/markdown/` passes a snapshot test.

---

## 6. Note view

Page at `/{user}/{path...}` looks up the note and renders it. 404 if not found.

- Use Go 1.22 mux pattern: `mux.HandleFunc("GET /{user}/{path...}", ...)`. The `path` segment is everything after the user. Last `/`-delimited piece = `slug`, the rest = `folder_path` (may be empty).
- e.g. `/shawn/music/mixing/why-i-rate-by-mood` → handle="shawn", folder_path="music/mixing", slug="why-i-rate-by-mood"
- Look up the note. Render title, author handle, folder breadcrumb, body (via `internal/markdown.Render`).
- Reserve top-level paths that conflict with feature routes (`/login`, `/auth/...`, `/write`, `/me`, `/feed`, `/api/...`, `/_test/...`) so they don't collide with handles. Either register them first (mux precedence handles this) or reject those handles at user creation in task 3.

**Done when:** Notes I wrote in task 4 render at their URLs. `/nonexistent/foo` returns 404.

---

## 7. Wiki link parser

Custom `goldmark` extension that matches `[[...]]` and turns it into an inline link node, then renders to an `<a>`.

- `internal/markdown/wikilink.go`: a `parser.InlineParser` registered for the `[` trigger byte. Parse `[[...]]` payloads.
- Parse all four syntax variants (per [CLAUDE.md](CLAUDE.md)): `[[slug]]`, `[[folder/slug]]`, `[[@user/slug]]`, `[[@user/folder/slug]]`. Pure parser: input string → `WikiLink{User, Folder, Slug, Raw string}`. Rendering is separate.
- For now every wiki link renders as `<a href="{guessedURL}">{label}</a>` based on the parsed parts; don't validate target existence yet.
- Unit-test the parser with table-driven tests covering all four variants and edge cases (whitespace, missing slug, unicode).

**Done when:** A note containing `[[foo]]` and `[[@alice/bar]]` renders with two clickable links pointing at `/{me}/foo` and `/alice/bar`. Parser tests pass.

---

## 8. Wiki link resolution + persistence

On note save, parse all wiki links and write to `links` with `resolved_target_id` set if the target exists.

- After INSERT/UPDATE of a note, parse its body with the wiki-link extension to extract `[]WikiLink`.
- Inside a transaction: `DELETE FROM links WHERE source_note_id = ?` then INSERT one row per parsed link. Each insert attempts resolution: SELECT a matching note by (handle, folder_path, slug); set `resolved_target_id` if found.
- Also re-resolve previously-unresolved links pointing at *this* note: `UPDATE links SET resolved_target_id = ? WHERE resolved_target_id IS NULL AND target_user_handle = ? AND target_folder_path = ? AND target_slug = ?`.
- Render unresolved links in note view differently: gray + dashed underline (CSS class `wiki-unresolved`).

**Done when:** Saving note A with `[[B]]` creates a links row (gray-dashed in A's view). Later creating note B updates that row's `resolved_target_id`. Reloading A's page shows the link in its resolved style.

---

## 9. Backlinks

On note view, query `links` for `resolved_target_id = current_note.id` and render a "Linked from" panel below the body.

- Show source note title + author handle + folder
- Sort by source note's `updated_at` DESC
- Empty state: don't render the panel at all if no backlinks

**Done when:** Note B links to note A → A's page shows "Linked from: B". Multiple backlinks all show, ordered correctly.

---

## 10. Profile page

`/{user}` shows the user's folder tree and recent notes.

- Folder tree: `SELECT DISTINCT folder_path FROM notes WHERE author_id = ? ORDER BY folder_path`. Build a nested tree in Go, render as a nested `<ul>`.
- Recent notes: 20 most recent, plain card list (title, excerpt, folder, date).
- Public — no auth gate in v0.
- Handle the conflict from task 6: this route is `GET /{user}` (exact, no trailing path), so it doesn't shadow the note view.

**Done when:** Visiting `/shawn` shows folders and recent notes.

---

## 11. Cross-vault link resolution

Tighten the resolver to fully handle `[[@user/...]]`. Targets in another vault must point to a real note.

- Add tests for each syntax variant, including: `@user` doesn't exist → unresolved; user exists but note doesn't → unresolved; both exist → resolved.
- The link-row schema already supports this via `target_user_handle`; the work here is making the resolver query branch correctly and adding the tests.

**Done when:** `[[@alice/some-note]]` renders resolved when alice's note exists, unresolved when it doesn't. Clicking resolved navigates to alice's note.

---

## 12. Feed

`/feed` — 50 most recent notes site-wide. Plain reverse-chronological list.

- Each row: title, excerpt (first ~150 chars of `body_md`, markdown-stripped — strip via a quick regex pass, not a full goldmark render), author handle, folder, relative time.
- Server-rendered, no client state.
- Click row → note view.

**Done when:** Multiple users have written notes; `/feed` shows them newest-first.

---

## 13. Markdown export

A "download .md" button on every note view. Sends raw `body_md` as a download.

- Route handler at `GET /api/notes/{id}/raw`
- `Content-Disposition: attachment; filename="<slug>.md"`
- `Content-Type: text/markdown; charset=utf-8`
- Body is the raw `body_md`, no transformation

**Done when:** Clicking the button downloads `note-slug.md` containing the raw source.

---

# After 13

You have a working v0. **Stop adding features.** Get 5 friends to actually use it. Find out which of the cut features (follow, likes, masonry feed, search, import-to-obsidian) people miss most before adding anything back.

The biggest risks aren't technical:

1. Nobody writes anything — onboarding friction is too high. Mitigate with a "fork this example vault" button.
2. The first writers don't link out — no rabbit holes form. Mitigate by seeding a few showcase vaults yourself.
3. The wiki link UX feels worse than Obsidian. Mitigate by adding autocomplete on `[[` early.
