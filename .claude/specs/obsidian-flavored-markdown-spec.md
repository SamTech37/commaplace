# Obsidian-flavored markdown — section embeds, block refs, hover preview

Spec from /askme, 2026-10-15.

## Goal

An Obsidian user publishes a vault to commaplace and it reads the way it does
in Obsidian: no raw syntax leaks, and links, embeds and previews behave the
same. A non-technical user who only writes on commaplace gets the same
features without managing files. The bar is **both**: imported vaults render
clean (no junk), and every construct we support behaves the way Obsidian's
reading view does, enforced by tests. Obsidian-flavored markdown (OFM) is the
one storage format. Later dialects (Roam, HackMD, WordPress) convert into it
on import.

## Already shipped (do not rebuild)

`internal/markdown/render.go` already handles GFM, footnotes, `%%comments%%`,
`==highlight==`, `$math$`, mermaid/math fences, foldable callouts, heading IDs,
`#tags`, `[[wikilinks]]` with `#anchor` and `|alias`, `![[note]]` embeds
(whole note, depth cap 2) and frontmatter stripping. Hover preview exists:
`static/linkpreview.js` + `GET /api/preview/{user}/{slug}`, which shows the
masonry card.

## Scope (v1)

1. **Section embeds.** `![[note#Heading]]` embeds only that heading's section.
   `![[note#^id]]` embeds only that block, but only while the block-ref flag
   is on (see 2).
2. **Block refs `^id`, built but off.** Code ships behind an in-code constant
   (`const blockRefs = false` in `internal/markdown`). Tests flip it through
   an unexported hook.
   - **Always on, flag or not:** a trailing ` ^id` on a paragraph or list item
     is stripped from reading view, so imported vaults stay clean.
   - **Flag off:** `[[note#^id]]` links to the top of the note.
     `![[note#^id]]` embeds the whole note.
   - **Flag on:** the block element gets `id="^id"` (exact attribute form
     still to be decided, see open questions). Links scroll to the block and
     embeds slice the block.
3. **Hover preview, global and section-aware.**
   - Bind it to the wiki-link primitive site-wide: load `linkpreview.js` from
     `layout.templ` instead of three page templates. Containers opt out with
     `data-no-preview`. Opt-outs go only where a preview is wrong, such as the
     editor and the preview's own popup.
   - Embeds are covered too, since their links are ordinary wiki links.
   - `[[note#Heading]]` previews that section. The endpoint takes an optional
     anchor parameter, and the cache varies on it.
4. **Editor.**
   - After `[[note#`, the autocomplete lists the target note's headings. This
     needs a headings endpoint, or a `#` branch in `/api/wiki/suggest`.
   - Section embeds render in the existing `POST /preview` pane. This should
     come for free once rendering lands; verify it.
5. **Golden tests.** `internal/markdown/testdata/ofm/` holds `.md` inputs and
   expected `.html` outputs. Each Obsidian-parity case cites the behavior rule
   it checks (see Design decisions, "app.js").

## Design decisions

- **app.js is a behavior reference, nothing more.** Obsidian's `app.js`
  contains the editor and reading-view logic, which is exactly the behavior
  we want to match. We study it with REA (`rea-agents`, installed in the
  session) only to answer specific edge-case questions. Findings are
  written down as prose rules plus golden test cases. No Obsidian code is
  copied into this repo. goldmark's AST is different anyway, so a port would
  not fit.
- **No frontmatter aliases in link resolution.** Letting a note claim extra
  names would let anyone pollute the shared namespace. Only the citing side
  can rename a link (`[[original title|label]]`), and that already works.
  Imported `[[alias]]` links stay unresolved.
- **No properties panel.** Frontmatter stays out of reading view, as it is
  today. This overrides the round-1 pick: the user later said no panel at all.
- **OFM is canonical; convert on import.** One renderer. Future dialect
  importers rewrite into OFM, for example Roam `((uid))` becomes a block ref
  and HackMD `:::info` becomes a callout.
- **The feature flag is an in-code constant, not an env var.** This is the
  user's call. Turning it on takes a deploy.
- **Slice in markdown, then render.** The embed resolver gets `link.Anchor`,
  extracts the section or block from the source markdown, and renders only
  that. Rendering the whole note and cutting the HTML would break footnotes,
  heading IDs and relative structure.

