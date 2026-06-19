# HTMX Canon — commaplace

Distilled from htmx.org/docs (htmx **2.0.4**, vendored) + an audit of this repo
(2026-06-20). Rules that matter for OUR patterns, not generic advice.

1. **Detect htmx with the header, not a query param.**
   `r.Header.Get("HX-Request") == "true"` — NOT `?partial=1`.
   htmx sends `HX-Request: true` automatically on every AJAX request.

2. **Same URL serves full vs partial → set `Vary: HX-Request`.**
   Done in `render.go` `Render` + `RenderPartial`. Without it, a CDN/browser can
   cache a fragment and serve it as a full page (or vice-versa).

3. **Partials live in `Pages.partials` (no `_base.html`). Full pages in `Pages.cache`.**
   Returning a full `<html>…</html>` to an htmx request is the #1 anti-pattern.

4. **Infinite scroll pattern (feed):**
   - sentinel: `hx-trigger="revealed"`, `hx-target=".feed-items"`, `hx-swap="beforeend"`
   - partial response: new cards + an OOB sentinel
     (`hx-swap-oob="outerHTML"` with stable `id="feed-sentinel"`)

5. **Self-replacing fragments (like / follow / report):**
   `hx-target="this" hx-swap="outerHTML"`; handler returns HTML that re-includes
   its own `hx-*` attributes so behavior carries forward.

6. **Never hand-append params htmx provides.** No `&partial=1`. The header carries it.

7. **OOB needs a matching `id` already in the live DOM.** Missing id = htmx silently
   does nothing (no error). Give every OOB target a stable id.

8. **`allowNestedOobSwaps` defaults `true` in 2.x.** OOB elements nested inside the
   swap target ARE processed (that's how the feed sentinel works). Don't flip it off.

9. **Loading feedback:** htmx adds class `htmx-request` to the triggering element
   during a request; style a sibling `.htmx-indicator` spinner. (Not wired yet.)

10. **Plain `<a>` for nav / tabs / layout toggle is correct.** htmx where it isn't
    needed is itself the anti-pattern — the layout toggle deliberately uses links
    after an `outerHTML` swap double-rendered (see feed-layout-toggle-spec.md).

## Available but unused (adopt per-feature, not preemptively)
- `HX-Trigger` response header → fire a client event for toasts (report/like/follow).
- `hx-boost` on `<body>`/`<nav>` → SPA-feel nav, free (only if templates support
  body-only swaps).
- `HX-Redirect` / `HX-Location` response headers → client redirect from a fragment.
