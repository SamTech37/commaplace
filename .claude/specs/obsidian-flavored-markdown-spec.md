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

## Behavior questions to settle from app.js

Each answer becomes a prose rule plus at least one golden test:

1. Where a heading section ends. Assumed: at the next heading of the same or
   a higher level, or at the end of the note. Do nested callouts or code
   fences change that?
2. How a heading anchor matches its heading: case, whitespace, markdown
   inside the heading, the nested-path form `#H1#H2`, and duplicate headings
   (first match?).
3. The block-ID grammar: allowed characters, where it may appear (paragraph,
   list item, table, quote, the line after a code fence), and how it attaches
   when it sits on its own line after a block.
4. How a block embed works for a list item: does it take the item alone or
   its sub-list too?
5. What a missing section shows: Obsidian's fallback for an unknown heading
   or `^id` in links, embeds and hover.

## Open questions

- **N+1 queries.** `buildEmbedResolver` runs one query per embed per depth
  level. Section embeds don't make that worse, but a note with many embeds
  is already N+1. Batch it now or later?
- **HTML id for block refs.** `^` is a legal `id` character in HTML5 but
  awkward in CSS selectors. Use `id="^abc"` (Obsidian-style URLs) or
  `id="block-abc"` (clean, but `#^abc` URLs then need JS)?
- **Global hover cost.** Loading `linkpreview.js` on every page brings
  previews to the feed. Feed card excerpts don't render wiki links today, so
  confirm the global load is cheap and a no-op there.
- **Getting app.js.** The asar is not in the container. Either the user
  supplies it, or it is fetched from the `obsidianmd/obsidian-releases`
  GitHub releases. Running REA against it needs the user's approval (the
  `rea doctor` call was blocked by the permission classifier).

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
