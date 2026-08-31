# Feed Layout Toggle — Spec

> **Status: v0 shipped, then partly reverted; v1 substrate done, toggle UI deliberately
> deferred.** See "v0 — as built" below for the original deviations, then the
> "Update 2026-08-31" note right after it for what changed since. v1 scope
> (universal view interface) starts further down the file; "v1 — as built"
> immediately under that header covers what actually happened.

## Goal

Add a client-side layout switcher to `/feed` so users can view the same note feed as a dense list, the current aligned card grid, or a masonry column layout. Preference persists in `localStorage`. The graph layout is intentionally deferred; graph stays its own page.

This is the first concrete expression of the **meta-app** idea: one data source, multiple views. Handler unification (making all note-listing pages — `/tag`, `/search`, profile — share one template) is out of scope here but is the natural next step.

---

## v0 — as built (what actually shipped)

Reality diverged from the plan below. Corrections:

- ~~Default layout `cards`~~ → **default `grid`** (param `cards` renamed to `grid`).
- ~~HTMX swap on `.feed-container`~~ → **plain `<a href>` nav.** The HTMX
  `outerHTML` swap double-rendered the container (clicked button was inside the
  swap target). Plain links are correct and simpler; full reload is cheap.
- ~~`list` = title + meta only, no excerpt~~ → **list reuses `.note-card`**
  (title + 1-line excerpt + `@author · time`) — bare rows were too sparse.
- ~~masonry = `.feed-masonry` reusing `.entry`~~ → **restored old `.masonry` /
  `.masonry-card`** variant cards (text / quote / bullet-list / link-chips) from
  commit `58a8c1b`. The flat-`.entry`-in-columns version looked identical to grid.
- ~~`_feed_grid.html` partial~~ → not extracted; layout branch lives inline in
  `feed.html` + `feed_partial.html`.
- **Added (not in original plan):** all preview text is markdown-stripped
  (`markdown.Excerpt` now drops `[x](y)`, `![]()`, `![[embed]]`); masonry shows
  the first body image as a thumbnail (`markdown.FirstImageURL`).

Done checklist:
- [x] `/feed` three layouts: `list`, `grid`, `masonry`
- [x] Toggle UI inline above the grid
- [x] Preference saved to `localStorage` key `feed.layout` + restore on load
- [x] Infinite scroll propagates active layout param
- [x] Clean (no raw markdown) previews across all layouts
- [x] Image thumbnail in masonry only

