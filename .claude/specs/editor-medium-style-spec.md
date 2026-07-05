> **Status: DONE.** Core (v1–v3) shipped and merged — EasyMDE surface,
> draft model, autosave/publish, uuid-canonical wikilink renderer, tag/reply
> prefill, profile 公開/草稿 tabs. All items from the v3 "Remaining deferred"
> list are now resolved (2026-07-05):
> - `![[...]]` embed renderer — done (`internal/markdown/render.go` `EmbedResolver`)
> - `![[@handle/slug]]` cross-vault embed — done (`buildEmbedResolver`, `notes.go:799`,
>   falls back to `vaultHandle` only when `link.User == ""`)
> - Vault-import menu slot — repurposed, not built as originally scoped: now an
>   `obsidian://` deep-link ("開啟於 Obsidian"), not an actual import-from-vault feature
> - Discard/back button on `/write` — done (`write.html` editor-bar, `notes.go` `GetEdit`)
> - Delete button inside the editor — done (`write.html`, dedicated `.editor-delete-btn`)
> - Slug-history redirect table — ruled out, not needed: `PatchNote` (notes.go:620-624)
>   only regenerates slug while unpublished; once published, slug is permanent by
>   construction, so there's nothing that could ever go stale to redirect.

# SPEC — Medium-style markdown editor ("一體成形" writing experience)

## Goal

Replace the current split editor (separate `title` field + raw `<textarea>`
+ separate rendered `#preview` pane) with a single unified writing surface in
the spirit of Medium, but markdown-native. The writer sees one document:
markdown they type, styled live as they type it. No source-vs-rendered split,
no separate title box. A floating toolbar appears on text selection (bold,
italic, wikilink, external link, H1, H2, quote, tag); a `+` menu appears on an
empty line (image upload, code, and later embeds). For commonplace's writers —
who write CJK and lean on `[[wikilinks]]` as the product's killer feature.

## Scope

### In (v1)

- **Unified `contenteditable` editor surface.** One markdown document, styled
  inline as typed. Markdown remains the byte-for-byte source of truth (stored
  verbatim in `notes.body_md`).
