# MVP build plan

V0 proved the pipes work (write → publish → read → wiki-link → backlink). MVP is what gets 100 real people writing and reading on commaplace every week.

10 features in dependency order. Same rules as [v0-build-plan.md](v0-build-plan.md): one at a time, plan first, never bypass `go vet ./...` and `go test ./...`.

The three checkpoints marked **⏸ ASK ME** are UX decisions worth a real conversation before implementing — pause and surface options, don't pick. (When executed in batch mode, defaults will be picked and noted; the user can override after.)

Stack reminders: Go ≥1.22, SQLite (`modernc.org/sqlite`), `goldmark`, HTMX, no JS framework. See [CLAUDE.md](CLAUDE.md). Schema migrations are numbered SQL files in `internal/db/migrations/`.

---

## 1. Tags

Tags are the discovery scaffolding before we have search or recommendations.

- New table `note_tags(note_id, tag, created_at)` with `PRIMARY KEY (note_id, tag)` and an index on `tag`. Normalized rather than a JSON column — makes `/tag/{tag}` and per-tag counts simple SQL.
- Editor: a tags input below title, comma-separated. Normalize each tag (lowercase ASCII, drop symbols, collapse whitespace into `-`, trim, dedup). Unicode letters/digits allowed so `#混音` survives.
- Display tags as chip links in note view header.
- New page `/tag/{tag}` showing all notes with that tag, newest first. Add `tag` to `reservedHandles`.
- Click any tag → `/tag/{tag}`.

**Done when:** Add `混音, workflow` to a note. They appear under the title. `/tag/workflow` lists matching notes. `/tag/混音` lists matching notes (URL-encoded).

---

## 2. Likes

Lightest social signal. Calling the column "likes" internally; the UI shows a heart `♡`.

- New table `likes(user_id, note_id, created_at)` with `PRIMARY KEY (user_id, note_id)` and an index on `note_id` for fast count queries.
- Heart button on note view, posted via HTMX to `POST /api/like` which toggles the row and returns the updated heart+count fragment. Optimistic UI is fine; HTMX `hx-swap-oob` handles the swap.
- Per-note like count is computed in query (`SELECT COUNT(*) FROM likes WHERE note_id = ?`). Denormalize later if EXPLAIN says so.
- New page `/me/saved` listing notes I've liked, newest like first.

**Done when:** I can heart a note, the count goes up, refreshing keeps it. `/me/saved` shows everything I hearted in reverse-chrono.

---

## 3. Follow + following feed

The social spine. Without this, the feed is just "everyone everywhere"; with it, the platform has membership.

- New table `follows(follower_id, followed_id, created_at)` with `PRIMARY KEY (follower_id, followed_id)` and an index on `followed_id`.
- Follow / unfollow button on profile pages (hidden on own profile). HTMX `POST /api/follow` toggles, returns updated button + counts.
- Per-user follower / following counts on profile.
- `/feed` gets two tabs via `?tab=`: 推薦 (default — current site-wide behavior) and 追蹤中 (notes from people the viewer follows, newest first). Tabs render as a small nav above the list.

**Done when:** I follow someone, write a note as them in another browser, and the note appears on my 追蹤中 tab.

---

## 4. ⏸ ASK ME — Masonry feed with proper cards

This is the visual heart. Before implementing, surface design questions:

- Card variants: how do we decide which one to use? (Text excerpt vs list preview vs quote callout vs connection-preview-box for high-link-count notes?) Suggest a rule based on note structure.
- Mobile: do columns collapse to one or two? (Default suggestion: one column on mobile, matches our mobile-first norm.)
- Pagination vs infinite scroll: which? (Default suggestion: cursor-based "older" link at the bottom — simple, no JS state.)

After we decide, implement:

- Replace plain list `/feed` with CSS `column-count` masonry: 1 col below 720px, 2 cols at 720–1080, 3 cols above (still inside the centered column — no full-width).
- Card content variants per the rule we agree on; pick by inspecting note body structure on render.
- Card meta row: avatar dot (initials w/ hashed color), author handle, "in {folder}", like count `♡ N`, connection count `→ N`.
- Filter chips at top of feed: 全部 + top tags by recent volume (top 8, computed from `note_tags` joined to recent notes).

**Done when:** Feed renders as masonry with mixed-height cards, chips filter by tag, mobile collapses to one column gracefully.

---

## 5. Cross-vault visual differentiation

Make the network visible everywhere, not just in the data.