## Behavior rules (from Obsidian 1.14.4 `app.js`, read 2026-10-15)

Source: `obsidian-1.14.4.asar.gz` from `obsidianmd/obsidian-releases`. Its
SHA-256 matches `desktop-releases.json`. Rules are restated in prose; no code
was copied.

1. **Subpath parsing.** The text after the first `#` is split on `#`, and
   empty parts are dropped.
   - One part starting with `^` is a block ref.
   - One part of the form `[^x]` is a footnote ref (out of scope).
   - Anything else is a heading path.
2. **Heading match.** Both the heading text and the path part are normalized:
   - Each of ``!"#$%&()*+,.:;<=>?@^`{|}~/[]\`` and CR/LF is replaced with
     a space.
   - Runs of whitespace collapse to one space, and the result is trimmed and
     lowercased.

   Headings are walked in document order. Path part *k* matches only a
   heading deeper than the heading part *k-1* matched, so `#A#C` matches `C`
   anywhere below `A`, not only as a direct child. The first full match wins.
   [Inference] The heading text compared is the heading's source text, not
   its rendered text.
3. **Section extent.** A section runs from its heading to the next heading of
   the same or a higher level (level ≤ the target's), or to end of file. The
   heading line itself is included. Headings inside code fences are not
   headings, because the cache is built from the parsed tree.
4. **Block-ID grammar.** `^` followed by `[A-Za-z0-9-]+`. An ID is recognized
   in two forms:
   - **Inline:** at the very end of a block's text, preceded by whitespace.
     A `^` that starts the text does not count.
   - **Standalone:** a paragraph consisting only of `^id`, followed by end of
     input or a blank line. It attaches to the *previous* sibling block and is
     removed. Standalone as the first block, it is dropped.

   Placement rules:
   - **List items:** the ID found in an item (not counting its nested lists)
     belongs to that item.
   - **Other blocks** (quote, callout, table): an ID anywhere inside belongs
     to the outer block, and the last one wins.
   - **Lookup:** IDs are case-insensitive. A block ref is honored only as the
     sole subpath part, so `#H#^id` is a failed heading lookup.
5. **Block extent.** The block's own source range. For a list item, the range
   extends over every following item nested under it, so the sub-list is
   included.
6. **Missing target.** An embed whose section or block can't be found
   renders an h5 line, "unable to find section *subpath* in *file*". Links
   with a missing subpath still open the note. Our copy is
   `找不到段落 <code>#subpath</code>（<code>file</code>）`, in the existing
   `embed-missing` style.

## Open questions

- **Global hover cost.** Loading `linkpreview.js` on every page brings
  previews to the feed. Feed card excerpts don't render wiki links today, so
  confirm the global load is cheap and a no-op there.
- **Other N+1 queries.** `recomputeLinks` (on save) still runs two queries
  per link. That's the write path and outside this spec.

## Resolved during the build

- **Embed N+1:** fixed. `buildEmbedResolver` now runs one query per nesting
  level plus one for all the links inside embeds. Nested embeds now resolve
  against their own vault (they used to use the outer note's).
- **Nesting cap:** `maxEmbedDepth = 2` in `internal/handlers/links.go`.
  Lower it to 1 there when traffic warrants.
- **Block-ID HTML:** `id="^abc"`. Verified in Chromium 1194:
  - Fragment navigation to `#^abc` scrolls and sets `:target` natively.
  - `getElementById` works.
  - A raw `querySelector('#^abc')` throws, so JS selectors must use
    `CSS.escape`.

  `id="block-abc"` would need JS to make `#^abc` URLs work.

## Out of scope for v0

- Properties panel or any reader-facing frontmatter display.
- Resolving links through frontmatter `aliases`.
- `[[note#^` block-ID autocomplete and inserting generated `^id`s into other
  notes (it writes to a second note).
- Turning on the block-ref flag.
- Dialect importers for Roam, HackMD and WordPress/rich text. The direction
  is fixed (convert to OFM); the work is not scheduled.
- Canvas, Bases, Dataview, Excalidraw, and embedded attachments (no file
  hosting, by design; see `media.go`).
- Per-dialect renderers or a `notes.dialect` column.