- **Live-preview styling — Typora-lite fidelity.** Block + inline elements
  render their *visual* styling (heading sizes, bold/italic weight, blockquote
  bar, code-block mono background, list indent) while the markdown marks
  (`#`, `**`, `` ` ``, `>`) stay **visible but dimmed grey**. No
  cursor-line hide/reshow.
- **First line = title.** The document's first line is the title. On save it is
  split off into `notes.title` (leading `# ` stripped if present); the remainder
  is `notes.body_md`. On edit-load the document is reconstructed as
  `title + "\n" + body_md`.
- **Selection toolbar** (appears on non-empty selection) — all 8:
  - **B** → wrap/unwrap `**selection**`
  - **i** → wrap/unwrap `*selection*`
  - **wikilink** → wrap `[[selection]]` and trigger the existing
    `/api/wiki/suggest` autocomplete
  - **http link** → wrap `[selection](url)` (URL entered inline)
  - **T (H1)** → toggle line prefix `# `
  - **t (H2)** → toggle line prefix `## `
  - **quote** → once = blockquote `> `; twice = block-level (see Open Questions)
  - **tag #** → wrap selection as `#tag` (inline tag; goldmark tag extension)
- **`+` menu** (appears on an empty line):
  - **close** (dismiss menu)
  - **image upload** → async upload to existing 1/note `note_images`, insert
    `![](…)` ref at cursor (see Image model)
  - **code** → once = inline `` `code` ``; twice = fenced ```` ```block``` ````
- **Draft model A.** `GET /write` immediately `INSERT`s an empty draft note and
  hands its ID to the editor, so a real note ID always exists (needed for inline
  upload + autosave). A `published_at` column (NULL = draft) gates visibility.
- **Autosave.** Debounced `PATCH` of the document to the draft/note while typing,
  with a "Saved" status indicator. Explicit **Publish** sets `published_at`.

### Out (explicit non-goals for v1)

- Full Obsidian-style hide-on-inactive-line / reshow-on-cursor-line. (Typora-lite
  only.)
- Multiple inline images per note. (Kept at **1 image per note** — existing
  schema; second upload replaces the first.)
- `md embed` (`![[ ]]`) button — deferred per author. (`+` menu shows it later.)
- Any WYSIWYG / HTML-source data model (rejected: would break wikilinks via
  HTML→MD roundtrip; see Design decisions).
- Collaborative editing, version history beyond autosave, comments.
- New editor library / build step (rejected — hand-rolled vanilla JS).

## Design decisions

1. **Live-preview source, not WYSIWYG.** Markdown is the source of truth, stored
   verbatim. The editor only *styles* that source. Rejected the true-WYSIWYG /
   contenteditable-HTML model (yabwe/medium-editor + medium-editor-markdown via
   Turndown) because: (a) Turndown has no knowledge of `[[wikilinks]]`,
   callouts, tags, math → roundtrip destroys the product's killer feature;
   (b) HTML↔MD never roundtrips cleanly → stored markdown rots each edit;
   (c) medium-editor is abandoned (last release Dec 2017); (d) contenteditable
   HTML serialization + CJK IME is the worst-bugs path.

2. **EasyMDE (CodeMirror 5), vendored — NOT contenteditable.** *(Supersedes the
   original contenteditable decision.)* EasyMDE's markdown mode renders headings
   large + bold/italic styled inline with marks visible (= Typora-lite), giving
   the single styled surface without hand-rolling a decoration engine. A
   `<textarea>` upgrades into it. Stored value is byte-for-byte markdown
   (`easymde.value()`), so wikilinks/obsidian syntax are untouched and the note
   page still renders via server goldmark. Implemented in
   `internal/handlers/static/cmeditor.js` + `easymde.min.{js,css}` (vendored like
   `htmx.min.js`, no build step).

3. **Use the library, don't hand-roll.** CM5 handles the three hazards natively —
   **CJK IME composition, caret, undo/redo** — which is the whole reason to vendor
   it. FontAwesome is avoided by giving toolbar buttons text labels (`text:` on
   each button). EasyMDE's built-in preview is disabled (goldmark on the published
   page is canonical). In-editor highlighting of obsidian-specific syntax
   (`[[ ]]`, `#tag`, `==hl==`, `[!callout]`) is deferred — a CM5 overlay mode can
   add it later; it renders fully on the page regardless.

4. **The note-view render stays server-side goldmark.** The editor's inline
   styling is approximate/visual only; the canonical HTML on the note page is
   still produced by `internal/markdown` (goldmark + wikilink/callout/tag/math
   extensions). No client/server markdown-parity requirement for stored output.

5. **First line = title, split on save.** `body_md` excludes the first line;
   `title` holds it (this preserves current `note.html`, feed, profile, search,
   and slug logic). Editor reconstructs the unified document on edit-load.

6. **Image model unchanged (1/note).** Reuse `note_images` (PK `note_id`,
   `bytea`). Add an **async upload endpoint** `POST /api/notes/{id}/image`
   (new) that stores/replaces the single image and returns its URL; the existing
   `GET /api/notes/{id}/image` serves it. `+` upload inserts `![](…)` at the
   cursor; a second upload replaces the blob and reuses the same ref/URL.

7. **Draft row on first edit (model A).** Chosen over temp-staging (B) and
   defer (C) because it makes the editor itself simplest (a real ID always
   exists → inline upload, autosave, wikilink resolution all "just work") and
   unlocks autosave for free. Cost — accepted — is one schema column plus a
   `published_at IS NOT NULL` predicate added to every read path.