**Update 2026-08-31.** All of the above stopped being true sometime after this
was written — a later commit, `014ed21` ("masonry-only cards with shadow lift
+ meta hierarchy"), reverted `/feed` to masonry-only. No toggle, no `list`/
`grid`, no `feed.layout` in `localStorage` (verified: no such key in any
static JS, `feed.html` at the time only rendered `masonry_card`). This spec
was never updated to say so — found by reading `git log` against the live
code during the v1 work below, not from this document. Treat every claim
above as **historical** (what v0 shipped, once), not current state.

---

## ~~Scope (original plan — superseded by "as built")~~

---

## Scope

**In v1:**
- `/feed` only
- Three layouts: `list`, `cards` (current default), `masonry`
- Toggle UI inline above the grid, scrolls with page
- Preference saved to `localStorage` key `feed.layout`
- HTMX swap on toggle (server returns layout-appropriate HTML fragment)
- Infinite scroll (`?older=`) must propagate the active layout param

**Explicit non-goals:**
- Graph as an inline feed layout (graph stays at `/graph`)
- `/tag`, `/search`, profile — unified handler pass, separate work
- Server-side layout persistence (no DB column)
- Mobile-specific layouts (masonry collapses to 1 col; that's enough)
- Timeline, canvas, kanban — later

---

## Design Decisions

### HTMX swap, not CSS-only toggle

**Why:** Graph layout (deferred) requires completely different DOM. Committing to HTMX swap now means the mechanism is consistent when graph is added later. CSS-only toggle would also need separate handling for masonry (CSS columns reorder visually, acceptable, but HTMX is cleaner overall).

**How:** Toggle buttons emit `hx-get="/feed?layout=X"` with `hx-target=".feed-container"` (the wrapper including the toggle itself) and `hx-swap="outerHTML"`. The server re-renders the toggle (with the active state correct) + the grid.

**Why target the whole container, not just the grid:** The toggle buttons need to update their active state (highlight the selected layout). If only the grid swaps, the buttons don't re-render. Swapping the whole `.feed-container` (toggle row + grid) keeps the active state correct without JS.

### Layout param propagates to infinite scroll

The `feed_partial.html` sentinel (the "load more" trigger) must include `?layout=X&older=Y`. The server writes this into the template so the next chunk renders in the correct layout. No JS required to track layout mode.

### localStorage on page load

On `DOMContentLoaded`, read `localStorage.getItem('feed.layout')`. If it differs from the server-rendered layout (detectable via `data-layout` attribute on `.feed-container`), trigger an HTMX GET to swap. This avoids a flash of wrong layout on repeat visits without coupling the server to the preference.

### Masonry implementation: CSS columns

`column-count: 2` (desktop), `column-count: 1` (mobile). Cards break naturally. No JS masonry library. Column ordering (top-to-bottom, then left-to-right) is a known tradeoff — acceptable for notes, where chronological strict ordering is secondary to density.

### List layout

Title + `@author` + relative time, single row. No excerpt, no tags. Extremely dense. Useful for power users scanning by title.

---

## Layout Definitions

| Layout  | Container class   | Card class  | Structure                                     |
|---------|-------------------|-------------|-----------------------------------------------|
| `cards` | `feed-grid`       | `entry`     | Current aligned 1–2 col grid. Default.        |
| `list`  | `feed-list`       | `entry-row` | Single-col, title + meta per row. No excerpt. |
| `masonry`| `feed-masonry`   | `entry`     | CSS columns: 2 (≥768px), 1 (mobile).         |

Note: masonry reuses the same `.entry` card HTML as `cards`. Only the container changes.

---

## Toggle UI

```
[ ≡ List ]  [ ⊞ Cards ]  [ ⊟ Masonry ]
─────────────────────────────
[note card] [note card]
[note card] [note card]
...
```

- Three icon buttons, horizontally grouped, above the grid
- Active layout gets `.active` class (visual highlight)
- Each button: `hx-get="/feed?layout=X" hx-target=".feed-container" hx-swap="outerHTML"`
- On swap success, JS updates `localStorage.setItem('feed.layout', X)`

---

## Server Changes

### `feed.go`

- Parse `r.URL.Query().Get("layout")` → default `"cards"`; accept `list`, `cards`, `masonry`
- Pass `Layout string` to template data

### `feed.html`

- Wrap toggle + grid in `<div class="feed-container" data-layout="{{.Layout}}">`
- Render toggle buttons with active state based on `{{.Layout}}`
- Delegate grid HTML to partial `_feed_grid.html` (new, extracts the layout-specific container)

### `feed_partial.html` (infinite scroll chunk)

- Receives `Layout` in template data
- Renders cards in correct layout container
- Sentinel `hx-get` includes `?layout={{.Layout}}&older={{.OlderCursor}}`

---

## Open Questions

1. **Transition animation** — should layout swap animate (fade/crossfade)? HTMX `htmx:afterSwap` hook could add a CSS transition. Probably skip for v1.
2. **Empty state** — all three layouts need a "no notes" state. Currently only cards has one. Confirm list/masonry inherit it.
3. **First-visit default** — if `localStorage` is empty, should we default to `cards` (safe) or try to detect screen size and start at `masonry` on wide screens?

---

## Out of Scope for v0

- Graph as feed layout (inline force-directed view)
- Universal handler refactor (unify /feed, /tag, /search, profile)
- Per-layout sort options (by date asc/desc, by likes — the sort controls in the Windows screenshot)
- Timeline, canvas, kanban views from `plan.md`

----

# v1 — Universal View Interface (the meta-app)

## v1 — as built (2026-08-31)

Shipped alongside the `html/template` → `templ` migration (`docs/DECISIONS.md`
5), not before it, per that decision's own sequencing call. Reality diverged
from the plan below in one deliberate way and matched it in the rest:

- **No toggle UI, on purpose.** The "list ✅ / grid ✅" table below was already
  false by the time this work started (see the v0 update above) — the
  question wasn't "which layout to ship" but "make adding one cheap." Built
  the pluggable half (`cardRenderers map[string]func(feedCard)
  templ.Component` in `internal/handlers/notes_view.templ`, one entry
  registered: `"masonry"`) and stopped there — a toggle with one option is
  dead chrome. Adding `list`/`grid`/anything else later is one function + one
  registry line, no handler changes; still zero surfaces expose a switcher.
- **`NoteListView{ Cards, Layout, OlderURL, Empty }`** — built as scoped,
  `Empty` typed `templ.Component` (not `string`) so a surface can embed a real
  link in its empty state (feed's "寫第一篇" stayed clickable).
- **All five surfaces, not four** — this spec's own "Critical files" list
  below only names `{feed,tags,search,profile}.go`; `plan.md` separately
  flagged the spec forgot `saved` (`/me/saved`). Converted it too, plus a
  sixth consumer this spec doesn't mention at all: `preview.go`'s
  `/api/preview/{user}/{slug}` hover-preview endpoint, which shares the same
  `feedCard`/`masonryCard` rendering.
- **Pagination added, not just layout.** `tag`/`saved` were hard `LIMIT
  200`/`LIMIT 50` with no "load more" before this — real `updated_at`/
  `created_at` cursor pagination added (feed's exact pattern), since giving
  them the shared infinite-scroll component only makes sense if there's
  something to scroll to. `search` explicitly did not get this (`ts_rank`
  ordering has no cheap cursor) — stays capped, no sentinel.
- **One infinite-scroll mechanism, not two.** Profile had its own bespoke
  self-`outerHTML`-swap sentinel, different from feed's OOB-append pattern —
  unified on feed's (see `.claude/htmx-rules.md` 4). Found in the process:
  profile's HX-Request branch was calling `RenderPartial(..., "profile",
  ...)` against a partial name that was never registered — a live 500,
  uncaught by any test, fixed as a side effect of the rewrite.
- **A real bug fixed while porting `search.html`, unrelated to layout:**
  `search.go`'s snippet field was raw `template.HTML` around Postgres
  `ts_headline` output — `ts_headline` doesn't escape the surrounding text,
  only inserts `<mark>` tags, so a note body containing HTML-looking text
  would execute unescaped for anyone whose search matched it. Fixed with a
  helper that escapes everything except the `<mark>` delimiters.
- Open questions 1–4 below: **Q1** (one `localStorage` pref vs per-surface) is
  moot with no toggle built. **Q2** (drafts-tab vs toggle ordering) N/A, same
  reason. **Q3** (masonry thumbnails everywhere) — yes, resolved as scoped,
  every surface shows them now. **Q4** (per-surface empty text) — done.

**Goal:** a pluggable **view substrate** — one data source (a note query), many
view renderers. Not just unifying the four list pages; that's step 1. The end
state is a registry of views the user toggles between on any note collection,
per the Meta-App roster in `plan.md:29-43`:

| View | Status |
|------|--------|
| list | ✅ v0 |
| grid | ✅ v0 |
| masonry | ✅ v0 |
| graph | 🟡 exists, funky (no card-style nodes, double-click nav) |
| timeline (linear) | ⬜ |
| canvas (static draggables, non-shaky graph) | ⬜ |
| calendar (date view) | ⬜ |
| dora mode (wiki-explore, star-graph spotlight) | ⬜ |
| RSVP reader | ⬜ |
| kanban? | ⬜ maybe |

User estimate: ~5–8 of these are actually worth building. We have 4 today.

**Step 1 (this v1):** every note-listing page (`/feed`, `/tag/{tag}`, `/search`,
profile `/{user}`) becomes the *same* view component fed by a different query.
One toggle, one card-template set, one localStorage preference — everywhere notes
are listed. Today these are four handlers with four near-duplicate templates. v0
proved the view layer (list/grid/masonry + clean cards) on `/feed` alone; v1
lifts it out so the four handlers become thin query-producers.

**Step 2+ (later):** the toggle's layout enum becomes a view registry. Adding
timeline / canvas / calendar / dora = registering a renderer that consumes the
same `[]feedCard` (or a richer shared note model), not a new page. Graph folds in
here once it renders card-style nodes. Design step 1's `NoteListView` so the
`Layout` field can grow into this without a rewrite.

## Why now

- v0's `card` / `card_row` / `masonry_card` templates + layout toggle already are
  a reusable view. They just live in `feed.html`.
- `/tag`, `/search`, profile currently render their own `.note-card` lists with
  no layout choice and inconsistent excerpt handling (some still leak raw md —
  the `markdown.Excerpt` fix covers the data, but each template is its own copy).
- Unifying kills 3 template copies and makes "add a new layout" a one-place change.

## Scope (v1)

**In:**
- Extract the v0 view into a shared partial — `_notes_view.html` defining
  `notes_view` (toggle + layout branch + the three card templates). Move
  `card` / `card_row` / `masonry_card` there.
- A common Go view-model: `type NoteListView struct { Cards []feedCard; Layout
  string; OlderURL string; Empty string /* per-page empty message */ }`. All four
  handlers build feedCards (already the feed shape) + this wrapper.
- Wire the toggle + `?layout=` + localStorage onto `/tag`, `/search`, profile.
  localStorage key stays `feed.layout` (one global preference) — or scope per
  surface (open question below).
- Each surface keeps its own header (tag chips, search box, vault masthead) above
  the shared `notes_view`.

**Out (v1):**
- Graph as an inline layout (still its own page).
- Sort controls (date/likes) — v2.
- Server-side layout persistence (DB column) — localStorage stays the store.
- Timeline / canvas / kanban.

## Critical files

- `internal/handlers/templates/feed.html` — source of the view to extract.
- New `internal/handlers/templates/_notes_view.html`.
- `internal/handlers/{feed,tags,search,profile}.go` — adopt `NoteListView`.
- `internal/handlers/templates/{tag,search,profile}.html` — replace bespoke
  `.note-card` lists with `{{template "notes_view" .View}}`.
- `internal/handlers/feed.go` — `feedCard` + `analyzeCardBody` are the shared
  card model; reuse as-is.

## Open questions (v1)

1. **One preference or per-surface?** Single `feed.layout` for all listings, or
   `view.layout.{feed,tag,search,profile}`? Single is simpler; per-surface lets a
   user keep profile as list but feed as masonry.
2. **Profile drafts tab** — profile has a `公開 / 草稿` sub-nav. Does the layout
   toggle live above or below it? (Above — toggle is view-level, tab is filter-level.)
3. **Masonry image thumbnail everywhere?** v0 limited it to masonry on `/feed`.
   Keep that rule globally, or let tag/search masonry show thumbnails too? (Yes —
   it's a property of the masonry layout, not the surface.)
4. **Empty-state text** — per-surface message string passed in (`No notes carry
   #x` vs `No results for "q"` vs `還沒有筆記`).

## Verification (v1)

- `/feed`, `/tag/{t}`, `/search?q=`, `/{user}` all show the toggle; switching
  layout works and persists across surfaces (or per-surface per Q1).
- Infinite scroll on each surface stays in the active layout.
- No raw markdown in any card on any surface.
- Masonry thumbnails render where a body image exists.
- Existing handler tests still pass; add one per surface asserting `notes_view`
  renders with a chosen layout.