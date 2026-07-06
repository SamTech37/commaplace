# Share Button + Open Graph — Spec

Status: **v0 Done**

## Goal

Notes look good and are easy to share outside commaplace. Two parts:
1. A share button on the note page (Web Share API on mobile, copy-link fallback on desktop).
2. Open Graph / Twitter Card meta tags so pasting a note URL into iMessage/Slack/Discord/X/LINE renders a title, description, and image — not a bare link.

## Scope

**In:**
- Share button on the published note page (`note.html`), one location: near the author bar / note actions.
- `og:description`, `og:image`, `og:url`, `twitter:card` added to `note.html`'s existing `{{define "meta"}}` block (which already has `og:site_name`, `og:title`, `og:type`, `og:article:author`).
- A `resolveOGImage(note)`-style helper for image fallback logic.

**Out (see "Out of scope for v0"):**
- Feed/profile card share buttons (note page only for v0).
- Dynamically generated per-note OG image (title/author rendered onto a template, Vercel-OG/Reddit-style).
- Share targets other than native OS share sheet + copy-link (no explicit X/Facebook/LINE buttons).
- Share button on drafts, edit view, or admin-hidden notes.

## Design decisions

### Share button
- Icon-only button, inline hand-written SVG (no icon library dependency), placed on the note page only.
- Click handler: `navigator.share({title, url})` if available (mobile Safari/Chrome/most Android browsers); else `navigator.clipboard.writeText(url)` + a toast confirming "連結已複製" ("Link copied").
- Only rendered when the note is published and visible to the current viewer — i.e. gated the same way the page itself already gates hidden/draft content (`IsHidden`/`ViewerLoggedIn` checks already in `note.html`). Not shown on `/write`, `/edit/{id}`.

### OG description
- Reuse the existing `markdown.Excerpt` function (already used for feed-card previews — strips links/images/embeds), truncated to ~160 chars, from `Note.BodyMD`.
- No new truncation/stripping logic — call the same function already used elsewhere.

### OG image
- Priority: note's own uploaded image (existing 1-per-note upload at `GET /api/notes/{id}/image`, presence checked via existing `noteHasImage(r, s.DB, n.ID)` helper) → else one static site-wide default image (new static asset, e.g. `static/og-default.png`).
- Implemented as a single resolver function, e.g. `resolveOGImage(note) string` returning an absolute URL, called from the template data prep (same place `AuthorHandle`/`AuthorStats` etc. are already assembled for `note.html`).
- **No new route.** This function is intentionally the entire "scaffold" for a future dynamically-generated card: swapping to generation later means rewriting this one function's body (e.g. to call a new render step and cache it), not touching the route table or `note.html`. Decided against pre-adding a stub `/api/notes/{id}/og-image.png` route now — that hedge only pays off if we're sure we'll build generation later, and costs a route+handler today for no present benefit (YAGNI).
- `twitter:card` changes from the current `summary` to `summary_large_image` when an image is present (note image or default), since a real image benefits from the larger card. Existing hardcoded `<meta name="twitter:card" content="summary" />` in `note.html` becomes conditional/templated on whether `resolveOGImage` returned a real image.

### og:url / canonical
- Add `og:url` pointing at the canonical `/{handle}/{slug}` URL — needed because scrapers use it to dedupe and because the note may be reachable via more than one path historically (slug freezes on publish per existing `PatchNote` behavior, so this is stable once published).

### Draft/hidden handling
- Share button: hidden entirely on non-published/non-visible views (drafts, edit view). No share affordance where there's nothing shareable yet.
- OG tags on hidden (admin-hidden) notes: keep existing generic behavior — don't populate real title/description/image for a hidden note, since link-preview scrapers ignore auth/session and would otherwise leak hidden content to anyone who pastes the link. (Exact current hidden-page OG behavior should be checked against `IsHidden` handling in `note.html` before implementation — if it currently still emits real tags even when hidden, that's a pre-existing gap this work should also close, not introduce.)

## Open questions

- None blocking — all major decisions resolved above. Minor implementation detail to confirm during coding: absolute vs relative URL construction for `og:image`/`og:url` (needs `BASE_URL` config, already used for magic-link emails per README — reuse that, don't invent a second base-URL source).

## Out of scope for v0

- Dynamically generated OG image (title+author rendered as an image server-side). Deferred until there's a concrete reason to invest (e.g. note-image coverage is heavily used and imageless notes look bad enough in previews to justify it).
- Share buttons on feed cards / profile page notes — note-page only for v0.
- Explicit social-network share buttons (X/Facebook/LINE deep links) beyond the native OS share sheet.
- Any analytics/tracking on share-button clicks.