8. **Rename strategy: uuid-canonical (finish the postgres-spec intent).** Slugs
   and titles stay freely editable — NOT frozen. This closes an implementation
   gap against `postgres-railway-rebuild-spec.md` §"Identity resolution: UUID
   pointers, not name resolution" ("renaming must never break an existing
   `[[@handle/note]]` link"). That intent is already ~90% built — `links.
   resolved_target_id` is stored + maintained, and graph/backlinks/outgoing are
   all uuid-keyed (rename-safe). The lone gap: the wikilink **renderer** still
   builds the `<a href>` from the author-typed slug instead of the resolved
   target's *current* slug. Fix:
   - Change `markdown.Resolver` from `func(WikiLink) bool` (existence only) to
     return the target's current `(handle, slug, title)` or nil.
   - `wikiRenderer` builds `href` (and optionally label) from that current data.
   - `buildResolver` (`notes.go`) joins `resolved_target_id → notes/users` to
     supply current handle/slug.
   Result: renaming a note updates every in-app `[[link]]` + the graph
   automatically, zero body rewrites. The author's typed slug in the body
   becomes "intent"; display + href come from the live target via uuid.
   **Residual (deferred):** an external/bookmarked URL `/{handle}/oldslug` still
   404s — fix later with a slug-history redirect table or `/{handle}/slug-{shortid}`
   URLs. In-app, nothing breaks.

## Data / API changes

- **Migration `002_*.sql`**: `ALTER TABLE notes ADD COLUMN published_at BIGINT;`
  Backfill existing notes `SET published_at = created_at` (all current notes are
  published). NULL = draft.
- **Read-path edits** (add `AND published_at IS NOT NULL`, except author viewing
  own / admin): feed, profile, search (FTS), graph (`/api/graph*`), tag pages,
  backlinks/outgoing splits, likes/saved. *Broad but mechanical — TDD each.*
- **New endpoints**:
  - `GET /write` — now creates a draft, returns editor bound to its ID.
  - `PATCH /api/notes/{id}` (or equivalent) — autosave the document
    (split title/body, recompute links/tags), draft stays unpublished.
  - `POST /api/notes/{id}/publish` (or fold into existing `POST /edit/{id}`) —
    set `published_at`.
  - `POST /api/notes/{id}/image` — async image upload, returns URL.
- **Orphan drafts**: empty drafts (no title + empty body) abandoned at `/write`
  are lazy-swept — deleted when older than 7 days, on next visit (no scheduled
  job for v1). Excluded from all reads regardless.
- **Wikilink href via uuid**: `markdown.Resolver` returns current
  `(handle, slug, title)` or nil; `wikiRenderer` builds href from it (decision 8).

## Resolved decisions (were open questions)

1. **Quote button once/twice** → once = `> ` blockquote; twice = `> [!quote]`
   callout (obsidian-markdown; `calloutExt` already supports it). No inline-quote
   primitive exists, so this is the two quote levels we actually have.
2. **Rename / slug on title edit** → slugs + titles freely editable, NOT frozen.
   Handled by the uuid-canonical renderer fix (decision 8). Supersedes the earlier
   freeze proposal.
3. **Orphan-draft GC** → lazy sweep, delete empty drafts >7 days on next visit.
4. **Autosave on already-published notes** → pushes changes live instantly
   (the note is already public).
5. **http-link URL entry** → inline mini-input in the toolbar (not `prompt()`).
6. **Mobile selection toolbar** → same floating toolbar on touch-selection,
   positioned above the on-screen keyboard.

## Out of scope for v0

- `md embed` (`![[ ]]`) button.
- Multiple inline images / image positioning beyond the single 1/note ref.
- Full Obsidian hide/reshow live preview.
- Editor library / build step.
- Collaboration, version history, comments.


# Refinements v2:


   What was done 1~2 sessions ago

    Backend bugs fixed:
    - loadOutgoingSplit: removed ORDER BY n.updated_at DESC from SELECT DISTINCT (Postgres
    rejects ORDER BY on non-select columns under DISTINCT — outgoing links section was silently
    empty)
    - PatchNote: slug now frozen after publish — was regenerating from title on every autosave,
    silently breaking published note URLs
    - PublishNote: rejects publish when title is empty or slug still starts with draft- (422
    with human-readable message surfaced in editor status bar)
    - GetNote / GetNoteImage: both gate unpublished drafts to author/admin only
    - Resolver type changed from func(WikiLink) bool to func(WikiLink) *ResolvedTarget —
    UUID-canonical wikilinks; renamed targets auto-update inbound links without rewriting note
    bodies

    Frontend fixes:
    - save() returns Promise always (inflight path polls 50ms until clear); publish chains via
    .then() to avoid race
    - loaded flag skips initial EasyMDE change event so edit-load doesn't trigger autosave
    - Autocomplete popup: position: fixed + cursorCoords("window") for correct placement
    regardless of scroll position
    - Editor auto-grows with content: viewportMargin: Infinity + overflow: hidden on scroll
    containers
    - Autocomplete results capped to LIMIT 6 (no scrollbar in popup)
    - insertWikiLink: only moves caret inside [[]] when no text selected

    New architecture:
    - Draft model: GET /write creates an empty draft immediately; autosave via PATCH
    /api/notes/{id}; explicit POST /api/notes/{id}/publish
    - published_at column (migration 002): NULL = draft, non-NULL = published; all read paths
    filter on this
    - Orphan draft sweep: empty drafts >7 days deleted on next /write visit

    Deferred (next session)

    UX gaps:
    1. No discard/back button on /write — users can't abandon a draft without navigating away
    manually (draft stays, gets swept in 7 days if empty)
    2. No delete button inside the editor (only accessible via note view page)
    3. Reply-prefill: navigating to /write?reply-to=<id> creates a draft with body pre-filled
    but no title — slug stays draft-xxxxxxxx until title is typed and autosave fires

    CJK URLs: accepted as-is (percent-encoded in copy-from-address-bar, visually decoded in
    browser bar) — Option A

    Key files changed

    internal/handlers/notes.go         — PatchNote, PublishNote, draft model, resolver
    internal/handlers/handlers.go      — routes: PATCH/POST /api/notes/{id}/[publish|image]
    internal/handlers/note_image.go    — published_at gate on GetNoteImage
    internal/handlers/static/cmeditor.js — full EasyMDE editor JS (new file)
    internal/handlers/static/style.css — EasyMDE section
    internal/handlers/templates/write.html — replaced form with EasyMDE surface
    internal/handlers/wiki.go          — LIMIT 6 for autocomplete
    internal/markdown/render.go        — ResolvedTarget struct, Resolver type change
    internal/db/migrations/002_add_published_at.sql — new migration


# Refinements v3 (current session)

## Done

Profile page — "公開 | 草稿" tabs:
- `loadRecentNotes` takes `tab` param; default = published only; `?tab=drafts` = drafts only (self only)
- `IsDraft bool` added to `profileNote` struct (for future use)
- Profile template shows `<nav class="feed-tabs">` with 公開 / 草稿 tabs when `IsSelf`
- HTMX load-more URL threads `&tab=` through
- Visitors still see plain heading (no tabs)

Note action menu:
- Only ♡ like stays inline
- Everything else moved into ⋯ menu with separators: 回覆 | (sep) 編輯 / 刪除 | (sep) 匯入(disabled/TODO) / 下載 / 複製 | (sep) 檢舉
- "匯入到我的 vault" kept as disabled placeholder button (TODO: not yet implemented)
- `.action-menu-sep`, `.action-menu-danger`, `.action-menu-disabled` CSS classes added

Navbar hamburger:
- ≡ button appears on ≤600px; flat nav-links hidden
- Reuses `.action-menu` + `.action-menu-list` — no new dropdown styles
- Both hamburger and note menu close on outside-click (copy.js delegated listener)

Reply prefill (`/write?reply-to=<id>`):
- Title prefilled: `Re: {original title}`
- Body: `![[slug]]` (same vault) or `![[@ handle/slug]]` (cross-vault) + `\n\n---\n\n`
- Embed syntax consistent across vault boundaries; renderer support deferred

## Status

73 tests pass. Branch clean. All deferred items from v2 resolved. Ready to merge.

## Remaining deferred (post-merge)

- Discard/back button in editor (draft sweeps itself in 7 days if empty)
- Delete button inside editor (accessible via note view for now)
- `![[...]]` embed renderer (goldmark extension) — syntax is in place, just renders as broken image/plain text for now
- `![[@ handle/slug]]` cross-vault embed renderer
- Vault import feature (button placeholder exists in ⋯ menu)
- Slug-history redirect table for external/bookmarked URLs after rename