- On feed/profile cards: when a note's links include cross-vault targets (`links.target_user_handle != note's author handle`), show a small teal badge `→ N 跨` alongside `→ N`.
- Inline wiki links: same-vault renders info-blue (existing `wiki-resolved`), cross-vault renders teal (new `wiki-cross`). Both keep dotted underline. Teach the goldmark renderer to look at `link.User != "" && link.User != vaultHandle`.
- Note view's connections panel ("Linked from"): split into 「本 vault」(same author) and 「跨 vault · 連到別人」(different author).
- "From X" breadcrumb at top of note view when navigating via backlink — pass `?from=<note-id>` in backlink URLs, render a small banner above the note if the param is present and resolves; banner has a "← 回 {title}" link.

**Done when:** Walk through a cross-vault rabbit hole (e.g. alice → @bob/foo → backlink banner back to alice). Cross-vault links render teal, same-vault stays info-blue, "from X" banner appears and works.

> Note: the original plan referenced "the bishop → medieval careers rabbit hole I designed in chat" — that mockup wasn't shared. Implementing against the description above; if you have a specific visual mock, share it and I'll align.

---

## 6. ⏸ ASK ME — Wiki link autocomplete

Single biggest editor UX win.

Surface to me:
- Suggestion ranking: my own notes first, then people I follow, then site-wide popular? (Default: yes, exactly that order.)
- Should `[[@` immediately filter to other users? (Default: yes.)
- How do we handle ambiguous matches (multiple notes with the same title)? (Default: show full path `folder/slug` as secondary line, sort identical-titles by author with my own first.)

Then implement:

- When the user types `[[` in the editor, open a popup positioned under the caret. Use HTMX `hx-trigger="keyup changed delay:120ms"` to query `GET /api/wiki/suggest?q=...&me=...` which returns an HTML fragment of `<li>` results.
- Tiny vanilla JS (~30 lines, in `static/editor.js`): track caret, detect `[[` trigger, manage popup show/hide, arrow keys + Enter insert.
- Inserted text uses the right syntax: `[[slug]]` for own notes, `[[@user/slug]]` for cross-vault, with folder if needed.

**Done when:** Typing `[[mix` shows my mixing notes; typing `[[@al` shows alice's notes; selecting one inserts the right syntax.

---

## 7. Basic search

SQLite FTS5 is more than enough for v1.

- Migration creates `notes_fts` virtual table: `CREATE VIRTUAL TABLE notes_fts USING fts5(title, body_md, tokenize='unicode61 remove_diacritics 2', content='notes', content_rowid='id')`. Add INSERT/UPDATE/DELETE triggers on `notes` to keep `notes_fts` in sync. Backfill at migration time.
- Search bar at the top of every page (in `_base.html`); submit goes to `/search?q=...`.
- Search results page: matching notes with snippet from `snippet(notes_fts, 1, '<mark>', '</mark>', '…', 16)`, ranked by `bm25(notes_fts, 2.0, 1.0)` (title weighted 2x).
- Filter chips: 全部 / 我的 / 我追蹤的人 (the latter only when logged in).

**Done when:** Search "主教" returns matching notes with the term highlighted.

---

## 8. ⏸ ASK ME — Onboarding: example vault fork

Single biggest mitigation for empty-state failure.

Surface to me:
- What's IN the example vault? (Default suggestion: 12–15 notes under `@commaplace-tour` covering folders, wiki links, cross-vault links, tags. Outline below.)
- One example vault or three? (Default: one curated vault for v1.)
- Forking copies notes only, or also tags/folders verbatim? (Default: copy notes + folders + tags. Cross-vault links to other seeded users stay pointing at the originals.)

Then implement:

- A seed command `go run ./cmd/seed` (or a `--seed` flag on the server) creates the `@commaplace-tour` user plus a small handful of supporting users with linked content.
- New screen at signup (after magic-link consume but before /me): "Start fresh" or "Fork the tour" — defaults to fork.
- Fork action: copy all notes from the tour vault into the new user's vault, rewriting same-vault `[[slug]]` to stay same-vault (now in the new user's vault), and rewriting `[[@commaplace-tour/foo]]` to keep pointing at `@commaplace-tour/foo`.
- Welcome note pinned at top of the new user's `/me`. Add `users.pinned_note_id INTEGER NULL`.

**Done when:** New signup → defaults to fork → lands on their own profile pre-populated with the tour notes that already wiki-link to each other and to `@commaplace-tour`.

---

## 9. Polish pass

Accumulated UX debt of features 1–8.

- Avatars: gravatar fallback, then initials with hashed background color (small palette, deterministic by handle hash).
- Loading skeletons on feed and note view (CSS-only — server returns the page with placeholder shapes if any data is missing, but for HTMX-loaded fragments include a `<div class="skeleton">` that the response replaces).
- Empty states: "no notes yet, write your first" / "follow some people to populate this feed" / "nothing here yet" for /me/saved when empty.
- 404 and 500 pages with a tiny bit of personality (already have `error.html`; add a one-liner that depends on the code).
- Per-page `<title>` (already done) plus OG / Twitter card meta tags via a `{{block "meta" .}}` in `_base.html`.

**Done when:** Click around with throttled network — every page has a graceful loading and empty state. Share a note URL — preview card looks good in iMessage / Twitter / 微信.

---

## 10. Light moderation

Last because it matters once people show up.

- Report button on every note (any signed-in user). HTMX `POST /api/report` with `{note_id, reason}`.
- New table `reports(id, note_id, reporter_id, reason, created_at, status)` and `notes.hidden_at INTEGER NULL`.
- Admin user (env `ADMIN_HANDLE`) gets `/admin/reports` showing open reports.
- Admin can hide a note (sets `notes.hidden_at = now`). Hidden notes 404 for everyone except the author and the admin; backlinks/feed/search exclude them via `WHERE hidden_at IS NULL`.
- Email to admin on new report (uses existing `auth.Mailer`; in dev, logs to stdout).

**Done when:** I report a note as another user. Admin sees it on `/admin/reports`. Admin hides it. The original URL 404s for me but the author and admin still see it; `/feed` and `/search` no longer show it.

---

# After 10

You have an MVP. Resist the urge to add: comments, notifications, recommendations, native apps, the Obsidian plugin. Talk to your first 20 users about what they actually want before any of it.

Most likely lessons in priority order:
1. Writing flow needs to be way easier (mobile editor, better autocomplete, paste-from-Obsidian import).
2. The "from X" breadcrumb isn't enough — people lose their place. Real navigation history needed.
3. Notifications (someone liked your note, someone wrote a note linking to yours) drive return visits more than the feed does.
